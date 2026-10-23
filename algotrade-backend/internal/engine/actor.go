package engine

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	"github.com/ajaynegi33/AlgoTrade/internal/scanner"
	"github.com/ajaynegi33/AlgoTrade/internal/ws"
)

const (
	maxTrackedStocks   = 512
	nifty50Token       = 256265
	bankNiftyToken     = 260105
)

// BarFlushItem holds a finalized 5m candle for background SQLite persistence
type BarFlushItem struct {
	Token uint32
	Bar   OHLCVBar
}

// Actor is the high-performance Single-Writer Actor goroutine
type Actor struct {
	db             *sql.DB
	tickerClient   *kite.TickerClient
	histClient     *kite.HistoricalService
	wsHub          *ws.ClientHub
	scannerService *scanner.Service

	// Pure Actor Memory: Owned exclusively by the actor goroutine (zero mutexes in hot loop)
	stocks         [maxTrackedStocks]StockMetrics
	tokenMap       map[uint32]uint16
	symbolMap      map[string]uint16
	activeIndices  []uint16
	sectorIndices  map[uint8]uint16
	niftyIdx       uint16
	bankNiftyIdx   uint16
	hasNifty       bool
	hasBankNifty   bool

	barFlushChan   chan BarFlushItem
	today0915Epoch int64
	ticksProcessed atomic.Uint64
	isRunning      atomic.Bool

	ctx            context.Context
	cancel         context.CancelFunc
}

