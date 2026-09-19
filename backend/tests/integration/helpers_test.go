// Package integration exercises the real MongoDB and Redis servers rather than
// fakes, because the guarantees this application leans on — unique indexes,
// duplicate-key errors, Pub/Sub delivery — are properties of those servers and a
// mock would only ever confirm our own assumptions about them.
//
// The tests skip themselves when no server is configured, so `go test ./...`
// still works on a machine with nothing running:
//
//	docker compose up -d
//	TEST_MONGODB_URI=mongodb://localhost:27017 TEST_REDIS_URL=redis://localhost:6379/1 go test ./tests/...
package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/skrisharam-web/live-polling/backend/internal/database"
	"github.com/skrisharam-web/live-polling/backend/internal/redis"
)

// mongoURI returns the configured test server, or "" when integration tests
// should be skipped.
func mongoURI() string {
	if uri := os.Getenv("TEST_MONGODB_URI"); uri != "" {
		return uri
	}
	return os.Getenv("MONGODB_URI")
}

// newTestDB connects to MongoDB and hands back an empty, uniquely named database
// that is dropped when the test finishes. A per-test database means tests cannot
// see each other's documents and can run in any order.
func newTestDB(t *testing.T) *database.DB {
	t.Helper()

	uri := mongoURI()
	if uri == "" {
		t.Skip("set TEST_MONGODB_URI (e.g. mongodb://localhost:27017) to run integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	name := fmt.Sprintf("livepolling_test_%d_%d", time.Now().UnixNano(), os.Getpid())
	db, err := database.Connect(ctx, uri, name)
	if err != nil {
		t.Fatalf("connect to test mongodb: %v", err)
	}

	if err := database.EnsureIndexes(ctx, db); err != nil {
		t.Fatalf("create indexes on test database: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := db.Database.Drop(cleanupCtx); err != nil {
			t.Logf("could not drop test database %s: %v", name, err)
		}
		if err := db.Close(cleanupCtx); err != nil {
			t.Logf("could not disconnect from test mongodb: %v", err)
		}
	})

	return db
}

// testContext bounds every operation in a test so a hung server fails the test
// instead of the whole suite timing out.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// redisURL returns the configured test Redis server, or "" when Redis-backed
// tests should be skipped.
func redisURL() string {
	if url := os.Getenv("TEST_REDIS_URL"); url != "" {
		return url
	}
	return os.Getenv("REDIS_URL")
}

// newTestRedis connects to Redis and flushes the database it is pointed at, so
// each test starts with no counters. It returns nil when Redis is not
// configured, which is a supported mode: the result service then serves every
// read from MongoDB, and the tests that need Redis skip themselves.
//
// Point TEST_REDIS_URL at a scratch database (…/1), never at the one the running
// application uses, because this flushes it.
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()

	url := redisURL()
	if url == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := redis.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect to test redis: %v", err)
	}
	if err := client.Raw().FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush test redis: %v", err)
	}

	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Logf("could not close test redis: %v", err)
		}
	})
	return client
}

// requireRedis skips a test that cannot run without Redis.
func requireRedis(t *testing.T, client *redis.Client) *redis.Client {
	t.Helper()
	if client == nil {
		t.Skip("set TEST_REDIS_URL (e.g. redis://localhost:6379/1) to run Redis-backed tests")
	}
	return client
}
