package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
	"github.com/skrisharam-web/live-polling/backend/internal/repositories"
	"github.com/skrisharam-web/live-polling/backend/internal/validation"
)

// MaxPollLifetime bounds how far ahead a poll may be scheduled to close. An
// unbounded expiry is not useful and an accidental year-2099 date is almost
// always a typo.
const MaxPollLifetime = 30 * 24 * time.Hour

// PollStore is the persistence the poll service needs.
type PollStore interface {
	Create(ctx context.Context, poll *models.Poll) error
	FindByID(ctx context.Context, id string) (*models.Poll, error)
	ListByOwner(ctx context.Context, ownerID bson.ObjectID, limit int64) ([]models.Poll, error)
	Update(ctx context.Context, id, ownerID bson.ObjectID, update repositories.PollUpdate) (*models.Poll, error)
	Delete(ctx context.Context, id, ownerID bson.ObjectID) error
}

// VoteCounter is the part of the vote repository the poll service consults. Poll
// management needs to know whether a poll has been voted on, because that decides
// what may still be edited.
type VoteCounter interface {
	CountByPoll(ctx context.Context, pollID bson.ObjectID) (int64, error)
	TotalsByPolls(ctx context.Context, pollIDs []bson.ObjectID) (map[bson.ObjectID]int64, error)
	DeleteByPoll(ctx context.Context, pollID bson.ObjectID) error
}

// ResultsCleaner lets the poll service discard a deleted poll's derived Redis
// state. It is an interface so that Redis being unavailable cannot block a
// delete, and so this service does not depend on the Redis package directly.
type ResultsCleaner interface {
	DeleteResults(ctx context.Context, pollID string) error
}

// PollService owns the rules for creating and managing polls.
type PollService struct {
	polls   PollStore
	votes   VoteCounter
	results ResultsCleaner
	// announcer lets a management action — closing a poll — reach the people
	// currently looking at it, instead of them finding out when they next try to
	// vote.
	announcer PollAnnouncer

	now func() time.Time
	// dashboardLimit caps how many polls a dashboard request returns, so a single
	// query can never be made to load an unbounded result set.
	dashboardLimit int64
}

// PollAnnouncer pushes a poll's current standing to everyone watching it.
type PollAnnouncer interface {
	AnnouncePoll(ctx context.Context, poll *models.Poll)
}

// NewPollService builds the service. results and announcer may be nil, which
// degrades the Redis cleanup and the live close notification respectively while
// leaving poll management itself working.
func NewPollService(polls PollStore, votes VoteCounter, results ResultsCleaner, announcer PollAnnouncer) *PollService {
	return &PollService{
		polls:          polls,
		votes:          votes,
		results:        results,
		announcer:      announcer,
		now:            func() time.Time { return time.Now().UTC() },
		dashboardLimit: 100,
	}
}

// CreatePollInput is the untrusted create payload.
type CreatePollInput struct {
	Question  string
	Options   []string
	ExpiresAt *time.Time
}

// UpdatePollInput is the untrusted patch payload. A nil pointer means "not
// supplied", which is how PATCH distinguishes "leave it alone" from "set it".
type UpdatePollInput struct {
	Question  *string
	Options   []string
	ExpiresAt *time.Time
	// ClearExpiry removes an existing expiry, which a nil ExpiresAt cannot express.
	ClearExpiry bool
}

// PollWithTotal pairs a poll with its vote total for list views.
type PollWithTotal struct {
	Poll       models.Poll
	TotalVotes int64
}

// Create validates the input and stores a new poll owned by ownerID.
func (s *PollService) Create(ctx context.Context, ownerID bson.ObjectID, input CreatePollInput) (*models.Poll, error) {
	v := validation.New()
	question := v.Question("question", input.Question)
	optionTexts := v.Options("options", input.Options)
	expiresAt := s.validateExpiry(v, input.ExpiresAt)
	if !v.Valid() {
		return nil, apperr.Validation("Please correct the highlighted fields.", v.Fields())
	}

	options := make([]models.Option, 0, len(optionTexts))
	for _, text := range optionTexts {
		id, err := newOptionID()
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "Could not create the poll.", err)
		}
		options = append(options, models.Option{ID: id, Text: text})
	}

	now := s.now()
	poll := &models.Poll{
		OwnerID:   ownerID,
		Question:  question,
		Options:   options,
		Status:    models.PollStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: expiresAt,
	}

	if err := s.polls.Create(ctx, poll); err != nil {
		return nil, err
	}
	return poll, nil
}

// Get returns a poll by ID for anyone holding the share link. It is deliberately
// unauthenticated: the link is the credential, and the response carries no
// owner-private data.
func (s *PollService) Get(ctx context.Context, pollID string) (*models.Poll, error) {
	return s.polls.FindByID(ctx, pollID)
}

// GetOwned returns a poll only if the requesting user owns it. Every management
// operation goes through here, so the ownership rule is written once.
func (s *PollService) GetOwned(ctx context.Context, ownerID bson.ObjectID, pollID string) (*models.Poll, error) {
	poll, err := s.polls.FindByID(ctx, pollID)
	if err != nil {
		return nil, err
	}
	if poll.OwnerID != ownerID {
		return nil, apperr.New(apperr.CodeForbidden, "You do not have access to this poll.")
	}
	return poll, nil
}

// ListForOwner returns the signed-in user's polls, newest first, each with its
// vote total.
func (s *PollService) ListForOwner(ctx context.Context, ownerID bson.ObjectID) ([]PollWithTotal, error) {
	polls, err := s.polls.ListByOwner(ctx, ownerID, s.dashboardLimit)
	if err != nil {
		return nil, err
	}

	ids := make([]bson.ObjectID, 0, len(polls))
	for _, poll := range polls {
		ids = append(ids, poll.ID)
	}

	totals, err := s.votes.TotalsByPolls(ctx, ids)
	if err != nil {
		return nil, err
	}

	rows := make([]PollWithTotal, 0, len(polls))
	for _, poll := range polls {
		rows = append(rows, PollWithTotal{Poll: poll, TotalVotes: totals[poll.ID]})
	}
	return rows, nil
}

