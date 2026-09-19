package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// pollPayload mirrors the poll object the API returns.
type pollPayload struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Options  []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"options"`
	Status       string     `json:"status"`
	AcceptsVotes bool       `json:"acceptsVotes"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	TotalVotes   int64      `json:"totalVotes"`
}

func decodePoll(t *testing.T, res apiResponse) pollPayload {
	t.Helper()
	var wrapper struct {
		Poll pollPayload `json:"poll"`
	}
	if err := json.Unmarshal(res.Data, &wrapper); err != nil {
		t.Fatalf("decode poll: %v (data: %s)", err, string(res.Data))
	}
	return wrapper.Poll
}

// createPoll registers nothing; it assumes the caller already has a session.
func (a *testAPI) createPoll(question string, options []string) (int, pollPayload) {
	a.t.Helper()
	rec, res := a.do(http.MethodPost, "/api/polls", map[string]any{
		"question": question,
		"options":  options,
	})
	if rec.Code != http.StatusCreated {
		return rec.Code, pollPayload{}
	}
	return rec.Code, decodePoll(a.t, res)
}

func TestPollCreationRequiresAuthentication(t *testing.T) {
	api := newTestAPI(t)

	// This is the assignment's core rule: poll creation is not open to anyone with
	// the URL.
	rec, res := api.do(http.MethodPost, "/api/polls", map[string]any{
		"question": "Should this work?",
		"options":  []string{"Yes", "No"},
	})
	assertStatus(t, rec, http.StatusUnauthorized)
	assertErrorCode(t, res, "UNAUTHORIZED")
}

func TestPollLifecycle(t *testing.T) {
	api := newTestAPI(t)
	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("setup registration failed: %d", rec.Code)
	}

	status, poll := api.createPoll("Which release do we ship first?", []string{"The one with tests", "The one without"})
	if status != http.StatusCreated {
		t.Fatalf("create poll status = %d", status)
	}

	t.Run("the created poll is active and complete", func(t *testing.T) {
		if poll.ID == "" {
			t.Fatal("no poll id was returned")
		}
		if poll.Status != "active" || !poll.AcceptsVotes {
			t.Errorf("status = %q, acceptsVotes = %v; want an active poll", poll.Status, poll.AcceptsVotes)
		}
		if len(poll.Options) != 2 {
			t.Fatalf("got %d options, want 2", len(poll.Options))
		}
		for _, opt := range poll.Options {
			if opt.ID == "" {
				t.Error("an option came back without a server-generated ID")
			}
		}
	})

	t.Run("the poll is readable without a session, as a share link", func(t *testing.T) {
		anonymous := *api
		anonymous.cookies = map[string]string{}

		rec, res := anonymous.do(http.MethodGet, "/api/polls/"+poll.ID, nil)
		assertStatus(t, rec, http.StatusOK)

		public := decodePoll(t, res)
		if public.Question != poll.Question {
			t.Errorf("question = %q, want %q", public.Question, poll.Question)
		}

		// A share link is world-readable, so the payload must not carry anything
		// that identifies the owner.
		body := rec.Body.String()
		for _, leak := range []string{"ownerId", "ownerID", "ada@example.com", "passwordHash"} {
			if strings.Contains(body, leak) {
				t.Errorf("the public poll payload leaks %q: %s", leak, body)
			}
		}
	})

	t.Run("the dashboard lists the poll with its vote total", func(t *testing.T) {
		rec, res := api.do(http.MethodGet, "/api/me/polls", nil)
		assertStatus(t, rec, http.StatusOK)

		var wrapper struct {
			Polls []pollPayload `json:"polls"`
		}
		if err := json.Unmarshal(res.Data, &wrapper); err != nil {
			t.Fatalf("decode polls: %v", err)
		}
		if len(wrapper.Polls) != 1 {
			t.Fatalf("dashboard returned %d polls, want 1", len(wrapper.Polls))
		}
		if wrapper.Polls[0].ID != poll.ID {
			t.Errorf("dashboard poll = %q, want %q", wrapper.Polls[0].ID, poll.ID)
		}
		if wrapper.Polls[0].TotalVotes != 0 {
			t.Errorf("totalVotes = %d, want 0 for a poll with no votes", wrapper.Polls[0].TotalVotes)
		}
	})

	t.Run("the owner can edit it while it has no votes", func(t *testing.T) {
		rec, res := api.do(http.MethodPatch, "/api/polls/"+poll.ID, map[string]any{
			"question": "Which release ships on Friday?",
		})
		assertStatus(t, rec, http.StatusOK)

		updated := decodePoll(t, res)
		if updated.Question != "Which release ships on Friday?" {
			t.Errorf("question = %q", updated.Question)
		}
	})

	t.Run("the owner can close it", func(t *testing.T) {
		rec, res := api.do(http.MethodPost, "/api/polls/"+poll.ID+"/close", map[string]string{})
		assertStatus(t, rec, http.StatusOK)

		closed := decodePoll(t, res)
		if closed.Status != "closed" {
			t.Errorf("status = %q, want closed", closed.Status)
		}
		if closed.AcceptsVotes {
			t.Error("a closed poll must not report that it accepts votes")
		}
	})

	t.Run("the owner can delete it", func(t *testing.T) {
		rec, _ := api.do(http.MethodDelete, "/api/polls/"+poll.ID, nil)
		assertStatus(t, rec, http.StatusOK)

		rec, res := api.do(http.MethodGet, "/api/polls/"+poll.ID, nil)
		assertStatus(t, rec, http.StatusNotFound)
		assertErrorCode(t, res, "NOT_FOUND")
	})
}

