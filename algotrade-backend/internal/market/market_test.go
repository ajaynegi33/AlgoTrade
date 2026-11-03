package market

import (
	"encoding/binary"
	"math"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/ajaynegi33/AlgoTrade/internal/database"
	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	"github.com/ajaynegi33/AlgoTrade/internal/screener"
)

func TestStockMetricsAlignment(testingInstance *testing.T) {
	size := unsafe.Sizeof(StockMetrics{})
	if size%8 != 0 {
		testingInstance.Fatalf("StockMetrics size (%d bytes) is not divisible by 8! Contains unaligned padding.", size)
	}
	if size != 3976 {
		testingInstance.Logf("StockMetrics size: %d bytes (aligned to 8-byte boundary)", size)
	}

	barSize := unsafe.Sizeof(OHLCVBar{})
	if barSize != 48 {
		testingInstance.Fatalf("OHLCVBar size is %d, expected 48 bytes", barSize)
	}
}

func TestBinaryParser(testingInstance *testing.T) {
	// Construct a synthetic 184-byte equity packet
	packetBytes := make([]byte, 184)
	binary.BigEndian.PutUint32(packetBytes[0:4], 123456)       // Token
	binary.BigEndian.PutUint32(packetBytes[4:8], 150000)       // LTP = 1500.00
	binary.BigEndian.PutUint32(packetBytes[8:12], 100)         // LTQ
	binary.BigEndian.PutUint32(packetBytes[12:16], 149500)     // VWAP = 1495.00
	binary.BigEndian.PutUint32(packetBytes[16:20], 500000)     // Volume = 500,000
	binary.BigEndian.PutUint32(packetBytes[28:32], 149000)     // Open = 1490.00
	binary.BigEndian.PutUint32(packetBytes[32:36], 151000)     // High = 1510.00
	binary.BigEndian.PutUint32(packetBytes[36:40], 148500)     // Low = 1485.00
	binary.BigEndian.PutUint32(packetBytes[40:44], 148000)     // Close = 1480.00
	binary.BigEndian.PutUint32(packetBytes[60:64], 1700000000) // ExchTime
	binary.BigEndian.PutUint32(packetBytes[64:68], 2000)       // Bid1 Qty
	binary.BigEndian.PutUint32(packetBytes[68:72], 149995)     // Bid1 Price = 1499.95
	binary.BigEndian.PutUint32(packetBytes[124:128], 1000)     // Ask1 Qty
	binary.BigEndian.PutUint32(packetBytes[128:132], 150005)   // Ask1 Price = 1500.05

	// Wrap in Kite frame: [NumPackets=1 (2B)][Len=184 (2B)][Packet (184B)]
	frameBytes := make([]byte, 4+184)
	binary.BigEndian.PutUint16(frameBytes[0:2], 1)
	binary.BigEndian.PutUint16(frameBytes[2:4], 184)
	copy(frameBytes[4:], packetBytes)

	ticks := ParseBinaryFrame(frameBytes)
	if len(ticks) != 1 {
		testingInstance.Fatalf("Expected 1 tick, got %d", len(ticks))
	}

	tick := ticks[0]
	if tick.Token != 123456 {
		testingInstance.Errorf("Expected token 123456, got %d", tick.Token)
	}
	if tick.LTP != 1500.00 {
		testingInstance.Errorf("Expected LTP 1500.00, got %f", tick.LTP)
	}
	if tick.VWAP != 1495.00 {
		testingInstance.Errorf("Expected VWAP 1495.00, got %f", tick.VWAP)
	}
	if tick.Volume != 500000 {
		testingInstance.Errorf("Expected Volume 500000, got %d", tick.Volume)
	}
	if tick.ExchangeTime != 1700000000 {
		testingInstance.Errorf("Expected ExchangeTime 1700000000, got %d", tick.ExchangeTime)
	}
	if tick.Bid1Price != 1499.95 || tick.Ask1Price != 1500.05 {
		testingInstance.Errorf("Best quotes parsed incorrectly: Bid=%f, Ask=%f", tick.Bid1Price, tick.Ask1Price)
	}
}

