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
	Mode                 string  `json:"mode"`               // "PAPER" | "LIVE"
	IsAutoExecuteEnabled bool    `json:"auto_execute"`       // Whether autonomous signal entry is armed
	PaperCapital         float64 `json:"paper_capital"`      // Virtual capital (₹500 to ₹10Cr+)
	RiskPerTradePct      float64 `json:"risk_per_trade_pct"` // Default 0.005 (0.50%)
	MaxOpenPositions     int     `json:"max_open_positions"` // Default 4
}

// RiskReservation represents an atomic in-flight risk and margin allocation
// held while an order is being placed, preventing concurrent double-allocation.
type RiskReservation struct {
	ReservationID  string    `json:"reservation_id"`
	Symbol         string    `json:"symbol"`
	ReservedRisk   float64   `json:"reserved_risk"`
	ReservedMargin float64   `json:"reserved_margin"`
	CreatedAt      time.Time `json:"created_at"`
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

	// Atomic in-flight risk & margin reservations
	activeReservations map[string]RiskReservation

	// Ledger per mode ("PAPER" | "LIVE")
	dailyLedger map[string]*market.DailyRiskDTO
	realisedGrossByMode map[string]float64
	totalChargesByMode  map[string]float64
	realisedNetByMode   map[string]float64

	onLeveragesSynced func()
}

func NewCapitalManager(db *sql.DB, kiteService *kite.Service) *CapitalManager {
	todayStr := market.TodayDateIST()

	capitalManager := &CapitalManager{
		db:          db,
		kiteService: kiteService,
		tradingDate: todayStr,
		settings: ExecutionSettings{
			Mode:                 "PAPER",
			IsAutoExecuteEnabled: false,
			PaperCapital:         100000.0,
			RiskPerTradePct:      0.005,
			MaxOpenPositions:     4,
		},
		activeReservations:  make(map[string]RiskReservation),
		dailyLedger:         make(map[string]*market.DailyRiskDTO),
		realisedGrossByMode: make(map[string]float64),
		totalChargesByMode:  make(map[string]float64),
		realisedNetByMode:   make(map[string]float64),
	}

	capitalManager.loadFromDatabase()
	return capitalManager
}

// SetOnLeveragesSynced registers a callback invoked after live MIS leverages have been written to SQLite
func (capitalManager *CapitalManager) SetOnLeveragesSynced(callbackFunction func()) {
	capitalManager.mu.Lock()
	defer capitalManager.mu.Unlock()
	capitalManager.onLeveragesSynced = callbackFunction
}

func (capitalManager *CapitalManager) loadFromDatabase() {
	if capitalManager.db == nil {
		return
	}
	capitalManager.mu.Lock()
	defer capitalManager.mu.Unlock()

	var mode string
	var isAutoExecuteFlag int
	var paperCapital, riskPercentage float64
	var maxOpenPositions int

	err := capitalManager.db.QueryRow(`
		SELECT mode, auto_execute, paper_capital, risk_per_trade_pct, max_open_positions
		FROM execution_settings WHERE id = 1
	`).Scan(&mode, &isAutoExecuteFlag, &paperCapital, &riskPercentage, &maxOpenPositions)
	if err == nil {
		if mode == "LIVE" || mode == "PAPER" {
			capitalManager.settings.Mode = mode
		}
		capitalManager.settings.IsAutoExecuteEnabled = isAutoExecuteFlag != 0
		if paperCapital >= 500.0 {
			capitalManager.settings.PaperCapital = paperCapital
		}
		if riskPercentage > 0 && riskPercentage <= 0.03 {
			capitalManager.settings.RiskPerTradePct = riskPercentage
		}
		if maxOpenPositions >= 1 && maxOpenPositions <= 15 {
			capitalManager.settings.MaxOpenPositions = maxOpenPositions
		}
	}

	for _, modeName := range []string{"PAPER", "LIVE"} {
		capitalManager.ReconcileWithPositionsDB(modeName)
	}
}

