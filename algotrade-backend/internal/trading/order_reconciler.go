package trading

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
	db                          *sql.DB
	mu                          sync.Mutex
	lastFilledQuantityByOrderID map[string]int
	positionIDByOrderID         map[string]string
	legTypeByOrderID            map[string]string
	pendingEventsByOrderID      map[string]OrderUpdateEvent
	onOrderDelta                func(positionID, legType string, orderEvent OrderUpdateEvent, deltaFilled int)

	// Coalesced singleflight-style REST fetch cache
	fetchMu         sync.Mutex
	isFetchInFlight bool
	fetchWaitChannel chan struct{}
	lastFetchTime   time.Time
	lastFetchedList []OrderUpdateEvent
	lastFetchErr    error
}

func NewOrderReconciler(
	db *sql.DB,
	onOrderDelta func(positionID, legType string, orderEvent OrderUpdateEvent, deltaFilled int),
) *OrderReconciler {
	return &OrderReconciler{
		db:                          db,
		lastFilledQuantityByOrderID: make(map[string]int),
		positionIDByOrderID:         make(map[string]string),
		legTypeByOrderID:            make(map[string]string),
		pendingEventsByOrderID:      make(map[string]OrderUpdateEvent),
		onOrderDelta:                onOrderDelta,
	}
}

// SeedOrderFilled pre-populates lastFilledQuantityByOrderID during crash-recovery rehydration so already-counted fills are never re-applied
func (reconciler *OrderReconciler) SeedOrderFilled(orderID string, filledQty int) {
	if orderID == "" || filledQty <= 0 {
		return
	}
	reconciler.mu.Lock()
	if filledQty > reconciler.lastFilledQuantityByOrderID[orderID] {
		reconciler.lastFilledQuantityByOrderID[orderID] = filledQty
	}
	reconciler.mu.Unlock()
}

// RegisterOrder links a broker orderID to a positionID and legType ("ENTRY" | "SL" | "SCALE_OUT" | "EMERGENCY_EXIT")
func (reconciler *OrderReconciler) RegisterOrder(
	orderID, positionID, legType, symbol, txnType, orderType string,
	qty int,
	price, triggerPrice float64,
	status string,
) {
	if orderID == "" {
		return
	}
	reconciler.mu.Lock()
	reconciler.positionIDByOrderID[orderID] = positionID
	reconciler.legTypeByOrderID[orderID] = legType
	pendingOrderEvent, hasPendingOrderEvent := reconciler.pendingEventsByOrderID[orderID]
	if hasPendingOrderEvent {
		delete(reconciler.pendingEventsByOrderID, orderID)
	}
	reconciler.mu.Unlock()

	if reconciler.db != nil {
		_, err := reconciler.db.Exec(`
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
	if hasPendingOrderEvent {
		reconciler.ProcessOrderEvent(pendingOrderEvent)
	}
}

// HandleWebSocketTextFrame parses Kite Ticker JSON text frames and routes order postbacks
func (reconciler *OrderReconciler) HandleWebSocketTextFrame(raw []byte) {
	var envelope struct {
		Type string           `json:"type"`
		Data OrderUpdateEvent `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return
	}
	if envelope.Type == "order" && envelope.Data.OrderID != "" {
		reconciler.ProcessOrderEvent(envelope.Data)
	}
}

// ProcessOrderEvent deduplicates an incoming order update and emits exact incremental fill deltas
func (reconciler *OrderReconciler) ProcessOrderEvent(orderEvent OrderUpdateEvent) {
	if orderEvent.OrderID == "" {
		return
	}

	reconciler.mu.Lock()
	posID, tracked := reconciler.positionIDByOrderID[orderEvent.OrderID]
	if !tracked {
		// Falsification Guard (Bug #10): Never mutate lastFilledQuantityByOrderID before the order is registered!
		// Buffer early-arriving WebSocket/Paper events so RegisterOrder replays them immediately.
		prevPending, ok := reconciler.pendingEventsByOrderID[orderEvent.OrderID]
		if !ok || orderEvent.FilledQuantity >= prevPending.FilledQuantity {
			reconciler.pendingEventsByOrderID[orderEvent.OrderID] = orderEvent
		}
		reconciler.mu.Unlock()
		return
	}
	legType := reconciler.legTypeByOrderID[orderEvent.OrderID]
	prevFilled := reconciler.lastFilledQuantityByOrderID[orderEvent.OrderID]
	delta := 0
	if orderEvent.FilledQuantity > prevFilled {
		delta = orderEvent.FilledQuantity - prevFilled
		reconciler.lastFilledQuantityByOrderID[orderEvent.OrderID] = orderEvent.FilledQuantity
	}
	reconciler.mu.Unlock()

	if reconciler.db != nil {
		_, _ = reconciler.db.Exec(`
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
		`, orderEvent.OrderType, orderEvent.OrderType, orderEvent.Quantity, orderEvent.Quantity, orderEvent.FilledQuantity,
			orderEvent.Price, orderEvent.TriggerPrice, orderEvent.AveragePrice, orderEvent.AveragePrice,
			orderEvent.Status, orderEvent.StatusMessage, orderEvent.OrderID)
	}

	if reconciler.onOrderDelta != nil {
		reconciler.onOrderDelta(posID, legType, orderEvent, delta)
	}
}

// SyncOrdersCoalesced fetches all orders from the active broker, coalescing concurrent callers within 500ms
func (reconciler *OrderReconciler) SyncOrdersCoalesced(ctx context.Context, broker ExecutionBroker) ([]OrderUpdateEvent, error) {
	if broker == nil {
		return nil, nil
	}

	reconciler.fetchMu.Lock()
	if time.Since(reconciler.lastFetchTime) < 500*time.Millisecond && reconciler.lastFetchErr == nil {
		cached := reconciler.lastFetchedList
		reconciler.fetchMu.Unlock()
		return cached, nil
	}
	if reconciler.isFetchInFlight {
		waitCh := reconciler.fetchWaitChannel
		reconciler.fetchMu.Unlock()
		select {
		case <-waitCh:
			reconciler.fetchMu.Lock()
			res, err := reconciler.lastFetchedList, reconciler.lastFetchErr
			reconciler.fetchMu.Unlock()
			return res, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	reconciler.isFetchInFlight = true
	reconciler.fetchWaitChannel = make(chan struct{})
	waitCh := reconciler.fetchWaitChannel
	reconciler.fetchMu.Unlock()

	events, err := broker.FetchOrders(ctx)

	reconciler.fetchMu.Lock()
	reconciler.lastFetchTime = time.Now()
	reconciler.lastFetchedList = events
	reconciler.lastFetchErr = err
	reconciler.isFetchInFlight = false
	close(waitCh)
	reconciler.fetchMu.Unlock()

	if err == nil {
		for _, orderEvent := range events {
			reconciler.ProcessOrderEvent(orderEvent)
		}
	}
	return events, err
}
