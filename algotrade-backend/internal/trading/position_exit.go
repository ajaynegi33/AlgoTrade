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
		remainingOpenShares := worker.RemainingQty()
		if !hasStopLossOrder {
			worker.PlaceInitialStopLossOrderIfNotPlaced()
			if worker.isClosed.Load() || worker.isMarketExitPending.Load() {
				return
			}
		} else if remainingOpenShares > stopLossOrderQuantity && worker.modCount.Load() < MaxSafetyModifications {
			// Incremental fill arrived while SL is active: resize SL to cover all remaining open shares
			confirmedStopLossPrice := math.Float64frombits(worker.confirmedStopBits.Load())
			sellLimitPrice := CalculateProtectiveLimitPriceBelowStop(confirmedStopLossPrice, worker.TickSize)
			lastTradedPrice := worker.LatestLTP()
			worker.mu.RLock()
			stopLossOrderID := worker.slOrderID
			worker.mu.RUnlock()
			if stopLossOrderID != "" {
				if err := worker.broker.ModifySLOrder(worker.ctx, stopLossOrderID, remainingOpenShares, confirmedStopLossPrice, sellLimitPrice, lastTradedPrice, worker.TickSize); err == nil {
					worker.mu.Lock()
					worker.slOrderQty = remainingOpenShares
					worker.mu.Unlock()
					worker.modCount.Add(1)
					worker.lastModUnixMs.Store(time.Now().UnixMilli())
				}
			}
		}
	}

	// 2. Apply pending Stop Ratchet if proposedStop > confirmedStop and modCount < 24
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
		worker.proposedStopBits.Store(worker.confirmedStopBits.Load())
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
	var stockMetrics *market.StockMetrics
	if worker.getStockPtr != nil {
		stockMetrics = worker.getStockPtr()
	}
	stopLossOrderID, err := worker.broker.PlaceSLOrder(worker.ctx, worker.Symbol, worker.Token, remainingQuantity, worker.InitialStop, sellLimitPrice, lastTradedPrice, worker.TickSize, stockMetrics)
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

