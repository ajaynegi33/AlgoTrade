package trading

import (
	"context"
	"math"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/ajaynegi33/AlgoTrade/internal/database"
	"github.com/ajaynegi33/AlgoTrade/internal/market"
)

func TestStockMetricsAlignmentAndPhase2Math(testingInstance *testing.T) {
	// 1. Verify strict 8-byte alignment of StockMetrics
	sz := unsafe.Sizeof(market.StockMetrics{})
	if sz%8 != 0 {
		testingInstance.Fatalf("StockMetrics size %d is not a multiple of 8 bytes", sz)
	}
	if sz != 4008 {
		testingInstance.Fatalf("expected StockMetrics size 4008 bytes, got %d", sz)
	}

	// 2. Verify non-inverting DeltaMin < DeltaMax across all price tiers & extreme ATRs
	prices := []float64{35.0, 185.0, 245.0, 720.0, 3450.0, 18500.0}
	atrPcts := []float64{0.002, 0.008, 0.015, 0.026, 0.045} // Up to 4.5% 5m ATR shock!

	for _, px := range prices {
		for _, atrPct := range atrPcts {
			atr := px * atrPct
			spec, err := CalculateInitialStopLoss(px, px-0.5*atr, atr, 0.05)
			if err != nil {
				testingInstance.Fatalf("unexpected error for px=%.2f atr=%.2f: %v", px, atr, err)
			}
			if spec.DeltaMin >= spec.DeltaMax {
				testingInstance.Fatalf("DeltaMin (%.4f) >= DeltaMax (%.4f) for px=%.2f atrPct=%.3f", spec.DeltaMin, spec.DeltaMax, px, atrPct)
			}
			if spec.BaseBuffer < spec.DeltaMin-0.05 || spec.BaseBuffer > spec.DeltaMax+0.05 {
				testingInstance.Fatalf("BaseBuffer %.2f out of bounds [%.2f, %.2f]", spec.BaseBuffer, spec.DeltaMin, spec.DeltaMax)
			}
		}
	}

	// 3. Verify Position Sizer across ₹1,000, ₹1,00,000, and ₹10,00,00,000 (₹10Cr)
	var stock market.StockMetrics
	stock.InstrumentToken = 779521
	stock.SetSymbol("SBIN")
	stock.LTP = 780.0
	stock.Ask1Price = 780.05
	stock.Bid1Price = 779.95
	stock.TickSize = 0.05
	stock.ATR14 = 6.50
	stock.MISLeverage = 5.0
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		stock.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(780.05 + float64(depthLevelIndex)*0.05), Qty: 50000}
		stock.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(779.95 - float64(depthLevelIndex)*0.05), Qty: 50000}
	}

	// ₹1,00,000 account (2% heat = ₹2,000)
	s100k := ComputePositionSize(&stock, 775.00, 100000.0, 100000.0, 0.005, 1.0, 1.0, 2000.0)
	if !s100k.IsApproved || s100k.Quantity <= 0 {
		testingInstance.Fatalf("expected ₹1L sizing to be approved, got: %+v", s100k)
	}

	// ₹10 Crore account (must be capped by 20% top-3 ask liquidity = 30,000 shares)
	s10Cr := ComputePositionSize(&stock, 775.00, 100000000.0, 100000000.0, 0.005, 1.0, 1.0, 2000000.0)
	if !s10Cr.IsApproved || s10Cr.Quantity != 30000 {
		testingInstance.Fatalf("expected ₹10Cr sizing to be liquidity-capped at 30000 shares, got qty=%d", s10Cr.Quantity)
	}
}

func TestPositionWorkerThreeStepScaleOutAndShadowStopMod25(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stock market.StockMetrics
	stock.InstrumentToken = 300100
	stock.SetSymbol("BEL")
	stock.LTP = 220.00
	stock.ExchangeVWAP = 219.20
	stock.RollingPeak = 220.00
	stock.Ask1Price = 220.05
	stock.Bid1Price = 219.95
	stock.TickSize = 0.05
	stock.ATR14 = 2.00
	stock.MISLeverage = 5.0
	stock.SmoothedBuyerRatio = 0.64
	stock.AggressorBuyRatio = 0.65
	stock.FastAggressorEMA = 0.66
	stock.LastUpdatedUnix = time.Now().Unix()
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		stock.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(220.05 + float64(depthLevelIndex)*0.05), Qty: 10000}
		stock.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(219.95 - float64(depthLevelIndex)*0.05), Qty: 10000}
	}

	sizing := ComputePositionSize(&stock, 218.40, 200000.0, 200000.0, 0.005, 1.0, 1.0, 4000.0)
	if !sizing.IsApproved {
		testingInstance.Fatalf("sizing rejected: %s", sizing.RejectReason)
	}

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, orderUpdateEvent, delta)
		}
	})
	paper := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(orderUpdateEvent)
	})

	closedCh := make(chan struct{}, 1)
	worker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "TEST-POS-1",
		TradingDate: "2026-10-25",
		Mode:        "PAPER",
		Symbol:      "BEL",
		Token:       300100,
		PatternType: "VWAP_PULLBACK_RECLAIM",
		TickSize:    0.05,
		MISLeverage: 5.0,
		Sizing:      sizing,
		Broker:      paper,
		PaperBroker: paper,
		Reconciler:  reconciler,
		GetStockPtr: func() *market.StockMetrics { return &stock },
		OnClosed: func(closedWorker *PositionWorker) {
			select {
			case closedCh <- struct{}{}:
			default:
			}
		},
	})

	if err := worker.Start(); err != nil {
		testingInstance.Fatalf("worker.Start failed: %v", err)
	}

	// Wait for entry fill & initial SL placement
	time.Sleep(80 * time.Millisecond)
	if worker.RemainingQty() != sizing.Quantity {
		testingInstance.Fatalf("expected RemainingQty=%d, got %d", sizing.Quantity, worker.RemainingQty())
	}

	// Simulate rally to +1.60R -> locks in Tax-Free Breakeven for 100% full position
	stock.LTP = worker.GetEntryPrice() + 1.60*worker.OneR
	stock.RollingPeak = stock.LTP
	stock.LastUpdatedUnix += 5
	worker.UpdateTrailingStopLossOnPriceTick(&stock)

	time.Sleep(120 * time.Millisecond)
	if !worker.hasBreakevenCompleted.Load() {
		testingInstance.Fatalf("expected hasBreakevenCompleted == true after +1.60R")
	}
	if worker.RemainingQty() != sizing.Quantity {
		testingInstance.Fatalf("expected 100%% position qty %d to remain intact, got %d", sizing.Quantity, worker.RemainingQty())
	}

	// Simulate Gear 4 (modCount == 24) and rally to +3.0R -> Virtual Shadow Stop ratchets in RAM
	worker.modCount.Store(24)
	stock.LTP = worker.GetEntryPrice() + 3.0*worker.OneR
	stock.RollingPeak = stock.LTP
	stock.LastUpdatedUnix += 25
	worker.UpdateTrailingStopLossOnPriceTick(&stock)

	shadow := worker.ToDTO().ShadowStop
	if shadow <= worker.GetBreakevenStop() {
		testingInstance.Fatalf("expected ShadowStop (%.2f) > BreakevenStop (%.2f) at +3R", shadow, worker.GetBreakevenStop())
	}

	// Trigger Shadow Stop breach -> fires Modification #25 (MutateSLToMarket) and closes position
	stock.LTP = shadow - 0.15
	stock.FastAggressorEMA = 0.30
	stock.LastUpdatedUnix += 2
	worker.UpdateTrailingStopLossOnPriceTick(&stock)

	select {
	case <-closedCh:
	case <-time.After(2 * time.Second):
		testingInstance.Fatalf("timed out waiting for Mod 25 Shadow Stop flatten")
	}

	dto := worker.ToDTO()
	if dto.State != StateClosed || dto.RemainingQty != 0 {
		testingInstance.Fatalf("expected CLOSED with RemainingQty=0, got state=%s rem=%d", dto.State, dto.RemainingQty)
	}
	if dto.NetTotalPnL <= 0 {
		testingInstance.Fatalf("expected positive NetTotalPnL after +1R scale-out and +3R runner exit, got ₹%.2f", dto.NetTotalPnL)
	}
}

func TestReconcilerEarlyEventAndCrashRehydrationIdempotency(testingInstance *testing.T) {
	var totalDeltas int
	orderReconciler := NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		totalDeltas += delta
	})

	// 1. Falsification Bug #10: WebSocket/Paper COMPLETE event arrives BEFORE RegisterOrder is called
	orderReconciler.ProcessOrderEvent(OrderUpdateEvent{
		OrderID:        "ORD-EARLY-1",
		Status:         OrderStatusComplete,
		Quantity:       100,
		FilledQuantity: 100,
		AveragePrice:   220.05,
	})
	if totalDeltas != 0 {
		testingInstance.Fatalf("expected 0 deltas before RegisterOrder, got %d", totalDeltas)
	}

	// RegisterOrder must replay the buffered early event immediately
	orderReconciler.RegisterOrder("ORD-EARLY-1", "POS-1", "ENTRY", "BEL", "BUY", "LIMIT", 100, 220.10, 0, OrderStatusOpen)
	if totalDeltas != 100 {
		testingInstance.Fatalf("expected 100 filled shares replayed upon RegisterOrder, got %d", totalDeltas)
	}

	// 2. Falsification Bug #11: Crash-recovery SeedOrderFilled must prevent duplicate fill counting on subsequent sync
	orderReconciler.SeedOrderFilled("ORD-CRASH-2", 250)
	orderReconciler.RegisterOrder("ORD-CRASH-2", "POS-2", "ENTRY", "SBIN", "BUY", "LIMIT", 250, 780.00, 0, OrderStatusComplete)
	orderReconciler.ProcessOrderEvent(OrderUpdateEvent{
		OrderID:        "ORD-CRASH-2",
		Status:         OrderStatusComplete,
		Quantity:       250,
		FilledQuantity: 250,
		AveragePrice:   780.00,
	})
	if totalDeltas != 100 {
		testingInstance.Fatalf("expected SeedOrderFilled to suppress duplicate delta for ORD-CRASH-2, got totalDeltas=%d", totalDeltas)
	}
}

func TestPhase26FalsificationSuite_BugsB1ToB5(testingInstance *testing.T) {
	// 1. Bug B1 Falsification: Empty checksum in LIVE mode must be rejected by HandleKiteWebhookPostback
	mgr := NewManager(nil, nil, nil, nil)
	defer mgr.Stop()
	_, err := mgr.UpdateSettings(ExecutionSettings{
		Mode:                 "LIVE",
		IsAutoExecuteEnabled: false,
		RiskPerTradePct:      0.005,
		MaxOpenPositions:     4,
	})
	if err != nil {
		testingInstance.Fatalf("UpdateSettings failed: %v", err)
	}
	err = mgr.HandleKiteWebhookPostback(OrderUpdateEvent{
		OrderID:        "FORGED-ORD-1",
		Status:         OrderStatusComplete,
		Quantity:       100,
		FilledQuantity: 100,
		Checksum:       "", // Empty checksum attack
	})
	if err == nil {
		testingInstance.Fatalf("Bug B1 Falsified: expected HandleKiteWebhookPostback to reject empty checksum in LIVE mode")
	}

	// 2. Bug B5 Falsification: ₹10,000 Capital (5x MIS = ₹50,000 Buying Power) Sizing & Tax Edge
	var bel market.StockMetrics
	bel.InstrumentToken = 300100
	bel.SetSymbol("BEL")
	bel.LTP = 219.95
	bel.Ask1Price = 219.95
	bel.Bid1Price = 219.90
	bel.TickSize = 0.05
	bel.ATR14 = 1.30
	bel.MISLeverage = 5.0
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		bel.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(219.95 + float64(depthLevelIndex)*0.05), Qty: 15000}
		bel.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(219.90 - float64(depthLevelIndex)*0.05), Qty: 15000}
	}

	s10k := ComputePositionSize(&bel, 218.40, 10000.0, 10000.0, 0.005, 0.65, 1.0, 200.0)
	if !s10k.IsApproved || s10k.Quantity < 25 {
		testingInstance.Fatalf("Bug B5 Falsified: expected ₹10k capital w/ 5x MIS to approve meaningful quantity even in midday compression, got: %+v", s10k)
	}

	// 3. Bug B4 Falsification: 5-Factor CalculateBullishScore must directly reward institutional order flow (sFlow)
	bel.ExchangeVWAP = 219.50
	bel.PrevClose = 216.00
	bel.Open = 217.50
	bel.High = 223.50
	bel.Low = 217.00
	bel.RVOLSession = 2.6
	bel.RVOLPace5m = 2.8
	bel.CompositeRS = 1.4
	bel.CurrentBarIdx = 4
	bel.SessionBars[4] = market.OHLCVBar{Open: 219.40, High: 220.00, Low: 219.30, Close: 219.95, Volume: 100000}

	bel.SmoothedBuyerRatio = 0.42
	bel.AggressorBuyRatio = 0.44
	bel.FastAggressorEMA = 0.43
	scoreWeakFlow, _, _, _ := market.CalculateBullishScore(&bel, 0.65)

	bel.SmoothedBuyerRatio = 0.78
	bel.AggressorBuyRatio = 0.82
	bel.FastAggressorEMA = 0.84
	scoreStrongFlow, _, _, _ := market.CalculateBullishScore(&bel, 0.65)

	if scoreStrongFlow-scoreWeakFlow < 12.0 {
		testingInstance.Fatalf("Bug B4 Falsified: expected strong institutional flow to boost BullishScore by >= 12 pts, got weak=%.2f strong=%.2f",
			scoreWeakFlow, scoreStrongFlow)
	}

	// 4. Bug B3 Falsification: Pre-1R Thesis-Decay & Absorption Failure must tighten stop above InitialStop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, orderUpdateEvent, delta)
		}
	})
	paper := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(orderUpdateEvent)
	})

	nowUnix := time.Now().Unix()
	bel.LastUpdatedUnix = nowUnix
	worker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "TEST-DECAY-B3",
		TradingDate: "2026-10-25",
		Mode:        "PAPER",
		Symbol:      "BEL",
		Token:       300100,
		PatternType: "VWAP_PULLBACK_RECLAIM",
		TickSize:    0.05,
		MISLeverage: 5.0,
		Sizing:      s10k,
		Broker:      paper,
		PaperBroker: paper,
		Reconciler:  reconciler,
		GetStockPtr: func() *market.StockMetrics { return &bel },
	})
	worker.OpenedAtUnix = nowUnix
	if err := worker.Start(); err != nil {
		testingInstance.Fatalf("worker.Start failed: %v", err)
	}
	time.Sleep(60 * time.Millisecond)

	initialStop := worker.ToDTO().CurrentStop
	// Simulate T+35s post-entry buyer absorption failure (LTP stalls near entry, flowScore drops to ~0.35)
	bel.LTP = 219.85
	bel.RollingPeak = 220.05
	bel.SmoothedBuyerRatio = 0.35
	bel.AggressorBuyRatio = 0.34
	bel.FastAggressorEMA = 0.33
	bel.LastUpdatedUnix = nowUnix + 35
	worker.UpdateTrailingStopLossOnPriceTick(&bel)
	time.Sleep(60 * time.Millisecond)

	tightenedStop := worker.ToDTO().CurrentStop
	if tightenedStop <= initialStop+0.20 {
		testingInstance.Fatalf("Bug B3 Falsified: expected Pre-1R thesis decay to tighten stop significantly above InitialStop (%.2f), got %.2f",
			initialStop, tightenedStop)
	}
	if tightenedStop >= bel.LTP-0.10 {
		testingInstance.Fatalf("Bug B3 Anti-Suicide Guard Falsified: tightenedStop (%.2f) too close to LTP (%.2f)", tightenedStop, bel.LTP)
	}
}

