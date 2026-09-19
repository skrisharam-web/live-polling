package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
	"github.com/skrisharam-web/live-polling/backend/internal/repositories"
)

// fakePollStore is an in-memory poll store. The real repository is covered
// against MongoDB in tests/integration; these tests are about the rules.
type fakePollStore struct {
	polls map[bson.ObjectID]*models.Poll
}

func newFakePollStore() *fakePollStore {
	return &fakePollStore{polls: make(map[bson.ObjectID]*models.Poll)}
}

func (f *fakePollStore) Create(_ context.Context, poll *models.Poll) error {
	poll.ID = bson.NewObjectID()
	stored := *poll
	f.polls[poll.ID] = &stored
	return nil
}

func (f *fakePollStore) FindByID(_ context.Context, id string) (*models.Poll, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperr.New(apperr.CodeNotFound, "Not found.")
	}
	poll, ok := f.polls[objectID]
	if !ok {
		return nil, apperr.New(apperr.CodeNotFound, "Poll not found.")
	}
	copied := *poll
	return &copied, nil
}

func (f *fakePollStore) ListByOwner(_ context.Context, ownerID bson.ObjectID, limit int64) ([]models.Poll, error) {
	var out []models.Poll
	for _, poll := range f.polls {
		if poll.OwnerID == ownerID && int64(len(out)) < limit {
			out = append(out, *poll)
		}
	}
	return out, nil
}

func (f *fakePollStore) Update(_ context.Context, id, ownerID bson.ObjectID, update repositories.PollUpdate) (*models.Poll, error) {
	poll, ok := f.polls[id]
	if !ok || poll.OwnerID != ownerID {
		return nil, apperr.New(apperr.CodeNotFound, "Poll not found.")
	}
	if update.Question != nil {
		poll.Question = *update.Question
	}
	if update.Options != nil {
		poll.Options = update.Options
	}
	if update.Status != nil {
		poll.Status = *update.Status
	}
	if update.ExpiresAt != nil {
		poll.ExpiresAt = update.ExpiresAt
	}
	if update.ClearExpiry {
		poll.ExpiresAt = nil
	}
	poll.UpdatedAt = time.Now().UTC()
	copied := *poll
	return &copied, nil
}

func (f *fakePollStore) Delete(_ context.Context, id, ownerID bson.ObjectID) error {
	poll, ok := f.polls[id]
	if !ok || poll.OwnerID != ownerID {
		return apperr.New(apperr.CodeNotFound, "Poll not found.")
	}
	delete(f.polls, id)
	return nil
}

// fakeVoteCounter records vote totals and what was deleted.
type fakeVoteCounter struct {
	counts  map[bson.ObjectID]int64
	deleted []bson.ObjectID
}

func newFakeVoteCounter() *fakeVoteCounter {
	return &fakeVoteCounter{counts: make(map[bson.ObjectID]int64)}
}

func (f *fakeVoteCounter) CountByPoll(_ context.Context, pollID bson.ObjectID) (int64, error) {
	return f.counts[pollID], nil
}

func (f *fakeVoteCounter) TotalsByPolls(_ context.Context, pollIDs []bson.ObjectID) (map[bson.ObjectID]int64, error) {
	totals := make(map[bson.ObjectID]int64, len(pollIDs))
	for _, id := range pollIDs {
		if count, ok := f.counts[id]; ok {
			totals[id] = count
		}
	}
	return totals, nil
}

func (f *fakeVoteCounter) DeleteByPoll(_ context.Context, pollID bson.ObjectID) error {
	f.deleted = append(f.deleted, pollID)
	delete(f.counts, pollID)
	return nil
}

// fakeResultsCleaner records Redis cleanups and can be told to fail.
type fakeResultsCleaner struct {
	deleted []string
	err     error
}

func (f *fakeResultsCleaner) DeleteResults(_ context.Context, pollID string) error {
	f.deleted = append(f.deleted, pollID)
	return f.err
}

type pollFixture struct {
	svc     *PollService
	polls   *fakePollStore
	votes   *fakeVoteCounter
	results *fakeResultsCleaner
	owner   bson.ObjectID
	other   bson.ObjectID
}

