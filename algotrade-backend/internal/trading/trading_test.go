package trading

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/ajaynegi33/AlgoTrade/internal/database"
	"github.com/ajaynegi33/AlgoTrade/internal/market"
)

func TestStockMetricsAlignmentAndPhase2Math(t *testing.T) {
	// 1. Verify strict 8-byte alignment of StockMetrics
	sz := unsafe.Sizeof(market.StockMetrics{})
	if sz%8 != 0 {
		t.Fatalf("StockMetrics size %d is not a multiple of 8 bytes", sz)
	}
	if sz != 3992 {
		t.Fatalf("expected StockMetrics size 3992 bytes, got %d", sz)
	}

	// 2. Verify non-inverting DeltaMin < DeltaMax across all price tiers & extreme ATRs
	prices := []float64{35.0, 185.0, 245.0, 720.0, 3450.0, 18500.0}
	atrPcts := []float64{0.002, 0.008, 0.015, 0.026, 0.045} // Up to 4.5% 5m ATR shock!

	for _, px := range prices {
		for _, atrPct := range atrPcts {
			atr := px * atrPct
			spec, err := ComputeStopAndBufferSpec(px, px-0.5*atr, atr, 0.05)
			if err != nil {
				t.Fatalf("unexpected error for px=%.2f atr=%.2f: %v", px, atr, err)
			}
			if spec.DeltaMin >= spec.DeltaMax {
				t.Fatalf("DeltaMin (%.4f) >= DeltaMax (%.4f) for px=%.2f atrPct=%.3f", spec.DeltaMin, spec.DeltaMax, px, atrPct)
			}
			if spec.BaseBuffer < spec.DeltaMin-0.05 || spec.BaseBuffer > spec.DeltaMax+0.05 {
				t.Fatalf("BaseBuffer %.2f out of bounds [%.2f, %.2f]", spec.BaseBuffer, spec.DeltaMin, spec.DeltaMax)
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
	for i := 0; i < 5; i++ {
		stock.AskLevels[i] = market.DepthLevel{Price: float32(780.05 + float64(i)*0.05), Qty: 50000}
		stock.BidLevels[i] = market.DepthLevel{Price: float32(779.95 - float64(i)*0.05), Qty: 50000}
	}

	// ₹1,00,000 account
	s100k := ComputePositionSize(&stock, 775.00, 100000.0, 100000.0, 0.005, 1.0, 1.0)
	if !s100k.Approved || s100k.Quantity <= 0 {
		t.Fatalf("expected ₹1L sizing to be approved, got: %+v", s100k)
	}

	// ₹10 Crore account (must be capped by 20% top-3 ask liquidity = 30,000 shares)
	s10Cr := ComputePositionSize(&stock, 775.00, 100000000.0, 100000000.0, 0.005, 1.0, 1.0)
	if !s10Cr.Approved || s10Cr.Quantity != 30000 {
		t.Fatalf("expected ₹10Cr sizing to be liquidity-capped at 30000 shares, got qty=%d", s10Cr.Quantity)
	}
}

func TestPositionWorkerThreeStepScaleOutAndShadowStopMod25(t *testing.T) {
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
	for i := 0; i < 5; i++ {
		stock.AskLevels[i] = market.DepthLevel{Price: float32(220.05 + float64(i)*0.05), Qty: 10000}
		stock.BidLevels[i] = market.DepthLevel{Price: float32(219.95 - float64(i)*0.05), Qty: 10000}
	}

	sizing := ComputePositionSize(&stock, 218.40, 200000.0, 200000.0, 0.005, 1.0, 1.0)
	if !sizing.Approved {
		t.Fatalf("sizing rejected: %s", sizing.RejectReason)
	}

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, ev OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, ev, delta)
		}
	})
	paper := NewPaperBroker(func(ev OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(ev)
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
		OnClosed: func(w *PositionWorker) {
			select {
			case closedCh <- struct{}{}:
			default:
			}
		},
	})

	if err := worker.Start(); err != nil {
		t.Fatalf("worker.Start failed: %v", err)
	}

	// Wait for entry fill & initial SL placement
	time.Sleep(80 * time.Millisecond)
	if worker.RemainingQty() != sizing.Quantity {
		t.Fatalf("expected RemainingQty=%d, got %d", sizing.Quantity, worker.RemainingQty())
	}

	// Simulate rally to +1.15R -> triggers 3-Step Scale-Out (50% sold, runner SL moved to Breakeven)
	stock.LTP = worker.GetEntryPrice() + 1.15*worker.OneR
	stock.RollingPeak = stock.LTP
	stock.LastUpdatedUnix += 5
	worker.OnTickHotPath(&stock, false)

	time.Sleep(120 * time.Millisecond)
	if !worker.tranche1Done.Load() {
		t.Fatalf("expected tranche1Done == true after +1.15R")
	}
	expectedRunner := sizing.Quantity - (sizing.Quantity / 2)
	if worker.RemainingQty() != expectedRunner {
		t.Fatalf("expected runner qty %d, got %d", expectedRunner, worker.RemainingQty())
	}

	// Simulate Gear 4 (modCount == 24) and rally to +3.0R -> Virtual Shadow Stop ratchets in RAM
	worker.modCount.Store(24)
	stock.LTP = worker.GetEntryPrice() + 3.0*worker.OneR
	stock.RollingPeak = stock.LTP
	stock.LastUpdatedUnix += 25
	worker.OnTickHotPath(&stock, false)

	shadow := worker.ToDTO().ShadowStop
	if shadow <= worker.GetBreakevenStop() {
		t.Fatalf("expected ShadowStop (%.2f) > BreakevenStop (%.2f) at +3R", shadow, worker.GetBreakevenStop())
	}

	// Trigger Shadow Stop breach -> fires Modification #25 (MutateSLToMarket) and closes position
	stock.LTP = shadow - 0.15
	stock.FastAggressorEMA = 0.30
	stock.LastUpdatedUnix += 2
	worker.OnTickHotPath(&stock, false)

	select {
	case <-closedCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for Mod 25 Shadow Stop flatten")
	}

	dto := worker.ToDTO()
	if dto.State != StateClosed || dto.RemainingQty != 0 {
		t.Fatalf("expected CLOSED with RemainingQty=0, got state=%s rem=%d", dto.State, dto.RemainingQty)
	}
	if dto.NetTotalPnL <= 0 {
		t.Fatalf("expected positive NetTotalPnL after +1R scale-out and +3R runner exit, got ₹%.2f", dto.NetTotalPnL)
	}
}

