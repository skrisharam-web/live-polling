package repositories

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/database"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// VoteRepository persists votes. This collection is the durable record of every
// result the application ever shows.
type VoteRepository struct {
	col *mongo.Collection
}

func NewVoteRepository(db *database.DB) *VoteRepository {
	return &VoteRepository{col: db.Collection(database.CollectionVotes)}
}

// Create records a vote.
//
// Duplicate prevention lives here, in the unique (pollId, voterId) index. The
// alternative — check for an existing vote, then insert — has a window between
// the two operations in which a second request can slip through, and that window
// is trivially hit by a double-tap on a phone. Letting the insert fail and
// translating the duplicate-key error closes it completely.
func (r *VoteRepository) Create(ctx context.Context, vote *models.Vote) error {
	result, err := r.col.InsertOne(ctx, vote)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apperr.New(apperr.CodeAlreadyVoted, "You have already voted in this poll.")
		}
		return apperr.Wrap(apperr.CodeInternal, "Could not record your vote.", err)
	}
	if id, ok := result.InsertedID.(bson.ObjectID); ok {
		vote.ID = id
	}
	return nil
}

// FindByPollAndVoter returns this voter's existing vote, or a NOT_FOUND error if
// they have not voted. It is what lets the poll page show "you chose X" when
// somebody returns to a poll they already answered.
func (r *VoteRepository) FindByPollAndVoter(ctx context.Context, pollID bson.ObjectID, voterID string) (*models.Vote, error) {
	var vote models.Vote
	err := r.col.FindOne(ctx, bson.M{"pollId": pollID, "voterId": voterID}).Decode(&vote)
	if err != nil {
		if isNotFound(err) {
			return nil, apperr.New(apperr.CodeNotFound, "No vote recorded.")
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not look up your vote.", err)
	}
	return &vote, nil
}

// CountsByPoll aggregates votes per option straight from MongoDB.
//
// This is the authoritative tally. It backs the Redis rebuild and the fallback
// path when Redis is unavailable, which is precisely why Redis can be treated as
// a disposable cache rather than a second source of truth.
func (r *VoteRepository) CountsByPoll(ctx context.Context, pollID bson.ObjectID) ([]models.OptionCount, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "pollId", Value: pollID}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$optionId"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := r.col.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not tally the votes.", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	counts := make([]models.OptionCount, 0)
	if err := cursor.All(ctx, &counts); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not tally the votes.", err)
	}
	return counts, nil
}

// DeleteByPoll removes every vote for a poll. Deleting a poll without this would
// leave orphaned votes that still occupy the unique index.
func (r *VoteRepository) DeleteByPoll(ctx context.Context, pollID bson.ObjectID) error {
	if _, err := r.col.DeleteMany(ctx, bson.M{"pollId": pollID}); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "Could not delete the poll's votes.", err)
	}
	return nil
}

// CountByPoll returns the total number of votes cast in a poll.
func (r *VoteRepository) CountByPoll(ctx context.Context, pollID bson.ObjectID) (int64, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"pollId": pollID})
	if err != nil {
		return 0, apperr.Wrap(apperr.CodeInternal, "Could not count the votes.", err)
	}
	return count, nil
}

// TotalsByPolls returns vote totals for several polls in one round trip.
//
// The dashboard shows a total beside every poll. Counting them one at a time
// would issue a query per row — the classic N+1 — so the totals are aggregated in
// a single grouped query instead, served by the votes.pollId index.
func (r *VoteRepository) TotalsByPolls(ctx context.Context, pollIDs []bson.ObjectID) (map[bson.ObjectID]int64, error) {
	totals := make(map[bson.ObjectID]int64, len(pollIDs))
	if len(pollIDs) == 0 {
		return totals, nil
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "pollId", Value: bson.D{{Key: "$in", Value: pollIDs}}}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$pollId"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := r.col.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not count the votes.", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var rows []struct {
		PollID bson.ObjectID `bson:"_id"`
		Count  int64         `bson:"count"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not count the votes.", err)
	}

	for _, row := range rows {
		totals[row.PollID] = row.Count
	}
	return totals, nil
}
