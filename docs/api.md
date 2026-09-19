# API

Base URL `http://localhost:8080` in development. Every example below is a real
response captured from a running server, not an illustration.

---

## Conventions

**Every response uses the same envelope.** A client can therefore branch on
`success` before knowing anything else about the endpoint.

```json
{ "success": true,  "data": { } }
{ "success": false, "error": { "code": "…", "message": "…", "fields": { } } }
```

`message` is written for a person and safe to display. `fields` appears only on
validation failures and maps a field name to the sentence belonging under that
input.

**Content type.** Every state-changing request must send
`Content-Type: application/json`. This is not pedantry — it is the CSRF defence.
A browser can only submit a cross-site form as one of three content types, none
of which is JSON, so a forged request becomes "non-simple", requires a preflight,
and is refused by the origin allow-list.

**Authentication** is an `HttpOnly` cookie (`lp_session`) set on register and
login. Send credentials with every request (`fetch(..., { credentials: 'include' })`,
`curl -b jar -c jar`). No bearer token is ever returned in a body.

**Voter identity** is a second cookie (`lp_voter`), signed and `HttpOnly`, issued
automatically on first contact with a voting endpoint. It is never read from a
request body.

---

## Errors

| Code | HTTP | Meaning |
| --- | --- | --- |
| `VALIDATION_ERROR` | 422 | Input was rejected; see `fields` |
| `UNAUTHORIZED` | 401 | No valid session |
| `FORBIDDEN` | 403 | Signed in, but not yours |
| `NOT_FOUND` | 404 | No such poll — also returned for a malformed id |
| `CONFLICT` | 409 | Generic conflict |
| `ALREADY_VOTED` | 409 | This voter has already voted in this poll |
| `POLL_CLOSED` | 409 | Voting has ended |
| `RATE_LIMITED` | 429 | Too many requests in the window |
| `PAYLOAD_TOO_LARGE` | 413 | Body over 64 KB |
| `DEPENDENCY_UNAVAILABLE` | 503 | MongoDB or Redis is unreachable |
| `INTERNAL_ERROR` | 500 | Anything else; details go to the log, never the response |

A malformed poll id answers `404`, not `400`, so the endpoint cannot be used to
learn which id shapes are real.

---

## Health

### `GET /health`

Unauthenticated. Reports each dependency separately, because "the API is up but
Redis is unreachable" and "everything is fine" must not look alike.

```json
{
  "success": true,
  "data": {
    "status": "ok",
    "timestamp": "2026-09-19T10:44:39.111262514Z",
    "services": { "mongodb": { "status": "ok" }, "redis": { "status": "ok" } }
  }
}
```

---

## Authentication

### `POST /api/auth/register`

```json
{ "name": "Ada", "email": "ada@example.com", "password": "correct-horse-battery" }
```

```json
{
  "success": true,
  "data": {
    "user": {
      "id": "6aae6ca1d028e32e5cc4b339",
      "name": "Ada",
      "email": "ada@example.com",
      "createdAt": "2026-09-19T11:06:09.707210751Z"
    }
  }
}
```

Sets `lp_session`. The password is bcrypt-hashed at cost 12 and capped at 72
bytes, because bcrypt silently ignores anything beyond that and accepting a
longer password would make two different long passwords interchangeable.

Registration deliberately tells you when an address is already taken. It is the
one place account enumeration is accepted, because silently doing nothing is a
worse experience for the common case of someone who forgot they had an account.

### `POST /api/auth/login`

Same shape. **Every failure returns an identical code and message**, and an
unknown address still pays for a bcrypt comparison against a dummy hash so the
timing matches. Anything else would let an attacker enumerate accounts.

Rate limited: 20 attempts per 15 minutes per IP.

### `POST /api/auth/logout`

Clears the session cookie. Always succeeds.

### `GET /api/auth/me`

Requires a session; returns the current user.

---

## Polls

### `POST /api/polls` — create *(session required)*

```json
{
  "question": "Where should the offsite be?",
  "options": ["Lisbon", "Tallinn", "Neither"],
  "expiresAt": "2026-09-26T17:00:00Z"
}
```

`expiresAt` is optional: a closing time in the future, at most 30 days out.

```json
{
  "success": true,
  "data": {
    "poll": {
      "id": "6aae6ca1d028e32e5cc4b33a",
      "question": "Where should the offsite be?",
      "options": [
        { "id": "f41bec4b2554", "text": "Lisbon" },
        { "id": "c3935caf6eb0", "text": "Tallinn" },
        { "id": "4a3283db6d7a", "text": "Neither" }
      ],
      "status": "active",
      "createdAt": "2026-09-19T11:06:09.727044651Z",
      "updatedAt": "2026-09-19T11:06:09.727044651Z",
      "acceptsVotes": true
    }
  }
}
```

The owner is taken from the session; the request type has no owner field, so
sending `ownerId`, `id` or `status` changes nothing. Option ids are generated
server-side. Rate limited: 30 per hour.

