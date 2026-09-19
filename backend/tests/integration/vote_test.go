package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/skrisharam-web/live-polling/backend/internal/middleware"
)

type resultsPayload struct {
	PollID  string `json:"pollId"`
	Results []struct {
		OptionID string `json:"optionId"`
		Text     string `json:"text"`
		Count    int64  `json:"count"`
	} `json:"results"`
	TotalVotes int64  `json:"totalVotes"`
	Status     string `json:"status"`
	YourVote   string `json:"yourVote"`
}

func decodeResults(t *testing.T, res apiResponse) resultsPayload {
	t.Helper()
	var payload resultsPayload
	if err := json.Unmarshal(res.Data, &payload); err != nil {
		t.Fatalf("decode results: %v (data: %s)", err, string(res.Data))
	}
	return payload
}

// seedPoll registers an owner, creates a poll and then signs out, leaving the
// API in the state a member of the audience would find it: no session at all.
func seedPoll(t *testing.T, api *testAPI, question string, options []string) pollPayload {
	t.Helper()
	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("owner registration failed: %d", rec.Code)
	}
	status, poll := api.createPoll(question, options)
	if status != http.StatusCreated {
		t.Fatalf("create poll status = %d", status)
	}
	api.logout()
	return poll
}

// anonymousVisitor returns a client with its own cookie jar — a separate browser.
func anonymousVisitor(api *testAPI) *testAPI {
	visitor := *api
	visitor.cookies = map[string]string{}
	return &visitor
}

func TestVotingNeedsNoAccount(t *testing.T) {
	api := newTestAPI(t)
	poll := seedPoll(t, api, "Which release do we ship first?", []string{"The one with tests", "The one without"})

	voter := anonymousVisitor(api)
	rec, res := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[0].ID,
	})
	assertStatus(t, rec, http.StatusCreated)

	results := decodeResults(t, res)
	if results.TotalVotes != 1 {
		t.Errorf("totalVotes = %d, want 1", results.TotalVotes)
	}
	if results.YourVote != poll.Options[0].ID {
		t.Errorf("yourVote = %q, want the option just chosen", results.YourVote)
	}

	t.Run("the voter was given an anonymous identity", func(t *testing.T) {
		if _, ok := voter.cookies[middleware.VoterCookieName]; !ok {
			t.Fatal("no voter cookie was issued")
		}
		if _, ok := voter.cookies[middleware.SessionCookieName]; ok {
			t.Error("voting must not create a login session")
		}
	})

	t.Run("every option is reported, including the unchosen one", func(t *testing.T) {
		if len(results.Results) != 2 {
			t.Fatalf("got %d result rows, want 2", len(results.Results))
		}
		if results.Results[0].Count != 1 || results.Results[1].Count != 0 {
			t.Errorf("counts = %d/%d, want 1/0", results.Results[0].Count, results.Results[1].Count)
		}
		if results.Results[0].Text == "" {
			t.Error("result rows should carry the option text so results render standalone")
		}
	})
}

func TestDuplicateVoteIsRefused(t *testing.T) {
	api := newTestAPI(t)
	poll := seedPoll(t, api, "Vote once?", []string{"Yes", "No"})

	voter := anonymousVisitor(api)
	if rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[0].ID,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("first vote status = %d", rec.Code)
	}

	t.Run("the same browser voting again is a conflict", func(t *testing.T) {
		rec, res := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
			"optionId": poll.Options[1].ID,
		})
		assertStatus(t, rec, http.StatusConflict)
		assertErrorCode(t, res, "ALREADY_VOTED")
	})

	t.Run("the refused vote did not change the tally", func(t *testing.T) {
		rec, res := voter.do(http.MethodGet, "/api/polls/"+poll.ID+"/results", nil)
		assertStatus(t, rec, http.StatusOK)

		results := decodeResults(t, res)
		if results.TotalVotes != 1 {
			t.Errorf("totalVotes = %d, want 1", results.TotalVotes)
		}
		if results.Results[0].Count != 1 || results.Results[1].Count != 0 {
			t.Errorf("counts = %d/%d, want the first vote only", results.Results[0].Count, results.Results[1].Count)
		}
	})

	t.Run("the voter still sees their own choice on return", func(t *testing.T) {
		rec, res := voter.do(http.MethodGet, "/api/polls/"+poll.ID+"/results", nil)
		assertStatus(t, rec, http.StatusOK)
		if got := decodeResults(t, res).YourVote; got != poll.Options[0].ID {
			t.Errorf("yourVote = %q, want the option they chose", got)
		}
	})

	t.Run("a different browser can still vote", func(t *testing.T) {
		other := anonymousVisitor(api)
		rec, res := other.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
			"optionId": poll.Options[1].ID,
		})
		assertStatus(t, rec, http.StatusCreated)
		if got := decodeResults(t, res).TotalVotes; got != 2 {
			t.Errorf("totalVotes = %d, want 2", got)
		}
	})
}

