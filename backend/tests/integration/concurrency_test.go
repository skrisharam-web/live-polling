package integration

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
	"github.com/skrisharam-web/live-polling/backend/internal/repositories"
)

// TestConcurrentDuplicateVotesAtTheDatabase is the test that justifies the whole
// design of duplicate prevention.
//
// A read-then-write check ("has this voter voted? no? then insert") passes every
// single-threaded test and still fails here: several requests read "no" before
// any of them writes. The unique (pollId, voterId) index has no such window, so
// exactly one insert can win no matter how many arrive together — which is
// precisely what a double-tapped button on a phone produces.
func TestConcurrentDuplicateVotesAtTheDatabase(t *testing.T) {
	db := newTestDB(t)
	ctx := testContext(t)
	votes := repositories.NewVoteRepository(db)

	pollID := bson.NewObjectID()
	const attempts = 40

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		duplicate int
		other     []error
	)

	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Release every goroutine at once, so the inserts genuinely overlap.
			<-start

			err := votes.Create(ctx, &models.Vote{
				PollID:    pollID,
				OptionID:  "opt-a",
				VoterID:   "one-determined-voter",
				CreatedAt: time.Now().UTC(),
			})

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case apperr.Is(err, apperr.CodeAlreadyVoted):
				duplicate++
			default:
				other = append(other, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(other) > 0 {
		t.Fatalf("unexpected errors: %v", other)
	}
	if succeeded != 1 {
		t.Errorf("%d inserts succeeded, want exactly 1", succeeded)
	}
	if duplicate != attempts-1 {
		t.Errorf("%d attempts were reported as duplicates, want %d", duplicate, attempts-1)
	}

	counts, err := votes.CountsByPoll(ctx, pollID)
	if err != nil {
		t.Fatalf("CountsByPoll() = %v", err)
	}
	var total int64
	for _, row := range counts {
		total += row.Count
	}
	if total != 1 {
		t.Errorf("the poll ended up with %d votes, want 1", total)
	}
}

// TestConcurrentVotesFromDifferentVotersAllCount is the other half: the uniqueness
// rule must not accidentally serialise or drop legitimate simultaneous votes,
// which is exactly what a burst of audience traffic looks like.
func TestConcurrentVotesFromDifferentVotersAllCount(t *testing.T) {
	db := newTestDB(t)
	ctx := testContext(t)
	votes := repositories.NewVoteRepository(db)

	pollID := bson.NewObjectID()
	const voters = 60

	var wg sync.WaitGroup
	errs := make(chan error, voters)
	start := make(chan struct{})

	for i := 0; i < voters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start

			optionID := "opt-a"
			if i%2 == 1 {
				optionID = "opt-b"
			}
			if err := votes.Create(ctx, &models.Vote{
				PollID:    pollID,
				OptionID:  optionID,
				VoterID:   "voter-" + bson.NewObjectID().Hex(),
				CreatedAt: time.Now().UTC(),
			}); err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("a legitimate concurrent vote failed: %v", err)
	}

	counts, err := votes.CountsByPoll(ctx, pollID)
	if err != nil {
		t.Fatalf("CountsByPoll() = %v", err)
	}

	tally := map[string]int64{}
	var total int64
	for _, row := range counts {
		tally[row.OptionID] = row.Count
		total += row.Count
	}
	if total != voters {
		t.Errorf("total votes = %d, want %d", total, voters)
	}
	if tally["opt-a"] != voters/2 || tally["opt-b"] != voters/2 {
		t.Errorf("tally = %v, want an even split", tally)
	}
}

// TestConcurrentVotesThroughTheAPI runs the same race one layer up, through the
// real HTTP handler, service and repository, with one browser's cookie.
func TestConcurrentVotesThroughTheAPI(t *testing.T) {
	api := newTestAPI(t)
	poll := seedPoll(t, api, "Double tap?", []string{"Yes", "No"})

	// Establish the voter identity once, the way a browser would, then reuse that
	// exact cookie on every concurrent request.
	voter := anonymousVisitor(api)
	if rec, _ := voter.do(http.MethodGet, "/api/polls/"+poll.ID+"/results", nil); rec.Code != http.StatusOK {
		t.Fatalf("priming request status = %d", rec.Code)
	}

	const attempts = 20
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		created  int
		conflict int
		unknown  []int
	)

	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			// Each goroutine gets its own client struct sharing the same cookie
			// values, because the helper's jar is not itself concurrency-safe.
			client := *voter
			client.cookies = copyCookies(voter.cookies)

			rec, _ := client.do(http.MethodPost, "/api/polls/"+poll.ID+"/vote", map[string]string{
				"optionId": poll.Options[0].ID,
			})

			mu.Lock()
			defer mu.Unlock()
			switch rec.Code {
			case http.StatusCreated:
				created++
			case http.StatusConflict:
				conflict++
			default:
				unknown = append(unknown, rec.Code)
			}
		}()
	}
	close(start)
	wg.Wait()

	if len(unknown) > 0 {
		t.Fatalf("unexpected status codes: %v", unknown)
	}
	if created != 1 {
		t.Errorf("%d requests were accepted, want exactly 1", created)
	}
	if conflict != attempts-1 {
		t.Errorf("%d requests were rejected as duplicates, want %d", conflict, attempts-1)
	}

	rec, res := voter.do(http.MethodGet, "/api/polls/"+poll.ID+"/results", nil)
	assertStatus(t, rec, http.StatusOK)
	if got := decodeResults(t, res).TotalVotes; got != 1 {
		t.Errorf("the poll ended up with %d votes, want 1", got)
	}
}

func copyCookies(source map[string]string) map[string]string {
	copied := make(map[string]string, len(source))
	for name, value := range source {
		copied[name] = value
	}
	return copied
}
