package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// resultsTTL bounds how long an untouched poll's counters occupy memory. Every
// write refreshes it, so an active poll never expires, while a poll nobody has
// looked at for a week releases its memory. Losing the key is safe by
// construction: MongoDB can rebuild it (see ReplaceResults), which is the same
// path a Redis restart takes.
const resultsTTL = 7 * 24 * time.Hour

// coldKey is what incrementIfWarm returns when the counter hash does not exist.
const coldKey = -1

// incrementIfWarmScript increments one option's counter, but only if the poll's
// hash already exists.
//
// This condition is the whole point. A plain HINCRBY on a missing key creates a
// hash containing exactly one field, so after a Redis restart the first vote
// would produce a counter set that looks complete and is wrong: one option with
// one vote, every earlier vote silently gone. Refusing to increment a cold key
// lets the caller notice and rebuild the whole set from MongoDB instead — and
// because the vote is written to MongoDB first, that rebuild already includes
// the vote that triggered it.
var incrementIfWarmScript = goredis.NewScript(`
	if redis.call('EXISTS', KEYS[1]) == 1 then
		local count = redis.call('HINCRBY', KEYS[1], ARGV[1], 1)
		redis.call('EXPIRE', KEYS[1], ARGV[2])
		return count
	end
	return -1
`)

// IncrementOption records one vote for an option in the live counters.
//
// It reports whether the counters were warm. A false means the caller should
// rebuild from MongoDB rather than trusting what is in Redis.
func (c *Client) IncrementOption(ctx context.Context, pollID, optionID string) (warm bool, err error) {
	result, err := incrementIfWarmScript.Run(ctx, c.rdb,
		[]string{resultsKey(pollID)},
		optionID, int64(resultsTTL.Seconds()),
	).Int64()
	if err != nil {
		return false, fmt.Errorf("increment poll counter: %w", err)
	}
	return result != coldKey, nil
}

// Results reads the live counters for a poll.
//
// The second return value distinguishes "this poll has no votes" from "Redis has
// nothing for this poll". They look identical in the returned map and mean
// completely different things: the first is an answer, the second means the
// answer has to come from MongoDB.
func (c *Client) Results(ctx context.Context, pollID string) (counts map[string]int64, warm bool, err error) {
	raw, err := c.rdb.HGetAll(ctx, resultsKey(pollID)).Result()
	if err != nil {
		return nil, false, fmt.Errorf("read poll counters: %w", err)
	}
	if len(raw) == 0 {
		return nil, false, nil
	}

	counts = make(map[string]int64, len(raw))
	for optionID, value := range raw {
		count, convErr := strconv.ParseInt(value, 10, 64)
		if convErr != nil {
			// A non-numeric counter means the key is not what this application
			// wrote. Treating it as cold makes the next read rebuild it from the
			// durable data rather than reporting a nonsense tally.
			return nil, false, nil
		}
		counts[optionID] = count
	}
	return counts, true, nil
}

// ReplaceResults overwrites a poll's counters with an authoritative tally.
//
// The delete and the writes go in one transaction, so a concurrent reader sees
// either the old counters or the new ones — never a half-built hash that would
// read as a poll losing most of its votes for a few milliseconds.
//
// Options with no votes are written as explicit zeroes. Without them a poll that
// nobody has voted in yet would have no key at all, every read would look cold,
// and the rebuild would run on every single request.
func (c *Client) ReplaceResults(ctx context.Context, pollID string, counts map[string]int64) error {
	key := resultsKey(pollID)

	values := make([]any, 0, len(counts)*2)
	for optionID, count := range counts {
		values = append(values, optionID, count)
	}

	_, err := c.rdb.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
		pipe.Del(ctx, key)
		if len(values) > 0 {
			pipe.HSet(ctx, key, values...)
			pipe.Expire(ctx, key, resultsTTL)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("replace poll counters: %w", err)
	}
	return nil
}

// DeleteResults discards a poll's counters, used when the poll itself is deleted.
func (c *Client) DeleteResults(ctx context.Context, pollID string) error {
	if err := c.rdb.Del(ctx, resultsKey(pollID)).Err(); err != nil {
		return fmt.Errorf("delete poll counters: %w", err)
	}
	return nil
}
