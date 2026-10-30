package trading

import (
	"math"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
)

// OnTickHotPath is executed by the Actor on every incoming tick for this position's instrument.
func (worker *PositionWorker) OnTickHotPath(stockMetrics *market.StockMetrics, isInMacroShock bool) {
	if worker.isClosed.Load() {
		return
	}

	// If running in Paper Mode, match resting Paper SL orders against live 5-level depth first
	if worker.Mode == "PAPER" && worker.paperBroker != nil {
		worker.paperBroker.EvaluateTickForToken(stockMetrics)
	}

	if worker.isMarketExitPending.Load() {
		return
	}

	lastTradedPrice := stockMetrics.LTP
	if lastTradedPrice <= 0 {
		return
	}
	worker.latestLTPBits.Store(math.Float64bits(lastTradedPrice))

	// Runaway Price Guard: Cancel entry limit buy if price ran away without filling
	if worker.filledEntryQty.Load() <= 0 {
		worker.mu.RLock()
		isEntryPlaced := worker.state == StateEntryPlaced
		entryPx := worker.EntryPrice
		worker.mu.RUnlock()

		if isEntryPlaced && worker.OneR > 0 && lastTradedPrice >= entryPx+1.2*worker.OneR {
			runawayStart := worker.runawaySinceUnix.Load()
			if runawayStart == 0 {
				worker.runawaySinceUnix.Store(stockMetrics.LastUpdatedUnix)
			} else if (stockMetrics.LastUpdatedUnix - runawayStart) >= 30 {
				worker.TriggerEmergencyExit("ENTRY_CANCELLED_PRICE_RUNAWAY")
				return
			}
		} else {
			worker.runawaySinceUnix.Store(0)
		}
		return
	}

	if worker.GetRemainingQty() <= 0 {
		return
	}

	// 1. Update High-Water Mark (HWM) and Low-Water Mark (LWM)
	entryPrice := worker.GetEntryPrice()
	breakevenStop := worker.GetBreakevenStop()
	highWaterMark := math.Float64frombits(worker.highWaterMarkBits.Load())
	if lastTradedPrice > highWaterMark {
		highWaterMark = lastTradedPrice
		worker.highWaterMarkBits.Store(math.Float64bits(highWaterMark))
	}
	lowWaterMark := math.Float64frombits(worker.lowWaterMarkBits.Load())
	if lowWaterMark == 0 || lastTradedPrice < lowWaterMark {
		lowWaterMark = lastTradedPrice
		worker.lowWaterMarkBits.Store(math.Float64bits(lowWaterMark))
	}
	currentRMultiple := (lastTradedPrice - entryPrice) / worker.OneR
	maxFavorableRMultiple := (highWaterMark - entryPrice) / worker.OneR
	confirmedStopLoss := math.Float64frombits(worker.confirmedStopBits.Load())

	// 2. GUARD A: Gapped-Through / Stranded SL Watchdog (1s Epoch or 3-Tick Breach)
	if lastTradedPrice <= confirmedStopLoss {
		breachStartTimeUnix := worker.slBreachSinceUnix.Load()
		if breachStartTimeUnix == 0 {
			worker.slBreachSinceUnix.Store(stockMetrics.LastUpdatedUnix)
		} else if (stockMetrics.LastUpdatedUnix-breachStartTimeUnix) >= 1 || lastTradedPrice <= confirmedStopLoss-(3.0*worker.TickSize) {
			worker.TriggerEmergencyExit("SL_GAP_THROUGH_BREACH")
			return
		}
	} else {
		worker.slBreachSinceUnix.Store(0)
	}

	// 3. GUARD B: Gear 4 Virtual Shadow Stop (Active when modCount >= 24; Mod 25 Reserved for Exit!)
	if worker.modCount.Load() >= 24 {
		shadowStop := math.Float64frombits(worker.shadowStopBits.Load())
		if shadowStop > 0 && lastTradedPrice <= shadowStop {
			consecutiveBreachHits := worker.shadowBreachTicks.Add(1)
			if consecutiveBreachHits >= 2 || stockMetrics.FastAggressorEMA < 0.42 || lastTradedPrice <= shadowStop-(2.0*worker.TickSize) {
				worker.TriggerEmergencyExit(ExitReasonShadowStopHit)
				return
			}
		} else {
			worker.shadowBreachTicks.Store(0)
		}
	}

	// 4. GUARD C: Volatility-Normalized Sudden Shock & Book-Vacuum Reflex
	// Uses continuous RollingPeak (tau=20s) + FastAggressorEMA (zero 30s reset blind spot!)
	safeATR := math.Max(stockMetrics.ATR14, lastTradedPrice*0.0025)
	effectiveAggressorBuy := stockMetrics.AggressorBuyRatio
	if stockMetrics.FastAggressorEMA < effectiveAggressorBuy {
		effectiveAggressorBuy = stockMetrics.FastAggressorEMA
	}

	// 5. GUARD D: +0.65R High-Frequency Minimum Profit Scale-Out Trigger (Early Harvest at R1)
	if !worker.hasTranche1Completed.Load() && !worker.isTranche1Pending.Load() && currentRMultiple >= 0.65 {
		worker.isTranche1Pending.Store(true)
		worker.wakeWorker()
		return
	}

	// 6. Compute Dynamic Order-Flow Multiplier M_flow & Parabolic Climax Factor C_climax
	currentBar := stockMetrics.SessionBars[stockMetrics.CurrentBarIdx]
	var closeLocationValue float64 = 0.50
	if currentBar.High > currentBar.Low {
		closeLocationValue = (lastTradedPrice - currentBar.Low) / (currentBar.High - currentBar.Low)
	}
	flowScore := 0.50*float64(effectiveAggressorBuy) + 0.35*float64(stockMetrics.SmoothedBuyerRatio) + 0.15*closeLocationValue

	isStructureIntact := lastTradedPrice >= stockMetrics.ExchangeVWAP && lastTradedPrice > worker.SignalBarLow
	var orderFlowMultiplier float64
	if flowScore >= 0.56 && isStructureIntact {
		orderFlowMultiplier = math.Min(1.10, 0.85+(flowScore-0.56)*1.25)
	} else if isStructureIntact {
		// Structure intact: floor M_flow at 0.65 so normal wicks don't choke the trade
		orderFlowMultiplier = math.Max(0.65, 0.65+math.Max(0, flowScore-0.35)*0.95)
	} else {
		orderFlowMultiplier = math.Max(0.40, 0.40+math.Max(0, flowScore-0.25)*1.40)
	}

	// Parabolic Climax Tightener: When runner is >= +2.2R and extended > 1.8 ATR above VWAP
	parabolicClimaxFactor := 1.0
	vwapExtensionMultiple := (lastTradedPrice - stockMetrics.ExchangeVWAP) / safeATR
	if maxFavorableRMultiple >= 2.2 && vwapExtensionMultiple > 1.8 {
		parabolicClimaxFactor = math.Max(0.35, 1.0-0.22*(vwapExtensionMultiple-1.8))
	}

	// 7. Dynamic Breathing Buffer & High-Frequency Breakeven Ratchet
	effectiveBaseBuffer := worker.BaseBuffer
	if !worker.hasTranche1Completed.Load() && flowScore < 0.55 {
		effectiveBaseBuffer = math.Min(worker.BaseBuffer, worker.OneR)
	}

	dynamicBreathingBuffer := effectiveBaseBuffer * orderFlowMultiplier * parabolicClimaxFactor
	unclampedCandidateStop := RoundToTick(highWaterMark-dynamicBreathingBuffer, worker.TickSize)

	// Early Profit Protection: Once trade reaches +0.50R, lock Tax-Free Breakeven immediately!
	if maxFavorableRMultiple >= 0.50 {
		unclampedCandidateStop = math.Max(unclampedCandidateStop, breakevenStop)
	}
	if worker.hasTranche1Completed.Load() && stockMetrics.CurrentBarIdx > worker.EntryBarIdx && stockMetrics.CurrentBarIdx >= 1 {
		prev5mLow := stockMetrics.SessionBars[stockMetrics.CurrentBarIdx-1].Low - 2.0*worker.TickSize
		unclampedCandidateStop = math.Max(unclampedCandidateStop, RoundToTick(prev5mLow, worker.TickSize))
	}

	// 8. OrderBlock Rule: Judge the Zone on Completed 5-Minute Candle Closes (Never on Intra-Candle Wicks)
	if stockMetrics.CurrentBarIdx > worker.EntryBarIdx && stockMetrics.CurrentBarIdx >= 1 {
		previousBar := stockMetrics.SessionBars[stockMetrics.CurrentBarIdx-1]
		if previousBar.Close > 0 && previousBar.Close < worker.InitialStop {
			worker.TriggerEmergencyExit("COMPLETED_5M_STRUCTURAL_STOP_CLOSE")
			return
		}
	}

	// 9. Stop Ratchet Execution
	if unclampedCandidateStop <= confirmedStopLoss {
		return
	}

	maxLegalTrigger := RoundDownToTick(lastTradedPrice-math.Max(2.0*worker.TickSize, 0.001*lastTradedPrice), worker.TickSize)
	if lastTradedPrice <= unclampedCandidateStop {
		return
	}

	safeCandidateStop := math.Min(unclampedCandidateStop, maxLegalTrigger)
	if safeCandidateStop > confirmedStopLoss {
		worker.ProposeStopRatchet(safeCandidateStop, safeATR, false)
	}
}

