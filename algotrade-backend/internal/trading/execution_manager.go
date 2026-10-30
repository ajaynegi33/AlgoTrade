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

	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	"github.com/ajaynegi33/AlgoTrade/internal/market"
	"github.com/ajaynegi33/AlgoTrade/internal/monitoring"
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
			_, _ = m.reconciler.SyncOrdersCoalesced(m.ctx, m.getActiveBroker())
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

func (manager *Manager) Stop() {
	manager.cancel()
}

func (manager *Manager) getActiveBroker() ExecutionBroker {
	if manager.capitalMgr.GetSettings().Mode == "LIVE" {
		return manager.liveBroker
	}
	return manager.paperBroker
}

func (manager *Manager) handleOrderDelta(positionID, legType string, orderEvent OrderUpdateEvent, deltaFilled int) {
	manager.mu.RLock()
	worker, isWorkerFound := manager.activeWorkers[positionID]
	manager.mu.RUnlock()
	if isWorkerFound && worker != nil {
		worker.OnOrderDelta(legType, orderEvent, deltaFilled)
	} else if deltaFilled > 0 && orderEvent.TransactionType == "BUY" {
		// Late fill after worker was closed/deleted!
		slog.Error("ORPHANED BUY FILL DETECTED: Position worker closed or not found, initiating emergency market liquidation",
			"position_id", positionID, "order_id", orderEvent.OrderID, "delta_filled", deltaFilled, "symbol", orderEvent.Symbol)
		go func() {
			broker := manager.getActiveBroker()
			_, err := broker.PlaceEmergencySweepSell(context.Background(), orderEvent.Symbol, orderEvent.InstrumentToken, deltaFilled, orderEvent.AveragePrice, 0.05, 0.0350, nil)
			if err != nil {
				slog.Error("CRITICAL: Failed to liquidate orphaned shares", "symbol", orderEvent.Symbol, "qty", deltaFilled, "error", err)
			}
		}()
	}
}

func (manager *Manager) onWorkerClosed(worker *PositionWorker) {
	dto := worker.ToDTO()

	manager.mu.Lock()
	delete(manager.activeWorkers, worker.PositionID)
	if currentWorker, isSymbolMapped := manager.symbolToWorker[worker.Symbol]; isSymbolMapped && currentWorker.PositionID == worker.PositionID {
		delete(manager.symbolToWorker, worker.Symbol)
	}
	if worker.filledEntryQty.Load() > 0 {
		manager.symbolCooldowns[worker.Symbol] = time.Now().Add(15 * time.Minute)
	} else {
		// If 0 shares filled (timed out or rejected), only apply a 60-second backoff
		manager.symbolCooldowns[worker.Symbol] = time.Now().Add(60 * time.Second)
	}
	manager.closedPositions = append([]market.ActivePositionDTO{dto}, manager.closedPositions...)
	if len(manager.closedPositions) > 50 {
		manager.closedPositions = manager.closedPositions[:50]
	}
	manager.mu.Unlock()

	if manager.actor != nil {
		manager.actor.BindPositionWorker(worker.Token, worker.Symbol, nil)
	}

	// Only record in daily risk ledger if at least 1 share was actually traded
	if worker.filledEntryQty.Load() > 0 {
		manager.capitalMgr.RecordClosedTrade(worker.Mode, dto.RealisedGrossPnL, dto.EstimatedCharges, dto.NetTotalPnL)
	}
	if worker.Mode == "LIVE" {
		go func() { _ = manager.capitalMgr.RefreshLiveMargins() }()
	}
}

