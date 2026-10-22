package user

import (
	"net/http"

	"github.com/ajaynegi33/AlgoTrade/internal/auth"
	"github.com/ajaynegi33/AlgoTrade/internal/response"
)

type Handler struct {
	store Store
}

func NewHandler(store Store) *Handler {
	return &Handler{
		store: store,
	}
}

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "User claims not found in context")
		return
	}

	userName := claims.Email
	userRole := claims.Role
	if h.store != nil {
		if u, err := h.store.GetByID(claims.UserID); err == nil {
			userName = u.Name
			userRole = u.Role
		}
	}

	// Dynamic dummy data mapped to the authenticated user's session
	data := ProfileResponseData{
		Profile: Profile{
			ID:            claims.UserID,
			Name:          userName,
			Email:         claims.Email,
			Role:          userRole,
			AccountStatus: "ACTIVE",
			Tier:          "Institutional Tier 2",
			KYCVerified:   true,
			CreatedAt:     "2025-01-15T09:30:00Z",
		},
		PortfolioSummary: PortfolioSummary{
			AccountNumber:        "ACC-789012-US",
			Currency:             "USD",
			TotalEquity:          148520.75,
			CashBalance:          32410.50,
			UnrealizedPnL:        4210.25,
			UnrealizedPnLPercent: 2.92,
			ActiveBotsCount:      3,
		},
		ActivePositions: []Position{
			{
				Symbol:       "BTC/USDT",
				Side:         "LONG",
				Size:         1.25,
				EntryPrice:   64200.00,
				CurrentPrice: 66800.00,
				PnL:          3250.00,
				PnLPercent:   4.05,
			},
			{
				Symbol:       "ETH/USDT",
				Side:         "LONG",
				Size:         12.00,
				EntryPrice:   3450.00,
				CurrentPrice: 3530.00,
				PnL:          960.00,
				PnLPercent:   2.32,
			},
			{
				Symbol:       "SOL/USDT",
				Side:         "LONG",
				Size:         45.00,
				EntryPrice:   152.00,
				CurrentPrice: 154.50,
				PnL:          112.50,
				PnLPercent:   1.64,
			},
		},
	}

	response.Success(w, http.StatusOK, data)
}
