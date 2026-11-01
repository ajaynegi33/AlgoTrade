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
		// If emergReasonCh is already full or workerLoop has terminated, execute emergency exit directly
		go worker.EmergencySellAllAndClosePosition(reason)
	}
}

func (worker *PositionWorker) processPendingWork() {
	if worker.isClosed.Load() || worker.isMarketExitPending.Load() {
		return
	}

	// 1. Ensure initial SL is placed as soon as entry fills (full or partial)
	if worker.filledEntryQty.Load() > 0 {
		worker.mu.RLock()
		hasStopLossOrder := worker.slOrderID != ""
		stopLossOrderQuantity := worker.slOrderQty
		worker.mu.RUnlock()
		filledEntryQuantity := int(worker.filledEntryQty.Load())
		if !hasStopLossOrder {
			worker.PlaceInitialStopLossOrderIfNotPlaced()
			if worker.isClosed.Load() || worker.isMarketExitPending.Load() {
				return
			}
		} else if filledEntryQuantity > stopLossOrderQuantity && worker.state != StateScaleOutPending && !worker.hasTranche1Completed.Load() && worker.modCount.Load() < MaxSafetyModifications {
			// Incremental fill arrived while SL is active: resize SL to cover all filled shares
			confirmedStopLossPrice := math.Float64frombits(worker.confirmedStopBits.Load())
			sellLimitPrice := CalculateProtectiveLimitPriceBelowStop(confirmedStopLossPrice, worker.TickSize)
			lastTradedPrice := worker.LatestLTP()
			worker.mu.RLock()
			stopLossOrderID := worker.slOrderID
			worker.mu.RUnlock()
			if stopLossOrderID != "" {
				if err := worker.broker.ModifySLOrder(worker.ctx, stopLossOrderID, filledEntryQuantity, confirmedStopLossPrice, sellLimitPrice, lastTradedPrice, worker.TickSize); err == nil {
					worker.mu.Lock()
					worker.slOrderQty = filledEntryQuantity
					worker.mu.Unlock()
					worker.modCount.Add(1)
					worker.lastModUnixMs.Store(time.Now().UnixMilli())
				}
			}
		}
	}

	// 2. Execute +1.0R 3-Step Race-Free Scale-Out if triggered
	if worker.isTranche1Pending.Load() && !worker.hasTranche1Completed.Load() {
		worker.SellHalfSharesAtProfitTarget()
		if worker.isClosed.Load() || worker.isMarketExitPending.Load() {
			return
		}
	}

	// 3. Apply pending Stop Ratchet if proposedStop > confirmedStop and modCount < 24
	worker.mu.RLock()
	stopLossOrderID := worker.slOrderID
	worker.mu.RUnlock()
	if stopLossOrderID == "" || worker.modCount.Load() >= MaxSafetyModifications {
		return
	}

	proposedStopPrice := math.Float64frombits(worker.proposedStopBits.Load())
	confirmedStopPrice := math.Float64frombits(worker.confirmedStopBits.Load())
	if proposedStopPrice <= confirmedStopPrice {
		return
	}

	remainingQuantity := worker.RemainingQty()
	if remainingQuantity <= 0 {
		return
	}

	lastTradedPrice := worker.LatestLTP()
	maxLegalTrigger := RoundDownToTick(lastTradedPrice-math.Max(2.0*worker.TickSize, 0.001*lastTradedPrice), worker.TickSize)
	targetTriggerPrice := math.Min(proposedStopPrice, maxLegalTrigger)
	if targetTriggerPrice <= confirmedStopPrice {
		return
	}

	sellLimitPrice := CalculateProtectiveLimitPriceBelowStop(targetTriggerPrice, worker.TickSize)
	err := worker.broker.ModifySLOrder(worker.ctx, stopLossOrderID, remainingQuantity, targetTriggerPrice, sellLimitPrice, lastTradedPrice, worker.TickSize)
	if err != nil {
		slog.Warn("SL ratchet modification rejected by broker", "symbol", worker.Symbol, "target", targetTriggerPrice, "error", err)
		return
	}

	newModificationCount := worker.modCount.Add(1)
	worker.lastModUnixMs.Store(time.Now().UnixMilli())
	worker.confirmedStopBits.Store(math.Float64bits(targetTriggerPrice))

	if newModificationCount >= MaxSafetyModifications {
		worker.currentGear.Store(GearShadowExit)
		worker.mu.Lock()
		worker.state = StateShadowStopActive
		worker.mu.Unlock()
	} else if newModificationCount >= 18 {
		worker.currentGear.Store(GearProfitLock)
	}
	worker.persistPositionState()
}

