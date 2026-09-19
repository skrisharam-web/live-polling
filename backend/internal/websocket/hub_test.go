package websocket

import (
	"context"
	"sync"
	"testing"
	"time"
)

// newTestClient makes a client with no real socket. The hub only ever calls
// Send and close on a client, so a nil connection is enough to exercise every
// routing decision without standing up a server.
func newTestClient(hub *Hub, pollID string) *Client {
	return &Client{hub: hub, pollID: pollID, send: make(chan []byte, sendBuffer)}
}

func startHub(t *testing.T) (*Hub, context.CancelFunc) {
	t.Helper()
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	t.Cleanup(cancel)
	return hub, cancel
}

// receive reads one payload or fails. Every assertion about delivery needs a
// deadline, or a broken hub looks like a hanging test.
func receive(t *testing.T, client *Client, within time.Duration) string {
	t.Helper()
	select {
	case payload, ok := <-client.send:
		if !ok {
			t.Fatal("the client was closed while waiting for a message")
		}
		return string(payload)
	case <-time.After(within):
		t.Fatal("no message arrived")
		return ""
	}
}

func expectNothing(t *testing.T, client *Client, within time.Duration) {
	t.Helper()
	select {
	case payload := <-client.send:
		t.Fatalf("an unexpected message arrived: %s", payload)
	case <-time.After(within):
	}
}

func TestBroadcastReachesEveryClientInTheRoom(t *testing.T) {
	hub, _ := startHub(t)

	clients := make([]*Client, 3)
	for i := range clients {
		clients[i] = newTestClient(hub, "poll-1")
		if !hub.Register(clients[i]) {
			t.Fatal("Register() failed")
		}
	}

	hub.Broadcast("poll-1", []byte(`{"total":1}`))

	for i, client := range clients {
		if got := receive(t, client, 2*time.Second); got != `{"total":1}` {
			t.Errorf("client %d received %q", i, got)
		}
	}
}

// TestRoomsAreIsolated is what makes the poll ID in the URL meaningful: a viewer
// of one poll must never receive another poll's traffic.
func TestRoomsAreIsolated(t *testing.T) {
	hub, _ := startHub(t)

	watcher := newTestClient(hub, "poll-1")
	bystander := newTestClient(hub, "poll-2")
	hub.Register(watcher)
	hub.Register(bystander)

	hub.Broadcast("poll-1", []byte("for poll one"))

	if got := receive(t, watcher, 2*time.Second); got != "for poll one" {
		t.Errorf("watcher received %q", got)
	}
	expectNothing(t, bystander, 250*time.Millisecond)
}

func TestBroadcastToAnEmptyRoomIsHarmless(t *testing.T) {
	hub, _ := startHub(t)

	hub.Broadcast("nobody-is-watching", []byte("hello"))

	// The hub must still be alive and serving.
	client := newTestClient(hub, "poll-1")
	hub.Register(client)
	hub.Broadcast("poll-1", []byte("still here"))
	if got := receive(t, client, 2*time.Second); got != "still here" {
		t.Errorf("received %q", got)
	}
}

func TestUnregisterStopsDelivery(t *testing.T) {
	hub, _ := startHub(t)

	staying := newTestClient(hub, "poll-1")
	leaving := newTestClient(hub, "poll-1")
	hub.Register(staying)
	hub.Register(leaving)

	hub.Unregister(leaving)
	hub.Broadcast("poll-1", []byte("after the disconnect"))

	if got := receive(t, staying, 2*time.Second); got != "after the disconnect" {
		t.Errorf("the remaining client received %q", got)
	}

	// A removed client's channel is closed, which is how its write pump learns to
	// shut down.
	select {
	case _, open := <-leaving.send:
		if open {
			t.Error("the removed client still received a message")
		}
	case <-time.After(time.Second):
		t.Error("the removed client's channel was not closed")
	}
}

