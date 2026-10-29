package execution

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/engine"
)

const (
	StateEntryPlaced      = "ENTRY_PLACED"
	StateProtectedActive  = "PROTECTED_ACTIVE"
	StateScaleOutPending  = "SCALE_OUT_PENDING"
	StateRunnerActive     = "RUNNER_ACTIVE"
	StateShadowStopActive = "SHADOW_STOP_ACTIVE"
	StateEmergencyFlatten = "EMERGENCY_FLATTENING"
	StateClosed           = "CLOSED"
)

// PositionWorker owns the lifecycle, Multi-Gear SL ratchet, and bulletproof exit for a single open stock
type PositionWorker struct {
	PositionID    string
	TradingDate   string
	Mode          string
	Symbol        string
	Token         uint32
	StockIdx      uint16
	PatternType   string
	TickSize      float64
	MISLeverage   float64
	InitialQty    int
	DustFloorQty  int
	EntryBarIdx   uint8
	OpenedAt      time.Time
	OpenedAtUnix  int64

	// Protected by atomic operations or single worker goroutine
	EntryPrice    float64
	InitialStop   float64
	BreakevenStop float64
	BaseBuffer    float64
	OneR          float64
	SignalBarLow  float64

	// Lock-free atomic state shared with Actor.OnTickHotPath
	entryPriceBits       atomic.Uint64
	breakevenStopBits    atomic.Uint64
	latestLTPBits        atomic.Uint64
	highWaterMarkBits    atomic.Uint64
	confirmedStopBits    atomic.Uint64
	shadowStopBits       atomic.Uint64
	proposedStopBits     atomic.Uint64
	slBreachSinceUnix    atomic.Int64
	shadowBreachTicks    atomic.Uint32
	lastModUnixMs        atomic.Int64

	filledEntryQty    atomic.Int64
	filledExitQty     atomic.Int64
	modCount          atomic.Int32
	currentGear       atomic.Int32
	tranche1Done      atomic.Bool
	tranche1Pending   atomic.Bool
	pendingMarketExit atomic.Bool
	isClosed          atomic.Bool
	closedNotified    atomic.Bool

	mu               sync.RWMutex
	state            string
	entryOrderID     string
	slOrderID        string
	scaleOutOrderID  string
	slOrderStatus    string
	slOpenSinceMs    int64
	entryCostRupees  float64
	exitProceeds     float64
	realisedGrossPnL float64
	totalCharges     float64
	realisedNetPnL   float64
	exitReason       string
	closedAt         time.Time

	wakeCh         chan struct{}
	emergReasonCh  chan string
	fillNotifyCh   chan struct{}
	broker         ExecutionBroker
	paperBroker    *PaperBroker
	reconciler     *OrderReconciler
	db             *sql.DB
	getStockPtr    func() *engine.StockMetrics
	onClosed       func(w *PositionWorker)
	ctx            context.Context
	cancel         context.CancelFunc
}

type WorkerConfig struct {
	PositionID   string
	TradingDate  string
	Mode         string
	Symbol       string
	Token        uint32
	StockIdx     uint16
	PatternType  string
	TickSize     float64
	MISLeverage  float64
	EntryBarIdx  uint8
	Sizing       SizingDecision
	Broker       ExecutionBroker
	PaperBroker  *PaperBroker
	Reconciler   *OrderReconciler
	DB           *sql.DB
	GetStockPtr  func() *engine.StockMetrics
	OnClosed     func(w *PositionWorker)
}

