package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// fakeCounters stands in for Redis. It can be told to be cold, to fail, and it
// records what was written, so the tests can assert on the reconciliation
// behaviour rather than on Redis itself (which tests/integration covers against a
// real server).
type fakeCounters struct {
	data map[string]map[string]int64

	failRead      error
	failIncrement error
	failReplace   error

	replaceCalls   int
	incrementCalls int
	deleted        []string
}

func newFakeCounters() *fakeCounters {
	return &fakeCounters{data: make(map[string]map[string]int64)}
}

func (f *fakeCounters) IncrementOption(_ context.Context, pollID, optionID string) (bool, error) {
	f.incrementCalls++
	if f.failIncrement != nil {
		return false, f.failIncrement
	}
	counts, warm := f.data[pollID]
	if !warm {
		return false, nil
	}
	counts[optionID]++
	return true, nil
}

func (f *fakeCounters) Results(_ context.Context, pollID string) (map[string]int64, bool, error) {
	if f.failRead != nil {
		return nil, false, f.failRead
	}
	counts, warm := f.data[pollID]
	if !warm {
		return nil, false, nil
	}
	copied := make(map[string]int64, len(counts))
	for k, v := range counts {
		copied[k] = v
	}
	return copied, true, nil
}

func (f *fakeCounters) ReplaceResults(_ context.Context, pollID string, counts map[string]int64) error {
	f.replaceCalls++
	if f.failReplace != nil {
		return f.failReplace
	}
	copied := make(map[string]int64, len(counts))
	for k, v := range counts {
		copied[k] = v
	}
	f.data[pollID] = copied
	return nil
}

func (f *fakeCounters) DeleteResults(_ context.Context, pollID string) error {
	f.deleted = append(f.deleted, pollID)
	delete(f.data, pollID)
	return nil
}

type resultFixture struct {
	svc      *ResultService
	votes    *fakeVoteStore
	counters *fakeCounters
	poll     *models.Poll
}

func newResultFixture() *resultFixture {
	votes := newFakeVoteStore()
	counters := newFakeCounters()
	poll := &models.Poll{
		ID:       bson.NewObjectID(),
		Question: "Which release?",
		Options: []models.Option{
			{ID: "opt-a", Text: "With tests"},
			{ID: "opt-b", Text: "Without"},
		},
		Status: models.PollStatusActive,
	}
	return &resultFixture{
		svc:      NewResultService(votes, counters),
		votes:    votes,
		counters: counters,
		poll:     poll,
	}
}

// seedVotes writes votes straight into the durable store, bypassing Redis, which
// is exactly the state Redis has to be reconciled against.
func (f *resultFixture) seedVotes(t *testing.T, optionID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		err := f.votes.Create(context.Background(), &models.Vote{
			PollID:    f.poll.ID,
			OptionID:  optionID,
			VoterID:   bson.NewObjectID().Hex(),
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("seed vote: %v", err)
		}
	}
}

func totalOf(results *Results) int64 { return results.TotalVotes }

func countOf(results *Results, optionID string) int64 {
	for _, option := range results.Options {
		if option.OptionID == optionID {
			return option.Count
		}
	}
	return -1
}

func TestResultsAreServedFromRedisWhenWarm(t *testing.T) {
	ctx := context.Background()
	f := newResultFixture()

	// Warm the cache with a tally that does not match MongoDB, so the source of
	// the answer is unambiguous.
	f.counters.data[f.poll.ID.Hex()] = map[string]int64{"opt-a": 5, "opt-b": 2}

	results, err := f.svc.ForPoll(ctx, f.poll)
	if err != nil {
		t.Fatalf("ForPoll() = %v", err)
	}
	if countOf(results, "opt-a") != 5 || countOf(results, "opt-b") != 2 {
		t.Errorf("counts = %d/%d, want the Redis values 5/2", countOf(results, "opt-a"), countOf(results, "opt-b"))
	}
	if totalOf(results) != 7 {
		t.Errorf("TotalVotes = %d, want 7", totalOf(results))
	}
	if f.counters.replaceCalls != 0 {
		t.Errorf("a warm read triggered %d rebuilds, want 0", f.counters.replaceCalls)
	}
}

// TestColdCountersAreRebuiltFromMongo is the Redis-restart case: the counters are
// gone, and the correct answer still has to come out.
func TestColdCountersAreRebuiltFromMongo(t *testing.T) {
	ctx := context.Background()
	f := newResultFixture()
	f.seedVotes(t, "opt-a", 3)
	f.seedVotes(t, "opt-b", 1)

	results, err := f.svc.ForPoll(ctx, f.poll)
	if err != nil {
		t.Fatalf("ForPoll() = %v", err)
	}
	if countOf(results, "opt-a") != 3 || countOf(results, "opt-b") != 1 {
		t.Errorf("counts = %d/%d, want 3/1 from MongoDB", countOf(results, "opt-a"), countOf(results, "opt-b"))
	}

	t.Run("and Redis is repopulated on the way past", func(t *testing.T) {
		if f.counters.replaceCalls != 1 {
			t.Fatalf("rebuilds = %d, want 1", f.counters.replaceCalls)
		}
		stored := f.counters.data[f.poll.ID.Hex()]
		if stored["opt-a"] != 3 || stored["opt-b"] != 1 {
			t.Errorf("redis now holds %v, want opt-a=3 opt-b=1", stored)
		}
	})

	t.Run("so the next read is served warm", func(t *testing.T) {
		before := f.counters.replaceCalls
		if _, err := f.svc.ForPoll(ctx, f.poll); err != nil {
			t.Fatalf("ForPoll() = %v", err)
		}
		if f.counters.replaceCalls != before {
			t.Error("the second read rebuilt again; the cache is not being warmed")
		}
	})
}

