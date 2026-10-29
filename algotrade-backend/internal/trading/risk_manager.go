package trading

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
	"github.com/ajaynegi33/AlgoTrade/internal/kite"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

// ExecutionSettings holds persisted configuration for Paper vs Live execution
type ExecutionSettings struct {
	Mode             string  `json:"mode"`               // "PAPER" | "LIVE"
	AutoExecute      bool    `json:"auto_execute"`       // Whether autonomous signal entry is armed
	PaperCapital     float64 `json:"paper_capital"`      // Virtual capital (₹500 to ₹10Cr+)
	RiskPerTradePct  float64 `json:"risk_per_trade_pct"` // Default 0.005 (0.50%)
	MaxOpenPositions int     `json:"max_open_positions"` // Default 4
}

// CapitalManager manages real-time Zerodha & Paper capital, margin reservation, and daily circuit breakers
type CapitalManager struct {
	db          *sql.DB
	kiteService *kite.Service
	mu          sync.RWMutex

	settings          ExecutionSettings
	tradingDate       string
	liveFreeMargin    float64
	liveGrossBase     float64
	lastLiveRefreshAt time.Time

	// Ledger per mode ("PAPER" | "LIVE")
	dailyLedger map[string]*market.DailyRiskDTO
	realisedGrossByMode map[string]float64
	totalChargesByMode  map[string]float64
	realisedNetByMode   map[string]float64
}

func NewCapitalManager(db *sql.DB, kiteService *kite.Service) *CapitalManager {
	todayStr := market.TodayDateIST()

	cm := &CapitalManager{
		db:          db,
		kiteService: kiteService,
		tradingDate: todayStr,
		settings: ExecutionSettings{
			Mode:             "PAPER",
			AutoExecute:      false,
			PaperCapital:     100000.0,
			RiskPerTradePct:  0.005,
			MaxOpenPositions: 4,
		},
		dailyLedger:         make(map[string]*market.DailyRiskDTO),
		realisedGrossByMode: make(map[string]float64),
		totalChargesByMode:  make(map[string]float64),
		realisedNetByMode:   make(map[string]float64),
	}

	cm.loadFromDatabase()
	return cm
}

