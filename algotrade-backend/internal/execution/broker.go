package execution

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/engine"
	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

const (
	OrderTag               = "AT26LONG"
	MaxExchangeOrderMods   = 25
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
	PlaceLimitBuy(ctx context.Context, symbol string, token uint32, qty int, limitPrice float64, stock *engine.StockMetrics) (string, error)
	PlaceProtectedMarketSell(ctx context.Context, symbol string, token uint32, qty int, currentLTP, tickSize float64, stock *engine.StockMetrics) (string, error)
	PlaceEmergencySweepSell(ctx context.Context, symbol string, token uint32, qty int, currentLTP, tickSize, maxSlippagePct float64, stock *engine.StockMetrics) (string, error)
	PlaceSLOrder(ctx context.Context, symbol string, token uint32, qty int, triggerPrice, limitPrice, currentLTP, tickSize float64) (string, error)
	ModifySLOrder(ctx context.Context, orderID string, newQty int, newTrigger, newLimit, currentLTP, tickSize float64) error
	MutateSLToMarket(ctx context.Context, orderID string, qty int, stock *engine.StockMetrics) error
	CancelOrder(ctx context.Context, orderID string) error
	FetchOrders(ctx context.Context) ([]OrderUpdateEvent, error)
}

// computeSellSLLimitPrice calculates the protective limit price below triggerPrice for NSE SL (Stop-Loss Limit) orders
func computeSellSLLimitPrice(triggerPrice, tickSize float64) float64 {
	if tickSize <= 0 {
		tickSize = 0.05
	}
	slippageBand := math.Max(4.0*tickSize, 0.0040*triggerPrice)
	return RoundDownToTick(math.Max(tickSize, triggerPrice-slippageBand), tickSize)
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

func (b *ZerodhaLiveBroker) Mode() string {
	return "LIVE"
}

// waitRateLimit enforces an 8 req/sec token bucket (staying safely inside Zerodha's 10 req/sec order mutation limit)
func (b *ZerodhaLiveBroker) waitRateLimit(ctx context.Context) error {
	for {
		b.rateMu.Lock()
		now := time.Now()
		elapsed := now.Sub(b.lastRefill).Seconds()
		b.tokens = math.Min(6.0, b.tokens+elapsed*8.0)
		b.lastRefill = now
		if b.tokens >= 1.0 {
			b.tokens -= 1.0
			b.rateMu.Unlock()
			return nil
		}
		waitDuration := time.Duration(((1.0 - b.tokens) / 8.0) * float64(time.Second))
		b.rateMu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
		}
	}
}

