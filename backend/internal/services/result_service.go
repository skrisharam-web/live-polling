package services

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// VoteAggregator is the tally MongoDB can always produce from the votes
// collection. It is the definition of a correct result: everything else —
// the Redis counters added in the next phase — is a cache of this.
type VoteAggregator interface {
	CountsByPoll(ctx context.Context, pollID bson.ObjectID) ([]models.OptionCount, error)
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

// ResultService produces poll results.
type ResultService struct {
	votes VoteAggregator
	now   func() time.Time
}

func NewResultService(votes VoteAggregator) *ResultService {
	return &ResultService{votes: votes, now: func() time.Time { return time.Now().UTC() }}
}

// ForPoll tallies a poll.
//
// Every option is returned, in the order the poll declares them, including the
// ones nobody has chosen. A results view that silently omits zero-vote options
// would reorder itself as votes arrive and would make "no votes yet" look like
// "no options", so the shape of the response stays constant from the first
// render onwards.
func (s *ResultService) ForPoll(ctx context.Context, poll *models.Poll) (*Results, error) {
	counts, err := s.votes.CountsByPoll(ctx, poll.ID)
	if err != nil {
		return nil, err
	}

	byOption := make(map[string]int64, len(counts))
	for _, row := range counts {
		byOption[row.OptionID] = row.Count
	}

	return s.assemble(poll, byOption), nil
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
