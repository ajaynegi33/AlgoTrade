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

// PositionTickListener is the lock-free O(1) hot-path interface for active position workers
type PositionTickListener interface {
	OnTickHotPath(s *StockMetrics, inMacroShock bool)
}

// ExecutionHook is invoked once per second inside the Actor loop with zero lock contention
type ExecutionHook interface {
	OnSecondTick(
		stocks *[maxTrackedStocks]StockMetrics,
		activeIndices []uint16,
		bullishSorted []uint16,
		leaderMap map[uint8]bool,
		pulse MarketPulseDTO,
		inMacroShock bool,
	) *ExecutionStateDTO
}

type detailReq struct {
	symbol string
	reply  chan detailResp
}

type detailResp struct {
	dto   *StockDetailDTO
	found bool
}

type registerWorkerReq struct {
	token  uint32
	symbol string
	worker PositionTickListener
	reply  chan uint16
}

type seedTarget struct {
	token     uint32
	prevClose float64
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
	slotBaselines  [maxTrackedStocks][75]kite.SlotBaseline
	workers        [maxTrackedStocks]PositionTickListener
	tokenMap       map[uint32]uint16
	symbolMap      map[string]uint16
	activeIndices  []uint16
	sectorIndices  map[uint8]uint16
	niftyIdx       uint16
	bankNiftyIdx   uint16
	hasNifty       bool
	hasBankNifty   bool

	prevBreadth    float64
	breadthHist    [6]float64 // Rolling 5-second breadth ring for shock velocity
	breadthHistIdx uint8
	macroShockExp  int64 // Unix epoch when FLASH_SHOCK cooldown expires
	inMacroShock   atomic.Bool

	execHook          ExecutionHook
	detailReqChan     chan detailReq
	workerReqChan     chan registerWorkerReq
	barFlushChan      chan BarFlushItem
	baselineSeedChan  chan *kite.StockBaselineProfile
	seedTargetReqChan chan chan []seedTarget
	universeSyncChan  chan []scanner.ScannedStock
	seedingInProgress atomic.Bool
	today0915Epoch    int64
	ticksProcessed    atomic.Uint64
	isRunning         atomic.Bool

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
		db:                db,
		tickerClient:      tickerClient,
		histClient:        histClient,
		wsHub:             wsHub,
		scannerService:    scannerService,
		tokenMap:          make(map[uint32]uint16, maxTrackedStocks),
		symbolMap:         make(map[string]uint16, maxTrackedStocks),
		activeIndices:     make([]uint16, 0, maxTrackedStocks),
		sectorIndices:     make(map[uint8]uint16, 16),
		detailReqChan:     make(chan detailReq, 64),
		workerReqChan:     make(chan registerWorkerReq, 64),
		barFlushChan:      make(chan BarFlushItem, 4096),
		baselineSeedChan:  make(chan *kite.StockBaselineProfile, 512),
		seedTargetReqChan: make(chan chan []seedTarget, 8),
		universeSyncChan:  make(chan []scanner.ScannedStock, 4),
		ctx:               ctx,
		cancel:            cancel,
	}
}

// SetExecutionHook wires the Phase 2 Execution Manager into the Actor's 1s loop
func (a *Actor) SetExecutionHook(hook ExecutionHook) {
	a.execHook = hook
}

// BindPositionWorker registers or unregisters a PositionWorker on the O(1) hot path (thread-safe via Actor loop)
func (a *Actor) BindPositionWorker(token uint32, symbol string, worker PositionTickListener) uint16 {
	if !a.isRunning.Load() {
		idx, ok := a.tokenMap[token]
		if !ok && worker != nil {
			idx = a.addStock(token, symbol, scanner.SectorNifty50, 0.05)
		}
		if int(idx) < maxTrackedStocks {
			a.workers[idx] = worker
		}
		return idx
	}
	reply := make(chan uint16, 1)
	select {
	case a.workerReqChan <- registerWorkerReq{token: token, symbol: symbol, worker: worker, reply: reply}:
		select {
		case idx := <-reply:
			return idx
		case <-time.After(2 * time.Second):
			return 0
		}
	case <-a.ctx.Done():
		return 0
	}
}