func NewPositionWorker(parentCtx context.Context, cfg WorkerConfig) *PositionWorker {
	ctx, cancel := context.WithCancel(parentCtx)
	now := time.Now()
	tick := cfg.TickSize
	if tick <= 0 {
		tick = 0.05
	}
	lev := cfg.MISLeverage
	if lev <= 0 {
		lev = 5.0
	}

	w := &PositionWorker{
		PositionID:    cfg.PositionID,
		TradingDate:   cfg.TradingDate,
		Mode:          cfg.Mode,
		Symbol:        cfg.Symbol,
		Token:         cfg.Token,
		StockIdx:      cfg.StockIdx,
		PatternType:   cfg.PatternType,
		TickSize:      tick,
		MISLeverage:   lev,
		InitialQty:    cfg.Sizing.Quantity,
		DustFloorQty:  cfg.Sizing.DustFloorQty,
		EntryBarIdx:   cfg.EntryBarIdx,
		OpenedAt:      now,
		OpenedAtUnix:  now.Unix(),
		EntryPrice:    cfg.Sizing.EntryLimitPrice,
		InitialStop:   cfg.Sizing.StopSpec.InitialStop,
		BreakevenStop: cfg.Sizing.BreakevenStop,
		BaseBuffer:    cfg.Sizing.StopSpec.BaseBuffer,
		OneR:          cfg.Sizing.StopSpec.OneR,
		SignalBarLow:  cfg.Sizing.StopSpec.SignalBarLow,
		state:         StateEntryPlaced,
		wakeCh:        make(chan struct{}, 1),
		emergReasonCh: make(chan string, 4),
		fillNotifyCh:  make(chan struct{}, 8),
		broker:        cfg.Broker,
		paperBroker:   cfg.PaperBroker,
		reconciler:    cfg.Reconciler,
		db:            cfg.DB,
		getStockPtr:   cfg.GetStockPtr,
		onClosed:      cfg.OnClosed,
		ctx:           ctx,
		cancel:        cancel,
	}

	w.entryPriceBits.Store(math.Float64bits(cfg.Sizing.EntryLimitPrice))
	w.breakevenStopBits.Store(math.Float64bits(cfg.Sizing.BreakevenStop))
	w.latestLTPBits.Store(math.Float64bits(cfg.Sizing.EntryLimitPrice))
	w.highWaterMarkBits.Store(math.Float64bits(cfg.Sizing.EntryLimitPrice))
	w.confirmedStopBits.Store(math.Float64bits(cfg.Sizing.StopSpec.InitialStop))
	w.shadowStopBits.Store(math.Float64bits(cfg.Sizing.StopSpec.InitialStop))
	w.proposedStopBits.Store(math.Float64bits(cfg.Sizing.StopSpec.InitialStop))
	w.currentGear.Store(1)

	return w
}

func (w *PositionWorker) GetEntryPrice() float64 {
	return math.Float64frombits(w.entryPriceBits.Load())
}

func (w *PositionWorker) GetBreakevenStop() float64 {
	return math.Float64frombits(w.breakevenStopBits.Load())
}

// RemainingQty returns the exact net open share count (FilledEntryQty - FilledExitQty)
func (w *PositionWorker) RemainingQty() int {
	rem := int(w.filledEntryQty.Load() - w.filledExitQty.Load())
	if rem < 0 {
		return 0
	}
	return rem
}

// LatestLTP returns the most recent tick LTP seen by this worker
func (w *PositionWorker) LatestLTP() float64 {
	return math.Float64frombits(w.latestLTPBits.Load())
}

// Start places the entry limit order and launches the dedicated worker goroutine
func (w *PositionWorker) Start() error {
	w.persistPositionState()

	var stockPtr *engine.StockMetrics
	if w.getStockPtr != nil {
		stockPtr = w.getStockPtr()
	}

	orderID, err := w.broker.PlaceLimitBuy(w.ctx, w.Symbol, w.Token, w.InitialQty, w.EntryPrice, stockPtr)
	if err != nil {
		w.mu.Lock()
		w.state = StateClosed
		w.exitReason = fmt.Sprintf("ENTRY_ORDER_FAILED: %v", err)
		w.closedAt = time.Now()
		w.mu.Unlock()
		w.isClosed.Store(true)
		w.persistPositionState()
		w.notifyClosed()
		return err
	}

	w.mu.Lock()
	w.entryOrderID = orderID
	w.mu.Unlock()

	w.reconciler.RegisterOrder(orderID, w.PositionID, "ENTRY", w.Symbol, "BUY", "LIMIT", w.InitialQty, w.EntryPrice, 0, OrderStatusOpen)
	w.persistPositionState()

	go w.workerLoop()
	return nil
}

func (w *PositionWorker) wake() {
	select {
	case w.wakeCh <- struct{}{}:
	default:
	}
}

// notifyClosed triggers the onClosed callback with strict idempotency (guaranteed exactly once)
func (w *PositionWorker) notifyClosed() {
	if w.onClosed != nil && w.closedNotified.CompareAndSwap(false, true) {
		w.onClosed(w)
	}
}

