package trading

import (
	"log/slog"
	"math"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/diagnostics"
	"github.com/ajaynegi33/AlgoTrade/internal/market"
)

// TriggerEmergencyExit transitions immediately to market flatten (non-blocking from hot path)
func (worker *PositionWorker) TriggerEmergencyExit(reason string) {
	if worker.isClosed.Load() {
		return
	}
	if worker.isMarketExitPending.CompareAndSwap(false, true) {
		select {
		case worker.emergReasonCh <- reason:
		default:
		}
		worker.wakeWorker()
	}
}

// ForceEmergencyExit unconditionally forces an emergency exit, even if a previous exit attempt was pending
func (worker *PositionWorker) ForceEmergencyExit(reason string) {
	if worker.isClosed.Load() {
		return
	}
	worker.isMarketExitPending.Store(true)
	select {
	case worker.emergReasonCh <- reason:
		worker.wakeWorker()
	default:
		// If emergReasonCh is already full or workerLoop has terminated, execute emergency flatten directly
		go worker.executeEmergencyFlatten(reason)
	}
}

func (worker *PositionWorker) processPendingWork() {
	if worker.isClosed.Load() || worker.isMarketExitPending.Load() {
		return
	}

	// 1. Ensure initial SL is placed as soon as entry fills (full or partial)
	if worker.filledEntryQty.Load() > 0 {
		worker.mu.RLock()
		hasSL := worker.slOrderID != ""
		slQty := worker.slOrderQty
		worker.mu.RUnlock()
		filledEntry := int(worker.filledEntryQty.Load())
		if !hasSL {
			worker.ensureInitialSLPlaced()
			if worker.isClosed.Load() || worker.isMarketExitPending.Load() {
				return
			}
		} else if filledEntry > slQty && worker.state != StateScaleOutPending && !worker.hasTranche1Completed.Load() && worker.modCount.Load() < MaxSafetyModifications {
			// Incremental fill arrived while SL is active: resize SL to cover all filled shares
			curStop := math.Float64frombits(worker.confirmedStopBits.Load())
			limitPx := ComputeSellSLLimitPrice(curStop, worker.TickSize)
			ltp := worker.LatestLTP()
			worker.mu.RLock()
			slID := worker.slOrderID
			worker.mu.RUnlock()
			if slID != "" {
				if err := worker.broker.ModifySLOrder(worker.ctx, slID, filledEntry, curStop, limitPx, ltp, worker.TickSize); err == nil {
					worker.mu.Lock()
					worker.slOrderQty = filledEntry
					worker.mu.Unlock()
					worker.modCount.Add(1)
					worker.lastModUnixMs.Store(time.Now().UnixMilli())
				}
			}
		}
	}

	// 2. Execute +1.0R 3-Step Race-Free Scale-Out if triggered
	if worker.isTranche1Pending.Load() && !worker.hasTranche1Completed.Load() {
		worker.executeThreeStepScaleOut()
		if worker.isClosed.Load() || worker.isMarketExitPending.Load() {
			return
		}
	}

	// 3. Apply pending Stop Ratchet if proposedStop > confirmedStop and modCount < 24
	worker.mu.RLock()
	slOrdID := worker.slOrderID
	worker.mu.RUnlock()
	if slOrdID == "" || worker.modCount.Load() >= MaxSafetyModifications {
		return
	}

	proposed := math.Float64frombits(worker.proposedStopBits.Load())
	confirmed := math.Float64frombits(worker.confirmedStopBits.Load())
	if proposed <= confirmed {
		return
	}

	rem := worker.RemainingQty()
	if rem <= 0 {
		return
	}

	ltp := worker.LatestLTP()
	maxLegal := RoundDownToTick(ltp-math.Max(2.0*worker.TickSize, 0.001*ltp), worker.TickSize)
	targetTrigger := math.Min(proposed, maxLegal)
	if targetTrigger <= confirmed {
		return
	}

	limitPx := ComputeSellSLLimitPrice(targetTrigger, worker.TickSize)
	err := worker.broker.ModifySLOrder(worker.ctx, slOrdID, rem, targetTrigger, limitPx, ltp, worker.TickSize)
	if err != nil {
		slog.Warn("SL ratchet modification rejected by broker", "symbol", worker.Symbol, "target", targetTrigger, "error", err)
		return
	}

	newMods := worker.modCount.Add(1)
	worker.lastModUnixMs.Store(time.Now().UnixMilli())
	worker.confirmedStopBits.Store(math.Float64bits(targetTrigger))

	if newMods >= MaxSafetyModifications {
		worker.currentGear.Store(GearShadowExit)
		worker.mu.Lock()
		worker.state = StateShadowStopActive
		worker.mu.Unlock()
	} else if newMods >= 18 {
		worker.currentGear.Store(GearProfitLock)
	}
	worker.persistPositionState()
}