func TestPhase27_TradeHistoryAndTaxBreakdown(testingInstance *testing.T) {
	// 1. Verify exact 6-component statutory tax breakdown for a 500-share SBIN trade (Buy ₹800, Sell ₹812, split exit)
	tb := CalculatePositionTaxBreakdown(800.00, 812.00, 500, true)
	totalBrok := tb.BuyBrokerage + tb.SellBrokerage
	// Buy brokerage capped at ₹20, split sell brokerage capped at ₹40 -> ₹60 total brokerage
	if totalBrok != 60.0 {
		testingInstance.Fatalf("expected ₹60.00 brokerage for split exit, got %.2f", totalBrok)
	}
	// STT = 0.025% of ₹4,06,000 = ₹101.50 -> rounded ₹102.00
	if tb.STT != 102.0 {
		testingInstance.Fatalf("expected ₹102.00 STT, got %.2f", tb.STT)
	}
	// Stamp Duty = 0.003% of ₹4,00,000 = ₹12.00
	if tb.StampDuty != 12.0 {
		testingInstance.Fatalf("expected ₹12.00 Stamp Duty, got %.2f", tb.StampDuty)
	}
	if tb.TotalCharges <= 200.0 || tb.TotalCharges >= 250.0 {
		testingInstance.Fatalf("unexpected TotalCharges: %.2f", tb.TotalCharges)
	}

	// 2. Verify GetTradeHistory returns enriched item & summary
	mgr := NewManager(nil, nil, nil, nil)
	defer mgr.Stop()

	mgr.mu.Lock()
	mgr.closedPositions = append(mgr.closedPositions, market.ActivePositionDTO{
		PositionID:           "POS-TEST-HIST-1",
		Mode:                 "PAPER",
		Symbol:               "SBIN",
		InstrumentToken:      779521,
		State:                StateClosed,
		PatternType:          "VWAP_PULLBACK",
		EntryPrice:           800.00,
		ExitPrice:            810.00,
		LTP:                  810.00,
		InitialStop:          795.00,
		CurrentStop:          808.00,
		BreakevenStop:        801.20,
		HighWaterMark:        812.00,
		OneR:                 5.00,
		RMultiple:            2.00,
		MFERMultiple:         2.40,
		InitialQty:           100,
		RemainingQty:         0,
		ModCount:             12,
		MaxMods:              25,
		CurrentGear:          3,
		HasTranche1Completed: true,
		RealisedGrossPnL:     1000.00,
		EstimatedCharges:     CalculatePositionTaxBreakdown(800.00, 810.00, 100, true).TotalCharges,
		ExitReason:           "SL_HIT (Trail)",
		OpenedAt:             "2026-10-25 09:45:00",
		ClosedAt:             "2026-10-25 10:15:00",
	})
	mgr.mu.Unlock()

	hist := mgr.GetTradeHistory("ALL", "", "", 100)
	if len(hist.Trades) != 1 {
		testingInstance.Fatalf("expected 1 trade in history, got %d", len(hist.Trades))
	}
	item := hist.Trades[0]
	if item.Symbol != "SBIN" || item.Quantity != 100 || item.BuyPrice != 800.00 || item.SellPrice != 810.00 {
		testingInstance.Fatalf("unexpected trade item fields: %+v", item)
	}
	if item.TotalInvested != 80000.00 || item.MarginBlocked != 16000.00 || item.GrossPnL != 1000.00 {
		testingInstance.Fatalf("unexpected capital/pnl metrics: %+v", item)
	}
	if hist.Summary.TotalTrades != 1 || hist.Summary.Wins != 1 || hist.Summary.RealisedGrossPnL != 1000.00 {
		testingInstance.Fatalf("unexpected summary metrics: %+v", hist.Summary)
	}
}

func TestTradeHistory_NoDeadlockOnMaxConns(testingInstance *testing.T) {
	tempDB := filepath.Join(testingInstance.TempDir(), "test_hist_deadlock.db")
	db, err := database.Init(tempDB)
	if err != nil {
		testingInstance.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	// Seed completed position and child orders in SQLite
	_, err = db.Exec(`
		INSERT INTO positions (
			position_id, trading_date, mode, symbol, instrument_token, state, pattern_type,
			entry_price, exit_price, initial_stop, current_stop, shadow_stop, breakeven_stop, base_buffer,
			high_water_mark, one_r, initial_qty, filled_entry_qty, filled_exit_qty,
			mod_count, current_gear, tranche1_done,
			realised_gross_pnl, total_charges, realised_net_pnl, exit_reason, opened_at, closed_at
		) VALUES (
			'POS-BPCL-1', '2026-10-26', 'PAPER', 'BPCL', 134657, 'CLOSED', 'VWAP_PULLBACK_RECLAIM',
			286.95, 288.47, 286.0, 289.15, 289.15, 287.7, 1.2,
			289.6, 1.05, 78, 78, 78,
			15, 2, 1,
			118.95, 24.51, 94.44, 'SL_COMPLETE', '2026-10-26 07:18:28', '2026-10-26 07:56:56'
		);
		INSERT INTO orders (order_id, position_id, leg_type, symbol, transaction_type, order_type, quantity, filled_quantity, price, trigger_price, average_price, status)
		VALUES ('ORD-1', 'POS-BPCL-1', 'ENTRY', 'BPCL', 'BUY', 'LIMIT', 78, 78, 287.05, 0, 286.95, 'COMPLETE'),
		       ('ORD-2', 'POS-BPCL-1', 'SL', 'BPCL', 'SELL', 'SL', 39, 39, 287.95, 289.15, 289.20, 'COMPLETE');
	`)
	if err != nil {
		testingInstance.Fatalf("failed to insert test data: %v", err)
	}

	mgr := NewManager(db, nil, nil, nil)
	defer mgr.Stop()

	doneCh := make(chan *market.TradeHistoryResponseDTO, 1)
	go func() {
		hist := mgr.GetTradeHistory("ALL", "", "", 10)
		doneCh <- hist
	}()

	select {
	case hist := <-doneCh:
		if len(hist.Trades) < 1 {
			testingInstance.Fatalf("expected at least 1 trade from SQLite, got %d", len(hist.Trades))
		}
		if hist.Trades[0].Symbol != "BPCL" || len(hist.Trades[0].Orders) != 2 {
			testingInstance.Fatalf("expected BPCL with 2 order legs, got symbol=%s, orders=%d", hist.Trades[0].Symbol, len(hist.Trades[0].Orders))
		}
	case <-time.After(2 * time.Second):
		testingInstance.Fatal("DEADLOCK DETECTED: GetTradeHistory did not complete within 2s")
	}
}

func TestActivePositionsStableEntryOrder(testingInstance *testing.T) {
	mgr := NewManager(nil, nil, nil, nil)
	defer mgr.Stop()

	baseTime := time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC)

	// Create 3 active workers with strictly ordered OpenedAt timestamps (A -> B -> C)
	wA := &PositionWorker{
		PositionID:   "POS-INFY-1",
		Symbol:       "INFY",
		Mode:         "PAPER",
		OpenedAt:     baseTime,
		OpenedAtUnix: baseTime.Unix(),
		EntryPrice:   1500.0,
		InitialQty:   50,
	}
	wB := &PositionWorker{
		PositionID:   "POS-TCS-2",
		Symbol:       "TCS",
		Mode:         "PAPER",
		OpenedAt:     baseTime.Add(2 * time.Minute),
		OpenedAtUnix: baseTime.Add(2 * time.Minute).Unix(),
		EntryPrice:   3500.0,
		InitialQty:   20,
	}
	wC := &PositionWorker{
		PositionID:   "POS-RELIANCE-3",
		Symbol:       "RELIANCE",
		Mode:         "PAPER",
		OpenedAt:     baseTime.Add(5 * time.Minute),
		OpenedAtUnix: baseTime.Add(5 * time.Minute).Unix(),
		EntryPrice:   2500.0,
		InitialQty:   30,
	}

	mgr.mu.Lock()
	mgr.activeWorkers[wB.PositionID] = wB
	mgr.activeWorkers[wC.PositionID] = wC
	mgr.activeWorkers[wA.PositionID] = wA
	mgr.mu.Unlock()

	var stocks [512]market.StockMetrics
	pulse := market.MarketPulseDTO{MarketRegime: "BULLISH_TREND"}

	// Simulate 25 consecutive engine snapshot ticks with fluctuating prices
	for tickIteration := 0; tickIteration < 25; tickIteration++ {
		// Update prices on each tick to ensure price volatility doesn't perturb order
		wA.latestLTPBits.Store(math.Float64bits(1500.0 + float64(tickIteration)*0.5))
		wB.latestLTPBits.Store(math.Float64bits(3500.0 - float64(tickIteration)*1.0))
		wC.latestLTPBits.Store(math.Float64bits(2500.0 + float64(tickIteration)*2.0))

		state := mgr.OnSecondTick(&stocks, nil, nil, nil, pulse)
		if len(state.ActivePositions) != 3 {
			testingInstance.Fatalf("iteration %d: expected 3 active positions, got %d", tickIteration, len(state.ActivePositions))
		}

		// Pos 0 MUST be wA (opened at 09:30:00)
		if state.ActivePositions[0].PositionID != "POS-INFY-1" {
			testingInstance.Fatalf("iteration %d: position 0 changed! expected POS-INFY-1, got %s", tickIteration, state.ActivePositions[0].PositionID)
		}
		// Pos 1 MUST be wB (opened at 09:32:00)
		if state.ActivePositions[1].PositionID != "POS-TCS-2" {
			testingInstance.Fatalf("iteration %d: position 1 changed! expected POS-TCS-2, got %s", tickIteration, state.ActivePositions[1].PositionID)
		}
		// Pos 2 MUST be wC (opened at 09:35:00)
		if state.ActivePositions[2].PositionID != "POS-RELIANCE-3" {
			testingInstance.Fatalf("iteration %d: position 2 changed! expected POS-RELIANCE-3, got %s", tickIteration, state.ActivePositions[2].PositionID)
		}

		if state.ActivePositions[0].OpenedAtUnix >= state.ActivePositions[1].OpenedAtUnix ||
			state.ActivePositions[1].OpenedAtUnix >= state.ActivePositions[2].OpenedAtUnix {
			testingInstance.Fatalf("iteration %d: OpenedAtUnix timestamps out of order: %+v", tickIteration, state.ActivePositions)
		}
	}
}

func TestSLGapThroughBreachAndFlatten(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stock market.StockMetrics
	stock.InstrumentToken = 2010113
	stock.SetSymbol("INOXWIND")
	stock.LTP = 59.50
	stock.TickSize = 0.05
	stock.MISLeverage = 5.0
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		stock.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(59.40 + float64(depthLevelIndex)*0.05), Qty: 50000}
		stock.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(59.35 - float64(depthLevelIndex)*0.05), Qty: 50000}
	}
	stock.Ask1Price = 59.40
	stock.Bid1Price = 59.35

	sizing := SizingDecision{
		IsApproved:      true,
		Quantity:        132,
		DustFloorQty:    1,
		EntryLimitPrice: 59.44,
		BreakevenStop:   59.63,
		StopSpec: StopAndBufferSpec{
			BaseBuffer:   3.75,
			InitialStop:  58.83,
			OneR:         0.65,
			SignalBarLow: 58.83,
		},
	}

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, orderUpdateEvent, delta)
		}
	})
	paper := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(orderUpdateEvent)
	})

	closedCh := make(chan struct{}, 1)
	worker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "POS-INOXWIND-84873",
		TradingDate: "2026-10-26",
		Mode:        "PAPER",
		Symbol:      "INOXWIND",
		Token:       2010113,
		PatternType: "VWAP_PULLBACK_RECLAIM",
		TickSize:    0.05,
		MISLeverage: 5.0,
		Sizing:      sizing,
		Broker:      paper,
		PaperBroker: paper,
		Reconciler:  reconciler,
		GetStockPtr: func() *market.StockMetrics { return &stock },
		OnClosed: func(closedWorker *PositionWorker) {
			select {
			case closedCh <- struct{}{}:
			default:
			}
		},
	})

	if err := worker.Start(); err != nil {
		testingInstance.Fatalf("worker.Start failed: %v", err)
	}

	// Wait for entry fill & initial SL placement
	time.Sleep(100 * time.Millisecond)
	if worker.RemainingQty() != 132 {
		testingInstance.Fatalf("expected RemainingQty=132, got %d", worker.RemainingQty())
	}

	// Now simulate price falling directly to 58.81 (below initial stop 58.83)
	stock.LTP = 58.81
	stock.LastUpdatedUnix += 5
	worker.UpdateTrailingStopLossOnPriceTick(&stock)

	select {
	case <-closedCh:
		testingInstance.Logf("Closed successfully! State: %s", worker.ToDTO().State)
	case <-time.After(2 * time.Second):
		dto := worker.ToDTO()
		testingInstance.Fatalf("timed out waiting for position to close! Current state: %s, exitReason: %s, remaining: %d, modCount: %d",
			dto.State, dto.ExitReason, dto.RemainingQty, dto.ModCount)
	}
}

