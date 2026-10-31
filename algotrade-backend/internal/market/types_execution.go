package market

// =========================================================================
// Autonomous Execution Cockpit DTOs
// =========================================================================

// MarketSessionItemDTO represents a single global exchange clock
type MarketSessionItemDTO struct {
	ID          string `json:"id"`           // "NSE", "TOKYO", "SHANGHAI", "FRANKFURT", "LONDON", "NEW_YORK"
	Name        string `json:"name"`         // e.g. "London (LSE)"
	Timezone    string `json:"timezone"`     // IANA timezone
	LocalTime   string `json:"local_time"`   // "08:15:22"
	Status      string `json:"status"`       // "OPEN", "PRE_OPEN", "LUNCH", "CLOSED"
	StatusLabel string `json:"status_label"` // e.g. "Open • Closes in 4h 12m" or "Opens in 18m"
	IsOpen      bool   `json:"is_open"`
}

// GlobalSessionClockDTO provides DST-aware global clocks and the active NSE intraday playbook
type GlobalSessionClockDTO struct {
	ISTTime            string                 `json:"ist_time"`
	Phase              string                 `json:"phase"`                // "OPENING_NOISE_BLACKOUT", "MORNING_DISCOVERY_DRIVE", "MIDDAY_COMPRESSION", "EUROPE_OVERLAP_EXPANSION", "EXIT_ONLY_LOCKOUT", "AUTO_SQUAREOFF_SWEEP", "MARKET_CLOSED"
	PhaseLabel         string                 `json:"phase_label"`          // Human-readable playbook badge
	PlaybookNote       string                 `json:"playbook_note"`        // Actionable desk note
	IsTapeOverrideActive bool                 `json:"tape_override_active"` // True when strong domestic breadth/RVOL overrides midday lull
	IsNewEntryAllowed  bool                   `json:"allow_new_entries"`
	MinBullishScore    float64                `json:"min_bullish_score"`
	MinRVOL            float64                `json:"min_rvol"`
	SizeMultiplier     float64                `json:"size_multiplier"`
	Clocks             []MarketSessionItemDTO `json:"clocks"`
}

// CapitalSummaryDTO provides live Zerodha or Paper capital & margin accounting
type CapitalSummaryDTO struct {
	Mode                 string  `json:"mode"`                   // "PAPER" | "LIVE"
	PaperCapital         float64 `json:"paper_capital"`          // Editable virtual capital (₹500 to ₹10Cr+)
	NetEquity            float64 `json:"net_equity"`             // Effective capital for risk budgeting
	FreeMargin           float64 `json:"free_margin"`            // Unencumbered cash available for new positions
	BlockedMargin        float64 `json:"blocked_margin"`         // Margin utilized by active positions
	BuyingPower5x        float64 `json:"buying_power_5x"`        // 5x MIS Intraday Buying Power (NetEquity * 5.0)
	DeployedExposure     float64 `json:"deployed_exposure"`      // Total stock notional value active (BlockedMargin * 5.0)
	FreeBuyingPower5x    float64 `json:"free_buying_power_5x"`   // Remaining 5x MIS Buying Power (FreeMargin * 5.0)
	RealisedGrossPnL     float64 `json:"realised_gross_pnl"`     // Today's closed trade PnL before taxes
	TotalCharges         float64 `json:"total_charges"`          // Brokerage + STT + Exchange + SEBI + Stamp + GST
	RealisedNetPnL       float64 `json:"realised_net_pnl"`       // Net banked profit/loss after all charges
	UnrealisedPnL        float64 `json:"unrealised_pnl"`         // Open mark-to-market PnL
	NetDayPnL            float64 `json:"net_day_pnl"`            // RealisedNetPnL + UnrealisedPnL
	RiskPerTradePct      float64 `json:"risk_per_trade_pct"`     // e.g. 0.005 (0.50%)
	CurrentRiskBudget    float64 `json:"current_risk_budget"`    // Dynamic ₹ risk budget for next trade
	TotalOpenRiskRupees  float64 `json:"total_open_risk_rupees"` // Sum of open downside risk across active trades
	PortfolioHeatPct     float64 `json:"portfolio_heat_pct"`     // TotalOpenRiskRupees / NetEquity (e.g. 0.015 for 1.5%)
	MaxPortfolioHeatPct  float64 `json:"max_portfolio_heat_pct"` // Maximum allowed portfolio heat (0.03 = 3.0%)
	MaxOpenPositions     int     `json:"max_open_positions"`     // Legacy slot limit / secondary safety ceiling
	ActivePositionsCount int     `json:"active_positions_count"` // Currently open positions
}

