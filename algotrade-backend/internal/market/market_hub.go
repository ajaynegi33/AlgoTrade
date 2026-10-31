package market

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	"github.com/ajaynegi33/AlgoTrade/internal/screener"
	"github.com/ajaynegi33/AlgoTrade/internal/ws"
)

const (
	maxTrackedStocks = 512
)

// BarFlushItem holds a finalized 5m candle for background SQLite persistence
type BarFlushItem struct {
	Token uint32
	Bar   OHLCVBar
}

// PositionTickListener is the lock-free O(1) hot-path interface for active position workers
type PositionTickListener interface {
	OnTickHotPath(stockMetrics *StockMetrics)
}

// ExecutionHook is invoked once per second inside the Actor loop with zero lock contention
type ExecutionHook interface {
	OnSecondTick(
		stocks *[maxTrackedStocks]StockMetrics,
		activeStockIndices []uint16,
		bullishSorted []uint16,
		sectorHealthMap map[uint8]SectorHealth,
		pulse MarketPulseDTO,
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
	scannerService *screener.Service

	// Pure Actor Memory: Owned exclusively by the actor goroutine (zero mutexes in hot loop)
	stocks         [maxTrackedStocks]StockMetrics
	slotBaselines  [maxTrackedStocks][75]kite.SlotBaseline
	positionTickListeners [maxTrackedStocks]PositionTickListener
	stockIndexByToken    map[uint32]uint16
	stockIndexBySymbol   map[string]uint16
	activeStockIndices  []uint16
	sectorHealthMap map[uint8]SectorHealth

	execHook          ExecutionHook
	detailReqChan     chan detailReq
	workerReqChan     chan registerWorkerReq
	barFlushChan      chan BarFlushItem
	baselineSeedChan  chan *kite.StockBaselineProfile
	seedTargetReqChan chan chan []seedTarget
	universeSyncChan  chan []screener.ScannedStock
	seedingInProgress atomic.Bool
	today0915Epoch    int64
	ticksProcessed    atomic.Uint64
	isRunning         atomic.Bool

	ctx            context.Context
	cancel         context.CancelFunc
}

// NewActor creates a new Single-Writer Actor instance
func NewActor( db *sql.DB, tickerClient *kite.TickerClient, histClient *kite.HistoricalService, wsHub *ws.ClientHub, scannerService *screener.Service ) *Actor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Actor{
		db:                db,
		tickerClient:      tickerClient,
		histClient:        histClient,
		wsHub:             wsHub,
		scannerService:    scannerService,
		stockIndexByToken:     make(map[uint32]uint16, maxTrackedStocks),
		stockIndexBySymbol:    make(map[string]uint16, maxTrackedStocks),
		activeStockIndices:    make([]uint16, 0, maxTrackedStocks),
		sectorHealthMap:   make(map[uint8]SectorHealth, 16),
		detailReqChan:     make(chan detailReq, 64),
		workerReqChan:     make(chan registerWorkerReq, 64),
		barFlushChan:      make(chan BarFlushItem, 4096),
		baselineSeedChan:  make(chan *kite.StockBaselineProfile, 512),
		seedTargetReqChan: make(chan chan []seedTarget, 8),
		universeSyncChan:  make(chan []screener.ScannedStock, 4),
		ctx:               ctx,
		cancel:            cancel,
	}
}

// SetExecutionHook wires the Phase 2 Execution Manager into the Actor's 1s loop
func (actor *Actor) SetExecutionHook(hook ExecutionHook) {
	actor.execHook = hook
}

// BindPositionWorker registers or unregisters a PositionWorker on the O(1) hot path (thread-safe via Actor loop)
func (actor *Actor) BindPositionWorker(token uint32, symbol string, worker PositionTickListener) uint16 {
	if !actor.isRunning.Load() {
		idx, ok := actor.stockIndexByToken[token]
		if !ok && worker != nil {
			idx = actor.registerTrackedStock(token, symbol, screener.SectorNifty50, 0.05)
		}
		if int(idx) < maxTrackedStocks {
			actor.positionTickListeners[idx] = worker
		}
		return idx
	}
	reply := make(chan uint16, 1)
	select {
	case actor.workerReqChan <- registerWorkerReq{token: token, symbol: symbol, worker: worker, reply: reply}:
		select {
		case idx := <-reply:
			return idx
		case <-time.After(2 * time.Second):
			return 0
		}
	case <-actor.ctx.Done():
		return 0
	}
}

