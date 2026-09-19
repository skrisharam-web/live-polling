package events

import (
	"testing"
	"time"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	original := PollResultsUpdated{
		Type:       TypePollResultsUpdated,
		PollID:     "6aae43849751081b4d469065",
		Results:    []OptionCount{{OptionID: "opt-a", Count: 17}, {OptionID: "opt-b", Count: 9}},
		TotalVotes: 26,
		Status:     "active",
		Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
	}

	payload, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode() = %v", err)
	}

	decoded, err := Decode(payload)
	if err != nil {
		t.Fatalf("Decode() = %v", err)
	}
	if decoded.PollID != original.PollID || decoded.TotalVotes != original.TotalVotes {
		t.Errorf("decoded = %+v, want %+v", decoded, original)
	}
	if len(decoded.Results) != 2 || decoded.Results[0].Count != 17 {
		t.Errorf("results = %+v", decoded.Results)
	}
	if !decoded.Timestamp.Equal(original.Timestamp) {
		t.Errorf("timestamp = %v, want %v", decoded.Timestamp, original.Timestamp)
	}
}

func TestEncodeFillsInTheType(t *testing.T) {
	payload, err := Encode(PollResultsUpdated{PollID: "abc"})
	if err != nil {
		t.Fatalf("Encode() = %v", err)
	}
	decoded, err := Decode(payload)
	if err != nil {
		t.Fatalf("Decode() = %v", err)
	}
	if decoded.Type != TypePollResultsUpdated {
		t.Errorf("Type = %q, want %q", decoded.Type, TypePollResultsUpdated)
	}
}

// TestDecodeRejectsAnythingUnexpected matters because the Pub/Sub channel is a
// Redis key pattern: whatever can write to Redis can put bytes there, and those
// bytes must never reach a browser unexamined.
func TestDecodeRejectsAnythingUnexpected(t *testing.T) {
	cases := map[string]string{
		"not json":          "{definitely not json",
		"empty":             "",
		"json but not ours": `{"hello":"world"}`,
		"wrong type":        `{"type":"something_else","pollId":"abc"}`,
		"no poll id":        `{"type":"poll_results_updated"}`,
		"a bare array":      `[1,2,3]`,
	}

	for name, payload := range cases {
		t.Run(name+" is rejected", func(t *testing.T) {
			if _, err := Decode([]byte(payload)); err == nil {
				t.Fatalf("Decode(%q) accepted a payload it should not have", payload)
			}
		})
	}
}

// TestFullTallyIsSelfSufficient documents the reason the event carries the whole
// standing rather than a delta: any single message must be enough to render.
func TestFullTallyIsSelfSufficient(t *testing.T) {
	payload, err := Encode(PollResultsUpdated{
		PollID:     "abc",
		Results:    []OptionCount{{OptionID: "opt-a", Count: 3}, {OptionID: "opt-b", Count: 1}},
		TotalVotes: 4,
		Status:     "active",
	})
	if err != nil {
		t.Fatalf("Encode() = %v", err)
	}

	decoded, err := Decode(payload)
	if err != nil {
		t.Fatalf("Decode() = %v", err)
	}

	var sum int64
	for _, option := range decoded.Results {
		sum += option.Count
	}
	if sum != decoded.TotalVotes {
		t.Errorf("the option counts sum to %d but totalVotes is %d; a client cannot render this consistently", sum, decoded.TotalVotes)
	}
}