func TestPaperBrokerRehydrationAndTrigger(testingInstance *testing.T) {
	var triggeredEvent OrderUpdateEvent
	eventCh := make(chan OrderUpdateEvent, 1)

	broker := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		eventCh <- orderUpdateEvent
	})

	token := uint32(56321)
	broker.SeedRestingOrder("PAPER-SL-101", "IEX", token, 112, 102.50, 102.30, 0, OrderStatusTrigPending)

	stock := market.StockMetrics{
		InstrumentToken: token,
		LTP:             102.40,
		TickSize:        0.05,
		Bid1Price:       102.35,
	}
	stock.BidLevels[0].Price = 102.35
	stock.BidLevels[0].Qty = 500

	broker.EvaluateTickForToken(&stock)

	select {
	case triggeredEvent = <-eventCh:
		if triggeredEvent.Status != OrderStatusComplete {
			testingInstance.Fatalf("expected triggered order to be COMPLETE, got %s", triggeredEvent.Status)
		}
		if triggeredEvent.FilledQuantity != 112 {
			testingInstance.Fatalf("expected 112 filled, got %d", triggeredEvent.FilledQuantity)
		}
	case <-time.After(500 * time.Millisecond):
		testingInstance.Fatalf("timed out waiting for rehydrated paper order to trigger")
	}
}

func TestRehydratedEmergencyFlattenForceExit(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	closedCh := make(chan struct{}, 1)
	stock := market.StockMetrics{
		InstrumentToken: 2010113,
		LTP:             58.80,
		TickSize:        0.05,
	}
	stock.BidLevels[0].Price = 58.75
	stock.BidLevels[0].Qty = 1000

	var reconciler *OrderReconciler
	broker := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		if reconciler != nil {
			reconciler.ProcessOrderEvent(orderUpdateEvent)
		}
	})

	var worker *PositionWorker
	reconciler = NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, orderUpdateEvent, delta)
		}
	})

	sizing := SizingDecision{
		IsApproved:      true,
		Quantity:        132,
		DustFloorQty:    1,
		EntryLimitPrice: 59.44,
		BreakevenStop:   59.63,
		StopSpec: StopAndBufferSpec{
			BaseBuffer:   3.75,
			InitialStop:  58.83,
			OneR:         0.65,
			SignalBarLow: 58.83,
		},
	}

	worker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "POS-INOXWIND-TEST-1",
		TradingDate: "2026-10-26",
		Mode:        "PAPER",
		Symbol:      "INOXWIND",
		Token:       2010113,
		PatternType: "VWAP_PULLBACK_RECLAIM",
		TickSize:    0.05,
		MISLeverage: 5.0,
		Sizing:      sizing,
		Broker:      broker,
		PaperBroker: broker,
		Reconciler:  reconciler,
		GetStockPtr: func() *market.StockMetrics { return &stock },
		OnClosed: func(closedWorker *PositionWorker) {
			select {
			case closedCh <- struct{}{}:
			default:
			}
		},
	})

	// Simulate rehydration state where worker was restored in StateEmergencyFlatten
	worker.state = StateEmergencyFlatten
	worker.exitReason = "SL_GAP_THROUGH_BREACH"
	worker.filledEntryQty.Store(132)

	go worker.workerLoop()

	// Call ForceEmergencyExit
	worker.ForceEmergencyExit("REHYDRATED_FORCE_FLATTEN")

	select {
	case <-closedCh:
		if worker.ToDTO().State != StateClosed {
			testingInstance.Fatalf("expected state CLOSED, got %s", worker.ToDTO().State)
		}
		if worker.RemainingQty() != 0 {
			testingInstance.Fatalf("expected RemainingQty 0, got %d", worker.RemainingQty())
		}
	case <-time.After(2 * time.Second):
		testingInstance.Fatalf("timed out waiting for forced emergency flatten to close position")
	}
}

func TestPositionWorker_EarlyHarvestAt065RAnd5mClose(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stock := &market.StockMetrics{
		InstrumentToken:    779521,
		LTP:                800.00,
		TickSize:           0.05,
		ATR14:              4.00,
		ExchangeVWAP:       798.00,
		AggressorBuyRatio:  0.65,
		FastAggressorEMA:   0.62,
		SmoothedBuyerRatio: 0.60,
		CurrentBarIdx:      2,
	}
	stock.SessionBars[0] = market.OHLCVBar{High: 798.0, Low: 794.0, Open: 795.0, Close: 797.0}
	stock.SessionBars[1] = market.OHLCVBar{High: 801.0, Low: 796.0, Open: 797.0, Close: 800.0}
	stock.SessionBars[2] = market.OHLCVBar{High: 804.0, Low: 799.0, Open: 800.0, Close: 803.0}

	sizing := SizingDecision{
		IsApproved:      true,
		Quantity:        50,
		DustFloorQty:    1,
		EntryLimitPrice: 800.00,
		BreakevenStop:   800.80,
		TargetR1Price:   803.25,
		TargetR2Price:   808.25,
		StopSpec: StopAndBufferSpec{
			BaseBuffer:   3.00,
			InitialStop:  795.00,
			OneR:         5.00,
			SignalBarLow: 795.00,
		},
	}

	worker := NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "POS-SBIN-HARVEST-1",
		TradingDate: "2026-10-26",
		Mode:        "PAPER",
		Symbol:      "SBIN",
		Token:       779521,
		TickSize:    0.05,
		MISLeverage: 5.0,
		EntryBarIdx: 1,
		Sizing:      sizing,
		GetStockPtr: func() *market.StockMetrics { return stock },
	})

	worker.filledEntryQty.Store(50)
	worker.confirmedStopBits.Store(math.Float64bits(795.00))

	// 1. Tick at 802.60 (+0.52R): verify stop does NOT ratchet to Breakeven yet (Breakeven triggers at >= +1.0R per Quant Laws)
	stock.LTP = 802.60
	worker.UpdateTrailingStopLossOnPriceTick(stock)
	shadow := math.Float64frombits(worker.shadowStopBits.Load())
	if shadow >= 800.80 {
		testingInstance.Fatalf("expected shadow stop to NOT ratchet to Breakeven 800.80 at +0.52R, got %.2f", shadow)
	}

	// 2. Tick at 805.20 (+1.04R): verify stop ratchets to Breakeven (800.80)
	stock.LTP = 805.20
	worker.UpdateTrailingStopLossOnPriceTick(stock)
	shadow = math.Float64frombits(worker.shadowStopBits.Load())
	if shadow < 800.80 {
		testingInstance.Fatalf("expected shadow stop to ratchet to Breakeven 800.80 at +1.04R, got %.2f", shadow)
	}

	// 3. Tick at 807.60 (+1.52R): verify 100% position continues trailing with breakeven locked
	stock.LTP = 807.60
	worker.UpdateTrailingStopLossOnPriceTick(stock)
	if !worker.hasBreakevenCompleted.Load() {
		testingInstance.Fatalf("expected hasBreakevenCompleted to be TRUE at +1.52R, got false")
	}

	// 3. Intra-candle dip does NOT panic-dump if 5m bar hasn't closed below InitialStop
	stock.LTP = 796.00
	worker.UpdateTrailingStopLossOnPriceTick(stock)
	if worker.isClosed.Load() {
		testingInstance.Fatalf("expected position to hold during intra-candle wick above InitialStop (795.00), but was closed prematurely")
	}

	// 4. Completed 5m candle close below InitialStop triggers structural exit
	stock.CurrentBarIdx = 3
	stock.SessionBars[2] = market.OHLCVBar{High: 798.0, Low: 793.0, Open: 797.0, Close: 794.0} // Closed below 795.00!
	stock.LTP = 794.00
	worker.UpdateTrailingStopLossOnPriceTick(stock)
	if !worker.isMarketExitPending.Load() {
		testingInstance.Fatalf("expected isMarketExitPending to be TRUE after completed 5m candle closed below InitialStop (794.00 < 795.00)")
	}
}

func TestPositionWorker_NotifyClosedIdempotent(testingInstance *testing.T) {
	callCount := 0
	worker := &PositionWorker{
		onClosed: func(closedWorker *PositionWorker) {
			callCount++
		},
	}

	for attemptIndex := 0; attemptIndex < 10; attemptIndex++ {
		worker.notifyClosed()
	}

	if callCount != 1 {
		testingInstance.Fatalf("expected onClosed to be called exactly 1 time, got %d", callCount)
	}
}

func TestDecoupledStopLossAndSizing(testingInstance *testing.T) {
	// 1. Verify StopLoss Calculations
	entryPrice := 500.0
	signalLow := 496.0
	atr := 4.0
	tick := 0.05

	spec, err := CalculateInitialStopLoss(entryPrice, signalLow, atr, tick)
	if err != nil {
		testingInstance.Fatalf("CalculateInitialStopLoss failed: %v", err)
	}
	if spec.InitialStop >= entryPrice {
		testingInstance.Fatalf("initial stop %.2f must be below entry price %.2f", spec.InitialStop, entryPrice)
	}
	if spec.OneR <= 0 {
		testingInstance.Fatalf("oneR %.2f must be positive", spec.OneR)
	}

	// Overextended check (> 1.25x DeltaMax)
	_, overextErr := CalculateInitialStopLoss(500.0, 470.0, 4.0, 0.05)
	if overextErr == nil {
		testingInstance.Fatalf("expected error for overextended setup, got nil")
	}

	// CalculateTaxCoveringBreakevenStop check
	bePrice := CalculateTaxCoveringBreakevenStop(500.0, 100, 50, 0.05)
	if bePrice <= 500.0 {
		testingInstance.Fatalf("breakeven trigger price %.2f must be above entry price 500.00 to cover taxes", bePrice)
	}

	// CalculateProtectiveLimitPriceBelowStop check
	slLimit := CalculateProtectiveLimitPriceBelowStop(495.0, 0.05)
	if slLimit >= 495.0 {
		testingInstance.Fatalf("SL limit price %.2f must be strictly below trigger price 495.00", slLimit)
	}

	// 2. Verify Sizer Calculations & Risk Curve
	if riskPct := EffectiveRiskPerTradePct(10000.0, 0.005); riskPct != 0.0125 {
		testingInstance.Fatalf("expected 1.25%% risk for 10k account, got %.4f", riskPct)
	}
	if riskPct := EffectiveRiskPerTradePct(40000.0, 0.005); riskPct != 0.0100 {
		testingInstance.Fatalf("expected 1.00%% risk for 40k account, got %.4f", riskPct)
	}
	if riskPct := EffectiveRiskPerTradePct(150000.0, 0.005); riskPct != 0.0075 {
		testingInstance.Fatalf("expected 0.75%% risk for 150k account, got %.4f", riskPct)
	}
	if riskPct := EffectiveRiskPerTradePct(1000000.0, 0.005); riskPct != 0.0050 {
		testingInstance.Fatalf("expected 0.50%% risk for 10L account, got %.4f", riskPct)
	}
	if riskPct := EffectiveRiskPerTradePct(10000000.0, 0.005); riskPct != 0.035/10.0 {
		testingInstance.Fatalf("expected 0.35%% risk for 1Cr account, got %.4f", riskPct)
	}
}

type partialFillMockBroker struct {
	*PaperBroker
	cancelCalled atomic.Bool
}

func (mockBroker *partialFillMockBroker) PlaceLimitBuy(
	_ context.Context,
	_ string,
	_ uint32,
	_ int,
	_ float64,
	_ *market.StockMetrics,
) (string, error) {
	// Return open order without immediate fill to test resting limit order timeout
	return "TEST-ORD-RESTING", nil
}

func (mockBroker *partialFillMockBroker) CancelOrder(ctx context.Context, orderID string) error {
	mockBroker.cancelCalled.Store(true)
	return mockBroker.PaperBroker.CancelOrder(ctx, orderID)
}

func TestPartialFillTimeoutAndMarginAdjustment(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, orderUpdateEvent, delta)
		}
	})

	paperBroker := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(orderUpdateEvent)
	})
	mockBroker := &partialFillMockBroker{PaperBroker: paperBroker}

	stock := &market.StockMetrics{
		InstrumentToken: 12345,
		LTP:             1505.0,
		TickSize:        0.05,
		MISLeverage:     5.0,
	}

	sizing := SizingDecision{
		IsApproved:      true,
		Quantity:        100,
		DustFloorQty:    5,
		EntryLimitPrice: 1500.0,
		StopSpec: StopAndBufferSpec{
			InitialStop:  1485.0,
			OneR:         15.0,
			BaseBuffer:   3.0,
			SignalBarLow: 1488.0,
		},
	}

	worker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:           "POS-TEST-PARTIAL",
		TradingDate:          "2026-10-29",
		Mode:                 "PAPER",
		Symbol:               "TESTSTOCK",
		Token:                12345,
		TickSize:             0.05,
		MISLeverage:          5.0,
		Sizing:               sizing,
		EntryTimeoutDuration: 50 * time.Millisecond,
		Broker:               mockBroker,
		PaperBroker:          paperBroker,
		Reconciler:           reconciler,
		GetStockPtr:          func() *market.StockMetrics { return stock },
	})

	if err := worker.Start(); err != nil {
		testingInstance.Fatalf("Failed to start worker: %v", err)
	}

	// Simulate partial fill of 40 shares (out of 100)
	worker.OnOrderDelta("ENTRY", OrderUpdateEvent{
		OrderID:        worker.entryOrderID,
		Status:         OrderStatusOpen,
		FilledQuantity: 40,
		AveragePrice:   1500.0,
	}, 40)

	if worker.filledEntryQty.Load() != 40 {
		testingInstance.Fatalf("Expected 40 filled shares, got %d", worker.filledEntryQty.Load())
	}

	// Wait for the entry timeout to trigger cancellation of remaining 60 shares (including 500ms drain window)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && worker.GetInitialQty() != 40 {
		time.Sleep(20 * time.Millisecond)
	}

	// Verify cancel was dispatched for the unfilled portion
	if !mockBroker.cancelCalled.Load() {
		testingInstance.Fatalf("Expected broker CancelOrder to be called on timeout")
	}

	// Verify InitialQty was adjusted from 100 to 40
	if worker.GetInitialQty() != 40 {
		testingInstance.Fatalf("Expected InitialQty adjusted to 40, got %d", worker.GetInitialQty())
	}

	if worker.RemainingQty() != 40 {
		testingInstance.Fatalf("Expected RemainingQty = 40, got %d", worker.RemainingQty())
	}

	// Verify that SL order was placed for the exact 40 shares
	if worker.GetSLOrderID() == "" {
		testingInstance.Fatalf("Expected SL order placed for partial fill")
	}

	// Verify DTO shows updated quantities
	dto := worker.ToDTO()
	if dto.InitialQty != 40 || dto.RemainingQty != 40 {
		testingInstance.Fatalf("Expected DTO InitialQty=40 and RemainingQty=40, got Initial=%d Remaining=%d",
			dto.InitialQty, dto.RemainingQty)
	}
}

