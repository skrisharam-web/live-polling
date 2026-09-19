// Package validation holds every server-side input rule in one place.
//
// The frontend validates too, but that is a convenience for the person typing.
// These rules are the ones that actually protect the database: they run inside
// the service layer, after binding and before any repository call, so there is no
// path into MongoDB that bypasses them.
package validation

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits are exported so tests, documentation and the frontend can agree on the
// same numbers instead of each hard-coding their own.
const (
	NameMinLength     = 2
	NameMaxLength     = 64
	EmailMaxLength    = 254 // the practical maximum length of an address
	PasswordMinLength = 8
	// PasswordMaxLength is 72 because bcrypt hashes only the first 72 bytes of
	// input. Accepting more would silently ignore the rest, which would make two
	// different long passwords interchangeable.
	PasswordMaxLength = 72

	QuestionMinLength   = 3
	QuestionMaxLength   = 300
	OptionTextMinLength = 1
	OptionTextMaxLength = 120
	MinOptions          = 2
	MaxOptions          = 10
)

// Validator accumulates per-field messages so one response can report every
// problem at once, rather than making the user fix them one round trip at a time.
type Validator struct {
	fields map[string]string
}

func New() *Validator { return &Validator{fields: make(map[string]string)} }

// Add records the first error seen for a field. Later errors for the same field
// are dropped: the first is the most specific and a list of three complaints
// about one input is noise.
func (v *Validator) Add(field, message string) {
	if _, exists := v.fields[field]; !exists {
		v.fields[field] = message
	}
}

// Valid reports whether nothing has been recorded.
func (v *Validator) Valid() bool { return len(v.fields) == 0 }

// Fields returns the accumulated messages, keyed by request field name.
func (v *Validator) Fields() map[string]string { return v.fields }

// NormaliseEmail lower-cases and trims an address so that the unique index means
// one account per address rather than one per spelling.
func NormaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// CleanText trims surrounding whitespace, collapses internal runs of whitespace
// and strips control characters.
//
// Control characters matter here because poll questions and option labels are
// shown to an audience: a stray newline or a zero-width run can be used to make
// one option impersonate another in a list.
func CleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || r == 0xFEFF || r == 0x200B {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// Name checks a display name and returns its cleaned form.
func (v *Validator) Name(field, value string) string {
	clean := CleanText(value)
	switch {
	case clean == "":
		v.Add(field, "Enter your name.")
	case utf8.RuneCountInString(clean) < NameMinLength:
		v.Add(field, fmt.Sprintf("Your name must be at least %d characters.", NameMinLength))
	case utf8.RuneCountInString(clean) > NameMaxLength:
		v.Add(field, fmt.Sprintf("Your name must be %d characters or fewer.", NameMaxLength))
	}
	return clean
}

// Email checks an address and returns its normalised form.
func (v *Validator) Email(field, value string) string {
	normalised := NormaliseEmail(value)
	switch {
	case normalised == "":
		v.Add(field, "Enter your e-mail address.")
	case len(normalised) > EmailMaxLength:
		v.Add(field, "That e-mail address is too long.")
	default:
		// net/mail accepts display-name forms such as "Ada <ada@example.com>",
		// which are not what a login field should contain, so the parsed address
		// must match the input exactly.
		addr, err := mail.ParseAddress(normalised)
		if err != nil || addr.Address != normalised || !strings.Contains(normalised, ".") {
			v.Add(field, "Enter a valid e-mail address.")
		}
	}
	return normalised
}

// Password checks a new password. It is never cleaned or trimmed: leading and
// trailing spaces are legitimate characters that the user deliberately typed.
func (v *Validator) Password(field, value string) {
	switch {
	case value == "":
		v.Add(field, "Enter a password.")
	case len(value) < PasswordMinLength:
		v.Add(field, fmt.Sprintf("Your password must be at least %d characters.", PasswordMinLength))
	case len(value) > PasswordMaxLength:
		v.Add(field, fmt.Sprintf("Your password must be %d characters or fewer.", PasswordMaxLength))
	}
}

// Required checks that a non-empty password was supplied on login. Login must not
// apply the strength rules: those change over time, and an account created under
// older rules must still be able to sign in.
func (v *Validator) Required(field, value string) {
	if strings.TrimSpace(value) == "" {
		v.Add(field, "This field is required.")
	}
}

// Question checks a poll question and returns its cleaned form.
func (v *Validator) Question(field, value string) string {
	clean := CleanText(value)
	switch {
	case clean == "":
		v.Add(field, "Enter a question.")
	case utf8.RuneCountInString(clean) < QuestionMinLength:
		v.Add(field, fmt.Sprintf("The question must be at least %d characters.", QuestionMinLength))
	case utf8.RuneCountInString(clean) > QuestionMaxLength:
		v.Add(field, fmt.Sprintf("The question must be %d characters or fewer.", QuestionMaxLength))
	}
	return clean
}

// Options checks the list of option labels and returns their cleaned forms.
//
// Duplicates are rejected because two identically labelled options split the vote
// for one answer, which makes the result meaningless and is almost always a typo.
func (v *Validator) Options(field string, values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		clean := CleanText(value)
		if clean == "" {
			continue
		}
		cleaned = append(cleaned, clean)
	}

	switch {
	case len(cleaned) < MinOptions:
		v.Add(field, fmt.Sprintf("Add at least %d options.", MinOptions))
		return cleaned
	case len(cleaned) > MaxOptions:
		v.Add(field, fmt.Sprintf("A poll can have at most %d options.", MaxOptions))
		return cleaned
	}

	seen := make(map[string]struct{}, len(cleaned))
	for _, option := range cleaned {
		if utf8.RuneCountInString(option) > OptionTextMaxLength {
			v.Add(field, fmt.Sprintf("Each option must be %d characters or fewer.", OptionTextMaxLength))
			break
		}
		key := strings.ToLower(option)
		if _, duplicate := seen[key]; duplicate {
			v.Add(field, "Each option must be different.")
			break
		}
		seen[key] = struct{}{}
	}

	return cleaned
}