// IsInMacroShock returns true if Nifty 50 is currently in a FLASH_SHOCK state
func (a *Actor) IsInMacroShock() bool {
	return a.inMacroShock.Load()
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

		// 4. Pre-seed 20-Day Baselines & Today's Intraday Bars in background
		a.TriggerHistoricalReseed()

		slog.Info("Single-Writer Engine Actor started successfully",
			"tracked_stocks", len(a.activeIndices),
			"token_map_size", len(a.tokenMap),
		)
	}
	return nil
}

// TriggerHistoricalReseed starts or re-runs background 20-day historical baseline & intraday bar seeding
func (a *Actor) TriggerHistoricalReseed() {
	if a.histClient == nil {
		return
	}
	if !a.seedingInProgress.CompareAndSwap(false, true) {
		return
	}
	go a.seedBaselinesAsync()
}

// OnKiteAuthenticated is invoked when Zerodha Kite OAuth succeeds or restores a valid session.
// It syncs any newly scanned universe stocks and triggers historical baseline + intraday bar seeding.
func (a *Actor) OnKiteAuthenticated() {
	if a.histClient != nil {
		a.histClient.ClearNoSessionError()
	}
	go func() {
		if a.scannerService != nil {
			todayStr := time.Now().In(istZone).Format("2006-01-02")
			universe, err := a.scannerService.GetUniverse(todayStr)
			if err != nil || universe == nil || len(universe.Stocks) == 0 || universe.ScanDate != todayStr {
				if scanned, scanErr := a.scannerService.RunScan(false); scanErr == nil && scanned != nil {
					universe = scanned
				}
			}
			if universe != nil && len(universe.Stocks) > 0 && a.isRunning.Load() {
				select {
				case a.universeSyncChan <- universe.Stocks:
				case <-a.ctx.Done():
					return
				}
			}
		}
		a.TriggerHistoricalReseed()
	}()
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

// GetStockPtrByIndex returns a pointer to the pre-allocated StockMetrics slot for crash-recovery rehydration
func (a *Actor) GetStockPtrByIndex(idx uint16) *StockMetrics {
	if int(idx) >= len(a.stocks) {
		return nil
	}
	return &a.stocks[idx]
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
				if st.MISLeverage > 0 {
					a.stocks[idx].MISLeverage = float32(st.MISLeverage)
				} else {
					a.stocks[idx].MISLeverage = 5.0
				}
				a.stocks[idx].IsTradeable = true
			}
		}
	}

	// 4. Subscribe all tokens on Kite Ticker
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
	s.FastAggressorEMA = 0.50
	if s.MISLeverage <= 0 {
		s.MISLeverage = 5.0
	}

	// Initialize bar 0
	s.SessionBars[0] = OHLCVBar{Timestamp: a.today0915Epoch}

	// Only add tradeable equities to activeIndices for ranking (skip pure index tokens)
	if token != nifty50Token && token != bankNiftyToken && !strings.HasPrefix(symbol, "NIFTY ") {
		a.activeIndices = append(a.activeIndices, nextIdx)
	}

	return nextIdx
}

func (a *Actor) seedBaselinesAsync() {
	defer a.seedingInProgress.Store(false)
	if a.histClient == nil {
		return
	}

	var targets []seedTarget
	if a.isRunning.Load() {
		reply := make(chan []seedTarget, 1)
		select {
		case a.seedTargetReqChan <- reply:
			select {
			case targets = <-reply:
			case <-a.ctx.Done():
				return
			}
		case <-a.ctx.Done():
			return
		}
	} else {
		for _, idx := range a.activeIndices {
			s := &a.stocks[idx]
			if !s.IsWarm {
				targets = append(targets, seedTarget{token: s.InstrumentToken, prevClose: s.PrevClose})
			}
		}
	}

	for _, t := range targets {
		select {
		case <-a.ctx.Done():
			return
		default:
		}
		profile, err := a.histClient.SeedStockBaselines(t.token, t.prevClose)
		if err == nil && profile != nil {
			if a.isRunning.Load() {
				select {
				case a.baselineSeedChan <- profile:
				case <-a.ctx.Done():
					return
				}
			} else {
				a.applyBaselineProfile(profile)
			}
		}
	}
	slog.Info("Completed 20-day baseline seeding for active equities", "seeded_count", len(targets))
}