// TestPollManagementIsOwnerOnly is the authorization test at the HTTP boundary:
// a second, fully authenticated user must not be able to manage someone else's
// poll through any route.
func TestPollManagementIsOwnerOnly(t *testing.T) {
	api := newTestAPI(t)

	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("owner registration failed: %d", rec.Code)
	}
	status, poll := api.createPoll("Whose poll is this?", []string{"Mine", "Yours"})
	if status != http.StatusCreated {
		t.Fatalf("create poll status = %d", status)
	}
	api.logout()

	if rec, _ := api.register("Mallory", "mallory@example.com", "another-password"); rec.Code != http.StatusCreated {
		t.Fatalf("second registration failed: %d", rec.Code)
	}

	attempts := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"manage", http.MethodGet, "/api/polls/" + poll.ID + "/manage", nil},
		{"update", http.MethodPatch, "/api/polls/" + poll.ID, map[string]any{"question": "Mine now"}},
		{"close", http.MethodPost, "/api/polls/" + poll.ID + "/close", map[string]string{}},
		{"delete", http.MethodDelete, "/api/polls/" + poll.ID, nil},
	}

	for _, attempt := range attempts {
		t.Run(attempt.name+" by another user is forbidden", func(t *testing.T) {
			rec, res := api.do(attempt.method, attempt.path, attempt.body)
			assertStatus(t, rec, http.StatusForbidden)
			assertErrorCode(t, res, "FORBIDDEN")
		})
	}

	t.Run("the other user's dashboard does not show the poll", func(t *testing.T) {
		rec, res := api.do(http.MethodGet, "/api/me/polls", nil)
		assertStatus(t, rec, http.StatusOK)

		var wrapper struct {
			Polls []pollPayload `json:"polls"`
		}
		if err := json.Unmarshal(res.Data, &wrapper); err != nil {
			t.Fatalf("decode polls: %v", err)
		}
		if len(wrapper.Polls) != 0 {
			t.Errorf("dashboard returned %d polls for a user who owns none", len(wrapper.Polls))
		}
	})

	t.Run("the poll survived every attempt", func(t *testing.T) {
		rec, res := api.do(http.MethodGet, "/api/polls/"+poll.ID, nil)
		assertStatus(t, rec, http.StatusOK)

		unchanged := decodePoll(t, res)
		if unchanged.Question != "Whose poll is this?" {
			t.Errorf("question = %q, want the original", unchanged.Question)
		}
		if unchanged.Status != "active" {
			t.Errorf("status = %q, want active", unchanged.Status)
		}
	})
}

