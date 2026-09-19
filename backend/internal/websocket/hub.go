// Package websocket owns the live connections to browsers.
//
// The shape is deliberately small: a hub that knows which connections are
// watching which poll, a client that owns one connection's read and write pumps,
// and a manager that feeds the hub from the Redis subscription. Nothing here
// knows how a vote is recorded, and nothing in the vote path knows that
// WebSockets exist — Redis Pub/Sub sits between them.
package websocket

import (
	"context"
	"log/slog"
)

// Broadcast is one poll's payload, addressed to everyone watching that poll.
type Broadcast struct {
	PollID  string
	Payload []byte
}

// Hub tracks which clients are watching which poll and fans messages out to them.
//
// All of its state lives in one goroutine — Run — and every other goroutine talks
// to it over channels. That is the reason there is not a single mutex in this
// file: the rooms map has exactly one owner, so concurrent connects, disconnects
// and broadcasts cannot race by construction rather than by careful locking. It
// also means a slow or misbehaving client can never hold a lock that the rest of
// the application needs.
type Hub struct {
	register   chan *Client
	unregister chan *Client
	broadcast  chan Broadcast
	inspect    chan inspection

	// rooms maps a poll ID to the clients watching it. Only Run touches it.
	rooms map[string]map[*Client]struct{}

	// done is closed when Run returns, so callers never block on a stopped hub.
	done chan struct{}
}

// inspection is a request for a snapshot of the hub's state, answered by Run so
// that reading it does not race with mutating it.
type inspection struct {
	pollID string
	reply  chan int
}

func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		// A buffer here absorbs a burst of votes without making the Redis reader
		// wait; beyond it, broadcasts are dropped rather than queued (see Send).
		broadcast: make(chan Broadcast, 256),
		inspect:   make(chan inspection),
		rooms:     make(map[string]map[*Client]struct{}),
		done:      make(chan struct{}),
	}
}

// Run owns the hub's state until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	defer close(h.done)

	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return

		case client := <-h.register:
			room, exists := h.rooms[client.pollID]
			if !exists {
				room = make(map[*Client]struct{})
				h.rooms[client.pollID] = room
			}
			room[client] = struct{}{}

		case client := <-h.unregister:
			h.remove(client)

		case message := <-h.broadcast:
			for client := range h.rooms[message.PollID] {
				if !client.Send(message.Payload) {
					// The client's buffer is full: it is not keeping up, and
					// waiting for it would stall every other viewer of this poll.
					// Dropping it is safe because the client reconnects and
					// refetches the current results.
					slog.Warn("disconnecting a client that fell behind", "poll_id", message.PollID)
					h.remove(client)
				}
			}

		case request := <-h.inspect:
			request.reply <- len(h.rooms[request.pollID])
		}
	}
}

// remove takes a client out of its room and closes it. Empty rooms are deleted
// rather than left behind, so a server that has hosted a million polls does not
// keep a million empty maps.
func (h *Hub) remove(client *Client) {
	room, exists := h.rooms[client.pollID]
	if !exists {
		return
	}
	if _, present := room[client]; !present {
		return
	}

	delete(room, client)
	client.close()

	if len(room) == 0 {
		delete(h.rooms, client.pollID)
	}
}

func (h *Hub) closeAll() {
	for pollID, room := range h.rooms {
		for client := range room {
			client.close()
		}
		delete(h.rooms, pollID)
	}
}

// Register adds a client. It returns false if the hub has stopped, which tells
// the caller to close the connection instead of leaking it.
func (h *Hub) Register(client *Client) bool {
	select {
	case h.register <- client:
		return true
	case <-h.done:
		return false
	}
}

// Unregister removes a client. It is safe to call more than once for the same
// client, which matters because both pumps can notice a dead connection.
func (h *Hub) Unregister(client *Client) {
	select {
	case h.unregister <- client:
	case <-h.done:
	}
}

// Broadcast sends a payload to everyone watching a poll.
//
// It never blocks. If the hub is saturated the message is dropped and logged:
// each message carries a complete tally, so the next one restores the truth,
// whereas blocking here would back up into the Redis subscription and stall
// every poll on this instance.
func (h *Hub) Broadcast(pollID string, payload []byte) {
	select {
	case h.broadcast <- Broadcast{PollID: pollID, Payload: payload}:
	case <-h.done:
	default:
		slog.Warn("dropped a broadcast; the hub is saturated", "poll_id", pollID)
	}
}

// Viewers reports how many clients are watching a poll. It exists for tests and
// for an operator answering "is anyone actually connected?".
func (h *Hub) Viewers(pollID string) int {
	reply := make(chan int, 1)
	select {
	case h.inspect <- inspection{pollID: pollID, reply: reply}:
		return <-reply
	case <-h.done:
		return 0
	}
}