// OnSecondTick is called every 1.0s inside Actor.computeAndBroadcastSnapshot
func (manager *Manager) OnSecondTick(
	stocks *[512]market.StockMetrics,
	activeStockIndices []uint16,
	bullishSortedStockIndices []uint16,
	sectorHealthMap map[uint8]market.SectorHealth,
	pulse market.MarketPulseDTO,
) *market.ExecutionStateDTO {
	now := time.Now()
	var topRVOL float64
	if len(bullishSortedStockIndices) > 0 {
		topRankedStock := &stocks[bullishSortedStockIndices[0]]
		topRVOL = max(topRankedStock.RVOLSession, 0.85*topRankedStock.RVOLPace5m)
	}

	sessionClock := EvaluateGlobalSessionClocks(now, pulse.BreadthAboveVWAP, topRVOL)
	settings := manager.capitalMgr.GetSettings()

	// Collect active position telemetry & blocked margin
	manager.mu.RLock()
	activePositionWorkers := make([]*PositionWorker, 0, len(manager.activeWorkers))
	for _, worker := range manager.activeWorkers {
		if worker.Mode == settings.Mode && !worker.isClosed.Load() {
			activePositionWorkers = append(activePositionWorkers, worker)
		}
	}
	closedPositionDTOs := make([]market.ActivePositionDTO, 0, len(manager.closedPositions))
	for _, closedPosition := range manager.closedPositions {
		if closedPosition.Mode == settings.Mode {
			closedPositionDTOs = append(closedPositionDTOs, closedPosition)
		}
	}
	manager.mu.RUnlock()

	// Guarantee deterministic FIFO chronological entry order (oldest entry first)
	slices.SortFunc(activePositionWorkers, func(workerA, workerB *PositionWorker) int {
		if workerA.OpenedAt.Before(workerB.OpenedAt) {
			return -1
		}
		if workerA.OpenedAt.After(workerB.OpenedAt) {
			return 1
		}
		return strings.Compare(workerA.PositionID, workerB.PositionID)
	})

	var blockedMargin, unrealisedPnL, totalOpenRiskRupees float64
	activeDTOs := make([]market.ActivePositionDTO, 0, len(activePositionWorkers))
	for _, worker := range activePositionWorkers {
		// Mandatory 15:14 IST Auto-Squareoff Sweep
		if sessionClock.Phase == "AUTO_SQUAREOFF_SWEEP" {
			worker.TriggerEmergencyExit("MANDATORY_1514_IST_MIS_SQUAREOFF")
		}

		dto := worker.ToDTO()
		activeDTOs = append(activeDTOs, dto)
		unrealisedPnL += dto.UnrealisedPnL
		totalOpenRiskRupees += worker.GetOpenRiskRupees()
		qtyForMargin := max(dto.RemainingQty, dto.InitialQty)
		misLeverage := worker.MISLeverage
		if misLeverage < 1.0 {
			misLeverage = 5.0
		}
		blockedMargin += (float64(qtyForMargin) * dto.EntryPrice) / misLeverage
	}

	capDTO, dailyRiskDTO, drawdownMult := manager.capitalMgr.ComputeCapitalSummary(
		blockedMargin,
		unrealisedPnL,
		totalOpenRiskRupees,
		len(activeDTOs),
		sessionClock.SizeMultiplier,
	)

	maxPortfolioHeatRupees := 0.02 * capDTO.NetEquity
	remainingHeatBudgetRupees := math.Max(0.0, maxPortfolioHeatRupees-totalOpenRiskRupees)
	var portfolioHeatPercentage float64
	if capDTO.NetEquity > 0 {
		portfolioHeatPercentage = totalOpenRiskRupees / capDTO.NetEquity
	}
	isPortfolioHeatExceeded := totalOpenRiskRupees >= maxPortfolioHeatRupees

	gateStatus := "PAUSED_MANUAL"
	gateReason := "Auto-Execute is paused. Toggle ARMED to enable autonomous signal processing"

	// Always compute Top 5 Candidate Gate Diagnostics for the Trader Cockpit Inspector
	candidate, pickStatus, pickReason, candidateGateDiagnostics := PickBestLongCandidate(
		stocks,
		bullishSortedStockIndices,
		sectorHealthMap,
		sessionClock,
		capDTO.NetEquity,
		capDTO.FreeMargin,
		settings.RiskPerTradePct,
		drawdownMult,
		remainingHeatBudgetRupees,
		manager.isSymbolUnavailable,
	)

	if dailyRiskDTO.IsCircuitBreakerLocked {
		gateStatus = "LOCKED_CIRCUIT_BREAKER"
		gateReason = dailyRiskDTO.LockReason
	} else if settings.Mode == "LIVE" && (manager.kiteService == nil || !manager.kiteService.GetStatus().IsConnected) {
		gateStatus = "LOCKED_KITE_OFFLINE"
		gateReason = "Zerodha Kite session is disconnected. Authenticate to trade in LIVE mode."
	} else if isPortfolioHeatExceeded {
		gateStatus = "LOCKED_MAX_PORTFOLIO_HEAT"
		gateReason = fmt.Sprintf("Max Portfolio Heat (%.2f%% / 2.00%%) reached. Open risk is ₹%.2f. Awaiting active stops to trail to breakeven.",
			portfolioHeatPercentage*100.0, totalOpenRiskRupees)
		if candidate != nil {
			if collector := monitoring.GetCollector(); collector != nil {
				collector.EmitShadowSignal(monitoring.ShadowSignalEvent{
					TradingDate:      time.Now().Format("2006-01-02"),
					SignalTime:       time.Now(),
					Symbol:           candidate.Symbol,
					InstrumentToken:  candidate.Token,
					PatternType:      candidate.PatternType,
					BullishScore:     candidate.BullishScore,
					RejectionReason:  "Max Portfolio Heat 2.0% Cap Reached",
					EntryPrice:       candidate.Sizing.EntryLimitPrice,
					HypotheticalStop: candidate.Sizing.StopSpec.InitialStop,
					OneR:             candidate.Sizing.StopSpec.OneR,
					Target1R:         candidate.Sizing.EntryLimitPrice + candidate.Sizing.StopSpec.OneR,
				})
			}
		}
	} else if settings.AutoExecute {
		gateStatus = pickStatus
		gateReason = pickReason
		if candidate != nil {
			manager.spawnCandidateWorker(*candidate, settings.Mode, &stocks[candidate.StockIdx])
		}
	} else if candidate != nil {
		gateStatus = "PAUSED_SIGNAL_READY"
		gateReason = fmt.Sprintf("Ready: %s on %s (Qty %d @ ₹%.2f) — Arm Auto-Execute to fire",
			candidate.PatternType, candidate.Symbol, candidate.Sizing.Quantity, candidate.Sizing.EntryLimitPrice)
	}

	stateDTO := &market.ExecutionStateDTO{
		Mode:                 settings.Mode,
		IsAutoExecuteEnabled: settings.AutoExecute,
		IsMacroShockActive:   false,
		GateStatus:           gateStatus,
		GateReason:        gateReason,
		Capital:           capDTO,
		DailyRisk:         dailyRiskDTO,
		SessionClock:      sessionClock,
		ActivePositions:   activeDTOs,
		ClosedPositions:   closedPositionDTOs,
		TopCandidateGates: candidateGateDiagnostics,
	}

	manager.lastStateDTO.Store(stateDTO)
	return stateDTO
}

