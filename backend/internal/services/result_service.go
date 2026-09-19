package services

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// VoteAggregator is the tally MongoDB can always produce from the votes
// collection. It is the definition of a correct result: the Redis counters are a
// cache of this, and are rebuilt from it whenever they are missing or suspect.
type VoteAggregator interface {
	CountsByPoll(ctx context.Context, pollID bson.ObjectID) ([]models.OptionCount, error)
}

// LiveCounters is the Redis side: the hot path for reads and the counter the
// vote path increments.
//
// Every method here is allowed to fail without the application failing. Redis
// holds derived data, so an error means "fall back to MongoDB", never "lose a
// vote".
type LiveCounters interface {
	IncrementOption(ctx context.Context, pollID, optionID string) (warm bool, err error)
	Results(ctx context.Context, pollID string) (counts map[string]int64, warm bool, err error)
	ReplaceResults(ctx context.Context, pollID string, counts map[string]int64) error
	DeleteResults(ctx context.Context, pollID string) error
}

// OptionResult is one option's standing.
type OptionResult struct {
	OptionID string
	Text     string
	Count    int64
}

// Results is a poll's full standing at a point in time.
type Results struct {
	PollID     string
	Options    []OptionResult
	TotalVotes int64
	Status     models.PollStatus
	ComputedAt time.Time
}

// ResultService produces poll results, reading from Redis where it can and from
// MongoDB where it must.
type ResultService struct {
	votes    VoteAggregator
	counters LiveCounters
	now      func() time.Time
}

// NewResultService builds the service. counters may be nil, in which case every
// read is served from MongoDB — which is slower but always correct, and is what
// the service degrades to when Redis is unreachable anyway.
func NewResultService(votes VoteAggregator, counters LiveCounters) *ResultService {
	return &ResultService{
		votes:    votes,
		counters: counters,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// ForPoll tallies a poll.
//
// Redis is asked first because this is the endpoint every viewer hits, and a
// single HGETALL is cheaper than an aggregation over every vote document. If
// Redis has nothing — a restart, an expired key, a poll whose counters were never
// built — or if Redis cannot be reached at all, the answer comes from MongoDB and
// the counters are rebuilt on the way past. Nothing here can return a wrong
// number: the fallback is the same query that defines the right one.
//
// Every option is returned in the order the poll declares them, including the
// ones nobody has chosen, so the shape of the response stays constant from the
// first render onwards rather than reshuffling as votes arrive.
func (s *ResultService) ForPoll(ctx context.Context, poll *models.Poll) (*Results, error) {
	if s.counters != nil {
		counts, warm, err := s.counters.Results(ctx, poll.ID.Hex())
		if err != nil {
			slog.Warn("redis results read failed, falling back to mongodb",
				"poll_id", poll.ID.Hex(), "error", err)
		} else if warm {
			return s.assemble(poll, counts), nil
		}
	}

	return s.Rebuild(ctx, poll)
}

// Rebuild recomputes a poll's counters from MongoDB and replaces whatever Redis
// currently holds.
//
// This is the reconciliation path the whole "Redis is a cache" claim rests on.
// It runs when the counters are missing or unreadable, and it can be called
// deliberately after a Redis incident. Because it derives everything from the
// votes collection, running it can never make the numbers worse — the worst case
// is that it costs one aggregation.
func (s *ResultService) Rebuild(ctx context.Context, poll *models.Poll) (*Results, error) {
	counts, err := s.votes.CountsByPoll(ctx, poll.ID)
	if err != nil {
		return nil, err
	}

	byOption := make(map[string]int64, len(poll.Options))
	// Seed every option at zero so the rebuilt hash is complete; a hash missing
	// its zero-vote options would be indistinguishable from a partial rebuild.
	for _, option := range poll.Options {
		byOption[option.ID] = 0
	}
	for _, row := range counts {
		if _, known := byOption[row.OptionID]; known {
			byOption[row.OptionID] = row.Count
		}
	}

	if s.counters != nil {
		if err := s.counters.ReplaceResults(ctx, poll.ID.Hex(), byOption); err != nil {
			// The caller still gets correct results; only the cache is stale.
			slog.Warn("could not refresh redis counters",
				"poll_id", poll.ID.Hex(), "error", err)
		}
	}

	return s.assemble(poll, byOption), nil
}

// RecordVote updates the live counters after a vote has been durably stored.
//
// The vote is already in MongoDB by the time this runs, which is what makes
// every failure here survivable: a dropped increment shows up as a stale count
// until the next rebuild, never as a lost vote.
func (s *ResultService) RecordVote(ctx context.Context, poll *models.Poll, optionID string) {
	if s.counters == nil {
		return
	}

	warm, err := s.counters.IncrementOption(ctx, poll.ID.Hex(), optionID)
	if err != nil {
		slog.Warn("could not increment redis counter",
			"poll_id", poll.ID.Hex(), "option_id", optionID, "error", err)
		return
	}
	if !warm {
		// The counters were cold, so incrementing would have produced a hash with
		// this one vote in it and nothing else. Rebuild instead.
		if _, err := s.Rebuild(ctx, poll); err != nil {
			slog.Warn("could not rebuild cold redis counters",
				"poll_id", poll.ID.Hex(), "error", err)
		}
	}
}

// DeleteResults discards a poll's counters.
func (s *ResultService) DeleteResults(ctx context.Context, pollID string) error {
	if s.counters == nil {
		return nil
	}
	return s.counters.DeleteResults(ctx, pollID)
}

// assemble turns a map of counts into the ordered, complete result set.
func (s *ResultService) assemble(poll *models.Poll, byOption map[string]int64) *Results {
	options := make([]OptionResult, 0, len(poll.Options))
	var total int64
	for _, option := range poll.Options {
		count := byOption[option.ID]
		// Counts for options that no longer exist are ignored rather than shown:
		// they cannot be rendered meaningfully and including them in the total
		// would make the percentages not add up.
		total += count
		options = append(options, OptionResult{OptionID: option.ID, Text: option.Text, Count: count})
	}

	now := s.now()
	return &Results{
		PollID:     poll.ID.Hex(),
		Options:    options,
		TotalVotes: total,
		Status:     poll.EffectiveStatus(now),
		ComputedAt: now,
	}
}