// DailyRiskDTO represents the daily circuit breaker and win/loss ledger
type DailyRiskDTO struct {
	TradingDate          string  `json:"trading_date"`
	TradesTaken          int     `json:"trades_taken"`
	Wins                 int     `json:"wins"`
	Losses               int     `json:"losses"`
	WinRatePct           float64 `json:"win_rate_pct"`
	ConsecutiveLosses    int     `json:"consecutive_losses"`
	PeakNetPnL           float64 `json:"peak_net_pnl"`
	IsCircuitBreakerLocked bool `json:"circuit_breaker_locked"`
	LockReason           string  `json:"lock_reason,omitempty"`
}

// ActivePositionDTO represents a single open or recently closed intraday position
type ActivePositionDTO struct {
	PositionID       string  `json:"position_id"`
	Mode             string  `json:"mode"`
	Symbol           string  `json:"symbol"`
	InstrumentToken  uint32  `json:"instrument_token"`
	State            string  `json:"state"`        // "ENTRY_PLACED", "PROTECTED_ACTIVE", "SCALE_OUT_PENDING", "RUNNER_ACTIVE", "SHADOW_STOP_ACTIVE", "EMERGENCY_FLATTENING", "CLOSED"
	PatternType      string  `json:"pattern_type"` // "VWAP_PULLBACK_RECLAIM" | "FAILED_BREAKDOWN_SPRING" | "ORB_RETEST_RECLAIM"
	EntryPrice       float64 `json:"entry_price"`
	ExitPrice        float64 `json:"exit_price,omitempty"`
	LTP              float64 `json:"ltp"`
	InitialStop      float64 `json:"initial_stop"`
	CurrentStop      float64 `json:"current_stop"`
	ShadowStop       float64 `json:"shadow_stop"`
	BreakevenStop    float64 `json:"breakeven_stop"`
	BaseBuffer       float64 `json:"base_buffer"`
	HighWaterMark    float64 `json:"high_water_mark"`
	LowWaterMark     float64 `json:"low_water_mark,omitempty"`
	OneR             float64 `json:"one_r"`
	RMultiple        float64 `json:"r_multiple"`
	MFERMultiple     float64 `json:"mfe_r_multiple"`
	MAERMultiple     float64 `json:"mae_r_multiple,omitempty"`
	InitialQty       int     `json:"initial_qty"`
	RemainingQty     int     `json:"remaining_qty"`
	ModCount         int     `json:"mod_count"`
	MaxMods          int     `json:"max_mods"` // 25
	CurrentGear      int     `json:"current_gear"`
	GearLabel        string  `json:"gear_label"`
	HasTranche1Completed bool `json:"tranche1_done"`
	UnrealisedPnL    float64 `json:"unrealised_pnl"`
	RealisedGrossPnL float64 `json:"realised_gross_pnl"`
	EstimatedCharges float64 `json:"estimated_charges"`
	NetTotalPnL      float64 `json:"net_total_pnl"`
	ExitReason       string  `json:"exit_reason,omitempty"`
	OpenedAt         string  `json:"opened_at"`
	OpenedAtUnix     int64   `json:"opened_at_unix,omitempty"`
	ClosedAt         string  `json:"closed_at,omitempty"`
}

// CandidateGateDiagnosticDTO provides live 6-stage gate evaluation telemetry for the Top 5 ranked stocks
type CandidateGateDiagnosticDTO struct {
	Rank           int     `json:"rank"`
	Symbol         string  `json:"symbol"`
	Sector         string  `json:"sector"`
	LTP            float64 `json:"ltp"`
	BullishScore   float64 `json:"bullish_score"`
	CompositeRVOL  float64 `json:"composite_rvol"`
	EV             float64 `json:"ev"`
	IsRegimeValid  bool    `json:"regime_ok"`
	IsScoreValid   bool    `json:"score_ok"`
	IsRVOLValid    bool    `json:"rvol_ok"`
	IsBookQualityValid bool `json:"book_quality_ok"`
	PatternMatched string  `json:"pattern_matched"` // "VWAP_PULLBACK_RECLAIM" | "FAILED_BREAKDOWN_SPRING" | "ORB_RETEST_RECLAIM" | ""
	IsSizingApproved bool `json:"sizing_approved"`
	OverallStatus  string  `json:"overall_status"`  // "READY" | "ACTIVE_OR_COOLDOWN" | "BLOCKED"
	BlockingReason string  `json:"blocking_reason"` // Exact human-readable gate that held back or approved the stock
}

