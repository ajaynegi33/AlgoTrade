package trading

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
	"github.com/ajaynegi33/AlgoTrade/internal/kite"
)

// Manager orchestrates Signal Selection, Capital Sizing, PositionWorkers, Reconciler, and 15:14 IST Auto-Squareoff
type Manager struct {
	db             *sql.DB
	actor          *market.Actor
	kiteService    *kite.Service
	tickerClient   *kite.TickerClient
	capitalMgr     *CapitalManager
	reconciler     *OrderReconciler
	liveBroker     *ZerodhaLiveBroker
	paperBroker    *PaperBroker

	mu              sync.RWMutex
	activeWorkers   map[string]*PositionWorker // positionID -> worker
	symbolToWorker  map[string]*PositionWorker // symbol -> active worker
	closedPositions []market.ActivePositionDTO
	symbolCooldowns map[string]time.Time // 15m re-entry lockout per symbol
	lastEntryTime   time.Time
	lastStateDTO    atomic.Pointer[market.ExecutionStateDTO]
	posSeq          atomic.Uint64

	ctx    context.Context
	cancel context.CancelFunc
}

func NewManager( db *sql.DB, actor *market.Actor, kiteService *kite.Service, tickerClient *kite.TickerClient ) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		db:              db,
		actor:           actor,
		kiteService:     kiteService,
		tickerClient:    tickerClient,
		capitalMgr:      NewCapitalManager(db, kiteService),
		activeWorkers:   make(map[string]*PositionWorker),
		symbolToWorker:  make(map[string]*PositionWorker),
		closedPositions: make([]market.ActivePositionDTO, 0, 32),
		symbolCooldowns: make(map[string]time.Time),
		ctx:             ctx,
		cancel:          cancel,
	}
	m.posSeq.Store(uint64(time.Now().Unix() % 100000))

	m.reconciler = NewOrderReconciler(db, m.handleOrderDelta)
	m.liveBroker = NewZerodhaLiveBroker(kiteService)
	m.paperBroker = NewPaperBroker(func(ev OrderUpdateEvent) {
		m.reconciler.ProcessOrderEvent(ev)
	})

	// Wire WebSocket order postbacks and reconnect sync
	if tickerClient != nil {
		tickerClient.SetOrderUpdateHandler(m.reconciler.HandleWebSocketTextFrame)
		tickerClient.SetReconnectHandler(func() {
			slog.Info("Kite Ticker reconnected: running immediate REST order reconciliation")
			_, _ = m.reconciler.SyncOrdersCoalesced(m.ctx, m.activeBroker())
		})
	}

	// Wire Kite OAuth authentication hook for MIS Leverage sync & Live Margin refresh
	if kiteService != nil {
		kiteService.RegisterOnAuthenticated(func() {
			_ = m.capitalMgr.RefreshLiveMargins()
			m.capitalMgr.SyncUniverseLeverages(m.ctx)
		})
	}

	// Load today's closed & open positions from SQLite for crash-recovery continuity across restarts
	m.loadClosedPositionsFromDB()
	m.rehydrateOpenPositionsFromDB()

	// Wire 1-second hook into Single-Writer Actor
	if actor != nil {
		actor.SetExecutionHook(m)
	}

	go m.backgroundMaintenanceLoop()
	return m
}

func (m *Manager) Stop() {
	m.cancel()
}

func (m *Manager) activeBroker() ExecutionBroker {
	if m.capitalMgr.GetSettings().Mode == "LIVE" {
		return m.liveBroker
	}
	return m.paperBroker
}

func (m *Manager) handleOrderDelta(positionID, legType string, ev OrderUpdateEvent, deltaFilled int) {
	m.mu.RLock()
	w, exists := m.activeWorkers[positionID]
	m.mu.RUnlock()
	if exists && w != nil {
		w.OnOrderDelta(legType, ev, deltaFilled)
	}
}