func (b *ZerodhaLiveBroker) PlaceLimitBuy(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	limitPrice float64,
	_ *engine.StockMetrics,
) (string, error) {
	if qty <= 0 {
		return "", ErrInvalidQuantity
	}
	if err := b.waitRateLimit(ctx); err != nil {
		return "", err
	}
	client := b.kiteService.GetClient()
	session := b.kiteService.GetSession()
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

func (b *ZerodhaLiveBroker) PlaceProtectedMarketSell(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	currentLTP, tickSize float64,
	stock *engine.StockMetrics,
) (string, error) {
	return b.PlaceEmergencySweepSell(ctx, symbol, token, qty, currentLTP, tickSize, 0.0075, stock)
}

func (b *ZerodhaLiveBroker) PlaceEmergencySweepSell(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	currentLTP, tickSize, maxSlippagePct float64,
	stock *engine.StockMetrics,
) (string, error) {
	if qty <= 0 {
		return "", ErrInvalidQuantity
	}
	if err := b.waitRateLimit(ctx); err != nil {
		return "", err
	}
	client := b.kiteService.GetClient()
	session := b.kiteService.GetSession()
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

func (b *ZerodhaLiveBroker) PlaceSLOrder(
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
		limitPrice = computeSellSLLimitPrice(triggerPrice, tickSize)
	}
	if err := b.waitRateLimit(ctx); err != nil {
		return "", err
	}
	client := b.kiteService.GetClient()
	session := b.kiteService.GetSession()
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

func (b *ZerodhaLiveBroker) ModifySLOrder(
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
		newLimit = computeSellSLLimitPrice(newTrigger, tickSize)
	}
	if err := b.waitRateLimit(ctx); err != nil {
		return err
	}
	_, err := b.kiteService.RawModifyOrderPUT(ctx, DefaultVariety, orderID, "SL", newQty, newLimit, newTrigger)
	return err
}

func (b *ZerodhaLiveBroker) MutateSLToMarket(
	ctx context.Context,
	orderID string,
	qty int,
	_ *engine.StockMetrics,
) error {
	if qty <= 0 {
		return ErrInvalidQuantity
	}
	if err := b.waitRateLimit(ctx); err != nil {
		return err
	}
	_, err := b.kiteService.RawModifyOrderPUT(ctx, DefaultVariety, orderID, "MARKET", qty, 0.0, 0.0)
	return err
}

func (b *ZerodhaLiveBroker) CancelOrder(ctx context.Context, orderID string) error {
	if orderID == "" {
		return errors.New("order_id cannot be empty")
	}
	if err := b.waitRateLimit(ctx); err != nil {
		return err
	}
	client := b.kiteService.GetClient()
	session := b.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return kite.ErrNoSession
	}
	_, err := client.CancelOrder(DefaultVariety, orderID, nil)
	return err
}