func (worker *PositionWorker) ensureInitialSLPlaced() {
	worker.mu.RLock()
	if worker.slOrderID != "" || worker.isClosed.Load() {
		worker.mu.RUnlock()
		return
	}
	worker.mu.RUnlock()

	rem := worker.RemainingQty()
	if rem <= 0 {
		return
	}

	ltp := worker.LatestLTP()
	// Guard: If price already crashed below InitialStop during entry fill, flatten immediately
	if ltp > 0 && ltp <= worker.InitialStop+worker.TickSize {
		slog.Warn("LTP already at or below InitialStop upon entry fill; executing immediate flatten",
			"symbol", worker.Symbol, "ltp", ltp, "initial_stop", worker.InitialStop)
		worker.executeEmergencyFlatten("INITIAL_SL_BREACHED_ON_FILL")
		return
	}

	limitPx := ComputeSellSLLimitPrice(worker.InitialStop, worker.TickSize)
	slID, err := worker.broker.PlaceSLOrder(worker.ctx, worker.Symbol, worker.Token, rem, worker.InitialStop, limitPx, ltp, worker.TickSize)
	if err != nil {
		slog.Error("Initial SL placement failed; executing immediate protective flatten",
			"symbol", worker.Symbol, "error", err)
		worker.executeEmergencyFlatten("INITIAL_SL_REJECTED_BY_OMS")
		return
	}

	worker.mu.Lock()
	worker.slOrderID = slID
	worker.slOrderStatus = OrderStatusTrigPending
	worker.slOrderQty = rem
	worker.state = StateProtectedActive
	worker.mu.Unlock()

	worker.reconciler.RegisterOrder(slID, worker.PositionID, "SL", worker.Symbol, "SELL", "SL", rem, limitPx, worker.InitialStop, OrderStatusTrigPending)
	worker.persistPositionState()
}

