// Package apperr defines the error vocabulary shared by the service layer.
//
// Services never write HTTP responses; they return an *Error carrying a stable
// machine-readable code. The response package is the only place that turns those
// codes into status codes and JSON, which keeps transport concerns out of the
// business logic and guarantees every error leaves the process in one shape.
package apperr

import "fmt"

// Code is a stable, client-facing identifier for a class of failure.
type Code string

const (
	CodeValidation   Code = "VALIDATION_ERROR"
	CodeUnauthorized Code = "UNAUTHORIZED"
	CodeForbidden    Code = "FORBIDDEN"
	CodeNotFound     Code = "NOT_FOUND"
	CodeConflict     Code = "CONFLICT"
	CodeAlreadyVoted Code = "ALREADY_VOTED"
	CodePollClosed   Code = "POLL_CLOSED"
	CodeRateLimited  Code = "RATE_LIMITED"
	CodeTooLarge     Code = "PAYLOAD_TOO_LARGE"
	CodeInternal     Code = "INTERNAL_ERROR"
	CodeUnavailable  Code = "DEPENDENCY_UNAVAILABLE"
)

// Error is a domain error. Message is safe to show to a client: it must never
// contain driver output, query fragments or secrets. Cause is kept for logging
// only and is never serialised.
type Error struct {
	Code    Code
	Message string
	// Fields carries per-field validation messages, keyed by the request field name.
	Fields map[string]string
	Cause  error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// New builds a domain error with no underlying cause.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Wrap builds a domain error that keeps the original error for the logs.
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

// Validation builds a validation error carrying per-field messages.
func Validation(message string, fields map[string]string) *Error {
	return &Error{Code: CodeValidation, Message: message, Fields: fields}
}

// Is reports whether err is an *Error with the given code.
func Is(err error, code Code) bool {
	var e *Error
	if As(err, &e) {
		return e.Code == code
	}
	return false
}
