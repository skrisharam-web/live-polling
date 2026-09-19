# Database design

MongoDB is the durable source of truth for this application. Every user, poll and
vote is a document here, and everything Redis holds is derived data that can be
recomputed from these collections.

Database name: `livepolling` (configurable via `MONGODB_DATABASE`).

## Collections

### `users`

```jsonc
{
  "_id":          ObjectId("..."),
  "name":         "Ada Lovelace",
  "email":        "ada@example.com",   // stored lower-cased and trimmed
  "passwordHash": "$2a$12$...",        // bcrypt, cost 12 — never returned by the API
  "createdAt":    ISODate("..."),
  "updatedAt":    ISODate("...")
}
```

The e-mail is normalised before it is written so that "one account per address"
means one account per address, not one per spelling.

### `polls`

```jsonc
{
  "_id":       ObjectId("..."),   // also the share token in /polls/:id
  "ownerId":   ObjectId("..."),   // → users._id
  "question":  "Which release do we ship first?",
  "options": [
    { "id": "a1b2c3d4", "text": "The one with tests" },
    { "id": "e5f6a7b8", "text": "The one without" }
  ],
  "status":    "active",          // "active" | "closed"
  "createdAt": ISODate("..."),
  "updatedAt": ISODate("..."),
  "expiresAt": ISODate("...")     // optional
}
```

Options are **embedded**, not a separate collection. They are only ever read as
part of their poll, they are bounded in number, and embedding them means rendering
a poll is a single document read with no join.

Option IDs are generated on the server when the poll is created. A client can
therefore only submit an option ID the server itself minted, and validation is a
membership check against the poll document.

`expiresAt` needs no background job: `Poll.AcceptsVotes` treats an elapsed expiry
as closed, so expiry is evaluated at read time.

### `votes`

```jsonc
{
  "_id":       ObjectId("..."),
  "pollId":    ObjectId("..."),   // → polls._id
  "optionId":  "a1b2c3d4",        // → polls.options[].id
  "voterId":   "9f2c...",         // anonymous identity from the voter cookie
  "createdAt": ISODate("...")
}
```

One document per vote rather than a counter on the poll. That costs a little
storage and buys three things: the counts can always be re-derived, a voter's own
choice can be shown back to them when they return, and the duplicate-vote rule can
be enforced by an index instead of by application logic.

`voterId` is never read from the request body — it comes from a signed, HTTP-only
cookie the server issued.

## Indexes

Created idempotently at startup by `internal/database/indexes.go`, and asserted by
`tests/integration/indexes_test.go` so they cannot be quietly dropped.

| Collection | Index | Unique | Why it exists |
| --- | --- | --- | --- |
| `users` | `email` | yes | Serves the login lookup, and makes duplicate registration impossible even when two requests race. |
| `votes` | `pollId + voterId` | **yes** | The duplicate-vote guarantee. See below. |
| `votes` | `pollId` | no | Serves the result aggregation and the Redis rebuild, which both scan one poll's votes. |
| `polls` | `ownerId + createdAt (desc)` | no | Serves the dashboard query — a user's polls, newest first — from the index alone. |
| `polls` | `createdAt (desc)` | no | Ordering for administrative/recent-poll queries that are not owner-scoped. |

### Why duplicate voting is an index, not an `if`

The obvious implementation is: look for an existing vote, and insert if there
isn't one. Between those two operations there is a window, and two requests from
the same voter — a double tap on a phone, or a deliberate replay — can both pass
the check and both insert.

A unique index closes the window by making the database itself reject the second
write. The application inserts optimistically and translates the duplicate-key
error into `409 ALREADY_VOTED`. The guarantee then holds regardless of how many
backend instances are running, which a read-then-write check could never promise.

`tests/integration/concurrency_test.go` fires concurrent votes from one voter and
asserts that exactly one insert survives.

## Relationship to Redis

| Question | MongoDB | Redis |
| --- | --- | --- |
| Is a vote recorded? | Yes — authoritative | No |
| What is the live count? | Can compute (aggregation) | Yes — serves the hot path |
| Can it be rebuilt from the other? | No | Yes, from `votes` |

A vote is written to MongoDB first and acknowledged only if that write succeeds.
Redis is updated afterwards; a Redis failure at that point is logged, and the
counters are repaired by `ResultService.Rebuild`, which re-runs the aggregation
above and replaces the hash. See [`realtime.md`](./realtime.md).

## Operational notes

- Connection pool: max 50, 10s server-selection and connect timeouts, retryable
  writes on.
- No transactions are used. Every write in the application is a single-document
  operation, which MongoDB already makes atomic; adding transactions would require
  a replica set for no correctness gain.
- Deleting a poll also deletes its votes (`VoteRepository.DeleteByPoll`), otherwise
  orphaned votes would keep occupying the unique index.
