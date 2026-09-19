package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"

	"github.com/skrisharam-web/live-polling/backend/internal/events"
)

// liveServer starts the real application on a real port. The WebSocket tests
// cannot use httptest's in-process handler calls, because an upgrade hijacks the
// connection — these need a socket.
func liveServer(t *testing.T, api *testAPI) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(api.engine)
	t.Cleanup(server.Close)
	return server
}

// viewer is a browser watching a poll over a real WebSocket.
type viewer struct {
	t    *testing.T
	conn *gorilla.Conn
}

// watch opens a WebSocket to a poll, as the results page does.
func watch(t *testing.T, server *httptest.Server, pollID string, headers http.Header) (*viewer, *http.Response, error) {
	t.Helper()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/polls/" + pollID
	conn, resp, err := gorilla.DefaultDialer.Dial(url, headers)
	if err != nil {
		return nil, resp, err
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &viewer{t: t, conn: conn}, resp, nil
}

// next reads one event, failing if none arrives in time.
func (v *viewer) next(within time.Duration) events.PollResultsUpdated {
	v.t.Helper()

	if err := v.conn.SetReadDeadline(time.Now().Add(within)); err != nil {
		v.t.Fatalf("set read deadline: %v", err)
	}
	_, payload, err := v.conn.ReadMessage()
	if err != nil {
		v.t.Fatalf("read message: %v", err)
	}

	var event events.PollResultsUpdated
	if err := json.Unmarshal(payload, &event); err != nil {
		v.t.Fatalf("the payload sent to the browser is not a known event: %v (%s)", err, payload)
	}
	return event
}

// expectSilence asserts that nothing arrives within a window.
func (v *viewer) expectSilence(within time.Duration) {
	v.t.Helper()

	if err := v.conn.SetReadDeadline(time.Now().Add(within)); err != nil {
		v.t.Fatalf("set read deadline: %v", err)
	}
	_, payload, err := v.conn.ReadMessage()
	if err == nil {
		v.t.Fatalf("an unexpected message arrived: %s", payload)
	}
	if netErr, ok := err.(interface{ Timeout() bool }); !ok || !netErr.Timeout() {
		v.t.Fatalf("the connection failed rather than staying quiet: %v", err)
	}
}

func TestWebSocketSendsASnapshotOnConnect(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	server := liveServer(t, api)

	poll := seedPoll(t, api, "Is there a snapshot?", []string{"Yes", "No"})

	// Two votes before anyone is watching.
	for i := 0; i < 2; i++ {
		if rec, _ := anonymousVisitor(api).do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
			"optionId": poll.Options[0].ID,
		}); rec.Code != http.StatusCreated {
			t.Fatalf("vote status = %d", rec.Code)
		}
	}

	view, _, err := watch(t, server, poll.ID, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// A client that connects late must not have to wait for the next vote to see
	// anything, so the first message is the current standing.
	event := view.next(5 * time.Second)
	if event.TotalVotes != 2 {
		t.Errorf("snapshot totalVotes = %d, want 2", event.TotalVotes)
	}
	if event.Type != events.TypePollResultsUpdated {
		t.Errorf("snapshot type = %q", event.Type)
	}
	if len(event.Results) != 2 {
		t.Errorf("snapshot carries %d options, want 2", len(event.Results))
	}
}

// TestAVoteReachesEveryWatcher is the requirement in one test: several browsers
// watching, one votes, they all update without asking for anything.
func TestAVoteReachesEveryWatcher(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	server := liveServer(t, api)

	poll := seedPoll(t, api, "Does everyone see it?", []string{"Yes", "No"})

	const watchers = 4
	views := make([]*viewer, 0, watchers)
	for i := 0; i < watchers; i++ {
		view, _, err := watch(t, server, poll.ID, nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		// Consume the snapshot so the next read is the live update.
		if snapshot := view.next(5 * time.Second); snapshot.TotalVotes != 0 {
			t.Fatalf("snapshot totalVotes = %d, want 0", snapshot.TotalVotes)
		}
		views = append(views, view)
	}

	if rec, _ := anonymousVisitor(api).do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[1].ID,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("vote status = %d", rec.Code)
	}

	for i, view := range views {
		event := view.next(5 * time.Second)
		if event.PollID != poll.ID {
			t.Errorf("watcher %d received an event for poll %q", i, event.PollID)
		}
		if event.TotalVotes != 1 {
			t.Errorf("watcher %d saw totalVotes = %d, want 1", i, event.TotalVotes)
		}
		// The chosen option is the second one; the event must say so.
		for _, option := range event.Results {
			if option.OptionID == poll.Options[1].ID && option.Count != 1 {
				t.Errorf("watcher %d saw count %d for the chosen option, want 1", i, option.Count)
			}
		}
	}
}

