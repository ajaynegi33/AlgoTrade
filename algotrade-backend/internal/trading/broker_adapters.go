package trading

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/diagnostics"
	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	"github.com/ajaynegi33/AlgoTrade/internal/market"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

const (
	OrderTag               = "AT26LONG"
	DefaultVariety         = "regular"
	ExchangeNSE            = "NSE"
	ProductMIS             = "MIS"
	ProductCNC             = "CNC"
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
	PlaceSLOrder(ctx context.Context, symbol string, token uint32, qty int, triggerPrice, limitPrice, currentLTP, tickSize float64, stockMetrics *market.StockMetrics) (string, error)
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
}

func NewZerodhaLiveBroker(kiteService *kite.Service) *ZerodhaLiveBroker {
	return &ZerodhaLiveBroker{
		kiteService: kiteService,
	}
}

func (broker *ZerodhaLiveBroker) Mode() string {
	return "LIVE"
}

func determineOrderProduct(stockMetrics *market.StockMetrics) string {
	if stockMetrics != nil && stockMetrics.MISLeverage <= 1.0 {
		return ProductCNC
	}
	return ProductMIS
}

func (broker *ZerodhaLiveBroker) PlaceLimitBuy(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	limitPrice float64,
	stockMetrics *market.StockMetrics,
) (string, error) {
	if qty <= 0 {
		return "", ErrInvalidQuantity
	}
	client := broker.kiteService.GetClient()
	session := broker.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return "", kite.ErrNoSession
	}

	orderProduct := determineOrderProduct(stockMetrics)
	apiCallStartTime := time.Now()
	var orderResponse kiteconnect.OrderResponse
	if dispatcher := broker.kiteService.GetRateLimiter(); dispatcher != nil {
		placeOrderResult, dispatchErr := dispatcher.ExecuteStandardOrder(ctx, func() (interface{}, error) {
			return client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
				Exchange:        ExchangeNSE,
				Tradingsymbol:   symbol,
				Validity:        ValidityDay,
				Product:         orderProduct,
				OrderType:       "LIMIT",
				TransactionType: "BUY",
				Quantity:        qty,
				Price:           limitPrice,
				Tag:             OrderTag,
			})
		})
		diagnostics.RecordLatency("zerodha_place_limit_buy", time.Since(apiCallStartTime), 1500*time.Millisecond)
		if dispatchErr != nil {
			return "", fmt.Errorf("zerodha PlaceLimitBuy failed for %s: %w", symbol, dispatchErr)
		}
		orderResponse = placeOrderResult.(kiteconnect.OrderResponse)
	} else {
		var placeError error
		orderResponse, placeError = client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
			Exchange:        ExchangeNSE,
			Tradingsymbol:   symbol,
			Validity:        ValidityDay,
			Product:         orderProduct,
			OrderType:       "LIMIT",
			TransactionType: "BUY",
			Quantity:        qty,
			Price:           limitPrice,
			Tag:             OrderTag,
		})
		diagnostics.RecordLatency("zerodha_place_limit_buy", time.Since(apiCallStartTime), 1500*time.Millisecond)
		if placeError != nil {
			return "", fmt.Errorf("zerodha PlaceLimitBuy failed for %s: %w", symbol, placeError)
		}
	}
	return orderResponse.OrderID, nil
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

	orderProduct := determineOrderProduct(stock)

	// Stage C Final Escalation (>= 3.5%): attempt direct MARKET sell first, fallback to deep LPP limit
	if maxSlippagePct >= 0.035 {
		if dispatcher := broker.kiteService.GetRateLimiter(); dispatcher != nil {
			mktResult, mktErr := dispatcher.ExecuteEmergencyOrder(ctx, func() (interface{}, error) {
				return client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
					Exchange:        ExchangeNSE,
					Tradingsymbol:   symbol,
					Validity:        ValidityDay,
					Product:         orderProduct,
					OrderType:       "MARKET",
					TransactionType: "SELL",
					Quantity:        qty,
					Tag:             OrderTag,
				})
			})
			if mktErr == nil {
				mktResp := mktResult.(kiteconnect.OrderResponse)
				if mktResp.OrderID != "" {
					return mktResp.OrderID, nil
				}
			}
		} else {
			mktResp, mktErr := client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
				Exchange:        ExchangeNSE,
				Tradingsymbol:   symbol,
				Validity:        ValidityDay,
				Product:         orderProduct,
				OrderType:       "MARKET",
				TransactionType: "SELL",
				Quantity:        qty,
				Tag:             OrderTag,
			})
			if mktErr == nil && mktResp.OrderID != "" {
				return mktResp.OrderID, nil
			}
		}
	}

	protectiveLimit := RoundDownToTick(math.Max(tickSize, refPrice*(1.0-maxSlippagePct)), tickSize)
	apiCallStartTime := time.Now()
	var orderResponse kiteconnect.OrderResponse
	if dispatcher := broker.kiteService.GetRateLimiter(); dispatcher != nil {
		sweepOrderResult, dispatchErr := dispatcher.ExecuteEmergencyOrder(ctx, func() (interface{}, error) {
			return client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
				Exchange:        ExchangeNSE,
				Tradingsymbol:   symbol,
				Validity:        ValidityDay,
				Product:         orderProduct,
				OrderType:       "LIMIT",
				TransactionType: "SELL",
				Quantity:        qty,
				Price:           protectiveLimit,
				Tag:             OrderTag,
			})
		})
		diagnostics.RecordLatency("zerodha_emergency_sweep_sell", time.Since(apiCallStartTime), 1500*time.Millisecond)
		if dispatchErr != nil {
			return "", fmt.Errorf("zerodha PlaceEmergencySweepSell (slip=%.2f%%) failed for %s: %w", maxSlippagePct*100, symbol, dispatchErr)
		}
		orderResponse = sweepOrderResult.(kiteconnect.OrderResponse)
	} else {
		var placeError error
		orderResponse, placeError = client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
			Exchange:        ExchangeNSE,
			Tradingsymbol:   symbol,
			Validity:        ValidityDay,
			Product:         orderProduct,
			OrderType:       "LIMIT",
			TransactionType: "SELL",
			Quantity:        qty,
			Price:           protectiveLimit,
			Tag:             OrderTag,
		})
		diagnostics.RecordLatency("zerodha_emergency_sweep_sell", time.Since(apiCallStartTime), 1500*time.Millisecond)
		if placeError != nil {
			return "", fmt.Errorf("zerodha PlaceEmergencySweepSell (slip=%.2f%%) failed for %s: %w", maxSlippagePct*100, symbol, placeError)
		}
	}
	return orderResponse.OrderID, nil
}

