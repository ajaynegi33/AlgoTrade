package execution

import (
	"math"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/engine"
)

// OnTickHotPath is executed by the Actor on every incoming tick for this position's instrument.
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
				w.TriggerEmergencyExit(ExitReasonShadowStopHit)
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
	// 4. GUARD C: Macro Flash Shock Protection (Nifty 50 Liquidity Vacuum)
	if inMacroShock && ltp < s.ExchangeVWAP && effectiveAggBuy < 0.35 {
		if !w.tranche1Done.Load() {
			w.TriggerEmergencyExit(ExitReasonEmergencyTape)
			return
		}
		// Runner in profit during shock -> Clamp stop to max(Breakeven, LTP - 0.35*BaseBuffer)
		tightStop := math.Max(beStop, RoundDownToTick(ltp-0.35*w.BaseBuffer, w.TickSize))
		if tightStop > confirmedStop {
			w.ProposeStopRatchet(tightStop, atrSafe, true)
		}
		return
	}

	// 5. GUARD D: +0.65R High-Frequency Minimum Profit Scale-Out Trigger (Early Harvest at R1)
	if !w.tranche1Done.Load() && !w.tranche1Pending.Load() && rCur >= 0.65 {
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
		// Structure intact: floor M_flow at 0.65 so normal wicks don't choke the trade
		mFlow = math.Max(0.65, 0.65+math.Max(0, flowScore-0.35)*0.95)
	} else {
		mFlow = math.Max(0.40, 0.40+math.Max(0, flowScore-0.25)*1.40)
	}

	// Parabolic Climax Tightener: When runner is >= +2.2R and extended > 1.8 ATR above VWAP
	cClimax := 1.0
	xVWAP := (ltp - s.ExchangeVWAP) / atrSafe
	if rMFE >= 2.2 && xVWAP > 1.8 {
		cClimax = math.Max(0.35, 1.0-0.22*(xVWAP-1.8))
	}

	// 7. Dynamic Breathing Buffer & High-Frequency Breakeven Ratchet
	effectiveBase := w.BaseBuffer
	if !w.tranche1Done.Load() && flowScore < 0.55 {
		effectiveBase = math.Min(w.BaseBuffer, w.OneR)
	}

	bDynamic := effectiveBase * mFlow * cClimax
	unclampedCand := RoundToTick(hwm-bDynamic, w.TickSize)

	// Early Profit Protection: Once trade reaches +0.50R, lock Tax-Free Breakeven immediately!
	if rMFE >= 0.50 {
		unclampedCand = math.Max(unclampedCand, beStop)
	}
	if w.tranche1Done.Load() && s.CurrentBarIdx > w.EntryBarIdx && s.CurrentBarIdx >= 1 {
		prev5mLow := s.SessionBars[s.CurrentBarIdx-1].Low - 2.0*w.TickSize
		unclampedCand = math.Max(unclampedCand, RoundToTick(prev5mLow, w.TickSize))
	}

	// 8. OrderBlock Rule: Judge the Zone on Completed 5-Minute Candle Closes (Never on Intra-Candle Wicks)
	if s.CurrentBarIdx > w.EntryBarIdx && s.CurrentBarIdx >= 1 {
		prevBar := s.SessionBars[s.CurrentBarIdx-1]
		if prevBar.Close > 0 && prevBar.Close < w.InitialStop {
			w.TriggerEmergencyExit("COMPLETED_5M_STRUCTURAL_STOP_CLOSE")
			return
		}
	}

	// 9. Stop Ratchet Execution
	if unclampedCand <= confirmedStop {
		return
	}

	maxLegalTrigger := RoundDownToTick(ltp-math.Max(2.0*w.TickSize, 0.001*ltp), w.TickSize)
	if ltp <= unclampedCand {
		return
	}

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
	if mods >= MaxSafetyModifications {
		w.currentGear.Store(GearShadowExit)
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

