// Package repositories is the only place that talks to MongoDB.
//
// Each repository translates driver-specific results into domain errors, so no
// other layer has to know what a mongo.ErrNoDocuments or a duplicate-key write
// exception is. That translation is also what keeps driver text out of API
// responses.
package repositories

import (
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
)

// ParseObjectID converts a hex ID from a URL or a cookie into an ObjectID.
//
// A malformed ID is reported as "not found" rather than as a validation error:
// from the caller's point of view /api/polls/nonsense and /api/polls/<valid but
// unknown id> are the same situation, and answering differently would let an
// attacker distinguish "this ID format is real" from "this poll exists".
func ParseObjectID(hex string) (bson.ObjectID, error) {
	id, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		return bson.NilObjectID, apperr.New(apperr.CodeNotFound, "Not found.")
	}
	return id, nil
}

// isNotFound reports whether a driver error means "no such document".
func isNotFound(err error) bool { return errors.Is(err, mongo.ErrNoDocuments) }
