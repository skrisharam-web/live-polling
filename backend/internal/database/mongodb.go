// Package database owns the MongoDB connection and the index definitions.
// MongoDB is the durable source of truth for this application: every user, poll
// and vote lives here, and the Redis layer is a derived cache that can always be
// rebuilt from these collections.
package database

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Collection names, referenced from one place so a typo cannot silently create
// a second, empty collection.
const (
	CollectionUsers = "users"
	CollectionPolls = "polls"
	CollectionVotes = "votes"
)

// DB wraps the driver handle with the resolved database.
type DB struct {
	Client   *mongo.Client
	Database *mongo.Database
}

// Connect opens a connection pool and verifies it with a ping, so a bad URI or an
// unreachable server fails at startup rather than on the first request.
func Connect(ctx context.Context, uri, dbName string) (*DB, error) {
	opts := options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(10 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetMaxPoolSize(50).
		SetRetryWrites(true)

	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("connect to mongodb: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}

	return &DB{Client: client, Database: client.Database(dbName)}, nil
}

// Collection returns a handle to a named collection.
func (d *DB) Collection(name string) *mongo.Collection { return d.Database.Collection(name) }

// Ping reports whether MongoDB is currently reachable; used by /health.
func (d *DB) Ping(ctx context.Context) error { return d.Client.Ping(ctx, readpref.Primary()) }

// Close releases the connection pool.
func (d *DB) Close(ctx context.Context) error { return d.Client.Disconnect(ctx) }
