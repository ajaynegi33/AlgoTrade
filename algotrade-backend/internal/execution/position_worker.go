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
	decayBreachSinceUnix atomic.Int64
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
		if w.onClosed != nil {
			w.onClosed(w)
		}
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

// TriggerEmergencyExit transitions immediately to market flatten (non-blocking from hot path)
func (w *PositionWorker) TriggerEmergencyExit(reason string) {
	if w.isClosed.Load() {
		return
	}
	if w.pendingMarketExit.CompareAndSwap(false, true) {
		select {
		case w.emergReasonCh <- reason:
		default:
		}
		w.wake()
	}
}

// ForceEmergencyExit unconditionally forces an emergency exit, even if a previous exit attempt was pending
func (w *PositionWorker) ForceEmergencyExit(reason string) {
	if w.isClosed.Load() {
		return
	}
	w.pendingMarketExit.Store(true)
	select {
	case w.emergReasonCh <- reason:
		w.wake()
	default:
		// If emergReasonCh is already full or workerLoop has terminated, execute emergency flatten directly
		go w.executeEmergencyFlatten(reason)
	}
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

// OnTickHotPath executes in <250ns inside Actor.processTick with zero heap allocations
func (w *PositionWorker) OnTickHotPath(s *engine.StockMetrics, inMacroShock bool) {
	if w.isClosed.Load() {
		return
	}

	// If running in Paper Mode, match resting Paper SL orders against live 5-level depth first
	if w.Mode == "PAPER" && w.paperBroker != nil {
		w.paperBroker.EvaluateTickForToken(s)
	}

	if w.pendingMarketExit.Load() {
		return
	}

	ltp := s.LTP
	if ltp <= 0 {
		return
	}
	w.latestLTPBits.Store(math.Float64bits(ltp))

	// Only evaluate trailing stops once entry has filled at least 1 share
	if w.filledEntryQty.Load() <= 0 || w.RemainingQty() <= 0 {
		return
	}

	// 1. Update High-Water Mark (HWM)
	entryPx := w.GetEntryPrice()
	beStop := w.GetBreakevenStop()
	hwm := math.Float64frombits(w.highWaterMarkBits.Load())
	if ltp > hwm {
		hwm = ltp
		w.highWaterMarkBits.Store(math.Float64bits(hwm))
	}
	rCur := (ltp - entryPx) / w.OneR
	rMFE := (hwm - entryPx) / w.OneR
	confirmedStop := math.Float64frombits(w.confirmedStopBits.Load())

	// 2. GUARD A: Gapped-Through / Stranded SL Watchdog (1s Epoch or 3-Tick Breach)
	if ltp <= confirmedStop {
		breachStart := w.slBreachSinceUnix.Load()
		if breachStart == 0 {
			w.slBreachSinceUnix.Store(s.LastUpdatedUnix)
		} else if (s.LastUpdatedUnix-breachStart) >= 1 || ltp <= confirmedStop-(3.0*w.TickSize) {
			w.TriggerEmergencyExit("SL_GAP_THROUGH_BREACH")
			return
		}
	} else {
		w.slBreachSinceUnix.Store(0)
	}

	// 3. GUARD B: Gear 4 Virtual Shadow Stop (Active when modCount >= 24; Mod 25 Reserved for Exit!)
	if w.modCount.Load() >= 24 {
		shadowStop := math.Float64frombits(w.shadowStopBits.Load())
		if shadowStop > 0 && ltp <= shadowStop {
			hits := w.shadowBreachTicks.Add(1)
			if hits >= 2 || s.FastAggressorEMA < 0.42 || ltp <= shadowStop-(2.0*w.TickSize) {
				w.TriggerEmergencyExit("SHADOW_STOP_HIT_MOD25")
				return
			}
		} else {
			w.shadowBreachTicks.Store(0)
		}
	}

	// 4. GUARD C: Volatility-Normalized Sudden Shock & Book-Vacuum Reflex
	// Uses continuous RollingPeak (tau=20s) + FastAggressorEMA (zero 30s reset blind spot!)
	atrSafe := math.Max(s.ATR14, ltp*0.0025)
	effectiveAggBuy := s.AggressorBuyRatio
	if s.FastAggressorEMA < effectiveAggBuy {
		effectiveAggBuy = s.FastAggressorEMA
	}
	shockDrop := s.RollingPeak - ltp
	shockThreshold := math.Min(0.55*w.OneR, math.Max(0.45*atrSafe, 0.0030*ltp))
	bidVacuum := s.SmoothedBuyerRatio < 0.25 && s.FastAggressorEMA < 0.28

	if (shockDrop >= shockThreshold && bidVacuum) || (inMacroShock && ltp < s.ExchangeVWAP && effectiveAggBuy < 0.35) {
		if !w.tranche1Done.Load() && ltp < s.ExchangeVWAP {
			// Pre-1R position lost VWAP into a sudden vacuum shock -> Cut immediately!
			w.TriggerEmergencyExit("SUDDEN_MARKET_SHOCK_CUT")
			return
		}
		if w.tranche1Done.Load() {
			// Runner in profit during shock -> Ratchet stop to max(Breakeven, LTP - 0.35*BaseBuffer)
			tightStop := math.Max(beStop, RoundDownToTick(ltp-0.35*w.BaseBuffer, w.TickSize))
			if tightStop > confirmedStop {
				w.ProposeStopRatchet(tightStop, atrSafe, true)
			}
			return
		}
	}

	// 5. GUARD D: +1.0R 3-Step Race-Free Scale-Out Trigger
	if !w.tranche1Done.Load() && !w.tranche1Pending.Load() && rCur >= 1.00 {
		w.tranche1Pending.Store(true)
		w.wake()
		return
	}

	// 6. Compute Dynamic Order-Flow Multiplier M_flow & Parabolic Climax Factor C_climax
	currBar := s.SessionBars[s.CurrentBarIdx]
	var clv float64 = 0.50
	if currBar.High > currBar.Low {
		clv = (ltp - currBar.Low) / (currBar.High - currBar.Low)
	}
	flowScore := 0.50*float64(effectiveAggBuy) + 0.35*float64(s.SmoothedBuyerRatio) + 0.15*clv

	structureIntact := ltp >= s.ExchangeVWAP && ltp > w.SignalBarLow && !inMacroShock
	var mFlow float64
	if flowScore >= 0.56 && structureIntact {
		mFlow = math.Min(1.10, 0.85+(flowScore-0.56)*1.25)
	} else if structureIntact {
		// Structure (VWAP & SignalBarLow) intact: floor M_flow at 0.65 so 1 sell print doesn't choke the trade
		mFlow = math.Max(0.65, 0.65+math.Max(0, flowScore-0.35)*0.95)
	} else {
		// Below VWAP or in Macro Shock: unlock aggressive tightening down to 0.40x
		mFlow = math.Max(0.40, 0.40+math.Max(0, flowScore-0.25)*1.40)
	}

	// Parabolic Climax Tightener: When runner is >= +2.2R and extended > 1.8 ATR above VWAP
	cClimax := 1.0
	xVWAP := (ltp - s.ExchangeVWAP) / atrSafe
	if rMFE >= 2.2 && xVWAP > 1.8 {
		cClimax = math.Max(0.35, 1.0-0.22*(xVWAP-1.8))
	}

	// 7. Compute Dynamic Breathing Buffer (BaseBuffer / OneR * M_flow * C_climax * D_thesis)
	// Falsification Guard (Bug B3): Pre-1R, if BaseBuffer > OneR and order flow weakens (or trade stalls >= 90s),
	// anchor effectiveBase to min(BaseBuffer, OneR) so tightening multipliers immediately lift the stop above InitialStop!
	elapsedSec := max(int64(0), s.LastUpdatedUnix-w.OpenedAtUnix)
	effectiveBase := w.BaseBuffer
	if !w.tranche1Done.Load() && (flowScore < 0.55 || elapsedSec >= 90) {
		effectiveBase = math.Min(w.BaseBuffer, w.OneR)
	}

	// Pre-1R Thesis-Decay & Stagnation Ratchet (D_thesis):
	// Minimizes loss proactively when buyer absorption fails after entry or when a breakout stalls for > 90s.
	dThesis := 1.0
	if !w.tranche1Done.Load() {
		if elapsedSec >= 20 && rMFE < 0.25 && flowScore < 0.42 {
			// Case B: Immediate post-entry buyer absorption failure -> compress stop buffer to [0.45, 0.80]
			dThesis = math.Max(0.45, math.Min(0.80, 0.55+(flowScore-0.30)*2.0))
		} else if elapsedSec >= 90 && rMFE < 0.40 && flowScore < 0.50 {
			// Case C: Stagnation / Time-Decay Ratchet after 90s without reaching +0.40R
			dThesis = math.Max(0.40, 1.0-0.0033*float64(elapsedSec-90))
		}
	}

	bDynamic := effectiveBase * mFlow * cClimax * dThesis
	unclampedCand := RoundToTick(hwm-bDynamic, w.TickSize)

	// Minimum Tick-Clearance Guard (Anti-Suicide-Stop):
	// When ratcheting pre-1R due to thesis decay, leave at least max(3 ticks, 0.22*ATR, 0.18% LTP) below current LTP
	// so a single bid-ask bounce does not prematurely wick out the position.
	if !w.tranche1Done.Load() && dThesis < 1.0 {
		minClearance := math.Max(3.0*w.TickSize, math.Max(0.22*atrSafe, 0.0018*ltp))
		maxPre1RCand := RoundDownToTick(ltp-minClearance, w.TickSize)
		if unclampedCand > maxPre1RCand && maxPre1RCand > confirmedStop {
			unclampedCand = maxPre1RCand
		}
	}

	if rMFE >= 0.85 {
		unclampedCand = math.Max(unclampedCand, beStop)
	} else if rMFE >= 0.50 {
		unclampedCand = math.Max(unclampedCand, RoundToTick(entryPx-0.50*w.OneR, w.TickSize))
	}
	if w.tranche1Done.Load() && s.CurrentBarIdx > w.EntryBarIdx && s.CurrentBarIdx >= 1 {
		prev5mLow := s.SessionBars[s.CurrentBarIdx-1].Low - 2.0*w.TickSize
		unclampedCand = math.Max(unclampedCand, RoundToTick(prev5mLow, w.TickSize))
	}

	// 8. Completed 5m Bar Structural Invalidation Check (Pre-1R)
	if !w.tranche1Done.Load() && s.CurrentBarIdx > w.EntryBarIdx && s.CurrentBarIdx >= 1 {
		prevBar := s.SessionBars[s.CurrentBarIdx-1]
		if prevBar.Close < s.ExchangeVWAP && prevBar.Close < prevBar.Open && s.AggressorBuyRatio < 0.42 {
			w.TriggerEmergencyExit("COMPLETED_5M_VWAP_BREAKDOWN")
			return
		}
	}

	// 9. ZERO-FALLTHROUGH FALLING-KNIFE vs. NORMAL RATCHET:
	if unclampedCand <= confirmedStop {
		w.decayBreachSinceUnix.Store(0)
		return
	}

	maxLegalTrigger := RoundDownToTick(ltp-math.Max(2.0*w.TickSize, 0.001*ltp), w.TickSize)
	if ltp <= unclampedCand {
		// Price is ALREADY below unclampedCand! NEVER clamp safeCand down below LTP!
		if (s.LastUpdatedUnix-w.OpenedAtUnix) >= 20 && ltp < s.ExchangeVWAP && effectiveAggBuy < 0.40 {
			since := w.decayBreachSinceUnix.Load()
			if since == 0 {
				w.decayBreachSinceUnix.Store(s.LastUpdatedUnix)
			} else if (s.LastUpdatedUnix - since) >= 2 {
				w.TriggerEmergencyExit("THESIS_DECAY_FALLING_KNIFE_CUT")
			}
		}
		return
	}

	w.decayBreachSinceUnix.Store(0)
	safeCand := math.Min(unclampedCand, maxLegalTrigger)
	if safeCand > confirmedStop {
		w.ProposeStopRatchet(safeCand, atrSafe, false)
	}
}

// ProposeStopRatchet governs Mods 1–18 (Agile), Mods 19–24 (Coarse), and Gear 4 (Mod 24 Shadow Stop)
func (w *PositionWorker) ProposeStopRatchet(candidateStop, atrSafe float64, isUrgentShockClamp bool) {
	mods := w.modCount.Load()

	// Always advance Shadow Stop in RAM
	for {
		curShadowBits := w.shadowStopBits.Load()
		if candidateStop <= math.Float64frombits(curShadowBits) {
			break
		}
		if w.shadowStopBits.CompareAndSwap(curShadowBits, math.Float64bits(candidateStop)) {
			break
		}
	}

	// GEAR 4 INVARIANT: At modCount >= 24, NEVER send a price-ratchet PUT! Mod 25 is strictly for Market Exit!
	if mods >= 24 {
		w.currentGear.Store(4)
		return
	}

	confirmed := math.Float64frombits(w.confirmedStopBits.Load())
	improvement := candidateStop - confirmed
	if improvement <= 0 {
		return
	}

	nowMs := time.Now().UnixMilli()
	elapsedMs := nowMs - w.lastModUnixMs.Load()

	if !isUrgentShockClamp {
		if mods < 18 {
			minStep := math.Max(2.0*w.TickSize, 0.12*w.BaseBuffer)
			if improvement < minStep || elapsedMs < 350 {
				return
			}
		} else {
			minStep := math.Max(0.40*w.BaseBuffer, 0.35*atrSafe)
			if improvement < minStep || elapsedMs < 1500 {
				return
			}
		}
	}

	for {
		oldBits := w.proposedStopBits.Load()
		if candidateStop <= math.Float64frombits(oldBits) {
			return
		}
		if w.proposedStopBits.CompareAndSwap(oldBits, math.Float64bits(candidateStop)) {
			w.wake()
			return
		}
	}
}

// OnOrderDelta is invoked by OrderReconciler with idempotent incremental fill deltas
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
			if w.onClosed != nil {
				w.onClosed(w)
			}
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
				if w.onClosed != nil {
					w.onClosed(w)
				}
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
		if w.onClosed != nil {
			w.onClosed(w)
		}
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

func (w *PositionWorker) processPendingWork() {
	if w.isClosed.Load() || w.pendingMarketExit.Load() {
		return
	}

	// 1. Ensure initial SL is placed as soon as entry fills
	if w.filledEntryQty.Load() > 0 {
		w.mu.RLock()
		hasSL := w.slOrderID != ""
		w.mu.RUnlock()
		if !hasSL && int(w.filledEntryQty.Load()) >= w.InitialQty {
			w.ensureInitialSLPlaced()
			if w.isClosed.Load() || w.pendingMarketExit.Load() {
				return
			}
		}
	}

	// 2. Execute +1.0R 3-Step Race-Free Scale-Out if triggered
	if w.tranche1Pending.Load() && !w.tranche1Done.Load() {
		w.executeThreeStepScaleOut()
		if w.isClosed.Load() || w.pendingMarketExit.Load() {
			return
		}
	}

	// 3. Apply pending Stop Ratchet if proposedStop > confirmedStop and modCount < 24
	w.mu.RLock()
	slOrdID := w.slOrderID
	w.mu.RUnlock()
	if slOrdID == "" || w.modCount.Load() >= 24 {
		return
	}

	proposed := math.Float64frombits(w.proposedStopBits.Load())
	confirmed := math.Float64frombits(w.confirmedStopBits.Load())
	if proposed <= confirmed {
		return
	}

	rem := w.RemainingQty()
	if rem <= 0 {
		return
	}

	ltp := w.LatestLTP()
	maxLegal := RoundDownToTick(ltp-math.Max(2.0*w.TickSize, 0.001*ltp), w.TickSize)
	targetTrigger := math.Min(proposed, maxLegal)
	if targetTrigger <= confirmed {
		return
	}

	limitPx := computeSellSLLimitPrice(targetTrigger, w.TickSize)
	err := w.broker.ModifySLOrder(w.ctx, slOrdID, rem, targetTrigger, limitPx, ltp, w.TickSize)
	if err != nil {
		slog.Warn("SL ratchet modification rejected by broker", "symbol", w.Symbol, "target", targetTrigger, "error", err)
		return
	}

	newMods := w.modCount.Add(1)
	w.lastModUnixMs.Store(time.Now().UnixMilli())
	w.confirmedStopBits.Store(math.Float64bits(targetTrigger))

	if newMods >= 24 {
		w.currentGear.Store(4)
		w.mu.Lock()
		w.state = StateShadowStopActive
		w.mu.Unlock()
	} else if newMods >= 18 {
		w.currentGear.Store(3)
	}
	w.persistPositionState()
}

func (w *PositionWorker) ensureInitialSLPlaced() {
	w.mu.RLock()
	if w.slOrderID != "" || w.isClosed.Load() {
		w.mu.RUnlock()
		return
	}
	w.mu.RUnlock()

	rem := w.RemainingQty()
	if rem <= 0 {
		return
	}

	ltp := w.LatestLTP()
	// Guard: If price already crashed below InitialStop during entry fill, flatten immediately
	if ltp > 0 && ltp <= w.InitialStop+w.TickSize {
		slog.Warn("LTP already at or below InitialStop upon entry fill; executing immediate flatten",
			"symbol", w.Symbol, "ltp", ltp, "initial_stop", w.InitialStop)
		w.executeEmergencyFlatten("INITIAL_SL_BREACHED_ON_FILL")
		return
	}

	limitPx := computeSellSLLimitPrice(w.InitialStop, w.TickSize)
	slID, err := w.broker.PlaceSLOrder(w.ctx, w.Symbol, w.Token, rem, w.InitialStop, limitPx, ltp, w.TickSize)
	if err != nil {
		slog.Error("Initial SL placement failed; executing immediate protective flatten",
			"symbol", w.Symbol, "error", err)
		w.executeEmergencyFlatten("INITIAL_SL_REJECTED_BY_OMS")
		return
	}

	w.mu.Lock()
	w.slOrderID = slID
	w.slOrderStatus = OrderStatusTrigPending
	w.state = StateProtectedActive
	w.mu.Unlock()

	w.reconciler.RegisterOrder(slID, w.PositionID, "SL", w.Symbol, "SELL", "SL", rem, limitPx, w.InitialStop, OrderStatusTrigPending)
	w.persistPositionState()
}

// executeThreeStepScaleOut performs the Race-Free 3-Step Scale-Out at +1.0R with Compensating Rollback
func (w *PositionWorker) executeThreeStepScaleOut() {
	rem := w.RemainingQty()
	if rem < 2 {
		// Position is 1 share: cannot split; lock Breakeven stop directly
		w.tranche1Done.Store(true)
		w.tranche1Pending.Store(false)
		w.currentGear.Store(2)
		w.ProposeStopRatchet(w.BreakevenStop, w.OneR, true)
		return
	}

	w.mu.Lock()
	slOrdID := w.slOrderID
	w.state = StateScaleOutPending
	w.mu.Unlock()

	if slOrdID == "" {
		w.ensureInitialSLPlaced()
		w.mu.RLock()
		slOrdID = w.slOrderID
		w.mu.RUnlock()
		if slOrdID == "" {
			return
		}
	}

	scaleOutQty := rem / 2
	runnerQty := rem - scaleOutQty
	curStop := math.Float64frombits(w.confirmedStopBits.Load())
	curLimit := computeSellSLLimitPrice(curStop, w.TickSize)
	ltp := w.LatestLTP()

	// STEP 1: Shrink resting SL quantity from rem -> runnerQty while KEEPING curStop trigger unchanged
	// (Prevents resting SL from firing at Breakeven during the 3ms window while Step 2 is in flight!)
	if w.modCount.Load() >= 24 {
		w.tranche1Pending.Store(false)
		return
	}
	if err := w.broker.ModifySLOrder(w.ctx, slOrdID, runnerQty, curStop, curLimit, ltp, w.TickSize); err != nil {
		slog.Warn("Scale-out Step 1 (shrink SL qty) failed; keeping 100% SL intact", "symbol", w.Symbol, "error", err)
		w.mu.Lock()
		w.state = StateProtectedActive
		w.mu.Unlock()
		w.tranche1Pending.Store(false)
		return
	}
	w.modCount.Add(1)
	w.lastModUnixMs.Store(time.Now().UnixMilli())

	// STEP 2: Place Protected Marketable-Limit Sell for scaleOutQty (50%)
	var stockPtr *engine.StockMetrics
	if w.getStockPtr != nil {
		stockPtr = w.getStockPtr()
	}
	scaleOrdID, sellErr := w.broker.PlaceProtectedMarketSell(w.ctx, w.Symbol, w.Token, scaleOutQty, ltp, w.TickSize, stockPtr)
	if sellErr != nil {
		slog.Error("Scale-out Step 2 sell failed; rolling back resting SL to full quantity", "symbol", w.Symbol, "error", sellErr)
		// COMPENSATING ROLLBACK: Restore resting SL quantity back to full RemainingQty()
		fullRem := w.RemainingQty()
		if fullRem > 0 && w.modCount.Load() < 24 {
			if rbErr := w.broker.ModifySLOrder(w.ctx, slOrdID, fullRem, curStop, curLimit, w.LatestLTP(), w.TickSize); rbErr != nil {
				w.executeEmergencyFlatten("SCALE_OUT_ROLLBACK_FAILED")
				return
			}
			w.modCount.Add(1)
		}
		w.mu.Lock()
		w.state = StateProtectedActive
		w.mu.Unlock()
		w.tranche1Pending.Store(false)
		return
	}

	w.mu.Lock()
	w.scaleOutOrderID = scaleOrdID
	w.mu.Unlock()
	w.reconciler.RegisterOrder(scaleOrdID, w.PositionID, "SCALE_OUT", w.Symbol, "SELL", "LIMIT", scaleOutQty, ltp, 0, OrderStatusOpen)

	// Wait up to 800ms for scale-out fill confirmation
	waitTimer := time.NewTimer(800 * time.Millisecond)
	select {
	case <-w.fillNotifyCh:
	case <-waitTimer.C:
		_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)
	}
	waitTimer.Stop()

	// Falsification Guard (Bug #14): If scale-out limit order did not fully fill within 800ms,
	// cancel the unfilled remainder and reconcile before resizing the resting SL order so both cannot fill!
	if w.RemainingQty() > runnerQty && scaleOrdID != "" {
		_ = w.broker.CancelOrder(w.ctx, scaleOrdID)
		_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)
	}

	remAfter := w.RemainingQty()
	if remAfter <= 0 || w.isClosed.Load() {
		return
	}

	// STEP 3: Raise the remaining runner's SL trigger to Tax-Free Breakeven (Entry + Taxes + 0.15%)
	beTrigger := BreakevenTriggerPrice(w.GetEntryPrice(), int(w.filledEntryQty.Load()), remAfter, w.TickSize)
	w.breakevenStopBits.Store(math.Float64bits(beTrigger))
	w.tranche1Done.Store(true)
	w.tranche1Pending.Store(false)
	w.currentGear.Store(2)

	w.mu.Lock()
	w.BreakevenStop = beTrigger
	w.state = StateRunnerActive
	w.mu.Unlock()

	nowLTP := w.LatestLTP()
	if nowLTP <= beTrigger+2.0*w.TickSize {
		// Price whipped back down to Breakeven during scale-out -> Flatten runner to lock net profit!
		w.executeEmergencyFlatten("RUNNER_BREAKEVEN_LOCK")
		return
	}

	if beTrigger > curStop {
		// Always record beTrigger in shadowStopBits & proposedStopBits so if ModifySLOrder is transiently
		// rejected right now, processPendingWork() ratchets the SL to beTrigger on the next eligible tick.
		w.shadowStopBits.Store(math.Float64bits(beTrigger))
		w.proposedStopBits.Store(math.Float64bits(beTrigger))
		if w.modCount.Load() < 24 {
			beLimit := computeSellSLLimitPrice(beTrigger, w.TickSize)
			if err := w.broker.ModifySLOrder(w.ctx, slOrdID, remAfter, beTrigger, beLimit, nowLTP, w.TickSize); err == nil {
				w.modCount.Add(1)
				w.lastModUnixMs.Store(time.Now().UnixMilli())
				w.confirmedStopBits.Store(math.Float64bits(beTrigger))
			}
		}
	}
	w.persistPositionState()
}

