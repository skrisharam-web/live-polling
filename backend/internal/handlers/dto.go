package handlers

import (
	"time"

	"github.com/skrisharam-web/live-polling/backend/internal/models"
	"github.com/skrisharam-web/live-polling/backend/internal/services"
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

// OptionResponse is one choice as the client sees it.
type OptionResponse struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// PollResponse is the public projection of a poll. Note what is absent: the
// owner's ID, the owner's name and anything about who voted. The share link is
// world-readable, so this payload is written for a stranger.
type PollResponse struct {
	ID        string           `json:"id"`
	Question  string           `json:"question"`
	Options   []OptionResponse `json:"options"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	ExpiresAt *time.Time       `json:"expiresAt,omitempty"`
	// AcceptsVotes saves every client from re-deriving "active and not expired".
	AcceptsVotes bool `json:"acceptsVotes"`
}

func newPollResponse(poll *models.Poll, now time.Time) PollResponse {
	options := make([]OptionResponse, 0, len(poll.Options))
	for _, opt := range poll.Options {
		options = append(options, OptionResponse{ID: opt.ID, Text: opt.Text})
	}

	return PollResponse{
		ID:       poll.ID.Hex(),
		Question: poll.Question,
		Options:  options,
		// The effective status is reported, so a poll past its expiry reads as
		// closed even though no write has happened yet.
		Status:       string(poll.EffectiveStatus(now)),
		CreatedAt:    poll.CreatedAt,
		UpdatedAt:    poll.UpdatedAt,
		ExpiresAt:    poll.ExpiresAt,
		AcceptsVotes: poll.AcceptsVotes(now),
	}
}

// PollSummaryResponse is a dashboard row: the poll plus its vote total.
type PollSummaryResponse struct {
	PollResponse
	TotalVotes int64 `json:"totalVotes"`
}

func newPollSummaryResponse(poll *models.Poll, totalVotes int64, now time.Time) PollSummaryResponse {
	return PollSummaryResponse{
		PollResponse: newPollResponse(poll, now),
		TotalVotes:   totalVotes,
	}
}

// OptionResultResponse is one option's standing.
//
// The count is sent, not a percentage: a percentage is a presentation decision,
// and letting the server and the client each round it independently is how a
// results view ends up showing 33% three times and a total of 99%.
type OptionResultResponse struct {
	OptionID string `json:"optionId"`
	Text     string `json:"text"`
	Count    int64  `json:"count"`
}

// ResultsResponse is a poll's standing as the client sees it.
type ResultsResponse struct {
	PollID     string                 `json:"pollId"`
	Results    []OptionResultResponse `json:"results"`
	TotalVotes int64                  `json:"totalVotes"`
	Status     string                 `json:"status"`
	ComputedAt time.Time              `json:"computedAt"`
	// YourVote is the option this browser already chose, if any. It is derived
	// from the requester's own voter cookie, so it reveals nothing about anyone
	// else.
	YourVote string `json:"yourVote,omitempty"`
}

func newResultsResponse(results *services.Results, yourVote string) ResultsResponse {
	rows := make([]OptionResultResponse, 0, len(results.Options))
	for _, option := range results.Options {
		rows = append(rows, OptionResultResponse{
			OptionID: option.OptionID,
			Text:     option.Text,
			Count:    option.Count,
		})
	}

	return ResultsResponse{
		PollID:     results.PollID,
		Results:    rows,
		TotalVotes: results.TotalVotes,
		Status:     string(results.Status),
		ComputedAt: results.ComputedAt,
		YourVote:   yourVote,
	}
}
