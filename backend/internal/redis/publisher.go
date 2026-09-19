package redis

import (
	"context"
	"fmt"
)

// PublishPollUpdate broadcasts a poll's new standing to every backend instance.
//
// Publishing is fire-and-forget by design: Redis Pub/Sub delivers to whoever is
// listening right now and keeps nothing for anyone who is not. That is the right
// trade here because the payload is a full tally rather than a delta, so a client
// that missed a message catches up completely on the next one — or on the refetch
// it performs after reconnecting.
func (c *Client) PublishPollUpdate(ctx context.Context, pollID string, payload []byte) error {
	if err := c.rdb.Publish(ctx, updatesChannel(pollID), payload).Err(); err != nil {
		return fmt.Errorf("publish poll update: %w", err)
	}
	return nil
}
