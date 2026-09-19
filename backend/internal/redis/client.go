// Package redis owns everything this application does with Redis: the live
// result counters, the Pub/Sub channel that drives realtime updates, and the
// rate-limit counters.
//
// The package is deliberately named after the role it plays rather than the
// library it uses; the go-redis import is aliased so the distinction stays
// obvious at every call site.
package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Client wraps the go-redis client with the application's key conventions.
type Client struct {
	rdb *goredis.Client
}

// Connect parses a redis:// or rediss:// URL, opens the pool and pings it so a
// misconfigured URL fails at startup instead of on the first vote.
func Connect(ctx context.Context, url string) (*Client, error) {
	opts, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	opts.DialTimeout = 10 * time.Second
	opts.ReadTimeout = 5 * time.Second
	opts.WriteTimeout = 5 * time.Second

	rdb := goredis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

// Raw exposes the underlying client for the few places that need it (Pub/Sub
// subscription, rate-limit scripting).
func (c *Client) Raw() *goredis.Client { return c.rdb }

// Ping reports whether Redis is currently reachable; used by /health.
func (c *Client) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

// Close releases the connection pool.
func (c *Client) Close() error { return c.rdb.Close() }

// resultsKey is the hash holding the live per-option counters for a poll.
//
//	poll:{pollID}:results  ->  { optionID: count }
func resultsKey(pollID string) string { return "poll:" + pollID + ":results" }

// updatesChannel is the Pub/Sub channel carrying result updates for a poll.
//
//	poll:{pollID}:updates
func updatesChannel(pollID string) string { return "poll:" + pollID + ":updates" }

// ResultsKey exposes the counter key for documentation, tooling and tests.
func ResultsKey(pollID string) string { return resultsKey(pollID) }

// UpdatesChannel exposes the Pub/Sub channel name for tooling and tests.
func UpdatesChannel(pollID string) string { return updatesChannel(pollID) }