// ExecutionStateDTO is the complete Phase 2 telemetry payload
type ExecutionStateDTO struct {
	Mode              string                       `json:"mode"` // "PAPER" | "LIVE"
	IsAutoExecuteEnabled bool                   `json:"auto_execute"`
	IsMacroShockActive  bool                     `json:"macro_shock_active"`
	GateStatus        string                       `json:"gate_status"` // e.g. "ARMED_SCANNING", "PAUSED_MANUAL", "LOCKED_BEAR_REGIME", "LOCKED_CIRCUIT_BREAKER"
	GateReason        string                       `json:"gate_reason"`
	Capital           CapitalSummaryDTO            `json:"capital"`
	DailyRisk         DailyRiskDTO                 `json:"daily_risk"`
	SessionClock      GlobalSessionClockDTO        `json:"session_clock"`
	ActivePositions   []ActivePositionDTO          `json:"active_positions"`
	ClosedPositions   []ActivePositionDTO          `json:"closed_positions"`
	TopCandidateGates []CandidateGateDiagnosticDTO `json:"top_candidate_gates,omitempty"`
}

// =========================================================================
// Institutional Trade Ledger, Tax Audit & Engine Analytics DTOs
// =========================================================================

// TaxBreakdownDTO holds the 6-part itemised Indian Cash Equity Intraday (MIS) statutory & brokerage charges
type TaxBreakdownDTO struct {
	BuyBrokerage  float64 `json:"buy_brokerage"`
	SellBrokerage float64 `json:"sell_brokerage"`
	Brokerage     float64 `json:"brokerage"`
	STT           float64 `json:"stt"`           // 0.025% on Sell turnover
	ExchangeTxn   float64 `json:"exchange_txn"`  // 0.00297% on total turnover (NSE)
	SEBICharges   float64 `json:"sebi_charges"`  // 0.0001% on total turnover
	StampDuty     float64 `json:"stamp_duty"`    // 0.003% on Buy turnover
	GST           float64 `json:"gst"`           // 18% on (Brokerage + ExchangeTxn + SEBI)
	TotalCharges  float64 `json:"total_charges"` // Exact sum of all statutory & brokerage fees
}

// TradeOrderLegDTO represents a child order leg (ENTRY, SCALE_OUT, SL, EMERGENCY_EXIT) of a position
type TradeOrderLegDTO struct {
	OrderID         string  `json:"order_id"`
	PositionID      string  `json:"position_id"`
	LegType         string  `json:"leg_type"`
	TransactionType string  `json:"transaction_type"`
	OrderType       string  `json:"order_type"`
	Quantity        int     `json:"quantity"`
	FilledQuantity  int     `json:"filled_quantity"`
	Price           float64 `json:"price"`
	TriggerPrice    float64 `json:"trigger_price"`
	AveragePrice    float64 `json:"average_price"`
	Status          string  `json:"status"`
	ModCount        int     `json:"mod_count"`
	UpdatedAt       string  `json:"updated_at"`
}

// TradeHistoryItemDTO represents a complete historical or active trade record with full tax & engine audit fields
type TradeHistoryItemDTO struct {
	PositionID            string             `json:"position_id"`
	TradingDate           string             `json:"trading_date"`
	Mode                  string             `json:"mode"`
	Symbol                string             `json:"symbol"`
	CompanyName           string             `json:"company_name"`
	Sector                string             `json:"sector"`
	InstrumentToken       uint32             `json:"instrument_token"`
	State                 string             `json:"state"`
	PatternType           string             `json:"pattern_type"`
	PurchaseTime          string             `json:"purchase_time"`
	SellTime              string             `json:"sell_time"`
	HoldDurationSec       int64              `json:"hold_duration_sec"`
	HoldDurationLabel     string             `json:"hold_duration_label"`
	Quantity              int                `json:"quantity"`
	BuyPrice              float64            `json:"buy_price"`
	SellPrice             float64            `json:"sell_price"`
	TotalInvested         float64            `json:"total_invested"`
	MarginBlocked         float64            `json:"margin_blocked"`
	TotalSellValue        float64            `json:"total_sell_value"`
	PriceChangePerShare   float64            `json:"price_change_per_share"`
	GrossPnL              float64            `json:"gross_pnl"`
	Taxes                 TaxBreakdownDTO    `json:"taxes"`
	NetPnLAfterTax        float64            `json:"net_pnl_after_tax"`
	ReturnOnInvestedPct   float64            `json:"return_on_invested_pct"`
	ReturnOnMarginPct     float64            `json:"return_on_margin_pct"`
	InitialStop           float64            `json:"initial_stop"`
	FinalStop             float64            `json:"final_stop"`
	BreakevenStop         float64            `json:"breakeven_stop"`
	HighWaterMark         float64            `json:"high_water_mark"`
	OneR                  float64            `json:"one_r"`
	RealisedRMultiple     float64            `json:"realised_r_multiple"`
	PeakMFER              float64            `json:"peak_mfe_r"`
	RCaptureEfficiencyPct float64            `json:"r_capture_efficiency_pct"`
	ModCount              int                `json:"mod_count"`
	FinalGear             int                `json:"final_gear"`
	HasTranche1Completed  bool               `json:"tranche1_done"`
	ExitReason            string             `json:"exit_reason"`
	Orders                []TradeOrderLegDTO `json:"orders,omitempty"`
}