func (manager *CapitalManager) loadFromDatabase() {
	if manager.db == nil {
		return
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()

	var mode string
	var isAutoExecuteFlag int
	var paperCapital, riskPercentage float64
	var maxOpenPositions int

	err := manager.db.QueryRow(`
		SELECT mode, auto_execute, paper_capital, risk_per_trade_pct, max_open_positions
		FROM execution_settings WHERE id = 1
	`).Scan(&mode, &isAutoExecuteFlag, &paperCapital, &riskPercentage, &maxOpenPositions)
	if err == nil {
		if mode == "LIVE" || mode == "PAPER" {
			manager.settings.Mode = mode
		}
		manager.settings.AutoExecute = isAutoExecuteFlag != 0
		if paperCapital >= 500.0 {
			manager.settings.PaperCapital = paperCapital
		}
		if riskPercentage > 0 && riskPercentage <= 0.03 {
			manager.settings.RiskPerTradePct = riskPercentage
		}
		if maxOpenPositions >= 1 && maxOpenPositions <= 15 {
			manager.settings.MaxOpenPositions = maxOpenPositions
		}
	}

	for _, modeName := range []string{"PAPER", "LIVE"} {
		manager.ReconcileWithPositionsDB(modeName)
	}
}

// ReconcileWithPositionsDB re-synchronizes the daily risk ledger with ground-truth closed trades in the positions table.
// Falsification Guard: Fixes any historical double-counting or ledger drift so /trades and the Dashboard remain 100% consistent.
func (cm *CapitalManager) ReconcileWithPositionsDB(mode string) {
	if cm.db == nil {
		return
	}

	rows, err := cm.db.Query(`
		SELECT realised_gross_pnl, total_charges, realised_net_pnl, closed_at
		FROM positions
		WHERE trading_date = ? AND mode = ? AND state = 'CLOSED'
		ORDER BY closed_at ASC
	`, cm.tradingDate, mode)
	if err != nil {
		return
	}
	defer rows.Close()

	var tradesTaken, wins, losses, consecLosses int
	var cumGross, cumCharges, cumNet, peakNet float64

	for rows.Next() {
		var gross, charges, net float64
		var closedAt sql.NullString
		if err := rows.Scan(&gross, &charges, &net, &closedAt); err != nil {
			continue
		}
		tradesTaken++
		cumGross += gross
		cumCharges += charges
		cumNet += net
		if cumNet > peakNet {
			peakNet = cumNet
		}

		if net > 0 {
			wins++
			consecLosses = 0
		} else {
			losses++
			consecLosses++
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed iterating daily risk positions from DB", "error", err)
	}

	dto := &market.DailyRiskDTO{
		TradingDate:       cm.tradingDate,
		TradesTaken:       tradesTaken,
		Wins:              wins,
		Losses:            losses,
		ConsecutiveLosses: consecLosses,
		PeakNetPnL:        peakNet,
	}
	if tradesTaken > 0 {
		dto.WinRatePct = math.Round((float64(wins)/float64(tradesTaken))*1000.0) / 10.0
	}

	dto.PeakNetPnL = peakNet
	cm.evaluateCircuitBreakersLocked(mode, dto, cumNet)

	cm.dailyLedger[mode] = dto
	cm.realisedGrossByMode[mode] = cumGross
	cm.totalChargesByMode[mode] = cumCharges
	cm.realisedNetByMode[mode] = cumNet

	cm.persistDailyLedgerLocked(mode, dto, cumGross, cumCharges, cumNet)
}

func (cm *CapitalManager) evaluateCircuitBreakersLocked(mode string, ledger *market.DailyRiskDTO, cumNet float64) {
	baseCap := cm.settings.PaperCapital
	if mode == "LIVE" && cm.liveGrossBase > 0 {
		baseCap = cm.liveGrossBase
	}
	oneR := baseCap * cm.settings.RiskPerTradePct

	// 1. Max Daily Loss Circuit Breaker (-2.0% of Capital or -3.0R)
	if baseCap > 0 && cumNet <= -0.020*baseCap {
		ledger.IsCircuitBreakerLocked = true
		ledger.LockReason = fmt.Sprintf("Max Daily Loss Reached (₹%.0f / -2.0%% cap)", cumNet)
	} else if ledger.ConsecutiveLosses >= 2 {
		// 2. 2 Consecutive Losses Lockout (Preserves capital; stop for the day)
		ledger.IsCircuitBreakerLocked = true
		ledger.LockReason = "2 Consecutive Losses Circuit Breaker"
	} else if oneR > 0 && ledger.PeakNetPnL >= 2.0*oneR && cumNet <= 0.55*ledger.PeakNetPnL {
		// 3. Profit-Giveback Lock (Protects banked green days once PeakNetPnL >= +2.0R)
		ledger.IsCircuitBreakerLocked = true
		ledger.LockReason = fmt.Sprintf("Profit-Giveback Lock (Protected ₹%.0f from ₹%.0f peak)", cumNet, ledger.PeakNetPnL)
	}
}

func (cm *CapitalManager) persistDailyLedgerLocked(mode string, ledger *market.DailyRiskDTO, cumGross, cumCharges, cumNet float64) {
	if cm.db == nil {
		return
	}
	lockedInt := 0
	if ledger.IsCircuitBreakerLocked {
		lockedInt = 1
	}
	_, _ = cm.db.Exec(`
		INSERT INTO daily_risk_ledger (
			trading_date, mode, trades_taken, wins, losses, consecutive_losses,
			realised_gross_pnl, total_charges, realised_net_pnl, peak_net_pnl,
			circuit_breaker_locked, lock_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(trading_date, mode) DO UPDATE SET
			trades_taken = excluded.trades_taken,
			wins = excluded.wins,
			losses = excluded.losses,
			consecutive_losses = excluded.consecutive_losses,
			realised_gross_pnl = excluded.realised_gross_pnl,
			total_charges = excluded.total_charges,
			realised_net_pnl = excluded.realised_net_pnl,
			peak_net_pnl = excluded.peak_net_pnl,
			circuit_breaker_locked = excluded.circuit_breaker_locked,
			lock_reason = excluded.lock_reason;
	`, cm.tradingDate, mode, ledger.TradesTaken, ledger.Wins, ledger.Losses, ledger.ConsecutiveLosses,
		cumGross, cumCharges, cumNet, ledger.PeakNetPnL, lockedInt, ledger.LockReason)
}

func (cm *CapitalManager) GetSettings() ExecutionSettings {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.settings
}

func (cm *CapitalManager) UpdateSettings(newSettings ExecutionSettings, activeOpenCount int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if newSettings.Mode != "PAPER" && newSettings.Mode != "LIVE" {
		return errors.New("mode must be 'PAPER' or 'LIVE'")
	}
	if newSettings.Mode != cm.settings.Mode && activeOpenCount > 0 {
		return fmt.Errorf("cannot switch mode from %s to %s while %d position(s) are open", cm.settings.Mode, newSettings.Mode, activeOpenCount)
	}
	if newSettings.RiskPerTradePct <= 0 || newSettings.RiskPerTradePct > 0.03 {
		return errors.New("risk_per_trade_pct must be between 0.001 (0.1%) and 0.03 (3.0%)")
	}
	if newSettings.MaxOpenPositions < 1 || newSettings.MaxOpenPositions > 15 {
		return errors.New("max_open_positions must be between 1 and 15")
	}
	if newSettings.PaperCapital >= 500.0 {
		cm.settings.PaperCapital = newSettings.PaperCapital
	}
	cm.settings.Mode = newSettings.Mode
	cm.settings.AutoExecute = newSettings.AutoExecute
	cm.settings.RiskPerTradePct = newSettings.RiskPerTradePct
	cm.settings.MaxOpenPositions = newSettings.MaxOpenPositions

	if cm.db != nil {
		autoInt := 0
		if cm.settings.AutoExecute {
			autoInt = 1
		}
		_, err := cm.db.Exec(`
			UPDATE execution_settings SET
				mode = ?,
				auto_execute = ?,
				paper_capital = ?,
				risk_per_trade_pct = ?,
				max_open_positions = ?,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = 1;
		`, cm.settings.Mode, autoInt, cm.settings.PaperCapital, cm.settings.RiskPerTradePct, cm.settings.MaxOpenPositions)
		if err != nil {
			return fmt.Errorf("failed to persist execution settings: %w", err)
		}
	}
	return nil
}

func (cm *CapitalManager) UpdatePaperCapital(newCapital float64) error {
	if newCapital < 500.0 || newCapital > 1000000000.0 {
		return errors.New("paper_capital must be between ₹500 and ₹100 Crore")
	}
	cm.mu.Lock()
	cm.settings.PaperCapital = newCapital
	cm.mu.Unlock()

	if cm.db != nil {
		_, err := cm.db.Exec(`UPDATE execution_settings SET paper_capital = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`, newCapital)
		if err != nil {
			return fmt.Errorf("failed to update paper_capital in SQLite: %w", err)
		}
	}
	return nil
}

// RefreshLiveMargins polls Zerodha /user/margins and updates LiveGrossBase & LiveFreeMargin
func (cm *CapitalManager) RefreshLiveMargins() error {
	if cm.kiteService == nil {
		return kite.ErrNoSession
	}
	margins, err := cm.kiteService.GetEquityMargins()
	if err != nil {
		return err
	}

	grossBase := margins.AvailableCash + margins.Collateral + margins.AdHocMargin - margins.UtilisedPayout
	if grossBase < margins.Net {
		grossBase = margins.Net
	}

	cm.mu.Lock()
	cm.liveGrossBase = math.Max(0, grossBase)
	cm.liveFreeMargin = math.Max(0, margins.Net)
	cm.lastLiveRefreshAt = time.Now()
	cm.mu.Unlock()
	return nil
}

// ensureCurrentTradingDateLocked rolls over the daily risk ledger and PnL counters if the server stays up overnight
func (cm *CapitalManager) ensureCurrentTradingDateLocked() {
	todayStr := market.TodayDateIST()
	if todayStr == cm.tradingDate {
		return
	}
	cm.tradingDate = todayStr
	for _, m := range []string{"PAPER", "LIVE"} {
		cm.realisedGrossByMode[m] = 0
		cm.totalChargesByMode[m] = 0
		cm.realisedNetByMode[m] = 0
		cm.dailyLedger[m] = &market.DailyRiskDTO{
			TradingDate: todayStr,
		}
	}
}

// ComputeCapitalSummary returns the live CapitalSummaryDTO and drawdown sizing multiplier
func (cm *CapitalManager) ComputeCapitalSummary(
	blockedMargin float64,
	unrealisedPnL float64,
	activeCount int,
	sessionSizeMult float64,
) (market.CapitalSummaryDTO, market.DailyRiskDTO, float64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.ensureCurrentTradingDateLocked()

	mode := cm.settings.Mode
	grossPnL := cm.realisedGrossByMode[mode]
	charges := cm.totalChargesByMode[mode]
	netPnL := cm.realisedNetByMode[mode]

	var netEquity, freeMargin float64
	if mode == "LIVE" {
		freeMargin = math.Max(0, cm.liveFreeMargin)
		if cm.liveGrossBase > 0 {
			netEquity = math.Max(freeMargin+blockedMargin, cm.liveGrossBase+netPnL+math.Min(0, unrealisedPnL))
		} else {
			netEquity = freeMargin + blockedMargin
		}
	} else {
		baseCap := cm.settings.PaperCapital
		netEquity = math.Max(0, baseCap+netPnL+math.Min(0, unrealisedPnL))
		freeMargin = math.Max(0, baseCap+netPnL-blockedMargin)
	}

	ledgerPtr := cm.dailyLedger[mode]
	var ledger market.DailyRiskDTO
	if ledgerPtr != nil {
		ledger = *ledgerPtr
	} else {
		ledger = market.DailyRiskDTO{TradingDate: cm.tradingDate}
	}

	// Dynamic drawdown multiplier: never size up after a loss (fixed or reduced risk)
	drawdownMult := 1.0
	if ledger.ConsecutiveLosses == 1 {
		drawdownMult = 0.85
	} else if ledger.ConsecutiveLosses >= 2 {
		drawdownMult = 0.50
	}
	if sessionSizeMult <= 0 {
		sessionSizeMult = 1.0
	}

	effRiskPct := EffectiveRiskPerTradePct(netEquity, cm.settings.RiskPerTradePct)
	currentRiskBudget := netEquity * effRiskPct * sessionSizeMult * drawdownMult
	if netEquity <= 25000.0 && netEquity >= 500.0 {
		currentRiskBudget = math.Max(currentRiskBudget, netEquity*0.0075)
	}

	buyingPower5x := math.Round(netEquity * 5.0 * 100.0) / 100.0
	deployedExposure := math.Round(blockedMargin * 5.0 * 100.0) / 100.0
	freeBuyingPower5x := math.Round(freeMargin * 5.0 * 100.0) / 100.0

	capDTO := market.CapitalSummaryDTO{
		Mode:                 mode,
		PaperCapital:         cm.settings.PaperCapital,
		NetEquity:            math.Round(netEquity*100.0) / 100.0,
		FreeMargin:           math.Round(freeMargin*100.0) / 100.0,
		BlockedMargin:        math.Round(blockedMargin*100.0) / 100.0,
		BuyingPower5x:        buyingPower5x,
		DeployedExposure:     deployedExposure,
		FreeBuyingPower5x:    freeBuyingPower5x,
		RealisedGrossPnL:     math.Round(grossPnL*100.0) / 100.0,
		TotalCharges:         math.Round(charges*100.0) / 100.0,
		RealisedNetPnL:       math.Round(netPnL*100.0) / 100.0,
		UnrealisedPnL:        math.Round(unrealisedPnL*100.0) / 100.0,
		NetDayPnL:            math.Round((netPnL+unrealisedPnL)*100.0) / 100.0,
		RiskPerTradePct:      effRiskPct,
		CurrentRiskBudget:    math.Round(currentRiskBudget*100.0) / 100.0,
		MaxOpenPositions:     cm.settings.MaxOpenPositions,
		ActivePositionsCount: activeCount,
	}

	return capDTO, ledger, drawdownMult
}

// RecordClosedTrade updates the daily risk ledger and checks all 3 institutional circuit breakers
func (cm *CapitalManager) RecordClosedTrade(mode string, grossPnL, charges, netPnL float64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.ensureCurrentTradingDateLocked()

	cm.realisedGrossByMode[mode] += grossPnL
	cm.totalChargesByMode[mode] += charges
	cm.realisedNetByMode[mode] += netPnL

	ledger, ok := cm.dailyLedger[mode]
	if !ok {
		ledger = &market.DailyRiskDTO{TradingDate: cm.tradingDate}
		cm.dailyLedger[mode] = ledger
	}

	ledger.TradesTaken++
	if netPnL >= 0 {
		ledger.Wins++
		ledger.ConsecutiveLosses = 0
	} else {
		ledger.Losses++
		ledger.ConsecutiveLosses++
	}
	if ledger.TradesTaken > 0 {
		ledger.WinRatePct = math.Round((float64(ledger.Wins)/float64(ledger.TradesTaken))*1000.0) / 10.0
	}

	cumNet := cm.realisedNetByMode[mode]
	if cumNet > ledger.PeakNetPnL {
		ledger.PeakNetPnL = cumNet
	}

	cm.evaluateCircuitBreakersLocked(mode, ledger, cumNet)
	cm.persistDailyLedgerLocked(mode, ledger, cm.realisedGrossByMode[mode], cm.totalChargesByMode[mode], cumNet)
}

// SyncUniverseLeverages queries Zerodha POST /margins/orders in 50-stock chunks using LIMIT @ ₹1000
// (which works even at 08:30 AM pre-open when LTP is 0) and caches exact MIS leverages in SQLite.
func (cm *CapitalManager) SyncUniverseLeverages(ctx context.Context) {
	if cm.kiteService == nil || cm.db == nil {
		return
	}
	client := cm.kiteService.GetClient()
	session := cm.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return
	}

	rows, err := cm.db.Query(`SELECT instrument_token, tradingsymbol FROM scanned_stocks ORDER BY tradingsymbol ASC`)
	if err != nil {
		return
	}
	defer rows.Close()
	type item struct {
		token  uint32
		symbol string
	}
	var list []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.token, &it.symbol); err == nil && it.symbol != "" {
			list = append(list, it)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed iterating scanned stocks for leverage sync", "error", err)
	}

	if len(list) == 0 {
		return
	}

	updatedCount := 0
	const batchSize = 50
	for start := 0; start < len(list); start += batchSize {
		select {
		case <-ctx.Done():
			return
		default:
		}
		end := min(len(list), start+batchSize)
		chunk := list[start:end]

		params := make([]kiteconnect.OrderMarginParam, 0, len(chunk))
		for _, st := range chunk {
			params = append(params, kiteconnect.OrderMarginParam{
				Exchange:        ExchangeNSE,
				Tradingsymbol:   st.symbol,
				TransactionType: "BUY",
				Variety:         DefaultVariety,
				Product:         ProductMIS,
				OrderType:       "LIMIT",
				Quantity:        1,
				Price:           1000.0,
			})
		}

		margins, mErr := client.GetOrderMargins(kiteconnect.GetMarginParams{
			OrderParams: params,
			Compact:     true,
		})
		if mErr != nil {
			slog.Warn("GetOrderMargins batch failed, keeping cached SQLite MIS leverages", "error", mErr)
			return
		}

		tx, txErr := cm.db.Begin()
		if txErr == nil {
			stmt, sErr := tx.Prepare(`UPDATE scanned_stocks SET mis_leverage = ? WHERE tradingsymbol = ?`)
			if sErr == nil {
				for _, m := range margins {
					if m.Total > 0 && m.TradingSymbol != "" {
						lev := math.Round((1000.0/m.Total)*100.0) / 100.0
						lev = math.Max(1.0, math.Min(5.0, lev))
						_, _ = stmt.Exec(lev, m.TradingSymbol)
						updatedCount++
					}
				}
				stmt.Close()
			}
			_ = tx.Commit()
		}

		time.Sleep(350 * time.Millisecond)
	}

	if updatedCount > 0 {
		slog.Info("Synchronized live Zerodha MIS leverages for stock universe", "updated_stocks", updatedCount)
	}
}
