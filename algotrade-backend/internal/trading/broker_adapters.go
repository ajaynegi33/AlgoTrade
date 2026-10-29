package trading

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

const (
	OrderTag               = "AT26LONG"
	DefaultVariety         = "regular"
	ExchangeNSE            = "NSE"
	ProductMIS             = "MIS"
	ValidityDay            = "DAY"
	OrderStatusComplete    = "COMPLETE"
	OrderStatusRejected    = "REJECTED"
	OrderStatusCancelled   = "CANCELLED"
	OrderStatusOpen        = "OPEN"
	OrderStatusTrigPending = "TRIGGER PENDING"
)

var (
	ErrMaxModificationsReached = errors.New("order has reached NSE maximum 25 modifications limit")
	ErrTriggerAboveLTP         = errors.New("sell SL trigger price must be strictly below current LTP")
	ErrInvalidQuantity         = errors.New("order quantity must be greater than zero")
)

// OrderUpdateEvent represents a normalized order state update from WebSocket, Webhook, REST, or Paper OMS
type OrderUpdateEvent struct {
	OrderID         string  `json:"order_id"`
	Symbol          string  `json:"tradingsymbol"`
	InstrumentToken uint32  `json:"instrument_token"`
	TransactionType string  `json:"transaction_type"` // "BUY" | "SELL"
	OrderType       string  `json:"order_type"`       // "LIMIT" | "MARKET" | "SL"
	Status          string  `json:"status"`           // "COMPLETE" | "OPEN" | "TRIGGER PENDING" | "REJECTED" | "CANCELLED"
	StatusMessage   string  `json:"status_message"`
	Quantity        int     `json:"quantity"`
	FilledQuantity  int     `json:"filled_quantity"`
	PendingQuantity int     `json:"pending_quantity"`
	Price           float64 `json:"price"`
	TriggerPrice    float64 `json:"trigger_price"`
	AveragePrice    float64 `json:"average_price"`
	Tag             string  `json:"tag"`
	OrderTimestamp  string  `json:"order_timestamp"`
	Checksum        string  `json:"checksum"`
}

// ExecutionBroker abstracts Live Zerodha Kite OMS vs High-Fidelity 5-Depth Paper OMS
type ExecutionBroker interface {
	Mode() string
	PlaceLimitBuy(ctx context.Context, symbol string, token uint32, qty int, limitPrice float64, stock *market.StockMetrics) (string, error)
	PlaceProtectedMarketSell(ctx context.Context, symbol string, token uint32, qty int, currentLTP, tickSize float64, stock *market.StockMetrics) (string, error)
	PlaceEmergencySweepSell(ctx context.Context, symbol string, token uint32, qty int, currentLTP, tickSize, maxSlippagePct float64, stock *market.StockMetrics) (string, error)
	PlaceSLOrder(ctx context.Context, symbol string, token uint32, qty int, triggerPrice, limitPrice, currentLTP, tickSize float64) (string, error)
	ModifySLOrder(ctx context.Context, orderID string, newQty int, newTrigger, newLimit, currentLTP, tickSize float64) error
	MutateSLToMarket(ctx context.Context, orderID string, qty int, stock *market.StockMetrics) error
	CancelOrder(ctx context.Context, orderID string) error
	FetchOrders(ctx context.Context) ([]OrderUpdateEvent, error)
}


// =========================================================================
// 1. Live Zerodha Broker Adapter (ap-south-1 Optimized)
// =========================================================================

type ZerodhaLiveBroker struct {
	kiteService *kite.Service
	rateMu      sync.Mutex
	tokens      float64
	lastRefill  time.Time
}

func NewZerodhaLiveBroker(kiteService *kite.Service) *ZerodhaLiveBroker {
	return &ZerodhaLiveBroker{
		kiteService: kiteService,
		tokens:      6.0,
		lastRefill:  time.Now(),
	}
}

func (broker *ZerodhaLiveBroker) Mode() string {
	return "LIVE"
}

// waitRateLimit enforces an 8 req/sec token bucket (staying safely inside Zerodha's 10 req/sec order mutation limit)
func (broker *ZerodhaLiveBroker) waitRateLimit(ctx context.Context) error {
	for {
		broker.rateMu.Lock()
		now := time.Now()
		elapsed := now.Sub(broker.lastRefill).Seconds()
		broker.tokens = math.Min(6.0, broker.tokens+elapsed*8.0)
		broker.lastRefill = now
		if broker.tokens >= 1.0 {
			broker.tokens -= 1.0
			broker.rateMu.Unlock()
			return nil
		}
		waitDuration := time.Duration(((1.0 - broker.tokens) / 8.0) * float64(time.Second))
		broker.rateMu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
		}
	}
}

