package monitoring

import (
	"time"
)

// ShadowSignalEvent represents a qualified candidate setup that was blocked or skipped by pre-trade filters
type ShadowSignalEvent struct {
	TradingDate      string    `json:"trading_date"`
	SignalTime       time.Time `json:"signal_time"`
	Symbol           string    `json:"symbol"`
	InstrumentToken  uint32    `json:"instrument_token"`
	PatternType      string    `json:"pattern_type"`
	BullishScore     float64   `json:"bullish_score"`
	RejectionReason  string    `json:"rejection_reason"`
	EntryPrice       float64   `json:"entry_price"`
	HypotheticalStop float64   `json:"hypothetical_stop"`
	OneR             float64   `json:"one_r"`
	Target1R         float64   `json:"target_1r"`
	Outcome          string    `json:"outcome"`     // "PENDING", "WIN_1R", "LOSS_1R", "EOD_EXPIRE"
	SimulatedR       float64   `json:"simulated_r"` // Realized or simulated R-multiple
}

// TradeExecutionEvent records an actual position fill and its execution metrics
type TradeExecutionEvent struct {
	PositionID         string    `json:"position_id"`
	TradingDate        string    `json:"trading_date"`
	Symbol             string    `json:"symbol"`
	PatternType        string    `json:"pattern_type"`
	IntendedEntryPrice float64   `json:"intended_entry_price"`
	ActualFillPrice    float64   `json:"actual_fill_price"`
	EntrySlippagePct   float64   `json:"entry_slippage_pct"`
	FilledQty          int       `json:"filled_qty"`
	HighWaterMark      float64   `json:"high_water_mark"`
	LowWaterMark       float64   `json:"low_water_mark"`
	OneR               float64   `json:"one_r"`
	MFERMultiple       float64   `json:"mfe_r_multiple"`
	MAERMultiple       float64   `json:"mae_r_multiple"`
	RealisedNetPnL     float64   `json:"realised_net_pnl"`
	CurrentGear        int       `json:"current_gear"`
	ExitReason         string    `json:"exit_reason"`
	OpenedAt           time.Time `json:"opened_at"`
	ClosedAt           time.Time `json:"closed_at"`
}

// PositionReportRecord is an individual position entry in the EOD trade ledger
type PositionReportRecord struct {
	PositionID         string  `json:"position_id"`
	Symbol             string  `json:"symbol"`
	PatternType        string  `json:"pattern_type"`
	EntryPrice         float64 `json:"entry_price"`
	ExitPrice          float64 `json:"exit_price"`
	InitialStop        float64 `json:"initial_stop"`
	OneR               float64 `json:"one_r"`
	Quantity           int     `json:"quantity"`
	MFERMultiple       float64 `json:"mfe_r_multiple"`
	MAERMultiple       float64 `json:"mae_r_multiple"`
	RealisedRMultiple  float64 `json:"realised_r_multiple"`
	RealisedGrossPnL   float64 `json:"realised_gross_pnl"`
	TotalCharges       float64 `json:"total_charges"`
	RealisedNetPnL     float64 `json:"realised_net_pnl"`
	IntendedEntryPrice float64 `json:"intended_entry_price"`
	EntrySlippagePct   float64 `json:"entry_slippage_pct"`
	ExitReason         string  `json:"exit_reason"`
	MaxGearReached     int     `json:"max_gear_reached"`
	OpenedAt           string  `json:"opened_at"`
	ClosedAt           string  `json:"closed_at"`
	DurationMinutes    int     `json:"duration_minutes"`
	IsWinner           bool    `json:"is_winner"`
}

// MFEvsMAESummary breaks down favorable and adverse excursions
type MFEvsMAESummary struct {
	AverageMAE         float64 `json:"average_mae"`          // Negative R, e.g. -0.35R
	MedianMAEWinners   float64 `json:"median_mae_winners"`   // Negative R for winning trades
	PctWinnersUnder06R float64 `json:"pct_winners_under_06r"` // % of winners that never went past -0.6R
	StopLossAlert      string  `json:"stop_loss_alert"`      // Diagnostic alert if stops are too wide
	AverageMFE         float64 `json:"average_mfe"`          // Positive R, e.g. +2.40R
	AverageRealisedR   float64 `json:"average_realised_r"`   // Realized R e.g. +1.10R
	TrailingStopAlert  string  `json:"trailing_stop_alert"`  // Diagnostic alert if trailing stops are too loose
	EdgeRatio          float64 `json:"edge_ratio"`           // Avg MFE / |Avg MAE|
	CaptureEfficiency  float64 `json:"capture_efficiency"`   // Realized R / MFE R percentage
}