func newPollFixture() *pollFixture {
	polls := newFakePollStore()
	votes := newFakeVoteCounter()
	results := &fakeResultsCleaner{}
	return &pollFixture{
		svc:     NewPollService(polls, votes, results),
		polls:   polls,
		votes:   votes,
		results: results,
		owner:   bson.NewObjectID(),
		other:   bson.NewObjectID(),
	}
}

func (f *pollFixture) createPoll(t *testing.T) *models.Poll {
	t.Helper()
	poll, err := f.svc.Create(context.Background(), f.owner, CreatePollInput{
		Question: "Which release do we ship first?",
		Options:  []string{"The one with tests", "The one without"},
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	return poll
}

func TestCreatePoll(t *testing.T) {
	f := newPollFixture()
	poll := f.createPoll(t)

	t.Run("the creator becomes the owner", func(t *testing.T) {
		if poll.OwnerID != f.owner {
			t.Errorf("OwnerID = %v, want the authenticated user %v", poll.OwnerID, f.owner)
		}
	})

	t.Run("a new poll is active and accepts votes", func(t *testing.T) {
		if poll.Status != models.PollStatusActive {
			t.Errorf("Status = %q, want active", poll.Status)
		}
		if !poll.AcceptsVotes(time.Now().UTC()) {
			t.Error("a newly created poll should accept votes")
		}
	})

	t.Run("option IDs are generated server-side and are unique", func(t *testing.T) {
		seen := make(map[string]struct{})
		for _, opt := range poll.Options {
			if opt.ID == "" {
				t.Fatal("an option was stored without an ID")
			}
			if len(opt.ID) < 8 {
				t.Errorf("option ID %q is too short to be unguessable", opt.ID)
			}
			if _, duplicate := seen[opt.ID]; duplicate {
				t.Errorf("option ID %q was used twice", opt.ID)
			}
			seen[opt.ID] = struct{}{}
		}
	})

	t.Run("option text is cleaned", func(t *testing.T) {
		created, err := f.svc.Create(context.Background(), f.owner, CreatePollInput{
			Question: "  Tabs   or spaces?  ",
			Options:  []string{" Tabs ", "Spaces\n"},
		})
		if err != nil {
			t.Fatalf("Create() = %v", err)
		}
		if created.Question != "Tabs or spaces?" {
			t.Errorf("Question = %q, want the cleaned form", created.Question)
		}
		if created.Options[0].Text != "Tabs" || created.Options[1].Text != "Spaces" {
			t.Errorf("Options = %+v, want cleaned text", created.Options)
		}
	})
}

func TestCreatePollValidation(t *testing.T) {
	f := newPollFixture()
	future := time.Now().UTC().Add(time.Hour)
	past := time.Now().UTC().Add(-time.Hour)
	tooFar := time.Now().UTC().Add(MaxPollLifetime + time.Hour)

	cases := []struct {
		name      string
		input     CreatePollInput
		wantField string
	}{
		{"empty question", CreatePollInput{Question: "", Options: []string{"A", "B"}}, "question"},
		{"question too long", CreatePollInput{Question: strings.Repeat("a", 400), Options: []string{"A", "B"}}, "question"},
		{"one option", CreatePollInput{Question: "A real question?", Options: []string{"Only"}}, "options"},
		{"no options", CreatePollInput{Question: "A real question?"}, "options"},
		{"blank options do not count", CreatePollInput{Question: "A real question?", Options: []string{"A", "   ", ""}}, "options"},
		{"eleven options", CreatePollInput{Question: "A real question?", Options: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}}, "options"},
		{"duplicate options", CreatePollInput{Question: "A real question?", Options: []string{"Yes", "YES"}}, "options"},
		{"expiry in the past", CreatePollInput{Question: "A real question?", Options: []string{"A", "B"}, ExpiresAt: &past}, "expiresAt"},
		{"expiry beyond the maximum", CreatePollInput{Question: "A real question?", Options: []string{"A", "B"}, ExpiresAt: &tooFar}, "expiresAt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.Create(context.Background(), f.owner, tc.input)

			var domain *apperr.Error
			if !apperr.As(err, &domain) || domain.Code != apperr.CodeValidation {
				t.Fatalf("Create() = %v, want a VALIDATION_ERROR", err)
			}
			if _, ok := domain.Fields[tc.wantField]; !ok {
				t.Errorf("fields = %v, want an entry for %q", domain.Fields, tc.wantField)
			}
		})
	}

	t.Run("a valid future expiry is accepted", func(t *testing.T) {
		poll, err := f.svc.Create(context.Background(), f.owner, CreatePollInput{
			Question: "A real question?", Options: []string{"A", "B"}, ExpiresAt: &future,
		})
		if err != nil {
			t.Fatalf("Create() = %v", err)
		}
		if poll.ExpiresAt == nil || !poll.ExpiresAt.Equal(future) {
			t.Errorf("ExpiresAt = %v, want %v", poll.ExpiresAt, future)
		}
	})
}

