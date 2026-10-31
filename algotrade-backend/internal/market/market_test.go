package market

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	"github.com/ajaynegi33/AlgoTrade/internal/screener"
)

func TestStockMetricsAlignment(t *testing.T) {
	size := unsafe.Sizeof(StockMetrics{})
	if size%8 != 0 {
		t.Fatalf("StockMetrics size (%d bytes) is not divisible by 8! Contains unaligned padding.", size)
	}
	if size != 3976 {
		t.Logf("StockMetrics size: %d bytes (aligned to 8-byte boundary)", size)
	}

	barSize := unsafe.Sizeof(OHLCVBar{})
	if barSize != 48 {
		t.Fatalf("OHLCVBar size is %d, expected 48 bytes", barSize)
	}
}

func TestBinaryParser(t *testing.T) {
	// Construct a synthetic 184-byte equity packet
	pkt := make([]byte, 184)
	binary.BigEndian.PutUint32(pkt[0:4], 123456)             // Token
	binary.BigEndian.PutUint32(pkt[4:8], 150000)             // LTP = 1500.00
	binary.BigEndian.PutUint32(pkt[8:12], 100)               // LTQ
	binary.BigEndian.PutUint32(pkt[12:16], 149500)           // VWAP = 1495.00
	binary.BigEndian.PutUint32(pkt[16:20], 500000)           // Volume = 500,000
	binary.BigEndian.PutUint32(pkt[28:32], 149000)           // Open = 1490.00
	binary.BigEndian.PutUint32(pkt[32:36], 151000)           // High = 1510.00
	binary.BigEndian.PutUint32(pkt[36:40], 148500)           // Low = 1485.00
	binary.BigEndian.PutUint32(pkt[40:44], 148000)           // Close = 1480.00
	binary.BigEndian.PutUint32(pkt[60:64], 1700000000)       // ExchTime
	binary.BigEndian.PutUint32(pkt[64:68], 2000)             // Bid1 Qty
	binary.BigEndian.PutUint32(pkt[68:72], 149995)           // Bid1 Price = 1499.95
	binary.BigEndian.PutUint32(pkt[124:128], 1000)           // Ask1 Qty
	binary.BigEndian.PutUint32(pkt[128:132], 150005)         // Ask1 Price = 1500.05

	// Wrap in Kite frame: [NumPackets=1 (2B)][Len=184 (2B)][Packet (184B)]
	frame := make([]byte, 4+184)
	binary.BigEndian.PutUint16(frame[0:2], 1)
	binary.BigEndian.PutUint16(frame[2:4], 184)
	copy(frame[4:], pkt)

	ticks := ParseBinaryFrame(frame)
	if len(ticks) != 1 {
		t.Fatalf("Expected 1 tick, got %d", len(ticks))
	}

	tick := ticks[0]
	if tick.Token != 123456 {
		t.Errorf("Expected token 123456, got %d", tick.Token)
	}
	if tick.LTP != 1500.00 {
		t.Errorf("Expected LTP 1500.00, got %f", tick.LTP)
	}
	if tick.VWAP != 1495.00 {
		t.Errorf("Expected VWAP 1495.00, got %f", tick.VWAP)
	}
	if tick.Volume != 500000 {
		t.Errorf("Expected Volume 500000, got %d", tick.Volume)
	}
	if tick.ExchangeTime != 1700000000 {
		t.Errorf("Expected ExchangeTime 1700000000, got %d", tick.ExchangeTime)
	}
	if tick.Bid1Price != 1499.95 || tick.Ask1Price != 1500.05 {
		t.Errorf("Best quotes parsed incorrectly: Bid=%f, Ask=%f", tick.Bid1Price, tick.Ask1Price)
	}
}

