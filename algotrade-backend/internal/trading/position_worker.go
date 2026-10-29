package trading

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
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

	filledEntryQty        atomic.Int64
	filledExitQty         atomic.Int64
	modCount              atomic.Int32
	currentGear           atomic.Int32
	hasTranche1Completed  atomic.Bool
	isTranche1Pending     atomic.Bool
	isMarketExitPending   atomic.Bool
	isClosed              atomic.Bool
	hasClosedNotified     atomic.Bool

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
	getStockPtr    func() *market.StockMetrics
	onClosed       func(w *PositionWorker)
	entryTimeoutDuration time.Duration
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
	GetStockPtr  func() *market.StockMetrics
	OnClosed     func(w *PositionWorker)
	EntryTimeoutDuration time.Duration
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

	timeoutDur := cfg.EntryTimeoutDuration
	if timeoutDur <= 0 {
		timeoutDur = 5 * time.Minute
	}

	worker := &PositionWorker{
		PositionID:           cfg.PositionID,
		TradingDate:          cfg.TradingDate,
		Mode:                 cfg.Mode,
		Symbol:               cfg.Symbol,
		Token:                cfg.Token,
		StockIdx:             cfg.StockIdx,
		PatternType:          cfg.PatternType,
		TickSize:             tick,
		MISLeverage:          lev,
		InitialQty:           cfg.Sizing.Quantity,
		DustFloorQty:         cfg.Sizing.DustFloorQty,
		EntryBarIdx:          cfg.EntryBarIdx,
		OpenedAt:             now,
		OpenedAtUnix:         now.Unix(),
		EntryPrice:           cfg.Sizing.EntryLimitPrice,
		InitialStop:          cfg.Sizing.StopSpec.InitialStop,
		BreakevenStop:        cfg.Sizing.BreakevenStop,
		BaseBuffer:           cfg.Sizing.StopSpec.BaseBuffer,
		OneR:                 cfg.Sizing.StopSpec.OneR,
		SignalBarLow:         cfg.Sizing.StopSpec.SignalBarLow,
		state:                StateEntryPlaced,
		entryTimeoutDuration: timeoutDur,
		wakeCh:               make(chan struct{}, 1),
		emergReasonCh:        make(chan string, 4),
		fillNotifyCh:         make(chan struct{}, 8),
		broker:               cfg.Broker,
		paperBroker:          cfg.PaperBroker,
		reconciler:           cfg.Reconciler,
		db:                   cfg.DB,
		getStockPtr:          cfg.GetStockPtr,
		onClosed:             cfg.OnClosed,
		ctx:                  ctx,
		cancel:               cancel,
	}

	worker.entryPriceBits.Store(math.Float64bits(cfg.Sizing.EntryLimitPrice))
	worker.breakevenStopBits.Store(math.Float64bits(cfg.Sizing.BreakevenStop))
	worker.latestLTPBits.Store(math.Float64bits(cfg.Sizing.EntryLimitPrice))
	worker.highWaterMarkBits.Store(math.Float64bits(cfg.Sizing.EntryLimitPrice))
	worker.confirmedStopBits.Store(math.Float64bits(cfg.Sizing.StopSpec.InitialStop))
	worker.shadowStopBits.Store(math.Float64bits(cfg.Sizing.StopSpec.InitialStop))
	worker.proposedStopBits.Store(math.Float64bits(cfg.Sizing.StopSpec.InitialStop))
	worker.currentGear.Store(1)

	return worker
}

func (worker *PositionWorker) GetEntryPrice() float64 {
	return math.Float64frombits(worker.entryPriceBits.Load())
}

func (worker *PositionWorker) GetBreakevenStop() float64 {
	return math.Float64frombits(worker.breakevenStopBits.Load())
}

func (worker *PositionWorker) GetInitialQty() int {
	worker.mu.RLock()
	defer worker.mu.RUnlock()
	return worker.InitialQty
}

func (worker *PositionWorker) GetSLOrderID() string {
	worker.mu.RLock()
	defer worker.mu.RUnlock()
	return worker.slOrderID
}

// GetRemainingQty returns the exact net open share count (FilledEntryQty - FilledExitQty)
func (worker *PositionWorker) GetRemainingQty() int {
	remainingShares := int(worker.filledEntryQty.Load() - worker.filledExitQty.Load())
	if remainingShares < 0 {
		return 0
	}
	return remainingShares
}

// RemainingQty is a backward-compatible alias for GetRemainingQty
func (worker *PositionWorker) RemainingQty() int {
	return worker.GetRemainingQty()
}

// GetLatestLTP returns the most recent tick LTP seen by this worker
func (worker *PositionWorker) GetLatestLTP() float64 {
	return math.Float64frombits(worker.latestLTPBits.Load())
}