func TestReconcilerEarlyEventAndCrashRehydrationIdempotency(t *testing.T) {
	var totalDeltas int
	r := NewOrderReconciler(nil, func(posID, legType string, ev OrderUpdateEvent, delta int) {
		totalDeltas += delta
	})

	// 1. Falsification Bug #10: WebSocket/Paper COMPLETE event arrives BEFORE RegisterOrder is called
	r.ProcessOrderEvent(OrderUpdateEvent{
		OrderID:        "ORD-EARLY-1",
		Status:         OrderStatusComplete,
		Quantity:       100,
		FilledQuantity: 100,
		AveragePrice:   220.05,
	})
	if totalDeltas != 0 {
		t.Fatalf("expected 0 deltas before RegisterOrder, got %d", totalDeltas)
	}

	// RegisterOrder must replay the buffered early event immediately
	r.RegisterOrder("ORD-EARLY-1", "POS-1", "ENTRY", "BEL", "BUY", "LIMIT", 100, 220.10, 0, OrderStatusOpen)
	if totalDeltas != 100 {
		t.Fatalf("expected 100 filled shares replayed upon RegisterOrder, got %d", totalDeltas)
	}

	// 2. Falsification Bug #11: Crash-recovery SeedOrderFilled must prevent duplicate fill counting on subsequent sync
	r.SeedOrderFilled("ORD-CRASH-2", 250)
	r.RegisterOrder("ORD-CRASH-2", "POS-2", "ENTRY", "SBIN", "BUY", "LIMIT", 250, 780.00, 0, OrderStatusComplete)
	r.ProcessOrderEvent(OrderUpdateEvent{
		OrderID:        "ORD-CRASH-2",
		Status:         OrderStatusComplete,
		Quantity:       250,
		FilledQuantity: 250,
		AveragePrice:   780.00,
	})
	if totalDeltas != 100 {
		t.Fatalf("expected SeedOrderFilled to suppress duplicate delta for ORD-CRASH-2, got totalDeltas=%d", totalDeltas)
	}
}