// TestManagementRequiresOwnership is the core authorization test: every operation
// that changes a poll must refuse a signed-in user who does not own it.
func TestManagementRequiresOwnership(t *testing.T) {
	f := newPollFixture()
	poll := f.createPoll(t)
	id := poll.ID.Hex()
	ctx := context.Background()

	newQuestion := "Hijacked question"

	operations := map[string]func(actor bson.ObjectID) error{
		"read as owner": func(actor bson.ObjectID) error {
			_, err := f.svc.GetOwned(ctx, actor, id)
			return err
		},
		"update": func(actor bson.ObjectID) error {
			_, err := f.svc.Update(ctx, actor, id, UpdatePollInput{Question: &newQuestion})
			return err
		},
		"close": func(actor bson.ObjectID) error {
			_, err := f.svc.Close(ctx, actor, id)
			return err
		},
		"delete": func(actor bson.ObjectID) error {
			return f.svc.Delete(ctx, actor, id)
		},
	}

	for name, operation := range operations {
		t.Run(name+" is forbidden for a non-owner", func(t *testing.T) {
			err := operation(f.other)
			if !apperr.Is(err, apperr.CodeForbidden) {
				t.Fatalf("%s by a non-owner = %v, want FORBIDDEN", name, err)
			}
		})
	}

	t.Run("the poll is untouched after the attempts", func(t *testing.T) {
		stored, err := f.svc.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get() = %v", err)
		}
		if stored.Question != poll.Question {
			t.Errorf("the question changed to %q", stored.Question)
		}
		if stored.Status != models.PollStatusActive {
			t.Errorf("the status changed to %q", stored.Status)
		}
	})

	t.Run("an unknown poll is not found", func(t *testing.T) {
		_, err := f.svc.GetOwned(ctx, f.owner, bson.NewObjectID().Hex())
		if !apperr.Is(err, apperr.CodeNotFound) {
			t.Fatalf("GetOwned() for an unknown poll = %v, want NOT_FOUND", err)
		}
	})
}

