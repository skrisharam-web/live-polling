package services

import (
	"context"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// maxOptionIDLength bounds what the server will even look at. Option IDs are
// server-generated and short; anything longer is not a near miss, it is someone
// probing, and there is no reason to carry it as far as the database.
const maxOptionIDLength = 64

// PollReader is the poll lookup the vote path needs.
type PollReader interface {
	FindByID(ctx context.Context, id string) (*models.Poll, error)
}

// VoteStore persists and looks up votes.
type VoteStore interface {
	Create(ctx context.Context, vote *models.Vote) error
	FindByPollAndVoter(ctx context.Context, pollID bson.ObjectID, voterID string) (*models.Vote, error)
}

// Broadcaster announces a poll's new standing to everyone watching it.
type Broadcaster interface {
	PublishResults(ctx context.Context, results *Results)
}

// VoteService records votes and returns the resulting standing.
type VoteService struct {
	polls     PollReader
	votes     VoteStore
	results   *ResultService
	broadcast Broadcaster
	now       func() time.Time
}

// NewVoteService builds the service. broadcast may be nil, in which case votes
// are still recorded and results still correct — the application simply stops
// pushing them.
func NewVoteService(polls PollReader, votes VoteStore, results *ResultService, broadcast Broadcaster) *VoteService {
	return &VoteService{
		polls:     polls,
		votes:     votes,
		results:   results,
		broadcast: broadcast,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// Cast records one vote and returns the poll's new standing.
//
// The order of the checks below is the order of the canonical vote flow: identify
// the voter, validate the request, load the poll, check the option belongs to it,
// check the poll is still open, then let the database settle whether this voter
// has already voted. The duplicate check is deliberately last and is not a read:
// the unique (pollId, voterId) index decides it, which is the only way to get the
// right answer when two requests arrive at once.
func (s *VoteService) Cast(ctx context.Context, pollID, optionID, voterID string) (*Results, error) {
	if voterID == "" {
		return nil, apperr.New(apperr.CodeInternal, "Could not identify your browser. Please enable cookies and try again.")
	}
	if optionID == "" || utf8.RuneCountInString(optionID) > maxOptionIDLength {
		return nil, apperr.Validation("Choose one of the options.", map[string]string{
			"optionId": "Choose one of the options.",
		})
	}

	poll, err := s.polls.FindByID(ctx, pollID)
	if err != nil {
		return nil, err
	}

	// An option ID from a different poll is a real attack shape, not a typo, so
	// membership is checked against this poll's own options.
	if !poll.HasOption(optionID) {
		return nil, apperr.Validation("That option is not part of this poll.", map[string]string{
			"optionId": "Choose one of the options.",
		})
	}

	if !poll.AcceptsVotes(s.now()) {
		return nil, apperr.New(apperr.CodePollClosed, "This poll is closed, so votes are no longer being accepted.")
	}

	vote := &models.Vote{
		PollID:    poll.ID,
		OptionID:  optionID,
		VoterID:   voterID,
		CreatedAt: s.now(),
	}
	// MongoDB first. The vote is not acknowledged until it is durably stored, so
	// there is no state in which the application reports a vote that does not
	// exist. Everything after this line is cache maintenance.
	if err := s.votes.Create(ctx, vote); err != nil {
		return nil, err
	}

	s.results.RecordVote(ctx, poll, optionID)

	results, err := s.results.ForPoll(ctx, poll)
	if err != nil {
		return nil, err
	}

	// Announce the new standing to every viewer, wherever they are connected.
	// This is the step that makes the poll live; it is also the step that is
	// allowed to fail quietly, because the vote itself is already durable.
	if s.broadcast != nil {
		s.broadcast.PublishResults(ctx, results)
	}

	return results, nil
}

// Results returns a poll's standing for anyone holding the link.
func (s *VoteService) Results(ctx context.Context, pollID string) (*Results, error) {
	poll, err := s.polls.FindByID(ctx, pollID)
	if err != nil {
		return nil, err
	}
	return s.results.ForPoll(ctx, poll)
}

// VoteOf returns the option this voter already chose, or "" if they have not
// voted. It is what lets a returning voter see their own choice instead of an
// empty ballot they cannot use.
func (s *VoteService) VoteOf(ctx context.Context, pollID bson.ObjectID, voterID string) string {
	if voterID == "" {
		return ""
	}
	vote, err := s.votes.FindByPollAndVoter(ctx, pollID, voterID)
	if err != nil {
		// Not having voted is the common case, and a lookup failure must not stop
		// the results being shown, so both collapse to "no recorded choice".
		return ""
	}
	return vote.OptionID
}