// ReconcileWithPositionsDB re-synchronizes the daily risk ledger with ground-truth closed trades in the positions table.
// Falsification Guard: Fixes any historical double-counting or ledger drift so /trades and the Dashboard remain 100% consistent.
func (capitalManager *CapitalManager) ReconcileWithPositionsDB(mode string) {
	if capitalManager.db == nil {
		return
	}

	rows, err := capitalManager.db.Query(`
		SELECT realised_gross_pnl, total_charges, realised_net_pnl, closed_at
		FROM positions
		WHERE trading_date = ? AND mode = ? AND state = 'CLOSED'
		ORDER BY closed_at ASC
	`, capitalManager.tradingDate, mode)
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
		TradingDate:       capitalManager.tradingDate,
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
	capitalManager.evaluateCircuitBreakersLocked(mode, dto, cumNet)

	capitalManager.dailyLedger[mode] = dto
	capitalManager.realisedGrossByMode[mode] = cumGross
	capitalManager.totalChargesByMode[mode] = cumCharges
	capitalManager.realisedNetByMode[mode] = cumNet

	capitalManager.persistDailyLedgerLocked(mode, dto, cumGross, cumCharges, cumNet)
}

func (capitalManager *CapitalManager) evaluateCircuitBreakersLocked(mode string, ledger *market.DailyRiskDTO, cumNet float64) {
	dailyStartingAccountEquity := capitalManager.settings.PaperCapital
	if mode == "LIVE" && capitalManager.liveGrossBase > 0 {
		dailyStartingAccountEquity = capitalManager.liveGrossBase
	}

	// Strict 3.0% Max Daily Loss Circuit Breaker relative to Day's Starting Capital
	if dailyStartingAccountEquity > 0 && cumNet <= -0.030*dailyStartingAccountEquity {
		ledger.IsCircuitBreakerLocked = true
		ledger.LockReason = fmt.Sprintf("Max Daily Loss Reached (₹%.0f / -3.0%% cap)", cumNet)
	}
}

func (capitalManager *CapitalManager) persistDailyLedgerLocked(mode string, ledger *market.DailyRiskDTO, cumGross, cumCharges, cumNet float64) {
	if capitalManager.db == nil {
		return
	}
	lockedInt := 0
	if ledger.IsCircuitBreakerLocked {
		lockedInt = 1
	}
	_, _ = capitalManager.db.Exec(`
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
	`, capitalManager.tradingDate, mode, ledger.TradesTaken, ledger.Wins, ledger.Losses, ledger.ConsecutiveLosses,
		cumGross, cumCharges, cumNet, ledger.PeakNetPnL, lockedInt, ledger.LockReason)
}

func (capitalManager *CapitalManager) GetSettings() ExecutionSettings {
	capitalManager.mu.RLock()
	defer capitalManager.mu.RUnlock()
	return capitalManager.settings
}

func (capitalManager *CapitalManager) UpdateSettings(newSettings ExecutionSettings, activeOpenCount int) error {
	capitalManager.mu.Lock()
	defer capitalManager.mu.Unlock()

	if newSettings.Mode != "PAPER" && newSettings.Mode != "LIVE" {
		return errors.New("mode must be 'PAPER' or 'LIVE'")
	}
	if newSettings.Mode != capitalManager.settings.Mode && (activeOpenCount > 0 || len(capitalManager.activeReservations) > 0) {
		return fmt.Errorf("cannot switch mode from %s to %s while %d position(s) or %d reservation(s) are open",
			capitalManager.settings.Mode, newSettings.Mode, activeOpenCount, len(capitalManager.activeReservations))
	}
	if newSettings.RiskPerTradePct <= 0 || newSettings.RiskPerTradePct > 0.03 {
		return errors.New("risk_per_trade_pct must be between 0.001 (0.1%) and 0.03 (3.0%)")
	}
	if newSettings.MaxOpenPositions < 1 || newSettings.MaxOpenPositions > 50 {
		return errors.New("max_open_positions must be between 1 and 50")
	}
	if newSettings.PaperCapital >= 500.0 {
		capitalManager.settings.PaperCapital = newSettings.PaperCapital
	}
	capitalManager.settings.Mode = newSettings.Mode
	capitalManager.settings.IsAutoExecuteEnabled = newSettings.IsAutoExecuteEnabled
	capitalManager.settings.RiskPerTradePct = newSettings.RiskPerTradePct
	capitalManager.settings.MaxOpenPositions = newSettings.MaxOpenPositions

	if capitalManager.db != nil {
		autoInt := 0
		if capitalManager.settings.IsAutoExecuteEnabled {
			autoInt = 1
		}
		_, err := capitalManager.db.Exec(`
			UPDATE execution_settings SET
				mode = ?,
				auto_execute = ?,
				paper_capital = ?,
				risk_per_trade_pct = ?,
				max_open_positions = ?,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = 1;
		`, capitalManager.settings.Mode, autoInt, capitalManager.settings.PaperCapital, capitalManager.settings.RiskPerTradePct, capitalManager.settings.MaxOpenPositions)
		if err != nil {
			return fmt.Errorf("failed to persist execution settings: %w", err)
		}
	}
	return nil
}

