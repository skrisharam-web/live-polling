// Package events defines the realtime event contract: the payload that travels
// from the backend that accepted a vote, through Redis Pub/Sub, to every backend
// holding a WebSocket connection, and on to the browser.
//
// It lives in its own package so the Redis layer can carry these messages without
// knowing what is in them, and so the service layer and the WebSocket layer agree
// on one definition of the wire format rather than two that drift.
package events

import (
	"encoding/json"
	"fmt"
	"time"
)

// TypePollResultsUpdated is the only event this application publishes. Naming the
// type in the payload costs a few bytes and means a second event type can be
// added later without the client having to guess from the shape.
const TypePollResultsUpdated = "poll_results_updated"

// OptionCount is one option's tally.
//
// Counts travel, percentages do not: the client already knows the option text
// from the poll it loaded, and a percentage computed in two places rounds in two
// places.
type OptionCount struct {
	OptionID string `json:"optionId"`
	Count    int64  `json:"count"`
}

// PollResultsUpdated says that a poll's standing has changed.
//
// It carries the whole tally rather than a delta. A delta would be smaller, but
// it would only be correct if every client had received every previous message in
// order — and a client that reconnects after a dropped connection has not. Sending
// the full standing makes every message self-sufficient, so a missed one costs
// nothing.
type PollResultsUpdated struct {
	Type       string        `json:"type"`
	PollID     string        `json:"pollId"`
	Results    []OptionCount `json:"results"`
	TotalVotes int64         `json:"totalVotes"`
	// Status lets a viewer see a poll close without polling for it.
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// Message is one raw Pub/Sub delivery: which poll it concerns and the bytes.
// The Redis layer deals only in this.
type Message struct {
	PollID  string
	Payload []byte
}

// Encode serialises an event for publication.
func Encode(event PollResultsUpdated) ([]byte, error) {
	if event.Type == "" {
		event.Type = TypePollResultsUpdated
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("encode %s event: %w", event.Type, err)
	}
	return payload, nil
}

// Decode parses a published payload.
//
// It is strict about the type field. Anything else on the channel was not put
// there by this application, and forwarding it to browsers unexamined would make
// the WebSocket a relay for whatever can write to Redis.
func Decode(payload []byte) (PollResultsUpdated, error) {
	var event PollResultsUpdated
	if err := json.Unmarshal(payload, &event); err != nil {
		return PollResultsUpdated{}, fmt.Errorf("decode event: %w", err)
	}
	if event.Type != TypePollResultsUpdated {
		return PollResultsUpdated{}, fmt.Errorf("unknown event type %q", event.Type)
	}
	if event.PollID == "" {
		return PollResultsUpdated{}, fmt.Errorf("event is missing a poll id")
	}
	return event, nil
}