func (worker *PositionWorker) PlaceInitialStopLossOrderIfNotPlaced() {
	worker.mu.RLock()
	if worker.slOrderID != "" || worker.isClosed.Load() {
		worker.mu.RUnlock()
		return
	}
	worker.mu.RUnlock()

	remainingQuantity := worker.RemainingQty()
	if remainingQuantity <= 0 {
		return
	}

	lastTradedPrice := worker.LatestLTP()
	// Guard: If price already crashed below InitialStop during entry fill, flatten immediately
	if lastTradedPrice > 0 && lastTradedPrice <= worker.InitialStop+worker.TickSize {
		slog.Warn("LTP already at or below InitialStop upon entry fill; executing immediate flatten",
			"symbol", worker.Symbol, "ltp", lastTradedPrice, "initial_stop", worker.InitialStop)
		worker.EmergencySellAllAndClosePosition("INITIAL_SL_BREACHED_ON_FILL")
		return
	}

	sellLimitPrice := CalculateProtectiveLimitPriceBelowStop(worker.InitialStop, worker.TickSize)
	stopLossOrderID, err := worker.broker.PlaceSLOrder(worker.ctx, worker.Symbol, worker.Token, remainingQuantity, worker.InitialStop, sellLimitPrice, lastTradedPrice, worker.TickSize)
	if err != nil {
		slog.Error("Initial SL placement failed; executing immediate protective flatten",
			"symbol", worker.Symbol, "error", err)
		worker.EmergencySellAllAndClosePosition("INITIAL_SL_REJECTED_BY_OMS")
		return
	}

	worker.mu.Lock()
	worker.slOrderID = stopLossOrderID
	worker.slOrderStatus = OrderStatusTrigPending
	worker.slOrderQty = remainingQuantity
	worker.state = StateProtectedActive
	worker.mu.Unlock()

	worker.reconciler.RegisterOrder(stopLossOrderID, worker.PositionID, "SL", worker.Symbol, "SELL", "SL", remainingQuantity, sellLimitPrice, worker.InitialStop, OrderStatusTrigPending)
	worker.persistPositionState()
}