func (w *PositionWorker) OnOrderDelta(legType string, ev OrderUpdateEvent, deltaFilled int) {
	w.mu.Lock()
	switch legType {
	case "ENTRY":
		if deltaFilled > 0 {
			prevEntry := w.filledEntryQty.Load()
			newEntry := prevEntry + int64(deltaFilled)
			w.filledEntryQty.Store(newEntry)
			if ev.AveragePrice > 0 {
				w.entryCostRupees += float64(deltaFilled) * ev.AveragePrice
				w.EntryPrice = w.entryCostRupees / float64(newEntry)
				w.entryPriceBits.Store(math.Float64bits(w.EntryPrice))
			} else {
				w.entryCostRupees += float64(deltaFilled) * w.EntryPrice
			}
		}
		if (ev.Status == OrderStatusRejected || ev.Status == OrderStatusCancelled) && w.filledEntryQty.Load() == 0 {
			w.state = StateClosed
			w.exitReason = fmt.Sprintf("ENTRY_%s: %s", ev.Status, ev.StatusMessage)
			w.closedAt = time.Now()
			w.isClosed.Store(true)
			w.mu.Unlock()
			w.persistPositionState()
			w.notifyClosed()
			return
		}

	case "SL", "SCALE_OUT", "EMERGENCY_EXIT":
		if legType == "SL" {
			w.slOrderStatus = ev.Status
			if ev.Status == OrderStatusOpen && w.slOpenSinceMs == 0 {
				w.slOpenSinceMs = time.Now().UnixMilli()
			} else if ev.Status != OrderStatusOpen {
				w.slOpenSinceMs = 0
			}
		}
		if deltaFilled > 0 {
			newExit := w.filledExitQty.Add(int64(deltaFilled))
			fillPx := ev.AveragePrice
			if fillPx <= 0 {
				fillPx = w.LatestLTP()
			}
			w.exitProceeds += float64(deltaFilled) * fillPx

			// Check if entire position is now flat
			totEntry := w.filledEntryQty.Load()
			if totEntry > 0 && newExit >= totEntry {
				w.finalizeClosedLocked(legType + "_" + ev.Status)
				w.mu.Unlock()
				w.persistPositionState()
				w.notifyClosed()
				return
			}
		}
	}
	w.mu.Unlock()

	select {
	case w.fillNotifyCh <- struct{}{}:
	default:
	}
	w.wake()
}

func (w *PositionWorker) finalizeClosedLocked(reason string) {
	if w.isClosed.Load() {
		return
	}
	totQty := int(w.filledEntryQty.Load())
	if totQty <= 0 {
		totQty = w.InitialQty
	}
	avgBuy := w.EntryPrice
	if totQty > 0 && w.entryCostRupees > 0 {
		avgBuy = w.entryCostRupees / float64(totQty)
	}
	avgSell := avgBuy
	if totQty > 0 && w.exitProceeds > 0 {
		avgSell = w.exitProceeds / float64(totQty)
	}

	taxes := CalculateRoundTripCharges(avgBuy, avgSell, totQty)
	gross := w.exitProceeds - w.entryCostRupees
	net := gross - taxes.TotalCharges

	w.realisedGrossPnL = gross
	w.totalCharges = taxes.TotalCharges
	w.realisedNetPnL = net
	w.state = StateClosed
	if w.exitReason == "" {
		w.exitReason = reason
	}
	w.closedAt = time.Now()
	w.isClosed.Store(true)
}

func (w *PositionWorker) workerLoop() {
	entryTimeout := time.NewTimer(3500 * time.Millisecond)
	defer entryTimeout.Stop()

	watchdogTicker := time.NewTicker(500 * time.Millisecond)
	defer watchdogTicker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return

		case reason := <-w.emergReasonCh:
			w.executeEmergencyFlatten(reason)
			return

		case <-entryTimeout.C:
			w.handleEntryTimeout()
			if w.isClosed.Load() {
				return
			}

		case <-watchdogTicker.C:
			if w.isClosed.Load() {
				return
			}
			// Check if SL order has been stranded in OPEN status for >= 500ms (gapped through limit)
			w.mu.RLock()
			openSince := w.slOpenSinceMs
			slStat := w.slOrderStatus
			w.mu.RUnlock()
			if slStat == OrderStatusOpen && openSince > 0 && (time.Now().UnixMilli()-openSince) >= 500 {
				w.executeEmergencyFlatten("SL_STRANDED_OPEN_500MS")
				return
			}
			w.processPendingWork()

		case <-w.wakeCh:
			if w.isClosed.Load() {
				return
			}
			select {
			case reason := <-w.emergReasonCh:
				w.executeEmergencyFlatten(reason)
				return
			default:
			}
			w.processPendingWork()
		}
	}
}