func TestGaussianVWAPSweetSpotAndCLV(t *testing.T) {
	stock := &StockMetrics{
		LTP:                1002.50,
		ExchangeVWAP:       1000.00,
		ATR14:              10.0,
		TickSize:           0.05,
		PrevClose:          990.00,
		Open:               995.00,
		High:               1010.00,
		Low:                994.00,
		Volume:             500000,
		BaselineCumVolume:  200000,
		BaselineSlotVolume: 10000,
		Bid1Price:          1002.45,
		Ask1Price:          1002.55,
		SmoothedBuyerRatio: 0.62,
		AggressorBuyRatio:  0.64,
		CurrentBarIdx:      5,
	}
	// Setup 5m bar with green candle and high CLV
	stock.SessionBars[5] = OHLCVBar{
		Open:  1000.00,
		High:  1003.00,
		Low:   999.00,
		Close: 1002.50,
	}

	score, ev, pWin, reasons := CalculateBullishScore(stock, 0.65)
	if pWin < 0.65 {
		t.Errorf("Expected high confluence pWin >= 0.65, got %f", pWin)
	}
	if ev <= 0.20 {
		t.Errorf("Expected positive EV >= 0.20R, got %f", ev)
	}
	if reasons&ReasonVWAPSweetSpot == 0 {
		t.Errorf("Expected ReasonVWAPSweetSpot flag, got reasons: %b", reasons)
	}
	if reasons&ReasonOrderBookWall == 0 {
		t.Errorf("Expected ReasonOrderBookWall flag, got reasons: %b", reasons)
	}
	if score < 70.0 {
		t.Errorf("Expected high BullishScore >= 70, got %f", score)
	}
}

func TestOpeningRange15mHelper(t *testing.T) {
	stock := &StockMetrics{CurrentBarIdx: 4}
	stock.SessionBars[0] = OHLCVBar{High: 105.0, Low: 99.0}
	stock.SessionBars[1] = OHLCVBar{High: 107.5, Low: 101.0}
	stock.SessionBars[2] = OHLCVBar{High: 106.0, Low: 98.5}

	orh, orl, ready := stock.OpeningRange15m()
	if !ready {
		t.Fatalf("Expected ready=true for CurrentBarIdx >= 3")
	}
	if orh != 107.5 {
		t.Errorf("Expected ORH 107.5, got %f", orh)
	}
	if orl != 98.5 {
		t.Errorf("Expected ORL 98.5, got %f", orl)
	}
}

func TestSectorRestriction(t *testing.T) {
	health := SectorHealth{
		SectorID:          screener.SectorIT,
		SectorName:        "IT",
		AverageChange:     -1.2,
		BreadthAboveVWAP:  20.0,
		IsRestricted:      true,
		RestrictionReason: "Weak Breadth (20% > VWAP)",
	}

	if !health.IsRestricted {
		t.Fatalf("Expected IT sector to be restricted")
	}
	if health.BreadthAboveVWAP >= 40.0 {
		t.Errorf("Expected breadth < 40%%, got %f", health.BreadthAboveVWAP)
	}
}

func TestGetStockDetail_ETERNAL(t *testing.T) {
	actor := NewActor(nil, nil, nil, nil, nil)
	idx := actor.registerTrackedStock(1304833, "ETERNAL", 1, 0.05)
	s := &actor.stocks[idx]
	s.LTP = 316.60
	s.PrevClose = 304.45
	s.ExchangeVWAP = 311.20
	s.BidLevels[0] = DepthLevel{Price: 325.80, Qty: 184583, Orders: 20}
	s.AskLevels[0] = DepthLevel{Price: 306.90, Qty: 4547, Orders: 2}

	// 1. Exact match lookup
	detail, found := actor.GetStockDetail("ETERNAL")
	if !found || detail == nil {
		t.Fatalf("Expected GetStockDetail(ETERNAL) to return true, got false")
	}
	if detail.Symbol != "ETERNAL" {
		t.Errorf("Expected symbol ETERNAL, got %s", detail.Symbol)
	}
	if detail.Token != 1304833 {
		t.Errorf("Expected token 1304833, got %d", detail.Token)
	}
	if detail.Bids[0].Price != 325.80 {
		t.Errorf("Expected rounded Bid price 325.80, got %f", detail.Bids[0].Price)
	}

	// 2. Case invariance lookup
	detailLower, foundLower := actor.GetStockDetail("eternal")
	if !foundLower || detailLower == nil {
		t.Fatalf("Expected GetStockDetail(eternal) to be case-insensitive, got false")
	}

	// 3. Whitespace invariance lookup
	detailSpace, foundSpace := actor.GetStockDetail("  ETERNAL  ")
	if !foundSpace || detailSpace == nil {
		t.Fatalf("Expected GetStockDetail('  ETERNAL  ') to handle whitespace, got false")
	}

	// 4. Non-existent symbol lookup
	_, foundMissing := actor.GetStockDetail("INVALID_XYZ")
	if foundMissing {
		t.Errorf("Expected GetStockDetail(INVALID_XYZ) to return false, got true")
	}
}