// SellHalfSharesAtProfitTarget performs the Race-Free 3-Step Scale-Out at +1.0R with Compensating Rollback
func (worker *PositionWorker) SellHalfSharesAtProfitTarget() {
	// Guard against racing entry limit buy: if entry is still resting, cancel it and freeze InitialQty
	worker.mu.RLock()
	entryOrderID := worker.entryOrderID
	initialQuantity := worker.InitialQty
	worker.mu.RUnlock()
	filledEntryQuantity := int(worker.filledEntryQty.Load())
	if entryOrderID != "" && filledEntryQuantity < initialQuantity {
		_ = worker.broker.CancelOrder(worker.ctx, entryOrderID)
		_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
		filledEntryQuantity = int(worker.filledEntryQty.Load())
		worker.mu.Lock()
		worker.InitialQty = filledEntryQuantity
		worker.mu.Unlock()
	}

	remainingQuantity := worker.RemainingQty()
	if remainingQuantity < 2 {
		// Position is 1 share: cannot split; lock Breakeven stop directly
		worker.hasTranche1Completed.Store(true)
		worker.isTranche1Pending.Store(false)
		worker.currentGear.Store(GearDynamicTrail)
		worker.RaiseStopLossOrder(worker.BreakevenStop, worker.OneR, true)
		return
	}

	worker.mu.Lock()
	stopLossOrderID := worker.slOrderID
	worker.state = StateScaleOutPending
	worker.mu.Unlock()

	if stopLossOrderID == "" {
		worker.PlaceInitialStopLossOrderIfNotPlaced()
		worker.mu.RLock()
		stopLossOrderID = worker.slOrderID
		worker.mu.RUnlock()
		if stopLossOrderID == "" {
			return
		}
	}

	scaleOutQty := remainingQuantity / 2
	runnerQty := remainingQuantity - scaleOutQty
	confirmedStopLossPrice := math.Float64frombits(worker.confirmedStopBits.Load())
	currentLimitPrice := CalculateProtectiveLimitPriceBelowStop(confirmedStopLossPrice, worker.TickSize)
	lastTradedPrice := worker.LatestLTP()

	// STEP 1: Shrink resting SL quantity from remainingQuantity -> runnerQty while KEEPING confirmedStopLossPrice trigger unchanged
	// (Prevents resting SL from firing at Breakeven during the 3ms window while Step 2 is in flight!)
	if worker.modCount.Load() >= MaxSafetyModifications {
		worker.isTranche1Pending.Store(false)
		return
	}
	if err := worker.broker.ModifySLOrder(worker.ctx, stopLossOrderID, runnerQty, confirmedStopLossPrice, currentLimitPrice, lastTradedPrice, worker.TickSize); err != nil {
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
	scaleOutOrderID, scaleOutSellErr := worker.broker.PlaceProtectedMarketSell(worker.ctx, worker.Symbol, worker.Token, scaleOutQty, lastTradedPrice, worker.TickSize, stockPtr)
	if scaleOutSellErr != nil {
		slog.Error("Scale-out Step 2 sell failed; rolling back resting SL to full quantity", "symbol", worker.Symbol, "error", scaleOutSellErr)
		// COMPENSATING ROLLBACK: Restore resting SL quantity back to full RemainingQty()
		fullRemainingQuantity := worker.RemainingQty()
		if fullRemainingQuantity > 0 && worker.modCount.Load() < MaxSafetyModifications {
			if rbErr := worker.broker.ModifySLOrder(worker.ctx, stopLossOrderID, fullRemainingQuantity, confirmedStopLossPrice, currentLimitPrice, worker.LatestLTP(), worker.TickSize); rbErr != nil {
				worker.EmergencySellAllAndClosePosition("SCALE_OUT_ROLLBACK_FAILED")
				return
			}
			worker.mu.Lock()
			worker.slOrderQty = fullRemainingQuantity
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
	worker.scaleOutOrderID = scaleOutOrderID
	worker.mu.Unlock()
	worker.reconciler.RegisterOrder(scaleOutOrderID, worker.PositionID, "SCALE_OUT", worker.Symbol, "SELL", "LIMIT", scaleOutQty, lastTradedPrice, 0, OrderStatusOpen)

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
	if worker.RemainingQty() > runnerQty && scaleOutOrderID != "" {
		_ = worker.broker.CancelOrder(worker.ctx, scaleOutOrderID)
		_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
	}

	remainingRunnerQuantity := worker.RemainingQty()
	if remainingRunnerQuantity <= 0 || worker.isClosed.Load() {
		return
	}

	// STEP 3: Raise the remaining runner's SL trigger to Tax-Free Breakeven (Entry + Taxes + 0.15%)
	breakevenTriggerPrice := CalculateTaxCoveringBreakevenStop(worker.GetEntryPrice(), int(worker.filledEntryQty.Load()), remainingRunnerQuantity, worker.TickSize)
	worker.breakevenStopBits.Store(math.Float64bits(breakevenTriggerPrice))
	worker.hasTranche1Completed.Store(true)
	worker.isTranche1Pending.Store(false)
	worker.currentGear.Store(GearDynamicTrail)

	worker.mu.Lock()
	worker.BreakevenStop = breakevenTriggerPrice
	worker.state = StateRunnerActive
	worker.mu.Unlock()

	currentLastTradedPrice := worker.LatestLTP()
	if currentLastTradedPrice <= breakevenTriggerPrice+2.0*worker.TickSize {
		// Price whipped back down to Breakeven during scale-out -> Exit runner to lock net profit!
		worker.EmergencySellAllAndClosePosition("RUNNER_BREAKEVEN_LOCK")
		return
	}

	if breakevenTriggerPrice > confirmedStopLossPrice {
		// Always record breakevenTriggerPrice in shadowStopBits & proposedStopBits so if ModifySLOrder is transiently
		// rejected right now, processPendingWork() ratchets the SL to breakevenTriggerPrice on the next eligible tick.
		worker.shadowStopBits.Store(math.Float64bits(breakevenTriggerPrice))
		worker.proposedStopBits.Store(math.Float64bits(breakevenTriggerPrice))
		if worker.modCount.Load() < MaxSafetyModifications {
			breakevenLimitPrice := CalculateProtectiveLimitPriceBelowStop(breakevenTriggerPrice, worker.TickSize)
			if err := worker.broker.ModifySLOrder(worker.ctx, stopLossOrderID, remainingRunnerQuantity, breakevenTriggerPrice, breakevenLimitPrice, currentLastTradedPrice, worker.TickSize); err == nil {
				worker.modCount.Add(1)
				worker.lastModUnixMs.Store(time.Now().UnixMilli())
				worker.confirmedStopBits.Store(math.Float64bits(breakevenTriggerPrice))
				worker.mu.Lock()
				worker.slOrderQty = remainingRunnerQuantity
				worker.mu.Unlock()
			}
		}
	}
	worker.persistPositionState()
}

// EmergencySellAllAndClosePosition runs the 3-Stage Bulletproof Exit Protocol until RemainingQty() == 0
func (worker *PositionWorker) EmergencySellAllAndClosePosition(reason string) {
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
	stopLossOrderID := worker.slOrderID
	entryOrderID := worker.entryOrderID
	worker.mu.Unlock()

	worker.persistPositionState()

	// If entry order is still open/partially filled, cancel it first
	if entryOrderID != "" && int(worker.filledEntryQty.Load()) < worker.InitialQty {
		_ = worker.broker.CancelOrder(worker.ctx, entryOrderID)
	}

	remainingQuantity := worker.RemainingQty()
	if remainingQuantity <= 0 {
		_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
		remainingQuantity = worker.RemainingQty()
		if remainingQuantity <= 0 {
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
	if stopLossOrderID != "" && worker.modCount.Load() < MaxExchangeOrderMods {
		if err := worker.broker.MutateSLToMarket(worker.ctx, stopLossOrderID, remainingQuantity, stockPtr); err == nil {
			worker.modCount.Add(1)
			waitTimerA := time.NewTimer(900 * time.Millisecond)
			select {
			case <-worker.fillNotifyCh:
			case <-waitTimerA.C:
				_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
			}
			waitTimerA.Stop()
			if worker.RemainingQty() <= 0 || worker.isClosed.Load() {
				return
			}
		} else {
			slog.Warn("Stage A MutateSLToMarket failed, escalating to Stage B Cancel+Sweep",
				"symbol", worker.Symbol, "error", err)
		}
	}

	// STAGE B & C: Cancel Resting SL -> Reconcile -> Escalating LPP Sweep (0.75% -> 1.50% -> 2.50% -> 3.50%/Market)
	if stopLossOrderID != "" {
		_ = worker.broker.CancelOrder(worker.ctx, stopLossOrderID)
	}
	_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)

	slippageSchedule := [4]float64{0.0075, 0.0150, 0.0250, 0.0350}
	for attempt := 1; attempt <= 4; attempt++ {
		remainingQuantity = worker.RemainingQty()
		if remainingQuantity <= 0 || worker.isClosed.Load() {
			return
		}

		lastTradedPrice := worker.LatestLTP()
		if stockPtr != nil && stockPtr.LTP > 0 {
			lastTradedPrice = stockPtr.LTP
		}
		slippagePercent := slippageSchedule[attempt-1]
		emergencyOrderID, err := worker.broker.PlaceEmergencySweepSell(worker.ctx, worker.Symbol, worker.Token, remainingQuantity, lastTradedPrice, worker.TickSize, slippagePercent, stockPtr)
		if err == nil && emergencyOrderID != "" {
			worker.reconciler.RegisterOrder(emergencyOrderID, worker.PositionID, "EMERGENCY_EXIT", worker.Symbol, "SELL", "LIMIT", remainingQuantity, lastTradedPrice, 0, OrderStatusOpen)
		}

		waitTimerB := time.NewTimer(900 * time.Millisecond)
		select {
		case <-worker.fillNotifyCh:
		case <-waitTimerB.C:
			_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
		}
		waitTimerB.Stop()

		if worker.RemainingQty() <= 0 || worker.isClosed.Load() {
			return
		}
		// Falsification Guard (Bug #15): Cancel any unfilled portion of emergencyOrderID and reconcile
		// before placing the next retry attempt so two emergency sell orders are never open simultaneously!
		if emergencyOrderID != "" {
			_ = worker.broker.CancelOrder(worker.ctx, emergencyOrderID)
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
			remainingQuantity = worker.RemainingQty()
			if remainingQuantity <= 0 || worker.isClosed.Load() {
				return
			}
			lastTradedPrice := worker.LatestLTP()
			if stockPtr != nil && stockPtr.LTP > 0 {
				lastTradedPrice = stockPtr.LTP
			}
			recoveryOrderID, err := worker.broker.PlaceEmergencySweepSell(worker.ctx, worker.Symbol, worker.Token, remainingQuantity, lastTradedPrice, worker.TickSize, 0.0350, stockPtr)
			if err == nil && recoveryOrderID != "" {
				worker.reconciler.RegisterOrder(recoveryOrderID, worker.PositionID, "EMERGENCY_EXIT", worker.Symbol, "SELL", "LIMIT", remainingQuantity, lastTradedPrice, 0, OrderStatusOpen)
			}
			waitTimerRec := time.NewTimer(900 * time.Millisecond)
			select {
			case <-worker.fillNotifyCh:
			case <-waitTimerRec.C:
				_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
			}
			waitTimerRec.Stop()
			if worker.RemainingQty() <= 0 || worker.isClosed.Load() {
				return
			}
			if recoveryOrderID != "" {
				_ = worker.broker.CancelOrder(worker.ctx, recoveryOrderID)
				_, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
			}
		}
	}
}