Validation, all enforced server-side before anything is written:

| Field | Rule |
| --- | --- |
| `question` | 3–300 characters |
| `options` | 2–10 of them, each 1–120 characters, no duplicates |
| `expiresAt` | in the future, no more than 30 days out |
| `name` | 2–64 characters |
| `email` | valid, at most 254 characters |
| `password` | 8–72 characters |

Zero-width and control characters are stripped before these rules are applied, so
a "non-empty" question cannot be made of invisible characters. Rejections carry
`fields`:

```json
{"success":false,"error":{"code":"VALIDATION_ERROR","message":"Please correct the highlighted fields.","fields":{"options":"Add at least 2 options."}}}
```

### `GET /api/polls/:id` — read *(public)*

Anyone with the share link. Returns the poll without vote counts — the ballot
does not need them, and a voter should not be anchored by the standing before
choosing.

### `GET /api/me/polls` — list *(session required)*

The caller's own polls, newest first, each with `totalVotes`. Bounded, so no
request can ask for an unbounded result set.

### `GET /api/polls/:id/manage` — owner view *(session required, owner only)*

404 if it does not exist, 403 if it belongs to someone else. The repository also
scopes its filter by `ownerId`, so another user's poll does not match even if a
future caller forgets the check.

### `PATCH /api/polls/:id` *(owner only)*

Accepts `question`, `options` and `expiresAt`, each optional — only what is sent
is changed. A request that changes nothing is rejected rather than silently
succeeding. `expiresAt: null` explicitly removes an expiry, which an absent field
cannot express; that distinction is why the field is decoded as a nullable type
rather than a pointer.

### `DELETE /api/polls/:id` *(owner only)*

Deletes the poll and its votes.

### `POST /api/polls/:id/close` *(owner only)*

```json
{ "success": true, "data": { "poll": { "…": "…", "status": "closed", "acceptsVotes": false } } }
```

Closing is announced over the realtime channel, so every open results page
switches to "Voting has closed" without a refresh.

A poll also stops accepting votes once its `expiresAt` passes, without its stored
status changing. Expiry is evaluated on read rather than by a background job, so
there is no window in which a sweeper has not yet run and an expired poll still
takes votes.

---

## Voting

### `POST /api/polls/:id/vote` *(public)*

```json
{ "optionId": "f41bec4b2554" }
```

Returns the new standing directly, so the voter's screen moves to results without
a second request:

```json
{
  "success": true,
  "data": {
    "pollId": "6aae6ca1d028e32e5cc4b33a",
    "results": [
      { "optionId": "f41bec4b2554", "text": "Lisbon", "count": 1 },
      { "optionId": "c3935caf6eb0", "text": "Tallinn", "count": 0 },
      { "optionId": "4a3283db6d7a", "text": "Neither", "count": 0 }
    ],
    "totalVotes": 1,
    "status": "active",
    "computedAt": "2026-09-19T11:06:09.854731685Z",
    "yourVote": "f41bec4b2554"
  }
}
```

Voting again with the same `lp_voter` cookie:

```json
{"success":false,"error":{"code":"ALREADY_VOTED","message":"You have already voted in this poll."}}
```

That refusal comes from the unique `(pollId, voterId)` index rejecting the
insert, not from a check performed beforehand. The difference only shows under
concurrency — which is exactly when it matters.

Voting in a closed poll, with an otherwise valid option:

```json
{"success":false,"error":{"code":"POLL_CLOSED","message":"This poll is closed, so votes are no longer being accepted."}}
```

Note the ordering: an option that belongs to no poll is rejected as a validation
error *before* the closed check is reached, so a bad option id in a closed poll
answers `VALIDATION_ERROR`, not `POLL_CLOSED`.

Rate limited: 60 per minute per IP — loose on purpose, since a lecture hall
voting at once shares very few source addresses.

### `GET /api/polls/:id/results` *(public)*

The same payload without `yourVote` unless the caller's cookie has voted. Served
from the Redis hash; if that key is cold it is rebuilt from MongoDB first, so a
flushed Redis costs one slower request rather than a wrong answer.

---

## Realtime

### `GET /ws/polls/:id`

WebSocket. Sends the current results on connect, then a message on every change.
Fully described in [realtime.md](./realtime.md).

```json
{
  "type": "poll_results_updated",
  "pollId": "6aae698c419e5871dcb00076",
  "results": [ { "optionId": "8c9ef54a8ee1", "count": 1 }, { "optionId": "8d1253821ab9", "count": 1 } ],
  "totalVotes": 2,
  "status": "active",
  "timestamp": "2026-09-19T10:53:04.625428802Z"
}
```

The socket is read-only from the client's perspective; it sits outside `/api`
because it is not a REST resource, and because the JSON and body-size middleware
make no sense for a hijacked connection. The `Origin` header is checked against
the same allow-list as CORS, since **WebSocket upgrades are not covered by CORS
or the same-origin policy**.