// executeEmergencyFlatten runs the 3-Stage Bulletproof Exit Protocol until RemainingQty() == 0
func (w *PositionWorker) executeEmergencyFlatten(reason string) {
	if w.isClosed.Load() {
		return
	}
	w.pendingMarketExit.Store(true)

	w.mu.Lock()
	if w.exitReason == "" {
		w.exitReason = reason
	}
	w.state = StateEmergencyFlatten
	slOrdID := w.slOrderID
	entryOrdID := w.entryOrderID
	w.mu.Unlock()

	w.persistPositionState()

	// If entry order is still open/partially filled, cancel it first
	if entryOrdID != "" && int(w.filledEntryQty.Load()) < w.InitialQty {
		_ = w.broker.CancelOrder(w.ctx, entryOrdID)
	}

	rem := w.RemainingQty()
	if rem <= 0 {
		_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)
		rem = w.RemainingQty()
		if rem <= 0 {
			w.mu.Lock()
			w.finalizeClosedLocked(reason)
			w.mu.Unlock()
			w.persistPositionState()
			if w.onClosed != nil {
				w.onClosed(w)
			}
			return
		}
	}

	var stockPtr *engine.StockMetrics
	if w.getStockPtr != nil {
		stockPtr = w.getStockPtr()
	}

	// STAGE A: Mutate resting SL order directly into MARKET (Uses Modification #25 or remaining slot)
	if slOrdID != "" && w.modCount.Load() < MaxExchangeOrderMods {
		if err := w.broker.MutateSLToMarket(w.ctx, slOrdID, rem, stockPtr); err == nil {
			w.modCount.Add(1)
			waitA := time.NewTimer(900 * time.Millisecond)
			select {
			case <-w.fillNotifyCh:
			case <-waitA.C:
				_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)
			}
			waitA.Stop()
			if w.RemainingQty() <= 0 || w.isClosed.Load() {
				return
			}
		} else {
			slog.Warn("Stage A MutateSLToMarket failed, escalating to Stage B Cancel+Sweep",
				"symbol", w.Symbol, "error", err)
		}
	}

	// STAGE B & C: Cancel Resting SL -> Reconcile -> Escalating LPP Sweep (0.75% -> 1.50% -> 2.50% -> 3.50%/Market)
	if slOrdID != "" {
		_ = w.broker.CancelOrder(w.ctx, slOrdID)
	}
	_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)

	slippageSchedule := [4]float64{0.0075, 0.0150, 0.0250, 0.0350}
	for attempt := 1; attempt <= 4; attempt++ {
		rem = w.RemainingQty()
		if rem <= 0 || w.isClosed.Load() {
			return
		}

		ltp := w.LatestLTP()
		if stockPtr != nil && stockPtr.LTP > 0 {
			ltp = stockPtr.LTP
		}
		slipPct := slippageSchedule[attempt-1]
		emergOrdID, err := w.broker.PlaceEmergencySweepSell(w.ctx, w.Symbol, w.Token, rem, ltp, w.TickSize, slipPct, stockPtr)
		if err == nil && emergOrdID != "" {
			w.reconciler.RegisterOrder(emergOrdID, w.PositionID, "EMERGENCY_EXIT", w.Symbol, "SELL", "LIMIT", rem, ltp, 0, OrderStatusOpen)
		}

		waitB := time.NewTimer(900 * time.Millisecond)
		select {
		case <-w.fillNotifyCh:
		case <-waitB.C:
			_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)
		}
		waitB.Stop()

		if w.RemainingQty() <= 0 || w.isClosed.Load() {
			return
		}
		// Falsification Guard (Bug #15): Cancel any unfilled portion of emergOrdID and reconcile
		// before placing the next retry attempt so two emergency sell orders are never open simultaneously!
		if emergOrdID != "" {
			_ = w.broker.CancelOrder(w.ctx, emergOrdID)
			_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)
		}
	}

	// STAGE D: Persistent Anti-Orphan Recovery Loop (Bug B2 Fix)
	// If all 4 rapid attempts failed (e.g., temporary air-pocket, lower circuit freeze, or transient API error),
	// NEVER return and abandon a live position with no resting SL order! Keep retrying every 2s until RemainingQty == 0.
	slog.Warn("[CRITICAL OMS RECOVERY] Position still open after 4 emergency sweep attempts; entering persistent 2s recovery loop",
		"position_id", w.PositionID, "symbol", w.Symbol, "remaining_qty", w.RemainingQty())

	recoveryTicker := time.NewTicker(2 * time.Second)
	defer recoveryTicker.Stop()

	for {
		if w.RemainingQty() <= 0 || w.isClosed.Load() {
			return
		}
		select {
		case <-w.ctx.Done():
			return
		case <-w.fillNotifyCh:
			if w.RemainingQty() <= 0 || w.isClosed.Load() {
				return
			}
		case <-recoveryTicker.C:
			_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)
			rem = w.RemainingQty()
			if rem <= 0 || w.isClosed.Load() {
				return
			}
			ltp := w.LatestLTP()
			if stockPtr != nil && stockPtr.LTP > 0 {
				ltp = stockPtr.LTP
			}
			recOrdID, err := w.broker.PlaceEmergencySweepSell(w.ctx, w.Symbol, w.Token, rem, ltp, w.TickSize, 0.0350, stockPtr)
			if err == nil && recOrdID != "" {
				w.reconciler.RegisterOrder(recOrdID, w.PositionID, "EMERGENCY_EXIT", w.Symbol, "SELL", "LIMIT", rem, ltp, 0, OrderStatusOpen)
			}
			waitRec := time.NewTimer(900 * time.Millisecond)
			select {
			case <-w.fillNotifyCh:
			case <-waitRec.C:
				_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)
			}
			waitRec.Stop()
			if w.RemainingQty() <= 0 || w.isClosed.Load() {
				return
			}
			if recOrdID != "" {
				_ = w.broker.CancelOrder(w.ctx, recOrdID)
				_, _ = w.reconciler.SyncOrdersCoalesced(w.ctx, w.broker)
			}
		}
	}
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