func TestUpdatePoll(t *testing.T) {
	ctx := context.Background()

	t.Run("the question can be changed before any votes", func(t *testing.T) {
		f := newPollFixture()
		poll := f.createPoll(t)

		question := "Which release ships on Friday?"
		updated, err := f.svc.Update(ctx, f.owner, poll.ID.Hex(), UpdatePollInput{Question: &question})
		if err != nil {
			t.Fatalf("Update() = %v", err)
		}
		if updated.Question != question {
			t.Errorf("Question = %q, want %q", updated.Question, question)
		}
	})

	t.Run("editing an option keeps the IDs of options whose text is unchanged", func(t *testing.T) {
		f := newPollFixture()
		poll := f.createPoll(t)
		originalFirstID := poll.Options[0].ID

		updated, err := f.svc.Update(ctx, f.owner, poll.ID.Hex(), UpdatePollInput{
			Options: []string{"The one with tests", "The one with fewer tests"},
		})
		if err != nil {
			t.Fatalf("Update() = %v", err)
		}
		if updated.Options[0].ID != originalFirstID {
			t.Errorf("the unchanged option was given a new ID (%q → %q)", originalFirstID, updated.Options[0].ID)
		}
		if updated.Options[1].ID == poll.Options[1].ID {
			t.Error("the rewritten option should get a fresh ID")
		}
	})

	// Once a vote exists, the question and options are the thing those votes
	// answered. Changing them would silently rewrite history.
	t.Run("the question is frozen once a vote exists", func(t *testing.T) {
		f := newPollFixture()
		poll := f.createPoll(t)
		f.votes.counts[poll.ID] = 1

		question := "A completely different question?"
		_, err := f.svc.Update(ctx, f.owner, poll.ID.Hex(), UpdatePollInput{Question: &question})
		if !apperr.Is(err, apperr.CodeConflict) {
			t.Fatalf("Update() after a vote = %v, want CONFLICT", err)
		}
	})

	t.Run("the options are frozen once a vote exists", func(t *testing.T) {
		f := newPollFixture()
		poll := f.createPoll(t)
		f.votes.counts[poll.ID] = 1

		_, err := f.svc.Update(ctx, f.owner, poll.ID.Hex(), UpdatePollInput{Options: []string{"A", "B", "C"}})
		if !apperr.Is(err, apperr.CodeConflict) {
			t.Fatalf("Update() after a vote = %v, want CONFLICT", err)
		}
	})

	// Scheduling only affects what happens next, so it stays editable.
	t.Run("the expiry can still be changed after votes", func(t *testing.T) {
		f := newPollFixture()
		poll := f.createPoll(t)
		f.votes.counts[poll.ID] = 5

		expiry := time.Now().UTC().Add(2 * time.Hour)
		updated, err := f.svc.Update(ctx, f.owner, poll.ID.Hex(), UpdatePollInput{ExpiresAt: &expiry})
		if err != nil {
			t.Fatalf("Update() = %v", err)
		}
		if updated.ExpiresAt == nil {
			t.Fatal("ExpiresAt was not set")
		}
	})

	t.Run("the expiry can be cleared", func(t *testing.T) {
		f := newPollFixture()
		expiry := time.Now().UTC().Add(time.Hour)
		poll, err := f.svc.Create(ctx, f.owner, CreatePollInput{
			Question: "A real question?", Options: []string{"A", "B"}, ExpiresAt: &expiry,
		})
		if err != nil {
			t.Fatalf("Create() = %v", err)
		}

		updated, err := f.svc.Update(ctx, f.owner, poll.ID.Hex(), UpdatePollInput{ClearExpiry: true})
		if err != nil {
			t.Fatalf("Update() = %v", err)
		}
		if updated.ExpiresAt != nil {
			t.Errorf("ExpiresAt = %v, want nil", updated.ExpiresAt)
		}
	})

	t.Run("an empty patch is rejected", func(t *testing.T) {
		f := newPollFixture()
		poll := f.createPoll(t)

		_, err := f.svc.Update(ctx, f.owner, poll.ID.Hex(), UpdatePollInput{})
		if !apperr.Is(err, apperr.CodeValidation) {
			t.Fatalf("Update() with nothing set = %v, want VALIDATION_ERROR", err)
		}
	})

	t.Run("invalid content is rejected", func(t *testing.T) {
		f := newPollFixture()
		poll := f.createPoll(t)

		empty := ""
		_, err := f.svc.Update(ctx, f.owner, poll.ID.Hex(), UpdatePollInput{Question: &empty})
		if !apperr.Is(err, apperr.CodeValidation) {
			t.Fatalf("Update() with an empty question = %v, want VALIDATION_ERROR", err)
		}
	})
}