// TestRedisFailureFallsBackToMongo covers the harder case: Redis is reachable
// enough to answer, but with an error. Results must still be correct.
func TestRedisFailureFallsBackToMongo(t *testing.T) {
	ctx := context.Background()
	f := newResultFixture()
	f.seedVotes(t, "opt-a", 2)
	f.counters.failRead = errors.New("connection refused")
	f.counters.failReplace = errors.New("connection refused")

	results, err := f.svc.ForPoll(ctx, f.poll)
	if err != nil {
		t.Fatalf("ForPoll() = %v, want results despite Redis being down", err)
	}
	if countOf(results, "opt-a") != 2 {
		t.Errorf("opt-a = %d, want 2 from MongoDB", countOf(results, "opt-a"))
	}
	if totalOf(results) != 2 {
		t.Errorf("TotalVotes = %d, want 2", totalOf(results))
	}
}

// TestRebuildRepairsDriftedCounters is the reconciliation guarantee: whatever
// Redis holds, a rebuild makes it agree with the durable data.
func TestRebuildRepairsDriftedCounters(t *testing.T) {
	ctx := context.Background()
	f := newResultFixture()
	f.seedVotes(t, "opt-a", 4)

	// Simulate drift: a plausible but wrong tally, including an option that no
	// longer exists.
	f.counters.data[f.poll.ID.Hex()] = map[string]int64{"opt-a": 99, "opt-b": 7, "opt-gone": 3}

	results, err := f.svc.Rebuild(ctx, f.poll)
	if err != nil {
		t.Fatalf("Rebuild() = %v", err)
	}
	if countOf(results, "opt-a") != 4 || countOf(results, "opt-b") != 0 {
		t.Errorf("counts = %d/%d, want 4/0", countOf(results, "opt-a"), countOf(results, "opt-b"))
	}
	if totalOf(results) != 4 {
		t.Errorf("TotalVotes = %d, want 4", totalOf(results))
	}

	stored := f.counters.data[f.poll.ID.Hex()]
	if stored["opt-a"] != 4 || stored["opt-b"] != 0 {
		t.Errorf("redis holds %v after rebuild, want opt-a=4 opt-b=0", stored)
	}
	if _, present := stored["opt-gone"]; present {
		t.Error("the rebuild kept a counter for an option the poll no longer has")
	}
}

// TestRecordVoteOnColdCountersRebuilds is the subtle one. Incrementing a cold key
// would create a hash holding exactly one vote and look authoritative, so a cold
// increment must turn into a rebuild instead.
func TestRecordVoteOnColdCountersRebuilds(t *testing.T) {
	ctx := context.Background()
	f := newResultFixture()

	// Five votes are already durable; Redis knows nothing about them.
	f.seedVotes(t, "opt-a", 5)
	// A sixth arrives and is stored durably, as the vote service does first.
	f.seedVotes(t, "opt-a", 1)

	f.svc.RecordVote(ctx, f.poll, "opt-a")

	if f.counters.replaceCalls != 1 {
		t.Fatalf("rebuilds = %d, want 1 after an increment against cold counters", f.counters.replaceCalls)
	}
	stored := f.counters.data[f.poll.ID.Hex()]
	if stored["opt-a"] != 6 {
		t.Errorf("redis holds opt-a=%d, want 6 — a cold increment must not publish a tally of 1", stored["opt-a"])
	}
}

func TestRecordVoteOnWarmCountersIncrements(t *testing.T) {
	ctx := context.Background()
	f := newResultFixture()
	f.counters.data[f.poll.ID.Hex()] = map[string]int64{"opt-a": 2, "opt-b": 1}

	f.svc.RecordVote(ctx, f.poll, "opt-a")

	if f.counters.replaceCalls != 0 {
		t.Errorf("a warm increment triggered %d rebuilds, want 0", f.counters.replaceCalls)
	}
	if got := f.counters.data[f.poll.ID.Hex()]["opt-a"]; got != 3 {
		t.Errorf("opt-a = %d, want 3", got)
	}
}

// TestRecordVoteSurvivesRedisFailure: the vote is already durable by this point,
// so a Redis failure must not turn into a failed vote.
func TestRecordVoteSurvivesRedisFailure(t *testing.T) {
	ctx := context.Background()
	f := newResultFixture()
	f.seedVotes(t, "opt-a", 1)
	f.counters.failIncrement = errors.New("connection refused")

	f.svc.RecordVote(ctx, f.poll, "opt-a")

	// And the result is still correct, because reading falls back to MongoDB.
	f.counters.failRead = errors.New("connection refused")
	results, err := f.svc.ForPoll(ctx, f.poll)
	if err != nil {
		t.Fatalf("ForPoll() = %v", err)
	}
	if totalOf(results) != 1 {
		t.Errorf("TotalVotes = %d, want 1", totalOf(results))
	}
}

func TestResultsWithoutRedisConfigured(t *testing.T) {
	votes := newFakeVoteStore()
	svc := NewResultService(votes, nil)
	poll := &models.Poll{
		ID:      bson.NewObjectID(),
		Options: []models.Option{{ID: "opt-a", Text: "A"}},
		Status:  models.PollStatusActive,
	}

	results, err := svc.ForPoll(context.Background(), poll)
	if err != nil {
		t.Fatalf("ForPoll() = %v", err)
	}
	if totalOf(results) != 0 {
		t.Errorf("TotalVotes = %d, want 0", totalOf(results))
	}
	// And the vote path must not panic when there is nothing to increment.
	svc.RecordVote(context.Background(), poll, "opt-a")
}
