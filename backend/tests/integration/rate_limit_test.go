package integration

import (
	"net/http"
	"testing"
	"time"
)

// TestRateLimitBlocksABurst drives the limiter through the real middleware and a
// real Redis, because the interesting parts — the counter being shared and the
// window expiring — are Redis behaviour, not Go behaviour.
func TestRateLimitBlocksABurst(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	ctx := testContext(t)

	const (
		limit  = 3
		window = 2 * time.Second
	)

	var blocked int
	for i := 0; i < limit+2; i++ {
		allowed, retryAfter, err := api.redis.Allow(ctx, "test-scope:198.51.100.7", limit, window)
		if err != nil {
			t.Fatalf("Allow() = %v", err)
		}
		if !allowed {
			blocked++
			if retryAfter <= 0 || retryAfter > window {
				t.Errorf("retryAfter = %v, want something inside the window", retryAfter)
			}
		}
	}
	if blocked != 2 {
		t.Errorf("%d requests were blocked, want 2 (the two beyond the limit of %d)", blocked, limit)
	}

	t.Run("a different client has its own allowance", func(t *testing.T) {
		allowed, _, err := api.redis.Allow(ctx, "test-scope:203.0.113.9", limit, window)
		if err != nil {
			t.Fatalf("Allow() = %v", err)
		}
		if !allowed {
			t.Error("one client's burst exhausted another client's allowance")
		}
	})

	t.Run("a different scope has its own allowance", func(t *testing.T) {
		allowed, _, err := api.redis.Allow(ctx, "other-scope:198.51.100.7", limit, window)
		if err != nil {
			t.Fatalf("Allow() = %v", err)
		}
		if !allowed {
			t.Error("hitting one endpoint's limit blocked a different endpoint")
		}
	})
}

// TestRateLimitWindowExpires checks that the window is fixed rather than sliding
// forward on every request, which would lock a busy client out permanently.
func TestRateLimitWindowExpires(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	ctx := testContext(t)

	const (
		limit  = 2
		window = 600 * time.Millisecond
	)
	key := "expiry-scope:198.51.100.22"

	for i := 0; i < limit; i++ {
		if allowed, _, err := api.redis.Allow(ctx, key, limit, window); err != nil || !allowed {
			t.Fatalf("request %d: allowed=%v err=%v", i, allowed, err)
		}
	}
	if allowed, _, _ := api.redis.Allow(ctx, key, limit, window); allowed {
		t.Fatal("the limiter did not block the request beyond the limit")
	}

	// Keep sending while the window runs out: a sliding expiry would keep pushing
	// the reset away and this would never recover.
	deadline := time.Now().Add(3 * window)
	recovered := false
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if allowed, _, err := api.redis.Allow(ctx, key, limit, window); err == nil && allowed {
			recovered = true
			break
		}
	}
	if !recovered {
		t.Error("the client never recovered; the window is sliding rather than fixed")
	}
}

// TestVoteEndpointIsRateLimited proves the middleware is actually mounted, rather
// than just tested in isolation.
func TestVoteEndpointIsRateLimited(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)

	poll := seedPoll(t, api, "Can I spam this?", []string{"Yes", "No"})

	// The limiter is keyed by client IP, and httptest gives every request the
	// same one, so a burst from many "browsers" still shares an allowance.
	var lastStatus int
	var sawLimit bool
	for i := 0; i < 70; i++ {
		voter := anonymousVisitor(api)
		rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
			"optionId": poll.Options[0].ID,
		})
		lastStatus = rec.Code
		if rec.Code == http.StatusTooManyRequests {
			sawLimit = true
			if retry := rec.Header().Get("Retry-After"); retry == "" {
				t.Error("a 429 was returned without a Retry-After header")
			}
			break
		}
	}

	if !sawLimit {
		t.Errorf("70 votes from one address were all accepted (last status %d); the limiter is not mounted", lastStatus)
	}
}

func TestRateLimitFailsOpenWhenRedisIsUnavailable(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)

	// Closing the client makes every limiter call fail, which is the shape of a
	// Redis outage. Voting must continue to work.
	if err := api.redis.Close(); err != nil {
		t.Fatalf("close redis: %v", err)
	}

	poll := seedPoll(t, api, "Does the site survive Redis?", []string{"Yes", "No"})
	voter := anonymousVisitor(api)
	rec, res := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[0].ID,
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("vote status = %d (%v); a Redis outage must not stop voting", rec.Code, res.Error)
	}
	if got := decodeResults(t, res).TotalVotes; got != 1 {
		t.Errorf("totalVotes = %d, want 1 from MongoDB", got)
	}
}
