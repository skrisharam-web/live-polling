package integration

import (
	"net/http"
	"testing"
)

// TestRedisCountersTrackTheVoteFlow follows a vote through the whole path and
// checks that Redis ends up agreeing with MongoDB.
func TestRedisCountersTrackTheVoteFlow(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	ctx := testContext(t)

	poll := seedPoll(t, api, "Does Redis follow along?", []string{"Yes", "No"})

	for i := 0; i < 3; i++ {
		voter := anonymousVisitor(api)
		if rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
			"optionId": poll.Options[0].ID,
		}); rec.Code != http.StatusCreated {
			t.Fatalf("vote %d status = %d", i, rec.Code)
		}
	}

	counts, warm, err := api.redis.Results(ctx, poll.ID)
	if err != nil {
		t.Fatalf("Results() = %v", err)
	}
	if !warm {
		t.Fatal("Redis holds no counters after three votes")
	}
	if counts[poll.Options[0].ID] != 3 {
		t.Errorf("redis counter = %d, want 3", counts[poll.Options[0].ID])
	}
	if counts[poll.Options[1].ID] != 0 {
		t.Errorf("the unchosen option reads %d, want an explicit 0", counts[poll.Options[1].ID])
	}
}

// TestResultsSurviveARedisFlush is the recovery drill the architecture claims to
// support, run for real: wipe Redis with votes already in flight, and check that
// the application answers correctly and repairs itself.
func TestResultsSurviveARedisFlush(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	ctx := testContext(t)

	poll := seedPoll(t, api, "What happens when Redis dies?", []string{"Nothing good", "Nothing bad"})

	// Two votes for the first option, one for the second.
	for _, optionIndex := range []int{0, 0, 1} {
		voter := anonymousVisitor(api)
		if rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
			"optionId": poll.Options[optionIndex].ID,
		}); rec.Code != http.StatusCreated {
			t.Fatalf("vote status = %d", rec.Code)
		}
	}

	// Redis loses everything, exactly as it would on a restart with no persistence.
	if err := api.redis.Raw().FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
	if _, warm, err := api.redis.Results(ctx, poll.ID); err != nil || warm {
		t.Fatalf("the counters survived the flush (warm=%v, err=%v)", warm, err)
	}

	t.Run("results are still correct, from MongoDB", func(t *testing.T) {
		rec, res := anonymousVisitor(api).do(http.MethodGet, "/api/polls/"+poll.ID+"/results", nil)
		assertStatus(t, rec, http.StatusOK)

		results := decodeResults(t, res)
		if results.TotalVotes != 3 {
			t.Fatalf("totalVotes = %d, want 3", results.TotalVotes)
		}
		if results.Results[0].Count != 2 || results.Results[1].Count != 1 {
			t.Errorf("counts = %d/%d, want 2/1", results.Results[0].Count, results.Results[1].Count)
		}
	})

	t.Run("and Redis has been rebuilt as a side effect", func(t *testing.T) {
		counts, warm, err := api.redis.Results(ctx, poll.ID)
		if err != nil {
			t.Fatalf("Results() = %v", err)
		}
		if !warm {
			t.Fatal("the counters were not rebuilt")
		}
		if counts[poll.Options[0].ID] != 2 || counts[poll.Options[1].ID] != 1 {
			t.Errorf("rebuilt counters = %v, want 2 and 1", counts)
		}
	})
}

// TestAVoteIntoColdCountersDoesNotPublishAPartialTally is the failure this design
// exists to prevent: Redis is wiped, then a vote arrives. A naive increment would
// leave Redis reporting a single vote for a poll that has four.
func TestAVoteIntoColdCountersDoesNotPublishAPartialTally(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	ctx := testContext(t)

	poll := seedPoll(t, api, "Cold start?", []string{"Handled", "Not handled"})

	for i := 0; i < 3; i++ {
		voter := anonymousVisitor(api)
		if rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
			"optionId": poll.Options[0].ID,
		}); rec.Code != http.StatusCreated {
			t.Fatalf("vote status = %d", rec.Code)
		}
	}

	if err := api.redis.Raw().FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}

	// A fourth vote arrives while the counters are cold.
	voter := anonymousVisitor(api)
	rec, res := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[1].ID,
	})
	assertStatus(t, rec, http.StatusCreated)

	results := decodeResults(t, res)
	if results.TotalVotes != 4 {
		t.Errorf("the voter was shown totalVotes = %d, want 4", results.TotalVotes)
	}
	if results.Results[0].Count != 3 || results.Results[1].Count != 1 {
		t.Errorf("counts = %d/%d, want 3/1", results.Results[0].Count, results.Results[1].Count)
	}

	counts, warm, err := api.redis.Results(ctx, poll.ID)
	if err != nil {
		t.Fatalf("Results() = %v", err)
	}
	if !warm {
		t.Fatal("the counters are still cold after a vote")
	}
	if counts[poll.Options[0].ID] != 3 {
		t.Errorf("redis holds %d for the earlier option, want 3 — the cold increment published a partial tally", counts[poll.Options[0].ID])
	}
}

// TestDeletingAPollClearsItsCounters closes the loop on cleanup.
func TestDeletingAPollClearsItsCounters(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	ctx := testContext(t)

	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("registration failed: %d", rec.Code)
	}
	status, poll := api.createPoll("Delete me", []string{"Yes", "No"})
	if status != http.StatusCreated {
		t.Fatalf("create poll status = %d", status)
	}

	voter := anonymousVisitor(api)
	if rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[0].ID,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("vote status = %d", rec.Code)
	}
	if _, warm, _ := api.redis.Results(ctx, poll.ID); !warm {
		t.Fatal("expected warm counters before the delete")
	}

	if rec, _ := api.do(http.MethodDelete, "/api/polls/"+poll.ID, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}

	if _, warm, err := api.redis.Results(ctx, poll.ID); err != nil || warm {
		t.Errorf("the deleted poll's counters are still in Redis (warm=%v, err=%v)", warm, err)
	}
}