// TestWatchersOnlyHearTheirOwnPoll: the room boundary has to hold over a real
// socket, not just in the hub's unit tests.
func TestWatchersOnlyHearTheirOwnPoll(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	server := liveServer(t, api)

	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("registration failed: %d", rec.Code)
	}
	_, watched := api.createPoll("The poll being watched", []string{"A", "B"})
	_, other := api.createPoll("A different poll", []string{"C", "D"})
	api.logout()

	view, _, err := watch(t, server, watched.ID, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	view.next(5 * time.Second) // snapshot

	if rec, _ := anonymousVisitor(api).do(http.MethodPost, "/api/polls/"+other.ID+"/vote", map[string]string{
		"optionId": other.Options[0].ID,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("vote status = %d", rec.Code)
	}

	view.expectSilence(1500 * time.Millisecond)
}

// TestClosingAPollReachesWatchers: a viewer should see voting end without having
// to try to vote.
func TestClosingAPollReachesWatchers(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	server := liveServer(t, api)

	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("registration failed: %d", rec.Code)
	}
	_, poll := api.createPoll("Closing live?", []string{"Yes", "No"})

	view, _, err := watch(t, server, poll.ID, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	view.next(5 * time.Second) // snapshot

	if rec, _ := api.do(http.MethodPost, "/api/polls/"+poll.ID+"/close", map[string]string{}); rec.Code != http.StatusOK {
		t.Fatalf("close status = %d", rec.Code)
	}

	event := view.next(5 * time.Second)
	if event.Status != "closed" {
		t.Errorf("Status = %q, want closed", event.Status)
	}
}

func TestWebSocketRejectsUnknownPolls(t *testing.T) {
	api := newTestAPI(t)
	server := liveServer(t, api)

	for _, id := range []string{"6aae43849751081b4d469065", "not-an-object-id"} {
		t.Run(id, func(t *testing.T) {
			// The poll is checked before the upgrade, so the client gets a readable
			// 404 rather than a socket that opens and vanishes.
			_, resp, err := watch(t, server, id, nil)
			if err == nil {
				t.Fatal("the connection was accepted for a poll that does not exist")
			}
			if resp == nil {
				t.Fatalf("no HTTP response: %v", err)
			}
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", resp.StatusCode)
			}
		})
	}
}

// TestWebSocketChecksTheOrigin is the security case. A WebSocket handshake is not
// covered by CORS, so without this check any page on the internet could open a
// socket here.
func TestWebSocketChecksTheOrigin(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	server := liveServer(t, api)

	poll := seedPoll(t, api, "Whose origin?", []string{"Yes", "No"})

	t.Run("the configured frontend is allowed", func(t *testing.T) {
		headers := http.Header{"Origin": []string{"http://localhost:5173"}}
		view, _, err := watch(t, server, poll.ID, headers)
		if err != nil {
			t.Fatalf("the allowed origin was refused: %v", err)
		}
		view.next(5 * time.Second)
	})

	t.Run("another site is refused", func(t *testing.T) {
		headers := http.Header{"Origin": []string{"https://evil.example.com"}}
		_, resp, err := watch(t, server, poll.ID, headers)
		if err == nil {
			t.Fatal("a socket was opened from an unknown origin")
		}
		if resp != nil && resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("a non-browser client with no origin is allowed", func(t *testing.T) {
		// curl, a mobile app or a test sends no Origin. Refusing those would break
		// tooling without protecting anyone.
		view, _, err := watch(t, server, poll.ID, nil)
		if err != nil {
			t.Fatalf("a client with no Origin was refused: %v", err)
		}
		view.next(5 * time.Second)
	})
}

// TestADisconnectedWatcherIsForgotten checks the cleanup path over a real socket:
// a closed connection must not keep occupying a room.
func TestADisconnectedWatcherIsForgotten(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)
	server := liveServer(t, api)

	poll := seedPoll(t, api, "Do disconnects clean up?", []string{"Yes", "No"})

	staying, _, err := watch(t, server, poll.ID, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	staying.next(5 * time.Second)

	leaving, _, err := watch(t, server, poll.ID, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	leaving.next(5 * time.Second)
	if err := leaving.conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The remaining watcher keeps working, which is the point: one client going
	// away must not disturb the others.
	if rec, _ := anonymousVisitor(api).do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[0].ID,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("vote status = %d", rec.Code)
	}

	event := staying.next(5 * time.Second)
	if event.TotalVotes != 1 {
		t.Errorf("the remaining watcher saw totalVotes = %d, want 1", event.TotalVotes)
	}
}