func (b *ZerodhaLiveBroker) FetchOrders(ctx context.Context) ([]OrderUpdateEvent, error) {
	client := b.kiteService.GetClient()
	session := b.kiteService.GetSession()
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

func (p *PaperBroker) Mode() string {
	return "PAPER"
}

func (p *PaperBroker) nextOrderID() string {
	n := p.seq.Add(1)
	return fmt.Sprintf("PAPER-%d", n)
}

// walkAskBook computes the realistic volume-weighted average fill price across the 5 visible Ask levels
func walkAskBook(qty int, limitPrice float64, s *engine.StockMetrics) float64 {
	if s == nil || s.LTP <= 0 {
		return limitPrice
	}
	tick := float64(s.TickSize)
	if tick <= 0 {
		tick = 0.05
	}
	rem := qty
	var cost float64
	var filled int

	for i := 0; i < 5 && rem > 0; i++ {
		lvlPrice := float64(s.AskLevels[i].Price)
		lvlQty := int(s.AskLevels[i].Qty)
		if lvlPrice <= 0 || lvlQty <= 0 {
			continue
		}
		if limitPrice > 0 && lvlPrice > limitPrice+1e-6 {
			break
		}
		take := min(rem, lvlQty)
		cost += float64(take) * lvlPrice
		filled += take
		rem -= take
	}

	if filled == 0 {
		px := RoundToTick(s.LTP+tick, tick)
		if s.Ask1Price > 0 {
			px = RoundToTick(float64(s.Ask1Price), tick)
		}
		if limitPrice > 0 && px > limitPrice {
			px = limitPrice
		}
		return px
	}
	if rem > 0 {
		lastAsk := float64(s.AskLevels[4].Price)
		if lastAsk <= 0 {
			lastAsk = s.LTP + 2.0*tick
		}
		sweepPrice := lastAsk + tick
		if limitPrice > 0 && sweepPrice > limitPrice {
			sweepPrice = limitPrice
		}
		cost += float64(rem) * sweepPrice
		filled += rem
	}
	return RoundToTick(cost/float64(filled), 0.01)
}

// walkBidBook computes the realistic volume-weighted average fill price across the 5 visible Bid levels
func walkBidBook(qty int, limitPrice float64, s *engine.StockMetrics) float64 {
	if s == nil || s.LTP <= 0 {
		return limitPrice
	}
	tick := float64(s.TickSize)
	if tick <= 0 {
		tick = 0.05
	}
	// If order book levels are unpopulated or stale relative to LTP (>0.5% away), fill at LTP - 1 tick (clamped to limitPrice)
	topBid := float64(s.BidLevels[0].Price)
	if topBid <= 0 || math.Abs(s.LTP-topBid)/s.LTP > 0.005 {
		px := RoundToTick(math.Max(tick, s.LTP-tick), tick)
		if limitPrice > 0 && px < limitPrice {
			px = limitPrice
		}
		return px
	}
	rem := qty
	var proceeds float64
	var filled int

	for i := 0; i < 5 && rem > 0; i++ {
		lvlPrice := float64(s.BidLevels[i].Price)
		lvlQty := int(s.BidLevels[i].Qty)
		if lvlPrice <= 0 || lvlQty <= 0 {
			continue
		}
		if limitPrice > 0 && lvlPrice < limitPrice-1e-6 {
			break
		}
		take := min(rem, lvlQty)
		proceeds += float64(take) * lvlPrice
		filled += take
		rem -= take
	}

	if filled == 0 {
		px := RoundToTick(math.Max(tick, s.LTP-tick), tick)
		if s.Bid1Price > 0 {
			px = RoundToTick(float64(s.Bid1Price), tick)
		}
		if limitPrice > 0 && px < limitPrice {
			px = limitPrice
		}
		return px
	}
	if rem > 0 {
		lastBid := float64(s.BidLevels[4].Price)
		if lastBid <= 0 {
			lastBid = math.Max(tick, s.LTP-2.0*tick)
		}
		sweepPrice := math.Max(tick, lastBid-tick)
		if limitPrice > 0 && sweepPrice < limitPrice {
			sweepPrice = limitPrice
		}
		proceeds += float64(rem) * sweepPrice
		filled += rem
	}
	return RoundToTick(proceeds/float64(filled), 0.01)
}

func (p *PaperBroker) emit(o *paperRestingOrder) {
	if p.onEvent == nil {
		return
	}
	ev := OrderUpdateEvent{
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
	}
	go p.onEvent(ev)
}

func (p *PaperBroker) PlaceLimitBuy(
	_ context.Context,
	symbol string,
	token uint32,
	qty int,
	limitPrice float64,
	stock *engine.StockMetrics,
) (string, error) {
	if qty <= 0 {
		return "", ErrInvalidQuantity
	}
	orderID := p.nextOrderID()
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

	p.mu.Lock()
	p.orders[orderID] = ord
	p.mu.Unlock()

	p.emit(ord)
	return orderID, nil
}

func (p *PaperBroker) PlaceProtectedMarketSell(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	currentLTP, tickSize float64,
	stock *engine.StockMetrics,
) (string, error) {
	return p.PlaceEmergencySweepSell(ctx, symbol, token, qty, currentLTP, tickSize, 0.0075, stock)
}

func (p *PaperBroker) PlaceEmergencySweepSell(
	_ context.Context,
	symbol string,
	token uint32,
	qty int,
	currentLTP, tickSize, maxSlippagePct float64,
	stock *engine.StockMetrics,
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
	orderID := p.nextOrderID()
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

	p.mu.Lock()
	p.orders[orderID] = ord
	p.mu.Unlock()

	p.emit(ord)
	return orderID, nil
}

func (p *PaperBroker) PlaceSLOrder(
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
		limitPrice = computeSellSLLimitPrice(triggerPrice, tickSize)
	}

	orderID := p.nextOrderID()
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

	p.mu.Lock()
	p.orders[orderID] = ord
	p.byToken[token] = append(p.byToken[token], ord)
	p.mu.Unlock()

	p.emit(ord)
	return orderID, nil
}

