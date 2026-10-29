package execution

import (
	"log/slog"
	"math"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/engine"
)

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
	if slOrdID == "" || w.modCount.Load() >= MaxSafetyModifications {
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

	if newMods >= MaxSafetyModifications {
		w.currentGear.Store(GearShadowExit)
		w.mu.Lock()
		w.state = StateShadowStopActive
		w.mu.Unlock()
	} else if newMods >= 18 {
		w.currentGear.Store(GearProfitLock)
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
		w.currentGear.Store(GearDynamicTrail)
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
	if w.modCount.Load() >= MaxSafetyModifications {
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
		if fullRem > 0 && w.modCount.Load() < MaxSafetyModifications {
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
	w.currentGear.Store(GearDynamicTrail)

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
		if w.modCount.Load() < MaxSafetyModifications {
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
			w.notifyClosed()
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

