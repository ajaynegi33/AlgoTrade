package kite

import (
	"math"
	"testing"
	"time"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/zerodha/gokiteconnect/v4/models"
)

func TestComputeBaselinesIgnoresOvernightGapInATR14(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	var candles []kiteconnect.HistoricalData

	// Day 1: 15 five-minute bars ending at 15:25 IST around ₹200.00 with exact ₹1.50 bar range (High - Low = 1.50)
	day1Start := time.Date(2026, 10, 22, 14, 15, 0, 0, ist)
	for i := 0; i < 15; i++ {
		barTime := day1Start.Add(time.Duration(i*5) * time.Minute)
		candles = append(candles, kiteconnect.HistoricalData{
			Date:   models.Time{Time: barTime},
			Open:   200.00,
			High:   201.00,
			Low:    199.50,
			Close:  200.00,
			Volume: 50000,
		})
	}

	// Day 2: 09:15 AM (slot 0) opens with a massive +₹15.00 (+7.5%) overnight gap at ₹215.00,
	// but its intraday 5-minute bar range is still ₹1.50 (High 216.00, Low 214.50).
	day2Start := time.Date(2026, 10, 23, 9, 15, 0, 0, ist)
	for i := 0; i < 6; i++ {
		barTime := day2Start.Add(time.Duration(i*5) * time.Minute)
		candles = append(candles, kiteconnect.HistoricalData{
			Date:   models.Time{Time: barTime},
			Open:   215.00,
			High:   216.00,
			Low:    214.50,
			Close:  215.20,
			Volume: 60000,
		})
	}

	svc := &HistoricalService{}
	profile := &StockBaselineProfile{Token: 300100}
	svc.computeBaselines(candles, profile, 215.20)

	// True 5m intraday ATR14 must be ~1.50 (never contaminated by the ₹15.00 overnight gap!)
	if math.Abs(profile.ATR14-1.50) > 0.05 {
		t.Fatalf("expected 5m ATR14 ~ 1.50 (uncontaminated by overnight gap), got %.4f", profile.ATR14)
	}
}
