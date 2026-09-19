package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// rateLimitScript increments a fixed-window counter and returns the count along
// with the window's remaining life.
//
// The expiry is set only when the counter is created. Refreshing it on every
// request would slide the window forward forever, so a client sending a steady
// stream would never be let through again — a subtle way to turn a rate limiter
// into a permanent ban.
var rateLimitScript = goredis.NewScript(`
	local count = redis.call('INCR', KEYS[1])
	if count == 1 then
		redis.call('PEXPIRE', KEYS[1], ARGV[1])
	end
	return {count, redis.call('PTTL', KEYS[1])}
`)

// Allow records one hit against a fixed window and reports whether the caller is
// still within the limit, along with how long the current window has left.
//
// A fixed window is coarse — a client can send `limit` requests at the end of one
// window and `limit` more at the start of the next — but it costs a single round
// trip and it is enough for what it is here for: slowing down credential
// stuffing and vote spamming, not shaping traffic.
func (c *Client) Allow(ctx context.Context, key string, limit int64, window time.Duration) (allowed bool, retryAfter time.Duration, err error) {
	result, err := rateLimitScript.Run(ctx, c.rdb,
		[]string{"ratelimit:" + key},
		window.Milliseconds(),
	).Slice()
	if err != nil {
		return false, 0, fmt.Errorf("rate limit check: %w", err)
	}
	if len(result) != 2 {
		return false, 0, fmt.Errorf("rate limit check returned %d values, want 2", len(result))
	}

	count, _ := result[0].(int64)
	ttlMillis, _ := result[1].(int64)
	if ttlMillis < 0 {
		ttlMillis = window.Milliseconds()
	}

	return count <= limit, time.Duration(ttlMillis) * time.Millisecond, nil
}
