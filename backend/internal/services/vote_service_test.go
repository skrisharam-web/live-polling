package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// fakeVoteStore models the unique (pollId, voterId) index in memory, so the
// service's duplicate handling is exercised the same way the database exercises
// it. The real index is verified against MongoDB in tests/integration.
type fakeVoteStore struct {
	votes map[string]*models.Vote
}

func newFakeVoteStore() *fakeVoteStore {
	return &fakeVoteStore{votes: make(map[string]*models.Vote)}
}

func voteKey(pollID bson.ObjectID, voterID string) string { return pollID.Hex() + "|" + voterID }

func (f *fakeVoteStore) Create(_ context.Context, vote *models.Vote) error {
	key := voteKey(vote.PollID, vote.VoterID)
	if _, exists := f.votes[key]; exists {
		return apperr.New(apperr.CodeAlreadyVoted, "You have already voted in this poll.")
	}
	vote.ID = bson.NewObjectID()
	stored := *vote
	f.votes[key] = &stored
	return nil
}

func (f *fakeVoteStore) FindByPollAndVoter(_ context.Context, pollID bson.ObjectID, voterID string) (*models.Vote, error) {
	if vote, ok := f.votes[voteKey(pollID, voterID)]; ok {
		return vote, nil
	}
	return nil, apperr.New(apperr.CodeNotFound, "No vote recorded.")
}

func (f *fakeVoteStore) CountsByPoll(_ context.Context, pollID bson.ObjectID) ([]models.OptionCount, error) {
	tally := make(map[string]int64)
	for _, vote := range f.votes {
		if vote.PollID == pollID {
			tally[vote.OptionID]++
		}
	}
	counts := make([]models.OptionCount, 0, len(tally))
	for optionID, count := range tally {
		counts = append(counts, models.OptionCount{OptionID: optionID, Count: count})
	}
	return counts, nil
}

type voteFixture struct {
	svc   *VoteService
	polls *fakePollStore
	votes *fakeVoteStore
	poll  *models.Poll
}

func newVoteFixture(t *testing.T) *voteFixture {
	t.Helper()

	polls := newFakePollStore()
	votes := newFakeVoteStore()
	poll := &models.Poll{
		OwnerID:  bson.NewObjectID(),
		Question: "Which release do we ship first?",
		Options: []models.Option{
			{ID: "opt-a", Text: "The one with tests"},
			{ID: "opt-b", Text: "The one without"},
		},
		Status:    models.PollStatusActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := polls.Create(context.Background(), poll); err != nil {
		t.Fatalf("seed poll: %v", err)
	}

	return &voteFixture{
		svc:   NewVoteService(polls, votes, NewResultService(votes)),
		polls: polls,
		votes: votes,
		poll:  poll,
	}
}

func TestCastVote(t *testing.T) {
	ctx := context.Background()
	f := newVoteFixture(t)

	results, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "opt-a", "voter-1")
	if err != nil {
		t.Fatalf("Cast() = %v", err)
	}

	t.Run("the standing comes back with the vote", func(t *testing.T) {
		if results.TotalVotes != 1 {
			t.Errorf("TotalVotes = %d, want 1", results.TotalVotes)
		}
		if len(results.Options) != 2 {
			t.Fatalf("got %d options, want every option including the unchosen one", len(results.Options))
		}
		if results.Options[0].Count != 1 || results.Options[1].Count != 0 {
			t.Errorf("counts = %d/%d, want 1/0", results.Options[0].Count, results.Options[1].Count)
		}
	})

	t.Run("options keep their declared order", func(t *testing.T) {
		if results.Options[0].OptionID != "opt-a" || results.Options[1].OptionID != "opt-b" {
			t.Errorf("order = %q, %q; want the poll's own order", results.Options[0].OptionID, results.Options[1].OptionID)
		}
	})

	t.Run("the option text travels with the count", func(t *testing.T) {
		if results.Options[0].Text != "The one with tests" {
			t.Errorf("Text = %q", results.Options[0].Text)
		}
	})

	t.Run("a different voter can still vote", func(t *testing.T) {
		second, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "opt-b", "voter-2")
		if err != nil {
			t.Fatalf("Cast() for a second voter = %v", err)
		}
		if second.TotalVotes != 2 {
			t.Errorf("TotalVotes = %d, want 2", second.TotalVotes)
		}
	})

	t.Run("the voter's own choice can be read back", func(t *testing.T) {
		if got := f.svc.VoteOf(ctx, f.poll.ID, "voter-1"); got != "opt-a" {
			t.Errorf("VoteOf() = %q, want opt-a", got)
		}
		if got := f.svc.VoteOf(ctx, f.poll.ID, "voter-never"); got != "" {
			t.Errorf("VoteOf() for a voter who has not voted = %q, want empty", got)
		}
	})
}