func TestPhase26FalsificationSuite_BugsB1ToB5(t *testing.T) {
	// 1. Bug B1 Falsification: Empty checksum in LIVE mode must be rejected by HandleKiteWebhookPostback
	mgr := NewManager(nil, nil, nil, nil)
	defer mgr.Stop()
	_, err := mgr.UpdateSettings(ExecutionSettings{
		Mode:             "LIVE",
		AutoExecute:      false,
		RiskPerTradePct:  0.005,
		MaxOpenPositions: 4,
	})
	if err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}
	err = mgr.HandleKiteWebhookPostback(OrderUpdateEvent{
		OrderID:        "FORGED-ORD-1",
		Status:         OrderStatusComplete,
		Quantity:       100,
		FilledQuantity: 100,
		Checksum:       "", // Empty checksum attack
	})
	if err == nil {
		t.Fatalf("Bug B1 Falsified: expected HandleKiteWebhookPostback to reject empty checksum in LIVE mode")
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
	for i := 0; i < 5; i++ {
		bel.AskLevels[i] = market.DepthLevel{Price: float32(219.95 + float64(i)*0.05), Qty: 15000}
		bel.BidLevels[i] = market.DepthLevel{Price: float32(219.90 - float64(i)*0.05), Qty: 15000}
	}

	s10k := ComputePositionSize(&bel, 218.40, 10000.0, 10000.0, 0.005, 0.65, 1.0)
	if !s10k.Approved || s10k.Quantity < 25 {
		t.Fatalf("Bug B5 Falsified: expected ₹10k capital w/ 5x MIS to approve meaningful quantity even in midday compression, got: %+v", s10k)
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
	scoreWeakFlow, _, _, _ := market.CalculateBullishScore(&bel, nil, nil, true, 0.65)

	bel.SmoothedBuyerRatio = 0.78
	bel.AggressorBuyRatio = 0.82
	bel.FastAggressorEMA = 0.84
	scoreStrongFlow, _, _, _ := market.CalculateBullishScore(&bel, nil, nil, true, 0.65)

	if scoreStrongFlow-scoreWeakFlow < 12.0 {
		t.Fatalf("Bug B4 Falsified: expected strong institutional flow to boost BullishScore by >= 12 pts, got weak=%.2f strong=%.2f",
			scoreWeakFlow, scoreStrongFlow)
	}

	// 4. Bug B3 Falsification: Pre-1R Thesis-Decay & Absorption Failure must tighten stop above InitialStop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var worker *PositionWorker
	reconciler := NewOrderReconciler(nil, func(posID, legType string, ev OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, ev, delta)
		}
	})
	paper := NewPaperBroker(func(ev OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(ev)
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
		t.Fatalf("worker.Start failed: %v", err)
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
	worker.OnTickHotPath(&bel, false)
	time.Sleep(60 * time.Millisecond)

	tightenedStop := worker.ToDTO().CurrentStop
	if tightenedStop <= initialStop+0.20 {
		t.Fatalf("Bug B3 Falsified: expected Pre-1R thesis decay to tighten stop significantly above InitialStop (%.2f), got %.2f",
			initialStop, tightenedStop)
	}
	if tightenedStop >= bel.LTP-0.10 {
		t.Fatalf("Bug B3 Anti-Suicide Guard Falsified: tightenedStop (%.2f) too close to LTP (%.2f)", tightenedStop, bel.LTP)
	}
}

func TestPhase27_TradeHistoryAndTaxBreakdown(t *testing.T) {
	// 1. Verify exact 6-component statutory tax breakdown for a 500-share SBIN trade (Buy ₹800, Sell ₹812, split exit)
	tb := CalculatePositionTaxBreakdown(800.00, 812.00, 500, true)
	totalBrok := tb.BuyBrokerage + tb.SellBrokerage
	// Buy brokerage capped at ₹20, split sell brokerage capped at ₹40 -> ₹60 total brokerage
	if totalBrok != 60.0 {
		t.Fatalf("expected ₹60.00 brokerage for split exit, got %.2f", totalBrok)
	}
	// STT = 0.025% of ₹4,06,000 = ₹101.50 -> rounded ₹102.00
	if tb.STT != 102.0 {
		t.Fatalf("expected ₹102.00 STT, got %.2f", tb.STT)
	}
	// Stamp Duty = 0.003% of ₹4,00,000 = ₹12.00
	if tb.StampDuty != 12.0 {
		t.Fatalf("expected ₹12.00 Stamp Duty, got %.2f", tb.StampDuty)
	}
	if tb.TotalCharges <= 200.0 || tb.TotalCharges >= 250.0 {
		t.Fatalf("unexpected TotalCharges: %.2f", tb.TotalCharges)
	}

	// 2. Verify GetTradeHistory returns enriched item & summary
	mgr := NewManager(nil, nil, nil, nil)
	defer mgr.Stop()

	mgr.mu.Lock()
	mgr.closedPositions = append(mgr.closedPositions, market.ActivePositionDTO{
		PositionID:       "POS-TEST-HIST-1",
		Mode:             "PAPER",
		Symbol:           "SBIN",
		InstrumentToken:  779521,
		State:            StateClosed,
		PatternType:      "VWAP_PULLBACK",
		EntryPrice:       800.00,
		ExitPrice:        810.00,
		LTP:              810.00,
		InitialStop:      795.00,
		CurrentStop:      808.00,
		BreakevenStop:    801.20,
		HighWaterMark:    812.00,
		OneR:             5.00,
		RMultiple:        2.00,
		MFERMultiple:     2.40,
		InitialQty:       100,
		RemainingQty:     0,
		ModCount:         12,
		MaxMods:          25,
		CurrentGear:      3,
		Tranche1Done:     true,
		RealisedGrossPnL: 1000.00,
		EstimatedCharges: CalculatePositionTaxBreakdown(800.00, 810.00, 100, true).TotalCharges,
		ExitReason:       "SL_HIT (Trail)",
		OpenedAt:         "2026-10-25 09:45:00",
		ClosedAt:         "2026-10-25 10:15:00",
	})
	mgr.mu.Unlock()

	hist := mgr.GetTradeHistory("ALL", "", "", 100)
	if len(hist.Trades) != 1 {
		t.Fatalf("expected 1 trade in history, got %d", len(hist.Trades))
	}
	item := hist.Trades[0]
	if item.Symbol != "SBIN" || item.Quantity != 100 || item.BuyPrice != 800.00 || item.SellPrice != 810.00 {
		t.Fatalf("unexpected trade item fields: %+v", item)
	}
	if item.TotalInvested != 80000.00 || item.MarginBlocked != 16000.00 || item.GrossPnL != 1000.00 {
		t.Fatalf("unexpected capital/pnl metrics: %+v", item)
	}
	if hist.Summary.TotalTrades != 1 || hist.Summary.Wins != 1 || hist.Summary.RealisedGrossPnL != 1000.00 {
		t.Fatalf("unexpected summary metrics: %+v", hist.Summary)
	}
}

func TestTradeHistory_NoDeadlockOnMaxConns(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_hist_deadlock.db")
	db, err := database.Init(tempDB)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
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
		t.Fatalf("failed to insert test data: %v", err)
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
			t.Fatalf("expected at least 1 trade from SQLite, got %d", len(hist.Trades))
		}
		if hist.Trades[0].Symbol != "BPCL" || len(hist.Trades[0].Orders) != 2 {
			t.Fatalf("expected BPCL with 2 order legs, got symbol=%s, orders=%d", hist.Trades[0].Symbol, len(hist.Trades[0].Orders))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DEADLOCK DETECTED: GetTradeHistory did not complete within 2s")
	}
}