func (m *Manager) onWorkerClosed(w *PositionWorker) {
	dto := w.ToDTO()

	m.mu.Lock()
	delete(m.activeWorkers, w.PositionID)
	if cur, ok := m.symbolToWorker[w.Symbol]; ok && cur.PositionID == w.PositionID {
		delete(m.symbolToWorker, w.Symbol)
	}
	m.symbolCooldowns[w.Symbol] = time.Now().Add(15 * time.Minute)
	m.closedPositions = append([]market.ActivePositionDTO{dto}, m.closedPositions...)
	if len(m.closedPositions) > 50 {
		m.closedPositions = m.closedPositions[:50]
	}
	m.mu.Unlock()

	if m.actor != nil {
		m.actor.BindPositionWorker(w.Token, w.Symbol, nil)
	}

	// Only record in daily risk ledger if at least 1 share was actually traded
	if w.filledEntryQty.Load() > 0 {
		m.capitalMgr.RecordClosedTrade(w.Mode, dto.RealisedGrossPnL, dto.EstimatedCharges, dto.NetTotalPnL)
	}
	if w.Mode == "LIVE" {
		go func() { _ = m.capitalMgr.RefreshLiveMargins() }()
	}
}

// OnSecondTick is called every 1.0s inside Actor.computeAndBroadcastSnapshot
func (m *Manager) OnSecondTick(
	stocks *[512]market.StockMetrics,
	activeIndices []uint16,
	bullishSorted []uint16,
	sectorHealthMap map[uint8]market.SectorHealth,
	pulse market.MarketPulseDTO,
) *market.ExecutionStateDTO {
	now := time.Now()
	var topRVOL float64
	if len(bullishSorted) > 0 {
		top := &stocks[bullishSorted[0]]
		topRVOL = max(top.RVOLSession, 0.85*top.RVOLPace5m)
	}

	sessionClock := EvaluateGlobalSessionClocks(now, pulse.BreadthAboveVWAP, topRVOL)
	settings := m.capitalMgr.GetSettings()

	// Collect active position telemetry & blocked margin
	m.mu.RLock()
	activeList := make([]*PositionWorker, 0, len(m.activeWorkers))
	for _, w := range m.activeWorkers {
		if w.Mode == settings.Mode && !w.isClosed.Load() {
			activeList = append(activeList, w)
		}
	}
	closedCopy := make([]market.ActivePositionDTO, 0, len(m.closedPositions))
	for _, cp := range m.closedPositions {
		if cp.Mode == settings.Mode {
			closedCopy = append(closedCopy, cp)
		}
	}
	m.mu.RUnlock()

	// Guarantee deterministic FIFO chronological entry order (oldest entry first)
	slices.SortFunc(activeList, func(a, b *PositionWorker) int {
		if a.OpenedAt.Before(b.OpenedAt) {
			return -1
		}
		if a.OpenedAt.After(b.OpenedAt) {
			return 1
		}
		return strings.Compare(a.PositionID, b.PositionID)
	})

	var blockedMargin, unrealisedPnL float64
	activeDTOs := make([]market.ActivePositionDTO, 0, len(activeList))
	for _, w := range activeList {
		// Mandatory 15:14 IST Auto-Squareoff Sweep
		if sessionClock.Phase == "AUTO_SQUAREOFF_SWEEP" {
			w.TriggerEmergencyExit("MANDATORY_1514_IST_MIS_SQUAREOFF")
		}

		dto := w.ToDTO()
		activeDTOs = append(activeDTOs, dto)
		unrealisedPnL += dto.UnrealisedPnL
		qtyForMargin := max(dto.RemainingQty, dto.InitialQty)
		lev := w.MISLeverage
		if lev < 1.0 {
			lev = 5.0
		}
		blockedMargin += (float64(qtyForMargin) * dto.EntryPrice) / lev
	}

	capDTO, dailyRiskDTO, drawdownMult := m.capitalMgr.ComputeCapitalSummary(
		blockedMargin,
		unrealisedPnL,
		len(activeDTOs),
		sessionClock.SizeMultiplier,
	)

	gateStatus := "PAUSED_MANUAL"
	gateReason := "Auto-Execute is paused. Toggle ARMED to enable autonomous signal "

	// Always compute Top 5 Candidate Gate Diagnostics for the Trader Cockpit Inspector
	candidate, pickStatus, pickReason, candidateGates := PickBestLongCandidate(
		stocks,
		bullishSorted,
		sectorHealthMap,
		sessionClock,
		capDTO.NetEquity,
		capDTO.FreeMargin,
		settings.RiskPerTradePct,
		drawdownMult,
		m.isSymbolUnavailable,
	)

	if dailyRiskDTO.CircuitBreakerLocked {
		gateStatus = "LOCKED_CIRCUIT_BREAKER"
		gateReason = dailyRiskDTO.LockReason
	} else if settings.Mode == "LIVE" && (m.kiteService == nil || !m.kiteService.GetStatus().IsConnected) {
		gateStatus = "LOCKED_KITE_OFFLINE"
		gateReason = "Zerodha Kite session is disconnected. Authenticate to trade in LIVE mode."
	} else if len(activeDTOs) >= settings.MaxOpenPositions {
		gateStatus = "MAX_POSITIONS_ACTIVE"
		gateReason = fmt.Sprintf("Max concurrent positions (%d/%d) reached. Trailing active stops.", len(activeDTOs), settings.MaxOpenPositions)
	} else if settings.AutoExecute {
		gateStatus = pickStatus
		gateReason = pickReason
		if candidate != nil {
			m.spawnCandidateWorker(*candidate, settings.Mode, &stocks[candidate.StockIdx])
		}
	} else if candidate != nil {
		gateStatus = "PAUSED_SIGNAL_READY"
		gateReason = fmt.Sprintf("Ready: %s on %s (Qty %d @ ₹%.2f) — Arm Auto-Execute to fire",
			candidate.PatternType, candidate.Symbol, candidate.Sizing.Quantity, candidate.Sizing.EntryLimitPrice)
	}

	stateDTO := &market.ExecutionStateDTO{
		Mode:              settings.Mode,
		AutoExecute:       settings.AutoExecute,
		MacroShockActive:  false,
		GateStatus:        gateStatus,
		GateReason:        gateReason,
		Capital:           capDTO,
		DailyRisk:         dailyRiskDTO,
		SessionClock:      sessionClock,
		ActivePositions:   activeDTOs,
		ClosedPositions:   closedCopy,
		TopCandidateGates: candidateGates,
	}

	m.lastStateDTO.Store(stateDTO)
	return stateDTO
}