// LatestLTP is a backward-compatible alias for GetLatestLTP
func (worker *PositionWorker) LatestLTP() float64 {
	return worker.GetLatestLTP()
}

// Start places the entry limit order and launches the dedicated worker goroutine
func (worker *PositionWorker) Start() error {
	worker.persistPositionState()

	var stockPtr *market.StockMetrics
	if worker.getStockPtr != nil {
		stockPtr = worker.getStockPtr()
	}

	orderID, err := worker.broker.PlaceLimitBuy(worker.ctx, worker.Symbol, worker.Token, worker.InitialQty, worker.EntryPrice, stockPtr)
	if err != nil {
		worker.mu.Lock()
		worker.state = StateClosed
		worker.exitReason = fmt.Sprintf("ENTRY_ORDER_FAILED: %v", err)
		worker.closedAt = time.Now()
		worker.mu.Unlock()
		worker.isClosed.Store(true)
		worker.persistPositionState()
		worker.notifyClosed()
		return err
	}

	worker.mu.Lock()
	worker.entryOrderID = orderID
	worker.mu.Unlock()

	worker.reconciler.RegisterOrder(orderID, worker.PositionID, "ENTRY", worker.Symbol, "BUY", "LIMIT", worker.InitialQty, worker.EntryPrice, 0, OrderStatusOpen)
	worker.persistPositionState()

	go worker.workerLoop()
	return nil
}

func (worker *PositionWorker) wakeWorker() {
	select {
	case worker.wakeCh <- struct{}{}:
	default:
	}
}

// wake is a backward-compatible alias for wakeWorker
func (worker *PositionWorker) wake() {
	worker.wakeWorker()
}

// notifyWorkerClosed triggers the onClosed callback with strict idempotency (guaranteed exactly once)
func (worker *PositionWorker) notifyWorkerClosed() {
	if worker.onClosed != nil && worker.hasClosedNotified.CompareAndSwap(false, true) {
		worker.onClosed(worker)
	}
}

// notifyClosed is a backward-compatible alias for notifyWorkerClosed
func (worker *PositionWorker) notifyClosed() {
	worker.notifyWorkerClosed()
}

func (worker *PositionWorker) OnOrderDelta(legType string, orderEvent OrderUpdateEvent, deltaFilled int) {
	worker.mu.Lock()
	switch legType {
	case "ENTRY":
		if deltaFilled > 0 {
			prevEntry := worker.filledEntryQty.Load()
			newEntry := prevEntry + int64(deltaFilled)
			worker.filledEntryQty.Store(newEntry)
			if orderEvent.AveragePrice > 0 {
				worker.entryCostRupees += float64(deltaFilled) * orderEvent.AveragePrice
				worker.EntryPrice = worker.entryCostRupees / float64(newEntry)
				worker.entryPriceBits.Store(math.Float64bits(worker.EntryPrice))
			} else {
				worker.entryCostRupees += float64(deltaFilled) * worker.EntryPrice
			}
		}
		if (orderEvent.Status == OrderStatusRejected || orderEvent.Status == OrderStatusCancelled) && worker.filledEntryQty.Load() == 0 {
			worker.state = StateClosed
			worker.exitReason = fmt.Sprintf("ENTRY_%s: %s", orderEvent.Status, orderEvent.StatusMessage)
			worker.closedAt = time.Now()
			worker.isClosed.Store(true)
			worker.mu.Unlock()
			worker.persistPositionState()
			worker.notifyClosed()
			return
		}

	case "SL", "SCALE_OUT", "EMERGENCY_EXIT":
		if legType == "SL" {
			worker.slOrderStatus = orderEvent.Status
			if orderEvent.Status == OrderStatusOpen && worker.slOpenSinceMs == 0 {
				worker.slOpenSinceMs = time.Now().UnixMilli()
			} else if orderEvent.Status != OrderStatusOpen {
				worker.slOpenSinceMs = 0
			}
		}
		if deltaFilled > 0 {
			newExit := worker.filledExitQty.Add(int64(deltaFilled))
			fillPx := orderEvent.AveragePrice
			if fillPx <= 0 {
				fillPx = worker.LatestLTP()
			}
			worker.exitProceeds += float64(deltaFilled) * fillPx

			// Check if entire position is now flat
			totEntry := worker.filledEntryQty.Load()
			if totEntry > 0 && newExit >= totEntry {
				worker.finalizeClosedLocked(legType + "_" + orderEvent.Status)
				worker.mu.Unlock()
				worker.persistPositionState()
				worker.notifyClosed()
				return
			}
		}
	}
	worker.mu.Unlock()

	select {
	case worker.fillNotifyCh <- struct{}{}:
	default:
	}
	worker.wake()
}