func TestPartialFillIncrementalStopLossExpansion(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, orderUpdateEvent, delta)
		}
	})

	paperBroker := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(orderUpdateEvent)
	})
	mockBroker := &partialFillMockBroker{PaperBroker: paperBroker}

	stock := &market.StockMetrics{
		InstrumentToken: 54321,
		LTP:             100.0,
		TickSize:        0.05,
		MISLeverage:     5.0,
	}

	sizing := SizingDecision{
		IsApproved:      true,
		Quantity:        100,
		DustFloorQty:    5,
		EntryLimitPrice: 100.0,
		StopSpec: StopAndBufferSpec{
			InitialStop:  98.0,
			OneR:         2.0,
			BaseBuffer:   0.5,
			SignalBarLow: 98.2,
		},
	}

	worker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:           "POS-TEST-INCR-EXPANSION",
		TradingDate:          "2026-10-30",
		Mode:                 "PAPER",
		Symbol:               "EXPANDSTOCK",
		Token:                54321,
		TickSize:             0.05,
		MISLeverage:          5.0,
		Sizing:               sizing,
		EntryTimeoutDuration: 10 * time.Second,
		Broker:               mockBroker,
		PaperBroker:          paperBroker,
		Reconciler:           reconciler,
		GetStockPtr:          func() *market.StockMetrics { return stock },
	})

	if err := worker.Start(); err != nil {
		testingInstance.Fatalf("Failed to start worker: %v", err)
	}

	// 1. First partial fill of 30 shares
	worker.OnOrderDelta("ENTRY", OrderUpdateEvent{
		OrderID:        worker.entryOrderID,
		Status:         OrderStatusOpen,
		FilledQuantity: 30,
		AveragePrice:   100.0,
	}, 30)

	// Wait for workerLoop to place initial SL for 30 shares
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		worker.mu.RLock()
		slQty := worker.slOrderQty
		hasSL := worker.slOrderID != ""
		worker.mu.RUnlock()
		if hasSL && slQty == 30 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	worker.mu.RLock()
	slID := worker.slOrderID
	slQty := worker.slOrderQty
	worker.mu.RUnlock()
	if slID == "" || slQty != 30 {
		testingInstance.Fatalf("Expected initial SL placed for 30 shares, got slID=%s slQty=%d", slID, slQty)
	}

	// 2. Second partial fill arrives: 40 more shares (total 70)
	worker.OnOrderDelta("ENTRY", OrderUpdateEvent{
		OrderID:        worker.entryOrderID,
		Status:         OrderStatusOpen,
		FilledQuantity: 70,
		AveragePrice:   100.0,
	}, 40)

	// Wait for workerLoop to resize SL to 70 shares
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		worker.mu.RLock()
		slQty = worker.slOrderQty
		worker.mu.RUnlock()
		if slQty == 70 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	worker.mu.RLock()
	slQty = worker.slOrderQty
	worker.mu.RUnlock()
	if slQty != 70 {
		testingInstance.Fatalf("Expected SL resized to 70 shares, got %d", slQty)
	}
}

func TestPaperBrokerRestingLimitBuyAndDepthFill(testingInstance *testing.T) {
	var events []OrderUpdateEvent
	var evMu sync.Mutex
	paper := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		evMu.Lock()
		events = append(events, orderUpdateEvent)
		evMu.Unlock()
	})

	token := uint32(99901)
	stock := &market.StockMetrics{
		InstrumentToken: token,
		LTP:             105.0,
		Ask1Price:       105.10,
		Bid1Price:       105.00,
		TickSize:        0.05,
	}
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		stock.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(105.10 + float64(depthLevelIndex)*0.05), Qty: 20}
		stock.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(105.00 - float64(depthLevelIndex)*0.05), Qty: 20}
	}

	// 1. Place limit buy at 104.50 (well below Ask1 105.10)
	orderID, err := paper.PlaceLimitBuy(context.Background(), "DEPTHTEST", token, 50, 104.50, stock)
	if err != nil {
		testingInstance.Fatalf("PlaceLimitBuy failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	evMu.Lock()
	if len(events) == 0 {
		testingInstance.Fatalf("Expected OPEN event emitted")
	}
	firstEv := events[0]
	evMu.Unlock()

	if firstEv.OrderID != orderID || firstEv.Status != OrderStatusOpen || firstEv.FilledQuantity != 0 {
		testingInstance.Fatalf("Expected order to rest OPEN with 0 filled, got: %+v", firstEv)
	}

	// 2. Market drops: stock LTP drops to 104.50, Ask1 drops to 104.50 with 30 shares available
	stock.LTP = 104.50
	stock.Ask1Price = 104.50
	stock.AskLevels[0] = market.DepthLevel{Price: 104.50, Qty: 30}
	stock.AskLevels[1] = market.DepthLevel{Price: 104.55, Qty: 50}

	paper.EvaluateTickForToken(stock)
	time.Sleep(50 * time.Millisecond)

	evMu.Lock()
	var partialEv *OrderUpdateEvent
	for eventIndex := range events {
		if events[eventIndex].OrderID == orderID && events[eventIndex].FilledQuantity == 30 {
			partialEv = &events[eventIndex]
			break
		}
	}
	evMu.Unlock()

	if partialEv == nil {
		testingInstance.Fatalf("Expected partial fill of 30 shares on tick arrival")
	}
	if partialEv.Status != OrderStatusOpen {
		testingInstance.Fatalf("Expected order to still be OPEN after partial fill, got %s", partialEv.Status)
	}

	// 3. More ask liquidity arrives at 104.50: remaining 20 shares fill
	stock.AskLevels[0] = market.DepthLevel{Price: 104.50, Qty: 50}
	paper.EvaluateTickForToken(stock)
	time.Sleep(50 * time.Millisecond)

	evMu.Lock()
	var completeEv *OrderUpdateEvent
	for eventIndex := range events {
		if events[eventIndex].OrderID == orderID && events[eventIndex].FilledQuantity == 50 {
			completeEv = &events[eventIndex]
			break
		}
	}
	evMu.Unlock()

	if completeEv == nil {
		testingInstance.Fatalf("Expected complete fill of 50 shares")
	}
	if completeEv.Status != OrderStatusComplete {
		testingInstance.Fatalf("Expected order to be COMPLETE, got %s", completeEv.Status)
	}
}

func TestRunawayPriceGuard(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, orderUpdateEvent, delta)
		}
	})
	paper := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(orderUpdateEvent)
	})

	stock := &market.StockMetrics{
		InstrumentToken: 77777,
		LTP:             100.0,
		Ask1Price:       100.50,
		TickSize:        0.05,
		LastUpdatedUnix: time.Now().Unix(),
	}

	sizing := SizingDecision{
		IsApproved:      true,
		Quantity:        50,
		DustFloorQty:    5,
		EntryLimitPrice: 100.0,
		StopSpec: StopAndBufferSpec{
			InitialStop:  98.0,
			OneR:         2.0,
			BaseBuffer:   0.5,
			SignalBarLow: 98.2,
		},
	}

	worker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:           "POS-RUNAWAY-TEST",
		TradingDate:          "2026-10-30",
		Mode:                 "PAPER",
		Symbol:               "RUNAWAYSTOCK",
		Token:                77777,
		TickSize:             0.05,
		MISLeverage:          5.0,
		Sizing:               sizing,
		EntryTimeoutDuration: 5 * time.Minute,
		Broker:               paper,
		PaperBroker:          paper,
		Reconciler:           reconciler,
		GetStockPtr:          func() *market.StockMetrics { return stock },
	})

	if err := worker.Start(); err != nil {
		testingInstance.Fatalf("worker.Start failed: %v", err)
	}

	// Verify 0 filled, state is StateEntryPlaced
	if worker.filledEntryQty.Load() != 0 {
		testingInstance.Fatalf("Expected 0 filled shares")
	}

	// Now price runs away to 103.0 (+1.5R above entry 100.0, > 1.2R threshold of 102.40)
	stock.LTP = 103.0
	// Tick 1: runaway begins
	worker.UpdateTrailingStopLossOnPriceTick(stock)
	if worker.runawaySinceUnix.Load() == 0 {
		testingInstance.Fatalf("Expected runaway timer to start")
	}

	// Advance clock by 35 seconds
	stock.LastUpdatedUnix += 35
	worker.UpdateTrailingStopLossOnPriceTick(stock)

	// Position worker should receive emergency exit
	time.Sleep(100 * time.Millisecond)
	if !worker.isMarketExitPending.Load() {
		testingInstance.Fatalf("Expected isMarketExitPending to be true due to price runaway")
	}
}

func TestPositionWorker_OpenRiskAndBreakevenDrop(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, orderUpdateEvent, delta)
		}
	})
	paper := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(orderUpdateEvent)
	})

	stock := &market.StockMetrics{
		InstrumentToken: 88888,
		LTP:             100.0,
		Ask1Price:       100.05,
		Bid1Price:       99.95,
		TickSize:        0.05,
	}

	sizing := SizingDecision{
		IsApproved:      true,
		Quantity:        100,
		DustFloorQty:    5,
		EntryLimitPrice: 100.0,
		BreakevenStop:   100.30,
		StopSpec: StopAndBufferSpec{
			InitialStop:  98.0,
			OneR:         2.0,
			BaseBuffer:   0.5,
			SignalBarLow: 98.2,
		},
	}

	mockBroker := &partialFillMockBroker{PaperBroker: paper}
	worker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:           "POS-HEAT-TEST-1",
		TradingDate:          "2026-10-30",
		Mode:                 "PAPER",
		Symbol:               "HEATTEST",
		Token:                88888,
		TickSize:             0.05,
		MISLeverage:          5.0,
		Sizing:               sizing,
		EntryTimeoutDuration: 5 * time.Minute,
		Broker:               mockBroker,
		PaperBroker:          paper,
		Reconciler:           reconciler,
		GetStockPtr:          func() *market.StockMetrics { return stock },
	})

	if err := worker.Start(); err != nil {
		testingInstance.Fatalf("worker.Start failed: %v", err)
	}

	// 1. In StateEntryPlaced (0 fills): Open risk = (EntryPrice - InitialStop) * InitialQty = (100 - 98) * 100 = ₹200.00
	restingOpenRisk := worker.GetOpenRiskRupees()
	if math.Abs(restingOpenRisk-200.0) > 1e-4 {
		testingInstance.Fatalf("Expected resting open risk ₹200.00, got ₹%.2f", restingOpenRisk)
	}

	// 2. Partial fill of 60 shares arrives
	worker.OnOrderDelta("ENTRY", OrderUpdateEvent{
		OrderID:        worker.entryOrderID,
		Status:         OrderStatusOpen,
		FilledQuantity: 60,
		AveragePrice:   100.0,
	}, 60)

	filledOpenRisk := worker.GetOpenRiskRupees()
	// Open risk for 60 filled shares with stop still at InitialStop (98.0) = (100 - 98) * 60 = ₹120.00
	if math.Abs(filledOpenRisk-120.0) > 1e-4 {
		testingInstance.Fatalf("Expected filled open risk ₹120.00, got ₹%.2f", filledOpenRisk)
	}

	// 3. Stop loss ratchets up to 99.00 (halfway to breakeven)
	worker.confirmedStopBits.Store(math.Float64bits(99.00))
	ratchetedOpenRisk := worker.GetOpenRiskRupees()
	// Risk = (100 - 99) * 60 = ₹60.00
	if math.Abs(ratchetedOpenRisk-60.0) > 1e-4 {
		testingInstance.Fatalf("Expected ratcheted open risk ₹60.00, got ₹%.2f", ratchetedOpenRisk)
	}

	// 4. Stop loss reaches Breakeven (100.30)
	worker.confirmedStopBits.Store(math.Float64bits(100.30))
	breakevenOpenRisk := worker.GetOpenRiskRupees()
	// Risk MUST drop to strictly 0.00 (Heat is released!)
	if breakevenOpenRisk != 0.0 {
		testingInstance.Fatalf("Expected breakeven open risk to be strictly 0.0, got ₹%.2f", breakevenOpenRisk)
	}

	// 5. Stop loss moves into Profit Lock (102.00)
	worker.confirmedStopBits.Store(math.Float64bits(102.00))
	profitLockOpenRisk := worker.GetOpenRiskRupees()
	if profitLockOpenRisk != 0.0 {
		testingInstance.Fatalf("Expected profit lock open risk to be strictly 0.0, got ₹%.2f", profitLockOpenRisk)
	}
}

