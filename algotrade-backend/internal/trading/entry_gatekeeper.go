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

// PickBestLongCandidate runs Stage 0 Regime Filtering, Pattern A & B Structural Detection, and Pre-Trade Gatekeeping,
// while also producing the Top 5 CandidateGateDiagnosticDTO telemetry array for the Trader Cockpit UI.
func PickBestLongCandidate(
	stocks *[512]market.StockMetrics,
	bullishSorted []uint16,
	leaderMap map[uint8]bool,
	regime string,
	sessionClock market.GlobalSessionClockDTO,
	netEquity, freeMargin, riskPct, drawdownMult float64,
	isSymbolUnavailable func(symbol string, token uint32) bool,
) (*CandidateSignal, string, string, []market.CandidateGateDiagnosticDTO) {
	regimeOK := true
	var regimeLockStatus, regimeLockReason string

	// Stage 0: Macro Regime Gate
	switch regime {
	case "FLASH_SHOCK":
		regimeOK = false
		regimeLockStatus = "LOCKED_MACRO_SHOCK"
		regimeLockReason = "Nifty 50 Flash Shock active: New entries frozen & pending buys cancelled"
	case "BEARISH_EXPANSION":
		regimeOK = false
		regimeLockStatus = "LOCKED_BEAR_REGIME"
		regimeLockReason = "Stage 0 Lockout: Market in Bearish Expansion (Breadth ≤ 42%)"
	case "INTRADAY_DISTRIBUTION":
		regimeOK = false
		regimeLockStatus = "LOCKED_DISTRIBUTION"
		regimeLockReason = "Stage 0 Lockout: Intraday Distribution / Gap-Up Rollover detected"
	}

	if regimeOK && !sessionClock.AllowNewEntries {
		regimeOK = false
		regimeLockStatus = "LOCKED_SESSION_WINDOW"
		regimeLockReason = sessionClock.PlaybookNote
	}

	minScore := sessionClock.MinBullishScore
	minRVOL := sessionClock.MinRVOL
	sizeMult := sessionClock.SizeMultiplier
	allowPatternB := true

	if regime == "SIDEWAYS_CHOP" {
		minScore = math.Max(minScore, 82.0)
		sizeMult = math.Min(sizeMult, 0.75)
		allowPatternB = false // Only allow Pattern A (VWAP Pullback) in Leading Sectors during chop
	} else if regime == "V_RECOVERY_BULLISH" {
		minScore = math.Max(minScore, 78.0)
		sizeMult = math.Min(sizeMult, 0.85)
	}
	if sessionClock.Phase == "MIDDAY_COMPRESSION" && !sessionClock.TapeOverrideActive {
		allowPatternB = false
	}

	maxScan := min(5, len(bullishSorted))
	diagnostics := make([]market.CandidateGateDiagnosticDTO, 0, maxScan)
	var winningSignal *CandidateSignal

	// Scan Top 5 Ranked Bullish Outliers
	for rk := 0; rk < maxScan; rk++ {
		idx := bullishSorted[rk]
		s := &stocks[idx]
		sym := s.SymbolString()
		compRVOL := math.Max(s.RVOLSession, 0.85*s.RVOLPace5m)

		diag := market.CandidateGateDiagnosticDTO{
			Rank:          rk + 1,
			Symbol:        sym,
			Sector:        screener.GetSectorName(s.SectorID),
			LTP:           s.LTP,
			BullishScore:  s.BullishScore,
			CompositeRVOL: compRVOL,
			EV:            s.EV,
			RegimeOK:      regimeOK,
			OverallStatus: "BLOCKED",
		}

		if !s.IsTradeable || !s.IsWarm || s.LTP <= 0 || s.ExchangeVWAP <= 0 || s.CurrentBarIdx < 1 {
			diag.BlockingReason = "Warming up 5m bars / VWAP feed"
			diagnostics = append(diagnostics, diag)
			continue
		}
		if isSymbolUnavailable != nil && isSymbolUnavailable(sym, s.InstrumentToken) {
			diag.OverallStatus = "ACTIVE_OR_COOLDOWN"
			diag.BlockingReason = "Position already active or in 15m post-exit cooldown"
			diagnostics = append(diagnostics, diag)
			continue
		}

		// Sector Leadership Check for Chop or V-Recovery
		isLeader := leaderMap[s.SectorID]
		if (regime == "SIDEWAYS_CHOP" || regime == "V_RECOVERY_BULLISH") && !isLeader {
			diag.RegimeOK = false
			diag.BlockingReason = fmt.Sprintf("Sector (%s) not in Top-2 Leaders during %s", diag.Sector, regime)
			diagnostics = append(diagnostics, diag)
			continue
		}

		diag.ScoreOK = s.BullishScore >= minScore && s.EV >= 0.25
		diag.RVOLOK = compRVOL >= minRVOL

		// Disqualifying Penalty Bitmask & Toxic Ask Wall Check
		disqualifyingMask := market.PenaltyWideSpread |
			market.PenaltyOverextended |
			market.PenaltyExhaustedTrend |
			market.PenaltyIcebergSell |
			market.PenaltyPullbackRedBar
		hasPenalty := (s.Reasons & disqualifyingMask) != 0
		top2Bid := uint64(s.BidLevels[0].Qty) + uint64(s.BidLevels[1].Qty)
		hasAskWall := top2Bid == 0 || uint64(s.AskLevels[0].Qty) > 4*top2Bid
		diag.BookQualityOK = !hasPenalty && !hasAskWall

		atrSafe := math.Max(s.ATR14, s.LTP*0.0025)
		tickSize := float64(s.TickSize)
		if tickSize <= 0 {
			tickSize = 0.05
		}

		currBar := s.SessionBars[s.CurrentBarIdx]
		prevBar := s.SessionBars[s.CurrentBarIdx-1]
		var clv float64 = 0.50
		if currBar.High > currBar.Low {
			clv = (s.LTP - currBar.Low) / (currBar.High - currBar.Low)
		}

		var matchedPattern string
		var signalBarLow float64

		// Pattern A: Institutional VWAP Pullback & Midpoint Reclaim (Aligned with CalculateBullishScore sVWAP)
		xVWAP := (s.LTP - s.ExchangeVWAP) / atrSafe
		prevMid := (prevBar.Open + prevBar.Close) / 2.0
		if xVWAP >= 0.05 && xVWAP <= 0.55 &&
			prevBar.Low > 0 &&
			prevBar.Low <= s.ExchangeVWAP+0.35*atrSafe &&
			prevBar.Low >= s.ExchangeVWAP-0.15*atrSafe &&
			s.LTP > math.Max(currBar.Open+2.0*tickSize, prevMid) &&
			clv >= 0.58 &&
			s.AggressorBuyRatio >= 0.56 &&
			s.FastAggressorEMA >= 0.55 &&
			s.SmoothedBuyerRatio >= 0.55 {
			matchedPattern = PatternVWAPPullbackReclaim
			signalBarLow = math.Min(prevBar.Low, currBar.Low)
		}

		// Pattern B: 15-Minute Opening Range Breakout (ORB)
		if matchedPattern == "" && allowPatternB && s.CurrentBarIdx >= 3 {
			orh, orl, orbReady := s.OpeningRange15m()
			if orbReady && (orh-orl) <= 1.8*atrSafe &&
				s.LTP > orh+tickSize &&
				(s.LTP-orh) <= 0.35*atrSafe &&
				compRVOL >= 1.8 &&
				clv >= 0.65 &&
				s.AggressorBuyRatio >= 0.60 &&
				s.FastAggressorEMA >= 0.58 &&
				s.SmoothedBuyerRatio >= 0.58 {
				matchedPattern = PatternORB15MBreakout
				signalBarLow = math.Max(currBar.Low, orh-0.85*atrSafe)
			}
		}
		diag.PatternMatched = matchedPattern

		if !regimeOK {
			diag.BlockingReason = regimeLockReason
			diagnostics = append(diagnostics, diag)
			continue
		}
		if !diag.ScoreOK {
			diag.BlockingReason = fmt.Sprintf("Score %.1f < %.0f or EV %.2fR < 0.25R", s.BullishScore, minScore, s.EV)
			diagnostics = append(diagnostics, diag)
			continue
		}
		if !diag.BookQualityOK {
			if hasAskWall {
				diag.BlockingReason = "Ask1 Wall > 4x Top-2 Bid Depth"
			} else {
				diag.BlockingReason = "Disqualifying Microstructure Penalty Flag"
			}
			diagnostics = append(diagnostics, diag)
			continue
		}
		if !diag.RVOLOK {
			diag.BlockingReason = fmt.Sprintf("Composite RVOL %.2fx < %.1fx threshold", compRVOL, minRVOL)
			diagnostics = append(diagnostics, diag)
			continue
		}
		if matchedPattern == "" {
			diag.BlockingReason = fmt.Sprintf("Awaiting VWAP Pullback (0.05–0.55 ATR, cur %.2f) or 15m ORB", xVWAP)
			diagnostics = append(diagnostics, diag)
			continue
		}

		sizing := ComputePositionSize(s, signalBarLow, netEquity, freeMargin, riskPct, sizeMult, drawdownMult)
		diag.SizingApproved = sizing.Approved
		if !sizing.Approved {
			diag.BlockingReason = sizing.RejectReason
			diagnostics = append(diagnostics, diag)
			continue
		}

		diag.OverallStatus = "READY"
		diag.BlockingReason = fmt.Sprintf("Qualified %s • Qty %d @ ₹%.2f (1R=₹%.2f)", matchedPattern, sizing.Quantity, sizing.EntryLimitPrice, sizing.StopSpec.OneR)
		diagnostics = append(diagnostics, diag)

		if winningSignal == nil {
			winningSignal = &CandidateSignal{
				StockIdx:     idx,
				Token:        s.InstrumentToken,
				Symbol:       sym,
				PatternType:  matchedPattern,
				SignalBarLow: signalBarLow,
				BullishScore: s.BullishScore,
				Sizing:       sizing,
			}
		}
	}

	if !regimeOK {
		return nil, regimeLockStatus, regimeLockReason, diagnostics
	}
	if winningSignal != nil {
		return winningSignal, "SIGNAL_MATCHED", fmt.Sprintf("Matched %s on %s (Qty %d)", winningSignal.PatternType, winningSignal.Symbol, winningSignal.Sizing.Quantity), diagnostics
	}

	return nil, "ARMED_SCANNING", fmt.Sprintf("Scanning Top 5 Leaders (%s • Min Score %.0f)", regime, minScore), diagnostics
}