func TestActivePositionsStableEntryOrder(t *testing.T) {
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
	for i := 0; i < 25; i++ {
		// Update prices on each tick to ensure price volatility doesn't perturb order
		wA.latestLTPBits.Store(math.Float64bits(1500.0 + float64(i)*0.5))
		wB.latestLTPBits.Store(math.Float64bits(3500.0 - float64(i)*1.0))
		wC.latestLTPBits.Store(math.Float64bits(2500.0 + float64(i)*2.0))

		state := mgr.OnSecondTick(&stocks, nil, nil, nil, pulse, false)
		if len(state.ActivePositions) != 3 {
			t.Fatalf("iteration %d: expected 3 active positions, got %d", i, len(state.ActivePositions))
		}

		// Pos 0 MUST be wA (opened at 09:30:00)
		if state.ActivePositions[0].PositionID != "POS-INFY-1" {
			t.Fatalf("iteration %d: position 0 changed! expected POS-INFY-1, got %s", i, state.ActivePositions[0].PositionID)
		}
		// Pos 1 MUST be wB (opened at 09:32:00)
		if state.ActivePositions[1].PositionID != "POS-TCS-2" {
			t.Fatalf("iteration %d: position 1 changed! expected POS-TCS-2, got %s", i, state.ActivePositions[1].PositionID)
		}
		// Pos 2 MUST be wC (opened at 09:35:00)
		if state.ActivePositions[2].PositionID != "POS-RELIANCE-3" {
			t.Fatalf("iteration %d: position 2 changed! expected POS-RELIANCE-3, got %s", i, state.ActivePositions[2].PositionID)
		}

		if state.ActivePositions[0].OpenedAtUnix >= state.ActivePositions[1].OpenedAtUnix ||
			state.ActivePositions[1].OpenedAtUnix >= state.ActivePositions[2].OpenedAtUnix {
			t.Fatalf("iteration %d: OpenedAtUnix timestamps out of order: %+v", i, state.ActivePositions)
		}
	}
}