// NewActor creates a new Single-Writer Actor instance
func NewActor(
	db *sql.DB,
	tickerClient *kite.TickerClient,
	histClient *kite.HistoricalService,
	wsHub *ws.ClientHub,
	scannerService *scanner.Service,
) *Actor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Actor{
		db:             db,
		tickerClient:   tickerClient,
		histClient:     histClient,
		wsHub:          wsHub,
		scannerService: scannerService,
		tokenMap:       make(map[uint32]uint16, maxTrackedStocks),
		symbolMap:      make(map[string]uint16, maxTrackedStocks),
		activeIndices:  make([]uint16, 0, maxTrackedStocks),
		sectorIndices:  make(map[uint8]uint16, 16),
		barFlushChan:   make(chan BarFlushItem, 4096),
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start boots the actor goroutine and background SQLite writer
func (a *Actor) Start() error {
	if a.isRunning.CompareAndSwap(false, true) {
		a.initTodayEpoch()

		// 1. Initialize universe & baselines
		if err := a.seedUniverse(); err != nil {
			slog.Warn("Universe seed encountered issue, will run with partial set", "error", err)
		}

		// 2. Start background SQLite bar flush worker
		go a.sqliteBarWriter()

		// 3. Start Actor hot loop
		go a.runLoop()

		slog.Info("Single-Writer Engine Actor started successfully",
			"tracked_stocks", len(a.activeIndices),
			"token_map_size", len(a.tokenMap),
		)
	}
	return nil
}

// Stop shuts down the actor
func (a *Actor) Stop() {
	if a.isRunning.CompareAndSwap(true, false) {
		a.cancel()
		slog.Info("Engine Actor stopped")
	}
}

// GetStats returns actor performance stats
func (a *Actor) GetStats() (trackedCount int, ticksProcessed uint64) {
	return len(a.activeIndices), a.ticksProcessed.Load()
}

func (a *Actor) initTodayEpoch() {
	now := time.Now().In(istZone)
	y, m, d := now.Date()
	today0915 := time.Date(y, m, d, 9, 15, 0, 0, istZone)
	a.today0915Epoch = today0915.Unix()
}

func (a *Actor) seedUniverse() error {
	// 1. Add Index Tokens: Nifty 50 and Bank Nifty
	a.addStock(nifty50Token, "NIFTY 50", scanner.SectorNifty50, 0.05)
	a.niftyIdx = a.tokenMap[nifty50Token]
	a.hasNifty = true

	a.addStock(bankNiftyToken, "NIFTY BANK", scanner.SectorBank, 0.05)
	a.bankNiftyIdx = a.tokenMap[bankNiftyToken]
	a.hasBankNifty = true
	a.sectorIndices[scanner.SectorBank] = a.bankNiftyIdx

	// 2. Add Sector Tokens
	for _, info := range scanner.SectorRegistry {
		if info.Token != nifty50Token && info.Token != bankNiftyToken {
			a.addStock(info.Token, info.Name, info.ID, 0.05)
			a.sectorIndices[info.ID] = a.tokenMap[info.Token]
		}
	}

	// 3. Load Scanned F&O Cash Universe from scanner
	if a.scannerService != nil {
		todayStr := time.Now().In(istZone).Format("2006-01-02")
		universe, err := a.scannerService.GetUniverse(todayStr)
		if err == nil && universe != nil && len(universe.Stocks) > 0 {
			for _, st := range universe.Stocks {
				secID, _, _ := scanner.ResolveSector(a.db, st.Tradingsymbol)
				tickSize := float32(st.TickSize)
				if tickSize <= 0 {
					tickSize = 0.05
				}
				idx := a.addStock(uint32(st.InstrumentToken), st.Tradingsymbol, secID, tickSize)
				a.stocks[idx].LotSize = uint32(st.LotSize)
				a.stocks[idx].PrevClose = st.LastPrice
				a.stocks[idx].IsTradeable = true
			}
		}
	}

	// 4. Pre-seed 20-Day Baselines in background
	go a.seedBaselinesAsync()

	// 5. Subscribe all tokens on Kite Ticker
	if a.tickerClient != nil {
		tokens := make([]uint32, 0, len(a.tokenMap))
		for tok := range a.tokenMap {
			tokens = append(tokens, tok)
		}
		_ = a.tickerClient.Subscribe(tokens)
	}

	return nil
}

func (a *Actor) addStock(token uint32, symbol string, sectorID uint8, tickSize float32) uint16 {
	upper := strings.ToUpper(strings.TrimSpace(symbol))
	if idx, exists := a.tokenMap[token]; exists {
		a.symbolMap[upper] = idx
		return idx
	}

	nextIdx := uint16(len(a.tokenMap))
	if int(nextIdx) >= maxTrackedStocks {
		return 0
	}

	a.tokenMap[token] = nextIdx
	a.symbolMap[upper] = nextIdx
	s := &a.stocks[nextIdx]
	s.InstrumentToken = token
	s.SetSymbol(symbol)
	s.SectorID = sectorID
	s.TickSize = tickSize
	s.ActiveIndex = nextIdx
	s.SmoothedBuyerRatio = 0.50
	s.AggressorBuyRatio = 0.50

	// Initialize bar 0
	s.SessionBars[0] = OHLCVBar{Timestamp: a.today0915Epoch}

	// Only add tradeable equities to activeIndices for ranking (skip pure index tokens)
	if token != nifty50Token && token != bankNiftyToken && !strings.HasPrefix(symbol, "NIFTY ") {
		a.activeIndices = append(a.activeIndices, nextIdx)
	}

	return nextIdx
}

func (a *Actor) seedBaselinesAsync() {
	for _, idx := range a.activeIndices {
		s := &a.stocks[idx]
		profile, err := a.histClient.SeedStockBaselines(s.InstrumentToken, s.PrevClose)
		if err == nil && profile != nil {
			s.ATR14 = profile.ATR14
			s.BaselineSlotVolume = profile.Slots[0].MedianVolume
			s.BaselineCumVolume = profile.Slots[0].MedianCumVol
			s.IsWarm = true
		}
	}
	slog.Info("Completed 20-day baseline seeding for all active equities")
}

func (a *Actor) runLoop() {
	rawFeed := a.tickerClient.SubscribeRaw()
	periodic1s := time.NewTicker(1000 * time.Millisecond)
	defer periodic1s.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return

		case rawBytes, ok := <-rawFeed:
			if !ok {
				return
			}
			ticks := ParseBinaryFrame(rawBytes)
			for i := range ticks {
				a.processTick(&ticks[i])
			}

		case <-periodic1s.C:
			a.computeAndBroadcastSnapshot()
		}
	}
}