func TestCastVoteRejections(t *testing.T) {
	ctx := context.Background()

	t.Run("a repeat vote from the same browser is refused", func(t *testing.T) {
		f := newVoteFixture(t)
		if _, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "opt-a", "voter-1"); err != nil {
			t.Fatalf("first Cast() = %v", err)
		}

		_, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "opt-b", "voter-1")
		if !apperr.Is(err, apperr.CodeAlreadyVoted) {
			t.Fatalf("second Cast() = %v, want ALREADY_VOTED", err)
		}

		// The refused vote must not have moved anything.
		results, err := f.svc.Results(ctx, f.poll.ID.Hex())
		if err != nil {
			t.Fatalf("Results() = %v", err)
		}
		if results.TotalVotes != 1 {
			t.Errorf("TotalVotes = %d after a refused duplicate, want 1", results.TotalVotes)
		}
	})

	t.Run("an option from another poll is refused", func(t *testing.T) {
		f := newVoteFixture(t)
		other := &models.Poll{
			Question: "Another poll?",
			Options:  []models.Option{{ID: "foreign-option", Text: "Elsewhere"}},
			Status:   models.PollStatusActive,
		}
		if err := f.polls.Create(ctx, other); err != nil {
			t.Fatalf("seed second poll: %v", err)
		}

		_, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "foreign-option", "voter-1")
		if !apperr.Is(err, apperr.CodeValidation) {
			t.Fatalf("Cast() with another poll's option = %v, want VALIDATION_ERROR", err)
		}
	})

	t.Run("an unknown option is refused", func(t *testing.T) {
		f := newVoteFixture(t)
		_, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "opt-does-not-exist", "voter-1")
		if !apperr.Is(err, apperr.CodeValidation) {
			t.Fatalf("Cast() = %v, want VALIDATION_ERROR", err)
		}
	})

	t.Run("an empty option is refused", func(t *testing.T) {
		f := newVoteFixture(t)
		_, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "", "voter-1")
		if !apperr.Is(err, apperr.CodeValidation) {
			t.Fatalf("Cast() = %v, want VALIDATION_ERROR", err)
		}
	})

	t.Run("an absurdly long option id is refused before any lookup", func(t *testing.T) {
		f := newVoteFixture(t)
		_, err := f.svc.Cast(ctx, f.poll.ID.Hex(), strings.Repeat("a", 5000), "voter-1")
		if !apperr.Is(err, apperr.CodeValidation) {
			t.Fatalf("Cast() = %v, want VALIDATION_ERROR", err)
		}
	})

	t.Run("a closed poll refuses votes", func(t *testing.T) {
		f := newVoteFixture(t)
		closed := models.PollStatusClosed
		if _, err := f.polls.Update(ctx, f.poll.ID, f.poll.OwnerID, repoUpdateStatus(closed)); err != nil {
			t.Fatalf("close poll: %v", err)
		}

		_, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "opt-a", "voter-1")
		if !apperr.Is(err, apperr.CodePollClosed) {
			t.Fatalf("Cast() on a closed poll = %v, want POLL_CLOSED", err)
		}
	})

	t.Run("an expired poll refuses votes", func(t *testing.T) {
		f := newVoteFixture(t)
		past := time.Now().UTC().Add(-time.Minute)
		if _, err := f.polls.Update(ctx, f.poll.ID, f.poll.OwnerID, repoUpdateExpiry(&past)); err != nil {
			t.Fatalf("expire poll: %v", err)
		}

		_, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "opt-a", "voter-1")
		if !apperr.Is(err, apperr.CodePollClosed) {
			t.Fatalf("Cast() on an expired poll = %v, want POLL_CLOSED", err)
		}
	})

	t.Run("an unknown poll is not found", func(t *testing.T) {
		f := newVoteFixture(t)
		_, err := f.svc.Cast(ctx, bson.NewObjectID().Hex(), "opt-a", "voter-1")
		if !apperr.Is(err, apperr.CodeNotFound) {
			t.Fatalf("Cast() = %v, want NOT_FOUND", err)
		}
	})

	t.Run("a request with no voter identity is refused", func(t *testing.T) {
		f := newVoteFixture(t)
		_, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "opt-a", "")
		if err == nil {
			t.Fatal("Cast() without a voter identity must not succeed")
		}
	})
}

// TestValidationHappensBeforePersistence pins the order of the checks: a vote
// that is invalid for any reason must leave no trace, because a rejected vote
// that still moved a counter is worse than no vote at all.
func TestValidationHappensBeforePersistence(t *testing.T) {
	ctx := context.Background()
	f := newVoteFixture(t)

	attempts := []struct {
		name     string
		optionID string
	}{
		{"unknown option", "nope"},
		{"empty option", ""},
		{"over-long option", strings.Repeat("x", 500)},
	}

	for _, attempt := range attempts {
		if _, err := f.svc.Cast(ctx, f.poll.ID.Hex(), attempt.optionID, "voter-1"); err == nil {
			t.Fatalf("%s should have been refused", attempt.name)
		}
	}

	results, err := f.svc.Results(ctx, f.poll.ID.Hex())
	if err != nil {
		t.Fatalf("Results() = %v", err)
	}
	if results.TotalVotes != 0 {
		t.Errorf("TotalVotes = %d after only invalid attempts, want 0", results.TotalVotes)
	}

	// And the voter is still free to cast a real vote afterwards.
	if _, err := f.svc.Cast(ctx, f.poll.ID.Hex(), "opt-a", "voter-1"); err != nil {
		t.Fatalf("a valid vote after failed attempts = %v", err)
	}
}

func TestResultsForAPollWithNoVotes(t *testing.T) {
	f := newVoteFixture(t)

	results, err := f.svc.Results(context.Background(), f.poll.ID.Hex())
	if err != nil {
		t.Fatalf("Results() = %v", err)
	}
	if results.TotalVotes != 0 {
		t.Errorf("TotalVotes = %d, want 0", results.TotalVotes)
	}
	// Every option is still listed at zero: an empty poll renders the same shape
	// as a busy one, so nothing jumps around when the first vote lands.
	if len(results.Options) != 2 {
		t.Fatalf("got %d options, want 2", len(results.Options))
	}
	for _, option := range results.Options {
		if option.Count != 0 {
			t.Errorf("option %s has count %d, want 0", option.OptionID, option.Count)
		}
	}
	if results.Status != models.PollStatusActive {
		t.Errorf("Status = %q, want active", results.Status)
	}
}