// Start boots the actor goroutine and background SQLite writer
func (actor *Actor) Start() error {
	if actor.isRunning.CompareAndSwap(false, true) {
		actor.initializeTodayEpoch()

		// 1. Initialize universe & baselines
		if err := actor.seedUniverseFromScreener(); err != nil {
			slog.Warn("Universe seed encountered issue, will run with partial set", "error", err)
		}

		// 2. Start background SQLite bar flush worker
		go actor.sqliteBarWriter()

		// 3. Start Actor hot loop
		go actor.runLoop()

		// 4. Pre-seed 20-Day Baselines & Today's Intraday Bars in background
		actor.TriggerHistoricalReseed()

		slog.Info("Single-Writer Engine Actor started successfully",
			"tracked_stocks", len(actor.activeStockIndices),
			"token_map_size", len(actor.stockIndexByToken),
		)
	}
	return nil
}

// TriggerHistoricalReseed starts or re-runs background 20-day historical baseline & intraday bar seeding
func (actor *Actor) TriggerHistoricalReseed() {
	if actor.histClient == nil {
		return
	}
	if !actor.seedingInProgress.CompareAndSwap(false, true) {
		return
	}
	go actor.seedBaselinesAsynchronously()
}

// OnKiteAuthenticated is invoked when Zerodha Kite OAuth succeeds or restores a valid session.
// It syncs any newly scanned universe stocks and triggers historical baseline + intraday bar seeding.
func (actor *Actor) OnKiteAuthenticated() {
	if actor.histClient != nil {
		actor.histClient.ClearNoSessionError()
	}
	go func() {
		if actor.scannerService != nil {
			todayStr := TodayDateIST()
			universe, err := actor.scannerService.GetUniverse(todayStr)
			if err != nil || universe == nil || len(universe.Stocks) == 0 || universe.ScanDate != todayStr {
				if scanned, scanErr := actor.scannerService.RunScan(false); scanErr == nil && scanned != nil {
					universe = scanned
				}
			}
			if universe != nil && len(universe.Stocks) > 0 && actor.isRunning.Load() {
				select {
				case actor.universeSyncChan <- universe.Stocks:
				case <-actor.ctx.Done():
					return
				}
			}
		}
		actor.TriggerHistoricalReseed()
	}()
}

// Stop shuts down the actor
func (actor *Actor) Stop() {
	if actor.isRunning.CompareAndSwap(true, false) {
		actor.cancel()
		slog.Info("Engine Actor stopped")
	}
}

// GetStats returns actor performance stats
func (actor *Actor) GetStats() (trackedCount int, ticksProcessed uint64) {
	return len(actor.activeStockIndices), actor.ticksProcessed.Load()
}

// GetStockPtrByIndex returns a pointer to the pre-allocated StockMetrics slot for crash-recovery rehydration
func (actor *Actor) GetStockPtrByIndex(idx uint16) *StockMetrics {
	if int(idx) >= len(actor.stocks) {
		return nil
	}
	return &actor.stocks[idx]
}

func (actor *Actor) initializeTodayEpoch() {
	now := time.Now().In(istZone)
	y, m, d := now.Date()
	today0915 := time.Date(y, m, d, 9, 15, 0, 0, istZone)
	actor.today0915Epoch = today0915.Unix()
}

