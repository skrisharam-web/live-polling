package integration

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/database"
)

// TestIndexesExist pins down the indexes the application's correctness depends
// on. If someone later removes the unique (pollId, voterId) index, duplicate
// voting silently becomes possible; this test turns that into a build failure.
func TestIndexesExist(t *testing.T) {
	db := newTestDB(t)
	ctx := testContext(t)

	type want struct {
		name   string
		keys   bson.D
		unique bool
	}

	expectations := map[string][]want{
		database.CollectionUsers: {
			{name: "uniq_email", keys: bson.D{{Key: "email", Value: int32(1)}}, unique: true},
		},
		database.CollectionVotes: {
			{name: "uniq_poll_voter", keys: bson.D{{Key: "pollId", Value: int32(1)}, {Key: "voterId", Value: int32(1)}}, unique: true},
			{name: "poll_id", keys: bson.D{{Key: "pollId", Value: int32(1)}}},
		},
		database.CollectionPolls: {
			{name: "owner_created_at", keys: bson.D{{Key: "ownerId", Value: int32(1)}, {Key: "createdAt", Value: int32(-1)}}},
			{name: "created_at", keys: bson.D{{Key: "createdAt", Value: int32(-1)}}},
		},
	}

	for collection, wants := range expectations {
		cursor, err := db.Collection(collection).Indexes().List(ctx)
		if err != nil {
			t.Fatalf("list indexes on %s: %v", collection, err)
		}

		var found []bson.M
		if err := cursor.All(ctx, &found); err != nil {
			t.Fatalf("decode indexes on %s: %v", collection, err)
		}

		byName := make(map[string]bson.M, len(found))
		for _, idx := range found {
			name, _ := idx["name"].(string)
			byName[name] = idx
		}

		for _, w := range wants {
			idx, ok := byName[w.name]
			if !ok {
				t.Errorf("%s is missing index %q (have %v)", collection, w.name, keysOf(byName))
				continue
			}
			keys, ok := idx["key"].(bson.D)
			if !ok {
				t.Errorf("%s.%s has an unreadable key spec: %#v", collection, w.name, idx["key"])
				continue
			}
			if !keysEqual(keys, w.keys) {
				t.Errorf("%s.%s keys = %v, want %v", collection, w.name, keys, w.keys)
			}
			isUnique, _ := idx["unique"].(bool)
			if isUnique != w.unique {
				t.Errorf("%s.%s unique = %v, want %v", collection, w.name, isUnique, w.unique)
			}
		}
	}
}

func keysEqual(got, want bson.D) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i].Key != want[i].Key {
			return false
		}
		if toInt32(got[i].Value) != toInt32(want[i].Value) {
			return false
		}
	}
	return true
}

func toInt32(v any) int32 {
	switch n := v.(type) {
	case int32:
		return n
	case int64:
		return int32(n)
	case int:
		return int32(n)
	case float64:
		return int32(n)
	default:
		return 0
	}
}

func keysOf(m map[string]bson.M) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	return names
}