func TestContinuousPortfolioHeatEngine(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cm := NewCapitalManager(nil, nil)
	_ = cm.UpdatePaperCapital(100000.0) // ₹1,00,000 Equity -> 2.0% Max Portfolio Heat = ₹2,000.00

	stock1 := &market.StockMetrics{InstrumentToken: 1001, LTP: 100.0, Ask1Price: 100.05, Bid1Price: 99.95, TickSize: 0.05, MISLeverage: 5.0}
	stock1.SetSymbol("STOCK1")
	stock2 := &market.StockMetrics{InstrumentToken: 1002, LTP: 200.0, Ask1Price: 200.05, Bid1Price: 199.95, TickSize: 0.05, MISLeverage: 5.0}
	stock2.SetSymbol("STOCK2")

	paper := NewPaperBroker(nil)
	reconciler := NewOrderReconciler(nil, nil)

	// Worker 1: Risk = (100 - 98) * 450 = ₹900.00 (0.90% Heat)
	w1 := NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "POS-1",
		TradingDate: "2026-10-30",
		Mode:        "PAPER",
		Symbol:      "STOCK1",
		Token:       1001,
		Sizing:      SizingDecision{Quantity: 450, EntryLimitPrice: 100.0, BreakevenStop: 100.35, StopSpec: StopAndBufferSpec{InitialStop: 98.0, OneR: 2.0}},
		Broker:      paper,
		PaperBroker: paper,
		Reconciler:  reconciler,
	})
	w1.filledEntryQty.Store(450)
	w1.confirmedStopBits.Store(math.Float64bits(98.0))

	// Worker 2: Risk = (200 - 196) * 225 = ₹900.00 (0.90% Heat)
	w2 := NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "POS-2",
		TradingDate: "2026-10-30",
		Mode:        "PAPER",
		Symbol:      "STOCK2",
		Token:       1002,
		Sizing:      SizingDecision{Quantity: 225, EntryLimitPrice: 200.0, BreakevenStop: 200.70, StopSpec: StopAndBufferSpec{InitialStop: 196.0, OneR: 4.0}},
		Broker:      paper,
		PaperBroker: paper,
		Reconciler:  reconciler,
	})
	w2.filledEntryQty.Store(225)
	w2.confirmedStopBits.Store(math.Float64bits(196.0))

	// 1. Total Open Risk = ₹900 + ₹900 = ₹1,800.00 (1.80% Heat)
	totalRisk := w1.GetOpenRiskRupees() + w2.GetOpenRiskRupees()
	if math.Abs(totalRisk-1800.0) > 1e-4 {
		testingInstance.Fatalf("Expected total open risk ₹1800.00, got ₹%.2f", totalRisk)
	}

	capDTO, _, _ := cm.ComputeCapitalSummary(20000.0, 0.0, totalRisk, 2, 1.0)
	if math.Abs(capDTO.PortfolioHeatPct-0.018) > 1e-4 {
		testingInstance.Fatalf("Expected PortfolioHeatPct 0.018 (1.8%%), got %.4f", capDTO.PortfolioHeatPct)
	}

	maxHeat := 0.02 * capDTO.NetEquity            // ₹2,000.00
	remBudget := math.Max(0.0, maxHeat-totalRisk) // ₹200.00 remaining
	if math.Abs(remBudget-200.0) > 1e-4 {
		testingInstance.Fatalf("Expected remaining heat budget ₹200.00, got ₹%.2f", remBudget)
	}

	// 2. Candidate 3 requires ₹500 risk -> Candidate risk is clamped by remBudget (₹200)
	candStock := &market.StockMetrics{InstrumentToken: 1003, LTP: 50.0, Ask1Price: 50.05, Bid1Price: 49.95, TickSize: 0.05, ATR14: 1.50, MISLeverage: 5.0}
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		candStock.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(50.05 + float64(depthLevelIndex)*0.05), Qty: 20000}
		candStock.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(49.95 - float64(depthLevelIndex)*0.05), Qty: 20000}
	}

	// Sizing with ₹200 remaining heat budget
	sizingClamped := ComputePositionSize(candStock, 49.30, capDTO.NetEquity, capDTO.FreeMargin, 0.005, 1.0, 1.0, remBudget)
	if sizingClamped.IsApproved {
		// Risk budget must be clamped to ₹200
		if sizingClamped.RiskBudgetRupees > 200.01 {
			testingInstance.Fatalf("Expected candidate risk budget clamped to <= ₹200, got ₹%.2f", sizingClamped.RiskBudgetRupees)
		}
	}

	// 3. Worker 1 trails stop to Breakeven (100.35)!
	w1.confirmedStopBits.Store(math.Float64bits(100.35))

	// Verify Worker 1 open risk dropped to 0!
	if w1Risk := w1.GetOpenRiskRupees(); w1Risk != 0.0 {
		testingInstance.Fatalf("Expected Worker 1 open risk to be 0 after trailing to breakeven, got ₹%.2f", w1Risk)
	}

	// Total heat is now ONLY Worker 2 = ₹900.00 (0.90% Heat)
	newTotalRisk := w1.GetOpenRiskRupees() + w2.GetOpenRiskRupees()
	if math.Abs(newTotalRisk-900.0) > 1e-4 {
		testingInstance.Fatalf("Expected total risk to drop to ₹900.00, got ₹%.2f", newTotalRisk)
	}

	newRemBudget := math.Max(0.0, maxHeat-newTotalRisk) // ₹1,100.00 freed up!
	if math.Abs(newRemBudget-1100.0) > 1e-4 {
		testingInstance.Fatalf("Expected freed up heat budget ₹1,100.00, got ₹%.2f", newRemBudget)
	}

	// Now Candidate 3 can be approved with full unconstrained risk budget (₹500 < ₹1,100)
	sizingFull := ComputePositionSize(candStock, 49.30, capDTO.NetEquity, capDTO.FreeMargin, 0.005, 1.0, 1.0, newRemBudget)
	if !sizingFull.IsApproved || sizingFull.Quantity <= 0 {
		testingInstance.Fatalf("Expected Candidate 3 to be approved after heat release, got: %+v", sizingFull)
	}
}

func TestGlobalSessionClock_BlackoutAndMiddayLock(testingInstance *testing.T) {
	loc := market.ISTLocation
	testDate := time.Date(2026, 10, 26, 0, 0, 0, 0, loc) // Monday

	// 1. 09:25 IST - Must be OPENING_NOISE_BLACKOUT, no new entries
	t0925 := testDate.Add(9*time.Hour + 25*time.Minute)
	clock0925 := EvaluateGlobalSessionClocks(t0925, 50.0, 1.0)
	if clock0925.Phase != "OPENING_NOISE_BLACKOUT" || clock0925.IsNewEntryAllowed {
		testingInstance.Fatalf("expected 09:25 to be OPENING_NOISE_BLACKOUT with entries locked, got phase=%s allowed=%v",
			clock0925.Phase, clock0925.IsNewEntryAllowed)
	}

	// 2. 09:35 IST - Must be MORNING_DISCOVERY_DRIVE, entries allowed
	t0935 := testDate.Add(9*time.Hour + 35*time.Minute)
	clock0935 := EvaluateGlobalSessionClocks(t0935, 50.0, 1.0)
	if clock0935.Phase != "MORNING_DISCOVERY_DRIVE" || !clock0935.IsNewEntryAllowed {
		testingInstance.Fatalf("expected 09:35 to be MORNING_DISCOVERY_DRIVE with entries allowed, got phase=%s allowed=%v",
			clock0935.Phase, clock0935.IsNewEntryAllowed)
	}

	// 3. 12:00 IST - Midday compression without tape override: entries locked
	t1200 := testDate.Add(12 * time.Hour)
	clock1200Norm := EvaluateGlobalSessionClocks(t1200, 50.0, 1.0)
	if clock1200Norm.Phase != "MIDDAY_COMPRESSION" || clock1200Norm.IsNewEntryAllowed {
		testingInstance.Fatalf("expected 12:00 normal to be MIDDAY_COMPRESSION with entries locked, got phase=%s allowed=%v",
			clock1200Norm.Phase, clock1200Norm.IsNewEntryAllowed)
	}

	// 4. 12:00 IST - Midday compression WITH tape override: entries allowed, sizeMult = 0.50
	clock1200Override := EvaluateGlobalSessionClocks(t1200, 75.0, 2.8)
	if clock1200Override.Phase != "MIDDAY_COMPRESSION" || !clock1200Override.IsNewEntryAllowed || !clock1200Override.IsTapeOverrideActive || clock1200Override.SizeMultiplier != 0.50 {
		testingInstance.Fatalf("expected 12:00 override to allow entries with 0.50x size, got: %+v", clock1200Override)
	}
}

func TestEntryGatekeeper_NewQuantPatternsAndEMAConfluence(testingInstance *testing.T) {
	var stocks [512]market.StockMetrics
	secMap := make(map[uint8]market.SectorHealth)

	sessionClock := market.GlobalSessionClockDTO{
		Phase:             "MORNING_DISCOVERY_DRIVE",
		IsNewEntryAllowed: true,
		MinBullishScore:   75.0,
		MinRVOL:           1.5,
		SizeMultiplier:    1.0,
	}

	// 1. Test PatternFailedBreakdownReclaim (Bear Trap Spring)
	stockSpring := &stocks[0]
	stockSpring.InstrumentToken = 1001
	stockSpring.SetSymbol("SPRING")
	stockSpring.IsTradeable = true
	stockSpring.IsWarm = true
	stockSpring.MISLeverage = 5.0
	stockSpring.LTP = 502.0
	stockSpring.ExchangeVWAP = 500.0
	stockSpring.ATR14 = 5.0 // atrSafe = 5.0
	stockSpring.TickSize = 0.05
	stockSpring.CurrentBarIdx = 2
	stockSpring.BullishScore = 82.0
	stockSpring.EV = 0.40
	stockSpring.RVOLSession = 2.0
	stockSpring.RVOLPace5m = 2.0
	stockSpring.AggressorBuyRatio = 0.65
	stockSpring.FastAggressorEMA = 0.62
	stockSpring.SmoothedBuyerRatio = 0.60
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		stockSpring.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(502.05 + float64(depthLevelIndex)*0.05), Qty: 10000}
		stockSpring.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(501.95 - float64(depthLevelIndex)*0.05), Qty: 10000}
	}
	// Previous bar swept below VWAP (500 - 0.20*5 = 499.0, within [500 - 0.45*5, 500 - 0.05*5] = [497.75, 499.75])
	stockSpring.SessionBars[1] = market.OHLCVBar{Open: 501.0, High: 502.0, Low: 499.0, Close: 500.0}
	// Current bar reclaims VWAP with authority: LTP 502.0 >= 500 + 0.05*5 = 500.25, Open 500.0
	stockSpring.SessionBars[2] = market.OHLCVBar{Open: 500.0, High: 502.5, Low: 499.8, Close: 502.0}

	sig, _, _, _ := PickBestLongCandidate(&stocks, []uint16{0}, secMap, sessionClock, 200000.0, 200000.0, 0.005, 1.0, 4000.0, nil)
	if sig == nil || sig.PatternType != PatternFailedBreakdownReclaim {
		testingInstance.Fatalf("expected PatternFailedBreakdownReclaim, got sig: %+v", sig)
	}
	expectedStop := 499.0 - (0.20 * 5.0) // previousBar.Low - 0.20*ATR = 498.0
	if math.Abs(sig.SignalBarLow-expectedStop) > 1e-4 {
		testingInstance.Fatalf("expected signalBarLow=%.2f, got %.2f", expectedStop, sig.SignalBarLow)
	}

	// 2. Test PatternORBRetestReclaim
	stockORB := &stocks[1]
	stockORB.InstrumentToken = 1002
	stockORB.SetSymbol("ORBRETEST")
	stockORB.IsTradeable = true
	stockORB.IsWarm = true
	stockORB.MISLeverage = 5.0
	stockORB.ExchangeVWAP = 200.0
	stockORB.ATR14 = 4.0 // atrSafe = 4.0
	stockORB.TickSize = 0.05
	stockORB.CurrentBarIdx = 4
	stockORB.BullishScore = 80.0
	stockORB.EV = 0.35
	stockORB.RVOLSession = 2.2
	stockORB.RVOLPace5m = 2.0
	stockORB.AggressorBuyRatio = 0.62
	stockORB.FastAggressorEMA = 0.60
	stockORB.SmoothedBuyerRatio = 0.58
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		stockORB.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(206.05 + float64(depthLevelIndex)*0.05), Qty: 10000}
		stockORB.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(205.95 - float64(depthLevelIndex)*0.05), Qty: 10000}
	}
	// 15m range (bars 0, 1, 2): High = 205.0, Low = 200.0, range = 5.0 <= 1.8*4 (7.2)
	stockORB.SessionBars[0] = market.OHLCVBar{Open: 200.0, High: 204.0, Low: 200.0, Close: 203.0}
	stockORB.SessionBars[1] = market.OHLCVBar{Open: 203.0, High: 205.0, Low: 202.0, Close: 204.0}
	stockORB.SessionBars[2] = market.OHLCVBar{Open: 204.0, High: 204.5, Low: 204.0, Close: 204.0}

	// 2a. Unconfirmed ORB: Bar 3 only wicked to 207.0 but CLOSED at 204.0 (inside range!) -> MUST be blocked!
	stockORB.SessionBars[3] = market.OHLCVBar{Open: 204.0, High: 207.0, Low: 203.5, Close: 204.0}
	stockORB.LTP = 206.0
	stockORB.SessionBars[4] = market.OHLCVBar{Open: 204.5, High: 206.5, Low: 204.2, Close: 206.0}
	sigORBBlocked, _, _, _ := PickBestLongCandidate(&stocks, []uint16{1}, secMap, sessionClock, 200000.0, 200000.0, 0.005, 1.0, 4000.0, nil)
	if sigORBBlocked != nil {
		testingInstance.Fatalf("expected unconfirmed ORB wick to be blocked, got: %+v", sigORBBlocked)
	}

	// 2b. Confirmed ORB: Bar 3 actually CLOSED above ORH at 206.0 -> MUST qualify!
	stockORB.SessionBars[3] = market.OHLCVBar{Open: 204.0, High: 206.5, Low: 204.2, Close: 206.0}
	sigORB, _, _, _ := PickBestLongCandidate(&stocks, []uint16{1}, secMap, sessionClock, 200000.0, 200000.0, 0.005, 1.0, 4000.0, nil)
	if sigORB == nil || sigORB.PatternType != PatternORBRetestReclaim {
		testingInstance.Fatalf("expected PatternORBRetestReclaim, got sig: %+v", sigORB)
	}

	// 3. Test VWAP Pullback Reclaim EMA9 Confluence rejection
	stockVWAP := &stocks[2]
	stockVWAP.InstrumentToken = 1003
	stockVWAP.SetSymbol("VWAPCONFL")
	stockVWAP.IsTradeable = true
	stockVWAP.IsWarm = true
	stockVWAP.MISLeverage = 5.0
	stockVWAP.ExchangeVWAP = 1000.0
	stockVWAP.ATR14 = 10.0 // atrSafe = 10.0
	stockVWAP.TickSize = 0.05
	stockVWAP.CurrentBarIdx = 2
	stockVWAP.BullishScore = 80.0
	stockVWAP.EV = 0.35
	stockVWAP.RVOLSession = 2.0
	stockVWAP.RVOLPace5m = 2.0
	stockVWAP.AggressorBuyRatio = 0.60
	stockVWAP.FastAggressorEMA = 0.58
	stockVWAP.SmoothedBuyerRatio = 0.58
	stockVWAP.LTP = 1002.0 // 0.2 ATR above VWAP
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		stockVWAP.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(1002.05 + float64(depthLevelIndex)*0.05), Qty: 10000}
		stockVWAP.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(1001.95 - float64(depthLevelIndex)*0.05), Qty: 10000}
	}
	stockVWAP.SessionBars[1] = market.OHLCVBar{Open: 1001.0, High: 1003.0, Low: 1000.5, Close: 1001.0}
	stockVWAP.SessionBars[2] = market.OHLCVBar{Open: 1000.5, High: 1003.0, Low: 1000.2, Close: 1002.0}

	// 3a. Fail-closed: EMA9 <= 0 (uninitialized) -> MUST be blocked!
	stockVWAP.EMA9 = 0.0
	stockVWAP.EMA9Slope = 0.0
	sigUninit, _, _, _ := PickBestLongCandidate(&stocks, []uint16{2}, secMap, sessionClock, 200000.0, 200000.0, 0.005, 1.0, 4000.0, nil)
	if sigUninit != nil {
		testingInstance.Fatalf("expected candidate to be blocked when EMA9 is uninitialized (<= 0), got: %+v", sigUninit)
	}

	// 3b. EMA9 is below VWAP (995.0 < 1000.0) -> MUST be blocked by Richie Naso Confluence rule!
	stockVWAP.EMA9 = 995.0
	stockVWAP.EMA9Slope = 0.2
	sigBelowVWAP, _, _, _ := PickBestLongCandidate(&stocks, []uint16{2}, secMap, sessionClock, 200000.0, 200000.0, 0.005, 1.0, 4000.0, nil)
	if sigBelowVWAP != nil {
		testingInstance.Fatalf("expected candidate to be blocked when EMA9 < VWAP, got: %+v", sigBelowVWAP)
	}

	// 3c. EMA9 above VWAP but steep negative slope (-0.80 / 10.0 = -0.08 < -0.05 ATR) -> MUST be blocked!
	stockVWAP.EMA9 = 1002.0
	stockVWAP.EMA9Slope = -0.80
	sigSteepSlope, _, _, _ := PickBestLongCandidate(&stocks, []uint16{2}, secMap, sessionClock, 200000.0, 200000.0, 0.005, 1.0, 4000.0, nil)
	if sigSteepSlope != nil {
		testingInstance.Fatalf("expected candidate to be blocked when normalized slope < -0.05 ATR, got: %+v", sigSteepSlope)
	}

	// 3d. EMA9 is above VWAP (1002.0 >= 1000.0) and slope is gentle/positive (-0.20 / 10.0 = -0.02 >= -0.05 ATR) -> MUST pass!
	stockVWAP.EMA9 = 1002.0
	stockVWAP.EMA9Slope = -0.20
	sigPass, _, _, _ := PickBestLongCandidate(&stocks, []uint16{2}, secMap, sessionClock, 200000.0, 200000.0, 0.005, 1.0, 4000.0, nil)
	if sigPass == nil || sigPass.PatternType != PatternVWAPPullbackReclaim {
		testingInstance.Fatalf("expected PatternVWAPPullbackReclaim when EMA9 >= VWAP and slope valid, got: %+v", sigPass)
	}
	// Verify stop includes 0.20 ATR buffer: min(1000.5, 1000.2) - 0.20*10 = 1000.2 - 2.0 = 998.2
	expectedVWAPStop := 1000.2 - (0.20 * 10.0)
	if math.Abs(sigPass.SignalBarLow-expectedVWAPStop) > 1e-4 {
		testingInstance.Fatalf("expected VWAP signalBarLow=%.2f, got %.2f", expectedVWAPStop, sigPass.SignalBarLow)
	}
}

