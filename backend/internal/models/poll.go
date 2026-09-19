package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// PollStatus is the lifecycle state of a poll.
type PollStatus string

const (
	// PollStatusActive accepts votes.
	PollStatusActive PollStatus = "active"
	// PollStatusClosed rejects votes but still serves results.
	PollStatusClosed PollStatus = "closed"
)

// Valid reports whether s is a status the application recognises.
func (s PollStatus) Valid() bool {
	return s == PollStatusActive || s == PollStatusClosed
}

// Option is one choice within a poll.
//
// ID is generated server-side when the poll is created. A client can therefore
// only vote for an option that the server itself created, which removes a whole
// class of injection: there is no way to smuggle in an arbitrary option key and
// have it counted.
type Option struct {
	ID   string `bson:"id"`
	Text string `bson:"text"`
}

// Poll is a question with a fixed set of options, owned by the user who created it.
type Poll struct {
	ID       bson.ObjectID `bson:"_id,omitempty"`
	OwnerID  bson.ObjectID `bson:"ownerId"`
	Question string        `bson:"question"`
	Options  []Option      `bson:"options"`
	Status   PollStatus    `bson:"status"`

	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
	// ExpiresAt is optional. When set and in the past, the poll behaves as closed
	// even if its stored status is still "active", so expiry needs no background job.
	ExpiresAt *time.Time `bson:"expiresAt,omitempty"`
}

// HasOption reports whether optionID belongs to this poll. Vote validation runs
// through here, so an option ID copied from a different poll is rejected.
func (p *Poll) HasOption(optionID string) bool {
	for _, opt := range p.Options {
		if opt.ID == optionID {
			return true
		}
	}
	return false
}

// IsExpired reports whether the poll's optional expiry has passed.
func (p *Poll) IsExpired(now time.Time) bool {
	return p.ExpiresAt != nil && !now.Before(*p.ExpiresAt)
}

// AcceptsVotes reports whether a vote may be recorded right now. Both the stored
// status and the expiry are consulted, so there is one answer to "can this poll be
// voted on" and every caller gets it.
func (p *Poll) AcceptsVotes(now time.Time) bool {
	return p.Status == PollStatusActive && !p.IsExpired(now)
}

// EffectiveStatus is the status as a client should see it: an expired poll reads
// as closed even before anything has written that to the database.
func (p *Poll) EffectiveStatus(now time.Time) PollStatus {
	if p.Status == PollStatusActive && p.IsExpired(now) {
		return PollStatusClosed
	}
	return p.Status
}

// OptionIDs returns the option IDs in their declared order. The results layer uses
// this to render every option, including ones with zero votes.
func (p *Poll) OptionIDs() []string {
	ids := make([]string, 0, len(p.Options))
	for _, opt := range p.Options {
		ids = append(ids, opt.ID)
	}
	return ids
}