func (p *PaperBroker) ModifySLOrder(
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
		newLimit = computeSellSLLimitPrice(newTrigger, tickSize)
	}

	p.mu.Lock()
	ord, exists := p.orders[orderID]
	if !exists {
		p.mu.Unlock()
		return fmt.Errorf("paper order %s not found", orderID)
	}
	if ord.Status == OrderStatusComplete || ord.Status == OrderStatusCancelled || ord.Status == OrderStatusRejected {
		p.mu.Unlock()
		return fmt.Errorf("cannot modify paper order %s in terminal status %s", orderID, ord.Status)
	}
	if ord.ModCount >= MaxExchangeOrderMods {
		p.mu.Unlock()
		return ErrMaxModificationsReached
	}

	ord.ModCount++
	ord.Quantity = newQty
	ord.TriggerPrice = newTrigger
	ord.Price = newLimit
	ord.UpdatedAt = time.Now()
	cp := *ord
	p.mu.Unlock()

	p.emit(&cp)
	return nil
}

func (p *PaperBroker) MutateSLToMarket(
	_ context.Context,
	orderID string,
	qty int,
	stock *engine.StockMetrics,
) error {
	if qty <= 0 {
		return ErrInvalidQuantity
	}

	p.mu.Lock()
	ord, exists := p.orders[orderID]
	if !exists {
		p.mu.Unlock()
		return fmt.Errorf("paper order %s not found", orderID)
	}
	if ord.Status == OrderStatusComplete {
		p.mu.Unlock()
		return nil
	}
	if ord.ModCount >= MaxExchangeOrderMods {
		p.mu.Unlock()
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
	p.mu.Unlock()

	p.emit(&cp)
	return nil
}

func (p *PaperBroker) CancelOrder(_ context.Context, orderID string) error {
	p.mu.Lock()
	ord, exists := p.orders[orderID]
	if !exists {
		p.mu.Unlock()
		return fmt.Errorf("paper order %s not found", orderID)
	}
	if ord.Status == OrderStatusComplete {
		p.mu.Unlock()
		return fmt.Errorf("paper order %s already COMPLETE", orderID)
	}
	ord.Status = OrderStatusCancelled
	ord.UpdatedAt = time.Now()
	cp := *ord
	p.mu.Unlock()

	p.emit(&cp)
	return nil
}

func (p *PaperBroker) FetchOrders(_ context.Context) ([]OrderUpdateEvent, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]OrderUpdateEvent, 0, len(p.orders))
	for _, o := range p.orders {
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

// EvaluateTickForToken checks resting Paper SL orders against live tick LTP and 5-level Bid depth
func (p *PaperBroker) EvaluateTickForToken(s *engine.StockMetrics) {
	if s == nil || s.LTP <= 0 {
		return
	}
	p.mu.Lock()
	list := p.byToken[s.InstrumentToken]
	if len(list) == 0 {
		p.mu.Unlock()
		return
	}
	var triggered []paperRestingOrder
	for _, ord := range list {
		if ord.Status == OrderStatusTrigPending && s.LTP <= ord.TriggerPrice {
			// Check if current LTP gapped below the protective limit price
			if ord.Price > 0 && s.LTP < ord.Price {
				ord.Status = OrderStatusOpen
				ord.UpdatedAt = time.Now()
				triggered = append(triggered, *ord)
				continue
			}
			fillPrice := walkBidBook(ord.Quantity, ord.Price, s)
			ord.Status = OrderStatusComplete
			ord.FilledQuantity = ord.Quantity
			ord.AveragePrice = fillPrice
			ord.UpdatedAt = time.Now()
			triggered = append(triggered, *ord)
		} else if ord.Status == OrderStatusOpen && (ord.Price == 0 || s.LTP >= ord.Price) {
			fillPrice := walkBidBook(ord.Quantity, ord.Price, s)
			ord.Status = OrderStatusComplete
			ord.FilledQuantity = ord.Quantity
			ord.AveragePrice = fillPrice
			ord.UpdatedAt = time.Now()
			triggered = append(triggered, *ord)
		}
	}
	p.mu.Unlock()

	for i := range triggered {
		p.emit(&triggered[i])
	}
}
