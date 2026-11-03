package trading

import (
	"math"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
)


// UpdateTrailingStopLossOnPriceTick is executed by the Actor on every incoming tick for this position's instrument.

// Step 1: Update the High-Water Mark (highWaterMark): If the stock price rises from ₹500 → ₹510 → ₹525, highWaterMark updates to ₹525.

// Step 2: Guard A — Gapped-Through Stop-Loss Watchdog: If the market gaps down below our stop-loss without Zerodha executing it, the watchdog detects the breach within 1 second and immediately fires an emergency market exit.

// Step 3: Guard D — The +1.50R Profit Scale-Out Trigger: If the price reaches +1.50R ( ₹ 500 + 1.5 × ₹ 10 = ₹ 515 ₹500+1.5×₹10=₹515), it triggers SellHalfSharesAtProfitTarget() to lock in gains.

// Step 4: Check Buyer Strength vs Seller Pressure (orderFlowMultiplier):
// - If buyers are aggressively dominating the tape, the algorithm gives the stock more breathing room so it doesn't get shaken out.
// - If buyer volume dries up and sellers start hitting bids, the algorithm tightens the buffer to protect profits.


// Step 5: Parabolic Climax Tightener (parabolicClimaxFactor)
// If the stock shoots up vertically like a rocket ( > 2.2 𝑅 >2.2R and far above VWAP), moves like that usually pull back violently. The algorithm tightens the stop right behind the price to capture the peak.


// Step 6: The +1.0R Tax-Free Breakeven Ratchet: As soon as the trade reaches +1.0R (e.g., ₹510), the stop-loss is raised to Tax-Free Breakeven via CalculateTaxCoveringBreakevenStop(). At this point, your downside risk becomes ₹0.00. You cannot lose money on this trade.

