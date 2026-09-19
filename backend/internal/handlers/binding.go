package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/response"
)

// bindJSON decodes a JSON body and reports whether the handler may continue.
//
// A decoding failure is answered with one generic message rather than the
// decoder's own text, which would otherwise expose Go type names and field
// offsets. The real rules live in the service layer, so nothing of value is lost
// by being vague here.
func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		// An oversized body is a different situation from a malformed one, and
		// answering 422 for it would send the client off to look for a typo that
		// does not exist.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Fail(c, apperr.CodeTooLarge, "That request is too large.")
			return false
		}
		response.Fail(c, apperr.CodeValidation, "The request body could not be read as JSON.")
		return false
	}
	return true
}
