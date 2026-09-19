package models

import (
	"testing"
	"time"
)

func testPoll(status PollStatus, expiresAt *time.Time) *Poll {
	return &Poll{
		Question:  "Tabs or spaces?",
		Options:   []Option{{ID: "opt1", Text: "Tabs"}, {ID: "opt2", Text: "Spaces"}},
		Status:    status,
		ExpiresAt: expiresAt,
	}
}

func TestHasOption(t *testing.T) {
	poll := testPoll(PollStatusActive, nil)

	if !poll.HasOption("opt2") {
		t.Error("HasOption should accept an option that belongs to the poll")
	}
	if poll.HasOption("opt3") {
		t.Error("HasOption must reject an option ID the poll does not contain")
	}
	if poll.HasOption("") {
		t.Error("HasOption must reject an empty option ID")
	}
}

func TestAcceptsVotes(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name      string
		poll      *Poll
		want      bool
		wantState PollStatus
	}{
		{"active with no expiry", testPoll(PollStatusActive, nil), true, PollStatusActive},
		{"active expiring later", testPoll(PollStatusActive, &future), true, PollStatusActive},
		{"active but expired", testPoll(PollStatusActive, &past), false, PollStatusClosed},
		{"closed", testPoll(PollStatusClosed, nil), false, PollStatusClosed},
		{"closed and expired", testPoll(PollStatusClosed, &past), false, PollStatusClosed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.poll.AcceptsVotes(now); got != tc.want {
				t.Errorf("AcceptsVotes() = %v, want %v", got, tc.want)
			}
			if got := tc.poll.EffectiveStatus(now); got != tc.wantState {
				t.Errorf("EffectiveStatus() = %v, want %v", got, tc.wantState)
			}
		})
	}
}

func TestExpiryIsInclusiveOfTheDeadline(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	poll := testPoll(PollStatusActive, &now)

	// A poll that expires "now" is over: accepting a vote at the exact deadline
	// would make the advertised close time a lie.
	if poll.AcceptsVotes(now) {
		t.Error("a poll must not accept votes at its exact expiry instant")
	}
}

func TestOptionIDsPreservesOrder(t *testing.T) {
	poll := testPoll(PollStatusActive, nil)

	ids := poll.OptionIDs()
	want := []string{"opt1", "opt2"}
	if len(ids) != len(want) {
		t.Fatalf("OptionIDs() = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("OptionIDs()[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestPollStatusValid(t *testing.T) {
	if !PollStatusActive.Valid() || !PollStatusClosed.Valid() {
		t.Error("active and closed must be valid statuses")
	}
	if PollStatus("deleted").Valid() {
		t.Error("an unknown status must be rejected")
	}
}