func (broker *ZerodhaLiveBroker) PlaceLimitBuy(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	limitPrice float64,
	_ *market.StockMetrics,
) (string, error) {
	if qty <= 0 {
		return "", ErrInvalidQuantity
	}
	if err := broker.waitRateLimit(ctx); err != nil {
		return "", err
	}
	client := broker.kiteService.GetClient()
	session := broker.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return "", kite.ErrNoSession
	}

	resp, err := client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
		Exchange:        ExchangeNSE,
		Tradingsymbol:   symbol,
		Validity:        ValidityDay,
		Product:         ProductMIS,
		OrderType:       "LIMIT",
		TransactionType: "BUY",
		Quantity:        qty,
		Price:           limitPrice,
		Tag:             OrderTag,
	})
	if err != nil {
		return "", fmt.Errorf("zerodha PlaceLimitBuy failed for %s: %w", symbol, err)
	}
	return resp.OrderID, nil
}

func (broker *ZerodhaLiveBroker) PlaceProtectedMarketSell(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	currentLTP, tickSize float64,
	stock *market.StockMetrics,
) (string, error) {
	return broker.PlaceEmergencySweepSell(ctx, symbol, token, qty, currentLTP, tickSize, 0.0075, stock)
}

func (broker *ZerodhaLiveBroker) PlaceEmergencySweepSell(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	currentLTP, tickSize, maxSlippagePct float64,
	stock *market.StockMetrics,
) (string, error) {
	if qty <= 0 {
		return "", ErrInvalidQuantity
	}
	if err := broker.waitRateLimit(ctx); err != nil {
		return "", err
	}
	client := broker.kiteService.GetClient()
	session := broker.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return "", kite.ErrNoSession
	}
	if tickSize <= 0 {
		tickSize = 0.05
	}
	if maxSlippagePct <= 0 {
		maxSlippagePct = 0.0075
	}

	// Anchor to min(currentLTP, BestBidPrice) so an air-pocket gap in bids is swept immediately
	refPrice := currentLTP
	if stock != nil && stock.BidLevels[0].Price > 0 {
		bid1 := float64(stock.BidLevels[0].Price)
		if refPrice <= 0 || bid1 < refPrice {
			refPrice = bid1
		}
	}

	// Stage C Final Escalation (>= 3.5%): attempt direct MARKET sell first, fallback to deep LPP limit
	if maxSlippagePct >= 0.035 {
		mktResp, mktErr := client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
			Exchange:        ExchangeNSE,
			Tradingsymbol:   symbol,
			Validity:        ValidityDay,
			Product:         ProductMIS,
			OrderType:       "MARKET",
			TransactionType: "SELL",
			Quantity:        qty,
			Tag:             OrderTag,
		})
		if mktErr == nil && mktResp.OrderID != "" {
			return mktResp.OrderID, nil
		}
	}

	protectiveLimit := RoundDownToTick(math.Max(tickSize, refPrice*(1.0-maxSlippagePct)), tickSize)
	resp, err := client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
		Exchange:        ExchangeNSE,
		Tradingsymbol:   symbol,
		Validity:        ValidityDay,
		Product:         ProductMIS,
		OrderType:       "LIMIT",
		TransactionType: "SELL",
		Quantity:        qty,
		Price:           protectiveLimit,
		Tag:             OrderTag,
	})
	if err != nil {
		return "", fmt.Errorf("zerodha PlaceEmergencySweepSell (slip=%.2f%%) failed for %s: %w", maxSlippagePct*100, symbol, err)
	}
	return resp.OrderID, nil
}

func (broker *ZerodhaLiveBroker) PlaceSLOrder(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	triggerPrice, limitPrice, currentLTP, tickSize float64,
) (string, error) {
	if qty <= 0 {
		return "", ErrInvalidQuantity
	}
	if tickSize <= 0 {
		tickSize = 0.05
	}
	if currentLTP > 0 && triggerPrice >= currentLTP-tickSize {
		return "", fmt.Errorf("%w: trigger=%.2f, ltp=%.2f", ErrTriggerAboveLTP, triggerPrice, currentLTP)
	}
	if limitPrice <= 0 || limitPrice > triggerPrice {
		limitPrice = ComputeSellSLLimitPrice(triggerPrice, tickSize)
	}
	if err := broker.waitRateLimit(ctx); err != nil {
		return "", err
	}
	client := broker.kiteService.GetClient()
	session := broker.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return "", kite.ErrNoSession
	}

	resp, err := client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
		Exchange:        ExchangeNSE,
		Tradingsymbol:   symbol,
		Validity:        ValidityDay,
		Product:         ProductMIS,
		OrderType:       "SL",
		TransactionType: "SELL",
		Quantity:        qty,
		TriggerPrice:    triggerPrice,
		Price:           limitPrice,
		Tag:             OrderTag,
	})
	if err != nil {
		return "", fmt.Errorf("zerodha PlaceSLOrder failed for %s: %w", symbol, err)
	}
	return resp.OrderID, nil
}

