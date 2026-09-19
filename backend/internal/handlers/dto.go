package handlers

import (
	"time"

	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// The types here are the API's wire format. They exist separately from the domain
// models so that adding a field to a model — a password hash, an internal flag —
// cannot accidentally publish it. Anything a client sees is listed explicitly
// below.

// UserResponse is the public projection of an account. There is deliberately no
// password field of any kind.
type UserResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
}

func newUserResponse(user *models.User) UserResponse {
	return UserResponse{
		ID:        user.ID.Hex(),
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	}
}
