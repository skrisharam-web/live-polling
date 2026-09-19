package handlers

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/middleware"
	"github.com/skrisharam-web/live-polling/backend/internal/response"
	"github.com/skrisharam-web/live-polling/backend/internal/services"
)

// voteService is what the HTTP layer needs from the vote service.
type voteService interface {
	Cast(ctx context.Context, pollID, optionID, voterID string) (*services.Results, error)
	Results(ctx context.Context, pollID string) (*services.Results, error)
	VoteOf(ctx context.Context, pollID string, voterID string) string
}

// VoteHandler exposes voting and results. Both routes are public: holding the
// share link is the only credential the audience needs.
type VoteHandler struct {
	votes voteService
}

func NewVoteHandler(votes voteService) *VoteHandler {
	return &VoteHandler{votes: votes}
}

// voteRequest carries only the chosen option. There is deliberately no voter
// field: the identity comes from the signed cookie, so a client cannot vote as
// somebody else or mint identities by editing a payload.
type voteRequest struct {
	OptionID string `json:"optionId"`
}

// Vote handles POST /api/polls/:id/vote and answers with the new standing, so a
// voter sees the result of their own vote without a second round trip.
func (h *VoteHandler) Vote(c *gin.Context) {
	voterID, ok := middleware.CurrentVoterID(c)
	if !ok {
		response.Fail(c, apperr.CodeInternal, "Could not identify your browser. Please enable cookies and try again.")
		return
	}

	var req voteRequest
	if !bindJSON(c, &req) {
		return
	}

	results, err := h.votes.Cast(c.Request.Context(), c.Param("id"), req.OptionID, voterID)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, newResultsResponse(results, req.OptionID))
}

// Results handles GET /api/polls/:id/results.
func (h *VoteHandler) Results(c *gin.Context) {
	results, err := h.votes.Results(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Error(c, err)
		return
	}

	yourVote := ""
	if voterID, ok := middleware.CurrentVoterID(c); ok {
		yourVote = h.votes.VoteOf(c.Request.Context(), results.PollID, voterID)
	}

	response.OK(c, newResultsResponse(results, yourVote))
}
