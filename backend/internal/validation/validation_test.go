package validation

import (
	"strings"
	"testing"
)

func TestEmail(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantValid bool
		wantValue string
	}{
		{"plain address", "ada@example.com", true, "ada@example.com"},
		{"upper case is normalised", "  Ada@Example.COM  ", true, "ada@example.com"},
		{"plus addressing", "ada+polls@example.com", true, "ada+polls@example.com"},
		{"empty", "", false, ""},
		{"no at sign", "ada.example.com", false, "ada.example.com"},
		{"no domain dot", "ada@localhost", false, "ada@localhost"},
		{"display name form is rejected", "Ada <ada@example.com>", false, "ada <ada@example.com>"},
		{"spaces inside", "ada lovelace@example.com", false, "ada lovelace@example.com"},
		{"too long", strings.Repeat("a", 250) + "@example.com", false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := New()
			got := v.Email("email", tc.input)
			if v.Valid() != tc.wantValid {
				t.Fatalf("Email(%q) valid = %v, want %v (fields: %v)", tc.input, v.Valid(), tc.wantValid, v.Fields())
			}
			if tc.wantValid && got != tc.wantValue {
				t.Errorf("Email(%q) = %q, want %q", tc.input, got, tc.wantValue)
			}
		})
	}
}

func TestPassword(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantValid bool
	}{
		{"typical", "correct-horse-battery", true},
		{"exactly the minimum", strings.Repeat("a", PasswordMinLength), true},
		{"exactly the maximum", strings.Repeat("a", PasswordMaxLength), true},
		{"empty", "", false},
		{"too short", "short", false},
		{"beyond bcrypt's 72-byte input limit", strings.Repeat("a", PasswordMaxLength+1), false},
		{"spaces are kept, not trimmed away", "  spaces  ", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := New()
			v.Password("password", tc.input)
			if v.Valid() != tc.wantValid {
				t.Errorf("Password(%q) valid = %v, want %v", tc.input, v.Valid(), tc.wantValid)
			}
		})
	}
}

func TestCleanText(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"trims and collapses whitespace", "  hello   world  ", "hello world"},
		{"newlines become spaces", "hello\nworld", "hello world"},
		{"tabs become spaces", "hello\tworld", "hello world"},
		{"control characters are stripped", "hel\x00lo", "hello"},
		{"zero-width characters are stripped", "hel" + string(rune(0x200B)) + "lo", "hello"},
		{"byte order marks are stripped", string(rune(0xFEFF)) + "hello", "hello"},
		{"unicode text survives", "¿Cuál prefieres?", "¿Cuál prefieres?"},
		{"emoji survive", "Pizza 🍕", "Pizza 🍕"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CleanText(tc.input); got != tc.want {
				t.Errorf("CleanText(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestQuestion(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantValid bool
	}{
		{"typical", "Which release do we ship first?", true},
		{"empty", "", false},
		{"whitespace only", "     ", false},
		{"too short", "Hi", false},
		{"at the limit", strings.Repeat("a", QuestionMaxLength), true},
		{"over the limit", strings.Repeat("a", QuestionMaxLength+1), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := New()
			v.Question("question", tc.input)
			if v.Valid() != tc.wantValid {
				t.Errorf("Question(%q…) valid = %v, want %v", truncate(tc.input), v.Valid(), tc.wantValid)
			}
		})
	}
}

func TestOptions(t *testing.T) {
	many := make([]string, MaxOptions+1)
	for i := range many {
		many[i] = string(rune('a' + i))
	}

	cases := []struct {
		name      string
		input     []string
		wantValid bool
		wantCount int
	}{
		{"two options", []string{"Yes", "No"}, true, 2},
		{"blank entries are dropped before counting", []string{"Yes", "No", "  ", ""}, true, 2},
		{"only one real option", []string{"Yes", "   "}, false, 1},
		{"none", nil, false, 0},
		{"too many", many, false, len(many)},
		{"duplicates", []string{"Yes", "yes"}, false, 2},
		{"duplicates after whitespace collapsing", []string{"Ship it", "Ship  it"}, false, 2},
		{"an over-long option", []string{"Yes", strings.Repeat("a", OptionTextMaxLength+1)}, false, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := New()
			got := v.Options("options", tc.input)
			if v.Valid() != tc.wantValid {
				t.Errorf("Options(%v) valid = %v, want %v (fields: %v)", tc.input, v.Valid(), tc.wantValid, v.Fields())
			}
			if len(got) != tc.wantCount {
				t.Errorf("Options(%v) returned %d cleaned values, want %d", tc.input, len(got), tc.wantCount)
			}
		})
	}
}

func TestValidatorKeepsTheFirstMessagePerField(t *testing.T) {
	v := New()
	v.Add("email", "first")
	v.Add("email", "second")

	if got := v.Fields()["email"]; got != "first" {
		t.Errorf("field message = %q, want the first one recorded", got)
	}
	if len(v.Fields()) != 1 {
		t.Errorf("fields = %v, want a single entry", v.Fields())
	}
}

func truncate(s string) string {
	if len(s) > 20 {
		return s[:20]
	}
	return s
}