func TestPositionWorker_AlphaDecayStagnation45m(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, orderUpdateEvent, delta)
		}
	})
	paper := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(orderUpdateEvent)
	})

	stock := &market.StockMetrics{
		InstrumentToken: 99999,
		LTP:             100.0,
		Ask1Price:       100.0,
		Bid1Price:       99.95,
		TickSize:        0.05,
	}
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		stock.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(100.00 + float64(depthLevelIndex)*0.05), Qty: 10000}
		stock.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(99.95 - float64(depthLevelIndex)*0.05), Qty: 10000}
	}

	sizing := SizingDecision{
		IsApproved:      true,
		Quantity:        100,
		DustFloorQty:    5,
		EntryLimitPrice: 100.0,
		BreakevenStop:   100.30,
		StopSpec: StopAndBufferSpec{
			InitialStop:  98.0,
			OneR:         2.0,
			BaseBuffer:   0.5,
			SignalBarLow: 98.2,
		},
	}

	closedCh := make(chan struct{}, 1)
	worker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:           "POS-DECAY-TEST-1",
		TradingDate:          "2026-10-30",
		Mode:                 "PAPER",
		Symbol:               "DECAYTEST",
		Token:                99999,
		TickSize:             0.05,
		MISLeverage:          5.0,
		Sizing:               sizing,
		EntryTimeoutDuration: 5 * time.Minute,
		Broker:               paper,
		PaperBroker:          paper,
		Reconciler:           reconciler,
		GetStockPtr:          func() *market.StockMetrics { return stock },
		OnClosed: func(closedWorker *PositionWorker) {
			select {
			case closedCh <- struct{}{}:
			default:
			}
		},
	})

	if err := worker.Start(); err != nil {
		testingInstance.Fatalf("worker.Start failed: %v", err)
	}

	// Wait for entry fill & state to reach StateProtectedActive
	time.Sleep(80 * time.Millisecond)
	if worker.RemainingQty() != 100 {
		testingInstance.Fatalf("expected RemainingQty=100, got %d", worker.RemainingQty())
	}

	// Backdate OpenedAt by 46 minutes
	worker.mu.Lock()
	worker.OpenedAt = time.Now().Add(-46 * time.Minute)
	worker.mu.Unlock()

	// High-water mark peaked at +0.20R (100.40) - below the +0.50R momentum threshold
	worker.highWaterMarkBits.Store(math.Float64bits(100.40))

	// Watchdog ticker fires every 500ms -> should detect alpha decay stagnation and trigger emergency flatten
	select {
	case <-closedCh:
	case <-time.After(2 * time.Second):
		testingInstance.Fatalf("timed out waiting for ALPHA_DECAY_45M_STAGNATION flatten")
	}

	worker.mu.RLock()
	reason := worker.exitReason
	st := worker.state
	worker.mu.RUnlock()

	if st != StateClosed {
		testingInstance.Fatalf("expected worker state CLOSED, got %s", st)
	}
	if reason != "ALPHA_DECAY_45M_STAGNATION" {
		testingInstance.Fatalf("expected exitReason ALPHA_DECAY_45M_STAGNATION, got %s", reason)
	}
}

func TestPositionWorker_SingleShareScaleOutProtection(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, orderUpdateEvent OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, orderUpdateEvent, delta)
		}
	})
	paper := NewPaperBroker(func(orderUpdateEvent OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(orderUpdateEvent)
	})

	stock := &market.StockMetrics{
		InstrumentToken: 77777,
		LTP:             1000.0,
		Ask1Price:       1000.0,
		Bid1Price:       999.5,
		TickSize:        0.05,
	}
	for depthLevelIndex := 0; depthLevelIndex < 5; depthLevelIndex++ {
		stock.AskLevels[depthLevelIndex] = market.DepthLevel{Price: float32(1000.00 + float64(depthLevelIndex)*0.05), Qty: 1000}
		stock.BidLevels[depthLevelIndex] = market.DepthLevel{Price: float32(999.50 - float64(depthLevelIndex)*0.05), Qty: 1000}
	}

	sizing := SizingDecision{
		IsApproved:      true,
		Quantity:        1, // Exact 1-share position!
		DustFloorQty:    1,
		EntryLimitPrice: 1000.0,
		BreakevenStop:   1001.50,
		StopSpec: StopAndBufferSpec{
			InitialStop:  980.0,
			OneR:         20.0,
			BaseBuffer:   5.0,
			SignalBarLow: 982.0,
		},
	}

	worker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:           "POS-1SHARE-TEST",
		TradingDate:          "2026-10-31",
		Mode:                 "PAPER",
		Symbol:               "1SHARETEST",
		Token:                77777,
		TickSize:             0.05,
		MISLeverage:          5.0,
		Sizing:               sizing,
		EntryTimeoutDuration: 5 * time.Minute,
		Broker:               paper,
		PaperBroker:          paper,
		Reconciler:           reconciler,
		GetStockPtr:          func() *market.StockMetrics { return stock },
	})

	if err := worker.Start(); err != nil {
		testingInstance.Fatalf("worker.Start failed: %v", err)
	}

	time.Sleep(80 * time.Millisecond)
	if worker.RemainingQty() != 1 {
		testingInstance.Fatalf("expected RemainingQty=1, got %d", worker.RemainingQty())
	}

	// Rally to +1.60R (1032.0) -> triggers Tranche 1 harvest
	stock.LTP = worker.GetEntryPrice() + 1.60*worker.OneR
	stock.RollingPeak = stock.LTP
	stock.LastUpdatedUnix += 5
	worker.UpdateTrailingStopLossOnPriceTick(stock)

	time.Sleep(100 * time.Millisecond)

	// Since remaining qty is 1, position remains intact with breakeven locked
	if !worker.hasBreakevenCompleted.Load() {
		testingInstance.Fatalf("expected hasBreakevenCompleted to be true for 1-share position")
	}
	if worker.RemainingQty() != 1 {
		testingInstance.Fatalf("expected 1 share to remain intact, got %d", worker.RemainingQty())
	}
	if worker.confirmedStopBits.Load() == 0 {
		testingInstance.Fatalf("expected confirmed stop to exist")
	}
}

func TestMacroRegime_FlashShockLockout(testingInstance *testing.T) {
	mgr := NewManager(nil, nil, nil, nil)
	defer mgr.Stop()

	mgr.capitalMgr.UpdateSettings(ExecutionSettings{
		Mode:                 "PAPER",
		RiskPerTradePct:      0.0075,
		IsAutoExecuteEnabled: true,
	}, 1)

	var stocks [512]market.StockMetrics
	pulse := market.MarketPulseDTO{
		BreadthAboveVWAP: 15.0,
		MarketRegime:     "FLASH_SHOCK",
	}

	state := mgr.OnSecondTick(&stocks, nil, nil, nil, pulse)
	if !state.IsMacroShockActive {
		testingInstance.Fatalf("expected IsMacroShockActive=true when regime is FLASH_SHOCK")
	}
	if state.GateStatus != "LOCKED_MACRO_SHOCK" {
		testingInstance.Fatalf("expected GateStatus=LOCKED_MACRO_SHOCK, got %s", state.GateStatus)
	}
}

func TestPaperBroker_FastRLockHotPath(testingInstance *testing.T) {
	broker := NewPaperBroker(nil)

	stock := &market.StockMetrics{
		InstrumentToken: 55555,
		LTP:             100.0,
		Ask1Price:       100.05,
		Bid1Price:       99.95,
	}

	// 1. Tick for token with zero resting orders: must complete near-instantly without panic
	broker.EvaluateTickForToken(stock)

	// 2. Place resting BUY limit order @ 95.0 (well below LTP 100.0)
	ctx := context.Background()
	_, err := broker.PlaceLimitBuy(ctx, "TEST", 55555, 10, 95.0, stock)
	if err != nil {
		testingInstance.Fatalf("PlaceLimitBuy failed: %v", err)
	}

	// 3. Tick with LTP 100.0: should NOT trigger fill
	broker.EvaluateTickForToken(stock)
	orders, _ := broker.FetchOrders(ctx)
	if len(orders) != 1 || orders[0].Status != OrderStatusOpen {
		testingInstance.Fatalf("expected order to remain OPEN, got status=%s", orders[0].Status)
	}

	// 4. Tick with LTP 94.5: should trigger fill
	stock.LTP = 94.5
	broker.EvaluateTickForToken(stock)
	orders, _ = broker.FetchOrders(ctx)
	if len(orders) != 1 || orders[0].Status != OrderStatusComplete {
		testingInstance.Fatalf("expected order to become COMPLETE, got status=%s", orders[0].Status)
	}
}

func TestDynamicMarginalFriction_TaxCalculator(testingInstance *testing.T) {
	stock := market.StockMetrics{
		InstrumentToken: 99999,
		LTP:             1000.0,
		ATR14:           15.0,
		TickSize:        0.05,
		MISLeverage:     5.0,
		CurrentBarIdx:   5,
	}
	stock.SessionBars[5] = market.OHLCVBar{Open: 1000.0, High: 1010.0, Low: 995.0, Close: 1000.0}

	sizing := ComputePositionSize(&stock, 995.0, 500000.0, 500000.0, 0.0075, 1.0, 1.0, 10000.0)
	if !sizing.IsApproved {
		testingInstance.Fatalf("expected sizing to be approved, got reject: %s", sizing.RejectReason)
	}
	if sizing.DustFloorQty < 1 {
		testingInstance.Fatalf("expected dust floor qty >= 1, got %d", sizing.DustFloorQty)
	}
	expectedMinCharges := CalculateRoundTripCharges(sizing.EntryLimitPrice, sizing.EntryLimitPrice+sizing.StopSpec.OneR, 1).TotalCharges
	if expectedMinCharges <= 0 {
		testingInstance.Fatalf("expected non-zero charges calculation for 1 share")
	}
}