// ProposeStopRatchet governs Mods 1–18 (Agile), Mods 19–24 (Coarse), and Gear 4 (Mod 24 Shadow Stop)
func (worker *PositionWorker) ProposeStopRatchet(candidateStop, safeATR float64, isUrgentShockClamp bool) {
	modificationCount := worker.modCount.Load()

	// Always advance Shadow Stop in RAM
	for {
		currentShadowBits := worker.shadowStopBits.Load()
		if candidateStop <= math.Float64frombits(currentShadowBits) {
			break
		}
		if worker.shadowStopBits.CompareAndSwap(currentShadowBits, math.Float64bits(candidateStop)) {
			break
		}
	}

	// GEAR 4 INVARIANT: At modCount >= 24, NEVER send a price-ratchet PUT! Mod 25 is strictly for Market Exit!
	if modificationCount >= MaxSafetyModifications {
		worker.currentGear.Store(GearShadowExit)
		return
	}

	confirmedStopPrice := math.Float64frombits(worker.confirmedStopBits.Load())
	priceImprovement := candidateStop - confirmedStopPrice
	if priceImprovement <= 0 {
		return
	}

	currentUnixMs := time.Now().UnixMilli()
	elapsedMilliseconds := currentUnixMs - worker.lastModUnixMs.Load()

	if !isUrgentShockClamp {
		if modificationCount < 18 {
			minimumStepSize := math.Max(2.0*worker.TickSize, 0.12*worker.BaseBuffer)
			if priceImprovement < minimumStepSize || elapsedMilliseconds < 350 {
				return
			}
		} else {
			minimumStepSize := math.Max(0.40*worker.BaseBuffer, 0.35*safeATR)
			if priceImprovement < minimumStepSize || elapsedMilliseconds < 1500 {
				return
			}
		}
	}

	for {
		oldProposedBits := worker.proposedStopBits.Load()
		if candidateStop <= math.Float64frombits(oldProposedBits) {
			return
		}
		if worker.proposedStopBits.CompareAndSwap(oldProposedBits, math.Float64bits(candidateStop)) {
			worker.wakeWorker()
			return
		}
	}
}
