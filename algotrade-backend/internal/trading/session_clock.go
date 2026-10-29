package trading

import (
	"fmt"
	"time"
	_ "time/tzdata" // Embedded IANA timezone database for automatic UK BST / EU CEST / US EDT transitions

	"github.com/ajaynegi33/AlgoTrade/internal/market"
)

type marketZoneSpec struct {
	ID         string
	Name       string
	IANA       string
	OpenMin    int // Minutes from midnight local time
	CloseMin   int
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

func formatDurationMin(totalMinutes int) string {
	if totalMinutes <= 0 {
		return "0m"
	}
	hours := totalMinutes / 60
	remainingMinutes := totalMinutes % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %02dm", hours, remainingMinutes)
	}
	return fmt.Sprintf("%dm", remainingMinutes)
}

// EvaluateGlobalSessionClocks computes live DST-aware global exchange clocks and the active NSE intraday playbook
func EvaluateGlobalSessionClocks(now time.Time, breadthPct float64, topBullishRVOL float64) market.GlobalSessionClockDTO {
	istLoc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		istLoc = ist
	}
	nowIST := now.In(istLoc)
	istMin := nowIST.Hour()*60 + nowIST.Minute()
	isWeekday := nowIST.Weekday() >= time.Monday && nowIST.Weekday() <= time.Friday

	marketSessionClocks := make([]market.MarketSessionItemDTO, 0, len(globalZones))
	var isFrankfurtOpen, isLondonOpen bool
	var minutesToEuropeOpen int = 999

	for _, zone := range globalZones {
		loc, locErr := time.LoadLocation(zone.IANA)
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
		} else if localMin < zone.OpenMin {
			diff := zone.OpenMin - localMin
			if diff <= 30 {
				status = "PRE_OPEN"
			}
			label = fmt.Sprintf("Opens in %s", formatDurationMin(diff))
			if (zone.ID == "FRANKFURT" || zone.ID == "LONDON") && diff < minutesToEuropeOpen {
				minutesToEuropeOpen = diff
			}
		} else if zone.LunchStart > 0 && localMin >= zone.LunchStart && localMin < zone.LunchEnd {
			status = "LUNCH"
			label = fmt.Sprintf("Lunch • Resumes in %s", formatDurationMin(zone.LunchEnd-localMin))
		} else if localMin < zone.CloseMin {
			status = "OPEN"
			isOpen = true
			label = fmt.Sprintf("Open • %s left", formatDurationMin(zone.CloseMin-localMin))
			if zone.ID == "FRANKFURT" {
				isFrankfurtOpen = true
			}
			if zone.ID == "LONDON" {
				isLondonOpen = true
			}
		} else {
			label = "Session Closed"
		}

		marketSessionClocks = append(marketSessionClocks, market.MarketSessionItemDTO{
			ID:          zone.ID,
			Name:        zone.Name,
			Timezone:    zone.IANA,
			LocalTime:   localNow.Format("15:04:05"),
			Status:      status,
			StatusLabel: label,
			IsOpen:      isOpen,
		})
	}

	dto := market.GlobalSessionClockDTO{
		ISTTime:           nowIST.Format("15:04:05 IST"),
		IsNewEntryAllowed: false,
		MinBullishScore:   78.0,
		MinRVOL:           1.5,
		SizeMultiplier:    1.0,
		Clocks:            marketSessionClocks,
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
		dto.IsNewEntryAllowed = false

	case istMin < 11*60+30:
		dto.Phase = "MORNING_DISCOVERY_DRIVE"
		dto.PhaseLabel = "Morning Institutional Discovery Drive"
		dto.PlaybookNote = "Peak domestic & Asia overlap liquidity. VWAP Pullback Reclaim & 15m Opening Range Breakout active."
		dto.IsNewEntryAllowed = true
		dto.MinBullishScore = 75.0
		dto.MinRVOL = 1.5
		dto.SizeMultiplier = 1.00

	case !isFrankfurtOpen && !isLondonOpen && istMin < 13*60+30:
		dto.Phase = "MIDDAY_COMPRESSION"
		dto.IsNewEntryAllowed = true
		// Check Live NSE Domestic Tape Override:
		if breadthPct >= 60.0 && topBullishRVOL >= 2.2 {
			dto.IsTapeOverrideActive = true
			dto.PhaseLabel = "Midday Window • Domestic Tape Override Active"
			dto.PlaybookNote = fmt.Sprintf("Breadth %.0f%% & RVOL %.1fx overriding midday lull (Europe opens in %s).", breadthPct, topBullishRVOL, formatDurationMin(minutesToEuropeOpen))
			dto.MinBullishScore = 78.0
			dto.MinRVOL = 1.8
			dto.SizeMultiplier = 0.90
		} else {
			dto.PhaseLabel = "Midday Compression Guard"
			dto.PlaybookNote = fmt.Sprintf("Awaiting Frankfurt/London desk flow (%s). Strict Score ≥ 82 & VWAP Pullback Reclaim only.", formatDurationMin(minutesToEuropeOpen))
			dto.MinBullishScore = 82.0
			dto.MinRVOL = 1.8
			dto.SizeMultiplier = 0.65
		}

	case istMin < 14*60+45:
		dto.Phase = "EUROPE_OVERLAP_EXPANSION"
		dto.PhaseLabel = "Europe / London Overlap Expansion"
		dto.PlaybookNote = "Frankfurt & London institutional desks active. Second-wave trend breakouts & VWAP reclaims unlocked."
		dto.IsNewEntryAllowed = true
		dto.MinBullishScore = 76.0
		dto.MinRVOL = 1.5
		dto.SizeMultiplier = 1.00

	case istMin < 15*60+14:
		dto.Phase = "EXIT_ONLY_LOCKOUT"
		dto.PhaseLabel = "14:45+ Exit-Only Trail Window"
		dto.PlaybookNote = "New entries locked out. Active runners trailing on Multi-Gear SL ahead of 15:14 IST square-off."
		dto.IsNewEntryAllowed = false

	default:
		dto.Phase = "AUTO_SQUAREOFF_SWEEP"
		dto.PhaseLabel = "15:14 IST Mandatory MIS Square-Off"
		dto.PlaybookNote = "Flattening all remaining intraday MIS positions before Zerodha RMS 15:20 IST sweep."
		dto.IsNewEntryAllowed = false
	}

	return dto
}
