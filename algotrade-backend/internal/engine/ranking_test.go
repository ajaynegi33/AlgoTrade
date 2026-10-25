package engine

import (
	"testing"
)

func TestCalculateBearishScoreCompositeRVOLAndReasonsBitmask(t *testing.T) {
	var stock StockMetrics
	stock.InstrumentToken = 779521
	stock.SetSymbol("SBIN")
	stock.TickSize = 0.05
	stock.LTP = 778.00
	stock.ExchangeVWAP = 778.50
	stock.Low = 772.00
	stock.High = 782.00
	stock.ATR14 = 2.00
	stock.RVOLSession = 0.95 // Quiet full-session RVOL
	stock.RVOLPace5m = 2.60  // Sudden 5m breakdown volume surge (0.85 * 2.60 = 2.21 >= 1.8)
	stock.AggressorBuyRatio = 0.35
	stock.SmoothedBuyerRatio = 0.36
	stock.CurrentBarIdx = 10
	stock.SessionBars[10] = OHLCVBar{
		Open:  779.00,
		High:  779.20,
		Low:   777.80,
		Close: 778.00,
	}

	score, ev, pWin, reasons := CalculateBearishScore(&stock, nil, nil, true, 0.38)
	if score <= 0 || pWin <= 0.44 {
		t.Fatalf("expected positive bearish score and elevated pWin from 5m RVOL surge, got score=%.2f pWin=%.2f ev=%.2f", score, pWin, ev)
	}
	if reasons&ReasonBearishBreakdown == 0 || reasons&ReasonSectorLagging == 0 {
		t.Fatalf("expected ReasonBearishBreakdown and ReasonSectorLagging in returned reasons, got %d", reasons)
	}
	if stock.Reasons&ReasonBearishBreakdown == 0 || stock.Reasons&ReasonSectorLagging == 0 {
		t.Fatalf("expected bearish reasons bitmask to be merged into stock.Reasons, got %d", stock.Reasons)
	}
}