// Update applies an owner's edit.
//
// The question and the options are frozen once the first vote is in. Rewriting
// the question afterwards would leave recorded votes answering something nobody
// was asked, and changing the options would either orphan votes or silently
// re-label them. Scheduling — the expiry — stays editable because it changes only
// what happens next.
func (s *PollService) Update(ctx context.Context, ownerID bson.ObjectID, pollID string, input UpdatePollInput) (*models.Poll, error) {
	poll, err := s.GetOwned(ctx, ownerID, pollID)
	if err != nil {
		return nil, err
	}

	v := validation.New()
	update := repositories.PollUpdate{ClearExpiry: input.ClearExpiry}

	contentChanged := input.Question != nil || input.Options != nil
	if contentChanged {
		voteCount, err := s.votes.CountByPoll(ctx, poll.ID)
		if err != nil {
			return nil, err
		}
		if voteCount > 0 {
			return nil, apperr.New(apperr.CodeConflict,
				"This poll already has votes, so its question and options can no longer be changed.")
		}
	}

	if input.Question != nil {
		question := v.Question("question", *input.Question)
		update.Question = &question
	}
	if input.Options != nil {
		optionTexts := v.Options("options", input.Options)
		if v.Valid() {
			options, err := s.rebuildOptions(poll, optionTexts)
			if err != nil {
				return nil, err
			}
			update.Options = options
		}
	}
	if input.ExpiresAt != nil {
		update.ExpiresAt = s.validateExpiry(v, input.ExpiresAt)
	}

	if !v.Valid() {
		return nil, apperr.Validation("Please correct the highlighted fields.", v.Fields())
	}
	if update.Question == nil && update.Options == nil && update.ExpiresAt == nil && !update.ClearExpiry {
		return nil, apperr.New(apperr.CodeValidation, "There is nothing to update.")
	}

	return s.polls.Update(ctx, poll.ID, ownerID, update)
}

// Close stops a poll accepting votes. Closing an already-closed poll is not an
// error: the caller asked for a state, and the poll is in it.
func (s *PollService) Close(ctx context.Context, ownerID bson.ObjectID, pollID string) (*models.Poll, error) {
	poll, err := s.GetOwned(ctx, ownerID, pollID)
	if err != nil {
		return nil, err
	}
	if poll.Status == models.PollStatusClosed {
		return poll, nil
	}

	closed := models.PollStatusClosed
	updated, err := s.polls.Update(ctx, poll.ID, ownerID, repositories.PollUpdate{Status: &closed})
	if err != nil {
		return nil, err
	}

	if s.announcer != nil {
		s.announcer.AnnouncePoll(ctx, updated)
	}
	return updated, nil
}

// Delete removes a poll, its votes and its derived Redis state.
//
// The order matters: the poll goes first, so a failure part-way through can never
// leave a reachable poll whose votes have been deleted underneath it. A leftover
// vote document is invisible and harmless by comparison, and the Redis key is
// disposable by construction.
func (s *PollService) Delete(ctx context.Context, ownerID bson.ObjectID, pollID string) error {
	poll, err := s.GetOwned(ctx, ownerID, pollID)
	if err != nil {
		return err
	}

	if err := s.polls.Delete(ctx, poll.ID, ownerID); err != nil {
		return err
	}
	if err := s.votes.DeleteByPoll(ctx, poll.ID); err != nil {
		return err
	}
	if s.results != nil {
		// Redis holds only derived data, so failing to clear it must not fail the
		// delete. The handler logs it; the key is orphaned, not wrong.
		_ = s.results.DeleteResults(ctx, poll.ID.Hex())
	}
	return nil
}

// rebuildOptions maps new option text onto option IDs.
//
// An option that keeps its text keeps its ID. That only matters while a poll has
// no votes — which is the only time this runs — but it keeps IDs stable across a
// typo fix, so any link or client state referring to an option stays valid.
func (s *PollService) rebuildOptions(poll *models.Poll, texts []string) ([]models.Option, error) {
	existing := make(map[string]string, len(poll.Options))
	for _, opt := range poll.Options {
		existing[opt.Text] = opt.ID
	}

	options := make([]models.Option, 0, len(texts))
	used := make(map[string]struct{}, len(texts))
	for _, text := range texts {
		id, reused := existing[text]
		if _, taken := used[id]; !reused || taken {
			generated, err := newOptionID()
			if err != nil {
				return nil, apperr.Wrap(apperr.CodeInternal, "Could not update the poll.", err)
			}
			id = generated
		}
		used[id] = struct{}{}
		options = append(options, models.Option{ID: id, Text: text})
	}
	return options, nil
}

// validateExpiry checks an optional closing time.
func (s *PollService) validateExpiry(v *validation.Validator, expiresAt *time.Time) *time.Time {
	if expiresAt == nil {
		return nil
	}

	expiry := expiresAt.UTC()
	now := s.now()
	switch {
	case !expiry.After(now):
		v.Add("expiresAt", "The closing time must be in the future.")
		return nil
	case expiry.After(now.Add(MaxPollLifetime)):
		v.Add("expiresAt", "A poll can stay open for at most 30 days.")
		return nil
	}
	return &expiry
}

// newOptionID mints an unguessable option identifier.
//
// IDs are generated here, on the server, and never taken from the request. That
// is what makes vote validation a simple membership test: a client can only ever
// submit an option ID that this function produced for that poll.
func newOptionID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
