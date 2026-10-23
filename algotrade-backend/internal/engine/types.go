package engine

// MarketPulseDTO provides index breadth and macro regime context
type MarketPulseDTO struct {
	MarketState            string  `json:"market_state"` // "OPEN", "PRE_OPEN", "CLOSED"
	NiftyLTP               float64 `json:"nifty_ltp"`
	NiftyChangePercent     float64 `json:"nifty_change_percent"`
	BankNiftyLTP           float64 `json:"banknifty_ltp"`
	BankNiftyChangePercent float64 `json:"banknifty_change_percent"`
	BreadthAboveVWAP       float64 `json:"breadth_above_vwap"` // Percentage of universe stocks > VWAP
	MarketRegime           string  `json:"market_regime"`      // "BULLISH_EXPANSION", "BEARISH_EXPANSION", "SIDEWAYS_CHOP"
}

// SectorRankDTO represents a sector's real-time performance on Dalal Street
type SectorRankDTO struct {
	SectorID         uint8   `json:"sector_id"`
	SectorName       string  `json:"sector_name"`
	ChangePercent    float64 `json:"change_percent"`
	BreadthAboveVWAP float64 `json:"breadth_above_vwap"`
	IsLeader         bool    `json:"is_leader"`
	IsLagging        bool    `json:"is_lagging"`
}

// StockRankDTO represents a fully calculated stock record for 1.0s UI consumption
type StockRankDTO struct {
	Rank              int     `json:"rank"`
	Token             uint32  `json:"token"`
	Symbol            string  `json:"symbol"`
	Sector            string  `json:"sector"`
	SectorID          uint8   `json:"sector_id"`
	LTP               float64 `json:"ltp"`
	ChangePercent     float64 `json:"change_percent"`
	VWAP              float64 `json:"vwap"`
	BullishScore      float64 `json:"bullish_score"`
	BearishScore      float64 `json:"bearish_score"`
	RVOLSession       float64 `json:"rvol_session"`
	RVOLPace5m        float64 `json:"rvol_pace_5m"`
	CompositeRS       float64 `json:"composite_rs"`
	BuyerRatio        float32 `json:"buyer_ratio"`
	AggressorBuyRatio float32 `json:"aggressor_buy_ratio"`
	EV                float64 `json:"ev"`
	PWin              float64 `json:"p_win"`
	StopProxy         float64 `json:"stop_proxy"`
	TargetProxy       float64 `json:"target_proxy"`
	Reasons           uint16  `json:"reasons"`
	PullbackCount     uint8   `json:"pullback_count"`
}

// RankedSnapshot is the 1.0s unified frame streamed to the frontend dashboard
type RankedSnapshot struct {
	Timestamp        int64            `json:"timestamp"`
	HistoricalActive bool             `json:"historical_active"`
	HistoricalError  string           `json:"historical_error,omitempty"`
	MarketPulse      MarketPulseDTO   `json:"market_pulse"`
	Sectors          []SectorRankDTO  `json:"sectors"`
	TopBullish       []StockRankDTO   `json:"top_bullish"`
	TopBearish       []StockRankDTO   `json:"top_bearish"`
	AllStocks        []StockRankDTO   `json:"all_stocks"`
}
