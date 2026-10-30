package trading

import (
	"fmt"
	"math"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
	"github.com/ajaynegi33/AlgoTrade/internal/screener"
)

// CandidateSignal represents a qualified long trade setup selected by the Signal Picker
type CandidateSignal struct {
	StockIdx     uint16
	Token        uint32
	Symbol       string
	PatternType  string // PatternVWAPPullbackReclaim | PatternORB15MBreakout
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
	isOpeningRangeBreakoutAllowed := true

	if sessionClock.Phase == "MIDDAY_COMPRESSION" && !sessionClock.IsTapeOverrideActive {
		isOpeningRangeBreakoutAllowed = false
	}

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
		if secHealth, ok := sectorHealthMap[stock.SectorID]; ok && secHealth.IsRestricted {
			candidateDiagnostic.IsRegimeValid = false
			candidateDiagnostic.OverallStatus = "BLOCKED"
			candidateDiagnostic.BlockingReason = fmt.Sprintf("Sector (%s) is RESTRICTED: %s", candidateDiagnostic.Sector, secHealth.RestrictionReason)
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

		atrSafe := math.Max(stock.ATR14, stock.LTP*0.0025)
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
		distanceToVWAPInATR := (stock.LTP - stock.ExchangeVWAP) / atrSafe
		previousBarMidpoint := (previousBar.Open + previousBar.Close) / 2.0
		if distanceToVWAPInATR >= 0.05 && distanceToVWAPInATR <= 0.55 &&
			previousBar.Low > 0 &&
			previousBar.Low <= stock.ExchangeVWAP+0.35*atrSafe &&
			previousBar.Low >= stock.ExchangeVWAP-0.15*atrSafe &&
			stock.LTP > math.Max(currentBar.Open+2.0*tickSize, previousBarMidpoint) &&
			closeLocationValue >= 0.58 &&
			stock.AggressorBuyRatio >= 0.56 &&
			stock.FastAggressorEMA >= 0.55 &&
			stock.SmoothedBuyerRatio >= 0.55 {
			matchedPattern = PatternVWAPPullbackReclaim
			signalBarLow = math.Min(previousBar.Low, currentBar.Low)
		}

		// Strategy Setup 2: 15-Minute Opening Range Breakout (ORB 15M)
		if matchedPattern == "" && isOpeningRangeBreakoutAllowed && stock.CurrentBarIdx >= 3 {
			openingRangeHigh, openingRangeLow, isORBReady := stock.OpeningRange15m()
			if isORBReady && (openingRangeHigh-openingRangeLow) <= 1.8*atrSafe &&
				stock.LTP > openingRangeHigh+tickSize &&
				(stock.LTP-openingRangeHigh) <= 0.35*atrSafe &&
				compositeRVOL >= 1.8 &&
				closeLocationValue >= 0.65 &&
				stock.AggressorBuyRatio >= 0.60 &&
				stock.FastAggressorEMA >= 0.58 &&
				stock.SmoothedBuyerRatio >= 0.58 {
				matchedPattern = PatternORB15MBreakout
				signalBarLow = math.Max(currentBar.Low, openingRangeHigh-0.85*atrSafe)
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
			candidateDiagnostic.BlockingReason = fmt.Sprintf("Awaiting VWAP Pullback Reclaim (0.05–0.55 ATR, cur %.2f) or 15m ORB Breakout", distanceToVWAPInATR)
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)
			continue
		}

		sizing := ComputePositionSize(stock, signalBarLow, netEquity, freeMargin, riskPct, sizeMult, drawdownMult, remainingHeatBudgetRupees)
		candidateDiagnostic.IsSizingApproved = sizing.IsApproved
		if !sizing.IsApproved {
			candidateDiagnostic.BlockingReason = sizing.RejectReason
			candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)
			continue
		}

		candidateDiagnostic.OverallStatus = "READY"
		candidateDiagnostic.BlockingReason = fmt.Sprintf("Qualified %s • Qty %d @ ₹%.2f (1R=₹%.2f)", matchedPattern, sizing.Quantity, sizing.EntryLimitPrice, sizing.StopSpec.OneR)
		candidateDiagnostics = append(candidateDiagnostics, candidateDiagnostic)

		if winningSignal == nil {
			winningSignal = &CandidateSignal{
				StockIdx:     stockIndex,
				Token:        stock.InstrumentToken,
				Symbol:       symbol,
				PatternType:  matchedPattern,
				SignalBarLow: signalBarLow,
				BullishScore: stock.BullishScore,
				Sizing:       sizing,
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