func TestSLGapThroughBreachAndFlatten(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stock market.StockMetrics
	stock.InstrumentToken = 2010113
	stock.SetSymbol("INOXWIND")
	stock.LTP = 59.50
	stock.TickSize = 0.05
	stock.MISLeverage = 5.0
	for i := 0; i < 5; i++ {
		stock.AskLevels[i] = market.DepthLevel{Price: float32(59.55 + float64(i)*0.05), Qty: 50000}
		stock.BidLevels[i] = market.DepthLevel{Price: float32(59.45 - float64(i)*0.05), Qty: 50000}
	}

	sizing := SizingDecision{
		Approved:        true,
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
	reconciler := NewOrderReconciler(nil, func(posID, legType string, ev OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, ev, delta)
		}
	})
	paper := NewPaperBroker(func(ev OrderUpdateEvent) {
		reconciler.ProcessOrderEvent(ev)
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
		OnClosed: func(w *PositionWorker) {
			select {
			case closedCh <- struct{}{}:
			default:
			}
		},
	})

	if err := worker.Start(); err != nil {
		t.Fatalf("worker.Start failed: %v", err)
	}

	// Wait for entry fill & initial SL placement
	time.Sleep(100 * time.Millisecond)
	if worker.RemainingQty() != 132 {
		t.Fatalf("expected RemainingQty=132, got %d", worker.RemainingQty())
	}

	// Now simulate price falling directly to 58.81 (below initial stop 58.83)
	stock.LTP = 58.81
	stock.LastUpdatedUnix += 5
	worker.OnTickHotPath(&stock, false)

	select {
	case <-closedCh:
		t.Logf("Closed successfully! State: %s", worker.ToDTO().State)
	case <-time.After(2 * time.Second):
		dto := worker.ToDTO()
		t.Fatalf("timed out waiting for position to close! Current state: %s, exitReason: %s, remaining: %d, modCount: %d",
			dto.State, dto.ExitReason, dto.RemainingQty, dto.ModCount)
	}
}

