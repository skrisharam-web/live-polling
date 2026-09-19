package websocket

import (
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// writeWait bounds how long a single write may take. Without it, a client on
	// a dead network holds a goroutine and a connection open indefinitely.
	writeWait = 10 * time.Second

	// pongWait is how long the server waits to hear from a client before deciding
	// the connection is gone. A browser that closes cleanly sends a close frame;
	// a laptop that goes into a tunnel does not, and this is what catches it.
	pongWait = 60 * time.Second

	// pingPeriod must be shorter than pongWait, or the server would time a client
	// out just before asking whether it is still there. Pings also keep the
	// connection alive through proxies that drop idle sockets — which most
	// hosting platforms do, typically after 60 seconds.
	pingPeriod = (pongWait * 9) / 10

	// maxMessageSize caps what a client may send. Clients have nothing to say on
	// this socket, so this is purely a guard: without it one connection could
	// stream unbounded data into the server's buffers.
	maxMessageSize = 512

	// sendBuffer is how many messages may queue for one client before it is
	// treated as too slow to keep.
	sendBuffer = 16
)

// Client is one browser's connection to one poll.
//
// It owns two goroutines: a reader that exists mainly to notice the connection
// dying, and a writer that is the only goroutine allowed to touch the socket.
// That single-writer rule matters — gorilla/websocket permits one concurrent
// writer, and violating it corrupts the stream rather than returning an error.
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	pollID string

	send chan []byte
	// closeOnce guards against the double close that happens naturally when both
	// pumps notice the same broken connection.
	closeOnce sync.Once
}

func NewClient(hub *Hub, conn *websocket.Conn, pollID string) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		pollID: pollID,
		send:   make(chan []byte, sendBuffer),
	}
}

// PollID is the poll this client is watching.
func (c *Client) PollID() string { return c.pollID }

// Send queues a payload. It reports false when the client's buffer is full,
// which the hub treats as grounds for disconnection: a viewer too slow to drain
// sixteen messages is not going to catch up, and blocking on them would hold up
// everyone else watching the same poll.
func (c *Client) Send(payload []byte) bool {
	select {
	case c.send <- payload:
		return true
	default:
		return false
	}
}

// Start launches the client's pumps.
func (c *Client) Start() {
	go c.writePump()
	go c.readPump()
}

// close shuts the client down. Only the hub calls it, and only from its own
// goroutine, so closing the send channel cannot race with a Send.
func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.send)
	})
}

// readPump drains anything the client sends and watches for the connection
// dying. Nothing a client sends is acted on: this socket is one-way by design,
// and accepting commands over it would be a second, unauthenticated API.
func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}
	// Every pong pushes the deadline out, so a live connection stays open and a
	// silent one is closed.
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("websocket closed unexpectedly", "poll_id", c.pollID, "error", err)
			}
			return
		}
	}
}

// writePump is the only goroutine that writes to the socket: payloads from the
// hub, and pings on a timer.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case payload, open := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if !open {
				// The hub closed this client; tell the browser properly so it can
				// distinguish a deliberate close from a network failure.
				_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
