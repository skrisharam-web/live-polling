package websocket

import (
	"context"
	"log/slog"

	"github.com/skrisharam-web/live-polling/backend/internal/events"
)

// EventStream is the source of poll updates — in production, the Redis
// subscription. Stating it as an interface keeps this package independent of
// Redis and lets the tests drive the hub from a plain channel.
type EventStream interface {
	Subscribe(ctx context.Context) (<-chan events.PollResultsUpdated, error)
}

// Manager connects the event stream to the hub: it subscribes once per process,
// re-encodes each event for the browser and hands it to the hub for fan-out.
//
// The re-encode is deliberate. The payload that travels over Redis is an internal
// contract between backend instances; what goes to a browser is a public one.
// Passing the Redis bytes straight through would make every future change to the
// internal event a breaking change for every connected client.
type Manager struct {
	hub    *Hub
	stream EventStream
}

func NewManager(hub *Hub, stream EventStream) *Manager {
	return &Manager{hub: hub, stream: stream}
}

// Run starts the hub and pumps events into it until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	go m.hub.Run(ctx)

	updates, err := m.stream.Subscribe(ctx)
	if err != nil {
		return err
	}

	go func() {
		for event := range updates {
			payload, err := events.Encode(event)
			if err != nil {
				slog.Error("could not encode a poll update for browsers",
					"poll_id", event.PollID, "error", err)
				continue
			}
			m.hub.Broadcast(event.PollID, payload)
		}
	}()

	return nil
}

// Hub exposes the hub so the upgrade handler can register connections.
func (m *Manager) Hub() *Hub { return m.hub }