func (worker *PositionWorker) finalizeClosedLocked(reason string) {
	if worker.isClosed.Load() {
		return
	}
	totQty := int(worker.filledEntryQty.Load())
	if totQty <= 0 {
		totQty = worker.InitialQty
	}
	avgBuy := worker.EntryPrice
	if totQty > 0 && worker.entryCostRupees > 0 {
		avgBuy = worker.entryCostRupees / float64(totQty)
	}
	avgSell := avgBuy
	if totQty > 0 && worker.exitProceeds > 0 {
		avgSell = worker.exitProceeds / float64(totQty)
	}

	taxes := CalculateRoundTripCharges(avgBuy, avgSell, totQty)
	gross := worker.exitProceeds - worker.entryCostRupees
	net := gross - taxes.TotalCharges

	worker.realisedGrossPnL = gross
	worker.totalCharges = taxes.TotalCharges
	worker.realisedNetPnL = net
	worker.state = StateClosed
	if worker.exitReason == "" {
		worker.exitReason = reason
	}
	worker.closedAt = time.Now()
	worker.isClosed.Store(true)
}

func (worker *PositionWorker) workerLoop() {
	entryTimeout := time.NewTimer(worker.entryTimeoutDuration)
	defer entryTimeout.Stop()

	watchdogTicker := time.NewTicker(500 * time.Millisecond)
	defer watchdogTicker.Stop()

	for {
		select {
		case <-worker.ctx.Done():
			return

		case reason := <-worker.emergReasonCh:
			worker.executeEmergencyFlatten(reason)
			return

		case <-entryTimeout.C:
			worker.handleEntryTimeout()
			if worker.isClosed.Load() {
				return
			}

		case <-watchdogTicker.C:
			if worker.isClosed.Load() {
				return
			}
			// Check if SL order has been stranded in OPEN status for >= 500ms (gapped through limit)
			worker.mu.RLock()
			openSince := worker.slOpenSinceMs
			slStat := worker.slOrderStatus
			worker.mu.RUnlock()
			if slStat == OrderStatusOpen && openSince > 0 && (time.Now().UnixMilli()-openSince) >= 500 {
				worker.executeEmergencyFlatten("SL_STRANDED_OPEN_500MS")
				return
			}
			worker.processPendingWork()

		case <-worker.wakeCh:
			if worker.isClosed.Load() {
				return
			}
			select {
			case reason := <-worker.emergReasonCh:
				worker.executeEmergencyFlatten(reason)
				return
			default:
			}
			worker.processPendingWork()
		}
	}
}

func (worker *PositionWorker) handleEntryTimeout() {
	if worker.isClosed.Load() {
		return
	}
	filled := int(worker.filledEntryQty.Load())

	worker.mu.RLock()
	initQty := worker.InitialQty
	entryOrdID := worker.entryOrderID
	worker.mu.RUnlock()

	if filled >= initQty {
		worker.ensureInitialSLPlaced()
		return
	}

	// Cancel remaining unfilled portion of entry order
	if entryOrdID != "" {
		_ = worker.broker.CancelOrder(worker.ctx, entryOrdID)
		_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
	}

	filled = int(worker.filledEntryQty.Load())
	if filled <= 0 {
		worker.mu.Lock()
		worker.state = StateClosed
		worker.exitReason = "ENTRY_TIMEOUT_UNFILLED"
		worker.closedAt = time.Now()
		worker.isClosed.Store(true)
		worker.mu.Unlock()
		worker.persistPositionState()
		worker.notifyClosed()
		return
	}

	// Partial Fill Dust Guard: If filled shares < DustFloorQty, flatten immediately
	if filled < worker.DustFloorQty {
		slog.Warn("Partial fill below viable dust floor, flattening immediately",
			"symbol", worker.Symbol, "filled", filled, "dust_floor", worker.DustFloorQty)
		worker.executeEmergencyFlatten("PARTIAL_FILL_DUST_GUARD")
		return
	}

	runnerQty := max(1, filled-(filled/2))
	beStop := BreakevenTriggerPrice(worker.GetEntryPrice(), filled, runnerQty, worker.TickSize)
	worker.mu.Lock()
	worker.InitialQty = filled
	worker.BreakevenStop = beStop
	worker.mu.Unlock()
	worker.breakevenStopBits.Store(math.Float64bits(beStop))
	worker.ensureInitialSLPlaced()
	worker.persistPositionState()
}

// BuildPositionDTO builds an ActivePositionDTO telemetry snapshot for frontend broadcasts and persistence
func (worker *PositionWorker) BuildPositionDTO() market.ActivePositionDTO {
	return worker.ToDTO()
}