func TestPaperBrokerRehydrationAndTrigger(t *testing.T) {
	var triggeredEvent OrderUpdateEvent
	eventCh := make(chan OrderUpdateEvent, 1)

	broker := NewPaperBroker(func(ev OrderUpdateEvent) {
		eventCh <- ev
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
			t.Fatalf("expected triggered order to be COMPLETE, got %s", triggeredEvent.Status)
		}
		if triggeredEvent.FilledQuantity != 112 {
			t.Fatalf("expected 112 filled, got %d", triggeredEvent.FilledQuantity)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for rehydrated paper order to trigger")
	}
}

func TestRehydratedEmergencyFlattenForceExit(t *testing.T) {
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
	broker := NewPaperBroker(func(ev OrderUpdateEvent) {
		if reconciler != nil {
			reconciler.ProcessOrderEvent(ev)
		}
	})

	var worker *PositionWorker
	reconciler = NewOrderReconciler(nil, func(posID, legType string, ev OrderUpdateEvent, delta int) {
		if worker != nil {
			worker.OnOrderDelta(legType, ev, delta)
		}
	})

	sizing := SizingDecision{
		Approved:        true,
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
		OnClosed: func(w *PositionWorker) {
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
			t.Fatalf("expected state CLOSED, got %s", worker.ToDTO().State)
		}
		if worker.RemainingQty() != 0 {
			t.Fatalf("expected RemainingQty 0, got %d", worker.RemainingQty())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for forced emergency flatten to close position")
	}
}

func TestPositionWorker_EarlyHarvestAt065RAnd5mClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stock := &market.StockMetrics{
		InstrumentToken: 779521,
		LTP:             800.00,
		TickSize:        0.05,
		ATR14:           4.00,
		ExchangeVWAP:    798.00,
		AggressorBuyRatio: 0.65,
		FastAggressorEMA:  0.62,
		SmoothedBuyerRatio: 0.60,
		CurrentBarIdx:   2,
	}
	stock.SessionBars[0] = market.OHLCVBar{High: 798.0, Low: 794.0, Open: 795.0, Close: 797.0}
	stock.SessionBars[1] = market.OHLCVBar{High: 801.0, Low: 796.0, Open: 797.0, Close: 800.0}
	stock.SessionBars[2] = market.OHLCVBar{High: 804.0, Low: 799.0, Open: 800.0, Close: 803.0}

	sizing := SizingDecision{
		Approved:        true,
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

	// 1. Tick at 802.60 (+0.52R): verify stop ratchets to Breakeven (800.80)
	stock.LTP = 802.60
	worker.OnTickHotPath(stock, false)
	shadow := math.Float64frombits(worker.shadowStopBits.Load())
	if shadow < 800.80 {
		t.Fatalf("expected shadow stop to ratchet to Breakeven 800.80 at +0.52R, got %.2f", shadow)
	}

	// 2. Tick at 803.30 (+0.66R): verify Tranche 1 50% scale-out is triggered
	stock.LTP = 803.30
	worker.OnTickHotPath(stock, false)
	if !worker.tranche1Pending.Load() {
		t.Fatalf("expected tranche1Pending to be TRUE at +0.66R for early profit harvest, got false")
	}

	// 3. Intra-candle dip does NOT panic-dump if 5m bar hasn't closed below InitialStop
	stock.LTP = 796.00
	worker.OnTickHotPath(stock, false)
	if worker.isClosed.Load() {
		t.Fatalf("expected position to hold during intra-candle wick above InitialStop (795.00), but was closed prematurely")
	}

	// 4. Completed 5m candle close below InitialStop triggers structural exit
	stock.CurrentBarIdx = 3
	stock.SessionBars[2] = market.OHLCVBar{High: 798.0, Low: 793.0, Open: 797.0, Close: 794.0} // Closed below 795.00!
	stock.LTP = 794.00
	worker.OnTickHotPath(stock, false)
	if !worker.pendingMarketExit.Load() {
		t.Fatalf("expected pendingMarketExit to be TRUE after completed 5m candle closed below InitialStop (794.00 < 795.00)")
	}
}

func TestPositionWorker_NotifyClosedIdempotent(t *testing.T) {
	callCount := 0
	w := &PositionWorker{
		onClosed: func(pw *PositionWorker) {
			callCount++
		},
	}

	for i := 0; i < 10; i++ {
		w.notifyClosed()
	}

	if callCount != 1 {
		t.Fatalf("expected onClosed to be called exactly 1 time, got %d", callCount)
	}
}

func TestDecoupledStopLossAndSizing(t *testing.T) {
	// 1. Verify StopLoss Calculations
	entryPrice := 500.0
	signalLow := 496.0
	atr := 4.0
	tick := 0.05

	spec, err := ComputeStopAndBufferSpec(entryPrice, signalLow, atr, tick)
	if err != nil {
		t.Fatalf("ComputeStopAndBufferSpec failed: %v", err)
	}
	if spec.InitialStop >= entryPrice {
		t.Fatalf("initial stop %.2f must be below entry price %.2f", spec.InitialStop, entryPrice)
	}
	if spec.OneR <= 0 {
		t.Fatalf("oneR %.2f must be positive", spec.OneR)
	}

	// Overextended check (> 1.25x DeltaMax)
	_, overextErr := ComputeStopAndBufferSpec(500.0, 470.0, 4.0, 0.05)
	if overextErr == nil {
		t.Fatalf("expected error for overextended setup, got nil")
	}

	// BreakevenTriggerPrice check
	bePrice := BreakevenTriggerPrice(500.0, 100, 50, 0.05)
	if bePrice <= 500.0 {
		t.Fatalf("breakeven trigger price %.2f must be above entry price 500.00 to cover taxes", bePrice)
	}

	// ComputeSellSLLimitPrice check
	slLimit := ComputeSellSLLimitPrice(495.0, 0.05)
	if slLimit >= 495.0 {
		t.Fatalf("SL limit price %.2f must be strictly below trigger price 495.00", slLimit)
	}

	// 2. Verify Sizer Calculations & Risk Curve
	if r := EffectiveRiskPerTradePct(10000.0, 0.005); r != 0.0125 {
		t.Fatalf("expected 1.25%% risk for 10k account, got %.4f", r)
	}
	if r := EffectiveRiskPerTradePct(40000.0, 0.005); r != 0.0100 {
		t.Fatalf("expected 1.00%% risk for 40k account, got %.4f", r)
	}
	if r := EffectiveRiskPerTradePct(150000.0, 0.005); r != 0.0075 {
		t.Fatalf("expected 0.75%% risk for 150k account, got %.4f", r)
	}
	if r := EffectiveRiskPerTradePct(1000000.0, 0.005); r != 0.0050 {
		t.Fatalf("expected 0.50%% risk for 10L account, got %.4f", r)
	}
	if r := EffectiveRiskPerTradePct(10000000.0, 0.005); r != 0.0035 {
		t.Fatalf("expected 0.35%% risk for 1Cr account, got %.4f", r)
	}
}