func (manager *Manager) isSymbolUnavailable(symbol string, _ uint32) bool {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if _, open := manager.symbolToWorker[symbol]; open {
		return true
	}
	if until, ok := manager.symbolCooldowns[symbol]; ok && time.Now().Before(until) {
		return true
	}
	return false
}

func (manager *Manager) spawnCandidateWorker(candidate CandidateSignal, mode string, targetStock *market.StockMetrics) {
	manager.mu.Lock()
	if _, exists := manager.symbolToWorker[candidate.Symbol]; exists {
		manager.mu.Unlock()
		return
	}
	seq := manager.posSeq.Add(1)
	positionID := fmt.Sprintf("POS-%s-%d", candidate.Symbol, seq)
	todayStr := market.TodayDateIST()

	var broker ExecutionBroker = manager.paperBroker
	if mode == "LIVE" {
		broker = manager.liveBroker
	}

	misLeverage := float64(targetStock.MISLeverage)
	if misLeverage < 1.0 {
		misLeverage = 5.0
	}

	worker := NewPositionWorker(manager.ctx, WorkerConfig{
		PositionID:           positionID,
		TradingDate:          todayStr,
		Mode:                 mode,
		Symbol:               candidate.Symbol,
		Token:                candidate.Token,
		StockIdx:             candidate.StockIdx,
		PatternType:          candidate.PatternType,
		TickSize:             float64(targetStock.TickSize),
		MISLeverage:          misLeverage,
		EntryBarIdx:          targetStock.CurrentBarIdx,
		Sizing:               candidate.Sizing,
		EntryTimeoutDuration: 5 * time.Minute,
		Broker:               broker,
		PaperBroker:          manager.paperBroker,
		Reconciler:           manager.reconciler,
		DB:                   manager.db,
		GetStockPtr:          func() *market.StockMetrics { return targetStock },
		OnClosed:             manager.onWorkerClosed,
	})

	manager.activeWorkers[positionID] = worker
	manager.symbolToWorker[candidate.Symbol] = worker
	manager.lastEntryTime = time.Now()
	manager.mu.Unlock()

	if manager.actor != nil {
		manager.actor.BindPositionWorker(candidate.Token, candidate.Symbol, worker)
	}

	go func() {
		if err := worker.Start(); err != nil {
			slog.Warn("Candidate PositionWorker failed to start", "symbol", candidate.Symbol, "error", err)
		} else {
			slog.Info("Spawned autonomous PositionWorker",
				"position_id", positionID,
				"mode", mode,
				"symbol", candidate.Symbol,
				"pattern", candidate.PatternType,
				"qty", candidate.Sizing.Quantity,
				"entry", candidate.Sizing.EntryLimitPrice,
				"stop", candidate.Sizing.StopSpec.InitialStop,
			)
		}
	}()
}