func (a *Actor) processTick(tick *ParsedTick) {
	idx, exists := a.tokenMap[tick.Token]
	if !exists {
		return
	}

	a.ticksProcessed.Add(1)
	stock := &a.stocks[idx]

	// Micro-Bug 1 Fix: First-tick baseline seeding & delta safety
	if stock.LastUpdatedUnix == 0 {
		stock.Volume = tick.Volume
		stock.BarStartVolume = tick.Volume
		stock.Bid1Price = tick.Bid1Price
		stock.Ask1Price = tick.Ask1Price
		stock.BidLevels = tick.BidLevels
		stock.AskLevels = tick.AskLevels
		stock.LTP = tick.LTP
		stock.Open = tick.Open
		stock.High = tick.High
		stock.Low = tick.Low
		stock.PrevClose = tick.PrevClose
		stock.ExchangeVWAP = tick.VWAP
		stock.LastUpdatedUnix = tick.ExchangeTime
		return
	}

	// Prevent out-of-order ticks from underflowing uint64
	var tickDeltaVol uint64
	if tick.Volume > stock.Volume {
		tickDeltaVol = tick.Volume - stock.Volume
	}

	// Micro-Bug 2 Fix: Check 5-minute bar rollover (exchTime / 300)
	barIdx := (tick.ExchangeTime - a.today0915Epoch) / 300
	if barIdx > int64(stock.CurrentBarIdx) && barIdx < 75 {
		// Finalize old bar
		oldIdx := stock.CurrentBarIdx
		stock.SessionBars[oldIdx].Close = stock.LTP
		if stock.SessionBars[oldIdx].Volume == 0 {
			stock.SessionBars[oldIdx].Volume = stock.Volume - stock.BarStartVolume
		}

		// Queue old bar for background SQLite flush
		select {
		case a.barFlushChan <- BarFlushItem{Token: stock.InstrumentToken, Bar: stock.SessionBars[oldIdx]}:
		default:
		}

		// Advance to new bar
		stock.CurrentBarIdx = uint8(barIdx)
		stock.SessionBars[barIdx] = OHLCVBar{
			Open:      tick.LTP,
			High:      tick.LTP,
			Low:       tick.LTP,
			Close:     tick.LTP,
			Timestamp: a.today0915Epoch + barIdx*300,
		}

		// Carry over 25% flow memory via bit shift (prevents 0.50 reset drop)
		stock.BarStartVolume = tick.Volume
		stock.BarBuyVolume = stock.BarBuyVolume >> 2
		stock.BarSellVolume = stock.BarSellVolume >> 2
	}

	// Update current 5m bar High/Low/Close
	currBar := &stock.SessionBars[stock.CurrentBarIdx]
	if currBar.Open == 0 {
		currBar.Open = tick.LTP
	}
	if tick.LTP > currBar.High || currBar.High == 0 {
		currBar.High = tick.LTP
	}
	if tick.LTP < currBar.Low || currBar.Low == 0 {
		currBar.Low = tick.LTP
	}
	currBar.Close = tick.LTP

	// Update stock scalar quotes
	stock.LTP = tick.LTP
	if tick.VWAP > 0 {
		stock.ExchangeVWAP = tick.VWAP
	}
	if tick.Open > 0 {
		stock.Open = tick.Open
	}
	if tick.High > 0 {
		stock.High = tick.High
	}
	if tick.Low > 0 {
		stock.Low = tick.Low
	}
	if tick.PrevClose > 0 {
		stock.PrevClose = tick.PrevClose
	}
	stock.Volume = tick.Volume
	stock.LastUpdatedUnix = tick.ExchangeTime

	if !tick.IsIndex {
		stock.Bid1Price = tick.Bid1Price
		stock.Ask1Price = tick.Ask1Price
		stock.BidLevels = tick.BidLevels
		stock.AskLevels = tick.AskLevels

		// Dual-alpha EMA Smoothing & Lee-Ready Aggressor Flow
		var alpha float32 = 0.03
		if tickDeltaVol > 0 {
			alpha = 0.20

			// 1. Quote Rule: Compare LTP against previous quotes
			if tick.LTP >= float64(stock.Ask1Price) && stock.Ask1Price > 0 {
				stock.BarBuyVolume += tickDeltaVol
			} else if tick.LTP <= float64(stock.Bid1Price) && stock.Bid1Price > 0 {
				stock.BarSellVolume += tickDeltaVol
			} else {
				// Midpoint check
				midpoint := float64(stock.Bid1Price+stock.Ask1Price) / 2.0
				if tick.LTP > midpoint {
					stock.BarBuyVolume += tickDeltaVol
				} else if tick.LTP < midpoint {
					stock.BarSellVolume += tickDeltaVol
				} else {
					// Tick rule fallback
					if tick.LTP >= stock.LTP {
						stock.BarBuyVolume += tickDeltaVol
					} else {
						stock.BarSellVolume += tickDeltaVol
					}
				}
			}
		}

		stock.SmoothedBuyerRatio = alpha*tick.RawBuyerRatio + (1.0-alpha)*stock.SmoothedBuyerRatio

		if totalBarVol := stock.BarBuyVolume + stock.BarSellVolume; totalBarVol >= 500 {
			stock.AggressorBuyRatio = float32(stock.BarBuyVolume) / float32(totalBarVol)
		} else {
			stock.AggressorBuyRatio = 0.50
		}

		// Pullback Hysteresis Latch
		atrSafe := math.Max(stock.ATR14, stock.LTP*0.0025)
		if stock.LTP > stock.ExchangeVWAP+1.0*atrSafe {
			stock.WasExtended = true
		} else if stock.WasExtended && stock.LTP <= stock.ExchangeVWAP+0.50*atrSafe {
			stock.PullbackCount++
			stock.WasExtended = false
		}
	}
}

