package execution

import (
	"fmt"
	"math"
	"time"
	_ "time/tzdata" // Embedded IANA timezone database for automatic UK BST / EU CEST / US EDT transitions

	"github.com/ajaynegi33/AlgoTrade/internal/engine"
	"github.com/ajaynegi33/AlgoTrade/internal/scanner"
)

type marketZoneSpec struct {
	ID        string
	Name      string
	IANA      string
	OpenMin   int // Minutes from midnight local time
	CloseMin  int
	LunchStart int
	LunchEnd   int
}

var globalZones = []marketZoneSpec{
	{ID: "NSE", Name: "India (NSE)", IANA: "Asia/Kolkata", OpenMin: 9*60 + 15, CloseMin: 15*60 + 30},
	{ID: "TOKYO", Name: "Tokyo (TSE)", IANA: "Asia/Tokyo", OpenMin: 9 * 60, CloseMin: 15*60 + 30, LunchStart: 11*60 + 30, LunchEnd: 12*60 + 30},
	{ID: "SHANGHAI", Name: "Shanghai / HK", IANA: "Asia/Shanghai", OpenMin: 9*60 + 30, CloseMin: 15 * 60, LunchStart: 11*60 + 30, LunchEnd: 13 * 60},
	{ID: "FRANKFURT", Name: "Frankfurt (XETRA)", IANA: "Europe/Berlin", OpenMin: 9 * 60, CloseMin: 17*60 + 30},
	{ID: "LONDON", Name: "London (LSE)", IANA: "Europe/London", OpenMin: 8 * 60, CloseMin: 16*60 + 30},
	{ID: "NEW_YORK", Name: "New York (NYSE)", IANA: "America/New_York", OpenMin: 9*60 + 30, CloseMin: 16 * 60},
}

func formatDurationMin(mins int) string {
	if mins <= 0 {
		return "0m"
	}
	h := mins / 60
	m := mins % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// EvaluateGlobalSessionClocks computes live DST-aware global exchange clocks and the active NSE intraday playbook
func EvaluateGlobalSessionClocks(now time.Time, breadthPct float64, topBullishRVOL float64) engine.GlobalSessionClockDTO {
	istLoc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		istLoc = time.FixedZone("IST", 5*3600+1800)
	}
	nowIST := now.In(istLoc)
	istMin := nowIST.Hour()*60 + nowIST.Minute()
	isWeekday := nowIST.Weekday() >= time.Monday && nowIST.Weekday() <= time.Friday

	clocks := make([]engine.MarketSessionItemDTO, 0, len(globalZones))
	var frankfurtOpen, londonOpen bool
	var minsToEuropeOpen int = 999

	for _, z := range globalZones {
		loc, locErr := time.LoadLocation(z.IANA)
		if locErr != nil {
			loc = istLoc
		}
		localNow := now.In(loc)
		localMin := localNow.Hour()*60 + localNow.Minute()
		localWeekday := localNow.Weekday() >= time.Monday && localNow.Weekday() <= time.Friday

		status := "CLOSED"
		label := "Closed"
		isOpen := false

		if !localWeekday {
			label = "Weekend"
		} else if localMin < z.OpenMin {
			diff := z.OpenMin - localMin
			if diff <= 30 {
				status = "PRE_OPEN"
			}
			label = fmt.Sprintf("Opens in %s", formatDurationMin(diff))
			if (z.ID == "FRANKFURT" || z.ID == "LONDON") && diff < minsToEuropeOpen {
				minsToEuropeOpen = diff
			}
		} else if z.LunchStart > 0 && localMin >= z.LunchStart && localMin < z.LunchEnd {
			status = "LUNCH"
			label = fmt.Sprintf("Lunch • Resumes in %s", formatDurationMin(z.LunchEnd-localMin))
		} else if localMin < z.CloseMin {
			status = "OPEN"
			isOpen = true
			label = fmt.Sprintf("Open • %s left", formatDurationMin(z.CloseMin-localMin))
			if z.ID == "FRANKFURT" {
				frankfurtOpen = true
			}
			if z.ID == "LONDON" {
				londonOpen = true
			}
		} else {
			label = "Session Closed"
		}

		clocks = append(clocks, engine.MarketSessionItemDTO{
			ID:          z.ID,
			Name:        z.Name,
			Timezone:    z.IANA,
			LocalTime:   localNow.Format("15:04:05"),
			Status:      status,
			StatusLabel: label,
			IsOpen:      isOpen,
		})
	}

	dto := engine.GlobalSessionClockDTO{
		ISTTime:         nowIST.Format("15:04:05 IST"),
		AllowNewEntries: false,
		MinBullishScore: 78.0,
		MinRVOL:         1.5,
		SizeMultiplier:  1.0,
		Clocks:          clocks,
	}

	if !isWeekday || istMin < 9*60+15 || istMin >= 15*60+30 {
		dto.Phase = "MARKET_CLOSED"
		dto.PhaseLabel = "NSE Market Closed"
		dto.PlaybookNote = "NSE Cash Equity intraday session runs 09:15–15:30 IST. Zero overnight exposure."
		return dto
	}

	switch {
	case istMin < 9*60+20:
		dto.Phase = "OPENING_NOISE_BLACKOUT"
		dto.PhaseLabel = "09:15–09:20 Opening Noise Blackout"
		dto.PlaybookNote = "Absorbing opening auction imbalances and building first 5-minute structural candle."
		dto.AllowNewEntries = false

	case istMin < 11*60+30:
		dto.Phase = "MORNING_DISCOVERY_DRIVE"
		dto.PhaseLabel = "Morning Institutional Discovery Drive"
		dto.PlaybookNote = "Peak domestic & Asia overlap liquidity. Pattern A (VWAP Pullback) & Pattern B (15m ORB) active."
		dto.AllowNewEntries = true
		dto.MinBullishScore = 75.0
		dto.MinRVOL = 1.5
		dto.SizeMultiplier = 1.00

	case !frankfurtOpen && !londonOpen && istMin < 13*60+30:
		dto.Phase = "MIDDAY_COMPRESSION"
		dto.AllowNewEntries = true
		// Check Live NSE Domestic Tape Override:
		if breadthPct >= 60.0 && topBullishRVOL >= 2.2 {
			dto.TapeOverrideActive = true
			dto.PhaseLabel = "Midday Window • Domestic Tape Override Active"
			dto.PlaybookNote = fmt.Sprintf("Breadth %.0f%% & RVOL %.1fx overriding midday lull (Europe opens in %s).", breadthPct, topBullishRVOL, formatDurationMin(minsToEuropeOpen))
			dto.MinBullishScore = 78.0
			dto.MinRVOL = 1.8
			dto.SizeMultiplier = 0.90
		} else {
			dto.PhaseLabel = "Midday Compression Guard"
			dto.PlaybookNote = fmt.Sprintf("Awaiting Frankfurt/London desk flow (%s). Strict Score ≥ 82 & VWAP Pullback only.", formatDurationMin(minsToEuropeOpen))
			dto.MinBullishScore = 82.0
			dto.MinRVOL = 1.8
			dto.SizeMultiplier = 0.65
		}

	case istMin < 14*60+45:
		dto.Phase = "EUROPE_OVERLAP_EXPANSION"
		dto.PhaseLabel = "Europe / London Overlap Expansion"
		dto.PlaybookNote = "Frankfurt & London institutional desks active. Second-wave trend breakouts & VWAP reclaims unlocked."
		dto.AllowNewEntries = true
		dto.MinBullishScore = 76.0
		dto.MinRVOL = 1.5
		dto.SizeMultiplier = 1.00

	case istMin < 15*60+14:
		dto.Phase = "EXIT_ONLY_LOCKOUT"
		dto.PhaseLabel = "14:45+ Exit-Only Trail Window"
		dto.PlaybookNote = "New entries locked out. Active runners trailing on Multi-Gear SL ahead of 15:14 IST square-off."
		dto.AllowNewEntries = false

	default:
		dto.Phase = "AUTO_SQUAREOFF_SWEEP"
		dto.PhaseLabel = "15:14 IST Mandatory MIS Square-Off"
		dto.PlaybookNote = "Flattening all remaining intraday MIS positions before Zerodha RMS 15:20 IST sweep."
		dto.AllowNewEntries = false
	}

	return dto
}