func (broker *ZerodhaLiveBroker) PlaceSLOrder(
	ctx context.Context,
	symbol string,
	token uint32,
	qty int,
	triggerPrice, limitPrice, currentLTP, tickSize float64,
	stockMetrics *market.StockMetrics,
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
		limitPrice = CalculateProtectiveLimitPriceBelowStop(triggerPrice, tickSize)
	}
	client := broker.kiteService.GetClient()
	session := broker.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return "", kite.ErrNoSession
	}

	orderProduct := determineOrderProduct(stockMetrics)
	apiCallStartTime := time.Now()
	var orderResponse kiteconnect.OrderResponse
	if dispatcher := broker.kiteService.GetRateLimiter(); dispatcher != nil {
		slOrderResult, dispatchErr := dispatcher.ExecuteEmergencyOrder(ctx, func() (interface{}, error) {
			return client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
				Exchange:        ExchangeNSE,
				Tradingsymbol:   symbol,
				Validity:        ValidityDay,
				Product:         orderProduct,
				OrderType:       "SL",
				TransactionType: "SELL",
				Quantity:        qty,
				TriggerPrice:    triggerPrice,
				Price:           limitPrice,
				Tag:             OrderTag,
			})
		})
		diagnostics.RecordLatency("zerodha_place_sl_order", time.Since(apiCallStartTime), 1500*time.Millisecond)
		if dispatchErr != nil {
			return "", fmt.Errorf("zerodha PlaceSLOrder failed for %s: %w", symbol, dispatchErr)
		}
		orderResponse = slOrderResult.(kiteconnect.OrderResponse)
	} else {
		var placeError error
		orderResponse, placeError = client.PlaceOrder(DefaultVariety, kiteconnect.OrderParams{
			Exchange:        ExchangeNSE,
			Tradingsymbol:   symbol,
			Validity:        ValidityDay,
			Product:         orderProduct,
			OrderType:       "SL",
			TransactionType: "SELL",
			Quantity:        qty,
			TriggerPrice:    triggerPrice,
			Price:           limitPrice,
			Tag:             OrderTag,
		})
		diagnostics.RecordLatency("zerodha_place_sl_order", time.Since(apiCallStartTime), 1500*time.Millisecond)
		if placeError != nil {
			return "", fmt.Errorf("zerodha PlaceSLOrder failed for %s: %w", symbol, placeError)
		}
	}
	return orderResponse.OrderID, nil
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
		newLimit = CalculateProtectiveLimitPriceBelowStop(newTrigger, tickSize)
	}
	apiCallStartTime := time.Now()
	_, err := broker.kiteService.RawModifyOrderPUT(ctx, DefaultVariety, orderID, "SL", newQty, newLimit, newTrigger)
	diagnostics.RecordLatency("zerodha_modify_sl_order", time.Since(apiCallStartTime), 1500*time.Millisecond)
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
	apiCallStartTime := time.Now()
	_, err := broker.kiteService.RawModifyOrderPUT(ctx, DefaultVariety, orderID, "MARKET", qty, 0.0, 0.0)
	diagnostics.RecordLatency("zerodha_mutate_sl_to_market", time.Since(apiCallStartTime), 1500*time.Millisecond)
	return err
}

