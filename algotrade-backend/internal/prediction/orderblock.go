package prediction

import (
	"math"

	"github.com/ajaynegi33/AlgoTrade/internal/engine"
)

// OrderBlockLevel represents an institutional Order Block zone and liquidity map
type OrderBlockLevel struct {
	Symbol              string    `json:"symbol"`
	Token               uint32    `json:"instrument_token"`
	Direction           string    `json:"direction"` // "BULLISH" | "BEARISH"
	ZoneLevelZ          float64   `json:"zone_level_z"`
	ZoneLow             float64   `json:"zone_low"`
	ZoneHigh            float64   `json:"zone_high"`
	InvalidationS1      float64   `json:"invalidation_s1"`
	InvalidationR1      float64   `json:"invalidation_r1"`
	TargetR1            float64   `json:"target_r1"`
	TargetR2            float64   `json:"target_r2"`
	TargetS1            float64   `json:"target_s1"`
	TargetS2            float64   `json:"target_s2"`
	OneR                float64   `json:"one_r"`
	RiskRewardRatio     float64   `json:"risk_reward_ratio"`
	BreakevenWinRatePct float64   `json:"breakeven_win_rate_pct"`
	ZoneAge             string    `json:"zone_age"` // "IMPULSE", "MARKED", "FIRST_RETEST", "LATER_RETEST", "BROKEN"
	RetestCount         int       `json:"retest_count"`
	IsChasing           bool      `json:"is_chasing"`
	ChasingRiskPerShare float64   `json:"chasing_risk_per_share"`
	ChasingRRRatio      float64   `json:"chasing_rr_ratio"`
	ChasingBEWinRatePct float64   `json:"chasing_be_win_rate_pct"`
	Candles5m           []Candle  `json:"candles_5m"`
}

