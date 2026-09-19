package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Vote is one audience member's choice, and the durable record of it.
//
// Every accepted vote is a document here before Redis is touched, which is what
// makes MongoDB the source of truth: the Redis counters are an aggregate of this
// collection and can always be recomputed from it.
type Vote struct {
	ID     bson.ObjectID `bson:"_id,omitempty"`
	PollID bson.ObjectID `bson:"pollId"`
	// OptionID references Poll.Options[].ID, validated against the poll before insert.
	OptionID string `bson:"optionId"`
	// VoterID is the anonymous identity from the voter cookie, never supplied in
	// the request body. Together with PollID it forms the unique index that makes
	// duplicate voting impossible rather than merely unlikely.
	VoterID   string    `bson:"voterId"`
	CreatedAt time.Time `bson:"createdAt"`
}

// OptionCount is one row of an aggregated result.
type OptionCount struct {
	OptionID string `bson:"_id"`
	Count    int64  `bson:"count"`
}
