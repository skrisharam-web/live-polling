package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/events"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
	"github.com/skrisharam-web/live-polling/backend/internal/response"
	"github.com/skrisharam-web/live-polling/backend/internal/services"
	ws "github.com/skrisharam-web/live-polling/backend/internal/websocket"
)

// pollLoader is the poll lookup the upgrade needs.
type pollLoader interface {
	Get(ctx context.Context, pollID string) (*models.Poll, error)
}

// resultsLoader provides the snapshot a client receives on connect.
type resultsLoader interface {
	ForPoll(ctx context.Context, poll *models.Poll) (*services.Results, error)
}

// WebSocketHandler upgrades a request into a live connection to one poll.
type WebSocketHandler struct {
	hub      *ws.Hub
	polls    pollLoader
	results  resultsLoader
	upgrader gorilla.Upgrader
}

// NewWebSocketHandler builds the handler with an origin allow-list.
//
// The origin check is the security-relevant part of this file. A WebSocket
// handshake is not subject to the same-origin policy and is not covered by CORS:
// without this check, any page on the internet could open a socket to this
// endpoint. The poll data behind it is public, so the exposure is modest, but
// the connection itself is a resource, and an unchecked endpoint is one a
// stranger's page can open thousands of.
func NewWebSocketHandler(hub *ws.Hub, polls pollLoader, results resultsLoader, allowedOrigins []string) *WebSocketHandler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[strings.TrimRight(strings.TrimSpace(origin), "/")] = struct{}{}
	}

	return &WebSocketHandler{
		hub:     hub,
		polls:   polls,
		results: results,
		upgrader: gorilla.Upgrader{
			HandshakeTimeout: 10 * time.Second,
			ReadBufferSize:   1024,
			WriteBufferSize:  1024,
			CheckOrigin: func(r *http.Request) bool {
				origin := strings.TrimRight(r.Header.Get("Origin"), "/")
				if origin == "" {
					// Non-browser clients (curl, a test, a mobile app) send no
					// Origin. They are not what the same-origin policy protects
					// against, and refusing them would break tooling.
					return true
				}
				_, ok := allowed[origin]
				return ok
			},
		},
	}
}

// Subscribe handles GET /ws/polls/:id.
//
// The poll is checked before the upgrade, so a bad ID gets a normal JSON 404
// rather than a socket that opens and immediately closes for reasons the client
// cannot read.
func (h *WebSocketHandler) Subscribe(c *gin.Context) {
	pollID := c.Param("id")

	poll, err := h.polls.Get(c.Request.Context(), pollID)
	if err != nil {
		response.Error(c, err)
		return
	}

	// Take the snapshot before upgrading: once the connection is hijacked there is
	// no way to report an error to the client.
	snapshot, err := h.snapshot(c.Request.Context(), poll)
	if err != nil {
		response.Error(c, err)
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade has already written its own HTTP error response.
		slog.Debug("websocket upgrade failed", "poll_id", pollID, "error", err)
		return
	}

	client := ws.NewClient(h.hub, conn, poll.ID.Hex())
	if !h.hub.Register(client) {
		// The hub is shutting down.
		_ = conn.Close()
		return
	}
	client.Start()

	// The snapshot means a client is never looking at an empty results view
	// waiting for someone to vote, and it closes the gap between loading the page
	// and the socket opening.
	client.Send(snapshot)
}

// snapshot encodes the poll's current standing as the same event clients receive
// from then on, so the client has exactly one code path for rendering results.
func (h *WebSocketHandler) snapshot(ctx context.Context, poll *models.Poll) ([]byte, error) {
	results, err := h.results.ForPoll(ctx, poll)
	if err != nil {
		return nil, err
	}

	counts := make([]events.OptionCount, 0, len(results.Options))
	for _, option := range results.Options {
		counts = append(counts, events.OptionCount{OptionID: option.OptionID, Count: option.Count})
	}

	payload, err := events.Encode(events.PollResultsUpdated{
		PollID:     results.PollID,
		Results:    counts,
		TotalVotes: results.TotalVotes,
		Status:     string(results.Status),
		Timestamp:  results.ComputedAt,
	})
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not load the results.", err)
	}
	return payload, nil
}