func (a *Actor) computeAndBroadcastSnapshot() {
	if a.wsHub == nil || a.wsHub.ClientCount() == 0 {
		return // Zero connected clients: zero JSON allocations
	}

	// 1. Calculate Market Breadth (% > VWAP)
	var aboveVWAPCount int
	totalActive := len(a.activeIndices)
	for _, idx := range a.activeIndices {
		s := &a.stocks[idx]
		if s.LTP > s.ExchangeVWAP && s.ExchangeVWAP > 0 {
			aboveVWAPCount++
		}
	}
	var breadthRatio float64 = 0.5
	if totalActive > 0 {
		breadthRatio = float64(aboveVWAPCount) / float64(totalActive)
	}

	// 2. Market Pulse & Regime Context
	var niftyLTP, niftyChg, bankLTP, bankChg float64
	var niftyStock *StockMetrics
	if a.hasNifty {
		niftyStock = &a.stocks[a.niftyIdx]
		niftyLTP = niftyStock.LTP
		if niftyStock.PrevClose > 0 {
			niftyChg = ((niftyLTP - niftyStock.PrevClose) / niftyStock.PrevClose) * 100.0
		}
	}
	if a.hasBankNifty {
		b := &a.stocks[a.bankNiftyIdx]
		bankLTP = b.LTP
		if b.PrevClose > 0 {
			bankChg = ((bankLTP - b.PrevClose) / b.PrevClose) * 100.0
		}
	}

	regime := "SIDEWAYS_CHOP"
	if breadthRatio >= 0.60 && niftyChg > 0.30 {
		regime = "BULLISH_EXPANSION"
	} else if breadthRatio <= 0.40 && niftyChg < -0.30 {
		regime = "BEARISH_EXPANSION"
	}

	// 3. Compute Sector Performance & Identify Leaders
	type secPerf struct {
		id       uint8
		chg      float64
		breadth  float64
		totCount int
		abvCount int
	}
	secStats := make(map[uint8]*secPerf)
	for id := range scanner.SectorRegistry {
		secStats[id] = &secPerf{id: id}
	}

	for _, idx := range a.activeIndices {
		s := &a.stocks[idx]
		if p, ok := secStats[s.SectorID]; ok {
			p.totCount++
			if s.LTP > s.ExchangeVWAP && s.ExchangeVWAP > 0 {
				p.abvCount++
			}
		}
	}

	// Build deterministic list of sectors (SectorAuto=2 through SectorInfra=12)
	sectorDTOs := make([]SectorRankDTO, 0, 11)
	for id := uint8(2); id <= 12; id++ {
		p := secStats[id]
		if p == nil {
			continue
		}
		if p.totCount > 0 {
			p.breadth = (float64(p.abvCount) / float64(p.totCount)) * 100.0
		}
		var secChg float64
		if sIdx, ok := a.sectorIndices[id]; ok {
			st := &a.stocks[sIdx]
			if st.PrevClose > 0 && st.LTP > 0 {
				secChg = ((st.LTP - st.PrevClose) / st.PrevClose) * 100.0
			}
		} else if id == scanner.SectorBank && a.hasBankNifty {
			b := &a.stocks[a.bankNiftyIdx]
			if b.PrevClose > 0 && b.LTP > 0 {
				secChg = ((b.LTP - b.PrevClose) / b.PrevClose) * 100.0
			}
		}
		p.chg = secChg

		sectorDTOs = append(sectorDTOs, SectorRankDTO{
			SectorID:         id,
			SectorName:       scanner.GetSectorName(id),
			ChangePercent:    secChg,
			BreadthAboveVWAP: p.breadth,
		})
	}

	// Deterministic Sort: Primary by ChangePercent DESC, Secondary tie-breaker by SectorID ASC
	slices.SortFunc(sectorDTOs, func(a, b SectorRankDTO) int {
		if a.ChangePercent > b.ChangePercent {
			return -1
		} else if a.ChangePercent < b.ChangePercent {
			return 1
		}
		// Deterministic tie-breaker prevents UI pills from shuffling when change is 0.00%
		if a.SectorID < b.SectorID {
			return -1
		} else if a.SectorID > b.SectorID {
			return 1
		}
		return 0
	})

	leaderMap := make(map[uint8]bool, 2)
	laggingMap := make(map[uint8]bool, 2)

	// Only assign leaders/laggards if there is real price movement
	hasMovement := false
	for _, s := range sectorDTOs {
		if s.ChangePercent != 0.0 {
			hasMovement = true
			break
		}
	}

	if hasMovement {
		for i := range sectorDTOs {
			if i < 2 && sectorDTOs[i].ChangePercent > 0 {
				sectorDTOs[i].IsLeader = true
				leaderMap[sectorDTOs[i].SectorID] = true
			}
			if i >= len(sectorDTOs)-2 && len(sectorDTOs) >= 4 && sectorDTOs[i].ChangePercent < 0 {
				sectorDTOs[i].IsLagging = true
				laggingMap[sectorDTOs[i].SectorID] = true
			}
		}
	}

	// 4. Quant Score Calculation for all active equities
	for _, idx := range a.activeIndices {
		s := &a.stocks[idx]
		var secStock *StockMetrics
		if sIdx, ok := a.sectorIndices[s.SectorID]; ok {
			secStock = &a.stocks[sIdx]
		} else if s.SectorID == scanner.SectorBank && a.hasBankNifty {
			secStock = &a.stocks[a.bankNiftyIdx]
		}
		isLeader := leaderMap[s.SectorID]
		isLagging := laggingMap[s.SectorID]

		CalculateBullishScore(s, niftyStock, secStock, isLeader, breadthRatio)
		CalculateBearishScore(s, niftyStock, secStock, isLagging, breadthRatio)
	}

	// 5. Quicksort Active Indices by BullishScore (~2 microseconds)
	bullishSorted := make([]uint16, len(a.activeIndices))
	copy(bullishSorted, a.activeIndices)
	slices.SortFunc(bullishSorted, func(i, j uint16) int {
		s1 := a.stocks[i].BullishScore
		s2 := a.stocks[j].BullishScore
		if s1 > s2 {
			return -1
		} else if s1 < s2 {
			return 1
		}
		return 0
	})

	// Bearish Sort for Top 5 Bearish Outliers
	bearishSorted := make([]uint16, len(a.activeIndices))
	copy(bearishSorted, a.activeIndices)
	slices.SortFunc(bearishSorted, func(i, j uint16) int {
		s1 := a.stocks[i].BearishScore
		s2 := a.stocks[j].BearishScore
		if s1 > s2 {
			return -1
		} else if s1 < s2 {
			return 1
		}
		return 0
	})

	// Build Top 5 Bullish Outliers
	topBullish := make([]StockRankDTO, 0, 5)
	for rk, idx := range bullishSorted {
		if rk >= 5 {
			break
		}
		dto := a.toStockDTO(&a.stocks[idx], rk+1)
		topBullish = append(topBullish, dto)
	}

	// Build Top 5 Bearish Outliers
	topBearish := make([]StockRankDTO, 0, 5)
	for rk, idx := range bearishSorted {
		if rk >= 5 {
			break
		}
		dto := a.toStockDTO(&a.stocks[idx], rk+1)
		topBearish = append(topBearish, dto)
	}

	// Build Full 213 Watchlist Table
	allStocks := make([]StockRankDTO, len(bullishSorted))
	for rk, idx := range bullishSorted {
		allStocks[rk] = a.toStockDTO(&a.stocks[idx], rk+1)
	}

	// Historical API Status
	histHealthy, histErr := a.histClient.IsHealthy()

	snapshot := RankedSnapshot{
		Timestamp:        time.Now().Unix(),
		HistoricalActive: histHealthy,
		HistoricalError:  histErr,
		MarketPulse: MarketPulseDTO{
			MarketState:            "OPEN",
			NiftyLTP:               niftyLTP,
			NiftyChangePercent:     niftyChg,
			BankNiftyLTP:           bankLTP,
			BankNiftyChangePercent: bankChg,
			BreadthAboveVWAP:       math.Round(breadthRatio * 1000.0) / 10.0,
			MarketRegime:           regime,
		},
		Sectors:    sectorDTOs,
		TopBullish: topBullish,
		TopBearish: topBearish,
		AllStocks:  allStocks,
	}

	a.wsHub.Broadcast(snapshot)
}