func (m *Manager) isSymbolUnavailable(symbol string, _ uint32) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, open := m.symbolToWorker[symbol]; open {
		return true
	}
	if until, ok := m.symbolCooldowns[symbol]; ok && time.Now().Before(until) {
		return true
	}
	return false
}

func (m *Manager) spawnCandidateWorker(cand CandidateSignal, mode string, stockPtr *market.StockMetrics) {
	m.mu.Lock()
	if _, exists := m.symbolToWorker[cand.Symbol]; exists {
		m.mu.Unlock()
		return
	}
	seq := m.posSeq.Add(1)
	posID := fmt.Sprintf("POS-%s-%d", cand.Symbol, seq)
	todayStr := market.TodayDateIST()

	var broker ExecutionBroker = m.paperBroker
	if mode == "LIVE" {
		broker = m.liveBroker
	}

	lev := float64(stockPtr.MISLeverage)
	if lev < 1.0 {
		lev = 5.0
	}

	worker := NewPositionWorker(m.ctx, WorkerConfig{
		PositionID:           posID,
		TradingDate:          todayStr,
		Mode:                 mode,
		Symbol:               cand.Symbol,
		Token:                cand.Token,
		StockIdx:             cand.StockIdx,
		PatternType:          cand.PatternType,
		TickSize:             float64(stockPtr.TickSize),
		MISLeverage:          lev,
		EntryBarIdx:          stockPtr.CurrentBarIdx,
		Sizing:               cand.Sizing,
		EntryTimeoutDuration: 5 * time.Minute,
		Broker:               broker,
		PaperBroker:          m.paperBroker,
		Reconciler:           m.reconciler,
		DB:                   m.db,
		GetStockPtr:          func() *market.StockMetrics { return stockPtr },
		OnClosed:             m.onWorkerClosed,
	})

	m.activeWorkers[posID] = worker
	m.symbolToWorker[cand.Symbol] = worker
	m.lastEntryTime = time.Now()
	m.mu.Unlock()

	if m.actor != nil {
		m.actor.BindPositionWorker(cand.Token, cand.Symbol, worker)
	}

	go func() {
		if err := worker.Start(); err != nil {
			slog.Warn("Candidate PositionWorker failed to start", "symbol", cand.Symbol, "error", err)
		} else {
			slog.Info("Spawned autonomous PositionWorker",
				"position_id", posID,
				"mode", mode,
				"symbol", cand.Symbol,
				"pattern", cand.PatternType,
				"qty", cand.Sizing.Quantity,
				"entry", cand.Sizing.EntryLimitPrice,
				"stop", cand.Sizing.StopSpec.InitialStop,
			)
		}
	}()
}

