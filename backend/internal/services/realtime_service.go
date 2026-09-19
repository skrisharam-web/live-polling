package services

import (
	"context"
	"log/slog"

	"github.com/skrisharam-web/live-polling/backend/internal/events"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// EventPublisher sends a poll update to every backend instance.
type EventPublisher interface {
	PublishPollUpdate(ctx context.Context, pollID string, payload []byte) error
}

// EventSubscriber delivers the updates published anywhere in the cluster.
type EventSubscriber interface {
	SubscribePollUpdates(ctx context.Context) (<-chan events.Message, error)
}

// RealtimeService is the seam between the vote path and the realtime fan-out.
//
// It exists so that neither side has to know about the other: the vote service
// says "these are the new results" without knowing whether anyone is watching,
// and the WebSocket hub receives poll updates without knowing which instance
// produced them. Redis sits in the middle, which is what lets a vote accepted by
// one backend instance reach a browser connected to a different one.
type RealtimeService struct {
	publisher  EventPublisher
	subscriber EventSubscriber
}

// NewRealtimeService builds the service. Both collaborators may be nil, in which
// case publishing and subscribing become no-ops — the application still records
// votes and serves results, it simply stops being live.
func NewRealtimeService(publisher EventPublisher, subscriber EventSubscriber) *RealtimeService {
	return &RealtimeService{publisher: publisher, subscriber: subscriber}
}

// PublishResults announces a poll's new standing.
//
// A failure here is logged and swallowed. By the time this runs the vote is
// already durable and the voter has their answer; refusing the vote because the
// broadcast failed would turn a cosmetic problem into a lost vote. Viewers
// recover on their own — every client refetches results when its socket
// reconnects.
func (s *RealtimeService) PublishResults(ctx context.Context, results *Results) {
	if s.publisher == nil || results == nil {
		return
	}

	payload, err := events.Encode(events.PollResultsUpdated{
		Type:       events.TypePollResultsUpdated,
		PollID:     results.PollID,
		Results:    toEventCounts(results),
		TotalVotes: results.TotalVotes,
		Status:     string(results.Status),
		Timestamp:  results.ComputedAt,
	})
	if err != nil {
		slog.Error("could not encode a poll update", "poll_id", results.PollID, "error", err)
		return
	}

	if err := s.publisher.PublishPollUpdate(ctx, results.PollID, payload); err != nil {
		slog.Warn("could not publish a poll update", "poll_id", results.PollID, "error", err)
	}
}

// Subscribe returns a stream of decoded poll updates from anywhere in the
// cluster. The stream closes when ctx is cancelled.
//
// Undecodable payloads are logged and skipped rather than passed on: the channel
// is a Redis key pattern, and anything that can write to Redis could put
// arbitrary bytes there. Only messages this application recognises reach a
// browser.
func (s *RealtimeService) Subscribe(ctx context.Context) (<-chan events.PollResultsUpdated, error) {
	if s.subscriber == nil {
		// A closed channel is a valid empty stream: callers range over it and stop
		// immediately, with no special case for "realtime is not configured".
		empty := make(chan events.PollResultsUpdated)
		close(empty)
		return empty, nil
	}

	messages, err := s.subscriber.SubscribePollUpdates(ctx)
	if err != nil {
		return nil, err
	}

	out := make(chan events.PollResultsUpdated, cap(messages))
	go func() {
		defer close(out)
		for message := range messages {
			event, err := events.Decode(message.Payload)
			if err != nil {
				slog.Warn("discarded an unrecognised poll update",
					"poll_id", message.PollID, "error", err)
				continue
			}
			// The channel name is authoritative for which poll this is about, so a
			// payload claiming to be about a different poll is discarded rather
			// than delivered to that poll's viewers.
			if event.PollID != message.PollID {
				slog.Warn("discarded a poll update whose payload disagreed with its channel",
					"channel_poll_id", message.PollID, "payload_poll_id", event.PollID)
				continue
			}

			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

func toEventCounts(results *Results) []events.OptionCount {
	counts := make([]events.OptionCount, 0, len(results.Options))
	for _, option := range results.Options {
		counts = append(counts, events.OptionCount{OptionID: option.OptionID, Count: option.Count})
	}
	return counts
}

// ResultBroadcaster computes a poll's current standing and announces it.
//
// It exists so that "announce this poll" is one dependency rather than a
// callback wired up at the composition root. That distinction is not cosmetic:
// the callback version compiled fine with the wiring missing, and the close
// announcement was silently dead everywhere except the one place it had been
// hooked up. A constructor parameter cannot be forgotten.
type ResultBroadcaster struct {
	results  *ResultService
	realtime *RealtimeService
}

func NewResultBroadcaster(results *ResultService, realtime *RealtimeService) *ResultBroadcaster {
	return &ResultBroadcaster{results: results, realtime: realtime}
}

// PublishResults announces an already-computed standing. It satisfies the
// Broadcaster the vote path uses, where the results are in hand already.
func (b *ResultBroadcaster) PublishResults(ctx context.Context, results *Results) {
	if b == nil || b.realtime == nil {
		return
	}
	b.realtime.PublishResults(ctx, results)
}

// AnnouncePoll computes a poll's standing and announces it. Management actions
// use this: closing a poll changes what viewers should see, but the caller has no
// tally in hand.
func (b *ResultBroadcaster) AnnouncePoll(ctx context.Context, poll *models.Poll) {
	if b == nil || b.realtime == nil || b.results == nil {
		return
	}

	results, err := b.results.ForPoll(ctx, poll)
	if err != nil {
		slog.Warn("could not announce a poll change", "poll_id", poll.ID.Hex(), "error", err)
		return
	}
	b.realtime.PublishResults(ctx, results)
}