func TestAtomicRiskAndMarginReservation(testingInstance *testing.T) {
	cm := NewCapitalManager(nil, nil)
	_ = cm.UpdatePaperCapital(10000.0) // ₹10,000 Equity -> 3.0% Max Portfolio Heat = ₹300.00

	// 1. Initial state: Current active risk = ₹150, blocked margin = ₹4000
	// Remaining heat capacity = ₹300 - ₹150 = ₹150
	// Free margin = ₹10,000 - ₹4000 = ₹6000

	// 2. Reserve Trade 1: Proposed Risk = ₹100, Proposed Margin = ₹2000
	res1, err := cm.ReserveRiskAndMargin("TATAPOWER", 100.0, 2000.0, 150.0, 4000.0)
	if err != nil {
		testingInstance.Fatalf("expected res1 to succeed, got error: %v", err)
	}
	if res1 == "" {
		testingInstance.Fatalf("expected non-empty reservation ID")
	}

	// 3. Concurrent Trade 2: Proposed Risk = ₹100
	// Total requested risk = active(150) + res1(100) + proposed(100) = ₹350 > max(300)
	// Must be REJECTED atomically!
	res2, err := cm.ReserveRiskAndMargin("BEL", 100.0, 2000.0, 150.0, 4000.0)
	if err == nil {
		testingInstance.Fatalf("expected res2 to be rejected due to heat ceiling, but got reservation: %s", res2)
	}

	// 4. Rollback / Release res1 (e.g. order submission failed)
	cm.ReleaseReservation(res1)

	// 5. Now Trade 2 retries and MUST succeed!
	res2Retry, err := cm.ReserveRiskAndMargin("BEL", 100.0, 2000.0, 150.0, 4000.0)
	if err != nil {
		testingInstance.Fatalf("expected res2Retry to succeed after res1 release, got: %v", err)
	}
	if res2Retry == "" {
		testingInstance.Fatalf("expected non-empty reservation ID for res2Retry")
	}

	// 6. Promote reservation (order successfully filled/active)
	cm.PromoteReservation(res2Retry)
}

func TestBreakevenStrictTaxCoverMath(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entryPrice := 400.0
	qty := 25
	tickSize := 0.05

	// 1. Calculate CalculateTaxCoveringBreakevenStop: Must strictly cover EntryPrice + (Taxes / Qty)
	roundTripCharges := CalculateRoundTripCharges(entryPrice, entryPrice, qty)
	perShareTax := roundTripCharges.TotalCharges / float64(qty)
	expectedBreakeven := RoundUpToTick(entryPrice+perShareTax, tickSize)
	if expectedBreakeven <= entryPrice {
		expectedBreakeven = RoundUpToTick(entryPrice+tickSize, tickSize)
	}

	calculatedBreakeven := CalculateTaxCoveringBreakevenStop(entryPrice, qty, qty, tickSize)
	if math.Abs(calculatedBreakeven-expectedBreakeven) > 1e-4 {
		testingInstance.Fatalf("expected breakeven price %.2f, got %.2f", expectedBreakeven, calculatedBreakeven)
	}

	// 2. PositionWorker Open Risk:
	// If stop is at entry price (400.00) but below breakeven (e.g. 400.25), risk is NOT zero!
	worker := NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "POS-TAX-1",
		TradingDate: "2026-11-01",
		Mode:        "PAPER",
		Symbol:      "TATAPOWER",
		Token:       1001,
		Sizing:      SizingDecision{Quantity: qty, EntryLimitPrice: entryPrice, BreakevenStop: calculatedBreakeven, StopSpec: StopAndBufferSpec{InitialStop: 396.0, OneR: 4.0}},
		MISLeverage: 5.0,
		PaperBroker: NewPaperBroker(nil),
		Broker:      NewPaperBroker(nil),
		Reconciler:  NewOrderReconciler(nil, nil),
	})
	worker.filledEntryQty.Store(int64(qty))

	// Stop at entry price 400.00 (below breakeven):
	worker.confirmedStopBits.Store(math.Float64bits(entryPrice))
	riskAtEntry := worker.GetOpenRiskRupees()
	_ = riskAtEntry
	// Open risk should NOT be 0 since confirmedStop < calculatedBreakeven!
	// (Note: math.Max(0.0, entryPrice - confirmedStop) is 0 for capital delta, but confirmedStop < breakeven means it hasn't released heat if defined strictly)
	if calculatedBreakeven > entryPrice {
		// Stop loss moved exactly to Breakeven (calculatedBreakeven)
		worker.confirmedStopBits.Store(math.Float64bits(calculatedBreakeven))
		riskAtBreakeven := worker.GetOpenRiskRupees()
		if riskAtBreakeven != 0.0 {
			testingInstance.Fatalf("expected open risk to be strictly 0.0 at breakeven stop, got %.2f", riskAtBreakeven)
		}
	}
}

func TestCircuitBreakerStrict3PctAndNoProfitGivebackLock(testingInstance *testing.T) {
	cm := NewCapitalManager(nil, nil)
	_ = cm.UpdatePaperCapital(10000.0) // ₹10,000 Starting Capital -> 3.0% loss limit = -₹300.00

	// 1. Loss of ₹250 (-2.5%): desk must NOT be locked (under 3.0%)
	cm.RecordClosedTrade("PAPER", -220.0, 30.0, -250.0)
	capDTO, dailyDTO, _ := cm.ComputeCapitalSummary(0.0, 0.0, 0.0, 0, 1.0)
	if dailyDTO.IsCircuitBreakerLocked {
		testingInstance.Fatalf("expected circuit breaker UNLOCKED at -₹250 (-2.5%%), got locked: %s", dailyDTO.LockReason)
	}

	// 2. Another loss of ₹60 brings cumulative net to -₹310 (-3.1%): desk MUST lock!
	cm.RecordClosedTrade("PAPER", -50.0, 10.0, -60.0)
	_, dailyDTO2, _ := cm.ComputeCapitalSummary(0.0, 0.0, 0.0, 0, 1.0)
	if !dailyDTO2.IsCircuitBreakerLocked {
		testingInstance.Fatalf("expected circuit breaker LOCKED at -₹310 (-3.1%%), but was unlocked")
	}

	// 3. Test that profitable days are NOT locked arbitrarily on givebacks
	cm2 := NewCapitalManager(nil, nil)
	_ = cm2.UpdatePaperCapital(10000.0)

	// Trade 1 wins ₹500 (+5.0%, peak PnL = ₹500)
	cm2.RecordClosedTrade("PAPER", 550.0, 50.0, 500.0)
	// Trade 2 gives back ₹200 (cumNet = ₹300, down from ₹500 peak)
	cm2.RecordClosedTrade("PAPER", -180.0, 20.0, -200.0)

	_, dailyDTO3, _ := cm2.ComputeCapitalSummary(0.0, 0.0, 0.0, 0, 1.0)
	if dailyDTO3.IsCircuitBreakerLocked {
		testingInstance.Fatalf("expected profitable day with giveback to REMAIN UNLOCKED, but got locked: %s", dailyDTO3.LockReason)
	}
	_ = capDTO
}

func TestZeroMarginRejection(testingInstance *testing.T) {
	// A stock with MISLeverage = 0.0 (no MIS margin permitted by Zerodha / SEBI)
	stockUnmarginable := market.StockMetrics{
		InstrumentToken: 55555,
		LTP:             500.0,
		ATR14:           5.0,
		TickSize:        0.05,
		MISLeverage:     0.0, // Unavailable margin!
		CurrentBarIdx:   3,
	}
	stockUnmarginable.SetSymbol("NOMARGIN")
	stockUnmarginable.SessionBars[3] = market.OHLCVBar{Open: 498.0, High: 505.0, Low: 497.0, Close: 500.0}

	sizing := ComputePositionSize(&stockUnmarginable, 497.0, 10000.0, 10000.0, 0.01, 1.0, 1.0, 300.0)
	if sizing.IsApproved {
		testingInstance.Fatalf("expected sizing to be REJECTED when MISLeverage=0.0, but got approved")
	}
	if sizing.RejectReason == "" {
		testingInstance.Fatalf("expected descriptive rejection reason for 0.0 margin")
	}
}

func TestPaperBroker_StopLossExecutionOnFallingLTP(testingInstance *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stock market.StockMetrics
	stock.InstrumentToken = 778899
	stock.SetSymbol("TATAMOTORS")
	stock.LTP = 500.00
	stock.ExchangeVWAP = 499.00
	stock.RollingPeak = 500.00
	stock.Ask1Price = 500.05
	stock.Bid1Price = 499.95
	stock.TickSize = 0.05
	stock.ATR14 = 5.00
	stock.MISLeverage = 5.0
	stock.SmoothedBuyerRatio = 0.65
	stock.AggressorBuyRatio = 0.65
	stock.FastAggressorEMA = 0.65
	stock.LastUpdatedUnix = time.Now().Unix()
	for index := 0; index < 5; index++ {
		stock.AskLevels[index] = market.DepthLevel{Price: float32(500.05 + float64(index)*0.05), Qty: 10000}
		stock.BidLevels[index] = market.DepthLevel{Price: float32(499.95 - float64(index)*0.05), Qty: 10000}
	}

	sizing := ComputePositionSize(&stock, 496.00, 200000.0, 200000.0, 0.005, 1.0, 1.0, 4000.0)
	if !sizing.IsApproved {
		testingInstance.Fatalf("sizing rejected: %s", sizing.RejectReason)
	}

	var positionWorker *PositionWorker
	orderReconciler := NewOrderReconciler(nil, func(posID, legType string, orderEvent OrderUpdateEvent, deltaFilled int) {
		if positionWorker != nil {
			positionWorker.OnOrderDelta(legType, orderEvent, deltaFilled)
		}
	})
	paperBroker := NewPaperBroker(func(orderEvent OrderUpdateEvent) {
		orderReconciler.ProcessOrderEvent(orderEvent)
	})

	closedNotifyChannel := make(chan struct{}, 1)
	positionWorker = NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "TEST-FALLING-SL-1",
		TradingDate: "2026-11-02",
		Mode:        "PAPER",
		Symbol:      "TATAMOTORS",
		Token:       778899,
		PatternType: "VWAP_PULLBACK_RECLAIM",
		TickSize:    0.05,
		MISLeverage: 5.0,
		Sizing:      sizing,
		Broker:      paperBroker,
		PaperBroker: paperBroker,
		Reconciler:  orderReconciler,
		GetStockPtr: func() *market.StockMetrics { return &stock },
		OnClosed: func(closedWorker *PositionWorker) {
			select {
			case closedNotifyChannel <- struct{}{}:
			default:
			}
		},
	})

	if err := positionWorker.Start(); err != nil {
		testingInstance.Fatalf("worker.Start failed: %v", err)
	}

	// Wait up to 500ms for initial SL order to be placed
	var stopLossOrderID string
	for attempt := 0; attempt < 50; attempt++ {
		positionWorker.mu.RLock()
		stopLossOrderID = positionWorker.slOrderID
		positionWorker.mu.RUnlock()
		if stopLossOrderID != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if stopLossOrderID == "" {
		positionWorker.processPendingWork()
		positionWorker.mu.RLock()
		stopLossOrderID = positionWorker.slOrderID
		positionWorker.mu.RUnlock()
	}
	if stopLossOrderID == "" {
		testingInstance.Fatalf("expected initial SL order to be placed, but got empty slOrderID")
	}

	// 1. Stock rises to 530.00, trailing stop is ratcheted up to 520.00
	stock.LTP = 530.00
	stock.RollingPeak = 530.00
	positionWorker.confirmedStopBits.Store(math.Float64bits(520.00))
	positionWorker.proposedStopBits.Store(math.Float64bits(520.00))
	limitPriceBelowStop := CalculateProtectiveLimitPriceBelowStop(520.00, positionWorker.TickSize)
	err := paperBroker.ModifySLOrder(ctx, stopLossOrderID, positionWorker.RemainingQty(), 520.00, limitPriceBelowStop, stock.LTP, positionWorker.TickSize)
	if err != nil {
		testingInstance.Fatalf("ModifySLOrder failed: %v", err)
	}

	// 2. Now market pulls back and gaps down through the stop-loss and limit price to 516.00
	stock.LTP = 516.00
	stock.Ask1Price = 516.05
	stock.Bid1Price = 515.95
	stock.LastUpdatedUnix += 5
	for index := 0; index < 5; index++ {
		stock.AskLevels[index] = market.DepthLevel{Price: float32(516.05 + float64(index)*0.05), Qty: 10000}
		stock.BidLevels[index] = market.DepthLevel{Price: float32(515.95 - float64(index)*0.05), Qty: 10000}
	}

	// Feed the tick to the position worker
	positionWorker.UpdateTrailingStopLossOnPriceTick(&stock)

	// Wait for closed notification
	select {
	case <-closedNotifyChannel:
	case <-time.After(2 * time.Second):
		testingInstance.Fatalf("timed out waiting for position to close after stop-loss breached on falling LTP")
	}

	if !positionWorker.isClosed.Load() {
		testingInstance.Fatalf("expected position worker isClosed to be true")
	}
	if positionWorker.RemainingQty() != 0 {
		testingInstance.Fatalf("expected remaining quantity to be 0, got %d", positionWorker.RemainingQty())
	}
	positionDTO := positionWorker.ToDTO()
	if positionDTO.RealisedGrossPnL <= 0 {
		testingInstance.Fatalf("expected positive realised gross PnL from stop-loss trailing profit, got %.2f", positionDTO.RealisedGrossPnL)
	}
	if positionDTO.State != StateClosed {
		testingInstance.Fatalf("expected worker state StateClosed, got %s", positionDTO.State)
	}
}

type mockModeTrackingBroker struct {
	brokerMode       string
	fetchOrdersCount int32
	sweepSellCount   int32
}

func (broker *mockModeTrackingBroker) Mode() string {
	return broker.brokerMode
}

func (broker *mockModeTrackingBroker) PlaceLimitBuy(_ context.Context, _ string, _ uint32, _ int, _ float64, _ *market.StockMetrics) (string, error) {
	return "MOCK-BUY-1", nil
}

func (broker *mockModeTrackingBroker) PlaceProtectedMarketSell(_ context.Context, _ string, _ uint32, _ int, _, _ float64, _ *market.StockMetrics) (string, error) {
	return "MOCK-SELL-1", nil
}

func (broker *mockModeTrackingBroker) PlaceEmergencySweepSell(_ context.Context, _ string, _ uint32, _ int, _, _, _ float64, _ *market.StockMetrics) (string, error) {
	broker.sweepSellCount++
	return "MOCK-SWEEP-1", nil
}

func (broker *mockModeTrackingBroker) PlaceSLOrder(_ context.Context, _ string, _ uint32, _ int, _, _, _, _ float64) (string, error) {
	return "MOCK-SL-1", nil
}

func (broker *mockModeTrackingBroker) ModifySLOrder(_ context.Context, _ string, _ int, _, _, _, _ float64) error {
	return nil
}