func (broker *ZerodhaLiveBroker) ModifySLOrder(
	ctx context.Context,
	orderID string,
	newQty int,
	newTrigger, newLimit, currentLTP, tickSize float64,
) error {
	if newQty <= 0 {
		return ErrInvalidQuantity
	}
	if tickSize <= 0 {
		tickSize = 0.05
	}
	if currentLTP > 0 && newTrigger >= currentLTP-tickSize {
		return fmt.Errorf("%w: newTrigger=%.2f, ltp=%.2f", ErrTriggerAboveLTP, newTrigger, currentLTP)
	}
	if newLimit <= 0 || newLimit > newTrigger {
		newLimit = ComputeSellSLLimitPrice(newTrigger, tickSize)
	}
	if err := broker.waitRateLimit(ctx); err != nil {
		return err
	}
	_, err := broker.kiteService.RawModifyOrderPUT(ctx, DefaultVariety, orderID, "SL", newQty, newLimit, newTrigger)
	return err
}

func (broker *ZerodhaLiveBroker) MutateSLToMarket(
	ctx context.Context,
	orderID string,
	qty int,
	_ *market.StockMetrics,
) error {
	if qty <= 0 {
		return ErrInvalidQuantity
	}
	if err := broker.waitRateLimit(ctx); err != nil {
		return err
	}
	_, err := broker.kiteService.RawModifyOrderPUT(ctx, DefaultVariety, orderID, "MARKET", qty, 0.0, 0.0)
	return err
}

func (broker *ZerodhaLiveBroker) CancelOrder(ctx context.Context, orderID string) error {
	if orderID == "" {
		return errors.New("order_id cannot be empty")
	}
	if err := broker.waitRateLimit(ctx); err != nil {
		return err
	}
	client := broker.kiteService.GetClient()
	session := broker.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return kite.ErrNoSession
	}
	_, err := client.CancelOrder(DefaultVariety, orderID, nil)
	return err
}

func (broker *ZerodhaLiveBroker) FetchOrders(ctx context.Context) ([]OrderUpdateEvent, error) {
	client := broker.kiteService.GetClient()
	session := broker.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return nil, kite.ErrNoSession
	}
	orders, err := client.GetOrders()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch orders from kite: %w", err)
	}
	events := make([]OrderUpdateEvent, 0, len(orders))
	for _, o := range orders {
		events = append(events, OrderUpdateEvent{
			OrderID:         o.OrderID,
			Symbol:          o.TradingSymbol,
			InstrumentToken: o.InstrumentToken,
			TransactionType: o.TransactionType,
			OrderType:       o.OrderType,
			Status:          o.Status,
			StatusMessage:   o.StatusMessage,
			Quantity:        int(o.Quantity),
			FilledQuantity:  int(o.FilledQuantity),
			PendingQuantity: int(o.PendingQuantity),
			Price:           o.Price,
			TriggerPrice:    o.TriggerPrice,
			AveragePrice:    o.AveragePrice,
			Tag:             o.Tag,
		})
	}
	return events, nil
}

// =========================================================================
// 2. High-Fidelity 5-Level Depth-Walking Paper Broker
// =========================================================================

type paperRestingOrder struct {
	OrderID         string
	Symbol          string
	InstrumentToken uint32
	TransactionType string
	OrderType       string
	Status          string
	Quantity        int
	FilledQuantity  int
	Price           float64
	TriggerPrice    float64
	AveragePrice    float64
	ModCount        int
	UpdatedAt       time.Time
}

type PaperBroker struct {
	mu          sync.RWMutex
	seq         atomic.Uint64
	orders      map[string]*paperRestingOrder
	byToken     map[uint32][]*paperRestingOrder
	onEvent     func(OrderUpdateEvent)
}

func NewPaperBroker(onEvent func(OrderUpdateEvent)) *PaperBroker {
	p := &PaperBroker{
		orders:  make(map[string]*paperRestingOrder),
		byToken: make(map[uint32][]*paperRestingOrder),
		onEvent: onEvent,
	}
	p.seq.Store(uint64(time.Now().UnixMilli() % 1000000))
	return p
}

func (broker *PaperBroker) Mode() string {
	return "PAPER"
}

