package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/skrisharam-web/live-polling/backend/internal/events"
	"github.com/skrisharam-web/live-polling/backend/internal/services"
)

// waitForEvent reads one event or fails the test. Every realtime assertion needs
// a deadline; without one a broken fan-out looks like a hung test.
func waitForEvent(t *testing.T, stream <-chan events.PollResultsUpdated, within time.Duration) events.PollResultsUpdated {
	t.Helper()
	select {
	case event, ok := <-stream:
		if !ok {
			t.Fatal("the event stream closed while waiting for an event")
		}
		return event
	case <-time.After(within):
		t.Fatalf("no event arrived within %v", within)
		return events.PollResultsUpdated{}
	}
}

func assertNoEvent(t *testing.T, stream <-chan events.PollResultsUpdated, within time.Duration) {
	t.Helper()
	select {
	case event := <-stream:
		t.Fatalf("an unexpected event arrived: %+v", event)
	case <-time.After(within):
	}
}

// TestVotePublishesToRedis follows a real vote all the way to a real Redis
// subscriber. This is the step that makes the poll live: without it the results
// are correct but nobody hears about them.
func TestVotePublishesToRedis(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)

	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()

	// A separate subscriber, exactly as a second backend instance would be.
	listener := services.NewRealtimeService(nil, api.redis)
	stream, err := listener.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}

	poll := seedPoll(t, api, "Does the event reach Redis?", []string{"Yes", "No"})

	voter := anonymousVisitor(api)
	if rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[0].ID,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("vote status = %d", rec.Code)
	}

	event := waitForEvent(t, stream, 5*time.Second)

	if event.Type != events.TypePollResultsUpdated {
		t.Errorf("Type = %q, want %q", event.Type, events.TypePollResultsUpdated)
	}
	if event.PollID != poll.ID {
		t.Errorf("PollID = %q, want %q", event.PollID, poll.ID)
	}
	if event.TotalVotes != 1 {
		t.Errorf("TotalVotes = %d, want 1", event.TotalVotes)
	}
	if event.Status != "active" {
		t.Errorf("Status = %q, want active", event.Status)
	}
	if event.Timestamp.IsZero() {
		t.Error("the event carries no timestamp")
	}

	// The event carries the whole standing, so one message is enough to render.
	var sum int64
	for _, option := range event.Results {
		sum += option.Count
	}
	if sum != event.TotalVotes {
		t.Errorf("counts sum to %d but totalVotes is %d", sum, event.TotalVotes)
	}
	if len(event.Results) != 2 {
		t.Errorf("got %d result rows, want every option including the unchosen one", len(event.Results))
	}
}

// TestEveryVoteProducesAnEvent checks the ordering and count of a burst, which is
// what a live audience actually generates.
func TestEveryVoteProducesAnEvent(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)

	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()

	listener := services.NewRealtimeService(nil, api.redis)
	stream, err := listener.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}

	poll := seedPoll(t, api, "How many events?", []string{"Yes", "No"})

	const votes = 5
	for i := 0; i < votes; i++ {
		voter := anonymousVisitor(api)
		if rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
			"optionId": poll.Options[i%2].ID,
		}); rec.Code != http.StatusCreated {
			t.Fatalf("vote %d status = %d", i, rec.Code)
		}
	}

	var last int64
	for i := 0; i < votes; i++ {
		event := waitForEvent(t, stream, 5*time.Second)
		if event.TotalVotes <= last {
			t.Errorf("event %d reported totalVotes %d after %d; the running total should only climb", i, event.TotalVotes, last)
		}
		last = event.TotalVotes
	}
	if last != votes {
		t.Errorf("the final event reported %d votes, want %d", last, votes)
	}
}

// TestRejectedVotesPublishNothing: a duplicate or invalid vote changes nothing,
// so it must not wake up every viewer's browser either.
func TestRejectedVotesPublishNothing(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)

	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()

	listener := services.NewRealtimeService(nil, api.redis)
	stream, err := listener.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}

	poll := seedPoll(t, api, "Do rejections publish?", []string{"Yes", "No"})

	voter := anonymousVisitor(api)
	if rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[0].ID,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("vote status = %d", rec.Code)
	}
	waitForEvent(t, stream, 5*time.Second)

	// A duplicate and an invalid option, neither of which changes the tally.
	if rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[1].ID,
	}); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate vote status = %d, want 409", rec.Code)
	}
	if rec, _ := anonymousVisitor(api).do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": "not-a-real-option",
	}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid vote status = %d, want 422", rec.Code)
	}

	assertNoEvent(t, stream, 750*time.Millisecond)
}

// TestEventsAreScopedToTheirPoll makes sure a subscriber can tell one poll's
// traffic from another's, which is what lets the hub route to the right room.
func TestEventsAreScopedToTheirPoll(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)

	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()

	listener := services.NewRealtimeService(nil, api.redis)
	stream, err := listener.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}

	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("registration failed: %d", rec.Code)
	}
	_, first := api.createPoll("First poll", []string{"A", "B"})
	_, second := api.createPoll("Second poll", []string{"C", "D"})
	api.logout()

	if rec, _ := anonymousVisitor(api).do(http.MethodPost, "/api/polls/"+second.ID+"/vote", map[string]string{
		"optionId": second.Options[1].ID,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("vote status = %d", rec.Code)
	}

	event := waitForEvent(t, stream, 5*time.Second)
	if event.PollID != second.ID {
		t.Fatalf("the event was attributed to poll %q, want %q", event.PollID, second.ID)
	}
	if event.PollID == first.ID {
		t.Error("a vote in one poll produced an event for another")
	}
}

// TestClosingAPollIsAnnounced: someone watching a poll should see it close
// without having to try to vote to find out.
func TestClosingAPollIsAnnounced(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)

	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()

	listener := services.NewRealtimeService(nil, api.redis)
	stream, err := listener.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}

	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("registration failed: %d", rec.Code)
	}
	_, poll := api.createPoll("Closing announcement?", []string{"Yes", "No"})

	if rec, _ := api.do(http.MethodPost, "/api/polls/"+poll.ID+"/close", map[string]string{}); rec.Code != http.StatusOK {
		t.Fatalf("close status = %d", rec.Code)
	}

	event := waitForEvent(t, stream, 5*time.Second)
	if event.PollID != poll.ID {
		t.Errorf("PollID = %q, want %q", event.PollID, poll.ID)
	}
	if event.Status != "closed" {
		t.Errorf("Status = %q, want closed", event.Status)
	}
}

// TestSeveralSubscribersAllReceiveTheEvent is the multi-instance property: Redis
// fans out to every backend, which is what lets a voter on one instance update a
// viewer connected to another.
func TestSeveralSubscribersAllReceiveTheEvent(t *testing.T) {
	api := newTestAPI(t)
	requireRedis(t, api.redis)

	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()

	const instances = 3
	streams := make([]<-chan events.PollResultsUpdated, 0, instances)
	for i := 0; i < instances; i++ {
		listener := services.NewRealtimeService(nil, api.redis)
		stream, err := listener.Subscribe(ctx)
		if err != nil {
			t.Fatalf("Subscribe() %d = %v", i, err)
		}
		streams = append(streams, stream)
	}

	poll := seedPoll(t, api, "Does everyone hear it?", []string{"Yes", "No"})

	if rec, _ := anonymousVisitor(api).do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[0].ID,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("vote status = %d", rec.Code)
	}

	for i, stream := range streams {
		event := waitForEvent(t, stream, 5*time.Second)
		if event.PollID != poll.ID || event.TotalVotes != 1 {
			t.Errorf("subscriber %d got %+v", i, event)
		}
	}
}