// PatternPerformanceDTO summarizes performance by entry pattern (e.g. VWAP_PULLBACK_RECLAIM, FAILED_BREAKDOWN_SPRING, ORB_RETEST_RECLAIM)
type PatternPerformanceDTO struct {
	PatternType    string  `json:"pattern_type"`
	Trades         int     `json:"trades"`
	Wins           int     `json:"wins"`
	WinRatePct     float64 `json:"win_rate_pct"`
	AvgRMultiple   float64 `json:"avg_r_multiple"`
	GrossPnL       float64 `json:"gross_pnl"`
	TotalTaxes     float64 `json:"total_taxes"`
	NetPnLAfterTax float64 `json:"net_pnl_after_tax"`
}

// ExitReasonStatDTO summarizes trade outcomes by exit reason
type ExitReasonStatDTO struct {
	ExitReason     string  `json:"exit_reason"`
	Count          int     `json:"count"`
	AvgRMultiple   float64 `json:"avg_r_multiple"`
	GrossPnL       float64 `json:"gross_pnl"`
	TotalTaxes     float64 `json:"total_taxes"`
	NetPnLAfterTax float64 `json:"net_pnl_after_tax"`
}

// TradeAnalyticsSummaryDTO provides executive PnL, tax audit, and engine expectancy metrics
type TradeAnalyticsSummaryDTO struct {
	TotalTrades            int                     `json:"total_trades"`
	Wins                   int                     `json:"wins"`
	Losses                 int                     `json:"losses"`
	BreakevenTrades        int                     `json:"breakeven_trades"`
	WinRatePct             float64                 `json:"win_rate_pct"`
	TotalInvestedTurnover  float64                 `json:"total_invested_turnover"`
	TotalSellTurnover      float64                 `json:"total_sell_turnover"`
	TotalMarginDeployed    float64                 `json:"total_margin_deployed"`
	GrossProfitRupees      float64                 `json:"gross_profit_rupees"`
	GrossLossRupees        float64                 `json:"gross_loss_rupees"`
	RealisedGrossPnL       float64                 `json:"realised_gross_pnl"`
	Taxes                  TaxBreakdownDTO         `json:"taxes"`
	TaxDragPctOfGrossWin   float64                 `json:"tax_drag_pct_of_gross_win"`
	RealisedNetPnL         float64                 `json:"realised_net_pnl"`
	ProfitFactor           float64                 `json:"profit_factor"`
	ExpectancyPerTrade     float64                 `json:"expectancy_per_trade"`
	AvgRMultiple           float64                 `json:"avg_r_multiple"`
	AvgWinR                float64                 `json:"avg_win_r"`
	AvgLossR               float64                 `json:"avg_loss_r"`
	AvgWinRupees           float64                 `json:"avg_win_rupees"`
	AvgLossRupees          float64                 `json:"avg_loss_rupees"`
	MaxWinRupees           float64                 `json:"max_win_rupees"`
	MaxLossRupees          float64                 `json:"max_loss_rupees"`
	AvgHoldDurationSec     int64                   `json:"avg_hold_duration_sec"`
	Tranche1ScaleOutRate   float64                 `json:"tranche1_scale_out_rate"`
	AvgRCaptureEfficiency  float64                 `json:"avg_r_capture_efficiency"`
	ByPattern              []PatternPerformanceDTO `json:"by_pattern"`
	ByExitReason           []ExitReasonStatDTO     `json:"by_exit_reason"`
}

// TradeHistoryResponseDTO is the response payload for GET /api/execution/history
type TradeHistoryResponseDTO struct {
	Summary TradeAnalyticsSummaryDTO `json:"summary"`
	Trades  []TradeHistoryItemDTO    `json:"trades"`
}