func (actor *Actor) seedUniverseFromScreener() error {
	// Load Scanned F&O Cash Universe from scanner
	if actor.scannerService != nil {
		todayStr := TodayDateIST()
		universe, err := actor.scannerService.GetUniverse(todayStr)
		if err == nil && universe != nil && len(universe.Stocks) > 0 {
			for _, st := range universe.Stocks {
				secID, _, _ := screener.ResolveSector(actor.db, st.Tradingsymbol)
				tickSize := float32(st.TickSize)
				if tickSize <= 0 {
					tickSize = 0.05
				}
				idx := actor.registerTrackedStock(uint32(st.InstrumentToken), st.Tradingsymbol, secID, tickSize)
				actor.stocks[idx].LotSize = uint32(st.LotSize)
				actor.stocks[idx].PrevClose = st.LastPrice
				if st.MISLeverage > 0 {
					actor.stocks[idx].MISLeverage = float32(st.MISLeverage)
				} else {
					actor.stocks[idx].MISLeverage = 5.0
				}
				actor.stocks[idx].IsTradeable = true
			}
		}
	}

	// 4. Subscribe all tokens on Kite Ticker
	if actor.tickerClient != nil {
		tokens := make([]uint32, 0, len(actor.stockIndexByToken))
		for tok := range actor.stockIndexByToken {
			tokens = append(tokens, tok)
		}
		_ = actor.tickerClient.Subscribe(tokens)
	}

	return nil
}

func (actor *Actor) registerTrackedStock(token uint32, symbol string, sectorID uint8, tickSize float32) uint16 {
	upper := strings.ToUpper(strings.TrimSpace(symbol))
	if idx, exists := actor.stockIndexByToken[token]; exists {
		actor.stockIndexBySymbol[upper] = idx
		return idx
	}

	nextIdx := uint16(len(actor.stockIndexByToken))
	if int(nextIdx) >= maxTrackedStocks {
		return 0
	}

	actor.stockIndexByToken[token] = nextIdx
	actor.stockIndexBySymbol[upper] = nextIdx
	s := &actor.stocks[nextIdx]
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
	s.SessionBars[0] = OHLCVBar{Timestamp: actor.today0915Epoch}

	actor.activeStockIndices = append(actor.activeStockIndices, nextIdx)

	return nextIdx
}

func (actor *Actor) seedBaselinesAsynchronously() {
	defer actor.seedingInProgress.Store(false)
	if actor.histClient == nil {
		return
	}

	var targets []seedTarget
	if actor.isRunning.Load() {
		reply := make(chan []seedTarget, 1)
		select {
		case actor.seedTargetReqChan <- reply:
			select {
			case targets = <-reply:
			case <-actor.ctx.Done():
				return
			}
		case <-actor.ctx.Done():
			return
		}
	} else {
		for _, idx := range actor.activeStockIndices {
			s := &actor.stocks[idx]
			if !s.IsWarm {
				targets = append(targets, seedTarget{token: s.InstrumentToken, prevClose: s.PrevClose})
			}
		}
	}

	for _, t := range targets {
		select {
		case <-actor.ctx.Done():
			return
		default:
		}
		profile, err := actor.histClient.SeedStockBaselines(t.token, t.prevClose)
		if err == nil && profile != nil {
			if actor.isRunning.Load() {
				select {
				case actor.baselineSeedChan <- profile:
				case <-actor.ctx.Done():
					return
				}
			} else {
				actor.applyBaselineProfile(profile)
			}
		}
	}
	slog.Info("Completed 20-day baseline seeding for active equities", "seeded_count", len(targets))
}

