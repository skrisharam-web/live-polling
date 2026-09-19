package integration

import (
	"testing"

	"github.com/skrisharam-web/live-polling/backend/internal/redis"
)

func TestRedisCounters(t *testing.T) {
	client := requireRedis(t, newTestRedis(t))
	ctx := testContext(t)
	const pollID = "poll-under-test"

	t.Run("an increment against cold counters is refused", func(t *testing.T) {
		// This is the property the whole design depends on: a plain HINCRBY would
		// happily create a hash holding one vote and nothing else.
		warm, err := client.IncrementOption(ctx, pollID, "opt-a")
		if err != nil {
			t.Fatalf("IncrementOption() = %v", err)
		}
		if warm {
			t.Fatal("IncrementOption() reported warm counters for a key that does not exist")
		}

		counts, warm, err := client.Results(ctx, pollID)
		if err != nil {
			t.Fatalf("Results() = %v", err)
		}
		if warm || len(counts) != 0 {
			t.Errorf("the refused increment created a key anyway: %v", counts)
		}
	})

	t.Run("replace establishes the counters, zeroes included", func(t *testing.T) {
		if err := client.ReplaceResults(ctx, pollID, map[string]int64{"opt-a": 3, "opt-b": 0}); err != nil {
			t.Fatalf("ReplaceResults() = %v", err)
		}

		counts, warm, err := client.Results(ctx, pollID)
		if err != nil {
			t.Fatalf("Results() = %v", err)
		}
		if !warm {
			t.Fatal("the counters are still cold after a replace")
		}
		if counts["opt-a"] != 3 {
			t.Errorf("opt-a = %d, want 3", counts["opt-a"])
		}
		// The zero has to be stored, otherwise a poll with no votes looks cold and
		// every read rebuilds it.
		if count, present := counts["opt-b"]; !present || count != 0 {
			t.Errorf("opt-b = %v (present: %v), want an explicit 0", count, present)
		}
	})

	t.Run("increments land once the counters are warm", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			warm, err := client.IncrementOption(ctx, pollID, "opt-b")
			if err != nil {
				t.Fatalf("IncrementOption() = %v", err)
			}
			if !warm {
				t.Fatal("IncrementOption() reported cold counters for a warm key")
			}
		}

		counts, _, err := client.Results(ctx, pollID)
		if err != nil {
			t.Fatalf("Results() = %v", err)
		}
		if counts["opt-b"] != 4 {
			t.Errorf("opt-b = %d, want 4", counts["opt-b"])
		}
		if counts["opt-a"] != 3 {
			t.Errorf("opt-a = %d, want the untouched 3", counts["opt-a"])
		}
	})

	t.Run("replace overwrites rather than merging", func(t *testing.T) {
		if err := client.ReplaceResults(ctx, pollID, map[string]int64{"opt-a": 1}); err != nil {
			t.Fatalf("ReplaceResults() = %v", err)
		}

		counts, _, err := client.Results(ctx, pollID)
		if err != nil {
			t.Fatalf("Results() = %v", err)
		}
		if len(counts) != 1 || counts["opt-a"] != 1 {
			t.Errorf("counts = %v, want only opt-a=1; a rebuild must not leave stale fields behind", counts)
		}
	})

	t.Run("delete clears the counters", func(t *testing.T) {
		if err := client.DeleteResults(ctx, pollID); err != nil {
			t.Fatalf("DeleteResults() = %v", err)
		}
		_, warm, err := client.Results(ctx, pollID)
		if err != nil {
			t.Fatalf("Results() = %v", err)
		}
		if warm {
			t.Error("the counters survived a delete")
		}
	})

	t.Run("a counter holding junk reads as cold", func(t *testing.T) {
		// Something other than this application wrote the key. Rebuilding from
		// MongoDB is the right answer; reporting a nonsense tally is not.
		if err := client.Raw().HSet(ctx, redis.ResultsKey(pollID), "opt-a", "not-a-number").Err(); err != nil {
			t.Fatalf("seed junk: %v", err)
		}
		_, warm, err := client.Results(ctx, pollID)
		if err != nil {
			t.Fatalf("Results() = %v", err)
		}
		if warm {
			t.Error("a non-numeric counter was reported as a usable tally")
		}
	})
}

func TestRedisCountersAreIsolatedPerPoll(t *testing.T) {
	client := requireRedis(t, newTestRedis(t))
	ctx := testContext(t)

	if err := client.ReplaceResults(ctx, "poll-one", map[string]int64{"opt-a": 2}); err != nil {
		t.Fatalf("ReplaceResults() = %v", err)
	}
	if err := client.ReplaceResults(ctx, "poll-two", map[string]int64{"opt-a": 9}); err != nil {
		t.Fatalf("ReplaceResults() = %v", err)
	}

	if err := client.DeleteResults(ctx, "poll-one"); err != nil {
		t.Fatalf("DeleteResults() = %v", err)
	}

	counts, warm, err := client.Results(ctx, "poll-two")
	if err != nil {
		t.Fatalf("Results() = %v", err)
	}
	if !warm || counts["opt-a"] != 9 {
		t.Errorf("poll-two = %v (warm: %v), want an untouched 9", counts, warm)
	}
}
