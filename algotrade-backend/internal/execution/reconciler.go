package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// OrderReconciler deduplicates order updates across WebSocket, Webhook, and REST polling,
// and coalesces concurrent FetchOrders calls so multiple PositionWorkers never trigger HTTP 429.
type OrderReconciler struct {
	db               *sql.DB
	mu               sync.Mutex
	lastFilledByOrd  map[string]int
	orderToPos       map[string]string
	orderLegType     map[string]string
	pendingUntracked map[string]OrderUpdateEvent
	onOrderDelta     func(positionID, legType string, ev OrderUpdateEvent, deltaFilled int)

	// Coalesced singleflight-style REST fetch cache
	fetchMu         sync.Mutex
	fetchInFlight   bool
	fetchWaitCh     chan struct{}
	lastFetchTime   time.Time
	lastFetchedList []OrderUpdateEvent
	lastFetchErr    error
}

func NewOrderReconciler(
	db *sql.DB,
	onOrderDelta func(positionID, legType string, ev OrderUpdateEvent, deltaFilled int),
) *OrderReconciler {
	return &OrderReconciler{
		db:               db,
		lastFilledByOrd:  make(map[string]int),
		orderToPos:       make(map[string]string),
		orderLegType:     make(map[string]string),
		pendingUntracked: make(map[string]OrderUpdateEvent),
		onOrderDelta:     onOrderDelta,
	}
}

// SeedOrderFilled pre-populates lastFilledByOrd during crash-recovery rehydration so already-counted fills are never re-applied
func (r *OrderReconciler) SeedOrderFilled(orderID string, filledQty int) {
	if orderID == "" || filledQty <= 0 {
		return
	}
	r.mu.Lock()
	if filledQty > r.lastFilledByOrd[orderID] {
		r.lastFilledByOrd[orderID] = filledQty
	}
	r.mu.Unlock()
}

// RegisterOrder links a broker orderID to a positionID and legType ("ENTRY" | "SL" | "SCALE_OUT" | "EMERGENCY_EXIT")
func (r *OrderReconciler) RegisterOrder(
	orderID, positionID, legType, symbol, txnType, orderType string,
	qty int,
	price, triggerPrice float64,
	status string,
) {
	if orderID == "" {
		return
	}
	r.mu.Lock()
	r.orderToPos[orderID] = positionID
	r.orderLegType[orderID] = legType
	pendingEv, hasPending := r.pendingUntracked[orderID]
	if hasPending {
		delete(r.pendingUntracked, orderID)
	}
	r.mu.Unlock()

	if r.db != nil {
		_, err := r.db.Exec(`
			INSERT INTO orders (
				order_id, position_id, leg_type, symbol, transaction_type, order_type,
				quantity, filled_quantity, price, trigger_price, average_price, status, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, 0, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(order_id) DO UPDATE SET
				position_id = excluded.position_id,
				leg_type = excluded.leg_type,
				order_type = excluded.order_type,
				quantity = excluded.quantity,
				price = excluded.price,
				trigger_price = excluded.trigger_price,
				status = CASE WHEN orders.status = 'COMPLETE' THEN orders.status ELSE excluded.status END,
				updated_at = CURRENT_TIMESTAMP;
		`, orderID, positionID, legType, symbol, txnType, orderType, qty, price, triggerPrice, status)
		if err != nil {
			slog.Warn("Failed to persist registered order to SQLite", "order_id", orderID, "error", err)
		}
	}

	// Replay any early WebSocket/Paper fill event that arrived before RegisterOrder completed
	if hasPending {
		r.ProcessOrderEvent(pendingEv)
	}
}

// HandleWebSocketTextFrame parses Kite Ticker JSON text frames and routes order postbacks
func (r *OrderReconciler) HandleWebSocketTextFrame(raw []byte) {
	var envelope struct {
		Type string           `json:"type"`
		Data OrderUpdateEvent `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return
	}
	if envelope.Type == "order" && envelope.Data.OrderID != "" {
		r.ProcessOrderEvent(envelope.Data)
	}
}

// ProcessOrderEvent deduplicates an incoming order update and emits exact incremental fill deltas
func (r *OrderReconciler) ProcessOrderEvent(ev OrderUpdateEvent) {
	if ev.OrderID == "" {
		return
	}

	r.mu.Lock()
	posID, tracked := r.orderToPos[ev.OrderID]
	if !tracked {
		// Falsification Guard (Bug #10): Never mutate lastFilledByOrd before the order is registered!
		// Buffer early-arriving WebSocket/Paper events so RegisterOrder replays them immediately.
		prevPending, ok := r.pendingUntracked[ev.OrderID]
		if !ok || ev.FilledQuantity >= prevPending.FilledQuantity {
			r.pendingUntracked[ev.OrderID] = ev
		}
		r.mu.Unlock()
		return
	}
	legType := r.orderLegType[ev.OrderID]
	prevFilled := r.lastFilledByOrd[ev.OrderID]
	delta := 0
	if ev.FilledQuantity > prevFilled {
		delta = ev.FilledQuantity - prevFilled
		r.lastFilledByOrd[ev.OrderID] = ev.FilledQuantity
	}
	r.mu.Unlock()

	if r.db != nil {
		_, _ = r.db.Exec(`
			UPDATE orders SET
				order_type = CASE WHEN ? != '' THEN ? ELSE order_type END,
				quantity = CASE WHEN ? > 0 THEN ? ELSE quantity END,
				filled_quantity = MAX(filled_quantity, ?),
				price = ?,
				trigger_price = ?,
				average_price = CASE WHEN ? > 0 THEN ? ELSE average_price END,
				status = ?,
				status_message = ?,
				updated_at = CURRENT_TIMESTAMP
			WHERE order_id = ?;
		`, ev.OrderType, ev.OrderType, ev.Quantity, ev.Quantity, ev.FilledQuantity,
			ev.Price, ev.TriggerPrice, ev.AveragePrice, ev.AveragePrice,
			ev.Status, ev.StatusMessage, ev.OrderID)
	}

	if r.onOrderDelta != nil {
		r.onOrderDelta(posID, legType, ev, delta)
	}
}

// SyncOrdersCoalesced fetches all orders from the active broker, coalescing concurrent callers within 250ms
func (r *OrderReconciler) SyncOrdersCoalesced(ctx context.Context, broker ExecutionBroker) ([]OrderUpdateEvent, error) {
	if broker == nil {
		return nil, nil
	}

	r.fetchMu.Lock()
	if time.Since(r.lastFetchTime) < 250*time.Millisecond && r.lastFetchErr == nil {
		cached := r.lastFetchedList
		r.fetchMu.Unlock()
		return cached, nil
	}
	if r.fetchInFlight {
		waitCh := r.fetchWaitCh
		r.fetchMu.Unlock()
		select {
		case <-waitCh:
			r.fetchMu.Lock()
			res, err := r.lastFetchedList, r.lastFetchErr
			r.fetchMu.Unlock()
			return res, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	r.fetchInFlight = true
	r.fetchWaitCh = make(chan struct{})
	waitCh := r.fetchWaitCh
	r.fetchMu.Unlock()

	events, err := broker.FetchOrders(ctx)

	r.fetchMu.Lock()
	r.lastFetchTime = time.Now()
	r.lastFetchedList = events
	r.lastFetchErr = err
	r.fetchInFlight = false
	close(waitCh)
	r.fetchMu.Unlock()

	if err == nil {
		for _, ev := range events {
			r.ProcessOrderEvent(ev)
		}
	}
	return events, err
}