func (m *Manager) backgroundMaintenanceLoop() {
	reconcileTicker := time.NewTicker(10 * time.Second)
	defer reconcileTicker.Stop()

	marginTicker := time.NewTicker(30 * time.Second)
	defer marginTicker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return

		case <-reconcileTicker.C:
			m.mu.RLock()
			activeCount := len(m.activeWorkers)
			m.mu.RUnlock()
			if activeCount > 0 {
				_, _ = m.reconciler.SyncOrdersCoalesced(m.ctx, m.activeBroker())
			}

		case <-marginTicker.C:
			if m.capitalMgr.GetSettings().Mode == "LIVE" {
				_ = m.capitalMgr.RefreshLiveMargins()
			}
		}
	}
}

func (m *Manager) loadClosedPositionsFromDB() {
	if m.db == nil {
		return
	}
	todayStr := market.TodayDateIST()

	rows, err := m.db.Query(`
		SELECT position_id, mode, symbol, instrument_token, state, pattern_type,
		       entry_price, initial_stop, current_stop, shadow_stop, breakeven_stop, base_buffer,
		       high_water_mark, one_r, initial_qty, mod_count, current_gear, tranche1_done,
		       realised_gross_pnl, total_charges, realised_net_pnl, exit_reason
		FROM positions
		WHERE trading_date = ? AND state = 'CLOSED'
		ORDER BY closed_at DESC LIMIT 50
	`, todayStr)
	if err != nil {
		return
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()
	for rows.Next() {
		var dto market.ActivePositionDTO
		var t1Int int
		if err := rows.Scan(
			&dto.PositionID, &dto.Mode, &dto.Symbol, &dto.InstrumentToken, &dto.State, &dto.PatternType,
			&dto.EntryPrice, &dto.InitialStop, &dto.CurrentStop, &dto.ShadowStop, &dto.BreakevenStop, &dto.BaseBuffer,
			&dto.HighWaterMark, &dto.OneR, &dto.InitialQty, &dto.ModCount, &dto.CurrentGear, &t1Int,
			&dto.RealisedGrossPnL, &dto.EstimatedCharges, &dto.NetTotalPnL, &dto.ExitReason,
		); err == nil {
			dto.MaxMods = MaxExchangeOrderMods
			dto.Tranche1Done = t1Int != 0
			m.closedPositions = append(m.closedPositions, dto)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed iterating closed positions from DB", "error", err)
	}
}

// rehydrateOpenPositionsFromDB restores any open position workers after an unexpected server restart
func (m *Manager) rehydrateOpenPositionsFromDB() {
	if m.db == nil {
		return
	}
	todayStr := market.TodayDateIST()

	type rawOpenPos struct {
		posID, mode, symbol, state, pattern, entryOrd, slOrd, scaleOrd, openedAtStr string
		token                                                                       uint32
		entryPx, initStop, curStop, shStop, beStop, baseBuf, hwm, oneR              float64
		initQty, modCnt, gear, t1Int                                               int
		filledEntry, filledExit                                                    int64
	}
	var openList []rawOpenPos

	rows, err := m.db.Query(`
		SELECT position_id, mode, symbol, instrument_token, state, pattern_type,
		       entry_order_id, sl_order_id, scale_out_order_id,
		       entry_price, initial_stop, current_stop, shadow_stop, breakeven_stop, base_buffer,
		       high_water_mark, one_r, initial_qty, filled_entry_qty, filled_exit_qty,
		       mod_count, current_gear, tranche1_done, opened_at
		FROM positions
		WHERE trading_date = ? AND state != 'CLOSED'
		ORDER BY opened_at ASC
	`, todayStr)
	if err != nil {
		slog.Error("Failed querying open positions for rehydration", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p rawOpenPos
		if sErr := rows.Scan(
			&p.posID, &p.mode, &p.symbol, &p.token, &p.state, &p.pattern,
			&p.entryOrd, &p.slOrd, &p.scaleOrd,
			&p.entryPx, &p.initStop, &p.curStop, &p.shStop, &p.beStop, &p.baseBuf,
			&p.hwm, &p.oneR, &p.initQty, &p.filledEntry, &p.filledExit,
			&p.modCnt, &p.gear, &p.t1Int, &p.openedAtStr,
		); sErr == nil {
			if !(p.filledEntry > 0 && p.filledExit >= p.filledEntry) {
				openList = append(openList, p)
			}
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed iterating open positions for rehydration", "error", err)
	}

	for _, p := range openList {
		var broker ExecutionBroker = m.paperBroker
		if p.mode == "LIVE" {
			broker = m.liveBroker
		}

		sizing := SizingDecision{
			Approved:        true,
			Quantity:        p.initQty,
			DustFloorQty:    1,
			EntryLimitPrice: p.entryPx,
			BreakevenStop:   p.beStop,
			StopSpec: StopAndBufferSpec{
				BaseBuffer:   p.baseBuf,
				InitialStop:  p.initStop,
				OneR:         p.oneR,
				SignalBarLow: p.initStop,
			},
		}

		w := NewPositionWorker(m.ctx, WorkerConfig{
			PositionID:  p.posID,
			TradingDate: todayStr,
			Mode:        p.mode,
			Symbol:      p.symbol,
			Token:       p.token,
			PatternType: p.pattern,
			TickSize:    0.05,
			MISLeverage: 5.0,
			Sizing:      sizing,
			Broker:      broker,
			PaperBroker: m.paperBroker,
			Reconciler:  m.reconciler,
			DB:          m.db,
			OnClosed:    m.onWorkerClosed,
		})

		w.state = p.state
		w.entryOrderID = p.entryOrd
		w.slOrderID = p.slOrd
		w.scaleOutOrderID = p.scaleOrd
		w.entryCostRupees = float64(p.filledEntry) * p.entryPx
		w.exitProceeds = float64(p.filledExit) * p.entryPx
		w.filledEntryQty.Store(p.filledEntry)
		w.filledExitQty.Store(p.filledExit)
		w.confirmedStopBits.Store(math.Float64bits(p.curStop))
		w.shadowStopBits.Store(math.Float64bits(p.shStop))
		w.proposedStopBits.Store(math.Float64bits(p.curStop))
		w.highWaterMarkBits.Store(math.Float64bits(p.hwm))
		w.modCount.Store(int32(p.modCnt))
		w.currentGear.Store(int32(p.gear))
		w.tranche1Done.Store(p.t1Int != 0)
		if p.openedAtStr != "" {
			if t, err := time.Parse(time.RFC3339, p.openedAtStr); err == nil {
				w.OpenedAt = t
				w.OpenedAtUnix = t.Unix()
			} else if t, err := time.Parse("2006-01-02 15:04:05", p.openedAtStr); err == nil {
				w.OpenedAt = t
				w.OpenedAtUnix = t.Unix()
			}
		}

		if p.entryOrd != "" {
			m.reconciler.SeedOrderFilled(p.entryOrd, int(p.filledEntry))
			m.reconciler.RegisterOrder(p.entryOrd, p.posID, "ENTRY", p.symbol, "BUY", "LIMIT", p.initQty, p.entryPx, 0, OrderStatusComplete)
		}
		if p.scaleOrd != "" && p.filledExit > 0 {
			m.reconciler.SeedOrderFilled(p.scaleOrd, int(p.filledExit))
			m.reconciler.RegisterOrder(p.scaleOrd, p.posID, "SCALE_OUT", p.symbol, "SELL", "LIMIT", int(p.filledExit), p.entryPx, 0, OrderStatusComplete)
		}
		if p.slOrd != "" {
			if p.scaleOrd == "" && p.filledExit > 0 {
				m.reconciler.SeedOrderFilled(p.slOrd, int(p.filledExit))
			}
			remQty := int(p.filledEntry - p.filledExit)
			m.reconciler.RegisterOrder(p.slOrd, p.posID, "SL", p.symbol, "SELL", "SL", remQty, p.curStop, p.curStop, OrderStatusTrigPending)
			if p.mode == "PAPER" && m.paperBroker != nil && remQty > 0 {
				limitPx := ComputeSellSLLimitPrice(p.curStop, w.TickSize)
				m.paperBroker.SeedRestingOrder(p.slOrd, p.symbol, p.token, remQty, p.curStop, limitPx, p.modCnt, OrderStatusTrigPending)
			}
		}

		if m.actor != nil {
			idx := m.actor.BindPositionWorker(p.token, p.symbol, w)
			w.StockIdx = idx
			if stockPtr := m.actor.GetStockPtrByIndex(idx); stockPtr != nil {
				w.getStockPtr = func() *market.StockMetrics { return stockPtr }
				if stockPtr.TickSize > 0 {
					w.TickSize = float64(stockPtr.TickSize)
				}
				if stockPtr.MISLeverage >= 1.0 {
					w.MISLeverage = float64(stockPtr.MISLeverage)
				}
			}
		}

		m.mu.Lock()
		m.activeWorkers[p.posID] = w
		m.symbolToWorker[p.symbol] = w
		m.mu.Unlock()

		go w.workerLoop()
		if p.state == StateEmergencyFlatten && w.RemainingQty() > 0 {
			exitR := w.exitReason
			if exitR == "" {
				exitR = "REHYDRATED_EMERGENCY_FLATTEN"
			}
			go w.ForceEmergencyExit(exitR)
		}
		slog.Info("Rehydrated open PositionWorker from SQLite crash recovery",
			"position_id", p.posID, "symbol", p.symbol, "state", p.state, "remaining_qty", w.RemainingQty())
	}
}

// GetCurrentState returns the latest 1s ExecutionStateDTO (or builds one on demand)
func (m *Manager) GetCurrentState() *market.ExecutionStateDTO {
	if cached := m.lastStateDTO.Load(); cached != nil {
		return cached
	}
	now := time.Now()
	clock := EvaluateGlobalSessionClocks(now, 50.0, 1.0)
	settings := m.capitalMgr.GetSettings()
	capDTO, riskDTO, _ := m.capitalMgr.ComputeCapitalSummary(0, 0, 0, clock.SizeMultiplier)
	return &market.ExecutionStateDTO{
		Mode:            settings.Mode,
		AutoExecute:     settings.AutoExecute,
		GateStatus:      "PAUSED_MANUAL",
		GateReason:      "Ready",
		Capital:         capDTO,
		DailyRisk:       riskDTO,
		SessionClock:    clock,
		ActivePositions: []market.ActivePositionDTO{},
		ClosedPositions: []market.ActivePositionDTO{},
	}
}

// UpdateSettings updates mode/auto_execute/risk settings safely
func (m *Manager) UpdateSettings(s ExecutionSettings) (*market.ExecutionStateDTO, error) {
	m.mu.RLock()
	openCount := len(m.activeWorkers)
	m.mu.RUnlock()

	if err := m.capitalMgr.UpdateSettings(s, openCount); err != nil {
		return nil, err
	}
	if s.Mode == "LIVE" {
		_ = m.capitalMgr.RefreshLiveMargins()
	}
	return m.GetCurrentState(), nil
}

// UpdatePaperCapital updates virtual paper capital (₹500 to ₹100Cr) in real time
func (m *Manager) UpdatePaperCapital(paperCapital float64) (*market.ExecutionStateDTO, error) {
	if err := m.capitalMgr.UpdatePaperCapital(paperCapital); err != nil {
		return nil, err
	}
	return m.GetCurrentState(), nil
}

// FlattenPosition triggers immediate emergency flatten for one position or all open positions
func (m *Manager) FlattenPosition(positionID string, flattenAll bool) (int, error) {
	m.mu.RLock()
	var targets []*PositionWorker
	if flattenAll {
		for _, w := range m.activeWorkers {
			if !w.isClosed.Load() {
				targets = append(targets, w)
			}
		}
	} else if w, ok := m.activeWorkers[positionID]; ok && !w.isClosed.Load() {
		targets = append(targets, w)
	}
	m.mu.RUnlock()

	if len(targets) == 0 && !flattenAll {
		return 0, fmt.Errorf("active position %s not found", positionID)
	}

	for _, w := range targets {
		w.ForceEmergencyExit("USER_MANUAL_FLATTEN")
	}
	return len(targets), nil
}

// HandleKiteWebhookPostback verifies Zerodha's SHA-256 checksum and routes the order update.
// Falsification Guard (Bug B1): Never allow an empty checksum to bypass verification in LIVE mode
// or when a Kite API secret is configured.
func (m *Manager) HandleKiteWebhookPostback(ev OrderUpdateEvent) error {
	requireAuth := m.capitalMgr.GetSettings().Mode == "LIVE" || (m.kiteService != nil && m.kiteService.HasAPISecret())
	if requireAuth {
		if ev.Checksum == "" || m.kiteService == nil || !m.kiteService.VerifyPostbackChecksum(ev.OrderID, ev.OrderTimestamp, ev.Checksum) {
			slog.Warn("Rejected unauthorized or forged Kite webhook postback",
				"order_id", ev.OrderID,
				"has_checksum", ev.Checksum != "",
			)
			return fmt.Errorf("invalid or missing postback checksum for order_id=%s", ev.OrderID)
		}
	}
	m.reconciler.ProcessOrderEvent(ev)
	return nil
}