type Candle struct {
	Timestamp int64   `json:"timestamp"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    uint64  `json:"volume"`
}

func round2(v float64) float64 {
	return math.Round(v*100.0) / 100.0
}

func extractSessionCandles(s *engine.StockMetrics) []Candle {
	barCount := int(s.CurrentBarIdx)
	if barCount < 2 {
		barCount = 2
	}
	if barCount > 75 {
		barCount = 75
	}

	candles := make([]Candle, 0, barCount)
	for i := 0; i < barCount; i++ {
		b := s.SessionBars[i]
		if b.Open > 0 {
			candles = append(candles, Candle{
				Timestamp: b.Timestamp,
				Open:      b.Open,
				High:      b.High,
				Low:       b.Low,
				Close:     b.Close,
				Volume:    b.Volume,
			})
		}
	}
	return candles
}

// DetectBullishOrderBlock extracts the structural Order Block from finished 5m candles
func DetectBullishOrderBlock(s *engine.StockMetrics) OrderBlockLevel {
	ltp := s.LTP
	if ltp <= 0 {
		ltp = 100.0
	}
	atrSafe := math.Max(s.ATR14, ltp*0.0030)
	tick := float64(s.TickSize)
	if tick <= 0 {
		tick = 0.05
	}

	candles := extractSessionCandles(s)
	barCount := len(candles)
	if barCount < 2 {
		barCount = 2
	}

	// Default fallback values if early in session
	zoneLow := round2(math.Max(tick, ltp-0.45*atrSafe))
	zoneHigh := round2(math.Max(tick, ltp-0.10*atrSafe))
	equilibriumZ := round2((zoneLow + zoneHigh) / 2.0)
	invalidationS1 := round2(math.Max(tick, zoneLow-2.0*tick))
	zoneAge := "MARKED"
	retests := 0

	// Scan backwards for the last impulse move that broke structure
	var impulseIdx int = -1
	for i := barCount - 1; i >= 1; i-- {
		b := s.SessionBars[i]
		prev := s.SessionBars[i-1]
		if b.Open > 0 && prev.Open > 0 {
			body := b.Close - b.Open
			if body >= 0.70*atrSafe && b.Close > prev.High {
				impulseIdx = i
				break
			}
		}
	}

	if impulseIdx >= 1 {
		// The Order Block is the last opposite or base candle before the impulse
		obCandle := s.SessionBars[impulseIdx-1]
		zoneLow = round2(obCandle.Low)
		zoneHigh = round2(math.Max(obCandle.Open, obCandle.Close))
		if zoneHigh <= zoneLow {
			zoneHigh = round2(zoneLow + 0.35*atrSafe)
		}
		equilibriumZ = round2((zoneLow + zoneHigh) / 2.0)
		invalidationS1 = round2(math.Max(tick, zoneLow-2.0*tick))

		// Check zone aging on completed 5m candle closes
		for i := impulseIdx + 1; i < barCount; i++ {
			b := s.SessionBars[i]
			if b.Open > 0 {
				if b.Close < invalidationS1 {
					zoneAge = "BROKEN"
					break
				}
				if b.Low <= zoneHigh && b.High >= zoneLow {
					retests++
				}
			}
		}

		if zoneAge != "BROKEN" {
			if retests == 0 {
				if ltp <= zoneHigh+0.15*atrSafe && ltp >= zoneLow {
					zoneAge = "FIRST_RETEST"
					retests = 1
				} else {
					zoneAge = "IMPULSE"
				}
			} else if retests == 1 {
				zoneAge = "FIRST_RETEST"
			} else {
				zoneAge = "LATER_RETEST"
			}
		}
	}

	oneR := round2(math.Max(2.0*tick, equilibriumZ-invalidationS1))
	targetR1 := round2(equilibriumZ + 0.65*oneR)
	targetR2 := round2(equilibriumZ + 1.85*oneR)

	// Sample setup math at Equilibrium Zone Z
	rrZ := round2((targetR2 - equilibriumZ) / oneR)
	beZ := round2((oneR / (oneR + (targetR2 - equilibriumZ))) * 100.0)

	// Setup math if chasing at current LTP
	isChasing := ltp > zoneHigh+0.25*atrSafe
	chaseRisk := round2(math.Max(tick, ltp-invalidationS1))
	chaseRoom := round2(math.Max(tick, targetR2-ltp))
	chaseRR := round2(chaseRoom / chaseRisk)
	chaseBE := round2((chaseRisk / (chaseRisk + chaseRoom)) * 100.0)

	return OrderBlockLevel{
		Symbol:              s.SymbolString(),
		Token:               s.InstrumentToken,
		Direction:           "BULLISH",
		ZoneLevelZ:          equilibriumZ,
		ZoneLow:             zoneLow,
		ZoneHigh:            zoneHigh,
		InvalidationS1:      invalidationS1,
		TargetR1:            targetR1,
		TargetR2:            targetR2,
		OneR:                oneR,
		RiskRewardRatio:     rrZ,
		BreakevenWinRatePct: beZ,
		ZoneAge:             zoneAge,
		RetestCount:         retests,
		IsChasing:           isChasing,
		ChasingRiskPerShare: chaseRisk,
		ChasingRRRatio:      chaseRR,
		ChasingBEWinRatePct: chaseBE,
		Candles5m:           candles,
	}
}

// DetectBearishOrderBlock extracts the structural Bearish Order Block from finished 5m candles
func DetectBearishOrderBlock(s *engine.StockMetrics) OrderBlockLevel {
	ltp := s.LTP
	if ltp <= 0 {
		ltp = 100.0
	}
	atrSafe := math.Max(s.ATR14, ltp*0.0030)
	tick := float64(s.TickSize)
	if tick <= 0 {
		tick = 0.05
	}

	candles := extractSessionCandles(s)
	barCount := len(candles)
	if barCount < 2 {
		barCount = 2
	}

	zoneHigh := round2(ltp + 0.45*atrSafe)
	zoneLow := round2(ltp + 0.10*atrSafe)
	equilibriumZ := round2((zoneLow + zoneHigh) / 2.0)
	invalidationR1 := round2(zoneHigh + 2.0*tick)
	zoneAge := "MARKED"
	retests := 0

	var breakdownIdx int = -1
	for i := barCount - 1; i >= 1; i-- {
		b := s.SessionBars[i]
		prev := s.SessionBars[i-1]
		if b.Open > 0 && prev.Open > 0 {
			body := b.Open - b.Close
			if body >= 0.70*atrSafe && b.Close < prev.Low {
				breakdownIdx = i
				break
			}
		}
	}

	if breakdownIdx >= 1 {
		obCandle := s.SessionBars[breakdownIdx-1]
		zoneHigh = round2(obCandle.High)
		zoneLow = round2(math.Min(obCandle.Open, obCandle.Close))
		if zoneLow >= zoneHigh {
			zoneLow = round2(zoneHigh - 0.35*atrSafe)
		}
		equilibriumZ = round2((zoneLow + zoneHigh) / 2.0)
		invalidationR1 = round2(zoneHigh + 2.0*tick)

		for i := breakdownIdx + 1; i < barCount; i++ {
			b := s.SessionBars[i]
			if b.Open > 0 {
				if b.Close > invalidationR1 {
					zoneAge = "BROKEN"
					break
				}
				if b.High >= zoneLow && b.Low <= zoneHigh {
					retests++
				}
			}
		}

		if zoneAge != "BROKEN" {
			if retests == 0 {
				if ltp >= zoneLow-0.15*atrSafe && ltp <= zoneHigh {
					zoneAge = "FIRST_RETEST"
					retests = 1
				} else {
					zoneAge = "IMPULSE"
				}
			} else if retests == 1 {
				zoneAge = "FIRST_RETEST"
			} else {
				zoneAge = "LATER_RETEST"
			}
		}
	}

	oneR := round2(math.Max(2.0*tick, invalidationR1-equilibriumZ))
	targetS1 := round2(math.Max(tick, equilibriumZ-0.65*oneR))
	targetS2 := round2(math.Max(tick, equilibriumZ-1.85*oneR))

	rrZ := round2((equilibriumZ - targetS2) / oneR)
	beZ := round2((oneR / (oneR + (equilibriumZ - targetS2))) * 100.0)

	isChasing := ltp < zoneLow-0.25*atrSafe
	chaseRisk := round2(math.Max(tick, invalidationR1-ltp))
	chaseRoom := round2(math.Max(tick, ltp-targetS2))
	chaseRR := round2(chaseRoom / chaseRisk)
	chaseBE := round2((chaseRisk / (chaseRisk + chaseRoom)) * 100.0)

	return OrderBlockLevel{
		Symbol:              s.SymbolString(),
		Token:               s.InstrumentToken,
		Direction:           "BEARISH",
		ZoneLevelZ:          equilibriumZ,
		ZoneLow:             zoneLow,
		ZoneHigh:            zoneHigh,
		InvalidationR1:      invalidationR1,
		TargetS1:            targetS1,
		TargetS2:            targetS2,
		OneR:                oneR,
		RiskRewardRatio:     rrZ,
		BreakevenWinRatePct: beZ,
		ZoneAge:             zoneAge,
		RetestCount:         retests,
		IsChasing:           isChasing,
		ChasingRiskPerShare: chaseRisk,
		ChasingRRRatio:      chaseRR,
		ChasingBEWinRatePct: chaseBE,
		Candles5m:           candles,
	}
}