// executeThreeStepScaleOut performs the Race-Free 3-Step Scale-Out at +1.0R with Compensating Rollback
func (worker *PositionWorker) executeThreeStepScaleOut() {
	// Guard against racing entry limit buy: if entry is still resting, cancel it and freeze InitialQty
	worker.mu.RLock()
	entryOrdID := worker.entryOrderID
	initQty := worker.InitialQty
	worker.mu.RUnlock()
	filledEntry := int(worker.filledEntryQty.Load())
	if entryOrdID != "" && filledEntry < initQty {
		_ = worker.broker.CancelOrder(worker.ctx, entryOrdID)
		_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
		filledEntry = int(worker.filledEntryQty.Load())
		worker.mu.Lock()
		worker.InitialQty = filledEntry
		worker.mu.Unlock()
	}

	rem := worker.RemainingQty()
	if rem < 2 {
		// Position is 1 share: cannot split; lock Breakeven stop directly
		worker.hasTranche1Completed.Store(true)
		worker.isTranche1Pending.Store(false)
		worker.currentGear.Store(GearDynamicTrail)
		worker.ProposeStopRatchet(worker.BreakevenStop, worker.OneR, true)
		return
	}

	worker.mu.Lock()
	slOrdID := worker.slOrderID
	worker.state = StateScaleOutPending
	worker.mu.Unlock()

	if slOrdID == "" {
		worker.ensureInitialSLPlaced()
		worker.mu.RLock()
		slOrdID = worker.slOrderID
		worker.mu.RUnlock()
		if slOrdID == "" {
			return
		}
	}

	if rem <= 1 {
		// 1-share position cannot be halved (rem / 2 = 0).
		// Mark Tranche 1 as completed so it does not loop, and let the 1 share ride with the runner trailing stop.
		worker.hasTranche1Completed.Store(true)
		worker.isTranche1Pending.Store(false)
		worker.mu.Lock()
		worker.state = StateProtectedActive
		worker.mu.Unlock()

		// Ensure stop is immediately ratcheted to Tax-Free Breakeven if not already higher
		curStop := math.Float64frombits(worker.confirmedStopBits.Load())
		beStop := worker.GetBreakevenStop()
		ltp := worker.LatestLTP()
		if beStop > curStop && worker.modCount.Load() < MaxSafetyModifications {
			beLimit := ComputeSellSLLimitPrice(beStop, worker.TickSize)
			if err := worker.broker.ModifySLOrder(worker.ctx, slOrdID, rem, beStop, beLimit, ltp, worker.TickSize); err == nil {
				worker.modCount.Add(1)
				worker.lastModUnixMs.Store(time.Now().UnixMilli())
				worker.confirmedStopBits.Store(math.Float64bits(beStop))
				worker.shadowStopBits.Store(math.Float64bits(beStop))
			}
		}
		worker.persistPositionState()
		return
	}

	scaleOutQty := rem / 2
	runnerQty := rem - scaleOutQty
	curStop := math.Float64frombits(worker.confirmedStopBits.Load())
	curLimit := ComputeSellSLLimitPrice(curStop, worker.TickSize)
	ltp := worker.LatestLTP()

	// STEP 1: Shrink resting SL quantity from rem -> runnerQty while KEEPING curStop trigger unchanged
	// (Prevents resting SL from firing at Breakeven during the 3ms window while Step 2 is in flight!)
	if worker.modCount.Load() >= MaxSafetyModifications {
		worker.isTranche1Pending.Store(false)
		return
	}
	if err := worker.broker.ModifySLOrder(worker.ctx, slOrdID, runnerQty, curStop, curLimit, ltp, worker.TickSize); err != nil {
		slog.Warn("Scale-out Step 1 (shrink SL qty) failed; keeping 100% SL intact", "symbol", worker.Symbol, "error", err)
		worker.mu.Lock()
		worker.state = StateProtectedActive
		worker.mu.Unlock()
		worker.isTranche1Pending.Store(false)
		return
	}
	worker.mu.Lock()
	worker.slOrderQty = runnerQty
	worker.mu.Unlock()
	worker.modCount.Add(1)
	worker.lastModUnixMs.Store(time.Now().UnixMilli())

	// STEP 2: Place Protected Marketable-Limit Sell for scaleOutQty (50%)
	var stockPtr *market.StockMetrics
	if worker.getStockPtr != nil {
		stockPtr = worker.getStockPtr()
	}
	scaleOrdID, sellErr := worker.broker.PlaceProtectedMarketSell(worker.ctx, worker.Symbol, worker.Token, scaleOutQty, ltp, worker.TickSize, stockPtr)
	if sellErr != nil {
		slog.Error("Scale-out Step 2 sell failed; rolling back resting SL to full quantity", "symbol", worker.Symbol, "error", sellErr)
		// COMPENSATING ROLLBACK: Restore resting SL quantity back to full RemainingQty()
		fullRem := worker.RemainingQty()
		if fullRem > 0 && worker.modCount.Load() < MaxSafetyModifications {
			if rbErr := worker.broker.ModifySLOrder(worker.ctx, slOrdID, fullRem, curStop, curLimit, worker.LatestLTP(), worker.TickSize); rbErr != nil {
				worker.executeEmergencyFlatten("SCALE_OUT_ROLLBACK_FAILED")
				return
			}
			worker.mu.Lock()
			worker.slOrderQty = fullRem
			worker.mu.Unlock()
			worker.modCount.Add(1)
		}
		worker.mu.Lock()
		worker.state = StateProtectedActive
		worker.mu.Unlock()
		worker.isTranche1Pending.Store(false)
		return
	}

	worker.mu.Lock()
	worker.scaleOutOrderID = scaleOrdID
	worker.mu.Unlock()
	worker.reconciler.RegisterOrder(scaleOrdID, worker.PositionID, "SCALE_OUT", worker.Symbol, "SELL", "LIMIT", scaleOutQty, ltp, 0, OrderStatusOpen)

	// Wait up to 800ms for scale-out fill confirmation
	waitTimer := time.NewTimer(800 * time.Millisecond)
	select {
	case <-worker.fillNotifyCh:
	case <-waitTimer.C:
		_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
	}
	waitTimer.Stop()

	// Falsification Guard (Bug #14): If scale-out limit order did not fully fill within 800ms,
	// cancel the unfilled remainder and reconcile before resizing the resting SL order so both cannot fill!
	if worker.RemainingQty() > runnerQty && scaleOrdID != "" {
		_ = worker.broker.CancelOrder(worker.ctx, scaleOrdID)
		_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
	}

	remAfter := worker.RemainingQty()
	if remAfter <= 0 || worker.isClosed.Load() {
		return
	}

	// STEP 3: Raise the remaining runner's SL trigger to Tax-Free Breakeven (Entry + Taxes + 0.15%)
	beTrigger := BreakevenTriggerPrice(worker.GetEntryPrice(), int(worker.filledEntryQty.Load()), remAfter, worker.TickSize)
	worker.breakevenStopBits.Store(math.Float64bits(beTrigger))
	worker.hasTranche1Completed.Store(true)
	worker.isTranche1Pending.Store(false)
	worker.currentGear.Store(GearDynamicTrail)

	worker.mu.Lock()
	worker.BreakevenStop = beTrigger
	worker.state = StateRunnerActive
	worker.mu.Unlock()

	nowLTP := worker.LatestLTP()
	if nowLTP <= beTrigger+2.0*worker.TickSize {
		// Price whipped back down to Breakeven during scale-out -> Flatten runner to lock net profit!
		worker.executeEmergencyFlatten("RUNNER_BREAKEVEN_LOCK")
		return
	}

	if beTrigger > curStop {
		// Always record beTrigger in shadowStopBits & proposedStopBits so if ModifySLOrder is transiently
		// rejected right now, processPendingWork() ratchets the SL to beTrigger on the next eligible tick.
		worker.shadowStopBits.Store(math.Float64bits(beTrigger))
		worker.proposedStopBits.Store(math.Float64bits(beTrigger))
		if worker.modCount.Load() < MaxSafetyModifications {
			beLimit := ComputeSellSLLimitPrice(beTrigger, worker.TickSize)
			if err := worker.broker.ModifySLOrder(worker.ctx, slOrdID, remAfter, beTrigger, beLimit, nowLTP, worker.TickSize); err == nil {
				worker.modCount.Add(1)
				worker.lastModUnixMs.Store(time.Now().UnixMilli())
				worker.confirmedStopBits.Store(math.Float64bits(beTrigger))
				worker.mu.Lock()
				worker.slOrderQty = remAfter
				worker.mu.Unlock()
			}
		}
	}
	worker.persistPositionState()
}