func (manager *Manager) backgroundMaintenanceLoop() {
	reconcileTicker := time.NewTicker(10 * time.Second)
	defer reconcileTicker.Stop()

	marginTicker := time.NewTicker(30 * time.Second)
	defer marginTicker.Stop()

	for {
		select {
		case <-manager.ctx.Done():
			return

		case <-reconcileTicker.C:
			manager.mu.RLock()
			activeCount := len(manager.activeWorkers)
			manager.mu.RUnlock()
			if activeCount > 0 {
				_, _ = manager.reconciler.SyncOrdersCoalesced(manager.ctx, manager.getActiveBroker())
			}

		case <-marginTicker.C:
			if manager.capitalMgr.GetSettings().Mode == "LIVE" {
				_ = manager.capitalMgr.RefreshLiveMargins()
			}
		}
	}
}

func (manager *Manager) loadClosedPositionsFromDB() {
	if manager.db == nil {
		return
	}
	todayStr := market.TodayDateIST()

	rows, err := manager.db.Query(`
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

	manager.mu.Lock()
	defer manager.mu.Unlock()
	for rows.Next() {
		var dto market.ActivePositionDTO
		var hasTranche1CompletedInt int
		if err := rows.Scan(
			&dto.PositionID, &dto.Mode, &dto.Symbol, &dto.InstrumentToken, &dto.State, &dto.PatternType,
			&dto.EntryPrice, &dto.InitialStop, &dto.CurrentStop, &dto.ShadowStop, &dto.BreakevenStop, &dto.BaseBuffer,
			&dto.HighWaterMark, &dto.OneR, &dto.InitialQty, &dto.ModCount, &dto.CurrentGear, &hasTranche1CompletedInt,
			&dto.RealisedGrossPnL, &dto.EstimatedCharges, &dto.NetTotalPnL, &dto.ExitReason,
		); err == nil {
			dto.MaxMods = MaxExchangeOrderMods
			dto.HasTranche1Completed = hasTranche1CompletedInt != 0
			manager.closedPositions = append(manager.closedPositions, dto)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed iterating closed positions from DB", "error", err)
	}
}

// rehydrateOpenPositionsFromDB restores any open position workers after an unexpected server restart
func (manager *Manager) rehydrateOpenPositionsFromDB() {
	if manager.db == nil {
		return
	}
	todayStr := market.TodayDateIST()

	type rawOpenPosition struct {
		positionID, mode, symbol, state, pattern, entryOrderID, slOrderID, scaleOutOrderID, openedAtStr string
		token                                                                                               uint32
		entryPrice, initialStop, currentStop, shadowStop, breakevenStop, baseBuffer, highWaterMark, oneR     float64
		initialQuantity, modCount, gear, hasTranche1CompletedInt                                            int
		filledEntryQuantity, filledExitQuantity                                                             int64
	}
	var rawOpenPositions []rawOpenPosition

	rows, err := manager.db.Query(`
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
		var positionRecord rawOpenPosition
		if scanErr := rows.Scan(
			&positionRecord.positionID, &positionRecord.mode, &positionRecord.symbol, &positionRecord.token, &positionRecord.state, &positionRecord.pattern,
			&positionRecord.entryOrderID, &positionRecord.slOrderID, &positionRecord.scaleOutOrderID,
			&positionRecord.entryPrice, &positionRecord.initialStop, &positionRecord.currentStop, &positionRecord.shadowStop, &positionRecord.breakevenStop, &positionRecord.baseBuffer,
			&positionRecord.highWaterMark, &positionRecord.oneR, &positionRecord.initialQuantity, &positionRecord.filledEntryQuantity, &positionRecord.filledExitQuantity,
			&positionRecord.modCount, &positionRecord.gear, &positionRecord.hasTranche1CompletedInt, &positionRecord.openedAtStr,
		); scanErr == nil {
			if !(positionRecord.filledEntryQuantity > 0 && positionRecord.filledExitQuantity >= positionRecord.filledEntryQuantity) {
				rawOpenPositions = append(rawOpenPositions, positionRecord)
			}
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed iterating open positions for rehydration", "error", err)
	}

	for _, positionRecord := range rawOpenPositions {
		var broker ExecutionBroker = manager.paperBroker
		if positionRecord.mode == "LIVE" {
			broker = manager.liveBroker
		}

		sizing := SizingDecision{
			IsApproved:      true,
			Quantity:        positionRecord.initialQuantity,
			DustFloorQty:    1,
			EntryLimitPrice: positionRecord.entryPrice,
			BreakevenStop:   positionRecord.breakevenStop,
			StopSpec: StopAndBufferSpec{
				BaseBuffer:   positionRecord.baseBuffer,
				InitialStop:  positionRecord.initialStop,
				OneR:         positionRecord.oneR,
				SignalBarLow: positionRecord.initialStop,
			},
		}

		worker := NewPositionWorker(manager.ctx, WorkerConfig{
			PositionID:  positionRecord.positionID,
			TradingDate: todayStr,
			Mode:        positionRecord.mode,
			Symbol:      positionRecord.symbol,
			Token:       positionRecord.token,
			PatternType: positionRecord.pattern,
			TickSize:    0.05,
			MISLeverage: 5.0,
			Sizing:      sizing,
			Broker:      broker,
			PaperBroker: manager.paperBroker,
			Reconciler:  manager.reconciler,
			DB:          manager.db,
			OnClosed:    manager.onWorkerClosed,
		})

		worker.state = positionRecord.state
		worker.entryOrderID = positionRecord.entryOrderID
		worker.slOrderID = positionRecord.slOrderID
		worker.scaleOutOrderID = positionRecord.scaleOutOrderID
		worker.entryCostRupees = float64(positionRecord.filledEntryQuantity) * positionRecord.entryPrice
		worker.exitProceeds = float64(positionRecord.filledExitQuantity) * positionRecord.entryPrice
		worker.filledEntryQty.Store(positionRecord.filledEntryQuantity)
		worker.filledExitQty.Store(positionRecord.filledExitQuantity)
		worker.confirmedStopBits.Store(math.Float64bits(positionRecord.currentStop))
		worker.shadowStopBits.Store(math.Float64bits(positionRecord.shadowStop))
		worker.proposedStopBits.Store(math.Float64bits(positionRecord.currentStop))
		worker.highWaterMarkBits.Store(math.Float64bits(positionRecord.highWaterMark))
		worker.modCount.Store(int32(positionRecord.modCount))
		worker.currentGear.Store(int32(positionRecord.gear))
		worker.hasTranche1Completed.Store(positionRecord.hasTranche1CompletedInt != 0)
		remainingTimeout := 5 * time.Minute
		if positionRecord.openedAtStr != "" {
			if t, err := time.Parse(time.RFC3339, positionRecord.openedAtStr); err == nil {
				worker.OpenedAt = t
				worker.OpenedAtUnix = t.Unix()
			} else if t, err := time.Parse("2006-01-02 15:04:05", positionRecord.openedAtStr); err == nil {
				worker.OpenedAt = t
				worker.OpenedAtUnix = t.Unix()
			}
			elapsed := time.Since(worker.OpenedAt)
			if 5*time.Minute > elapsed {
				remainingTimeout = 5*time.Minute - elapsed
				if remainingTimeout < 30*time.Second {
					remainingTimeout = 30 * time.Second
				}
			} else {
				remainingTimeout = 30 * time.Second
			}
		}
		worker.entryTimeoutDuration = remainingTimeout

		if positionRecord.entryOrderID != "" {
			manager.reconciler.SeedOrderFilled(positionRecord.entryOrderID, int(positionRecord.filledEntryQuantity))
			entryStatus := OrderStatusComplete
			if positionRecord.state == StateEntryPlaced && int(positionRecord.filledEntryQuantity) < positionRecord.initialQuantity {
				entryStatus = OrderStatusOpen
			}
			manager.reconciler.RegisterOrder(positionRecord.entryOrderID, positionRecord.positionID, "ENTRY", positionRecord.symbol, "BUY", "LIMIT", positionRecord.initialQuantity, positionRecord.entryPrice, 0, entryStatus)
			if positionRecord.mode == "PAPER" && manager.paperBroker != nil && entryStatus == OrderStatusOpen {
				manager.paperBroker.SeedRestingBuyOrder(positionRecord.entryOrderID, positionRecord.symbol, positionRecord.token, positionRecord.initialQuantity, int(positionRecord.filledEntryQuantity), positionRecord.entryPrice, OrderStatusOpen)
			}
		}
		if positionRecord.scaleOutOrderID != "" && positionRecord.filledExitQuantity > 0 {
			manager.reconciler.SeedOrderFilled(positionRecord.scaleOutOrderID, int(positionRecord.filledExitQuantity))
			manager.reconciler.RegisterOrder(positionRecord.scaleOutOrderID, positionRecord.positionID, "SCALE_OUT", positionRecord.symbol, "SELL", "LIMIT", int(positionRecord.filledExitQuantity), positionRecord.entryPrice, 0, OrderStatusComplete)
		}
		if positionRecord.slOrderID != "" {
			if positionRecord.scaleOutOrderID == "" && positionRecord.filledExitQuantity > 0 {
				manager.reconciler.SeedOrderFilled(positionRecord.slOrderID, int(positionRecord.filledExitQuantity))
			}
			remQty := int(positionRecord.filledEntryQuantity - positionRecord.filledExitQuantity)
			worker.mu.Lock()
			worker.slOrderQty = remQty
			worker.mu.Unlock()
			manager.reconciler.RegisterOrder(positionRecord.slOrderID, positionRecord.positionID, "SL", positionRecord.symbol, "SELL", "SL", remQty, positionRecord.currentStop, positionRecord.currentStop, OrderStatusTrigPending)
			if positionRecord.mode == "PAPER" && manager.paperBroker != nil && remQty > 0 {
				limitPx := ComputeSellSLLimitPrice(positionRecord.currentStop, worker.TickSize)
				manager.paperBroker.SeedRestingOrder(positionRecord.slOrderID, positionRecord.symbol, positionRecord.token, remQty, positionRecord.currentStop, limitPx, positionRecord.modCount, OrderStatusTrigPending)
			}
		}

		if manager.actor != nil {
			idx := manager.actor.BindPositionWorker(positionRecord.token, positionRecord.symbol, worker)
			worker.StockIdx = idx
			if stockPtr := manager.actor.GetStockPtrByIndex(idx); stockPtr != nil {
				worker.getStockPtr = func() *market.StockMetrics { return stockPtr }
				if stockPtr.TickSize > 0 {
					worker.TickSize = float64(stockPtr.TickSize)
				}
				if stockPtr.MISLeverage >= 1.0 {
					worker.MISLeverage = float64(stockPtr.MISLeverage)
				}
			}
		}

		manager.mu.Lock()
		manager.activeWorkers[positionRecord.positionID] = worker
		manager.symbolToWorker[positionRecord.symbol] = worker
		manager.mu.Unlock()

		go worker.workerLoop()
		if positionRecord.state == StateEmergencyFlatten && worker.RemainingQty() > 0 {
			exitReason := worker.exitReason
			if exitReason == "" {
				exitReason = "REHYDRATED_EMERGENCY_FLATTEN"
			}
			go worker.ForceEmergencyExit(exitReason)
		}
		slog.Info("Rehydrated open PositionWorker from SQLite crash recovery",
			"position_id", positionRecord.positionID, "symbol", positionRecord.symbol, "state", positionRecord.state, "remaining_qty", worker.RemainingQty())
	}
}

// GetCurrentState returns the latest 1s ExecutionStateDTO (or builds one on demand)
func (manager *Manager) GetCurrentState() *market.ExecutionStateDTO {
	if cached := manager.lastStateDTO.Load(); cached != nil {
		return cached
	}
	now := time.Now()
	clock := EvaluateGlobalSessionClocks(now, 50.0, 1.0)
	settings := manager.capitalMgr.GetSettings()
	capDTO, riskDTO, _ := manager.capitalMgr.ComputeCapitalSummary(0, 0, 0, 0, clock.SizeMultiplier)
	return &market.ExecutionStateDTO{
		Mode:            settings.Mode,
		IsAutoExecuteEnabled: settings.AutoExecute,
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
func (manager *Manager) UpdateSettings(settings ExecutionSettings) (*market.ExecutionStateDTO, error) {
	manager.mu.RLock()
	openCount := len(manager.activeWorkers)
	manager.mu.RUnlock()

	if err := manager.capitalMgr.UpdateSettings(settings, openCount); err != nil {
		return nil, err
	}
	if settings.Mode == "LIVE" {
		_ = manager.capitalMgr.RefreshLiveMargins()
	}
	return manager.GetCurrentState(), nil
}

// UpdatePaperCapital updates virtual paper capital (₹500 to ₹100Cr) in real time
func (manager *Manager) UpdatePaperCapital(paperCapital float64) (*market.ExecutionStateDTO, error) {
	if err := manager.capitalMgr.UpdatePaperCapital(paperCapital); err != nil {
		return nil, err
	}
	return manager.GetCurrentState(), nil
}

// FlattenPosition triggers immediate emergency flatten for one position or all open positions
func (manager *Manager) FlattenPosition(positionID string, shouldFlattenAll bool) (int, error) {
	manager.mu.RLock()
	var targetWorkers []*PositionWorker
	if shouldFlattenAll {
		for _, worker := range manager.activeWorkers {
			if !worker.isClosed.Load() {
				targetWorkers = append(targetWorkers, worker)
			}
		}
	} else if worker, ok := manager.activeWorkers[positionID]; ok && !worker.isClosed.Load() {
		targetWorkers = append(targetWorkers, worker)
	}
	manager.mu.RUnlock()

	if len(targetWorkers) == 0 && !shouldFlattenAll {
		return 0, fmt.Errorf("active position %s not found", positionID)
	}

	for _, worker := range targetWorkers {
		worker.ForceEmergencyExit("USER_MANUAL_FLATTEN")
	}
	return len(targetWorkers), nil
}

// HandleKiteWebhookPostback verifies Zerodha's SHA-256 checksum and routes the order update.
// Falsification Guard: Never allow an empty checksum to bypass verification in LIVE mode
// or when a Kite API secret is configured.
func (manager *Manager) HandleKiteWebhookPostback(orderEvent OrderUpdateEvent) error {
	requireAuth := manager.capitalMgr.GetSettings().Mode == "LIVE" || (manager.kiteService != nil && manager.kiteService.HasAPISecret())
	if requireAuth {
		if orderEvent.Checksum == "" || manager.kiteService == nil || !manager.kiteService.VerifyPostbackChecksum(orderEvent.OrderID, orderEvent.OrderTimestamp, orderEvent.Checksum) {
			slog.Warn("Rejected unauthorized or forged Kite webhook postback",
				"order_id", orderEvent.OrderID,
				"has_checksum", orderEvent.Checksum != "",
			)
			return fmt.Errorf("invalid or missing postback checksum for order_id=%s", orderEvent.OrderID)
		}
	}
	manager.reconciler.ProcessOrderEvent(orderEvent)
	return nil
}