func (w *PositionWorker) handleEntryTimeout() {
	if w.isClosed.Load() {
		return
	}
	filled := int(w.filledEntryQty.Load())
	if filled >= w.InitialQty {
		w.ensureInitialSLPlaced()
		return
	}

	// Cancel remaining unfilled portion of entry order
	w.mu.RLock()
	entryOrdID := w.entryOrderID
	w.mu.RUnlock()
	if entryOrdID != "" {
		_ = w.broker.CancelOrder(w.ctx, entryOrdID)
		_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)
	}

	filled = int(w.filledEntryQty.Load())
	if filled <= 0 {
		w.mu.Lock()
		w.state = StateClosed
		w.exitReason = "ENTRY_TIMEOUT_UNFILLED"
		w.closedAt = time.Now()
		w.isClosed.Store(true)
		w.mu.Unlock()
		w.persistPositionState()
		w.notifyClosed()
		return
	}

	// Partial Fill Dust Guard: If filled shares < DustFloorQty, flatten immediately
	if filled < w.DustFloorQty {
		slog.Warn("Partial fill below viable dust floor, flattening immediately",
			"symbol", w.Symbol, "filled", filled, "dust_floor", w.DustFloorQty)
		w.executeEmergencyFlatten("PARTIAL_FILL_DUST_GUARD")
		return
	}

	runnerQty := max(1, filled-(filled/2))
	beStop := BreakevenTriggerPrice(w.GetEntryPrice(), filled, runnerQty, w.TickSize)
	w.mu.Lock()
	w.InitialQty = filled
	w.BreakevenStop = beStop
	w.mu.Unlock()
	w.breakevenStopBits.Store(math.Float64bits(beStop))
	w.ensureInitialSLPlaced()
}

func (w *PositionWorker) ToDTO() engine.ActivePositionDTO {
	w.mu.RLock()
	state := w.state
	exitReason := w.exitReason
	gross := w.realisedGrossPnL
	charges := w.totalCharges
	net := w.realisedNetPnL
	closedAt := w.closedAt
	entryPx := w.EntryPrice
	beStop := w.BreakevenStop
	initQty := w.InitialQty
	exitProceeds := w.exitProceeds
	w.mu.RUnlock()

	ltp := w.LatestLTP()
	hwm := math.Float64frombits(w.highWaterMarkBits.Load())
	curStop := math.Float64frombits(w.confirmedStopBits.Load())
	shadowStop := math.Float64frombits(w.shadowStopBits.Load())
	remQty := w.RemainingQty()
	mods := int(w.modCount.Load())
	gear := int(w.currentGear.Load())
	t1Done := w.tranche1Done.Load()

	var rMult, mfeR float64
	if w.OneR > 0 {
		rMult = (ltp - entryPx) / w.OneR
		mfeR = (hwm - entryPx) / w.OneR
	}

	var unrealised float64
	var estCharges, netTotal float64
	if state == StateClosed {
		estCharges = charges
		netTotal = net
	} else {
		filledEntry := int(w.filledEntryQty.Load())
		if filledEntry > 0 {
			unrealised = float64(remQty) * (ltp - entryPx)
			banked := exitProceeds - (float64(filledEntry-remQty) * entryPx)
			gross = banked
			estCharges = CalculateRoundTripCharges(entryPx, ltp, filledEntry).TotalCharges
			netTotal = banked + unrealised - estCharges
		}
	}

	gearLabel := "GEAR 1: AGILE DYNAMIC"
	switch gear {
	case 2:
		gearLabel = "GEAR 2: 50% BANKED + BE"
	case 3:
		gearLabel = "GEAR 3: COARSE 5M TRAIL"
	case 4:
		gearLabel = "GEAR 4: SHADOW SL (MOD 25 READY)"
	}

	var closedStr string
	if !closedAt.IsZero() {
		closedStr = closedAt.Format("15:04:05")
	}

	var exitPx float64
	if filledExit := w.filledExitQty.Load(); filledExit > 0 && exitProceeds > 0 {
		exitPx = exitProceeds / float64(filledExit)
	} else if state == StateClosed && initQty > 0 {
		exitPx = entryPx + (gross / float64(initQty))
	} else {
		exitPx = ltp
	}

	return engine.ActivePositionDTO{
		PositionID:       w.PositionID,
		Mode:             w.Mode,
		Symbol:           w.Symbol,
		InstrumentToken:  w.Token,
		State:            state,
		PatternType:      w.PatternType,
		EntryPrice:       math.Round(entryPx*100.0) / 100.0,
		ExitPrice:        math.Round(exitPx*100.0) / 100.0,
		LTP:              math.Round(ltp*100.0) / 100.0,
		InitialStop:      math.Round(w.InitialStop*100.0) / 100.0,
		CurrentStop:      math.Round(curStop*100.0) / 100.0,
		ShadowStop:       math.Round(shadowStop*100.0) / 100.0,
		BreakevenStop:    math.Round(beStop*100.0) / 100.0,
		BaseBuffer:       math.Round(w.BaseBuffer*100.0) / 100.0,
		HighWaterMark:    math.Round(hwm*100.0) / 100.0,
		OneR:             math.Round(w.OneR*100.0) / 100.0,
		RMultiple:        math.Round(rMult*100.0) / 100.0,
		MFERMultiple:     math.Round(mfeR*100.0) / 100.0,
		InitialQty:       initQty,
		RemainingQty:     remQty,
		ModCount:         mods,
		MaxMods:          MaxExchangeOrderMods,
		CurrentGear:      gear,
		GearLabel:        gearLabel,
		Tranche1Done:     t1Done,
		UnrealisedPnL:    math.Round(unrealised*100.0) / 100.0,
		RealisedGrossPnL: math.Round(gross*100.0) / 100.0,
		EstimatedCharges: math.Round(estCharges*100.0) / 100.0,
		NetTotalPnL:      math.Round(netTotal*100.0) / 100.0,
		ExitReason:       exitReason,
		OpenedAt:         w.OpenedAt.Format("15:04:05"),
		OpenedAtUnix:     w.OpenedAtUnix,
		ClosedAt:         closedStr,
	}
}