func (broker *PaperBroker) generateNextOrderID() string {
	sequenceNumber := broker.seq.Add(1)
	return fmt.Sprintf("PAPER-%d", sequenceNumber)
}

// SeedRestingOrder populates the in-memory paper order book during crash recovery rehydration
func (broker *PaperBroker) SeedRestingOrder(
	orderID, symbol string,
	token uint32,
	qty int,
	triggerPrice, limitPrice float64,
	modCount int,
	status string,
) {
	if orderID == "" || qty <= 0 {
		return
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	ord := &paperRestingOrder{
		OrderID:         orderID,
		Symbol:          symbol,
		InstrumentToken: token,
		TransactionType: "SELL",
		OrderType:       "SL",
		Status:          status,
		Quantity:        qty,
		FilledQuantity:  0,
		Price:           limitPrice,
		TriggerPrice:    triggerPrice,
		ModCount:        modCount,
		UpdatedAt:       time.Now(),
	}
	broker.orders[orderID] = ord

	// Prevent duplicate entries in byToken slice
	existing := broker.byToken[token]
	for _, o := range existing {
		if o.OrderID == orderID {
			return
		}
	}
	broker.byToken[token] = append(existing, ord)
}

// walkBook computes the realistic volume-weighted average fill price across 5 visible book levels
func walkBook(qty int, limitPrice float64, stockMetrics *market.StockMetrics, isBuy bool) float64 {
	if stockMetrics == nil || stockMetrics.LTP <= 0 {
		return limitPrice
	}
	tick := float64(stockMetrics.TickSize)
	if tick <= 0 {
		tick = 0.05
	}

	// For sell orders, check if top bid is stale or unpopulated (>0.5% away)
	if !isBuy {
		topBid := float64(stockMetrics.BidLevels[0].Price)
		if topBid <= 0 || math.Abs(stockMetrics.LTP-topBid)/stockMetrics.LTP > 0.005 {
			fillPrice := RoundToTick(math.Max(tick, stockMetrics.LTP-tick), tick)
			if limitPrice > 0 && fillPrice < limitPrice {
				fillPrice = limitPrice
			}
			return fillPrice
		}
	}

	remainingShares := qty
	var totalValue float64
	var filled int

	for i := 0; i < 5 && remainingShares > 0; i++ {
		var levelPrice float64
		var levelQuantity int
		if isBuy {
			levelPrice = float64(stockMetrics.AskLevels[i].Price)
			levelQuantity = int(stockMetrics.AskLevels[i].Qty)
			if limitPrice > 0 && levelPrice > limitPrice+1e-6 {
				break
			}
		} else {
			levelPrice = float64(stockMetrics.BidLevels[i].Price)
			levelQuantity = int(stockMetrics.BidLevels[i].Qty)
			if limitPrice > 0 && levelPrice < limitPrice-1e-6 {
				break
			}
		}
		if levelPrice <= 0 || levelQuantity <= 0 {
			continue
		}
		take := min(remainingShares, levelQuantity)
		totalValue += float64(take) * levelPrice
		filled += take
		remainingShares -= take
	}

	if filled == 0 {
		var fillPrice float64
		if isBuy {
			fillPrice = RoundToTick(stockMetrics.LTP+tick, tick)
			if stockMetrics.Ask1Price > 0 {
				fillPrice = RoundToTick(float64(stockMetrics.Ask1Price), tick)
			}
			if limitPrice > 0 && fillPrice > limitPrice {
				fillPrice = limitPrice
			}
		} else {
			fillPrice = RoundToTick(math.Max(tick, stockMetrics.LTP-tick), tick)
			if stockMetrics.Bid1Price > 0 {
				fillPrice = RoundToTick(float64(stockMetrics.Bid1Price), tick)
			}
			if limitPrice > 0 && fillPrice < limitPrice {
				fillPrice = limitPrice
			}
		}
		return fillPrice
	}

	if remainingShares > 0 {
		var sweepPrice float64
		if isBuy {
			lastAsk := float64(stockMetrics.AskLevels[4].Price)
			if lastAsk <= 0 {
				lastAsk = stockMetrics.LTP + 2.0*tick
			}
			sweepPrice = lastAsk + tick
			if limitPrice > 0 && sweepPrice > limitPrice {
				sweepPrice = limitPrice
			}
		} else {
			lastBid := float64(stockMetrics.BidLevels[4].Price)
			if lastBid <= 0 {
				lastBid = math.Max(tick, stockMetrics.LTP-2.0*tick)
			}
			sweepPrice = math.Max(tick, lastBid-tick)
			if limitPrice > 0 && sweepPrice < limitPrice {
				sweepPrice = limitPrice
			}
		}
		totalValue += float64(remainingShares) * sweepPrice
		filled += remainingShares
	}

	return RoundToTick(totalValue/float64(filled), 0.01)
}

// walkAskBook computes the realistic volume-weighted average fill price across the 5 visible Ask levels
func walkAskBook(qty int, limitPrice float64, stockMetrics *market.StockMetrics) float64 {
	return walkBook(qty, limitPrice, stockMetrics, true)
}

// walkBidBook computes the realistic volume-weighted average fill price across the 5 visible Bid levels
func walkBidBook(qty int, limitPrice float64, stockMetrics *market.StockMetrics) float64 {
	return walkBook(qty, limitPrice, stockMetrics, false)
}

func (broker *PaperBroker) emit(order *paperRestingOrder) {
	if broker.onEvent == nil {
		return
	}
	orderEvent := OrderUpdateEvent{
		OrderID:         order.OrderID,
		Symbol:          order.Symbol,
		InstrumentToken: order.InstrumentToken,
		TransactionType: order.TransactionType,
		OrderType:       order.OrderType,
		Status:          order.Status,
		Quantity:        order.Quantity,
		FilledQuantity:  order.FilledQuantity,
		PendingQuantity: max(0, order.Quantity-order.FilledQuantity),
		Price:           order.Price,
		TriggerPrice:    order.TriggerPrice,
		AveragePrice:    order.AveragePrice,
		Tag:             OrderTag,
		OrderTimestamp:  order.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
	go broker.onEvent(orderEvent)
}

func (broker *PaperBroker) PlaceLimitBuy(
	_ context.Context,
	symbol string,
	token uint32,
	qty int,
	limitPrice float64,
	stock *market.StockMetrics,
) (string, error) {
	if qty <= 0 {
		return "", ErrInvalidQuantity
	}
	orderID := broker.generateNextOrderID()

	// When stock depth is not available (e.g. mock tests without depth), fill immediately
	if stock == nil || stock.LTP <= 0 || (stock.Ask1Price <= 0 && stock.AskLevels[0].Price <= 0) {
		avgFill := walkAskBook(qty, limitPrice, stock)
		ord := &paperRestingOrder{
			OrderID:         orderID,
			Symbol:          symbol,
			InstrumentToken: token,
			TransactionType: "BUY",
			OrderType:       "LIMIT",
			Status:          OrderStatusComplete,
			Quantity:        qty,
			FilledQuantity:  qty,
			Price:           limitPrice,
			AveragePrice:    avgFill,
			UpdatedAt:       time.Now(),
		}
		broker.mu.Lock()
		broker.orders[orderID] = ord
		cp := *ord
		broker.mu.Unlock()

		broker.emit(&cp)
		return orderID, nil
	}

	// If stock metrics Ask1 is above limit price, limit order cannot immediately fill; keep OPEN
	if stock.Ask1Price > 0 && limitPrice < float64(stock.Ask1Price) {
		ord := &paperRestingOrder{
			OrderID:         orderID,
			Symbol:          symbol,
			InstrumentToken: token,
			TransactionType: "BUY",
			OrderType:       "LIMIT",
			Status:          OrderStatusOpen,
			Quantity:        qty,
			FilledQuantity:  0,
			Price:           limitPrice,
			UpdatedAt:       time.Now(),
		}
		broker.mu.Lock()
		broker.orders[orderID] = ord
		broker.byToken[token] = append(broker.byToken[token], ord)
		cp := *ord
		broker.mu.Unlock()

		broker.emit(&cp)
		return orderID, nil
	}

	// Calculate visible ask depth available at or below limitPrice
	availShares := 0
	for i := 0; i < 5; i++ {
		p := float64(stock.AskLevels[i].Price)
		q := int(stock.AskLevels[i].Qty)
		if p > 0 && q > 0 && (limitPrice <= 0 || p <= limitPrice+1e-6) {
			availShares += q
		}
	}

	if availShares <= 0 && stock.LTP > limitPrice {
		// No ask depth at or below limit price; rest OPEN
		ord := &paperRestingOrder{
			OrderID:         orderID,
			Symbol:          symbol,
			InstrumentToken: token,
			TransactionType: "BUY",
			OrderType:       "LIMIT",
			Status:          OrderStatusOpen,
			Quantity:        qty,
			FilledQuantity:  0,
			Price:           limitPrice,
			UpdatedAt:       time.Now(),
		}
		broker.mu.Lock()
		broker.orders[orderID] = ord
		broker.byToken[token] = append(broker.byToken[token], ord)
		cp := *ord
		broker.mu.Unlock()

		broker.emit(&cp)
		return orderID, nil
	}

	if availShares < qty && availShares > 0 {
		// Partial fill based on available liquidity at or below limit price
		avgFill := walkAskBook(availShares, limitPrice, stock)
		ord := &paperRestingOrder{
			OrderID:         orderID,
			Symbol:          symbol,
			InstrumentToken: token,
			TransactionType: "BUY",
			OrderType:       "LIMIT",
			Status:          OrderStatusOpen,
			Quantity:        qty,
			FilledQuantity:  availShares,
			Price:           limitPrice,
			AveragePrice:    avgFill,
			UpdatedAt:       time.Now(),
		}
		broker.mu.Lock()
		broker.orders[orderID] = ord
		broker.byToken[token] = append(broker.byToken[token], ord)
		cp := *ord
		broker.mu.Unlock()

		broker.emit(&cp)
		return orderID, nil
	}

	// Full immediate fill
	avgFill := walkAskBook(qty, limitPrice, stock)
	ord := &paperRestingOrder{
		OrderID:         orderID,
		Symbol:          symbol,
		InstrumentToken: token,
		TransactionType: "BUY",
		OrderType:       "LIMIT",
		Status:          OrderStatusComplete,
		Quantity:        qty,
		FilledQuantity:  qty,
		Price:           limitPrice,
		AveragePrice:    avgFill,
		UpdatedAt:       time.Now(),
	}

	broker.mu.Lock()
	broker.orders[orderID] = ord
	cp := *ord
	broker.mu.Unlock()

	broker.emit(&cp)
	return orderID, nil
}

func (broker *PaperBroker) PlaceProtectedMarketSell(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	currentLTP, tickSize float64,
	stock *market.StockMetrics,
) (string, error) {
	return broker.PlaceEmergencySweepSell(ctx, symbol, token, qty, currentLTP, tickSize, 0.0075, stock)
}

func (broker *PaperBroker) PlaceEmergencySweepSell(
	_ context.Context,
	symbol string,
	token uint32,
	qty int,
	currentLTP, tickSize, maxSlippagePct float64,
	stock *market.StockMetrics,
) (string, error) {
	if qty <= 0 {
		return "", ErrInvalidQuantity
	}
	if tickSize <= 0 {
		tickSize = 0.05
	}
	if maxSlippagePct <= 0 {
		maxSlippagePct = 0.0075
	}
	refPrice := currentLTP
	if stock != nil && stock.BidLevels[0].Price > 0 {
		bid1 := float64(stock.BidLevels[0].Price)
		if refPrice <= 0 || bid1 < refPrice {
			refPrice = bid1
		}
	}
	orderID := broker.generateNextOrderID()
	protectiveLimit := RoundDownToTick(math.Max(tickSize, refPrice*(1.0-maxSlippagePct)), tickSize)
	avgFill := walkBidBook(qty, protectiveLimit, stock)
	if avgFill <= 0 {
		avgFill = math.Max(tickSize, refPrice-tickSize)
	}

	ord := &paperRestingOrder{
		OrderID:         orderID,
		Symbol:          symbol,
		InstrumentToken: token,
		TransactionType: "SELL",
		OrderType:       "LIMIT",
		Status:          OrderStatusComplete,
		Quantity:        qty,
		FilledQuantity:  qty,
		Price:           protectiveLimit,
		AveragePrice:    avgFill,
		UpdatedAt:       time.Now(),
	}

	broker.mu.Lock()
	broker.orders[orderID] = ord
	cp := *ord
	broker.mu.Unlock()

	broker.emit(&cp)
	return orderID, nil
}

func (broker *PaperBroker) PlaceSLOrder(
	_ context.Context,
	symbol string,
	token uint32,
	qty int,
	triggerPrice, limitPrice, currentLTP, tickSize float64,
) (string, error) {
	if qty <= 0 {
		return "", ErrInvalidQuantity
	}
	if tickSize <= 0 {
		tickSize = 0.05
	}
	if currentLTP > 0 && triggerPrice >= currentLTP-tickSize {
		return "", fmt.Errorf("%w: trigger=%.2f, ltp=%.2f", ErrTriggerAboveLTP, triggerPrice, currentLTP)
	}
	if limitPrice <= 0 || limitPrice > triggerPrice {
		limitPrice = ComputeSellSLLimitPrice(triggerPrice, tickSize)
	}

	orderID := broker.generateNextOrderID()
	ord := &paperRestingOrder{
		OrderID:         orderID,
		Symbol:          symbol,
		InstrumentToken: token,
		TransactionType: "SELL",
		OrderType:       "SL",
		Status:          OrderStatusTrigPending,
		Quantity:        qty,
		FilledQuantity:  0,
		Price:           limitPrice,
		TriggerPrice:    triggerPrice,
		UpdatedAt:       time.Now(),
	}

	broker.mu.Lock()
	broker.orders[orderID] = ord
	broker.byToken[token] = append(broker.byToken[token], ord)
	cp := *ord
	broker.mu.Unlock()

	broker.emit(&cp)
	return orderID, nil
}

func (broker *PaperBroker) ModifySLOrder(
	_ context.Context,
	orderID string,
	newQty int,
	newTrigger, newLimit, currentLTP, tickSize float64,
) error {
	if newQty <= 0 {
		return ErrInvalidQuantity
	}
	if tickSize <= 0 {
		tickSize = 0.05
	}
	if currentLTP > 0 && newTrigger >= currentLTP-tickSize {
		return fmt.Errorf("%w: newTrigger=%.2f, ltp=%.2f", ErrTriggerAboveLTP, newTrigger, currentLTP)
	}
	if newLimit <= 0 || newLimit > newTrigger {
		newLimit = ComputeSellSLLimitPrice(newTrigger, tickSize)
	}

	broker.mu.Lock()
	ord, exists := broker.orders[orderID]
	if !exists {
		broker.mu.Unlock()
		return fmt.Errorf("paper order %s not found", orderID)
	}
	if ord.Status == OrderStatusComplete || ord.Status == OrderStatusCancelled || ord.Status == OrderStatusRejected {
		broker.mu.Unlock()
		return fmt.Errorf("cannot modify paper order %s in terminal status %s", orderID, ord.Status)
	}
	if ord.ModCount >= MaxExchangeOrderMods {
		broker.mu.Unlock()
		return ErrMaxModificationsReached
	}

	ord.ModCount++
	ord.Quantity = newQty
	ord.TriggerPrice = newTrigger
	ord.Price = newLimit
	ord.UpdatedAt = time.Now()
	cp := *ord
	broker.mu.Unlock()

	broker.emit(&cp)
	return nil
}

func (broker *PaperBroker) MutateSLToMarket(
	_ context.Context,
	orderID string,
	qty int,
	stock *market.StockMetrics,
) error {
	if qty <= 0 {
		return ErrInvalidQuantity
	}

	broker.mu.Lock()
	ord, exists := broker.orders[orderID]
	if !exists {
		broker.mu.Unlock()
		return fmt.Errorf("paper order %s not found", orderID)
	}
	if ord.Status == OrderStatusComplete {
		broker.mu.Unlock()
		return nil
	}
	if ord.ModCount >= MaxExchangeOrderMods {
		broker.mu.Unlock()
		return ErrMaxModificationsReached
	}

	fillPrice := ord.TriggerPrice
	if stock != nil && stock.LTP > 0 {
		fillPrice = walkBidBook(qty, 0, stock)
	}

	ord.ModCount++
	ord.OrderType = "MARKET"
	ord.Quantity = qty
	ord.FilledQuantity = qty
	ord.Price = 0
	ord.TriggerPrice = 0
	ord.AveragePrice = fillPrice
	ord.Status = OrderStatusComplete
	ord.UpdatedAt = time.Now()
	cp := *ord
	broker.mu.Unlock()

	broker.emit(&cp)
	return nil
}

func (broker *PaperBroker) CancelOrder(_ context.Context, orderID string) error {
	broker.mu.Lock()
	ord, exists := broker.orders[orderID]
	if !exists {
		broker.mu.Unlock()
		return fmt.Errorf("paper order %s not found", orderID)
	}
	if ord.Status == OrderStatusComplete || ord.Status == OrderStatusCancelled || ord.Status == OrderStatusRejected {
		broker.mu.Unlock()
		return fmt.Errorf("paper order %s already %s", orderID, ord.Status)
	}
	ord.Status = OrderStatusCancelled
	ord.UpdatedAt = time.Now()
	cp := *ord
	broker.mu.Unlock()

	broker.emit(&cp)
	return nil
}

// SeedRestingBuyOrder populates resting BUY limit orders in the in-memory paper order book during crash recovery
func (broker *PaperBroker) SeedRestingBuyOrder(
	orderID, symbol string,
	token uint32,
	qty, filledQty int,
	limitPrice float64,
	status string,
) {
	if orderID == "" || qty <= 0 {
		return
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	ord := &paperRestingOrder{
		OrderID:         orderID,
		Symbol:          symbol,
		InstrumentToken: token,
		TransactionType: "BUY",
		OrderType:       "LIMIT",
		Status:          status,
		Quantity:        qty,
		FilledQuantity:  filledQty,
		Price:           limitPrice,
		UpdatedAt:       time.Now(),
	}
	broker.orders[orderID] = ord

	for _, o := range broker.byToken[token] {
		if o.OrderID == orderID {
			return
		}
	}
	broker.byToken[token] = append(broker.byToken[token], ord)
}

func (broker *PaperBroker) FetchOrders(_ context.Context) ([]OrderUpdateEvent, error) {
	broker.mu.RLock()
	defer broker.mu.RUnlock()
	out := make([]OrderUpdateEvent, 0, len(broker.orders))
	for _, o := range broker.orders {
		out = append(out, OrderUpdateEvent{
			OrderID:         o.OrderID,
			Symbol:          o.Symbol,
			InstrumentToken: o.InstrumentToken,
			TransactionType: o.TransactionType,
			OrderType:       o.OrderType,
			Status:          o.Status,
			Quantity:        o.Quantity,
			FilledQuantity:  o.FilledQuantity,
			PendingQuantity: max(0, o.Quantity-o.FilledQuantity),
			Price:           o.Price,
			TriggerPrice:    o.TriggerPrice,
			AveragePrice:    o.AveragePrice,
			Tag:             OrderTag,
			OrderTimestamp:  o.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return out, nil
}

// EvaluateTickForToken checks resting Paper SL orders and BUY limit orders against live tick LTP and 5-level depth
func (broker *PaperBroker) EvaluateTickForToken(stockMetrics *market.StockMetrics) {
	if stockMetrics == nil || stockMetrics.LTP <= 0 {
		return
	}
	broker.mu.Lock()
	list := broker.byToken[stockMetrics.InstrumentToken]
	if len(list) == 0 {
		broker.mu.Unlock()
		return
	}
	var triggered []paperRestingOrder
	for _, ord := range list {
		if ord.TransactionType == "BUY" {
			if ord.Status == OrderStatusOpen {
				ask1 := float64(stockMetrics.Ask1Price)
				if stockMetrics.LTP <= ord.Price || (ask1 > 0 && ask1 <= ord.Price) {
					remToFill := ord.Quantity - ord.FilledQuantity
					if remToFill > 0 {
						availAtOrBelow := 0
						for i := 0; i < 5; i++ {
							p := float64(stockMetrics.AskLevels[i].Price)
							q := int(stockMetrics.AskLevels[i].Qty)
							if p > 0 && q > 0 && p <= ord.Price+1e-6 {
								availAtOrBelow += q
							}
						}
						if availAtOrBelow == 0 && stockMetrics.LTP <= ord.Price {
							availAtOrBelow = remToFill
						}
						fillQty := min(remToFill, availAtOrBelow)
						if fillQty > 0 {
							chunkAvg := walkAskBook(fillQty, ord.Price, stockMetrics)
							prevVal := float64(ord.FilledQuantity) * ord.AveragePrice
							newFilled := ord.FilledQuantity + fillQty
							ord.AveragePrice = (prevVal + float64(fillQty)*chunkAvg) / float64(newFilled)
							ord.FilledQuantity = newFilled
							if ord.FilledQuantity >= ord.Quantity {
								ord.Status = OrderStatusComplete
							}
							ord.UpdatedAt = time.Now()
							triggered = append(triggered, *ord)
						}
					}
				}
			}
		} else { // SELL (SL or LIMIT)
			if ord.Status == OrderStatusTrigPending && stockMetrics.LTP <= ord.TriggerPrice {
				// Check if current LTP gapped below the protective limit price
				if ord.Price > 0 && stockMetrics.LTP < ord.Price {
					ord.Status = OrderStatusOpen
					ord.UpdatedAt = time.Now()
					triggered = append(triggered, *ord)
					continue
				}
				fillPrice := walkBidBook(ord.Quantity, ord.Price, stockMetrics)
				ord.Status = OrderStatusComplete
				ord.FilledQuantity = ord.Quantity
				ord.AveragePrice = fillPrice
				ord.UpdatedAt = time.Now()
				triggered = append(triggered, *ord)
			} else if ord.Status == OrderStatusOpen && (ord.Price == 0 || stockMetrics.LTP >= ord.Price) {
				fillPrice := walkBidBook(ord.Quantity, ord.Price, stockMetrics)
				ord.Status = OrderStatusComplete
				ord.FilledQuantity = ord.Quantity
				ord.AveragePrice = fillPrice
				ord.UpdatedAt = time.Now()
				triggered = append(triggered, *ord)
			}
		}
	}
	broker.mu.Unlock()

	for i := range triggered {
		broker.emit(&triggered[i])
	}
}