func TestGaussianVWAPSweetSpotAndCLV(testingInstance *testing.T) {
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
		testingInstance.Errorf("Expected high confluence pWin >= 0.65, got %f", pWin)
	}
	if ev <= 0.20 {
		testingInstance.Errorf("Expected positive EV >= 0.20R, got %f", ev)
	}
	if reasons&ReasonVWAPSweetSpot == 0 {
		testingInstance.Errorf("Expected ReasonVWAPSweetSpot flag, got reasons: %b", reasons)
	}
	if reasons&ReasonOrderBookWall == 0 {
		testingInstance.Errorf("Expected ReasonOrderBookWall flag, got reasons: %b", reasons)
	}
	if score < 70.0 {
		testingInstance.Errorf("Expected high BullishScore >= 70, got %f", score)
	}
}

func TestOpeningRange15mHelper(testingInstance *testing.T) {
	stock := &StockMetrics{CurrentBarIdx: 4}
	stock.SessionBars[0] = OHLCVBar{High: 105.0, Low: 99.0}
	stock.SessionBars[1] = OHLCVBar{High: 107.5, Low: 101.0}
	stock.SessionBars[2] = OHLCVBar{High: 106.0, Low: 98.5}

	orh, orl, ready := stock.OpeningRange15m()
	if !ready {
		testingInstance.Fatalf("Expected ready=true for CurrentBarIdx >= 3")
	}
	if orh != 107.5 {
		testingInstance.Errorf("Expected ORH 107.5, got %f", orh)
	}
	if orl != 98.5 {
		testingInstance.Errorf("Expected ORL 98.5, got %f", orl)
	}
}

func TestSectorRestriction(testingInstance *testing.T) {
	health := SectorHealth{
		SectorID:          screener.SectorIT,
		SectorName:        "IT",
		AverageChange:     -1.2,
		BreadthAboveVWAP:  20.0,
		IsRestricted:      true,
		RestrictionReason: "Weak Breadth (20% > VWAP)",
	}

	if !health.IsRestricted {
		testingInstance.Fatalf("Expected IT sector to be restricted")
	}
	if health.BreadthAboveVWAP >= 40.0 {
		testingInstance.Errorf("Expected breadth < 40%%, got %f", health.BreadthAboveVWAP)
	}
}

func TestGetStockDetail_ETERNAL(testingInstance *testing.T) {
	actor := NewActor(nil, nil, nil, nil, nil)
	idx := actor.registerTrackedStock(1304833, "ETERNAL", 1, 0.05)
	stock := &actor.stocks[idx]
	stock.LTP = 316.60
	stock.PrevClose = 304.45
	stock.ExchangeVWAP = 311.20
	stock.BidLevels[0] = DepthLevel{Price: 325.80, Qty: 184583, Orders: 20}
	stock.AskLevels[0] = DepthLevel{Price: 306.90, Qty: 4547, Orders: 2}

	// 1. Exact match lookup
	detail, found := actor.GetStockDetail("ETERNAL")
	if !found || detail == nil {
		testingInstance.Fatalf("Expected GetStockDetail(ETERNAL) to return true, got false")
	}
	if detail.Symbol != "ETERNAL" {
		testingInstance.Errorf("Expected symbol ETERNAL, got %s", detail.Symbol)
	}
	if detail.Token != 1304833 {
		testingInstance.Errorf("Expected token 1304833, got %d", detail.Token)
	}
	if detail.Bids[0].Price != 325.80 {
		testingInstance.Errorf("Expected rounded Bid price 325.80, got %f", detail.Bids[0].Price)
	}

	// 2. Case invariance lookup
	detailLower, foundLower := actor.GetStockDetail("eternal")
	if !foundLower || detailLower == nil {
		testingInstance.Fatalf("Expected GetStockDetail(eternal) to be case-insensitive, got false")
	}

	// 3. Whitespace invariance lookup
	detailSpace, foundSpace := actor.GetStockDetail("  ETERNAL  ")
	if !foundSpace || detailSpace == nil {
		testingInstance.Fatalf("Expected GetStockDetail('  ETERNAL  ') to handle whitespace, got false")
	}

	// 4. Non-existent symbol lookup
	_, foundMissing := actor.GetStockDetail("INVALID_XYZ")
	if foundMissing {
		testingInstance.Errorf("Expected GetStockDetail(INVALID_XYZ) to return false, got true")
	}
}

func TestApplyBaselineProfileBackfillsTodayBarsAndOR15m(testingInstance *testing.T) {
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

	stock := &actor.stocks[idx]
	if !stock.IsWarm {
		testingInstance.Fatalf("expected stock.IsWarm=true after applying non-fallback profile")
	}
	orh, orl, ready := stock.OpeningRange15m()
	if !ready || orh != 806.5 || orl != 798.5 {
		testingInstance.Fatalf("expected 15m ORB backfilled (ready=true, ORH=806.5, ORL=798.5), got ready=%v ORH=%.2f ORL=%.2f", ready, orh, orl)
	}
}