// CandidateSignal represents a qualified long trade setup selected by the Signal Picker
type CandidateSignal struct {
	StockIdx     uint16
	Token        uint32
	Symbol       string
	PatternType  string // "VWAP_PULLBACK_RECLAIM" | "ORB_15M_BREAKOUT"
	SignalBarLow float64
	BullishScore float64
	Sizing       SizingDecision
}

// PickBestLongCandidate runs Stage 0 Regime Filtering, Pattern A & B Structural Detection, and Pre-Trade Gatekeeping,
// while also producing the Top 5 CandidateGateDiagnosticDTO telemetry array for the Trader Cockpit UI.
func PickBestLongCandidate(
	stocks *[512]engine.StockMetrics,
	bullishSorted []uint16,
	leaderMap map[uint8]bool,
	regime string,
	sessionClock engine.GlobalSessionClockDTO,
	netEquity, freeMargin, riskPct, drawdownMult float64,
	isSymbolUnavailable func(symbol string, token uint32) bool,
) (*CandidateSignal, string, string, []engine.CandidateGateDiagnosticDTO) {
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
	diagnostics := make([]engine.CandidateGateDiagnosticDTO, 0, maxScan)
	var winningSignal *CandidateSignal

	// Scan Top 5 Ranked Bullish Outliers
	for rk := 0; rk < maxScan; rk++ {
		idx := bullishSorted[rk]
		s := &stocks[idx]
		sym := s.SymbolString()
		compRVOL := math.Max(s.RVOLSession, 0.85*s.RVOLPace5m)

		diag := engine.CandidateGateDiagnosticDTO{
			Rank:          rk + 1,
			Symbol:        sym,
			Sector:        scanner.GetSectorName(s.SectorID),
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
		disqualifyingMask := engine.PenaltyWideSpread |
			engine.PenaltyOverextended |
			engine.PenaltyExhaustedTrend |
			engine.PenaltyIcebergSell |
			engine.PenaltyPullbackRedBar
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
			matchedPattern = "VWAP_PULLBACK_RECLAIM"
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
				matchedPattern = "ORB_15M_BREAKOUT"
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