func (actor *Actor) applyBaselineProfile(profile *kite.StockBaselineProfile) {
	if profile == nil {
		return
	}
	idx, exists := actor.stockIndexByToken[profile.Token]
	if !exists || int(idx) >= maxTrackedStocks {
		return
	}
	s := &actor.stocks[idx]
	actor.slotBaselines[idx] = profile.Slots

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
					Timestamp: actor.today0915Epoch + int64(tb.SlotIdx)*300,
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

		// Compute initial 9 EMA across today's seeded 5m bars
		const alphaEMA9 = 2.0 / (9.0 + 1.0)
		var runningEMA float64
		var previousEMA float64
		var completedBarCount int
		for barIndex := 0; barIndex <= maxSlot && barIndex < 75; barIndex++ {
			closingPrice := s.SessionBars[barIndex].Close
			if closingPrice > 0 {
				completedBarCount++
				if runningEMA <= 0 {
					runningEMA = closingPrice
				} else {
					previousEMA = runningEMA
					runningEMA = alphaEMA9*closingPrice + (1.0-alphaEMA9)*runningEMA
				}
			}
		}
		if runningEMA > 0 {
			s.EMA9 = runningEMA
			if completedBarCount >= 2 && previousEMA > 0 {
				s.EMA9Slope = runningEMA - previousEMA
			} else {
				s.EMA9Slope = 0.0
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

func (actor *Actor) runLoop() {
	var rawFeed <-chan []byte
	if actor.tickerClient != nil {
		rawFeed = actor.tickerClient.SubscribeRaw()
	}
	periodic1s := time.NewTicker(1000 * time.Millisecond)
	defer periodic1s.Stop()

	for {
		select {
		case <-actor.ctx.Done():
			return

		case rawBytes, ok := <-rawFeed:
			if !ok {
				return
			}
			ticks := ParseBinaryFrame(rawBytes)
			for i := range ticks {
				actor.processTick(&ticks[i])
			}

		case profile := <-actor.baselineSeedChan:
			actor.applyBaselineProfile(profile)

		case reply := <-actor.seedTargetReqChan:
			targets := make([]seedTarget, 0, len(actor.activeStockIndices))
			for _, idx := range actor.activeStockIndices {
				s := &actor.stocks[idx]
				// Re-seed if still on synthetic fallback (!s.IsWarm) or if today's 15m ORB bars (slots 0..2) are missing
				if !s.IsWarm || s.SessionBars[0].Open == 0 {
					targets = append(targets, seedTarget{
						token:     s.InstrumentToken,
						prevClose: s.PrevClose,
					})
				}
			}
			reply <- targets

		case scannedStocks := <-actor.universeSyncChan:
			var newTokens []uint32
			for _, st := range scannedStocks {
				tok := uint32(st.InstrumentToken)
				_, existed := actor.stockIndexByToken[tok]
				secID, _, _ := screener.ResolveSector(actor.db, st.Tradingsymbol)
				tickSize := float32(st.TickSize)
				if tickSize <= 0 {
					tickSize = 0.05
				}
				idx := actor.registerTrackedStock(tok, st.Tradingsymbol, secID, tickSize)
				actor.stocks[idx].LotSize = uint32(st.LotSize)
				if actor.stocks[idx].PrevClose <= 0 && st.LastPrice > 0 {
					actor.stocks[idx].PrevClose = st.LastPrice
				}
				if st.MISLeverage > 0 {
					actor.stocks[idx].MISLeverage = float32(st.MISLeverage)
				} else if actor.stocks[idx].MISLeverage <= 0 {
					actor.stocks[idx].MISLeverage = 5.0
				}
				actor.stocks[idx].IsTradeable = true
				if !existed {
					newTokens = append(newTokens, tok)
				}
			}
			if len(newTokens) > 0 && actor.tickerClient != nil {
				_ = actor.tickerClient.Subscribe(newTokens)
			}

		case <-periodic1s.C:
			actor.computeAndBroadcastSnapshot()

		case req := <-actor.detailReqChan:
			dto, found := actor.getStockDetailInternal(req.symbol)
			req.reply <- detailResp{dto: dto, found: found}

		case wReq := <-actor.workerReqChan:
			idx, exists := actor.stockIndexByToken[wReq.token]
			if !exists && wReq.worker != nil {
				secID, _, _ := screener.ResolveSector(actor.db, wReq.symbol)
				idx = actor.registerTrackedStock(wReq.token, wReq.symbol, secID, 0.05)
				actor.stocks[idx].IsTradeable = true
				if actor.tickerClient != nil {
					_ = actor.tickerClient.Subscribe([]uint32{wReq.token})
				}
			}
			if int(idx) < maxTrackedStocks {
				actor.positionTickListeners[idx] = wReq.worker
			}
			wReq.reply <- idx
		}
	}
}
