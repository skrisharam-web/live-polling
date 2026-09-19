package redis

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/skrisharam-web/live-polling/backend/internal/events"
)

// updatesPattern matches every poll's update channel.
const updatesPattern = "poll:*:updates"

// subscriberBuffer is how many messages may queue between Redis and the hub
// before the oldest are dropped. Dropping is acceptable — each message carries a
// complete tally, so the next one restores the truth — and it is much better than
// letting a stalled consumer block the single subscriber for everyone.
const subscriberBuffer = 256

// SubscribePollUpdates opens one Pub/Sub subscription for the whole process and
// returns a channel of decoded-by-poll messages. It stops when ctx is cancelled.
//
// One pattern subscription per backend instance, not one per browser or per poll.
// A subscription per browser would mean a Redis connection per viewer, which is
// exactly how a realtime feature turns into an outage when a poll goes around an
// auditorium. The cost of the pattern is that an instance receives events for
// polls nobody is watching there and discards them; with a handful of instances
// that is far cheaper than managing subscriptions per room. If this ever ran at a
// scale where that traffic mattered, the change would be to subscribe and
// unsubscribe per poll as rooms open and close — the hub already knows when that
// happens.
func (c *Client) SubscribePollUpdates(ctx context.Context) (<-chan events.Message, error) {
	pubsub := c.rdb.PSubscribe(ctx, updatesPattern)

	// Wait for the subscription to be confirmed, so a caller that publishes
	// immediately afterwards cannot race ahead of it.
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("subscribe to poll updates: %w", err)
	}

	out := make(chan events.Message, subscriberBuffer)

	go func() {
		defer close(out)
		defer func() { _ = pubsub.Close() }()

		incoming := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-incoming:
				if !ok {
					return
				}
				pollID := pollIDFromChannel(message.Channel)
				if pollID == "" {
					continue
				}

				select {
				case out <- events.Message{PollID: pollID, Payload: []byte(message.Payload)}:
				default:
					// The consumer is behind. Drop this message rather than block
					// the subscriber that every poll depends on.
					slog.Warn("dropped a poll update; the realtime consumer is behind",
						"poll_id", pollID)
				}
			}
		}
	}()

	return out, nil
}

// pollIDFromChannel extracts the poll ID from "poll:{id}:updates", returning ""
// for anything that does not match.
func pollIDFromChannel(channel string) string {
	const prefix = "poll:"
	const suffix = ":updates"

	if !strings.HasPrefix(channel, prefix) || !strings.HasSuffix(channel, suffix) {
		return ""
	}
	return channel[len(prefix) : len(channel)-len(suffix)]
}
