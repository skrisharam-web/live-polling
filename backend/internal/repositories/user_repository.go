package repositories

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/database"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// UserRepository persists accounts.
type UserRepository struct {
	col *mongo.Collection
}

func NewUserRepository(db *database.DB) *UserRepository {
	return &UserRepository{col: db.Collection(database.CollectionUsers)}
}

// Create inserts a new user and fills in its generated ID.
//
// Uniqueness of the e-mail address is enforced by the unique index, not by a
// prior lookup: two simultaneous registrations for the same address would both
// pass a read-then-write check, but only one can win an indexed insert.
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	result, err := r.col.InsertOne(ctx, user)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apperr.New(apperr.CodeConflict, "An account with that e-mail address already exists.")
		}
		return apperr.Wrap(apperr.CodeInternal, "Could not create the account.", err)
	}
	if id, ok := result.InsertedID.(bson.ObjectID); ok {
		user.ID = id
	}
	return nil
}

// FindByEmail looks up an account for login. The e-mail must already be
// normalised by the caller; the stored form is always lower-case.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := r.col.FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err != nil {
		if isNotFound(err) {
			return nil, apperr.New(apperr.CodeNotFound, "Account not found.")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not look up the account.", err)
	}
	return &user, nil
}

// FindByID resolves the user behind an authenticated session.
func (r *UserRepository) FindByID(ctx context.Context, id bson.ObjectID) (*models.User, error) {
	var user models.User
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&user)
	if err != nil {
		if isNotFound(err) {
			return nil, apperr.New(apperr.CodeNotFound, "Account not found.")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not look up the account.", err)
	}
	return &user, nil
}