func (worker *PositionWorker) ToDTO() market.ActivePositionDTO {
	worker.mu.RLock()
	state := worker.state
	exitReason := worker.exitReason
	gross := worker.realisedGrossPnL
	charges := worker.totalCharges
	net := worker.realisedNetPnL
	closedAt := worker.closedAt
	entryPx := worker.EntryPrice
	beStop := worker.BreakevenStop
	initQty := worker.InitialQty
	exitProceeds := worker.exitProceeds
	worker.mu.RUnlock()

	ltp := worker.LatestLTP()
	hwm := math.Float64frombits(worker.highWaterMarkBits.Load())
	curStop := math.Float64frombits(worker.confirmedStopBits.Load())
	shadowStop := math.Float64frombits(worker.shadowStopBits.Load())
	remQty := worker.RemainingQty()
	mods := int(worker.modCount.Load())
	gear := int(worker.currentGear.Load())
	hasTranche1Completed := worker.hasTranche1Completed.Load()

	var rMult, mfeR float64
	if worker.OneR > 0 {
		rMult = (ltp - entryPx) / worker.OneR
		mfeR = (hwm - entryPx) / worker.OneR
	}

	var unrealised float64
	var estCharges, netTotal float64
	if state == StateClosed {
		estCharges = charges
		netTotal = net
	} else {
		filledEntry := int(worker.filledEntryQty.Load())
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
	if filledExit := worker.filledExitQty.Load(); filledExit > 0 && exitProceeds > 0 {
		exitPx = exitProceeds / float64(filledExit)
	} else if state == StateClosed && initQty > 0 {
		exitPx = entryPx + (gross / float64(initQty))
	} else {
		exitPx = ltp
	}

	return market.ActivePositionDTO{
		PositionID:       worker.PositionID,
		Mode:             worker.Mode,
		Symbol:           worker.Symbol,
		InstrumentToken:  worker.Token,
		State:            state,
		PatternType:      worker.PatternType,
		EntryPrice:       math.Round(entryPx*100.0) / 100.0,
		ExitPrice:        math.Round(exitPx*100.0) / 100.0,
		LTP:              math.Round(ltp*100.0) / 100.0,
		InitialStop:      math.Round(worker.InitialStop*100.0) / 100.0,
		CurrentStop:      math.Round(curStop*100.0) / 100.0,
		ShadowStop:       math.Round(shadowStop*100.0) / 100.0,
		BreakevenStop:    math.Round(beStop*100.0) / 100.0,
		BaseBuffer:       math.Round(worker.BaseBuffer*100.0) / 100.0,
		HighWaterMark:    math.Round(hwm*100.0) / 100.0,
		OneR:             math.Round(worker.OneR*100.0) / 100.0,
		RMultiple:        math.Round(rMult*100.0) / 100.0,
		MFERMultiple:     math.Round(mfeR*100.0) / 100.0,
		InitialQty:       initQty,
		RemainingQty:     remQty,
		ModCount:         mods,
		MaxMods:          MaxExchangeOrderMods,
		CurrentGear:      gear,
		GearLabel:        gearLabel,
		HasTranche1Completed: hasTranche1Completed,
		UnrealisedPnL:    math.Round(unrealised*100.0) / 100.0,
		RealisedGrossPnL: math.Round(gross*100.0) / 100.0,
		EstimatedCharges: math.Round(estCharges*100.0) / 100.0,
		NetTotalPnL:      math.Round(netTotal*100.0) / 100.0,
		ExitReason:       exitReason,
		OpenedAt:         worker.OpenedAt.Format("15:04:05"),
		OpenedAtUnix:     worker.OpenedAtUnix,
		ClosedAt:         closedStr,
	}
}

func (worker *PositionWorker) persistPositionState() {
	if worker.db == nil {
		return
	}
	dto := worker.ToDTO()
	worker.mu.RLock()
	entryOrd := worker.entryOrderID
	slOrd := worker.slOrderID
	scaleOrd := worker.scaleOutOrderID
	closedAt := worker.closedAt
	worker.mu.RUnlock()

	t1Int := 0
	if dto.HasTranche1Completed {
		t1Int = 1
	}
	var closedVal interface{}
	if !closedAt.IsZero() {
		closedVal = closedAt.UTC().Format(time.RFC3339)
	}

	_, _ = worker.db.Exec(`
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
		worker.PositionID, worker.TradingDate, worker.Mode, worker.Symbol, worker.Token, dto.State, worker.PatternType,
		entryOrd, slOrd, scaleOrd,
		dto.EntryPrice, dto.ExitPrice, dto.InitialStop, dto.CurrentStop, dto.ShadowStop, dto.BreakevenStop, dto.BaseBuffer,
		dto.HighWaterMark, dto.OneR, dto.InitialQty, worker.filledEntryQty.Load(), worker.filledExitQty.Load(),
		dto.ModCount, dto.CurrentGear, t1Int,
		dto.RealisedGrossPnL, dto.EstimatedCharges, dto.NetTotalPnL, dto.ExitReason,
		worker.OpenedAt.UTC().Format(time.RFC3339), closedVal,
	)
}