func TestApplyBaselineProfileBackfillsTodayBarsAndOR15m(t *testing.T) {
	actor := NewActor(nil, nil, nil, nil, nil)
	actor.initializeTodayEpoch()
	idx := actor.registerTrackedStock(779521, "SBIN", screener.SectorBank, 0.05)

	profile := &kite.StockBaselineProfile{
		Token:      779521,
		ATR14:      4.25,
		IsFallback: false,
		TodayBars: []kite.TodaySlotBar{
			{SlotIdx: 0, Timestamp: actor.today0915Epoch, Open: 800.0, High: 804.0, Low: 798.5, Close: 802.0, Volume: 120000},
			{SlotIdx: 1, Timestamp: actor.today0915Epoch + 300, Open: 802.0, High: 806.5, Low: 801.0, Close: 805.0, Volume: 95000},
			{SlotIdx: 2, Timestamp: actor.today0915Epoch + 600, Open: 805.0, High: 805.5, Low: 800.0, Close: 804.0, Volume: 80000},
			{SlotIdx: 3, Timestamp: actor.today0915Epoch + 900, Open: 804.0, High: 808.0, Low: 803.5, Close: 807.5, Volume: 110000},
		},
	}
	profile.Slots[3] = kite.SlotBaseline{SlotIdx: 3, MedianVolume: 50000, MedianCumVol: 200000, ATR14: 4.25}

	actor.applyBaselineProfile(profile)

	s := &actor.stocks[idx]
	if !s.IsWarm {
		t.Fatalf("expected s.IsWarm=true after applying non-fallback profile")
	}
	orh, orl, ready := s.OpeningRange15m()
	if !ready || orh != 806.5 || orl != 798.5 {
		t.Fatalf("expected 15m ORB backfilled (ready=true, ORH=806.5, ORL=798.5), got ready=%v ORH=%.2f ORL=%.2f", ready, orh, orl)
	}
}

func TestSnapshotPublisher_FlashShockRegime(t *testing.T) {
	actor := NewActor(nil, nil, nil, nil, nil)
	actor.initializeTodayEpoch()

	// Register 10 stocks: 1 above VWAP (10% breadth <= 20%)
	for i := 0; i < 10; i++ {
		idx := actor.registerTrackedStock(uint32(10000+i), "TEST", screener.SectorNifty50, 0.05)
		s := &actor.stocks[idx]
		s.IsTradeable = true
		s.ExchangeVWAP = 100.0
		if i == 0 {
			s.LTP = 105.0 // Above VWAP
		} else {
			s.LTP = 95.0 // Below VWAP
		}
	}

	actor.computeAndBroadcastSnapshot()

	// Verify market regime derived from <= 20% breadth
	aboveVWAPCount := 0
	activeQuotedCount := 0
	for _, idx := range actor.activeStockIndices {
		st := &actor.stocks[idx]
		if st.IsTradeable && st.LTP > 0 && st.ExchangeVWAP > 0 {
			activeQuotedCount++
			if st.LTP > st.ExchangeVWAP {
				aboveVWAPCount++
			}
		}
	}
	breadthRatio := float64(aboveVWAPCount) / float64(activeQuotedCount)
	if breadthRatio > 0.20 {
		t.Fatalf("expected breadth <= 0.20, got %.2f", breadthRatio)
	}
}