func (a *Actor) applyBaselineProfile(profile *kite.StockBaselineProfile) {
	if profile == nil {
		return
	}
	idx, exists := a.tokenMap[profile.Token]
	if !exists || int(idx) >= maxTrackedStocks {
		return
	}
	s := &a.stocks[idx]
	a.slotBaselines[idx] = profile.Slots

	// Backfill today's completed 5m intraday bars (09:15 AM onwards, including 15m ORH/ORL slots 0,1,2)
	if len(profile.TodayBars) > 0 {
		var cumVol uint64
		var weightedPriceVol float64
		maxSlot := int(s.CurrentBarIdx)
		for _, tb := range profile.TodayBars {
			if tb.SlotIdx < 0 || tb.SlotIdx >= 75 {
				continue
			}
			existing := &s.SessionBars[tb.SlotIdx]
			if existing.Open == 0 || tb.Volume > existing.Volume {
				*existing = OHLCVBar{
					Open:      tb.Open,
					High:      tb.High,
					Low:       tb.Low,
					Close:     tb.Close,
					Volume:    tb.Volume,
					Timestamp: a.today0915Epoch + int64(tb.SlotIdx)*300,
				}
			}
			if tb.SlotIdx > maxSlot {
				maxSlot = tb.SlotIdx
			}
			if s.Open == 0 && tb.Open > 0 {
				s.Open = tb.Open
			}
			if tb.High > s.High {
				s.High = tb.High
			}
			if (s.Low == 0 || tb.Low < s.Low) && tb.Low > 0 {
				s.Low = tb.Low
			}
			cumVol += tb.Volume
			typPx := (tb.High + tb.Low + tb.Close) / 3.0
			weightedPriceVol += typPx * float64(tb.Volume)
		}
		if maxSlot > int(s.CurrentBarIdx) && maxSlot < 75 {
			s.CurrentBarIdx = uint8(maxSlot)
		}
		// If no live tick has arrived yet, seed LTP, Volume, and ExchangeVWAP from today's bars
		if s.LastUpdatedUnix == 0 && len(profile.TodayBars) > 0 {
			lastBar := profile.TodayBars[len(profile.TodayBars)-1]
			if s.LTP == 0 && lastBar.Close > 0 {
				s.LTP = lastBar.Close
				s.RollingPeak = lastBar.Close
			}
			if s.Volume == 0 && cumVol > 0 {
				s.Volume = cumVol
				if cumVol >= lastBar.Volume {
					s.BarStartVolume = cumVol - lastBar.Volume
				}
			}
			if s.ExchangeVWAP == 0 && cumVol > 0 {
				s.ExchangeVWAP = weightedPriceVol / float64(cumVol)
			}
		}
	}

	curSlot := s.CurrentBarIdx
	if curSlot >= 75 {
		curSlot = 74
	}
	s.ATR14 = profile.ATR14
	s.BaselineSlotVolume = profile.Slots[curSlot].MedianVolume
	s.BaselineCumVolume = profile.Slots[curSlot].MedianCumVol
	// Only mark IsWarm = true when seeded with real historical data (not synthetic fallback)
	s.IsWarm = !profile.IsFallback
}

