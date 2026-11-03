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

// PositionTickListener is the lock-free O(1) interface for active position workers to update trailing stops
type PositionTickListener interface {
	UpdateTrailingStopLossOnPriceTick(stockMetrics *StockMetrics)
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
	dto     *StockDetailDTO
	isFound bool
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
func (actor *Actor) GetStockPtrByIndex(stockIndex uint16) *StockMetrics {
	if int(stockIndex) >= len(actor.stocks) {
		return nil
	}
	return &actor.stocks[stockIndex]
}

// PushUniverseSync sends newly scanned or re-queried stocks to the Actor loop to update leverages and tokens
func (actor *Actor) PushUniverseSync(stocks []screener.ScannedStock) {
	if len(stocks) == 0 || !actor.isRunning.Load() {
		return
	}
	select {
	case actor.universeSyncChan <- stocks:
	case <-actor.ctx.Done():
	}
}

// ReloadUniverseFromDB re-queries scanned_stocks from SQLite and dispatches an in-memory sync to the actor loop
func (actor *Actor) ReloadUniverseFromDB() {
	if actor.scannerService == nil || !actor.isRunning.Load() {
		return
	}
	universe, err := actor.scannerService.GetUniverse("")
	if err == nil && universe != nil && len(universe.Stocks) > 0 {
		actor.PushUniverseSync(universe.Stocks)
	}
}

func (actor *Actor) initializeTodayEpoch() {
	now := time.Now().In(istZone)
	year, month, day := now.Date()
	today0915 := time.Date(year, month, day, 9, 15, 0, 0, istZone)
	actor.today0915Epoch = today0915.Unix()
}

func (actor *Actor) seedUniverseFromScreener() error {
	// Load Scanned F&O Cash Universe from scanner
	if actor.scannerService != nil {
		todayStr := TodayDateIST()
		universe, err := actor.scannerService.GetUniverse(todayStr)
		if err == nil && universe != nil && len(universe.Stocks) > 0 {
			for _, scannedStock := range universe.Stocks {
				secID, _, _ := screener.ResolveSector(actor.db, scannedStock.Tradingsymbol)
				tickSize := float32(scannedStock.TickSize)
				if tickSize <= 0 {
					tickSize = 0.05
				}
				idx := actor.registerTrackedStock(uint32(scannedStock.InstrumentToken), scannedStock.Tradingsymbol, secID, tickSize)
				actor.stocks[idx].LotSize = uint32(scannedStock.LotSize)
				actor.stocks[idx].PrevClose = scannedStock.LastPrice
				if scannedStock.MISLeverage > 0 {
					actor.stocks[idx].MISLeverage = float32(scannedStock.MISLeverage)
					if scannedStock.MISMarginPct > 0 {
						actor.stocks[idx].MISMarginFraction = float32(scannedStock.MISMarginPct)
					} else {
						actor.stocks[idx].MISMarginFraction = float32(1.0 / scannedStock.MISLeverage)
					}
				} else if actor.stocks[idx].MISLeverage <= 0 {
					actor.stocks[idx].MISLeverage = 5.0
					actor.stocks[idx].MISMarginFraction = 0.20
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
	stockMetrics := &actor.stocks[nextIdx]
	stockMetrics.InstrumentToken = token
	stockMetrics.SetSymbol(symbol)
	stockMetrics.SectorID = sectorID
	stockMetrics.TickSize = tickSize
	stockMetrics.ActiveIndex = nextIdx
	stockMetrics.SmoothedBuyerRatio = 0.50
	stockMetrics.AggressorBuyRatio = 0.50
	stockMetrics.FastAggressorEMA = 0.50
	if stockMetrics.MISLeverage <= 0 {
		stockMetrics.MISLeverage = 5.0
	}

	// Initialize bar 0
	stockMetrics.SessionBars[0] = OHLCVBar{Timestamp: actor.today0915Epoch}

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
			stockMetrics := &actor.stocks[idx]
			if !stockMetrics.IsWarm {
				targets = append(targets, seedTarget{token: stockMetrics.InstrumentToken, prevClose: stockMetrics.PrevClose})
			}
		}
	}

	for _, targetStock := range targets {
		select {
		case <-actor.ctx.Done():
			return
		default:
		}
		profile, err := actor.histClient.SeedStockBaselines(targetStock.token, targetStock.prevClose)
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
	stockMetrics := &actor.stocks[idx]
	actor.slotBaselines[idx] = profile.Slots

	// Backfill today's completed 5m intraday bars (09:15 AM onwards, including 15m ORH/ORL slots 0,1,2)
	if len(profile.TodayBars) > 0 {
		var cumVol uint64
		var weightedPriceVol float64
		maxSlot := int(stockMetrics.CurrentBarIdx)
		for _, todayBar := range profile.TodayBars {
			if todayBar.SlotIdx < 0 || todayBar.SlotIdx >= 75 {
				continue
			}
			existing := &stockMetrics.SessionBars[todayBar.SlotIdx]
			if existing.Open == 0 || todayBar.Volume > existing.Volume {
				*existing = OHLCVBar{
					Open:      todayBar.Open,
					High:      todayBar.High,
					Low:       todayBar.Low,
					Close:     todayBar.Close,
					Volume:    todayBar.Volume,
					Timestamp: actor.today0915Epoch + int64(todayBar.SlotIdx)*300,
				}
			}
			if todayBar.SlotIdx > maxSlot {
				maxSlot = todayBar.SlotIdx
			}
			if stockMetrics.Open == 0 && todayBar.Open > 0 {
				stockMetrics.Open = todayBar.Open
			}
			if todayBar.High > stockMetrics.High {
				stockMetrics.High = todayBar.High
			}
			if (stockMetrics.Low == 0 || todayBar.Low < stockMetrics.Low) && todayBar.Low > 0 {
				stockMetrics.Low = todayBar.Low
			}
			cumVol += todayBar.Volume
			typPx := (todayBar.High + todayBar.Low + todayBar.Close) / 3.0
			weightedPriceVol += typPx * float64(todayBar.Volume)
		}
		if maxSlot > int(stockMetrics.CurrentBarIdx) && maxSlot < 75 {
			stockMetrics.CurrentBarIdx = uint8(maxSlot)
		}
		// If no live tick has arrived yet, seed LTP, Volume, and ExchangeVWAP from today's bars
		if stockMetrics.LastUpdatedUnix == 0 && len(profile.TodayBars) > 0 {
			lastBar := profile.TodayBars[len(profile.TodayBars)-1]
			if stockMetrics.LTP == 0 && lastBar.Close > 0 {
				stockMetrics.LTP = lastBar.Close
				stockMetrics.RollingPeak = lastBar.Close
			}
			if stockMetrics.Volume == 0 && cumVol > 0 {
				stockMetrics.Volume = cumVol
				if cumVol >= lastBar.Volume {
					stockMetrics.BarStartVolume = cumVol - lastBar.Volume
				}
			}
			if stockMetrics.ExchangeVWAP == 0 && cumVol > 0 {
				stockMetrics.ExchangeVWAP = weightedPriceVol / float64(cumVol)
			}
		}

		// Compute initial 9 EMA strictly across completed 5m bars.
		// If market is currently open (09:15 - 15:30), the bar at maxSlot is actively forming
		// and will be finalized at rollover in tick_ingestion. Only completed bars seed runningEMA.
		const alphaEMA9 = 2.0 / (9.0 + 1.0)
		var runningEMA float64
		var previousEMA float64
		var completedBarCount int

		completedBarLimit := maxSlot - 1
		nowIST := time.Now().In(istZone)
		minsSince0915 := (nowIST.Hour()*60 + nowIST.Minute()) - (9*60 + 15)
		if minsSince0915 >= 0 && minsSince0915 < 375 {
			activeSlot := minsSince0915 / 5
			if completedBarLimit >= activeSlot {
				completedBarLimit = activeSlot - 1
			}
		} else if minsSince0915 >= 375 {
			// Post-market: all bars up to maxSlot are completed
			completedBarLimit = maxSlot
		}

		for barIndex := 0; barIndex <= completedBarLimit && barIndex < 75; barIndex++ {
			closingPrice := stockMetrics.SessionBars[barIndex].Close
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
			stockMetrics.EMA9 = runningEMA
			if completedBarCount >= 2 && previousEMA > 0 {
				stockMetrics.EMA9Slope = runningEMA - previousEMA
			} else {
				stockMetrics.EMA9Slope = 0.0
			}
		}
	}

	curSlot := stockMetrics.CurrentBarIdx
	if curSlot >= 75 {
		curSlot = 74
	}
	stockMetrics.ATR14 = profile.ATR14
	stockMetrics.BaselineSlotVolume = profile.Slots[curSlot].MedianVolume
	stockMetrics.BaselineCumVolume = profile.Slots[curSlot].MedianCumVol
	// Only mark IsWarm = true when seeded with real historical data (not synthetic fallback)
	stockMetrics.IsWarm = !profile.IsFallback
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
			for tickIndex := range ticks {
				actor.processTick(&ticks[tickIndex])
			}

		case profile := <-actor.baselineSeedChan:
			actor.applyBaselineProfile(profile)

		case reply := <-actor.seedTargetReqChan:
			targets := make([]seedTarget, 0, len(actor.activeStockIndices))
			for _, idx := range actor.activeStockIndices {
				targetMetrics := &actor.stocks[idx]
				// Re-seed if still on synthetic fallback (!targetMetrics.IsWarm) or if today's 15m ORB bars (slots 0..2) are missing
				if !targetMetrics.IsWarm || targetMetrics.SessionBars[0].Open == 0 {
					targets = append(targets, seedTarget{
						token:     targetMetrics.InstrumentToken,
						prevClose: targetMetrics.PrevClose,
					})
				}
			}
			reply <- targets

		case scannedStocks := <-actor.universeSyncChan:
			var newTokens []uint32
			for _, scannedStock := range scannedStocks {
				tok := uint32(scannedStock.InstrumentToken)
				_, existed := actor.stockIndexByToken[tok]
				secID, _, _ := screener.ResolveSector(actor.db, scannedStock.Tradingsymbol)
				tickSize := float32(scannedStock.TickSize)
				if tickSize <= 0 {
					tickSize = 0.05
				}
				idx := actor.registerTrackedStock(tok, scannedStock.Tradingsymbol, secID, tickSize)
				actor.stocks[idx].LotSize = uint32(scannedStock.LotSize)
				if actor.stocks[idx].PrevClose <= 0 && scannedStock.LastPrice > 0 {
					actor.stocks[idx].PrevClose = scannedStock.LastPrice
				}
				if scannedStock.MISLeverage > 0 {
					actor.stocks[idx].MISLeverage = float32(scannedStock.MISLeverage)
					if scannedStock.MISMarginPct > 0 {
						actor.stocks[idx].MISMarginFraction = float32(scannedStock.MISMarginPct)
					} else {
						actor.stocks[idx].MISMarginFraction = float32(1.0 / scannedStock.MISLeverage)
					}
				} else if actor.stocks[idx].MISLeverage <= 0 {
					actor.stocks[idx].MISLeverage = 5.0
					actor.stocks[idx].MISMarginFraction = 0.20
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
			dto, isFound := actor.getStockDetailInternal(req.symbol)
			req.reply <- detailResp{dto: dto, isFound: isFound}

		case workerReq := <-actor.workerReqChan:
			idx, exists := actor.stockIndexByToken[workerReq.token]
			if !exists && workerReq.worker != nil {
				secID, _, _ := screener.ResolveSector(actor.db, workerReq.symbol)
				idx = actor.registerTrackedStock(workerReq.token, workerReq.symbol, secID, 0.05)
				actor.stocks[idx].IsTradeable = true
				if actor.tickerClient != nil {
					_ = actor.tickerClient.Subscribe([]uint32{workerReq.token})
				}
			}
			if int(idx) < maxTrackedStocks {
				actor.positionTickListeners[idx] = workerReq.worker
			}
			workerReq.reply <- idx
		}
	}
}