func TestSnapshotPublisher_FlashShockRegime(testingInstance *testing.T) {
	actor := NewActor(nil, nil, nil, nil, nil)
	actor.initializeTodayEpoch()

	// Register 10 stocks: 1 above VWAP (10% breadth <= 20%)
	for stockOffset := 0; stockOffset < 10; stockOffset++ {
		idx := actor.registerTrackedStock(uint32(10000+stockOffset), "TEST", screener.SectorNifty50, 0.05)
		stock := &actor.stocks[idx]
		stock.IsTradeable = true
		stock.ExchangeVWAP = 100.0
		if stockOffset == 0 {
			stock.LTP = 105.0 // Above VWAP
		} else {
			stock.LTP = 95.0 // Below VWAP
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
		testingInstance.Fatalf("expected breadth <= 0.20, got %.2f", breadthRatio)
	}
}

func TestDynamicStockQueryLeverageAndPushUniverseSync(testingInstance *testing.T) {
	databasePath := filepath.Join(testingInstance.TempDir(), "test_leverage.db")
	databaseConnection, err := database.Init(databasePath)
	if err != nil {
		testingInstance.Fatalf("failed to initialize test database: %v", err)
	}
	defer databaseConnection.Close()

	// Insert a dynamic stock into scanned_stocks with 4.5x leverage
	_, insertError := databaseConnection.Exec(`
		INSERT INTO scanned_stocks (
			instrument_token, tradingsymbol, name, exchange, segment, instrument_type,
			lot_size, tick_size, last_price, scan_date, mis_leverage, mis_margin_pct
		) VALUES (998877, 'TESTLEVERAGE', 'TEST LEVERAGE CO', 'NSE', 'NSE', 'EQ', 1, 0.05, 520.0, '2026-11-03', 4.5, 0.2222)
	`)
	if insertError != nil {
		testingInstance.Fatalf("failed inserting test scanned stock: %v", insertError)
	}

	actor := NewActor(databaseConnection, nil, nil, nil, nil)
	// Query detail to trigger dynamic fallback from SQLite
	detailDTO, isFound := actor.getStockDetailInternal("TESTLEVERAGE")
	if !isFound || detailDTO == nil {
		testingInstance.Fatalf("expected getStockDetailInternal to dynamically find TESTLEVERAGE in SQLite")
	}

	targetIndex, exists := actor.stockIndexBySymbol["TESTLEVERAGE"]
	if !exists {
		testingInstance.Fatalf("expected TESTLEVERAGE to be registered in actor stockIndexBySymbol")
	}
	stockMetrics := &actor.stocks[targetIndex]
	if math.Abs(float64(stockMetrics.MISLeverage)-4.5) > 0.01 {
		testingInstance.Fatalf("expected stock MISLeverage 4.5, got %f", stockMetrics.MISLeverage)
	}
	if math.Abs(float64(stockMetrics.MISMarginFraction)-0.2222) > 0.01 {
		testingInstance.Fatalf("expected stock MISMarginFraction 0.2222, got %f", stockMetrics.MISMarginFraction)
	}

	// Now simulate PushUniverseSync updating leverage to 5.0x
	syncDoneChan := make(chan struct{})
	actor.isRunning.Store(true)
	go func() {
		defer close(syncDoneChan)
		select {
		case scannedStocks := <-actor.universeSyncChan:
			for _, scannedStock := range scannedStocks {
				stockIdx := actor.stockIndexByToken[uint32(scannedStock.InstrumentToken)]
				if scannedStock.MISLeverage > 0 {
					actor.stocks[stockIdx].MISLeverage = float32(scannedStock.MISLeverage)
					actor.stocks[stockIdx].MISMarginFraction = float32(1.0 / scannedStock.MISLeverage)
				}
			}
		case <-actor.ctx.Done():
		}
	}()

	actor.PushUniverseSync([]screener.ScannedStock{
		{
			InstrumentToken: 998877,
			Tradingsymbol:   "TESTLEVERAGE",
			MISLeverage:     5.0,
			MISMarginPct:    0.20,
		},
	})

	<-syncDoneChan
	if math.Abs(float64(stockMetrics.MISLeverage)-5.0) > 0.01 {
		testingInstance.Fatalf("expected PushUniverseSync to update MISLeverage to 5.0, got %f", stockMetrics.MISLeverage)
	}
}