func (a *Actor) runLoop() {
	var rawFeed <-chan []byte
	if a.tickerClient != nil {
		rawFeed = a.tickerClient.SubscribeRaw()
	}
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

		case profile := <-a.baselineSeedChan:
			a.applyBaselineProfile(profile)

		case reply := <-a.seedTargetReqChan:
			targets := make([]seedTarget, 0, len(a.activeIndices))
			for _, idx := range a.activeIndices {
				s := &a.stocks[idx]
				// Re-seed if still on synthetic fallback (!s.IsWarm) or if today's 15m ORB bars (slots 0..2) are missing
				if !s.IsWarm || s.SessionBars[0].Open == 0 {
					targets = append(targets, seedTarget{
						token:     s.InstrumentToken,
						prevClose: s.PrevClose,
					})
				}
			}
			reply <- targets

		case scannedStocks := <-a.universeSyncChan:
			var newTokens []uint32
			for _, st := range scannedStocks {
				tok := uint32(st.InstrumentToken)
				_, existed := a.tokenMap[tok]
				secID, _, _ := scanner.ResolveSector(a.db, st.Tradingsymbol)
				tickSize := float32(st.TickSize)
				if tickSize <= 0 {
					tickSize = 0.05
				}
				idx := a.addStock(tok, st.Tradingsymbol, secID, tickSize)
				a.stocks[idx].LotSize = uint32(st.LotSize)
				if a.stocks[idx].PrevClose <= 0 && st.LastPrice > 0 {
					a.stocks[idx].PrevClose = st.LastPrice
				}
				if st.MISLeverage > 0 {
					a.stocks[idx].MISLeverage = float32(st.MISLeverage)
				} else if a.stocks[idx].MISLeverage <= 0 {
					a.stocks[idx].MISLeverage = 5.0
				}
				a.stocks[idx].IsTradeable = true
				if !existed {
					newTokens = append(newTokens, tok)
				}
			}
			if len(newTokens) > 0 && a.tickerClient != nil {
				_ = a.tickerClient.Subscribe(newTokens)
			}

		case <-periodic1s.C:
			a.computeAndBroadcastSnapshot()

		case req := <-a.detailReqChan:
			dto, found := a.getStockDetailInternal(req.symbol)
			req.reply <- detailResp{dto: dto, found: found}

		case wReq := <-a.workerReqChan:
			idx, exists := a.tokenMap[wReq.token]
			if !exists && wReq.worker != nil {
				secID, _, _ := scanner.ResolveSector(a.db, wReq.symbol)
				idx = a.addStock(wReq.token, wReq.symbol, secID, 0.05)
				a.stocks[idx].IsTradeable = true
				if a.tickerClient != nil {
					_ = a.tickerClient.Subscribe([]uint32{wReq.token})
				}
			}
			if int(idx) < maxTrackedStocks {
				a.workers[idx] = wReq.worker
			}
			wReq.reply <- idx
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

	// Ensure today0915Epoch rolls over cleanly if server stays up across calendar days
	if tick.ExchangeTime > a.today0915Epoch+64800 {
		tIST := time.Unix(tick.ExchangeTime, 0).In(istZone)
		y, m, d := tIST.Date()
		newEpoch := time.Date(y, m, d, 9, 15, 0, 0, istZone).Unix()
		if newEpoch > a.today0915Epoch {
			a.today0915Epoch = newEpoch
			// Reset all tracked stocks' intraday session state for the new trading day
			for _, sIdx := range a.activeIndices {
				st := &a.stocks[sIdx]
				st.LastUpdatedUnix = 0
				st.CurrentBarIdx = 0
				st.PullbackCount = 0
				st.WasExtended = false
				st.BarStartVolume = 0
				st.BarBuyVolume = 0
				st.BarSellVolume = 0
				st.SessionBars = [75]OHLCVBar{}
			}
		}
	}

	// First-tick baseline seeding & bar slot initialization
	if stock.LastUpdatedUnix == 0 {
		stock.Volume = tick.Volume
		stock.BarStartVolume = tick.Volume
		stock.Bid1Price = tick.Bid1Price
		stock.Ask1Price = tick.Ask1Price
		stock.BidLevels = tick.BidLevels
		stock.AskLevels = tick.AskLevels
		stock.LTP = tick.LTP
		stock.RollingPeak = tick.LTP
		stock.Open = tick.Open
		stock.High = tick.High
		stock.Low = tick.Low
		if tick.PrevClose > 0 {
			stock.PrevClose = tick.PrevClose
		}
		if tick.IsIndex {
			if tick.Open > 0 {
				stock.ExchangeVWAP = tick.Open
			} else {
				stock.ExchangeVWAP = tick.LTP
			}
		} else {
			stock.ExchangeVWAP = tick.VWAP
		}
		stock.LastUpdatedUnix = tick.ExchangeTime

		barIdx := (tick.ExchangeTime - a.today0915Epoch) / 300
		if barIdx < 0 {
			barIdx = 0
		} else if barIdx >= 75 {
			barIdx = 74
		}
		stock.CurrentBarIdx = uint8(barIdx)
		stock.SessionBars[barIdx] = OHLCVBar{
			Open:      tick.LTP,
			High:      tick.LTP,
			Low:       tick.LTP,
			Close:     tick.LTP,
			Volume:    0,
			Timestamp: a.today0915Epoch + barIdx*300,
		}
		if stock.IsWarm {
			stock.BaselineSlotVolume = a.slotBaselines[idx][barIdx].MedianVolume
			stock.BaselineCumVolume = a.slotBaselines[idx][barIdx].MedianCumVol
		}
		if w := a.workers[idx]; w != nil {
			w.OnTickHotPath(stock, a.inMacroShock.Load())
		}
		return
	}

	// Update continuous time-decaying RollingPeak (tau = 20s) before overwriting LastUpdatedUnix
	dt := float64(tick.ExchangeTime - stock.LastUpdatedUnix)
	if dt < 0 {
		dt = 0
	}
	if tick.LTP >= stock.RollingPeak || stock.RollingPeak <= 0 {
		stock.RollingPeak = tick.LTP
	} else if dt > 0 {
		decay := math.Exp(-dt / 20.0)
		stock.RollingPeak = tick.LTP + (stock.RollingPeak-tick.LTP)*decay
	}

	// Prevent out-of-order ticks from underflowing uint64
	var tickDeltaVol uint64
	if tick.Volume > stock.Volume {
		tickDeltaVol = tick.Volume - stock.Volume
	}

	// Check 5-minute bar rollover (exchTime / 300)
	barIdx := (tick.ExchangeTime - a.today0915Epoch) / 300
	if barIdx > int64(stock.CurrentBarIdx) && barIdx < 75 {
		// Finalize old bar
		oldIdx := stock.CurrentBarIdx
		stock.SessionBars[oldIdx].Close = stock.LTP
		if stock.Volume >= stock.BarStartVolume {
			stock.SessionBars[oldIdx].Volume = stock.Volume - stock.BarStartVolume
		}

		// Queue old bar for background SQLite flush
		select {
		case a.barFlushChan <- BarFlushItem{Token: stock.InstrumentToken, Bar: stock.SessionBars[oldIdx]}:
		default:
		}

		// Advance to new bar & update time-of-day slot baselines
		stock.CurrentBarIdx = uint8(barIdx)
		if stock.IsWarm {
			stock.BaselineSlotVolume = a.slotBaselines[idx][barIdx].MedianVolume
			stock.BaselineCumVolume = a.slotBaselines[idx][barIdx].MedianCumVol
		}
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

	// Update current 5m bar High/Low/Close/Volume
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
	if tick.Volume >= stock.BarStartVolume {
		currBar.Volume = tick.Volume - stock.BarStartVolume
	}

	// Save previous quotes BEFORE overwriting scalar fields so Lee-Ready Quote & Tick rules are exact
	prevLTP := stock.LTP
	prevBid1 := stock.Bid1Price
	prevAsk1 := stock.Ask1Price

	// Update stock scalar quotes
	stock.LTP = tick.LTP
	if tick.IsIndex {
		// Indices do not transmit exchange VWAP; maintain synthetic intraday anchor so Nifty > VWAP works
		if stock.ExchangeVWAP <= 0 {
			if tick.Open > 0 {
				stock.ExchangeVWAP = tick.Open
			} else {
				stock.ExchangeVWAP = tick.LTP
			}
		} else {
			stock.ExchangeVWAP = 0.995*stock.ExchangeVWAP + 0.005*tick.LTP
		}
	} else if tick.VWAP > 0 {
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

		// Dual-alpha EMA Smoothing & Lee-Ready Aggressor Flow using prevAsk1/prevBid1/prevLTP
		var alpha float32 = 0.03
		if tickDeltaVol > 0 {
			alpha = 0.20
			var isBuyTick bool
			if tick.LTP >= float64(prevAsk1) && prevAsk1 > 0 {
				isBuyTick = true
			} else if tick.LTP <= float64(prevBid1) && prevBid1 > 0 {
				isBuyTick = false
			} else {
				midpoint := float64(prevBid1+prevAsk1) / 2.0
				if prevBid1 > 0 && prevAsk1 > 0 && tick.LTP != midpoint {
					isBuyTick = tick.LTP > midpoint
				} else {
					isBuyTick = tick.LTP >= prevLTP
				}
			}

			if isBuyTick {
				stock.BarBuyVolume += tickDeltaVol
				stock.FastAggressorEMA = 0.25*1.0 + 0.75*stock.FastAggressorEMA
			} else {
				stock.BarSellVolume += tickDeltaVol
				stock.FastAggressorEMA = 0.25*0.0 + 0.75*stock.FastAggressorEMA
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
	} else if a.hasNifty && idx == a.niftyIdx && stock.RollingPeak > 0 {
		// Instantaneous Nifty 50 Flash Shock detector (-0.18% drop from 20s RollingPeak)
		dropPct := ((stock.LTP - stock.RollingPeak) / stock.RollingPeak) * 100.0
		if dropPct <= -0.18 {
			a.macroShockExp = tick.ExchangeTime + 180
			a.inMacroShock.Store(true)
		}
	}

	// O(1) lock-free hot-path dispatch to active PositionWorker if bound
	if w := a.workers[idx]; w != nil {
		w.OnTickHotPath(stock, a.inMacroShock.Load())
	}
}

func (a *Actor) computeAndBroadcastSnapshot() {
	nowUnix := time.Now().Unix()

	// 1. Calculate Market Breadth (% > VWAP)
	var aboveVWAPCount, activeQuotedCount int
	for _, idx := range a.activeIndices {
		s := &a.stocks[idx]
		if s.LTP > 0 && s.ExchangeVWAP > 0 {
			activeQuotedCount++
			if s.LTP > s.ExchangeVWAP {
				aboveVWAPCount++
			}
		}
	}
	var breadthRatio float64 = 0.5
	if activeQuotedCount > 0 {
		breadthRatio = float64(aboveVWAPCount) / float64(activeQuotedCount)
	}

	prevBreadth5s := a.breadthHist[a.breadthHistIdx]
	if prevBreadth5s == 0 {
		prevBreadth5s = breadthRatio
	}
	a.breadthHist[a.breadthHistIdx] = breadthRatio
	a.breadthHistIdx = (a.breadthHistIdx + 1) % 6
	breadthDrop5s := breadthRatio - prevBreadth5s

	// 2. Market Pulse & Multi-Horizon Dynamic Regime Context
	var niftyLTP, niftyChg, bankLTP, bankChg float64
	var niftyIntradayRet, niftyDayCLV, niftyVel15m, niftyShockDrop float64
	niftyDayCLV = 0.50
	var niftyStock *StockMetrics
	if a.hasNifty {
		niftyStock = &a.stocks[a.niftyIdx]
		niftyLTP = niftyStock.LTP
		if niftyStock.PrevClose > 0 && niftyLTP > 0 {
			niftyChg = ((niftyLTP - niftyStock.PrevClose) / niftyStock.PrevClose) * 100.0
		}
		if niftyStock.Open > 0 && niftyLTP > 0 {
			niftyIntradayRet = ((niftyLTP - niftyStock.Open) / niftyStock.Open) * 100.0
		}
		if niftyStock.High > niftyStock.Low && niftyLTP > 0 {
			niftyDayCLV = (niftyLTP - niftyStock.Low) / (niftyStock.High - niftyStock.Low)
		}
		lookback3 := 0
		if niftyStock.CurrentBarIdx >= 3 {
			lookback3 = int(niftyStock.CurrentBarIdx) - 3
		}
		ref15m := niftyStock.SessionBars[lookback3].Close
		if ref15m <= 0 {
			ref15m = niftyStock.Open
		}
		if ref15m > 0 && niftyLTP > 0 {
			niftyVel15m = ((niftyLTP - ref15m) / ref15m) * 100.0
		}
		if niftyStock.RollingPeak > 0 && niftyLTP > 0 {
			niftyShockDrop = ((niftyLTP - niftyStock.RollingPeak) / niftyStock.RollingPeak) * 100.0
		}
	}
	if a.hasBankNifty {
		b := &a.stocks[a.bankNiftyIdx]
		bankLTP = b.LTP
		if b.PrevClose > 0 && bankLTP > 0 {
			bankChg = ((bankLTP - b.PrevClose) / b.PrevClose) * 100.0
		}
	}

	// Update FLASH_SHOCK state
	if niftyShockDrop <= -0.18 || (breadthDrop5s <= -0.12 && niftyVel15m <= -0.20) {
		a.macroShockExp = nowUnix + 180
		a.inMacroShock.Store(true)
	} else if a.inMacroShock.Load() {
		if nowUnix >= a.macroShockExp || (niftyVel15m >= 0.05 && breadthRatio >= 0.52) {
			a.inMacroShock.Store(false)
		}
	}

	// 6-State Dynamic Intraday Regime Classification
	regime := "SIDEWAYS_CHOP"
	switch {
	case a.inMacroShock.Load():
		regime = "FLASH_SHOCK"
	case niftyDayCLV < 0.35 && niftyVel15m <= -0.18 && breadthRatio < 0.48:
		// Gap-up rollover / afternoon institutional distribution
		regime = "INTRADAY_DISTRIBUTION"
	case breadthRatio <= 0.42 && (niftyIntradayRet < -0.20 || niftyChg < -0.35) && niftyDayCLV < 0.40:
		regime = "BEARISH_EXPANSION"
	case niftyChg < 0 && niftyIntradayRet >= 0.25 && niftyDayCLV >= 0.65 && breadthRatio >= 0.58 && niftyVel15m >= 0.10:
		// Gap-down followed by strong intraday DII/FII V-recovery
		regime = "V_RECOVERY_BULLISH"
	case breadthRatio >= 0.58 && (niftyIntradayRet >= 0.15 || niftyChg >= 0.25) && niftyDayCLV >= 0.55:
		regime = "BULLISH_EXPANSION"
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

	pulseDTO := MarketPulseDTO{
		MarketState:            "OPEN",
		NiftyLTP:               niftyLTP,
		NiftyChangePercent:     niftyChg,
		BankNiftyLTP:           bankLTP,
		BankNiftyChangePercent: bankChg,
		BreadthAboveVWAP:       math.Round(breadthRatio*1000.0) / 10.0,
		MarketRegime:           regime,
	}

	// Invoke Phase 2 Autonomous Execution Engine every 1.0s (even when 0 browser tabs are open!)
	var execState *ExecutionStateDTO
	if a.execHook != nil {
		execState = a.execHook.OnSecondTick(&a.stocks, a.activeIndices, bullishSorted, leaderMap, pulseDTO, a.inMacroShock.Load())
	}

	if a.wsHub == nil || a.wsHub.ClientCount() == 0 {
		return // Zero connected clients: skip DTO allocations & WebSocket broadcast
	}

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
		Timestamp:        nowUnix,
		HistoricalActive: histHealthy,
		HistoricalError:  histErr,
		MarketPulse:      pulseDTO,
		Sectors:          sectorDTOs,
		TopBullish:       topBullish,
		TopBearish:       topBearish,
		AllStocks:        allStocks,
		Execution:        execState,
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

// GetStockDetail returns the full quantitative and order book state for a specific stock (serialized via Actor loop)
func (a *Actor) GetStockDetail(symbol string) (*StockDetailDTO, bool) {
	if !a.isRunning.Load() {
		return a.getStockDetailInternal(symbol)
	}
	reply := make(chan detailResp, 1)
	select {
	case a.detailReqChan <- detailReq{symbol: symbol, reply: reply}:
		select {
		case res := <-reply:
			return res.dto, res.found
		case <-time.After(2 * time.Second):
			return nil, false
		}
	case <-a.ctx.Done():
		return nil, false
	}
}

func (a *Actor) getStockDetailInternal(symbol string) (*StockDetailDTO, bool) {
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
	if s.Reasons&PenaltyPullbackRedBar != 0 {
		reasonsList = append(reasonsList, "Penalty: Red Pullback Bar Unreclaimed")
	}
	if s.Reasons&PenaltyExhaustedTrend != 0 {
		reasonsList = append(reasonsList, "Penalty: 3rd+ Late-Stage Pullback")
	}
	if s.Reasons&PenaltyIcebergSell != 0 {
		reasonsList = append(reasonsList, "Penalty: Iceberg Sell Distribution")
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
