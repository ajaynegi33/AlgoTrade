package trading

import (
	"fmt"
	"math"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
	"github.com/ajaynegi33/AlgoTrade/internal/monitoring"
	"github.com/ajaynegi33/AlgoTrade/internal/screener"
)

// CandidateSignal represents a qualified long trade setup selected by the Signal Picker
type CandidateSignal struct {
	StockIndex   uint16
	Token        uint32
	Symbol       string
	PatternType  string // PatternVWAPPullbackReclaim | PatternFailedBreakdownReclaim | PatternORBRetestReclaim
	SignalBarLow float64
	BullishScore float64
	Sizing       SizingDecision
}

// PickBestLongCandidate runs Stage 0 Regime Filtering, Strategy Setup Structural Detection, and Pre-Trade Gatekeeping,
// while also producing the Top 5 CandidateGateDiagnosticDTO telemetry array for the Trader Cockpit UI.
func PickBestLongCandidate(
	stocks *[512]market.StockMetrics,
	bullishSortedStockIndices []uint16,
	sectorHealthMap map[uint8]market.SectorHealth,
	sessionClock market.GlobalSessionClockDTO,
	netEquity, freeMargin, riskPct, drawdownMult, remainingHeatBudgetRupees float64,
	isSymbolUnavailable func(symbol string, token uint32) bool,
) (*CandidateSignal, string, string, []market.CandidateGateDiagnosticDTO) {
	isRegimeValid := true
	var regimeLockStatus, regimeLockReason string

	if !sessionClock.IsNewEntryAllowed {
		isRegimeValid = false
		regimeLockStatus = "LOCKED_SESSION_WINDOW"
		regimeLockReason = sessionClock.PlaybookNote
	}

	minScore := sessionClock.MinBullishScore
	minRVOL := sessionClock.MinRVOL
	sizeMult := sessionClock.SizeMultiplier

	maxScan := min(5, len(bullishSortedStockIndices))
	candidateDiagnostics := make([]market.CandidateGateDiagnosticDTO, 0, maxScan)
	var winningSignal *CandidateSignal

	// Scan Top 5 Ranked Bullish Outliers
	for rankIndex := 0; rankIndex < maxScan; rankIndex++ {
		stockIndex := bullishSortedStockIndices[rankIndex]
		stock := &stocks[stockIndex]
		symbol := stock.SymbolString()
		compositeRVOL := math.Max(stock.RVOLSession, 0.85*stock.RVOLPace5m)

		candidateDiagnostic := market.CandidateGateDiagnosticDTO{
			Rank:          rankIndex + 1,
			Symbol:        symbol,
			Sector:        screener.GetSectorName(stock.SectorID),
			LTP:           stock.LTP,
			BullishScore:  stock.BullishScore,
			CompositeRVOL: compositeRVOL,
			EV:            stock.EV,
			IsRegimeValid: isRegimeValid,
			OverallStatus: "BLOCKED",
		}

		if !stock.IsTradeable || !stock.IsWarm || stock.LTP <= 0 || stock.ExchangeVWAP <= 0 || stock.CurrentBarIdx < 1 {
			candidateDiagnostic.BlockingReason = "Warming up 5m bars / VWAP feed"
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)
			continue
		}
		if isSymbolUnavailable != nil && isSymbolUnavailable(symbol, stock.InstrumentToken) {
			candidateDiagnostic.OverallStatus = "ACTIVE_OR_COOLDOWN"
			candidateDiagnostic.BlockingReason = "Position already active or in 15m post-exit cooldown"
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)
			continue
		}

		// Sector-Level Circuit Breaker: If this specific sector is restricted, block this stock ONLY!
		if sectorHealth, exists := sectorHealthMap[stock.SectorID]; exists && sectorHealth.IsRestricted {
			candidateDiagnostic.IsRegimeValid = false
			candidateDiagnostic.OverallStatus = "BLOCKED"
			candidateDiagnostic.BlockingReason = fmt.Sprintf("Sector (%s) is RESTRICTED: %s", candidateDiagnostic.Sector, sectorHealth.RestrictionReason)
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)
			continue
		}

		candidateDiagnostic.IsScoreValid = stock.BullishScore >= minScore && stock.EV >= 0.25
		candidateDiagnostic.IsRVOLValid = compositeRVOL >= minRVOL

		// Disqualifying Penalty Bitmask & Toxic Ask Wall Check
		disqualifyingMask := market.PenaltyWideSpread |
			market.PenaltyOverextended |
			market.PenaltyExhaustedTrend |
			market.PenaltyIcebergSell |
			market.PenaltyPullbackRedBar
		hasPenalty := (stock.Reasons & disqualifyingMask) != 0
		top2Bid := uint64(stock.BidLevels[0].Qty) + uint64(stock.BidLevels[1].Qty)
		hasAskWall := top2Bid == 0 || uint64(stock.AskLevels[0].Qty) > 4*top2Bid
		candidateDiagnostic.IsBookQualityValid = !hasPenalty && !hasAskWall

		safeATR := math.Max(stock.ATR14, stock.LTP*0.0025)
		tickSize := float64(stock.TickSize)
		if tickSize <= 0 {
			tickSize = 0.05
		}

		currentBar := stock.SessionBars[stock.CurrentBarIdx]
		previousBar := stock.SessionBars[stock.CurrentBarIdx-1]
		var closeLocationValue float64 = 0.50
		if currentBar.High > currentBar.Low {
			closeLocationValue = (stock.LTP - currentBar.Low) / (currentBar.High - currentBar.Low)
		}

		var matchedPattern string
		var signalBarLow float64

		// Strategy Setup 1: VWAP Pullback Reclaim (Aligned with CalculateBullishScore sVWAP)
		// Richie Naso Confluence Law: 9 EMA must be strictly positive (warm), above or at VWAP,
		// and ATR-normalized slope (ΔEMA9 / ATRsafe) must be flat or rising (>= -0.05 ATR).
		// Volatility Buffer Law: Stop is placed below signal candle low minus 0.20 ATR buffer to avoid stop hunts.
		distanceToVWAPInATR := (stock.LTP - stock.ExchangeVWAP) / safeATR
		previousBarMidpoint := (previousBar.Open + previousBar.Close) / 2.0
		normalizedEMA9Slope := stock.EMA9Slope / safeATR
		isEMA9Confluence := stock.EMA9 > 0 && stock.EMA9 >= stock.ExchangeVWAP && normalizedEMA9Slope >= -0.05

		if distanceToVWAPInATR >= 0.05 && distanceToVWAPInATR <= 0.55 &&
			previousBar.Low > 0 &&
			previousBar.Low <= stock.ExchangeVWAP+0.35*safeATR &&
			previousBar.Low >= stock.ExchangeVWAP-0.15*safeATR &&
			stock.LTP > math.Max(currentBar.Open+2.0*tickSize, previousBarMidpoint) &&
			closeLocationValue >= 0.58 &&
			stock.AggressorBuyRatio >= 0.56 &&
			stock.FastAggressorEMA >= 0.55 &&
			stock.SmoothedBuyerRatio >= 0.55 &&
			isEMA9Confluence {
			matchedPattern = PatternVWAPPullbackReclaim
			signalBarLow = math.Min(previousBar.Low, currentBar.Low) - (0.20 * safeATR)
		}

		// Strategy Setup 2: Failed Breakdown Reclaim (Specialist Liquidity Sweep / Bear Trap Spring)
		// Per Richie Naso & Rishi Narang: When market makers sweep below VWAP to trigger retail stops,
		// and volume immediately reclaims VWAP with aggressive buyers, ride the trapped-short squeeze.
		if matchedPattern == "" && stock.CurrentBarIdx >= 2 {
			sweptBelowVWAP := previousBar.Low < stock.ExchangeVWAP-0.05*safeATR &&
				previousBar.Low >= stock.ExchangeVWAP-0.45*safeATR
			reclaimedVWAP := stock.LTP >= stock.ExchangeVWAP+0.05*safeATR &&
				stock.LTP > currentBar.Open+2.0*tickSize

			if sweptBelowVWAP && reclaimedVWAP &&
				closeLocationValue >= 0.60 &&
				stock.AggressorBuyRatio >= 0.58 &&
				stock.FastAggressorEMA >= 0.56 &&
				stock.SmoothedBuyerRatio >= 0.56 &&
				compositeRVOL >= 1.5 {
				matchedPattern = PatternFailedBreakdownReclaim
				signalBarLow = previousBar.Low - (0.20 * safeATR)
			}
		}

		// Strategy Setup 3: 15-Minute Opening Range Retest Reclaim (Abolishes Naive 80%-Failure First Breakout)
		// Per Richie Naso: Never buy the initial breakout poke. Require a completed 5m bar close above ORH,
		// followed by a retest of the range high that holds as confirmed support before entry.
		if matchedPattern == "" && stock.CurrentBarIdx >= 4 {
			openingRangeHigh, openingRangeLow, isORBReady := stock.OpeningRange15m()
			hasConfirmedBreakoutClose := false
			if isORBReady {
				for completedBarIndex := uint8(3); completedBarIndex < stock.CurrentBarIdx; completedBarIndex++ {
					if stock.SessionBars[completedBarIndex].Close > openingRangeHigh {
						hasConfirmedBreakoutClose = true
						break
					}
				}
			}

			if isORBReady && hasConfirmedBreakoutClose && (openingRangeHigh-openingRangeLow) <= 1.8*safeATR &&
				previousBar.Low <= openingRangeHigh+0.25*safeATR &&
				previousBar.Low >= openingRangeHigh-0.35*safeATR &&
				stock.LTP > openingRangeHigh &&
				stock.LTP > currentBar.Open+tickSize &&
				(stock.LTP-openingRangeHigh) <= 0.45*safeATR &&
				compositeRVOL >= 1.8 &&
				closeLocationValue >= 0.62 &&
				stock.AggressorBuyRatio >= 0.58 &&
				stock.FastAggressorEMA >= 0.56 &&
				stock.SmoothedBuyerRatio >= 0.56 {
				matchedPattern = PatternORBRetestReclaim
				signalBarLow = math.Min(previousBar.Low, currentBar.Low) - (0.20 * safeATR)
			}
		}
		candidateDiagnostic.PatternMatched = matchedPattern

		if !isRegimeValid {
			candidateDiagnostic.BlockingReason = regimeLockReason
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)
			continue
		}
		if !candidateDiagnostic.IsScoreValid {
			candidateDiagnostic.BlockingReason = fmt.Sprintf("Score %.1f < %.0f or EV %.2fR < 0.25R", stock.BullishScore, minScore, stock.EV)
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)
			continue
		}
		if !candidateDiagnostic.IsBookQualityValid {
			if hasAskWall {
				candidateDiagnostic.BlockingReason = "Ask1 Wall > 4x Top-2 Bid Depth"
			} else {
				candidateDiagnostic.BlockingReason = "Disqualifying Microstructure Penalty Flag"
			}
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)
			continue
		}
		if !candidateDiagnostic.IsRVOLValid {
			candidateDiagnostic.BlockingReason = fmt.Sprintf("Composite RVOL %.2fx < %.1fx threshold", compositeRVOL, minRVOL)
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)
			continue
		}
		if matchedPattern == "" {
			candidateDiagnostic.BlockingReason = fmt.Sprintf("Awaiting VWAP Reclaim (cur %.2f ATR), Bear Trap Spring, or ORB Retest", distanceToVWAPInATR)
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)
			continue
		}

		sizing := ComputePositionSize(stock, signalBarLow, netEquity, freeMargin, riskPct, sizeMult, drawdownMult, remainingHeatBudgetRupees)
		candidateDiagnostic.IsSizingApproved = sizing.IsApproved
		if !sizing.IsApproved {
			candidateDiagnostic.BlockingReason = sizing.RejectReason
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)

			if collector := monitoring.GetCollector(); collector != nil {
				collector.EmitShadowSignal(monitoring.ShadowSignalEvent{
					TradingDate:      time.Now().Format("2006-01-02"),
					SignalTime:       time.Now(),
					Symbol:           symbol,
					InstrumentToken:  stock.InstrumentToken,
					PatternType:      matchedPattern,
					BullishScore:     stock.BullishScore,
					RejectionReason:  sizing.RejectReason,
					EntryPrice:       stock.LTP,
					HypotheticalStop: sizing.StopSpec.InitialStop,
					OneR:             sizing.StopSpec.OneR,
					Target1R:         stock.LTP + sizing.StopSpec.OneR,
				})
			}
			continue
		}

		candidateDiagnostic.OverallStatus = "READY"
		candidateDiagnostic.BlockingReason = fmt.Sprintf("Qualified %s • Qty %d @ ₹%.2f (1R=₹%.2f)", matchedPattern, sizing.Quantity, sizing.EntryLimitPrice, sizing.StopSpec.OneR)
		candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)

		if winningSignal == nil {
			winningSignal = &CandidateSignal{
				StockIndex:   stockIndex,
				Token:        stock.InstrumentToken,
				Symbol:       symbol,
				PatternType:  matchedPattern,
				SignalBarLow: signalBarLow,
				BullishScore: stock.BullishScore,
				Sizing:       sizing,
			}
		} else {
			// Track secondary qualified setups as shadow signals
			if collector := monitoring.GetCollector(); collector != nil {
				collector.EmitShadowSignal(monitoring.ShadowSignalEvent{
					TradingDate:      time.Now().Format("2006-01-02"),
					SignalTime:       time.Now(),
					Symbol:           symbol,
					InstrumentToken:  stock.InstrumentToken,
					PatternType:      matchedPattern,
					BullishScore:     stock.BullishScore,
					RejectionReason:  fmt.Sprintf("Lower rank than winning candidate (%s)", winningSignal.Symbol),
					EntryPrice:       stock.LTP,
					HypotheticalStop: sizing.StopSpec.InitialStop,
					OneR:             sizing.StopSpec.OneR,
					Target1R:         stock.LTP + sizing.StopSpec.OneR,
				})
			}
		}
	}

	if !isRegimeValid {
		return nil, regimeLockStatus, regimeLockReason, candidateDiagnostics
	}
	if winningSignal != nil {
		return winningSignal, "SIGNAL_MATCHED", fmt.Sprintf("Matched %s on %s (Qty %d)", winningSignal.PatternType, winningSignal.Symbol, winningSignal.Sizing.Quantity), candidateDiagnostics
	}

	return nil, "ARMED_SCANNING", fmt.Sprintf("Scanning Top 5 Leaders (%s • Min Score %.0f)", sessionClock.Phase, minScore), candidateDiagnostics
}