func (a *Actor) toStockDTO(s *StockMetrics, rank int) StockRankDTO {
	var chgPercent float64
	if s.PrevClose > 0 {
		chgPercent = ((s.LTP - s.PrevClose) / s.PrevClose) * 100.0
	}

	return StockRankDTO{
		Rank:              rank,
		Token:             s.InstrumentToken,
		Symbol:            s.SymbolString(),
		Sector:            scanner.GetSectorName(s.SectorID),
		SectorID:          s.SectorID,
		LTP:               s.LTP,
		ChangePercent:     math.Round(chgPercent*100.0) / 100.0,
		VWAP:              s.ExchangeVWAP,
		BullishScore:      math.Round(s.BullishScore*10.0) / 10.0,
		BearishScore:      math.Round(s.BearishScore*10.0) / 10.0,
		RVOLSession:       math.Round(s.RVOLSession*10.0) / 10.0,
		RVOLPace5m:        math.Round(s.RVOLPace5m*10.0) / 10.0,
		CompositeRS:       math.Round(s.CompositeRS*100.0) / 100.0,
		BuyerRatio:        float32(math.Round(float64(s.SmoothedBuyerRatio)*10000.0)) / 10000.0,
		AggressorBuyRatio: float32(math.Round(float64(s.AggressorBuyRatio)*10000.0)) / 10000.0,
		EV:                math.Round(s.EV*100.0) / 100.0,
		PWin:              math.Round(s.PWin*100.0) / 100.0,
		StopProxy:         s.StopProxy,
		TargetProxy:       s.TargetProxy,
		Reasons:           uint16(s.Reasons),
		PullbackCount:     s.PullbackCount,
	}
}