// executeEmergencyFlatten runs the 3-Stage Bulletproof Exit Protocol until RemainingQty() == 0
func (worker *PositionWorker) executeEmergencyFlatten(reason string) {
	emergencyFlattenStartTime := time.Now()
	defer func() {
		flattenElapsedDuration := time.Since(emergencyFlattenStartTime)
		diagnostics.RecordLatency("emergency_position_flatten", flattenElapsedDuration, 800*time.Millisecond)
	}()

	if worker.isClosed.Load() {
		return
	}
	worker.isMarketExitPending.Store(true)

	worker.mu.Lock()
	if worker.exitReason == "" {
		worker.exitReason = reason
	}
	worker.state = StateEmergencyFlatten
	slOrdID := worker.slOrderID
	entryOrdID := worker.entryOrderID
	worker.mu.Unlock()

	worker.persistPositionState()

	// If entry order is still open/partially filled, cancel it first
	if entryOrdID != "" && int(worker.filledEntryQty.Load()) < worker.InitialQty {
		_ = worker.broker.CancelOrder(worker.ctx, entryOrdID)
	}

	rem := worker.RemainingQty()
	if rem <= 0 {
		_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
		rem = worker.RemainingQty()
		if rem <= 0 {
			worker.mu.Lock()
			worker.finalizeClosedLocked(reason)
			worker.mu.Unlock()
			worker.persistPositionState()
			worker.notifyClosed()
			return
		}
	}

	var stockPtr *market.StockMetrics
	if worker.getStockPtr != nil {
		stockPtr = worker.getStockPtr()
	}

	// STAGE A: Mutate resting SL order directly into MARKET (Uses Modification #25 or remaining slot)
	if slOrdID != "" && worker.modCount.Load() < MaxExchangeOrderMods {
		if err := worker.broker.MutateSLToMarket(worker.ctx, slOrdID, rem, stockPtr); err == nil {
			worker.modCount.Add(1)
			waitA := time.NewTimer(900 * time.Millisecond)
			select {
			case <-worker.fillNotifyCh:
			case <-waitA.C:
				_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
			}
			waitA.Stop()
			if worker.RemainingQty() <= 0 || worker.isClosed.Load() {
				return
			}
		} else {
			slog.Warn("Stage A MutateSLToMarket failed, escalating to Stage B Cancel+Sweep",
				"symbol", worker.Symbol, "error", err)
		}
	}

	// STAGE B & C: Cancel Resting SL -> Reconcile -> Escalating LPP Sweep (0.75% -> 1.50% -> 2.50% -> 3.50%/Market)
	if slOrdID != "" {
		_ = worker.broker.CancelOrder(worker.ctx, slOrdID)
	}
	_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)

	slippageSchedule := [4]float64{0.0075, 0.0150, 0.0250, 0.0350}
	for attempt := 1; attempt <= 4; attempt++ {
		rem = worker.RemainingQty()
		if rem <= 0 || worker.isClosed.Load() {
			return
		}

		ltp := worker.LatestLTP()
		if stockPtr != nil && stockPtr.LTP > 0 {
			ltp = stockPtr.LTP
		}
		slipPct := slippageSchedule[attempt-1]
		emergOrdID, err := worker.broker.PlaceEmergencySweepSell(worker.ctx, worker.Symbol, worker.Token, rem, ltp, worker.TickSize, slipPct, stockPtr)
		if err == nil && emergOrdID != "" {
			worker.reconciler.RegisterOrder(emergOrdID, worker.PositionID, "EMERGENCY_EXIT", worker.Symbol, "SELL", "LIMIT", rem, ltp, 0, OrderStatusOpen)
		}

		waitB := time.NewTimer(900 * time.Millisecond)
		select {
		case <-worker.fillNotifyCh:
		case <-waitB.C:
			_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
		}
		waitB.Stop()

		if worker.RemainingQty() <= 0 || worker.isClosed.Load() {
			return
		}
		// Falsification Guard (Bug #15): Cancel any unfilled portion of emergOrdID and reconcile
		// before placing the next retry attempt so two emergency sell orders are never open simultaneously!
		if emergOrdID != "" {
			_ = worker.broker.CancelOrder(worker.ctx, emergOrdID)
			_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
		}
	}

	// STAGE D: Persistent Anti-Orphan Recovery Loop (Bug B2 Fix)
	// If all 4 rapid attempts failed (e.g., temporary air-pocket, lower circuit freeze, or transient API error),
	// NEVER return and abandon a live position with no resting SL order! Keep retrying every 2s until RemainingQty == 0.
	slog.Warn("[CRITICAL OMS RECOVERY] Position still open after 4 emergency sweep attempts; entering persistent 2s recovery loop",
		"position_id", worker.PositionID, "symbol", worker.Symbol, "remaining_qty", worker.RemainingQty())

	recoveryTicker := time.NewTicker(2 * time.Second)
	defer recoveryTicker.Stop()

	for {
		if worker.RemainingQty() <= 0 || worker.isClosed.Load() {
			return
		}
		select {
		case <-worker.ctx.Done():
			return
		case <-worker.fillNotifyCh:
			if worker.RemainingQty() <= 0 || worker.isClosed.Load() {
				return
			}
		case <-recoveryTicker.C:
			_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
			rem = worker.RemainingQty()
			if rem <= 0 || worker.isClosed.Load() {
				return
			}
			ltp := worker.LatestLTP()
			if stockPtr != nil && stockPtr.LTP > 0 {
				ltp = stockPtr.LTP
			}
			recOrdID, err := worker.broker.PlaceEmergencySweepSell(worker.ctx, worker.Symbol, worker.Token, rem, ltp, worker.TickSize, 0.0350, stockPtr)
			if err == nil && recOrdID != "" {
				worker.reconciler.RegisterOrder(recOrdID, worker.PositionID, "EMERGENCY_EXIT", worker.Symbol, "SELL", "LIMIT", rem, ltp, 0, OrderStatusOpen)
			}
			waitRec := time.NewTimer(900 * time.Millisecond)
			select {
			case <-worker.fillNotifyCh:
			case <-waitRec.C:
				_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
			}
			waitRec.Stop()
			if worker.RemainingQty() <= 0 || worker.isClosed.Load() {
				return
			}
			if recOrdID != "" {
				_ = worker.broker.CancelOrder(worker.ctx, recOrdID)
				_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
			}
		}
	}
}