func TestClosePoll(t *testing.T) {
	ctx := context.Background()
	f := newPollFixture()
	poll := f.createPoll(t)

	closed, err := f.svc.Close(ctx, f.owner, poll.ID.Hex())
	if err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if closed.Status != models.PollStatusClosed {
		t.Errorf("Status = %q, want closed", closed.Status)
	}
	if closed.AcceptsVotes(time.Now().UTC()) {
		t.Error("a closed poll must not accept votes")
	}

	t.Run("closing again is not an error", func(t *testing.T) {
		again, err := f.svc.Close(ctx, f.owner, poll.ID.Hex())
		if err != nil {
			t.Fatalf("Close() on an already closed poll = %v", err)
		}
		if again.Status != models.PollStatusClosed {
			t.Errorf("Status = %q, want closed", again.Status)
		}
	})
}

func TestDeletePoll(t *testing.T) {
	ctx := context.Background()

	t.Run("deleting removes the poll, its votes and its cached results", func(t *testing.T) {
		f := newPollFixture()
		poll := f.createPoll(t)
		f.votes.counts[poll.ID] = 3

		if err := f.svc.Delete(ctx, f.owner, poll.ID.Hex()); err != nil {
			t.Fatalf("Delete() = %v", err)
		}
		if _, err := f.svc.Get(ctx, poll.ID.Hex()); !apperr.Is(err, apperr.CodeNotFound) {
			t.Errorf("the poll is still readable: %v", err)
		}
		if len(f.votes.deleted) != 1 || f.votes.deleted[0] != poll.ID {
			t.Errorf("votes deleted = %v, want exactly the poll's votes", f.votes.deleted)
		}
		if len(f.results.deleted) != 1 || f.results.deleted[0] != poll.ID.Hex() {
			t.Errorf("results cleared = %v, want the poll's Redis key", f.results.deleted)
		}
	})

	// Redis holds derived data only. If clearing it fails the delete has still
	// happened, and reporting an error would be misleading.
	t.Run("a Redis failure does not fail the delete", func(t *testing.T) {
		f := newPollFixture()
		f.results.err = apperr.New(apperr.CodeUnavailable, "Redis is unavailable.")
		poll := f.createPoll(t)

		if err := f.svc.Delete(ctx, f.owner, poll.ID.Hex()); err != nil {
			t.Fatalf("Delete() = %v, want success despite the Redis failure", err)
		}
		if _, err := f.svc.Get(ctx, poll.ID.Hex()); !apperr.Is(err, apperr.CodeNotFound) {
			t.Errorf("the poll survived the delete: %v", err)
		}
	})
}

func TestListForOwner(t *testing.T) {
	ctx := context.Background()
	f := newPollFixture()

	mine := f.createPoll(t)
	f.votes.counts[mine.ID] = 7

	theirs, err := f.svc.Create(ctx, f.other, CreatePollInput{
		Question: "Someone else's poll?", Options: []string{"A", "B"},
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	f.votes.counts[theirs.ID] = 99

	rows, err := f.svc.ListForOwner(ctx, f.owner)
	if err != nil {
		t.Fatalf("ListForOwner() = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListForOwner() returned %d rows, want only the caller's poll", len(rows))
	}
	if rows[0].Poll.ID != mine.ID {
		t.Errorf("returned poll %v, want %v", rows[0].Poll.ID, mine.ID)
	}
	if rows[0].TotalVotes != 7 {
		t.Errorf("TotalVotes = %d, want 7", rows[0].TotalVotes)
	}
}

func TestExpiredPollReadsAsClosed(t *testing.T) {
	f := newPollFixture()
	// Create the poll as though it were made two hours ago with a one-hour life,
	// so it is already expired without any sleeping.
	f.svc.now = func() time.Time { return time.Now().UTC().Add(-2 * time.Hour) }
	expiry := time.Now().UTC().Add(-time.Hour)

	poll, err := f.svc.Create(context.Background(), f.owner, CreatePollInput{
		Question: "Already over?", Options: []string{"A", "B"}, ExpiresAt: &expiry,
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	now := time.Now().UTC()
	if poll.Status != models.PollStatusActive {
		t.Errorf("the stored status should still be active, got %q", poll.Status)
	}
	if poll.EffectiveStatus(now) != models.PollStatusClosed {
		t.Error("an expired poll must read as closed")
	}
	if poll.AcceptsVotes(now) {
		t.Error("an expired poll must not accept votes")
	}
}
