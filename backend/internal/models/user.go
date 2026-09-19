// Package models holds the domain types and their MongoDB representation.
//
// These structs are the shape of the data in the database, not the shape of the
// API. The HTTP layer maps them to response DTOs, which is what keeps fields
// like PasswordHash from ever reaching a client.
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// User is a registered account. Only registered users can create or manage polls;
// voting needs no account at all.
type User struct {
	ID   bson.ObjectID `bson:"_id,omitempty"`
	Name string        `bson:"name"`
	// Email is stored lower-cased and trimmed so the unique index actually means
	// "one account per address" rather than "one per spelling".
	Email string `bson:"email"`
	// PasswordHash is a bcrypt hash. The plaintext password is never stored,
	// logged, or returned, and this field has no json tag anywhere in the codebase.
	PasswordHash string    `bson:"passwordHash"`
	CreatedAt    time.Time `bson:"createdAt"`
	UpdatedAt    time.Time `bson:"updatedAt"`
}