func (broker *ZerodhaLiveBroker) CancelOrder(ctx context.Context, orderID string) error {
	if orderID == "" {
		return errors.New("order_id cannot be empty")
	}
	client := broker.kiteService.GetClient()
	session := broker.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return kite.ErrNoSession
	}
	apiCallStartTime := time.Now()
	if dispatcher := broker.kiteService.GetRateLimiter(); dispatcher != nil {
		_, cancelErr := dispatcher.ExecuteEmergencyOrder(ctx, func() (interface{}, error) {
			return client.CancelOrder(DefaultVariety, orderID, nil)
		})
		diagnostics.RecordLatency("zerodha_cancel_order", time.Since(apiCallStartTime), 1500*time.Millisecond)
		return cancelErr
	}
	_, cancelErr := client.CancelOrder(DefaultVariety, orderID, nil)
	diagnostics.RecordLatency("zerodha_cancel_order", time.Since(apiCallStartTime), 1500*time.Millisecond)
	return cancelErr
}

func (broker *ZerodhaLiveBroker) FetchOrders(ctx context.Context) ([]OrderUpdateEvent, error) {
	client := broker.kiteService.GetClient()
	session := broker.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return nil, kite.ErrNoSession
	}

	var orders []kiteconnect.Order
	if dispatcher := broker.kiteService.GetRateLimiter(); dispatcher != nil {
		ordersResult, dispatchErr := dispatcher.ExecuteAccountSync(ctx, func() (interface{}, error) {
			return client.GetOrders()
		})
		if dispatchErr != nil {
			return nil, fmt.Errorf("failed to fetch orders from kite: %w", dispatchErr)
		}
		orders = ordersResult.([]kiteconnect.Order)
	} else {
		var fetchErr error
		orders, fetchErr = client.GetOrders()
		if fetchErr != nil {
			return nil, fmt.Errorf("failed to fetch orders from kite: %w", fetchErr)
		}
	}
	orderEvents := make([]OrderUpdateEvent, 0, len(orders))
	for _, order := range orders {
		orderEvents = append(orderEvents, OrderUpdateEvent{
			OrderID:         order.OrderID,
			Symbol:          order.TradingSymbol,
			InstrumentToken: order.InstrumentToken,
			TransactionType: order.TransactionType,
			OrderType:       order.OrderType,
			Status:          order.Status,
			StatusMessage:   order.StatusMessage,
			Quantity:        int(order.Quantity),
			FilledQuantity:  int(order.FilledQuantity),
			PendingQuantity: int(order.PendingQuantity),
			Price:           order.Price,
			TriggerPrice:    order.TriggerPrice,
			AveragePrice:    order.AveragePrice,
			Tag:             order.Tag,
		})
	}
	return orderEvents, nil
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
	paperBroker := &PaperBroker{
		orders:  make(map[string]*paperRestingOrder),
		byToken: make(map[uint32][]*paperRestingOrder),
		onEvent: onEvent,
	}
	paperBroker.seq.Store(uint64(time.Now().UnixMilli() % 1000000))
	return paperBroker
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
	restingOrder := &paperRestingOrder{
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
	broker.orders[orderID] = restingOrder

	// Prevent duplicate entries in byToken slice
	existingOrders := broker.byToken[token]
	for _, existingOrder := range existingOrders {
		if existingOrder.OrderID == orderID {
			return
		}
	}
	broker.byToken[token] = append(existingOrders, restingOrder)
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

	for depthLevelIndex := 0; depthLevelIndex < 5 && remainingShares > 0; depthLevelIndex++ {
		var levelPrice float64
		var levelQuantity int
		if isBuy {
			levelPrice = float64(stockMetrics.AskLevels[depthLevelIndex].Price)
			levelQuantity = int(stockMetrics.AskLevels[depthLevelIndex].Qty)
			if limitPrice > 0 && levelPrice > limitPrice+1e-6 {
				break
			}
		} else {
			levelPrice = float64(stockMetrics.BidLevels[depthLevelIndex].Price)
			levelQuantity = int(stockMetrics.BidLevels[depthLevelIndex].Qty)
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
	broker.onEvent(orderEvent)
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
		restingOrder := &paperRestingOrder{
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
		broker.orders[orderID] = restingOrder
		orderCopy := *restingOrder
		broker.mu.Unlock()

		broker.emit(&orderCopy)
		return orderID, nil
	}

	// If stock metrics Ask1 is above limit price, limit order cannot immediately fill; keep OPEN
	if stock.Ask1Price > 0 && limitPrice < float64(stock.Ask1Price) {
		restingOrder := &paperRestingOrder{
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
		broker.orders[orderID] = restingOrder
		broker.byToken[token] = append(broker.byToken[token], restingOrder)
		orderCopy := *restingOrder
		broker.mu.Unlock()

		broker.emit(&orderCopy)
		return orderID, nil
	}

	// Calculate visible ask depth available at or below limitPrice
	availableShares := 0
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		levelPrice := float64(stock.AskLevels[depthLevelIndex].Price)
		levelQuantity := int(stock.AskLevels[depthLevelIndex].Qty)
		if levelPrice > 0 && levelQuantity > 0 && (limitPrice <= 0 || levelPrice <= limitPrice+1e-6) {
			availableShares += levelQuantity
		}
	}

	if availableShares <= 0 && stock.LTP > limitPrice {
		// No ask depth at or below limit price; rest OPEN
		restingOrder := &paperRestingOrder{
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
		broker.orders[orderID] = restingOrder
		broker.byToken[token] = append(broker.byToken[token], restingOrder)
		orderCopy := *restingOrder
		broker.mu.Unlock()

		broker.emit(&orderCopy)
		return orderID, nil
	}

	if availableShares < qty && availableShares > 0 {
		// Partial fill based on available liquidity at or below limit price
		averageFillPrice := walkAskBook(availableShares, limitPrice, stock)
		restingOrder := &paperRestingOrder{
			OrderID:         orderID,
			Symbol:          symbol,
			InstrumentToken: token,
			TransactionType: "BUY",
			OrderType:       "LIMIT",
			Status:          OrderStatusOpen,
			Quantity:        qty,
			FilledQuantity:  availableShares,
			Price:           limitPrice,
			AveragePrice:    averageFillPrice,
			UpdatedAt:       time.Now(),
		}
		broker.mu.Lock()
		broker.orders[orderID] = restingOrder
		broker.byToken[token] = append(broker.byToken[token], restingOrder)
		orderCopy := *restingOrder
		broker.mu.Unlock()

		broker.emit(&orderCopy)
		return orderID, nil
	}

	// Full immediate fill
	averageFillPrice := walkAskBook(qty, limitPrice, stock)
	restingOrder := &paperRestingOrder{
		OrderID:         orderID,
		Symbol:          symbol,
		InstrumentToken: token,
		TransactionType: "BUY",
		OrderType:       "LIMIT",
		Status:          OrderStatusComplete,
		Quantity:        qty,
		FilledQuantity:  qty,
		Price:           limitPrice,
		AveragePrice:    averageFillPrice,
		UpdatedAt:       time.Now(),
	}

	broker.mu.Lock()
	broker.orders[orderID] = restingOrder
	hasExistingByToken := false
	for _, existingOrder := range broker.byToken[token] {
		if existingOrder.OrderID == orderID {
			hasExistingByToken = true
			break
		}
	}
	if !hasExistingByToken {
		broker.byToken[token] = append(broker.byToken[token], restingOrder)
	}
	orderCopy := *restingOrder
	broker.mu.Unlock()

	broker.emit(&orderCopy)
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
	averageFillPrice := walkBidBook(qty, protectiveLimit, stock)
	if averageFillPrice <= 0 {
		averageFillPrice = math.Max(tickSize, refPrice-tickSize)
	}

	restingOrder := &paperRestingOrder{
		OrderID:         orderID,
		Symbol:          symbol,
		InstrumentToken: token,
		TransactionType: "SELL",
		OrderType:       "LIMIT",
		Status:          OrderStatusComplete,
		Quantity:        qty,
		FilledQuantity:  qty,
		Price:           protectiveLimit,
		AveragePrice:    averageFillPrice,
		UpdatedAt:       time.Now(),
	}

	broker.mu.Lock()
	broker.orders[orderID] = restingOrder
	hasExistingByToken := false
	for _, existingOrder := range broker.byToken[token] {
		if existingOrder.OrderID == orderID {
			hasExistingByToken = true
			break
		}
	}
	if !hasExistingByToken {
		broker.byToken[token] = append(broker.byToken[token], restingOrder)
	}
	orderCopy := *restingOrder
	broker.mu.Unlock()

	broker.emit(&orderCopy)
	return orderID, nil
}

func (broker *PaperBroker) PlaceSLOrder(
	_ context.Context,
	symbol string,
	token uint32,
	qty int,
	triggerPrice, limitPrice, currentLTP, tickSize float64,
	_ *market.StockMetrics,
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
		limitPrice = CalculateProtectiveLimitPriceBelowStop(triggerPrice, tickSize)
	}

	orderID := broker.generateNextOrderID()
	restingOrder := &paperRestingOrder{
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
	broker.orders[orderID] = restingOrder
	broker.byToken[token] = append(broker.byToken[token], restingOrder)
	orderCopy := *restingOrder
	broker.mu.Unlock()

	broker.emit(&orderCopy)
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
		newLimit = CalculateProtectiveLimitPriceBelowStop(newTrigger, tickSize)
	}

	broker.mu.Lock()
	restingOrder, exists := broker.orders[orderID]
	if !exists {
		broker.mu.Unlock()
		return fmt.Errorf("paper order %s not found", orderID)
	}
	if restingOrder.Status == OrderStatusComplete || restingOrder.Status == OrderStatusCancelled || restingOrder.Status == OrderStatusRejected {
		broker.mu.Unlock()
		return fmt.Errorf("cannot modify paper order %s in terminal status %s", orderID, restingOrder.Status)
	}
	if restingOrder.ModCount >= MaxExchangeOrderMods {
		broker.mu.Unlock()
		return ErrMaxModificationsReached
	}

	restingOrder.ModCount++
	restingOrder.Quantity = newQty
	restingOrder.TriggerPrice = newTrigger
	restingOrder.Price = newLimit
	restingOrder.UpdatedAt = time.Now()
	orderCopy := *restingOrder
	broker.mu.Unlock()

	broker.emit(&orderCopy)
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
	restingOrder, exists := broker.orders[orderID]
	if !exists {
		broker.mu.Unlock()
		return fmt.Errorf("paper order %s not found", orderID)
	}
	if restingOrder.Status == OrderStatusComplete {
		broker.mu.Unlock()
		return nil
	}
	if restingOrder.ModCount >= MaxExchangeOrderMods {
		broker.mu.Unlock()
		return ErrMaxModificationsReached
	}

	fillPrice := restingOrder.TriggerPrice
	if stock != nil && stock.LTP > 0 {
		fillPrice = walkBidBook(qty, 0, stock)
	}

	restingOrder.ModCount++
	restingOrder.OrderType = "MARKET"
	restingOrder.Quantity = qty
	restingOrder.FilledQuantity = qty
	restingOrder.Price = 0
	restingOrder.TriggerPrice = 0
	restingOrder.AveragePrice = fillPrice
	restingOrder.Status = OrderStatusComplete
	restingOrder.UpdatedAt = time.Now()
	orderCopy := *restingOrder
	broker.mu.Unlock()

	broker.emit(&orderCopy)
	return nil
}

func (broker *PaperBroker) CancelOrder(_ context.Context, orderID string) error {
	broker.mu.Lock()
	restingOrder, exists := broker.orders[orderID]
	if !exists {
		broker.mu.Unlock()
		return fmt.Errorf("paper order %s not found", orderID)
	}
	if restingOrder.Status == OrderStatusComplete || restingOrder.Status == OrderStatusCancelled || restingOrder.Status == OrderStatusRejected {
		broker.mu.Unlock()
		return fmt.Errorf("paper order %s already %s", orderID, restingOrder.Status)
	}
	restingOrder.Status = OrderStatusCancelled
	restingOrder.UpdatedAt = time.Now()
	orderCopy := *restingOrder
	broker.mu.Unlock()

	broker.emit(&orderCopy)
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
	restingOrder := &paperRestingOrder{
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
	broker.orders[orderID] = restingOrder

	for _, existingOrder := range broker.byToken[token] {
		if existingOrder.OrderID == orderID {
			return
		}
	}
	broker.byToken[token] = append(broker.byToken[token], restingOrder)
}

func (broker *PaperBroker) FetchOrders(_ context.Context) ([]OrderUpdateEvent, error) {
	broker.mu.RLock()
	defer broker.mu.RUnlock()
	orderEvents := make([]OrderUpdateEvent, 0, len(broker.orders))
	for _, order := range broker.orders {
		orderEvents = append(orderEvents, OrderUpdateEvent{
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
		})
	}
	return orderEvents, nil
}

// EvaluateTickForToken checks resting Paper SL orders and BUY limit orders against live tick LTP and 5-level depth
func (broker *PaperBroker) EvaluateTickForToken(stockMetrics *market.StockMetrics) {
	if stockMetrics == nil || stockMetrics.LTP <= 0 {
		return
	}

	// Phase 1: Fast read check under RLock (99.99% of incoming ticks exit here in < 20ns)
	broker.mu.RLock()
	restingOrders := broker.byToken[stockMetrics.InstrumentToken]
	if len(restingOrders) == 0 {
		broker.mu.RUnlock()
		return
	}

	hasPotentialTrigger := false
	ltp := stockMetrics.LTP
	ask1 := float64(stockMetrics.Ask1Price)
	for _, restingOrder := range restingOrders {
		if restingOrder.TransactionType == "BUY" {
			if restingOrder.Status == OrderStatusOpen && (ltp <= restingOrder.Price || (ask1 > 0 && ask1 <= restingOrder.Price)) {
				hasPotentialTrigger = true
				break
			}
		} else { // SELL
			if restingOrder.Status == OrderStatusTrigPending && restingOrder.TriggerPrice > 0 && ltp <= restingOrder.TriggerPrice {
				hasPotentialTrigger = true
				break
			} else if restingOrder.Status == OrderStatusOpen {
				if restingOrder.OrderType == "SL" || restingOrder.OrderType == "MARKET" || restingOrder.Price == 0 || ltp >= restingOrder.Price {
					hasPotentialTrigger = true
					break
				}
			}
		}
	}
	broker.mu.RUnlock()

	if !hasPotentialTrigger {
		return
	}

	// Phase 2: Escalate to write lock only to fill genuinely triggered orders
	broker.mu.Lock()
	restingOrders = broker.byToken[stockMetrics.InstrumentToken]
	var triggeredOrders []paperRestingOrder
	for _, restingOrder := range restingOrders {
		if restingOrder.TransactionType == "BUY" {
			if restingOrder.Status == OrderStatusOpen {
				ask1 := float64(stockMetrics.Ask1Price)
				if stockMetrics.LTP <= restingOrder.Price || (ask1 > 0 && ask1 <= restingOrder.Price) {
					remainingToFill := restingOrder.Quantity - restingOrder.FilledQuantity
					if remainingToFill > 0 {
						availableAtOrBelow := 0
						for index := 0; index < 5; index++ {
							levelPrice := float64(stockMetrics.AskLevels[index].Price)
							levelQuantity := int(stockMetrics.AskLevels[index].Qty)
							if levelPrice > 0 && levelQuantity > 0 && levelPrice <= restingOrder.Price+1e-6 {
								availableAtOrBelow += levelQuantity
							}
						}
						if availableAtOrBelow == 0 && stockMetrics.LTP <= restingOrder.Price {
							availableAtOrBelow = remainingToFill
						}
						fillQty := min(remainingToFill, availableAtOrBelow)
						if fillQty > 0 {
							chunkAveragePrice := walkAskBook(fillQty, restingOrder.Price, stockMetrics)
							previousTotalValue := float64(restingOrder.FilledQuantity) * restingOrder.AveragePrice
							newFilledQuantity := restingOrder.FilledQuantity + fillQty
							restingOrder.AveragePrice = (previousTotalValue + float64(fillQty)*chunkAveragePrice) / float64(newFilledQuantity)
							restingOrder.FilledQuantity = newFilledQuantity
							if restingOrder.FilledQuantity >= restingOrder.Quantity {
								restingOrder.Status = OrderStatusComplete
							}
							restingOrder.UpdatedAt = time.Now()
							triggeredOrders = append(triggeredOrders, *restingOrder)
						}
					}
				}
			}
		} else { // SELL (SL or LIMIT)
			if restingOrder.Status == OrderStatusTrigPending && stockMetrics.LTP <= restingOrder.TriggerPrice {
				fillPrice := walkBidBook(restingOrder.Quantity, 0, stockMetrics)
				restingOrder.Status = OrderStatusComplete
				restingOrder.FilledQuantity = restingOrder.Quantity
				restingOrder.AveragePrice = fillPrice
				restingOrder.UpdatedAt = time.Now()
				triggeredOrders = append(triggeredOrders, *restingOrder)
			} else if restingOrder.Status == OrderStatusOpen {
				isEligibleToFill := false
				if restingOrder.OrderType == "SL" || restingOrder.OrderType == "MARKET" || restingOrder.Price == 0 {
					isEligibleToFill = true
				} else if restingOrder.OrderType == "LIMIT" && stockMetrics.LTP >= restingOrder.Price {
					isEligibleToFill = true
				}
				if isEligibleToFill {
					fillPrice := walkBidBook(restingOrder.Quantity, 0, stockMetrics)
					restingOrder.Status = OrderStatusComplete
					restingOrder.FilledQuantity = restingOrder.Quantity
					restingOrder.AveragePrice = fillPrice
					restingOrder.UpdatedAt = time.Now()
					triggeredOrders = append(triggeredOrders, *restingOrder)
				}
			}
		}
	}
	broker.mu.Unlock()

	for index := range triggeredOrders {
		broker.emit(&triggeredOrders[index])
	}
}
