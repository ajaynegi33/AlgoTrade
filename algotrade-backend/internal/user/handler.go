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

// GetProfile returns the authenticated operator's real identity
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

	response.Success(w, http.StatusOK, Profile{
		ID:    claims.UserID,
		Name:  userName,
		Email: claims.Email,
		Role:  userRole,
	})
}
