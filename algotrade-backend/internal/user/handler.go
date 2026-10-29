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
func (handler *Handler) GetProfile(writer http.ResponseWriter, request *http.Request) {
	claims, ok := auth.GetUserFromContext(request.Context())
	if !ok {
		response.Error(writer, http.StatusUnauthorized, "UNAUTHORIZED", "User claims not found in context")
		return
	}

	userName := claims.Email
	userRole := claims.Role
	if handler.store != nil {
		if foundUser, err := handler.store.GetByID(claims.UserID); err == nil {
			userName = foundUser.Name
			userRole = foundUser.Role
		}
	}

	response.Success(writer, http.StatusOK, Profile{
		ID:    claims.UserID,
		Name:  userName,
		Email: claims.Email,
		Role:  userRole,
	})
}