// ShadowComparisonSummary compares actual trades against rejected/shadow setups
type ShadowComparisonSummary struct {
	ActualTradeCount      int     `json:"actual_trade_count"`
	ActualWinRatePct      float64 `json:"actual_win_rate_pct"`
	ActualAverageR        float64 `json:"actual_average_r"`
	ActualTotalNetPnL     float64 `json:"actual_total_net_pnl"`
	ShadowSignalCount     int     `json:"shadow_signal_count"`
	ShadowWinRatePct      float64 `json:"shadow_win_rate_pct"`
	ShadowAverageR        float64 `json:"shadow_average_r"`
	FilterEfficiencyAlert string  `json:"filter_efficiency_alert"` // Alert if shadow outperforms actual
	RejectionBreakdown    map[string]int `json:"rejection_breakdown"`
}

// SlippageAuditRecord evaluates execution friction per stock
type SlippageAuditRecord struct {
	Symbol             string  `json:"symbol"`
	TradeCount         int     `json:"trade_count"`
	AvgIntendedPrice   float64 `json:"avg_intended_price"`
	AvgFillPrice       float64 `json:"avg_fill_price"`
	AvgSlippagePct     float64 `json:"avg_slippage_pct"`
	MaxSlippagePct     float64 `json:"max_slippage_pct"`
	IsToxicLiquidity   bool    `json:"is_toxic_liquidity"` // True if avg slippage > 0.15% or max > 0.30%
	BlacklistReason    string  `json:"blacklist_reason,omitempty"`
}

// ProbabilityBucket aggregates calibration metrics by pre-trade score bucket
type ProbabilityBucket struct {
	BucketName   string  `json:"bucket_name"` // "50-65%", "65-75%", "75-85%", "85%+"
	MinScore     float64 `json:"min_score"`
	MaxScore     float64 `json:"max_score"`
	TradeCount   int     `json:"trade_count"`
	WinningCount int     `json:"winning_count"`
	WinRatePct   float64 `json:"win_rate_pct"`
	AverageR     float64 `json:"average_r"`
	NetPnL       float64 `json:"net_pnl"`
}

// TimeOfDayBucket aggregates performance by intraday trading window
type TimeOfDayBucket struct {
	SessionName   string  `json:"session_name"` // "Opening Drive (09:15-10:00)", etc.
	TimeWindow    string  `json:"time_window"`
	TradeCount    int     `json:"trade_count"`
	WinningCount  int     `json:"winning_count"`
	WinRatePct    float64 `json:"win_rate_pct"`
	GrossPnL      float64 `json:"gross_pnl"`
	NetPnL        float64 `json:"net_pnl"`
	AverageR      float64 `json:"average_r"`
}

// GearAttributionRecord evaluates trailing stop performance across gears
type GearAttributionRecord struct {
	Gear        int     `json:"gear"`
	GearLabel   string  `json:"gear_label"`
	TradesCount int     `json:"trades_count"`
	BankedPnL   float64 `json:"banked_pnl"`
}

// EODReportSummary is the master structure containing all diagnostic analytics for the day
type EODReportSummary struct {
	TradingDate         string                   `json:"trading_date"`
	GeneratedAt         time.Time                `json:"generated_at"`
	TotalTrades         int                      `json:"total_trades"`
	WinningTrades       int                      `json:"winning_trades"`
	LosingTrades        int                      `json:"losing_trades"`
	BreakevenTrades     int                      `json:"breakeven_trades"`
	WinRatePct          float64                  `json:"win_rate_pct"`
	RealisedGrossPnL    float64                  `json:"realised_gross_pnl"`
	TotalCharges        float64                  `json:"total_charges"`
	RealisedNetPnL      float64                  `json:"realised_net_pnl"`
	ProfitFactor        float64                  `json:"profit_factor"`
	AverageRMultiple    float64                  `json:"average_r_multiple"`
	ExcursionSummary    MFEvsMAESummary          `json:"excursion_summary"`
	ShadowSummary       ShadowComparisonSummary  `json:"shadow_summary"`
	SlippageRecords     []SlippageAuditRecord    `json:"slippage_records"`
	ToxicSymbols        []string                 `json:"toxic_symbols"`
	ProbabilityBuckets  []ProbabilityBucket      `json:"probability_buckets"`
	SessionBreakdowns   []TimeOfDayBucket        `json:"session_breakdowns"`
	GearAttributions    []GearAttributionRecord  `json:"gear_attributions"`
	TradeLedger         []PositionReportRecord   `json:"trade_ledger"`
}

