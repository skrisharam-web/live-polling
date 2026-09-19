package integration

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
	"github.com/skrisharam-web/live-polling/backend/internal/repositories"
)

func TestUserRepository(t *testing.T) {
	db := newTestDB(t)
	ctx := testContext(t)
	repo := repositories.NewUserRepository(db)

	user := &models.User{
		Name:         "Ada Lovelace",
		Email:        "ada@example.com",
		PasswordHash: "$2a$12$not-a-real-hash",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if user.ID.IsZero() {
		t.Fatal("Create() should populate the generated ID")
	}

	t.Run("duplicate email is a conflict", func(t *testing.T) {
		duplicate := &models.User{
			Name:         "Someone Else",
			Email:        "ada@example.com",
			PasswordHash: "$2a$12$another-hash",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Create(ctx, duplicate)
		if !apperr.Is(err, apperr.CodeConflict) {
			t.Fatalf("Create() with a duplicate e-mail = %v, want a CONFLICT domain error", err)
		}
	})

	t.Run("find by email", func(t *testing.T) {
		found, err := repo.FindByEmail(ctx, "ada@example.com")
		if err != nil {
			t.Fatalf("FindByEmail() = %v", err)
		}
		if found.ID != user.ID || found.Name != "Ada Lovelace" {
			t.Errorf("FindByEmail() returned %+v, want the created user", found)
		}
	})

	t.Run("find by id", func(t *testing.T) {
		found, err := repo.FindByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("FindByID() = %v", err)
		}
		if found.Email != "ada@example.com" {
			t.Errorf("FindByID() email = %q", found.Email)
		}
	})

	t.Run("unknown email is not found", func(t *testing.T) {
		_, err := repo.FindByEmail(ctx, "nobody@example.com")
		if !apperr.Is(err, apperr.CodeNotFound) {
			t.Fatalf("FindByEmail() for an unknown address = %v, want NOT_FOUND", err)
		}
	})

	t.Run("unknown id is not found", func(t *testing.T) {
		_, err := repo.FindByID(ctx, bson.NewObjectID())
		if !apperr.Is(err, apperr.CodeNotFound) {
			t.Fatalf("FindByID() for an unknown id = %v, want NOT_FOUND", err)
		}
	})
}

func newPoll(ownerID bson.ObjectID) *models.Poll {
	now := time.Now().UTC()
	return &models.Poll{
		OwnerID:  ownerID,
		Question: "Which release do we ship first?",
		Options: []models.Option{
			{ID: "opt1", Text: "The one with tests"},
			{ID: "opt2", Text: "The one without"},
		},
		Status:    models.PollStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestPollRepository(t *testing.T) {
	db := newTestDB(t)
	ctx := testContext(t)
	repo := repositories.NewPollRepository(db)

	owner := bson.NewObjectID()
	stranger := bson.NewObjectID()

	poll := newPoll(owner)
	if err := repo.Create(ctx, poll); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	t.Run("find by hex id", func(t *testing.T) {
		found, err := repo.FindByID(ctx, poll.ID.Hex())
		if err != nil {
			t.Fatalf("FindByID() = %v", err)
		}
		if found.Question != poll.Question || len(found.Options) != 2 {
			t.Errorf("FindByID() returned %+v", found)
		}
	})

	t.Run("a malformed id reads as not found", func(t *testing.T) {
		// Answering 404 rather than 400 keeps "that is not even an ID" and
		// "no such poll" indistinguishable to a caller probing for valid IDs.
		_, err := repo.FindByID(ctx, "definitely-not-an-object-id")
		if !apperr.Is(err, apperr.CodeNotFound) {
			t.Fatalf("FindByID() with a malformed id = %v, want NOT_FOUND", err)
		}
	})

	t.Run("list by owner is newest first", func(t *testing.T) {
		older := newPoll(owner)
		older.Question = "An older poll"
		older.CreatedAt = time.Now().UTC().Add(-time.Hour)
		if err := repo.Create(ctx, older); err != nil {
			t.Fatalf("Create() = %v", err)
		}
		if err := repo.Create(ctx, newPoll(stranger)); err != nil {
			t.Fatalf("Create() = %v", err)
		}

		polls, err := repo.ListByOwner(ctx, owner, 50)
		if err != nil {
			t.Fatalf("ListByOwner() = %v", err)
		}
		if len(polls) != 2 {
			t.Fatalf("ListByOwner() returned %d polls, want 2 (another user's poll must not appear)", len(polls))
		}
		if polls[0].CreatedAt.Before(polls[1].CreatedAt) {
			t.Error("ListByOwner() must return newest first")
		}
	})

	t.Run("update applies only the named fields", func(t *testing.T) {
		question := "Updated question"
		updated, err := repo.Update(ctx, poll.ID, owner, repositories.PollUpdate{Question: &question})
		if err != nil {
			t.Fatalf("Update() = %v", err)
		}
		if updated.Question != question {
			t.Errorf("Update() question = %q, want %q", updated.Question, question)
		}
		if len(updated.Options) != 2 {
			t.Error("Update() must leave untouched fields alone")
		}
		if !updated.UpdatedAt.After(poll.CreatedAt) {
			t.Error("Update() should move updatedAt forward")
		}
	})

	t.Run("update by a non-owner does not match", func(t *testing.T) {
		question := "Hijacked"
		_, err := repo.Update(ctx, poll.ID, stranger, repositories.PollUpdate{Question: &question})
		if !apperr.Is(err, apperr.CodeNotFound) {
			t.Fatalf("Update() by a non-owner = %v, want NOT_FOUND", err)
		}

		unchanged, err := repo.FindByID(ctx, poll.ID.Hex())
		if err != nil {
			t.Fatalf("FindByID() = %v", err)
		}
		if unchanged.Question == "Hijacked" {
			t.Fatal("a non-owner managed to modify the poll")
		}
	})

	t.Run("expiry can be set and cleared", func(t *testing.T) {
		expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
		withExpiry, err := repo.Update(ctx, poll.ID, owner, repositories.PollUpdate{ExpiresAt: &expiry})
		if err != nil {
			t.Fatalf("Update() = %v", err)
		}
		if withExpiry.ExpiresAt == nil || !withExpiry.ExpiresAt.Equal(expiry) {
			t.Fatalf("ExpiresAt = %v, want %v", withExpiry.ExpiresAt, expiry)
		}

		cleared, err := repo.Update(ctx, poll.ID, owner, repositories.PollUpdate{ClearExpiry: true})
		if err != nil {
			t.Fatalf("Update() = %v", err)
		}
		if cleared.ExpiresAt != nil {
			t.Errorf("ExpiresAt = %v, want nil after ClearExpiry", cleared.ExpiresAt)
		}
	})

	t.Run("delete is owner-scoped", func(t *testing.T) {
		victim := newPoll(owner)
		if err := repo.Create(ctx, victim); err != nil {
			t.Fatalf("Create() = %v", err)
		}

		if err := repo.Delete(ctx, victim.ID, stranger); !apperr.Is(err, apperr.CodeNotFound) {
			t.Fatalf("Delete() by a non-owner = %v, want NOT_FOUND", err)
		}
		if err := repo.Delete(ctx, victim.ID, owner); err != nil {
			t.Fatalf("Delete() by the owner = %v", err)
		}
		if _, err := repo.FindByID(ctx, victim.ID.Hex()); !apperr.Is(err, apperr.CodeNotFound) {
			t.Fatalf("FindByID() after delete = %v, want NOT_FOUND", err)
		}
	})
}

func TestVoteRepository(t *testing.T) {
	db := newTestDB(t)
	ctx := testContext(t)
	repo := repositories.NewVoteRepository(db)

	pollID := bson.NewObjectID()
	otherPollID := bson.NewObjectID()

	vote := &models.Vote{PollID: pollID, OptionID: "opt1", VoterID: "voter-a", CreatedAt: time.Now().UTC()}
	if err := repo.Create(ctx, vote); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	t.Run("the same voter cannot vote twice in one poll", func(t *testing.T) {
		second := &models.Vote{PollID: pollID, OptionID: "opt2", VoterID: "voter-a", CreatedAt: time.Now().UTC()}
		err := repo.Create(ctx, second)
		if !apperr.Is(err, apperr.CodeAlreadyVoted) {
			t.Fatalf("Create() for a repeat voter = %v, want ALREADY_VOTED", err)
		}
	})

	t.Run("the same voter can vote in a different poll", func(t *testing.T) {
		elsewhere := &models.Vote{PollID: otherPollID, OptionID: "opt1", VoterID: "voter-a", CreatedAt: time.Now().UTC()}
		if err := repo.Create(ctx, elsewhere); err != nil {
			t.Fatalf("Create() in another poll = %v", err)
		}
	})

	t.Run("find an existing vote", func(t *testing.T) {
		found, err := repo.FindByPollAndVoter(ctx, pollID, "voter-a")
		if err != nil {
			t.Fatalf("FindByPollAndVoter() = %v", err)
		}
		if found.OptionID != "opt1" {
			t.Errorf("OptionID = %q, want opt1", found.OptionID)
		}
	})

	t.Run("a voter who has not voted is not found", func(t *testing.T) {
		_, err := repo.FindByPollAndVoter(ctx, pollID, "voter-unknown")
		if !apperr.Is(err, apperr.CodeNotFound) {
			t.Fatalf("FindByPollAndVoter() = %v, want NOT_FOUND", err)
		}
	})

	t.Run("counts aggregate per option and ignore other polls", func(t *testing.T) {
		for _, voter := range []string{"voter-b", "voter-c"} {
			v := &models.Vote{PollID: pollID, OptionID: "opt2", VoterID: voter, CreatedAt: time.Now().UTC()}
			if err := repo.Create(ctx, v); err != nil {
				t.Fatalf("Create() = %v", err)
			}
		}

		counts, err := repo.CountsByPoll(ctx, pollID)
		if err != nil {
			t.Fatalf("CountsByPoll() = %v", err)
		}

		got := map[string]int64{}
		for _, c := range counts {
			got[c.OptionID] = c.Count
		}
		if got["opt1"] != 1 || got["opt2"] != 2 {
			t.Errorf("CountsByPoll() = %v, want opt1=1 opt2=2", got)
		}
	})

	t.Run("deleting a poll's votes leaves other polls alone", func(t *testing.T) {
		if err := repo.DeleteByPoll(ctx, pollID); err != nil {
			t.Fatalf("DeleteByPoll() = %v", err)
		}

		counts, err := repo.CountsByPoll(ctx, pollID)
		if err != nil {
			t.Fatalf("CountsByPoll() = %v", err)
		}
		if len(counts) != 0 {
			t.Errorf("CountsByPoll() after delete = %v, want empty", counts)
		}

		remaining, err := repo.CountsByPoll(ctx, otherPollID)
		if err != nil {
			t.Fatalf("CountsByPoll() = %v", err)
		}
		if len(remaining) != 1 {
			t.Errorf("the other poll's votes were deleted too: %v", remaining)
		}
	})
}