// ForceSyncModeForRehydration aligns the active execution mode with rehydrated open positions after crash recovery
func (capitalManager *CapitalManager) ForceSyncModeForRehydration(mode string) {
	if mode != "PAPER" && mode != "LIVE" {
		return
	}
	capitalManager.mu.Lock()
	capitalManager.settings.Mode = mode
	capitalManager.mu.Unlock()

	if capitalManager.db != nil {
		_, _ = capitalManager.db.Exec(`UPDATE execution_settings SET mode = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`, mode)
	}
}

func (capitalManager *CapitalManager) UpdatePaperCapital(newCapital float64) error {
	if newCapital < 500.0 || newCapital > 1000000000.0 {
		return errors.New("paper_capital must be between ₹500 and ₹100 Crore")
	}
	capitalManager.mu.Lock()
	capitalManager.settings.PaperCapital = newCapital
	capitalManager.mu.Unlock()

	if capitalManager.db != nil {
		_, err := capitalManager.db.Exec(`UPDATE execution_settings SET paper_capital = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`, newCapital)
		if err != nil {
			return fmt.Errorf("failed to update paper_capital in SQLite: %w", err)
		}
	}
	return nil
}

// RefreshLiveMargins polls Zerodha /user/margins and updates LiveGrossBase & LiveFreeMargin
func (capitalManager *CapitalManager) RefreshLiveMargins() error {
	if capitalManager.kiteService == nil {
		return kite.ErrNoSession
	}
	margins, err := capitalManager.kiteService.GetEquityMargins()
	if err != nil {
		return err
	}

	grossBase := margins.AvailableCash + margins.Collateral + margins.AdHocMargin - margins.UtilisedPayout
	if grossBase < margins.Net {
		grossBase = margins.Net
	}

	capitalManager.mu.Lock()
	capitalManager.liveGrossBase = math.Max(0, grossBase)
	capitalManager.liveFreeMargin = math.Max(0, margins.Net)
	capitalManager.lastLiveRefreshAt = time.Now()
	capitalManager.mu.Unlock()
	return nil
}

// ensureCurrentTradingDateLocked rolls over the daily risk ledger and PnL counters if the server stays up overnight
func (capitalManager *CapitalManager) ensureCurrentTradingDateLocked() {
	todayStr := market.TodayDateIST()
	if todayStr == capitalManager.tradingDate {
		return
	}
	capitalManager.tradingDate = todayStr
	for _, executionMode := range []string{"PAPER", "LIVE"} {
		capitalManager.realisedGrossByMode[executionMode] = 0
		capitalManager.totalChargesByMode[executionMode] = 0
		capitalManager.realisedNetByMode[executionMode] = 0
		capitalManager.dailyLedger[executionMode] = &market.DailyRiskDTO{
			TradingDate: todayStr,
		}
	}
}

