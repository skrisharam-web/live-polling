package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skrisharam-web/live-polling/backend/internal/events"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// fakeBus stands in for Redis Pub/Sub.
type fakeBus struct {
	published  []events.Message
	publishErr error

	incoming chan events.Message
	subErr   error
}

func newFakeBus() *fakeBus {
	return &fakeBus{incoming: make(chan events.Message, 8)}
}

func (f *fakeBus) PublishPollUpdate(_ context.Context, pollID string, payload []byte) error {
	if f.publishErr != nil {
		return f.publishErr
	}
	f.published = append(f.published, events.Message{PollID: pollID, Payload: payload})
	return nil
}

func (f *fakeBus) SubscribePollUpdates(_ context.Context) (<-chan events.Message, error) {
	if f.subErr != nil {
		return nil, f.subErr
	}
	return f.incoming, nil
}

func sampleResults() *Results {
	return &Results{
		PollID: "6aae43849751081b4d469065",
		Options: []OptionResult{
			{OptionID: "opt-a", Text: "With tests", Count: 3},
			{OptionID: "opt-b", Text: "Without", Count: 1},
		},
		TotalVotes: 4,
		Status:     models.PollStatusActive,
		ComputedAt: time.Now().UTC(),
	}
}

func TestPublishResults(t *testing.T) {
	bus := newFakeBus()
	svc := NewRealtimeService(bus, bus)

	svc.PublishResults(context.Background(), sampleResults())

	if len(bus.published) != 1 {
		t.Fatalf("published %d messages, want 1", len(bus.published))
	}
	if bus.published[0].PollID != "6aae43849751081b4d469065" {
		t.Errorf("published to poll %q", bus.published[0].PollID)
	}

	event, err := events.Decode(bus.published[0].Payload)
	if err != nil {
		t.Fatalf("the published payload does not decode: %v", err)
	}
	if event.TotalVotes != 4 || len(event.Results) != 2 {
		t.Errorf("event = %+v, want the full tally", event)
	}
	if event.Status != "active" {
		t.Errorf("Status = %q, want active", event.Status)
	}
}

// TestPublishFailureIsSwallowed: by the time this runs the vote is durable and
// the voter has their answer, so a broadcast failure must not become an error
// the caller has to handle.
func TestPublishFailureIsSwallowed(t *testing.T) {
	bus := newFakeBus()
	bus.publishErr = errors.New("connection refused")
	svc := NewRealtimeService(bus, bus)

	// The absence of a panic or a second return value is the assertion.
	svc.PublishResults(context.Background(), sampleResults())
}

func TestPublishWithoutAPublisherIsANoOp(t *testing.T) {
	svc := NewRealtimeService(nil, nil)
	svc.PublishResults(context.Background(), sampleResults())
}

func TestSubscribeDeliversDecodedEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	svc := NewRealtimeService(bus, bus)

	stream, err := svc.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}

	payload, err := events.Encode(events.PollResultsUpdated{
		PollID:     "poll-1",
		Results:    []events.OptionCount{{OptionID: "opt-a", Count: 2}},
		TotalVotes: 2,
		Status:     "active",
	})
	if err != nil {
		t.Fatalf("Encode() = %v", err)
	}
	bus.incoming <- events.Message{PollID: "poll-1", Payload: payload}

	select {
	case event := <-stream:
		if event.PollID != "poll-1" || event.TotalVotes != 2 {
			t.Errorf("event = %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
	}
}

// TestSubscribeDiscardsRubbish covers the case where something other than this
// application has written to the channel. Forwarding those bytes would make the
// WebSocket a relay for anything that can reach Redis.
func TestSubscribeDiscardsRubbish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	svc := NewRealtimeService(bus, bus)

	stream, err := svc.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}

	bus.incoming <- events.Message{PollID: "poll-1", Payload: []byte("not json at all")}
	bus.incoming <- events.Message{PollID: "poll-1", Payload: []byte(`{"type":"something_else","pollId":"poll-1"}`)}

	// A payload claiming to be about a different poll than the channel it arrived
	// on must not be delivered to that channel's viewers.
	mismatched, err := events.Encode(events.PollResultsUpdated{PollID: "poll-elsewhere", TotalVotes: 99})
	if err != nil {
		t.Fatalf("Encode() = %v", err)
	}
	bus.incoming <- events.Message{PollID: "poll-1", Payload: mismatched}

	// Then a good one, to prove the stream is still alive and the bad ones were
	// skipped rather than queued.
	good, err := events.Encode(events.PollResultsUpdated{PollID: "poll-1", TotalVotes: 5, Status: "active"})
	if err != nil {
		t.Fatalf("Encode() = %v", err)
	}
	bus.incoming <- events.Message{PollID: "poll-1", Payload: good}

	select {
	case event := <-stream:
		if event.TotalVotes != 5 {
			t.Errorf("the first delivered event was %+v; a discarded payload got through", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived; a bad payload stopped the stream")
	}
}

func TestSubscribeStopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	bus := newFakeBus()
	svc := NewRealtimeService(bus, bus)

	stream, err := svc.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}

	cancel()
	close(bus.incoming)

	select {
	case _, open := <-stream:
		if open {
			t.Error("the stream delivered an event after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the stream did not close after cancellation")
	}
}

func TestSubscribeWithoutASubscriberReturnsAClosedStream(t *testing.T) {
	svc := NewRealtimeService(nil, nil)

	stream, err := svc.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}
	// Callers range over this; it must terminate rather than block forever.
	for range stream {
		t.Fatal("a stream with no subscriber delivered an event")
	}
}