func TestUnregisteringTwiceIsSafe(t *testing.T) {
	hub, _ := startHub(t)

	client := newTestClient(hub, "poll-1")
	hub.Register(client)

	// Both pumps notice a dead connection, so this happens routinely. A second
	// close of the send channel would panic.
	hub.Unregister(client)
	hub.Unregister(client)

	if got := hub.Viewers("poll-1"); got != 0 {
		t.Errorf("viewers = %d, want 0", got)
	}
}

// TestEmptyRoomsAreReclaimed: a server that has hosted many polls must not keep a
// map entry per poll forever.
func TestEmptyRoomsAreReclaimed(t *testing.T) {
	hub, _ := startHub(t)

	client := newTestClient(hub, "poll-1")
	hub.Register(client)
	if got := hub.Viewers("poll-1"); got != 1 {
		t.Fatalf("viewers = %d, want 1", got)
	}

	hub.Unregister(client)

	if got := hub.Viewers("poll-1"); got != 0 {
		t.Errorf("viewers = %d, want 0 after the last client left", got)
	}
}

// TestASlowClientIsDroppedRatherThanBlockingTheRoom is the availability property:
// one viewer on a bad connection must not stall everyone else watching.
func TestASlowClientIsDroppedRatherThanBlockingTheRoom(t *testing.T) {
	hub, _ := startHub(t)

	healthy := newTestClient(hub, "poll-1")
	slow := newTestClient(hub, "poll-1")
	hub.Register(healthy)
	hub.Register(slow)

	// Fill the slow client's buffer without ever reading from it.
	for i := 0; i < sendBuffer; i++ {
		if !slow.Send([]byte("backlog")) {
			t.Fatalf("the buffer filled after %d messages, expected %d", i, sendBuffer)
		}
	}

	// Drain the healthy client as a browser would, while the hub keeps sending.
	drained := make(chan int, 1)
	go func() {
		count := 0
		deadline := time.After(3 * time.Second)
		for {
			select {
			case _, ok := <-healthy.send:
				if !ok {
					drained <- count
					return
				}
				count++
				if count == 5 {
					drained <- count
					return
				}
			case <-deadline:
				drained <- count
				return
			}
		}
	}()

	for i := 0; i < 5; i++ {
		hub.Broadcast("poll-1", []byte("update"))
	}

	if got := <-drained; got != 5 {
		t.Errorf("the healthy client received %d of 5 messages; a slow peer held up the room", got)
	}

	// And the slow client has been evicted.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.Viewers("poll-1") == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("viewers = %d, want 1 after the slow client was dropped", hub.Viewers("poll-1"))
}

func TestStoppingTheHubClosesEveryClient(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	client := newTestClient(hub, "poll-1")
	hub.Register(client)

	cancel()

	select {
	case _, open := <-client.send:
		if open {
			t.Error("a message arrived after shutdown")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the client was not closed when the hub stopped")
	}

	// And calls after shutdown must return rather than block forever.
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.Register(newTestClient(hub, "poll-1"))
		hub.Unregister(client)
		hub.Broadcast("poll-1", []byte("ignored"))
		_ = hub.Viewers("poll-1")
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("a hub call blocked after shutdown")
	}
}

// TestConcurrentTrafficIsRaceFree hammers the hub from many goroutines. It is
// written for `go test -race`: the assertion is that the detector stays quiet
// while connects, disconnects and broadcasts overlap.
func TestConcurrentTrafficIsRaceFree(t *testing.T) {
	hub, _ := startHub(t)

	const (
		polls          = 8
		clientsPerPoll = 12
		broadcasts     = 30
	)

	var wg sync.WaitGroup

	for p := 0; p < polls; p++ {
		pollID := string(rune('a' + p))

		for i := 0; i < clientsPerPoll; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				client := newTestClient(hub, pollID)
				if !hub.Register(client) {
					return
				}
				// Drain like a real client until the hub closes the channel.
				go func() {
					for range client.send {
					}
				}()
				time.Sleep(time.Duration(len(pollID)) * time.Millisecond)
				hub.Unregister(client)
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < broadcasts; i++ {
				hub.Broadcast(pollID, []byte("update"))
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < broadcasts; i++ {
				_ = hub.Viewers(pollID)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the hub deadlocked under concurrent traffic")
	}
}
