package database

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// EnsureIndexes creates every index the application depends on. Index creation in
// MongoDB is idempotent, so this runs safely on every boot.
//
// Two of these indexes are correctness guarantees rather than optimisations:
//
//   - users.email (unique) makes "one account per e-mail" true even when two
//     registrations race, because the uniqueness check happens in the database
//     rather than in application code.
//   - votes.pollId+voterId (unique) is how duplicate voting is actually prevented.
//     A read-then-write check in Go would have a race window; a unique index does
//     not, so a concurrent double submit produces one insert and one duplicate-key
//     error that the vote service maps to 409.
//
// The remaining indexes serve real query paths: results aggregation and rebuild
// scan votes by pollId, and the dashboard lists a user's polls newest first.
func EnsureIndexes(ctx context.Context, db *DB) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	specs := []struct {
		collection string
		model      mongo.IndexModel
	}{
		{
			collection: CollectionUsers,
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "email", Value: 1}},
				Options: options.Index().SetName("uniq_email").SetUnique(true),
			},
		},
		{
			collection: CollectionVotes,
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "pollId", Value: 1}, {Key: "voterId", Value: 1}},
				Options: options.Index().SetName("uniq_poll_voter").SetUnique(true),
			},
		},
		{
			collection: CollectionVotes,
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "pollId", Value: 1}},
				Options: options.Index().SetName("poll_id"),
			},
		},
		{
			collection: CollectionPolls,
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "ownerId", Value: 1}, {Key: "createdAt", Value: -1}},
				Options: options.Index().SetName("owner_created_at"),
			},
		},
		{
			collection: CollectionPolls,
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "createdAt", Value: -1}},
				Options: options.Index().SetName("created_at"),
			},
		},
	}

	for _, spec := range specs {
		if _, err := db.Collection(spec.collection).Indexes().CreateOne(ctx, spec.model); err != nil {
			return fmt.Errorf("create index on %s: %w", spec.collection, err)
		}
	}
	return nil
}
