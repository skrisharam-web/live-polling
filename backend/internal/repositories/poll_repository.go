package repositories

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/database"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// PollUpdate describes a partial update. A nil field means "leave unchanged",
// which is what makes PATCH semantics possible without a read-modify-write of the
// whole document.
type PollUpdate struct {
	Question  *string
	Options   []models.Option
	Status    *models.PollStatus
	ExpiresAt *time.Time
	// ClearExpiry removes an existing expiry; it exists because a nil ExpiresAt
	// already means "unchanged" and the two intentions must stay distinguishable.
	ClearExpiry bool
}

// PollRepository persists polls.
type PollRepository struct {
	col *mongo.Collection
}

func NewPollRepository(db *database.DB) *PollRepository {
	return &PollRepository{col: db.Collection(database.CollectionPolls)}
}

// Create inserts a poll and fills in its generated ID.
func (r *PollRepository) Create(ctx context.Context, poll *models.Poll) error {
	result, err := r.col.InsertOne(ctx, poll)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Could not create the poll.", err)
	}
	if id, ok := result.InsertedID.(bson.ObjectID); ok {
		poll.ID = id
	}
	return nil
}

// FindByID loads a poll by its hex ID, as it appears in a share link.
func (r *PollRepository) FindByID(ctx context.Context, id string) (*models.Poll, error) {
	objectID, err := ParseObjectID(id)
	if err != nil {
		return nil, err
	}
	return r.findByObjectID(ctx, objectID)
}

func (r *PollRepository) findByObjectID(ctx context.Context, id bson.ObjectID) (*models.Poll, error) {
	var poll models.Poll
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&poll)
	if err != nil {
		if isNotFound(err) {
			return nil, apperr.New(apperr.CodeNotFound, "Poll not found.")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not load the poll.", err)
	}
	return &poll, nil
}

// ListByOwner returns a user's polls, newest first. This is the dashboard query,
// and it is served by the owner_created_at index rather than a collection scan.
func (r *PollRepository) ListByOwner(ctx context.Context, ownerID bson.ObjectID, limit int64) ([]models.Poll, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(limit)

	cursor, err := r.col.Find(ctx, bson.M{"ownerId": ownerID}, opts)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not load your polls.", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	polls := make([]models.Poll, 0)
	if err := cursor.All(ctx, &polls); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not load your polls.", err)
	}
	return polls, nil
}

// Update applies a partial update and returns the poll as it now stands.
//
// The ownerID is part of the filter rather than checked afterwards, so a poll
// belonging to somebody else simply does not match. The caller has already
// verified ownership; this is the second line of defence.
func (r *PollRepository) Update(ctx context.Context, id, ownerID bson.ObjectID, update PollUpdate) (*models.Poll, error) {
	set := bson.M{"updatedAt": time.Now().UTC()}
	if update.Question != nil {
		set["question"] = *update.Question
	}
	if update.Options != nil {
		set["options"] = update.Options
	}
	if update.Status != nil {
		set["status"] = *update.Status
	}
	if update.ExpiresAt != nil {
		set["expiresAt"] = *update.ExpiresAt
	}

	change := bson.M{"$set": set}
	if update.ClearExpiry {
		change["$unset"] = bson.M{"expiresAt": ""}
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var poll models.Poll
	err := r.col.FindOneAndUpdate(ctx, bson.M{"_id": id, "ownerId": ownerID}, change, opts).Decode(&poll)
	if err != nil {
		if isNotFound(err) {
			return nil, apperr.New(apperr.CodeNotFound, "Poll not found.")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not update the poll.", err)
	}
	return &poll, nil
}

// Delete removes a poll owned by ownerID.
func (r *PollRepository) Delete(ctx context.Context, id, ownerID bson.ObjectID) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id, "ownerId": ownerID})
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Could not delete the poll.", err)
	}
	if result.DeletedCount == 0 {
		return apperr.New(apperr.CodeNotFound, "Poll not found.")
	}
	return nil
}