// TestForgedVoterCookieIsIgnored checks the signature on the anonymous identity.
// Without it, a client could hand itself a fresh identity per request and vote as
// often as it liked without even clearing cookies.
func TestForgedVoterCookieIsIgnored(t *testing.T) {
	api := newTestAPI(t)
	poll := seedPoll(t, api, "Can I forge an identity?", []string{"Yes", "No"})

	voter := anonymousVisitor(api)
	if rec, _ := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
		"optionId": poll.Options[0].ID,
	}); rec.Code != http.StatusCreated {
		t.Fatalf("first vote status = %d", rec.Code)
	}
	issued := voter.cookies[middleware.VoterCookieName]

	forgeries := map[string]string{
		"invented value":     "i-made-this-up",
		"no signature":       "abcdefghijklmnopqrstuv",
		"tampered identity":  "tampered." + issued[len(issued)-43:],
		"tampered signature": issued[:len(issued)-4] + "AAAA",
		"empty":              "",
	}

	for name, forged := range forgeries {
		t.Run(name+" is replaced with a fresh identity", func(t *testing.T) {
			attacker := anonymousVisitor(api)
			attacker.cookies[middleware.VoterCookieName] = forged

			rec, _ := attacker.do(http.MethodGet, "/api/polls/"+poll.ID+"/results", nil)
			assertStatus(t, rec, http.StatusOK)

			// The server refuses to honour the forged value and issues its own.
			replacement := attacker.cookies[middleware.VoterCookieName]
			if replacement == forged {
				t.Fatalf("the server accepted a forged voter cookie %q", forged)
			}
			if replacement == "" {
				t.Fatal("no replacement identity was issued")
			}
		})
	}
}

func TestVoteRejections(t *testing.T) {
	api := newTestAPI(t)
	poll := seedPoll(t, api, "What gets rejected?", []string{"A", "B"})

	other := seedPollAsSecondOwner(t, api)

	cases := []struct {
		name     string
		pollID   string
		optionID string
		status   int
		code     string
	}{
		{"unknown option", poll.ID, "not-a-real-option", http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{"empty option", poll.ID, "", http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{"an option belonging to another poll", poll.ID, other.Options[0].ID, http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{"unknown poll", "6aae43849751081b4d469065", poll.Options[0].ID, http.StatusNotFound, "NOT_FOUND"},
		{"malformed poll id", "not-an-object-id", poll.Options[0].ID, http.StatusNotFound, "NOT_FOUND"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			voter := anonymousVisitor(api)
			rec, res := voter.do(http.MethodPost, "/api/polls/"+tc.pollID+"/vote", map[string]string{
				"optionId": tc.optionID,
			})
			assertStatus(t, rec, tc.status)
			assertErrorCode(t, res, tc.code)
		})
	}

	t.Run("none of the rejected attempts were recorded", func(t *testing.T) {
		rec, res := anonymousVisitor(api).do(http.MethodGet, "/api/polls/"+poll.ID+"/results", nil)
		assertStatus(t, rec, http.StatusOK)
		if got := decodeResults(t, res).TotalVotes; got != 0 {
			t.Errorf("totalVotes = %d after only invalid attempts, want 0", got)
		}
	})
}

func TestVotingOnAClosedPoll(t *testing.T) {
	api := newTestAPI(t)

	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("owner registration failed: %d", rec.Code)
	}
	status, poll := api.createPoll("Closing time?", []string{"Yes", "No"})
	if status != http.StatusCreated {
		t.Fatalf("create poll status = %d", status)
	}
	if rec, _ := api.do(http.MethodPost, "/api/polls/"+poll.ID+"/close", map[string]string{}); rec.Code != http.StatusOK {
		t.Fatalf("close status = %d", rec.Code)
	}
	api.logout()

	t.Run("a vote is refused", func(t *testing.T) {
		voter := anonymousVisitor(api)
		rec, res := voter.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
			"optionId": poll.Options[0].ID,
		})
		assertStatus(t, rec, http.StatusConflict)
		assertErrorCode(t, res, "POLL_CLOSED")
	})

	t.Run("but the results are still readable", func(t *testing.T) {
		rec, res := anonymousVisitor(api).do(http.MethodGet, "/api/polls/"+poll.ID+"/results", nil)
		assertStatus(t, rec, http.StatusOK)
		if got := decodeResults(t, res).Status; got != "closed" {
			t.Errorf("status = %q, want closed", got)
		}
	})
}

// seedPollAsSecondOwner creates a poll owned by a different account, used as the
// source of a "foreign" option id.
func seedPollAsSecondOwner(t *testing.T, api *testAPI) pollPayload {
	t.Helper()
	owner := anonymousVisitor(api)
	if rec, _ := owner.register("Grace", "grace@example.com", "another-password"); rec.Code != http.StatusCreated {
		t.Fatalf("second owner registration failed: %d", rec.Code)
	}
	status, poll := owner.createPoll("A different poll", []string{"Elsewhere", "Somewhere"})
	if status != http.StatusCreated {
		t.Fatalf("second poll creation status = %d", status)
	}
	return poll
}
