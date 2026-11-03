package trading

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

type brokerFetchState struct {
	isFetchInFlight   bool
	fetchWaitChannel  chan struct{}
	lastFetchTime     time.Time
	lastFetchedOrders []OrderUpdateEvent
	lastFetchErr      error
}

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

	// Per-mode coalesced singleflight-style REST fetch cache ("PAPER" vs "LIVE")
	fetchMu           sync.Mutex
	fetchStatesByMode map[string]*brokerFetchState
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
		fetchStatesByMode: map[string]*brokerFetchState{
			"PAPER": {},
			"LIVE":  {},
		},
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

// SyncOrdersCoalesced fetches all orders from the specified broker, coalescing concurrent callers per broker mode within 500ms
func (reconciler *OrderReconciler) SyncOrdersCoalesced(ctx context.Context, broker ExecutionBroker) ([]OrderUpdateEvent, error) {
	if broker == nil {
		return nil, nil
	}

	brokerMode := broker.Mode()
	if brokerMode == "" {
		brokerMode = "PAPER"
	}

	reconciler.fetchMu.Lock()
	if reconciler.fetchStatesByMode == nil {
		reconciler.fetchStatesByMode = make(map[string]*brokerFetchState)
	}
	fetchState, isModeTracked := reconciler.fetchStatesByMode[brokerMode]
	if !isModeTracked {
		fetchState = &brokerFetchState{}
		reconciler.fetchStatesByMode[brokerMode] = fetchState
	}

	if time.Since(fetchState.lastFetchTime) < 500*time.Millisecond && fetchState.lastFetchErr == nil {
		cachedOrderEvents := fetchState.lastFetchedOrders
		reconciler.fetchMu.Unlock()
		return cachedOrderEvents, nil
	}
	if fetchState.isFetchInFlight {
		waitChannel := fetchState.fetchWaitChannel
		reconciler.fetchMu.Unlock()
		select {
		case <-waitChannel:
			reconciler.fetchMu.Lock()
			fetchedOrderEvents, fetchErr := fetchState.lastFetchedOrders, fetchState.lastFetchErr
			reconciler.fetchMu.Unlock()
			return fetchedOrderEvents, fetchErr
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	fetchState.isFetchInFlight = true
	fetchState.fetchWaitChannel = make(chan struct{})
	waitChannel := fetchState.fetchWaitChannel
	reconciler.fetchMu.Unlock()

	orderEvents, fetchErr := broker.FetchOrders(ctx)

	reconciler.fetchMu.Lock()
	fetchState.lastFetchTime = time.Now()
	fetchState.lastFetchedOrders = orderEvents
	fetchState.lastFetchErr = fetchErr
	fetchState.isFetchInFlight = false
	close(waitChannel)
	reconciler.fetchMu.Unlock()

	if fetchErr == nil {
		for _, orderEvent := range orderEvents {
			reconciler.ProcessOrderEvent(orderEvent)
		}
	}
	return orderEvents, fetchErr
}
