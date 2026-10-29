package engine

import (
	"context"
	"database/sql"
	"log/slog"
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
func NewActor( db *sql.DB, tickerClient *kite.TickerClient, histClient *kite.HistoricalService, wsHub *ws.ClientHub, scannerService *scanner.Service ) *Actor {
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
			todayStr := TodayDateIST()
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
		todayStr := TodayDateIST()
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
