package user

type Profile struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Role          string `json:"role"`
	AccountStatus string `json:"account_status"`
	Tier          string `json:"tier"`
	KYCVerified   bool   `json:"kyc_verified"`
	CreatedAt     string `json:"created_at"`
}

type PortfolioSummary struct {
	AccountNumber        string  `json:"account_number"`
	Currency             string  `json:"currency"`
	TotalEquity          float64 `json:"total_equity"`
	CashBalance          float64 `json:"cash_balance"`
	UnrealizedPnL        float64 `json:"unrealized_pnl"`
	UnrealizedPnLPercent float64 `json:"unrealized_pnl_percent"`
	ActiveBotsCount      int     `json:"active_bots_count"`
}

type Position struct {
	Symbol       string  `json:"symbol"`
	Side         string  `json:"side"` // "LONG" or "SHORT"
	Size         float64 `json:"size"`
	EntryPrice   float64 `json:"entry_price"`
	CurrentPrice float64 `json:"current_price"`
	PnL          float64 `json:"pnl"`
	PnLPercent   float64 `json:"pnl_percent"`
}

type ProfileResponseData struct {
	Profile          Profile          `json:"profile"`
	PortfolioSummary PortfolioSummary `json:"portfolio_summary"`
	ActivePositions  []Position       `json:"active_positions"`
}