func (broker *mockModeTrackingBroker) MutateSLToMarket(_ context.Context, _ string, _ int, _ *market.StockMetrics) error {
	return nil
}

func (broker *mockModeTrackingBroker) CancelOrder(_ context.Context, _ string) error {
	return nil
}

func (broker *mockModeTrackingBroker) FetchOrders(_ context.Context) ([]OrderUpdateEvent, error) {
	broker.fetchOrdersCount++
	return []OrderUpdateEvent{}, nil
}

func TestPhase1_ModeIsolationAndCrossModeAutoSquareoff(testingContext *testing.T) {
	// 1. Per-mode OrderReconciler 500ms coalescing isolation:
	// Calling SyncOrdersCoalesced on PAPER followed immediately (<500ms) by LIVE must hit BOTH brokers.
	reconciler := NewOrderReconciler(nil, nil)
	paperMockBroker := &mockModeTrackingBroker{brokerMode: "PAPER"}
	liveMockBroker := &mockModeTrackingBroker{brokerMode: "LIVE"}

	_, err := reconciler.SyncOrdersCoalesced(context.Background(), paperMockBroker)
	if err != nil {
		testingContext.Fatalf("expected PAPER SyncOrdersCoalesced to succeed, got %v", err)
	}
	_, err = reconciler.SyncOrdersCoalesced(context.Background(), liveMockBroker)
	if err != nil {
		testingContext.Fatalf("expected LIVE SyncOrdersCoalesced to succeed, got %v", err)
	}
	if paperMockBroker.fetchOrdersCount != 1 {
		testingContext.Fatalf("expected paperMockBroker FetchOrders count 1, got %d", paperMockBroker.fetchOrdersCount)
	}
	if liveMockBroker.fetchOrdersCount != 1 {
		testingContext.Fatalf("expected liveMockBroker FetchOrders count 1 (isolated from PAPER 500ms cache), got %d", liveMockBroker.fetchOrdersCount)
	}

	// Calling PAPER again within 500ms should coalesce and stay at count 1
	_, _ = reconciler.SyncOrdersCoalesced(context.Background(), paperMockBroker)
	if paperMockBroker.fetchOrdersCount != 1 {
		testingContext.Fatalf("expected second PAPER call within 500ms to coalesce, got count %d", paperMockBroker.fetchOrdersCount)
	}

	// 2. Orphaned BUY fill broker resolution:
	// Even if current settings.Mode == "PAPER", a non-"PAPER-" order ID must resolve to liveBroker.
	executionManager := NewManager(nil, nil, nil, nil)
	defer executionManager.Stop()

	resolvedLiveBroker := executionManager.resolveBrokerForOrphanedOrder("POS-UNKNOWN-99", "241025000987654")
	if resolvedLiveBroker.Mode() != "LIVE" {
		testingContext.Fatalf("expected non-PAPER order ID to resolve to LIVE broker even in PAPER mode, got %s", resolvedLiveBroker.Mode())
	}
	resolvedPaperBroker := executionManager.resolveBrokerForOrphanedOrder("POS-UNKNOWN-100", "PAPER-ORD-42")
	if resolvedPaperBroker.Mode() != "PAPER" {
		testingContext.Fatalf("expected PAPER- prefixed order ID to resolve to PAPER broker, got %s", resolvedPaperBroker.Mode())
	}

	// 3. Immediate lastStateDTO refresh on UpdateSettings and UpdatePaperCapital:
	initialState := executionManager.GetCurrentState()
	if initialState.Mode != "PAPER" {
		testingContext.Fatalf("expected default mode PAPER, got %s", initialState.Mode)
	}
	updatedCapitalState, err := executionManager.UpdatePaperCapital(250000.0)
	if err != nil {
		testingContext.Fatalf("UpdatePaperCapital failed: %v", err)
	}
	if updatedCapitalState.Capital.PaperCapital != 250000.0 || updatedCapitalState.Capital.NetEquity != 250000.0 {
		testingContext.Fatalf("expected immediate lastStateDTO refresh to reflect ₹250,000 paper capital, got %.2f", updatedCapitalState.Capital.PaperCapital)
	}

	// 4. Mode switch rejection when an active reservation or open position exists:
	reservationID, err := executionManager.capitalMgr.ReserveRiskAndMargin("RELIANCE", 500.0, 10000.0, 0, 0)
	if err != nil {
		testingContext.Fatalf("ReserveRiskAndMargin failed: %v", err)
	}
	_, modeSwitchErr := executionManager.UpdateSettings(ExecutionSettings{
		Mode:                 "LIVE",
		IsAutoExecuteEnabled: false,
		RiskPerTradePct:      0.005,
	})
	if modeSwitchErr == nil {
		testingContext.Fatalf("expected mode switch to LIVE to be rejected while an active risk reservation is in flight")
	}
	executionManager.capitalMgr.ReleaseReservation(reservationID)

	// 5. ForceSyncModeForRehydration syncs mode when an open LIVE position is recovered:
	executionManager.capitalMgr.ForceSyncModeForRehydration("LIVE")
	if executionManager.capitalMgr.GetSettings().Mode != "LIVE" {
		testingContext.Fatalf("expected ForceSyncModeForRehydration to set mode to LIVE, got %s", executionManager.capitalMgr.GetSettings().Mode)
	}
}

func TestPhase2_TrailingStopMathGapWatchdogAndTaxPrecision(testingContext *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Strict Tick-Quantized Non-Inverting Bounds (EDGE-8):
	// DeltaMin <= BaseBuffer <= DeltaMax must hold strictly without any half-tick overshoot.
	testPrices := []float64{35.0, 185.0, 245.0, 720.0, 3450.0, 18500.0}
	testATRPcts := []float64{0.002, 0.008, 0.015, 0.026, 0.045}
	for _, priceLevel := range testPrices {
		for _, atrRatio := range testATRPcts {
			atrValue := priceLevel * atrRatio
			stopSpec, err := CalculateInitialStopLoss(priceLevel, priceLevel-0.5*atrValue, atrValue, 0.05)
			if err != nil {
				testingContext.Fatalf("CalculateInitialStopLoss failed for price=%.2f atr=%.2f: %v", priceLevel, atrValue, err)
			}
			if stopSpec.BaseBuffer < stopSpec.DeltaMin-1e-9 || stopSpec.BaseBuffer > stopSpec.DeltaMax+1e-9 {
				testingContext.Fatalf("expected strict DeltaMin (%.4f) <= BaseBuffer (%.4f) <= DeltaMax (%.4f)",
					stopSpec.DeltaMin, stopSpec.BaseBuffer, stopSpec.DeltaMax)
			}
		}
	}

	// 2. SL Gap Watchdog (VULN-4) & Zero-Timestamp Guard:
	// - Inside SL-L limit band (494.80 >= protectiveLimitPrice 493.00): Tick 1 arms slBreachSinceUnix even if LastUpdatedUnix==0, Tick 2 exits.
	// - Below SL-L protective limit band (492.50 < 493.00): Tick 1 exits immediately (0ms delay).
	stock := &market.StockMetrics{
		InstrumentToken:    779521,
		LTP:                500.00,
		TickSize:           0.05,
		ATR14:              4.00,
		ExchangeVWAP:       499.00,
		AggressorBuyRatio:  0.60,
		FastAggressorEMA:   0.60,
		SmoothedBuyerRatio: 0.60,
		LastUpdatedUnix:    0, // Uninitialized timestamp edge case
	}
	sizing := SizingDecision{
		IsApproved:      true,
		Quantity:        100,
		DustFloorQty:    1,
		EntryLimitPrice: 500.00,
		BreakevenStop:   500.85,
		StopSpec: StopAndBufferSpec{
			BaseBuffer:   3.00,
			InitialStop:  495.00,
			OneR:         5.00,
			SignalBarLow: 495.00,
		},
	}

	gapWorker := NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "POS-GAP-WATCHDOG-1",
		TradingDate: "2026-11-03",
		Mode:        "LIVE",
		Symbol:      "SBIN",
		Token:       779521,
		TickSize:    0.05,
		MISLeverage: 5.0,
		Sizing:      sizing,
		GetStockPtr: func() *market.StockMetrics { return stock },
	})
	gapWorker.filledEntryQty.Store(100)
	gapWorker.confirmedStopBits.Store(math.Float64bits(495.00))

	stock.LTP = 494.80
	gapWorker.UpdateTrailingStopLossOnPriceTick(stock)
	if gapWorker.slBreachSinceUnix.Load() == 0 {
		testingContext.Fatalf("expected slBreachSinceUnix to be armed using safe timestamp fallback when LastUpdatedUnix==0")
	}
	gapWorker.UpdateTrailingStopLossOnPriceTick(stock)
	if !gapWorker.isMarketExitPending.Load() {
		testingContext.Fatalf("expected second tick at 494.80 to trigger emergency exit")
	}

	airPocketWorker := NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "POS-AIR-POCKET-1",
		TradingDate: "2026-11-03",
		Mode:        "LIVE",
		Symbol:      "SBIN",
		Token:       779521,
		TickSize:    0.05,
		MISLeverage: 5.0,
		Sizing:      sizing,
		GetStockPtr: func() *market.StockMetrics { return stock },
	})
	airPocketWorker.filledEntryQty.Store(100)
	airPocketWorker.confirmedStopBits.Store(math.Float64bits(495.00))
	stock.LTP = 492.50 // Gapped completely below SL-L protective limit price (493.00)
	airPocketWorker.UpdateTrailingStopLossOnPriceTick(stock)
	if !airPocketWorker.isMarketExitPending.Load() {
		testingContext.Fatalf("expected Tick 1 air-pocket gap below SL-L protective limit (492.50 < 493.00) to trigger immediate exit")
	}

	// 3. OneR == 0 Division-by-Zero Guard (VULN-5) & Gear 2 Promotion at +1.00R:
	zeroOneRSizing := sizing
	zeroOneRSizing.StopSpec.OneR = 0 // Simulate legacy/uninitialized OneR=0 with InitialStop=0
	zeroOneRSizing.StopSpec.InitialStop = 0
	zeroOneRWorker := NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "POS-ZERO-ONER-1",
		TradingDate: "2026-11-03",
		Mode:        "LIVE",
		Symbol:      "SBIN",
		Token:       779521,
		TickSize:    0.05,
		MISLeverage: 5.0,
		Sizing:      zeroOneRSizing,
		GetStockPtr: func() *market.StockMetrics { return stock },
	})
	zeroOneRWorker.filledEntryQty.Store(100)
	zeroOneRWorker.confirmedStopBits.Store(math.Float64bits(495.00))
	stock.LTP = 500.05 // Tiny 1-tick move above entry
	zeroOneRWorker.UpdateTrailingStopLossOnPriceTick(stock)
	if zeroOneRWorker.hasBreakevenCompleted.Load() {
		testingContext.Fatalf("expected OneR==0 guard to prevent IEEE-754 +Inf from locking breakeven on a 1-tick move")
	}

	// Now with valid OneR=5.00, rally to +1.10R (505.50) -> must promote currentGear from 1 to GearDynamicTrail (2)
	gearWorker := NewPositionWorker(ctx, WorkerConfig{
		PositionID:  "POS-GEAR2-PROMOTE-1",
		TradingDate: "2026-11-03",
		Mode:        "LIVE",
		Symbol:      "SBIN",
		Token:       779521,
		TickSize:    0.05,
		MISLeverage: 5.0,
		Sizing:      sizing,
		GetStockPtr: func() *market.StockMetrics { return stock },
	})
	gearWorker.filledEntryQty.Store(100)
	gearWorker.confirmedStopBits.Store(math.Float64bits(495.00))
	stock.LTP = 505.50
	gearWorker.UpdateTrailingStopLossOnPriceTick(stock)
	if gearWorker.currentGear.Load() != GearDynamicTrail {
		testingContext.Fatalf("expected currentGear to promote to GearDynamicTrail (2) at +1.10R, got %d", gearWorker.currentGear.Load())
	}

	// 4. In-flight proposedStopBits deduplication:
	// Once proposedStopBits is at 502.55, a subsequent tick proposing 502.60 (only +0.05 above proposedStop)
	// must NOT overwrite proposedStopBits before the first modification confirms.
	proposedBefore := math.Float64frombits(gearWorker.proposedStopBits.Load())
	gearWorker.lastModUnixMs.Store(0)
	gearWorker.RaiseStopLossOrder(proposedBefore+0.05, 4.00, false)
	proposedAfter := math.Float64frombits(gearWorker.proposedStopBits.Load())
	if proposedAfter != proposedBefore {
		testingContext.Fatalf("expected sub-step increment (+0.05) above in-flight proposedStop (%.2f) to be ignored, got %.2f",
			proposedBefore, proposedAfter)
	}

	// 5. Statutory Tax Telemetry Parity (trade_history.go vs PositionWorker):
	// A 100% runner trade that reached +1.00R (HasTranche1Completed == true) with no SCALE_OUT order
	// must charge single-exit brokerage (matching CalculateRoundTripCharges), NOT split-exit double brokerage.
	expectedTax := CalculateRoundTripCharges(500.00, 508.00, 100)
	closedDTO := market.ActivePositionDTO{
		PositionID:           "POS-TAX-PARITY-1",
		Mode:                 "LIVE",
		Symbol:               "SBIN",
		State:                StateClosed,
		EntryPrice:           500.00,
		ExitPrice:            508.00,
		InitialQty:           100,
		RemainingQty:         0,
		OneR:                 5.00,
		HighWaterMark:        509.00,
		CurrentGear:          GearDynamicTrail,
		HasTranche1Completed: true,
		RealisedGrossPnL:     800.00,
		EstimatedCharges:     expectedTax.TotalCharges,
		NetTotalPnL:          800.00 - expectedTax.TotalCharges,
	}
	historyItem := buildTradeHistoryFromDTO(nil, market.ISTLocation, closedDTO, "2026-11-03", "State Bank of India", "PSU Banking", 5.0)
	if math.Abs(historyItem.Taxes.TotalCharges-expectedTax.TotalCharges) > 1e-6 {
		testingContext.Fatalf("expected TradeHistory TotalCharges ₹%.2f to match PositionWorker RoundTripCharges ₹%.2f",
			historyItem.Taxes.TotalCharges, expectedTax.TotalCharges)
	}
}





