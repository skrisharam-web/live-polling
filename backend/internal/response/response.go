// Package response owns the HTTP wire format. Every handler answers through the
// helpers here so success and failure always look the same to the client, and so
// internal detail (driver errors, stack traces, key names) can never leak by
// accident: only the fields set below are ever serialised.
package response

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
)

// Envelope is the single response shape: {"success": true, "data": ...} or
// {"success": false, "error": {...}}.
type Envelope struct {
	Success bool      `json:"success"`
	Data    any       `json:"data,omitempty"`
	Error   *APIError `json:"error,omitempty"`
}

// APIError is the public projection of a domain error.
type APIError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// OK writes 200 with a data payload.
func OK(c *gin.Context, data any) { c.JSON(http.StatusOK, Envelope{Success: true, Data: data}) }

// Created writes 201 with a data payload.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Envelope{Success: true, Data: data})
}

// NoContent acknowledges a successful side effect that has no body.
func NoContent(c *gin.Context) { c.JSON(http.StatusOK, Envelope{Success: true, Data: gin.H{}}) }

// statusFor maps a domain error code to the HTTP status that expresses it.
func statusFor(code apperr.Code) int {
	switch code {
	case apperr.CodeValidation:
		return http.StatusUnprocessableEntity
	case apperr.CodeUnauthorized:
		return http.StatusUnauthorized
	case apperr.CodeForbidden:
		return http.StatusForbidden
	case apperr.CodeNotFound:
		return http.StatusNotFound
	case apperr.CodeConflict, apperr.CodeAlreadyVoted, apperr.CodePollClosed:
		return http.StatusConflict
	case apperr.CodeRateLimited:
		return http.StatusTooManyRequests
	case apperr.CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// Fail writes an explicit error response.
func Fail(c *gin.Context, code apperr.Code, message string) {
	writeError(c, statusFor(code), &APIError{Code: string(code), Message: message})
}

// FailValidation writes a 422 with per-field messages.
func FailValidation(c *gin.Context, message string, fields map[string]string) {
	writeError(c, http.StatusUnprocessableEntity, &APIError{
		Code:    string(apperr.CodeValidation),
		Message: message,
		Fields:  fields,
	})
}

// Error is the handler's single exit for a service error. Domain errors keep
// their code and message; anything else becomes a generic 500 and the real error
// is logged rather than returned, so internals never reach the client.
func Error(c *gin.Context, err error) {
	var domain *apperr.Error
	if errors.As(err, &domain) {
		if domain.Code == apperr.CodeInternal || domain.Code == apperr.CodeUnavailable {
			logInternal(c, domain)
		}
		writeError(c, statusFor(domain.Code), &APIError{
			Code:    string(domain.Code),
			Message: domain.Message,
			Fields:  domain.Fields,
		})
		return
	}

	logInternal(c, err)
	writeError(c, http.StatusInternalServerError, &APIError{
		Code:    string(apperr.CodeInternal),
		Message: "Something went wrong. Please try again.",
	})
}

func logInternal(c *gin.Context, err error) {
	slog.Error("request failed",
		"error", err,
		"path", c.FullPath(),
		"method", c.Request.Method,
		"request_id", c.GetString("request_id"),
	)
}

func writeError(c *gin.Context, status int, apiErr *APIError) {
	c.AbortWithStatusJSON(status, Envelope{Success: false, Error: apiErr})
}
