package handlers

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/middleware"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
	"github.com/skrisharam-web/live-polling/backend/internal/response"
	"github.com/skrisharam-web/live-polling/backend/internal/services"
)

// pollService is what the HTTP layer needs from the poll service.
type pollService interface {
	Create(ctx context.Context, ownerID bson.ObjectID, input services.CreatePollInput) (*models.Poll, error)
	Get(ctx context.Context, pollID string) (*models.Poll, error)
	GetOwned(ctx context.Context, ownerID bson.ObjectID, pollID string) (*models.Poll, error)
	ListForOwner(ctx context.Context, ownerID bson.ObjectID) ([]services.PollWithTotal, error)
	Update(ctx context.Context, ownerID bson.ObjectID, pollID string, input services.UpdatePollInput) (*models.Poll, error)
	Close(ctx context.Context, ownerID bson.ObjectID, pollID string) (*models.Poll, error)
	Delete(ctx context.Context, ownerID bson.ObjectID, pollID string) error
}

// PollHandler exposes poll creation and management over HTTP.
type PollHandler struct {
	polls pollService
}

func NewPollHandler(polls pollService) *PollHandler {
	return &PollHandler{polls: polls}
}

type createPollRequest struct {
	Question  string     `json:"question"`
	Options   []string   `json:"options"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// updatePollRequest uses pointers so an absent field and an explicitly null field
// stay distinguishable — the difference between "leave the expiry alone" and
// "remove the expiry".
type updatePollRequest struct {
	Question  *string          `json:"question"`
	Options   *[]string        `json:"options"`
	ExpiresAt jsonNullableTime `json:"expiresAt"`
}

// Create handles POST /api/polls. It is behind RequireAuth, and the owner is
// taken from the session — never from the payload, which is why the request type
// above has no owner field for a client to set.
func (h *PollHandler) Create(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, apperr.CodeUnauthorized, "Please sign in to continue.")
		return
	}

	var req createPollRequest
	if !bindJSON(c, &req) {
		return
	}

	poll, err := h.polls.Create(c.Request.Context(), ownerID, services.CreatePollInput{
		Question:  req.Question,
		Options:   req.Options,
		ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, gin.H{"poll": newPollResponse(poll, time.Now().UTC())})
}

// Get handles GET /api/polls/:id. Public: holding the link is the credential.
func (h *PollHandler) Get(c *gin.Context) {
	poll, err := h.polls.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"poll": newPollResponse(poll, time.Now().UTC())})
}

// List handles GET /api/me/polls.
func (h *PollHandler) List(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, apperr.CodeUnauthorized, "Please sign in to continue.")
		return
	}

	rows, err := h.polls.ListForOwner(c.Request.Context(), ownerID)
	if err != nil {
		response.Error(c, err)
		return
	}

	now := time.Now().UTC()
	polls := make([]PollSummaryResponse, 0, len(rows))
	for i := range rows {
		polls = append(polls, newPollSummaryResponse(&rows[i].Poll, rows[i].TotalVotes, now))
	}
	response.OK(c, gin.H{"polls": polls})
}

// Update handles PATCH /api/polls/:id.
func (h *PollHandler) Update(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, apperr.CodeUnauthorized, "Please sign in to continue.")
		return
	}

	var req updatePollRequest
	if !bindJSON(c, &req) {
		return
	}

	input := services.UpdatePollInput{
		Question:    req.Question,
		ExpiresAt:   req.ExpiresAt.Value,
		ClearExpiry: req.ExpiresAt.Present && req.ExpiresAt.Value == nil,
	}
	if req.Options != nil {
		input.Options = *req.Options
	}

	poll, err := h.polls.Update(c.Request.Context(), ownerID, c.Param("id"), input)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"poll": newPollResponse(poll, time.Now().UTC())})
}

// Close handles POST /api/polls/:id/close.
func (h *PollHandler) Close(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, apperr.CodeUnauthorized, "Please sign in to continue.")
		return
	}

	poll, err := h.polls.Close(c.Request.Context(), ownerID, c.Param("id"))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"poll": newPollResponse(poll, time.Now().UTC())})
}

// Delete handles DELETE /api/polls/:id.
func (h *PollHandler) Delete(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, apperr.CodeUnauthorized, "Please sign in to continue.")
		return
	}

	if err := h.polls.Delete(c.Request.Context(), ownerID, c.Param("id")); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// Manage handles GET /api/polls/:id/manage: the same poll, but only for its
// owner. The management screen needs to know it is looking at its own poll, and
// this endpoint answers 403 rather than quietly serving the public view.
func (h *PollHandler) Manage(c *gin.Context) {
	ownerID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, apperr.CodeUnauthorized, "Please sign in to continue.")
		return
	}

	poll, err := h.polls.GetOwned(c.Request.Context(), ownerID, c.Param("id"))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"poll": newPollResponse(poll, time.Now().UTC())})
}