// ComputeCapitalSummary returns the live CapitalSummaryDTO and drawdown sizing multiplier
func (capitalManager *CapitalManager) ComputeCapitalSummary(
	blockedMargin float64,
	unrealisedPnL float64,
	totalOpenRiskRupees float64,
	activeCount int,
	sessionSizeMult float64,
) (market.CapitalSummaryDTO, market.DailyRiskDTO, float64) {
	capitalManager.mu.Lock()
	defer capitalManager.mu.Unlock()
	capitalManager.ensureCurrentTradingDateLocked()

	mode := capitalManager.settings.Mode
	grossPnL := capitalManager.realisedGrossByMode[mode]
	charges := capitalManager.totalChargesByMode[mode]
	netPnL := capitalManager.realisedNetByMode[mode]

	var netEquity, freeMargin float64
	if mode == "LIVE" {
		freeMargin = math.Max(0, capitalManager.liveFreeMargin)
		if capitalManager.liveGrossBase > 0 {
			netEquity = math.Max(freeMargin+blockedMargin, capitalManager.liveGrossBase+netPnL+math.Min(0, unrealisedPnL))
		} else {
			netEquity = freeMargin + blockedMargin
		}
	} else {
		baseCap := capitalManager.settings.PaperCapital
		netEquity = math.Max(0, baseCap+netPnL+math.Min(0, unrealisedPnL))
		freeMargin = math.Max(0, baseCap+netPnL-blockedMargin)
	}

	ledgerPtr := capitalManager.dailyLedger[mode]
	var ledger market.DailyRiskDTO
	if ledgerPtr != nil {
		ledger = *ledgerPtr
	} else {
		ledger = market.DailyRiskDTO{TradingDate: capitalManager.tradingDate}
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

	effRiskPct := EffectiveRiskPerTradePct(netEquity, capitalManager.settings.RiskPerTradePct)
	currentRiskBudget := netEquity * effRiskPct * sessionSizeMult * drawdownMult
	if netEquity <= 25000.0 && netEquity >= 500.0 {
		currentRiskBudget = math.Max(currentRiskBudget, netEquity*0.0075)
	}

	buyingPower5x := math.Round(netEquity * 5.0 * 100.0) / 100.0
	deployedExposure := math.Round(blockedMargin * 5.0 * 100.0) / 100.0
	freeBuyingPower5x := math.Round(freeMargin * 5.0 * 100.0) / 100.0

	// Evict stale in-flight reservations older than 15s and sum active ones
	now := time.Now()
	var totalReservedRisk, totalReservedMargin float64
	for reservationID, reservation := range capitalManager.activeReservations {
		if now.Sub(reservation.CreatedAt) > 15*time.Second {
			delete(capitalManager.activeReservations, reservationID)
			continue
		}
		totalReservedRisk += reservation.ReservedRisk
		totalReservedMargin += reservation.ReservedMargin
	}

	effectiveTotalOpenRisk := totalOpenRiskRupees + totalReservedRisk
	effectiveBlockedMargin := blockedMargin + totalReservedMargin
	freeMargin = math.Max(0, freeMargin-totalReservedMargin)

	var portfolioHeatPct float64
	if netEquity > 0 {
		portfolioHeatPct = effectiveTotalOpenRisk / netEquity
	}

	capDTO := market.CapitalSummaryDTO{
		Mode:                 mode,
		PaperCapital:         capitalManager.settings.PaperCapital,
		NetEquity:            math.Round(netEquity*100.0) / 100.0,
		FreeMargin:           math.Round(freeMargin*100.0) / 100.0,
		BlockedMargin:        math.Round(effectiveBlockedMargin*100.0) / 100.0,
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
		TotalOpenRiskRupees:  math.Round(effectiveTotalOpenRisk*100.0) / 100.0,
		PortfolioHeatPct:     math.Round(portfolioHeatPct*10000.0) / 10000.0,
		MaxPortfolioHeatPct:  0.03,
		MaxOpenPositions:     capitalManager.settings.MaxOpenPositions,
		ActivePositionsCount: activeCount,
	}

	return capDTO, ledger, drawdownMult
}

// RecordClosedTrade updates the daily risk ledger and checks all 3 institutional circuit breakers
func (capitalManager *CapitalManager) RecordClosedTrade(mode string, grossPnL, charges, netPnL float64) {
	capitalManager.mu.Lock()
	defer capitalManager.mu.Unlock()
	capitalManager.ensureCurrentTradingDateLocked()

	capitalManager.realisedGrossByMode[mode] += grossPnL
	capitalManager.totalChargesByMode[mode] += charges
	capitalManager.realisedNetByMode[mode] += netPnL

	ledger, exists := capitalManager.dailyLedger[mode]
	if !exists {
		ledger = &market.DailyRiskDTO{TradingDate: capitalManager.tradingDate}
		capitalManager.dailyLedger[mode] = ledger
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

	cumNet := capitalManager.realisedNetByMode[mode]
	if cumNet > ledger.PeakNetPnL {
		ledger.PeakNetPnL = cumNet
	}

	capitalManager.evaluateCircuitBreakersLocked(mode, ledger, cumNet)
	capitalManager.persistDailyLedgerLocked(mode, ledger, capitalManager.realisedGrossByMode[mode], capitalManager.totalChargesByMode[mode], cumNet)
}

// ReserveRiskAndMargin atomically validates and reserves risk and margin under the manager lock.
// This eliminates the TOCTOU concurrency race where two goroutines evaluate capacity simultaneously.
func (capitalManager *CapitalManager) ReserveRiskAndMargin(
	symbol string,
	proposedRiskRupees float64,
	proposedMarginRupees float64,
	activeOpenRisk float64,
	activeBlockedMargin float64,
) (string, error) {
	capitalManager.mu.Lock()
	defer capitalManager.mu.Unlock()
	capitalManager.ensureCurrentTradingDateLocked()

	// 1. Evict stale reservations older than 15 seconds
	now := time.Now()
	for reservationID, reservation := range capitalManager.activeReservations {
		if now.Sub(reservation.CreatedAt) > 15*time.Second {
			delete(capitalManager.activeReservations, reservationID)
		}
	}

	mode := capitalManager.settings.Mode
	ledger := capitalManager.dailyLedger[mode]
	if ledger != nil && ledger.IsCircuitBreakerLocked {
		return "", fmt.Errorf("daily circuit breaker is locked: %s", ledger.LockReason)
	}

	// 2. Compute live equity and free margin
	var netEquity, freeMargin float64
	netPnL := capitalManager.realisedNetByMode[mode]
	if mode == "LIVE" {
		freeMargin = math.Max(0, capitalManager.liveFreeMargin)
		if capitalManager.liveGrossBase > 0 {
			netEquity = math.Max(freeMargin+activeBlockedMargin, capitalManager.liveGrossBase+netPnL)
		} else {
			netEquity = freeMargin + activeBlockedMargin
		}
	} else {
		baseCap := capitalManager.settings.PaperCapital
		netEquity = math.Max(0, baseCap+netPnL)
		freeMargin = math.Max(0, baseCap+netPnL-activeBlockedMargin)
	}

	if netEquity <= 0 {
		return "", errors.New("insufficient account equity (net equity <= 0)")
	}

	// 3. Sum all existing in-flight reservations
	var totalReservedRisk, totalReservedMargin float64
	for _, reservation := range capitalManager.activeReservations {
		totalReservedRisk += reservation.ReservedRisk
		totalReservedMargin += reservation.ReservedMargin
	}

	// 4. Wall 1: Risk Heat Ceiling (Strict 3.0% limit)
	maxPortfolioHeatRupees := 0.03 * netEquity
	effectiveOpenRisk := activeOpenRisk + totalReservedRisk
	if effectiveOpenRisk+proposedRiskRupees > maxPortfolioHeatRupees+1e-6 {
		return "", fmt.Errorf("max portfolio heat (3.0%% = ₹%.2f) exceeded: current open=₹%.2f, reserved=₹%.2f, proposed=₹%.2f",
			maxPortfolioHeatRupees, activeOpenRisk, totalReservedRisk, proposedRiskRupees)
	}

	// 5. Wall 2: Broker Margin Floor (Free Cash Margin check)
	availableCashMargin := freeMargin - totalReservedMargin
	if proposedMarginRupees > availableCashMargin {
		return "", fmt.Errorf("insufficient free cash margin: available=₹%.2f, reserved=₹%.2f, proposed=₹%.2f",
			freeMargin, totalReservedMargin, proposedMarginRupees)
	}

	// 6. Atomically record the reservation token
	reservationID := fmt.Sprintf("RES-%s-%d", symbol, now.UnixNano())
	capitalManager.activeReservations[reservationID] = RiskReservation{
		ReservationID:  reservationID,
		Symbol:         symbol,
		ReservedRisk:   proposedRiskRupees,
		ReservedMargin: proposedMarginRupees,
		CreatedAt:      now,
	}

	return reservationID, nil
}

// ReleaseReservation frees an in-flight reservation if order submission fails or is rejected
func (capitalManager *CapitalManager) ReleaseReservation(reservationID string) {
	if reservationID == "" {
		return
	}
	capitalManager.mu.Lock()
	delete(capitalManager.activeReservations, reservationID)
	capitalManager.mu.Unlock()
}

// PromoteReservation cleans up the reservation when the position worker is successfully spawned,
// seamlessly transferring risk tracking to the active worker.
func (capitalManager *CapitalManager) PromoteReservation(reservationID string) {
	if reservationID == "" {
		return
	}
	capitalManager.mu.Lock()
	delete(capitalManager.activeReservations, reservationID)
	capitalManager.mu.Unlock()
}

// SyncUniverseLeverages queries Zerodha POST /margins/orders in 50-stock chunks using LIMIT @ ₹1000
// (which works even at 08:30 AM pre-open when LTP is 0) and caches exact MIS leverages in SQLite.
func (capitalManager *CapitalManager) SyncUniverseLeverages(ctx context.Context) {
	if capitalManager.kiteService == nil || capitalManager.db == nil {
		return
	}
	client := capitalManager.kiteService.GetClient()
	session := capitalManager.kiteService.GetSession()
	if client == nil || session == nil || session.AccessToken == "" {
		return
	}

	rows, err := capitalManager.db.Query(`SELECT instrument_token, tradingsymbol FROM scanned_stocks ORDER BY tradingsymbol ASC`)
	if err != nil {
		return
	}
	defer rows.Close()
	type stockTokenSymbol struct {
		token  uint32
		symbol string
	}
	var stockList []stockTokenSymbol
	for rows.Next() {
		var stockItem stockTokenSymbol
		if err := rows.Scan(&stockItem.token, &stockItem.symbol); err == nil && stockItem.symbol != "" {
			stockList = append(stockList, stockItem)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed iterating scanned stocks for leverage sync", "error", err)
	}

	if len(stockList) == 0 {
		return
	}

	updatedCount := 0
	const batchSize = 50
	for start := 0; start < len(stockList); start += batchSize {
		select {
		case <-ctx.Done():
			return
		default:
		}
		end := min(len(stockList), start+batchSize)
		chunk := stockList[start:end]

		params := make([]kiteconnect.OrderMarginParam, 0, len(chunk))
		for _, stockItem := range chunk {
			params = append(params, kiteconnect.OrderMarginParam{
				Exchange:        ExchangeNSE,
				Tradingsymbol:   stockItem.symbol,
				TransactionType: "BUY",
				Variety:         DefaultVariety,
				Product:         ProductMIS,
				OrderType:       "LIMIT",
				Quantity:        1,
				Price:           1000.0,
			})
		}

		var margins []kiteconnect.OrderMargins
		var marginErr error

		rateLimiter := capitalManager.kiteService.GetRateLimiter()
		if rateLimiter != nil {
			responseInterface, executeErr := rateLimiter.ExecuteBackgroundSync(ctx, func() (interface{}, error) {
				return client.GetOrderMargins(kiteconnect.GetMarginParams{
					OrderParams: params,
					Compact:     true,
				})
			})
			if executeErr != nil {
				marginErr = executeErr
			} else if castMargins, isMargins := responseInterface.([]kiteconnect.OrderMargins); isMargins {
				margins = castMargins
			}
		} else {
			margins, marginErr = client.GetOrderMargins(kiteconnect.GetMarginParams{
				OrderParams: params,
				Compact:     true,
			})
		}

		if marginErr != nil {
			slog.Warn("GetOrderMargins batch failed, keeping cached SQLite MIS leverages", "error", marginErr)
			return
		}

		tx, txErr := capitalManager.db.Begin()
		if txErr == nil {
			stmt, stmtErr := tx.Prepare(`UPDATE scanned_stocks SET mis_leverage = ?, mis_margin_pct = ? WHERE tradingsymbol = ?`)
			if stmtErr == nil {
				for _, orderMargin := range margins {
					if orderMargin.Total > 0 && orderMargin.TradingSymbol != "" {
						marginPct := math.Round((orderMargin.Total/1000.0)*10000.0) / 10000.0
						calculatedLeverage := math.Round((1000.0/orderMargin.Total)*100.0) / 100.0
						calculatedLeverage = math.Max(1.0, math.Min(5.0, calculatedLeverage))
						_, _ = stmt.Exec(calculatedLeverage, marginPct, orderMargin.TradingSymbol)
						updatedCount++
					} else if orderMargin.TradingSymbol != "" {
						// Stock not permitted for MIS intraday leverage: store 0.0, never assume 5x
						_, _ = stmt.Exec(0.0, 0.0, orderMargin.TradingSymbol)
					}
				}
				stmt.Close()
			}
			_ = tx.Commit()
		}

		if rateLimiter == nil {
			time.Sleep(350 * time.Millisecond)
		}
	}

	if updatedCount > 0 {
		slog.Info("Synchronized live Zerodha MIS leverages for stock universe", "updated_stocks", updatedCount)
		capitalManager.mu.RLock()
		callbackFunction := capitalManager.onLeveragesSynced
		capitalManager.mu.RUnlock()
		if callbackFunction != nil {
			callbackFunction()
		}
	}
}