// TestOwnershipCannotBeClaimedInThePayload checks that the owner is taken from
// the session and not from anything a client can write.
func TestOwnershipCannotBeClaimedInThePayload(t *testing.T) {
	api := newTestAPI(t)

	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("owner registration failed: %d", rec.Code)
	}
	status, victimPoll := api.createPoll("Ada's poll", []string{"A", "B"})
	if status != http.StatusCreated {
		t.Fatalf("create poll status = %d", status)
	}
	api.logout()

	if rec, _ := api.register("Mallory", "mallory@example.com", "another-password"); rec.Code != http.StatusCreated {
		t.Fatalf("second registration failed: %d", rec.Code)
	}

	// Mallory creates a poll while claiming Ada's user id and an option id of her
	// choosing. Both claims must be ignored.
	rec, res := api.do(http.MethodPost, "/api/polls", map[string]any{
		"question": "Can I forge ownership?",
		"options":  []string{"Yes", "No"},
		"ownerId":  "000000000000000000000001",
		"status":   "closed",
		"id":       victimPoll.ID,
	})
	assertStatus(t, rec, http.StatusCreated)

	forged := decodePoll(t, res)
	if forged.ID == victimPoll.ID {
		t.Fatal("a client-supplied id overwrote the server-generated one")
	}
	if forged.Status != "active" {
		t.Errorf("status = %q; a client-supplied status must be ignored", forged.Status)
	}

	t.Run("the forged poll belongs to its actual creator", func(t *testing.T) {
		rec, _ := api.do(http.MethodGet, "/api/polls/"+forged.ID+"/manage", nil)
		assertStatus(t, rec, http.StatusOK)
	})

	t.Run("and still cannot reach the other user's poll", func(t *testing.T) {
		rec, res := api.do(http.MethodGet, "/api/polls/"+victimPoll.ID+"/manage", nil)
		assertStatus(t, rec, http.StatusForbidden)
		assertErrorCode(t, res, "FORBIDDEN")
	})
}

func TestPollValidationOverHTTP(t *testing.T) {
	api := newTestAPI(t)
	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("setup registration failed: %d", rec.Code)
	}

	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"no question", map[string]any{"options": []string{"A", "B"}}, "question"},
		{"one option", map[string]any{"question": "A real question?", "options": []string{"A"}}, "options"},
		{"duplicate options", map[string]any{"question": "A real question?", "options": []string{"A", "a"}}, "options"},
		{"expiry in the past", map[string]any{
			"question":  "A real question?",
			"options":   []string{"A", "B"},
			"expiresAt": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		}, "expiresAt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, res := api.do(http.MethodPost, "/api/polls", tc.body)
			assertStatus(t, rec, http.StatusUnprocessableEntity)
			assertErrorCode(t, res, "VALIDATION_ERROR")
			if _, ok := res.Error.Fields[tc.field]; !ok {
				t.Errorf("fields = %v, want an entry for %q", res.Error.Fields, tc.field)
			}
		})
	}
}

func TestUnknownPollIsNotFound(t *testing.T) {
	api := newTestAPI(t)

	for _, id := range []string{"6aae43849751081b4d469065", "not-an-object-id"} {
		t.Run(id, func(t *testing.T) {
			// A malformed ID and an unknown ID answer identically, so the endpoint
			// cannot be used to learn which ID shapes are real.
			rec, res := api.do(http.MethodGet, "/api/polls/"+id, nil)
			assertStatus(t, rec, http.StatusNotFound)
			assertErrorCode(t, res, "NOT_FOUND")
		})
	}
}