func (w *PositionWorker) persistPositionState() {
	if w.db == nil {
		return
	}
	dto := w.ToDTO()
	w.mu.RLock()
	entryOrd := w.entryOrderID
	slOrd := w.slOrderID
	scaleOrd := w.scaleOutOrderID
	closedAt := w.closedAt
	w.mu.RUnlock()

	t1Int := 0
	if dto.Tranche1Done {
		t1Int = 1
	}
	var closedVal interface{}
	if !closedAt.IsZero() {
		closedVal = closedAt.UTC().Format(time.RFC3339)
	}

	_, _ = w.db.Exec(`
		INSERT INTO positions (
			position_id, trading_date, mode, symbol, instrument_token, state, pattern_type,
			entry_order_id, sl_order_id, scale_out_order_id,
			entry_price, exit_price, initial_stop, current_stop, shadow_stop, breakeven_stop, base_buffer,
			high_water_mark, one_r, initial_qty, filled_entry_qty, filled_exit_qty,
			mod_count, current_gear, tranche1_done,
			realised_gross_pnl, total_charges, realised_net_pnl, exit_reason, opened_at, closed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(position_id) DO UPDATE SET
			state = excluded.state,
			entry_order_id = excluded.entry_order_id,
			sl_order_id = excluded.sl_order_id,
			scale_out_order_id = excluded.scale_out_order_id,
			entry_price = excluded.entry_price,
			exit_price = excluded.exit_price,
			current_stop = excluded.current_stop,
			shadow_stop = excluded.shadow_stop,
			breakeven_stop = excluded.breakeven_stop,
			high_water_mark = excluded.high_water_mark,
			initial_qty = excluded.initial_qty,
			filled_entry_qty = excluded.filled_entry_qty,
			filled_exit_qty = excluded.filled_exit_qty,
			mod_count = excluded.mod_count,
			current_gear = excluded.current_gear,
			tranche1_done = excluded.tranche1_done,
			realised_gross_pnl = excluded.realised_gross_pnl,
			total_charges = excluded.total_charges,
			realised_net_pnl = excluded.realised_net_pnl,
			exit_reason = excluded.exit_reason,
			closed_at = excluded.closed_at;
	`,
		w.PositionID, w.TradingDate, w.Mode, w.Symbol, w.Token, dto.State, w.PatternType,
		entryOrd, slOrd, scaleOrd,
		dto.EntryPrice, dto.ExitPrice, dto.InitialStop, dto.CurrentStop, dto.ShadowStop, dto.BreakevenStop, dto.BaseBuffer,
		dto.HighWaterMark, dto.OneR, dto.InitialQty, w.filledEntryQty.Load(), w.filledExitQty.Load(),
		dto.ModCount, dto.CurrentGear, t1Int,
		dto.RealisedGrossPnL, dto.EstimatedCharges, dto.NetTotalPnL, dto.ExitReason,
		w.OpenedAt.UTC().Format(time.RFC3339), closedVal,
	)
}