func (a *Actor) sqliteBarWriter() {
	if a.db == nil {
		return
	}

	insertSQL := `
		INSERT INTO historical_5m_bars (instrument_token, bar_time, open, high, low, close, volume)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instrument_token, bar_time) DO UPDATE SET
			high = max(high, excluded.high),
			low = min(low, excluded.low),
			close = excluded.close,
			volume = excluded.volume;
	`
	stmt, err := a.db.Prepare(insertSQL)
	if err != nil {
		slog.Error("Failed to prepare bar flush statement", "error", err)
		return
	}
	defer stmt.Close()

	for {
		select {
		case <-a.ctx.Done():
			return
		case item := <-a.barFlushChan:
			barTime := time.Unix(item.Bar.Timestamp, 0).In(istZone).Format("2006-01-02 15:04:05")
			_, _ = stmt.Exec(
				item.Token,
				barTime,
				item.Bar.Open,
				item.Bar.High,
				item.Bar.Low,
				item.Bar.Close,
				item.Bar.Volume,
			)
		}
	}
}

// GetStockDetail returns the full quantitative and order book state for a specific stock
func (a *Actor) GetStockDetail(symbol string) (*StockDetailDTO, bool) {
	upperSym := strings.ToUpper(strings.TrimSpace(symbol))

	targetIdx, found := a.symbolMap[upperSym]
	if !found {
		// Also check index tokens (Nifty 50 / Bank Nifty)
		if a.hasNifty && a.stocks[a.niftyIdx].SymbolString() == upperSym {
			targetIdx = a.niftyIdx
			found = true
		} else if a.hasBankNifty && a.stocks[a.bankNiftyIdx].SymbolString() == upperSym {
			targetIdx = a.bankNiftyIdx
			found = true
		}
	}

	// Dynamic fallback: check SQLite scanned_stocks table in case stock was added dynamically
	if !found && a.db != nil {
		var token uint32
		var tradingsymbol string
		var lotSize int
		var tickSize, lastPrice float64
		err := a.db.QueryRow(`
			SELECT instrument_token, tradingsymbol, lot_size, tick_size, last_price
			FROM scanned_stocks
			WHERE tradingsymbol = ?
			ORDER BY scan_date DESC LIMIT 1
		`, upperSym).Scan(&token, &tradingsymbol, &lotSize, &tickSize, &lastPrice)
		if err == nil && token > 0 {
			secID, _, _ := scanner.ResolveSector(a.db, tradingsymbol)
			if tickSize <= 0 {
				tickSize = 0.05
			}
			targetIdx = a.addStock(token, tradingsymbol, secID, float32(tickSize))
			a.stocks[targetIdx].LotSize = uint32(lotSize)
			a.stocks[targetIdx].PrevClose = lastPrice
			a.stocks[targetIdx].IsTradeable = true
			found = true
		}
	}

	if !found {
		return nil, false
	}

	s := &a.stocks[targetIdx]

	var chg, chgPct float64
	if s.PrevClose > 0 && s.LTP > 0 {
		chg = s.LTP - s.PrevClose
		chgPct = (chg / s.PrevClose) * 100.0
	}

	var vwapDist, vwapDistPct float64
	if s.ExchangeVWAP > 0 && s.LTP > 0 {
		vwapDist = s.LTP - s.ExchangeVWAP
		vwapDistPct = (vwapDist / s.ExchangeVWAP) * 100.0
	}

	orh, orl, hasOR := s.OpeningRange15m()

	// Compute CLV
	var clv float64
	if s.High > s.Low {
		clv = ((s.LTP - s.Low) - (s.High - s.LTP)) / (s.High - s.Low)
	}

	// Format session bars
	bars := make([]SessionBarDTO, 0, int(s.CurrentBarIdx)+1)
	for i := 0; i <= int(s.CurrentBarIdx) && i < 75; i++ {
		b := s.SessionBars[i]
		if b.Open > 0 || b.Close > 0 || b.Volume > 0 {
			barTimeStr := time.Unix(b.Timestamp, 0).In(istZone).Format("15:04")
			bars = append(bars, SessionBarDTO{
				Timestamp: b.Timestamp,
				Time:      barTimeStr,
				Open:      b.Open,
				High:      b.High,
				Low:       b.Low,
				Close:     b.Close,
				Volume:    b.Volume,
			})
		}
	}

	// Format 5-Level Depth
	bids := make([]DepthLevelDTO, 5)
	asks := make([]DepthLevelDTO, 5)
	var totalBidQty, totalAskQty uint64
	var bidTop2, bidRest3, askTop2, askRest3 uint64

	for lvl := 0; lvl < 5; lvl++ {
		b := s.BidLevels[lvl]
		bids[lvl] = DepthLevelDTO{
			Price:  math.Round(float64(b.Price)*100.0) / 100.0,
			Qty:    b.Qty,
			Orders: b.Orders,
		}
		totalBidQty += uint64(b.Qty)
		if lvl < 2 {
			bidTop2 += uint64(b.Qty)
		} else {
			bidRest3 += uint64(b.Qty)
		}

		aL := s.AskLevels[lvl]
		asks[lvl] = DepthLevelDTO{
			Price:  math.Round(float64(aL.Price)*100.0) / 100.0,
			Qty:    aL.Qty,
			Orders: aL.Orders,
		}
		totalAskQty += uint64(aL.Qty)
		if lvl < 2 {
			askTop2 += uint64(aL.Qty)
		} else {
			askRest3 += uint64(aL.Qty)
		}
	}

	var antiSpoofRatio float64 = 0.50
	wBid := 0.6*float64(bidTop2) + 0.4*float64(bidRest3)
	wAsk := 0.6*float64(askTop2) + 0.4*float64(askRest3)
	if tot := wBid + wAsk; tot > 0 {
		antiSpoofRatio = wBid / tot
	}

	// Decode reasons
	reasonsList := make([]string, 0)
	if s.Reasons&ReasonVWAPSweetSpot != 0 {
		reasonsList = append(reasonsList, "VWAP Sweet Spot")
	}
	if s.Reasons&ReasonRVOLSurge != 0 {
		reasonsList = append(reasonsList, "High RVOL Volume Surge")
	}
	if s.Reasons&ReasonSectorLeader != 0 {
		reasonsList = append(reasonsList, "Leading Sector Tailwinds")
	}
	if s.Reasons&ReasonOrderBookWall != 0 {
		reasonsList = append(reasonsList, "Anti-Spoof Order Depth Wall")
	}
	if s.Reasons&ReasonORBBreakout != 0 {
		reasonsList = append(reasonsList, "15m Opening Range Breakout")
	}
	if s.Reasons&ReasonHighEV != 0 {
		reasonsList = append(reasonsList, "High Expected Value (EV ≥ +0.40R)")
	}
	if s.Reasons&ReasonBearishBreakdown != 0 {
		reasonsList = append(reasonsList, "Bearish Breakdown Below VWAP")
	}
	if s.Reasons&ReasonSectorLagging != 0 {
		reasonsList = append(reasonsList, "Lagging Sector Drag")
	}
	if s.Reasons&PenaltyWideSpread != 0 {
		reasonsList = append(reasonsList, "Penalty: Wide Bid-Ask Spread (>0.10%)")
	}
	if s.Reasons&PenaltyBelowPrevClose != 0 {
		reasonsList = append(reasonsList, "Penalty: Trading Below Previous Close")
	}
	if s.Reasons&PenaltyOverextended != 0 {
		reasonsList = append(reasonsList, "Penalty: Overextended > 1.5 ATR From VWAP")
	}
	if s.Reasons&PenaltyLowVolume != 0 {
		reasonsList = append(reasonsList, "Penalty: Dry Volume (<1.0x RVOL)")
	}

	dto := &StockDetailDTO{
		Symbol:            s.SymbolString(),
		Token:             s.InstrumentToken,
		Sector:            scanner.GetSectorName(s.SectorID),
		SectorID:          s.SectorID,
		LotSize:           s.LotSize,
		TickSize:          s.TickSize,
		IsTradeable:       s.IsTradeable,
		LTP:               s.LTP,
		Open:              s.Open,
		High:              s.High,
		Low:               s.Low,
		PrevClose:         s.PrevClose,
		Change:            chg,
		ChangePercent:     chgPct,
		VWAP:              s.ExchangeVWAP,
		VWAPDistance:      vwapDist,
		VWAPDistancePct:   vwapDistPct,
		ORH:               orh,
		ORL:               orl,
		HasOR15m:          hasOR,
		CLV:               clv,
		ATR14:             s.ATR14,
		ADX14:             s.ADX14,
		Volume:            s.Volume,
		RVOLSession:       s.RVOLSession,
		RVOLPace5m:        s.RVOLPace5m,
		BullishScore:      s.BullishScore,
		BearishScore:      s.BearishScore,
		CompositeRS:       s.CompositeRS,
		BuyerRatio:        s.SmoothedBuyerRatio,
		AggressorBuyRatio: s.AggressorBuyRatio,
		EV:                s.EV,
		PWin:              s.PWin,
		StopProxy:         s.StopProxy,
		TargetProxy:       s.TargetProxy,
		Reasons:           reasonsList,
		Bids:              bids,
		Asks:              asks,
		TotalBidQty:       totalBidQty,
		TotalAskQty:       totalAskQty,
		AntiSpoofRatio:    antiSpoofRatio,
		Bars:              bars,
		LastUpdated:       s.LastUpdatedUnix,
	}

	return dto, true
}