// Step 7: Propose the New Higher Stop Price: If the candidate stop is higher than the current stop on the exchange, it calls RaiseStopLossOrder().
func (worker *PositionWorker) UpdateTrailingStopLossOnPriceTick(stockMetrics *market.StockMetrics) {
	if worker.isClosed.Load() {
		return
	}

	// If running in Paper Mode, match resting Paper SL orders against live 5-level depth first
	if worker.Mode == "PAPER" && worker.paperBroker != nil {
		worker.paperBroker.EvaluateTickForToken(stockMetrics)
	}

	if worker.isClosed.Load() {
		return
	}

	if worker.isMarketExitPending.Load() {
		return
	}

	lastTradedPrice := stockMetrics.LTP
	if lastTradedPrice <= 0 {
		return
	}
	worker.latestLTPBits.Store(math.Float64bits(lastTradedPrice))

	currentTickUnix := stockMetrics.LastUpdatedUnix
	if currentTickUnix <= 0 {
		currentTickUnix = time.Now().Unix()
	}

	// Runaway Price Guard: Cancel entry limit buy if price ran away without filling
	if worker.filledEntryQty.Load() <= 0 {
		worker.mu.RLock()
		isEntryPlaced := worker.state == StateEntryPlaced
		entryPx := worker.EntryPrice
		worker.mu.RUnlock()

		if isEntryPlaced && worker.OneR > 0 && lastTradedPrice >= entryPx+1.2*worker.OneR {
			runawayStart := worker.runawaySinceUnix.Load()
			if runawayStart == 0 {
				worker.runawaySinceUnix.Store(currentTickUnix)
			} else if (currentTickUnix - runawayStart) >= 30 {
				worker.TriggerEmergencyExit("ENTRY_CANCELLED_PRICE_RUNAWAY")
				return
			}
		} else {
			worker.runawaySinceUnix.Store(0)
		}
		return
	}

	if worker.RemainingQty() <= 0 {
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

	effectiveOneR := worker.OneR
	if effectiveOneR <= 0 && worker.InitialStop > 0 && entryPrice > worker.InitialStop {
		effectiveOneR = entryPrice - worker.InitialStop
	}
	var maxFavorableRMultiple float64
	if effectiveOneR > 0 && highWaterMark > entryPrice {
		maxFavorableRMultiple = (highWaterMark - entryPrice) / effectiveOneR
	}
	confirmedStopLoss := math.Float64frombits(worker.confirmedStopBits.Load())

	// 2. GUARD A: Gapped-Through / Stranded SL Watchdog
	// - If price gaps completely below the SL-L protective limit band, the exchange order cannot fill -> exit on Tick 1.
	// - If price is inside [protectiveLimitPrice, confirmedStopLoss], Tick 1 arms slBreachSinceUnix so normal
	//   exchange SL-L fills don't race our emergency sweep, and Tick 2+ (or >= 1s epoch) triggers emergency exit.
	if lastTradedPrice <= confirmedStopLoss {
		protectiveLimitPrice := CalculateProtectiveLimitPriceBelowStop(confirmedStopLoss, worker.TickSize)
		if lastTradedPrice < protectiveLimitPrice {
			worker.TriggerEmergencyExit("SL_GAP_THROUGH_BREACH")
			return
		}
		breachStartTimeUnix := worker.slBreachSinceUnix.Load()
		if breachStartTimeUnix == 0 {
			worker.slBreachSinceUnix.Store(currentTickUnix)
		} else if (currentTickUnix-breachStartTimeUnix) >= 1 || lastTradedPrice <= confirmedStopLoss-(3.0*worker.TickSize) {
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

	// 5. Compute Dynamic Order-Flow Multiplier M_flow & Parabolic Climax Factor C_climax
	var closeLocationValue float64 = 0.50
	if int(stockMetrics.CurrentBarIdx) < len(stockMetrics.SessionBars) {
		currentBar := stockMetrics.SessionBars[stockMetrics.CurrentBarIdx]
		if currentBar.High > currentBar.Low {
			rawCloseLocationValue := (lastTradedPrice - currentBar.Low) / (currentBar.High - currentBar.Low)
			closeLocationValue = math.Max(0.0, math.Min(1.0, rawCloseLocationValue))
		}
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

	// 6. Dynamic Breathing Buffer & High-Frequency Breakeven Ratchet
	effectiveBaseBuffer := worker.BaseBuffer
	if !worker.hasBreakevenCompleted.Load() && flowScore < 0.55 && effectiveOneR > 0 {
		effectiveBaseBuffer = math.Min(worker.BaseBuffer, effectiveOneR)
	}

	dynamicBreathingBuffer := effectiveBaseBuffer * orderFlowMultiplier * parabolicClimaxFactor
	unclampedCandidateStop := RoundToTick(highWaterMark-dynamicBreathingBuffer, worker.TickSize)

	// Early Profit Protection: Once trade reaches +1.00R, lock Tax-Free Breakeven immediately!
	// Per Rishi Narang & Dr. Samir Varma: Ratcheting to breakeven at +0.50R suffocated trades in normal Brownian noise.
	// Moving to BE at +1.00R guarantees market noise cannot stop out valid trending moves prematurely.
	if maxFavorableRMultiple >= 1.00 {
		worker.hasBreakevenCompleted.Store(true)
		worker.currentGear.CompareAndSwap(1, GearDynamicTrail)
		unclampedCandidateStop = math.Max(unclampedCandidateStop, breakevenStop)
	}
	if worker.hasBreakevenCompleted.Load() && stockMetrics.CurrentBarIdx > worker.EntryBarIdx && stockMetrics.CurrentBarIdx >= 1 && int(stockMetrics.CurrentBarIdx-1) < len(stockMetrics.SessionBars) {
		previousBar := stockMetrics.SessionBars[stockMetrics.CurrentBarIdx-1]
		if previousBar.Low > 0 {
			prev5mLow := previousBar.Low - 2.0*worker.TickSize
			unclampedCandidateStop = math.Max(unclampedCandidateStop, RoundToTick(prev5mLow, worker.TickSize))
		}
	}

	// 7. OrderBlock Rule: Judge the Zone on Completed 5-Minute Candle Closes (Never on Intra-Candle Wicks)
	if stockMetrics.CurrentBarIdx > worker.EntryBarIdx && stockMetrics.CurrentBarIdx >= 1 && int(stockMetrics.CurrentBarIdx-1) < len(stockMetrics.SessionBars) {
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
		worker.RaiseStopLossOrder(safeCandidateStop, safeATR, false)
	}
}

// This function manages the NSE 25-Modification Law using 4 Mechanical Gears
// RaiseStopLossOrder governs Mods 1–18 (Agile), Mods 19–24 (Coarse), and Gear 4 (Mod 24 Shadow Stop)
// Gear 1 (Agile, Mods 1–18): Moves quickly. Only requires a 350-millisecond pause between edits. Its job is to move your stop out of the danger zone as soon as possible.
// Gear 3 (Coarse, Mods 19–24): Slows down. Enforces a 1,500-millisecond pause and requires a larger price jump (
// Gear 4 (Virtual Shadow Stop in RAM, Mod ≥ 24): When modification count reaches 24, it stops sending modification orders to Zerodha completely! The stop-loss continues trailing virtually in computer memory (RAM). If the price drops and breaches the virtual shadow stop, the system uses its strictly preserved 25th modification to convert the resting stop-loss into an immediate market sell order, closing the position cleanly.
func (worker *PositionWorker) RaiseStopLossOrder(candidateStop, safeATR float64, isUrgentShockClamp bool) {
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

	referenceStopPrice := math.Max(
		math.Float64frombits(worker.confirmedStopBits.Load()),
		math.Float64frombits(worker.proposedStopBits.Load()),
	)
	priceImprovement := candidateStop - referenceStopPrice
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
