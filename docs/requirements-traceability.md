# Requirements Traceability Matrix

This document maps every requirement from the assignment brief to the architecture
decision that satisfies it, the implementation, the files involved, the tests that
prove it, and how it is verified.

**Status legend**

| Status | Meaning |
| --- | --- |
| `PLANNED` | Designed, not yet implemented |
| `IN PROGRESS` | Partially implemented |
| `DONE` | Implemented, tested and verified |
| `BLOCKED` | Everything within the codebase is done; completion needs something outside it |

The final sign-off is in
[`final-compliance-report.md`](./final-compliance-report.md).

---

## R1 — Poll creation

**Requirement.** An authenticated user can create a poll with a question and options.

**Architecture.** `POST /api/polls` → Gin route behind `RequireAuth` middleware →
`PollService.Create` validates and builds the domain object → `PollRepository.Create`
persists to MongoDB. Option IDs are generated server-side so a client can never choose
or forge them.

**Implementation.**
- Route + handler binding and HTTP mapping
- Service-level validation and option ID generation
- Mongo insert

**Files.** `backend/internal/handlers/poll_handler.go`,
`backend/internal/services/poll_service.go`,
`backend/internal/repositories/poll_repository.go`,
`backend/internal/models/poll.go`, `backend/internal/validation/validation.go`

**Tests.** `poll_service_test.go` (validation, option ID generation),
`tests/integration/poll_test.go` (create → fetch round trip)

**Verification.** Create a poll in the UI; the share link resolves to the poll and the
options rendered match what was submitted. Backend verified live with curl and by
`TestPollLifecycle`.

**Status.** `DONE` — backend as described, and the create-poll screen drives it: question
first, option list with add/remove, and the share link presented on success. Verified in a
browser end to end.

---

## R2 — Share link

**Requirement.** The poll is shared by link and the audience votes through it.

**Architecture.** The poll ID is the share token: `/{frontend}/polls/:id` is a public
route that requires no session. Poll fetch (`GET /api/polls/:id`) is public and returns
only public fields (no owner e-mail, no per-voter data).

**Files.** `frontend/src/pages/PollPage.tsx`, `frontend/src/router/index.tsx`,
`backend/internal/handlers/poll_handler.go`

**Tests.** `tests/integration/poll_test.go` — unauthenticated `GET /api/polls/:id` is 200
and the payload contains no owner-private fields.

**Verification.** Copy the share link, open it in a browser with no session, vote.
`GET /api/polls/:id`, `POST /api/polls/:id/vote` and `GET /api/polls/:id/results` all verified
unauthenticated, and the poll payload asserted free of `ownerId` and the owner's e-mail.

**Status.** `DONE` — the share link resolves to a public voting page that needs no account.
Verified in a browser: two separate browser contexts opened the link, voted, and saw results
in the same view.

---

## R3 — Audience voting without an account

**Requirement.** Anyone with the link can vote; only creation/management needs auth.

**Architecture.** On first contact the backend issues a signed, HTTP-only `voter_id`
cookie holding a random 128-bit identity. The vote endpoint is public but always
identifies the voter server-side from that cookie — never from the request body.

**Files.** `backend/internal/middleware/voter.go`,
`backend/internal/handlers/vote_handler.go`,
`backend/internal/services/vote_service.go`

**Tests.** `vote_service_test.go`, `tests/integration/vote_test.go`

**Verification.** Vote from a private window with no account — verified live: a client with
only a `lp_voter` cookie and no session voted successfully, and no session cookie was created.

**Status.** `DONE` — signed, HTTP-only `lp_voter` cookie issued on first contact; the vote
endpoint reads the voter only from that cookie, and the vote request type has no voter field.
Forged, unsigned and tampered cookies are all rejected and replaced
(`TestForgedVoterCookieIsIgnored`). The honest limit — clearing cookies or using another
browser yields a new identity — is documented in `docs/security.md`.

---

## R4 — Live results with no refresh

**Requirement.** Everyone watching sees results update live as votes come in.

**Architecture.**

```
vote → MongoDB (durable) → Redis HINCRBY (counter)
     → Redis PUBLISH poll:{id}:updates
     → single backend subscriber
     → WebSocket hub → poll room → connected clients
     → React state update
```

**Implementation.** Redis publisher, one long-lived pattern subscriber per backend
process, a hub with per-poll rooms, and a React hook holding the socket.

**Files.** `backend/internal/redis/publisher.go`, `backend/internal/redis/subscriber.go`,
`backend/internal/services/realtime_service.go`, `backend/internal/websocket/{hub,client,manager}.go`,
`backend/internal/handlers/websocket_handler.go`, `frontend/src/hooks/usePollWebSocket.ts`

**Tests.** `internal/websocket/hub_test.go` (fan-out, room isolation, unregister, double
unregister, empty-room reclamation, slow-client eviction, shutdown, and a concurrency test
run under `-race`), and `tests/integration/websocket_test.go` over real sockets: snapshot on
connect, a vote reaching four watchers, room isolation, close delivery, 404 before upgrade,
the origin allow-list, and disconnect cleanup.

**Verification.** Done in Phase 13 with four independent browser sessions, scripted as
`frontend/e2e/realtime.mjs` so it can be re-run rather than remembered. A vote cast in B
appeared on A (the owner, watching results) and on C (a bystander who never voted), with
zero navigations recorded on any page. Every layer was checked for that same poll rather
than inferred from the screen:

| Layer | Evidence |
| --- | --- |
| MongoDB | 2 vote documents, 2 distinct `voterId`s, correct option ids |
| Redis counters | `HGETALL poll:{id}:results` → `1` and `1`, TTL refreshed to ~7 days |
| Redis Pub/Sub | `PSUBSCRIBE poll:*:updates` captured both events, each carrying the full tally (`totalVotes` 1, then 2) |
| WebSocket | frames recorded in-page by a wrapper installed before any application code, so a refetch cannot account for the update |
| React | totals moved 0 → 1 → 2 with `framenavigated` count unchanged |

The reconnect gap is covered too: with C's network cut, a vote it could not receive was
cast, and on reconnecting C refetched and recovered the missed vote without reloading.

**Status.** `DONE`

---

## R5 — Redis does real work

**Requirement.** Redis must drive live updates/counts, not sit unused.

**Architecture.** Redis owns three concrete jobs:
1. **Live counters** — `poll:{id}:results` hash, `optionID → count`, incremented on
   every accepted vote and read by `GET /api/polls/:id/results` (hot path never touches
   Mongo when the hash is warm).
2. **Pub/Sub** — `poll:{id}:updates` carries the result-updated event that drives every
   connected browser.
3. **Rate limiting** — fixed-window `INCR`/`EXPIRE` counters gate auth and vote endpoints.

**Files.** `backend/internal/redis/{client,counters,publisher,subscriber}.go`,
`backend/internal/middleware/rate_limit.go`,
`backend/internal/services/result_service.go`

**Tests.** `redis/counters_test.go`, `redis/publisher_test.go`, `middleware/rate_limit_test.go`

**Verification.** `redis-cli HGETALL poll:{id}:results` matches MongoDB's aggregation —
verified live (3/1 in both after four votes, TTL 604800). `redis-cli PSUBSCRIBE
'poll:*:updates'` showed one `poll_results_updated` message per accepted vote with a
climbing total, and a further message with `status: closed` when the owner closed the poll.

**Status.** `DONE` — all three Redis jobs are real: the counters serve the read hot path,
Pub/Sub carries every result change between instances, and the rate limiter is a shared
fixed-window counter.

Two details worth naming, because they are what make the counters trustworthy:
an increment against a **cold** key is refused by a Lua script and turned into a rebuild,
so a Redis restart cannot leave a hash holding one vote that looks authoritative; and a
rebuild replaces the hash inside a transaction, so no reader sees a half-built tally.

---

## R6 — MongoDB is the durable source of truth

**Requirement.** MongoDB must be doing meaningful work and must be able to reconstruct
results.

**Architecture.** Users, polls and votes are Mongo documents. A vote is only acknowledged
after the Mongo insert succeeds. Redis is a derived cache: `ResultService.Rebuild`
re-aggregates counts from the `votes` collection and replaces the Redis hash atomically,
so Redis can be flushed or lost with no data loss.

**Files.** `backend/internal/repositories/vote_repository.go`,
`backend/internal/services/result_service.go`, `backend/internal/database/mongodb.go`

**Tests.** `tests/integration/result_rebuild_test.go` — flush Redis, read results, counts
are restored from Mongo.

**Verification.** Flush Redis, reload the results — counts are intact and Redis is
repopulated. Verified live: after `FLUSHDB`, `GET /api/polls/:id/results` still reported
4 votes (3/1) and the hash was rebuilt as a side effect.

**Status.** `DONE` — a vote is acknowledged only after the MongoDB write; Redis is updated
afterwards and every failure there is logged and survivable. `ResultService.Rebuild`
re-derives the counters from the votes collection, so Redis can be flushed, expired or lost
with no data loss.

---

## R7 — Authentication before creating/managing polls

**Requirement.** Poll creation must not be open to anyone with the URL.

**Architecture.** E-mail + password, bcrypt hashing, JWT (HS256) delivered in an
HTTP-only, `SameSite`-scoped cookie. `RequireAuth` middleware resolves the user from the
cookie and puts the user ID into the request context; handlers never read a user ID from
the body or query.

**Files.** `backend/internal/services/auth_service.go`,
`backend/internal/handlers/auth_handler.go`, `backend/internal/middleware/auth.go`,
`frontend/src/context/AuthContext.tsx`

**Tests.** `auth_service_test.go` (hashing, token issue/verify, wrong password),
`tests/integration/auth_test.go` (register → login → me → logout, and 401 paths)

**Verification.** `POST /api/polls` without a cookie returns 401 (verified live with curl,
and asserted by `TestProtectedRouteRejectsTamperedSession`).

**Status.** `DONE` — register/login/logout/me implemented; bcrypt cost 12 (asserted by
`TestProductionUsesAStrongBcryptCost`); HS256 tokens pinned to one algorithm and issuer,
expiry required; HTTP-only cookie, `SameSite=None; Secure` in production and `Lax` locally;
login failures are indistinguishable; state-changing requests must declare JSON, which is
the CSRF defence that replaces SameSite in production.

---

## R8 — Authorization on poll management

**Requirement.** Only the owner may manage a poll.

**Architecture.** Every management operation loads the poll and compares `poll.OwnerID`
with the authenticated user ID, returning 404 for a missing poll and 403 for a poll owned
by someone else.

**Files.** `backend/internal/services/poll_service.go`

**Tests.** `poll_service_test.go` (`TestManagementRequiresOwnership`) and
`tests/integration/poll_test.go` (`TestPollManagementIsOwnerOnly`,
`TestOwnershipCannotBeClaimedInThePayload`) — a second authenticated user cannot manage,
update, close or delete another user's poll, and cannot claim ownership through the payload.

**Verification.** Attempt a cross-account manage/update/close/delete with curl; all four
return 403 FORBIDDEN and the poll is verified unchanged afterwards.

**Status.** `DONE` — ownership is checked in `PollService.GetOwned`, which every management
operation routes through, and the repository additionally scopes its filter by `ownerId`.

---

## R9 — Backend validation

**Requirement.** Never trust client input; validate server-side before touching the DB.

**Architecture.** A `validation` package holds the rules (lengths, counts, formats,
enum membership). Services validate before any repository call. Gin binding errors and
validation errors both map to a single `VALIDATION_ERROR` response shape with field
details. A body-size limit rejects oversized payloads before decoding.

**Files.** `backend/internal/validation/validation.go`,
`backend/internal/services/*.go`, `backend/internal/middleware/body_limit.go`

**Tests.** `validation/validation_test.go` (table-driven), plus negative cases in every
service test.

**Verification.** Run against the live API in Phase 13. Every case is rejected with
`422 VALIDATION_ERROR` and a field-level detail rather than a bare refusal: empty question,
1 option, 30 options, a 10 000-character question, duplicate options, no options at all,
and an option ID that belongs to no poll. Sample body:

```json
{"success":false,"error":{"code":"VALIDATION_ERROR","message":"Please correct the highlighted fields.","fields":{"options":"Add at least 2 options."}}}
```

**Status.** `DONE`

---

## R10 — Duplicate vote prevention

**Requirement.** Obvious duplicate voting must be prevented, server-side.

**Architecture.** Unique compound index on `votes(pollId, voterId)`. The service inserts
the vote and translates a Mongo duplicate-key error into a 409 `ALREADY_VOTED`. The
uniqueness guarantee lives in the database, so it holds under concurrency.

**Files.** `backend/internal/database/indexes.go`,
`backend/internal/repositories/vote_repository.go`,
`backend/internal/services/vote_service.go`

**Tests.** `tests/integration/vote_test.go` (second vote → 409, tally unchanged),
`tests/integration/concurrency_test.go`: 40 concurrent inserts for one voter → exactly 1
succeeds and 39 report ALREADY_VOTED; 20 concurrent votes through the real HTTP stack with
one cookie → 1 created, 19 conflicts; and 60 simultaneous distinct voters → all 60 counted,
proving the rule does not drop legitimate concurrent traffic.

**Verification.** Vote twice from the same browser; second attempt is refused (409
ALREADY_VOTED) and the counter does not move — verified live with curl and in MongoDB
(`db.votes.countDocuments()` = 2 for two distinct voters, not 3).

**Status.** `DONE` — enforced by the unique `(pollId, voterId)` index, not by a
read-then-write check, so it holds under concurrency and across backend instances.

---

## R11 — Separation of concerns

**Requirement.** Frontend and backend in their own folders; no mixed concerns in a file.

**Architecture.** Monorepo with `/frontend` and `/backend`. Inside the backend:
`handler → service → repository → database`. Handlers do HTTP only; services hold
business rules; repositories hold persistence. In the frontend: `api/` for transport,
`hooks/` for data access, `components/` for presentation, `pages/` for composition.

**Files.** Whole tree.

**Tests.** Enforced by review; services are unit-tested without any Gin dependency, which
is only possible if the layering holds.

**Verification.** Checked mechanically in the Phase 17 review rather than asserted:

| Check | Result |
| --- | --- |
| Gin imported in `services`, `repositories`, `models` or `validation` | none — the layering claim is load-bearing, not decorative |
| Handlers touching MongoDB or Redis directly | none, after the fix below |
| Business rules in repositories | none; they return `NOT_FOUND` and wrapped internal errors only |
| Frontend components or pages calling `fetch` | none; transport stays in `api/` |

The review found one real leak: `VoteHandler.Results` parsed a hex string into a
`bson.ObjectID` so it could call `VoteOf`. Which string shapes are valid identifiers is a
persistence detail, and a handler that knows it has to import the database driver to ask.
The parsing moved into the service and `vote_handler.go` no longer imports the driver at
all. Verified afterwards against the production build: `yourVote` is still returned for a
voter who has voted, still absent for one who has not, and a malformed poll id still
answers 404.

One deliberate compromise remains: `models.User.ID` and `models.Poll.ID` are typed
`bson.ObjectID`, so that type appears in service signatures. Replacing it with a domain id
type would touch every layer for a naming benefit, and the driver type is not doing
anything a domain type would do differently. Recorded here rather than quietly fixed or
quietly ignored.

**Status.** `DONE`

---

## R12 — Security

**Requirement.** No obvious shortcuts; clean, security-conscious code.

**Architecture.** bcrypt (cost 12), HTTP-only + `Secure` + `SameSite` cookies, no secrets
in the repo, strict CORS allow-list with credentials, Redis-backed rate limiting on auth
and vote endpoints, panic recovery, request IDs, structured logs that never contain
credentials, and error responses that never leak internals.

**Files.** `backend/internal/middleware/*.go`, `backend/internal/response/response.go`,
`backend/internal/config/config.go`, `docs/security.md`

**Tests.** `tests/integration/api_test.go` (cookie flags, CORS, CSRF content type, oversized
bodies, security headers), `auth_service_test.go` (token forgery, enumeration),
`poll_service_test.go` and `poll_test.go` (ownership), plus a 24-check adversarial suite run
against the live server.

**Verification.** Phase 12 audit: 24/24 adversarial checks pass (authentication bypass,
authorization bypass, NoSQL operator injection, mass assignment, output safety, error
leakage, transport). In a real browser, stored `<img onerror>`, `<script>` and `<svg onload>`
payloads render as inert text with no script execution. Production cookies carry
`HttpOnly; Secure; SameSite=None`, and production refuses to start with a short secret,
insecure cookies or a wildcard origin. Written up in `docs/security.md`.

**Status.** `DONE`

---

## R13 — MongoDB indexes

**Requirement.** (Implied by correctness and performance.) Indexes must exist and be
justified.

**Architecture.** Created at startup, idempotently:

| Index | Purpose |
| --- | --- |
| `users.email` unique | login lookup + registration race safety |
| `votes.pollId+voterId` unique | duplicate-vote guarantee |
| `votes.pollId` | result aggregation / rebuild |
| `polls.ownerId` | dashboard listing |
| `polls.createdAt` | dashboard ordering |

**Files.** `backend/internal/database/indexes.go`, `docs/database.md`

**Tests.** `tests/integration/indexes_test.go` — indexes exist with the expected keys and
uniqueness flags.

**Verification.** `db.votes.getIndexes()`.

**Status.** `DONE` — created at startup, documented in `docs/database.md`, asserted by
`tests/integration/indexes_test.go` (keys and uniqueness flags).

---

## R14 — Redis failure recovery

**Requirement.** (Implied by "real use of the stack" and data consistency.) A Redis
outage must not corrupt or lose votes.

**Architecture.** Mongo write first, then Redis. A Redis error after a successful Mongo
write is logged and the vote is still acknowledged. Reads fall back to a Mongo aggregation
and repopulate Redis (`ResultService.Rebuild`). WebSocket clients refetch results on
reconnect, so a missed event self-heals.

**Files.** `backend/internal/services/{vote_service,result_service}.go`

**Tests.** `internal/services/result_service_test.go` (warm read, cold rebuild, Redis read
failure, drifted counters, cold increment, increment failure) and
`tests/integration/reconciliation_test.go` (real flush mid-flight, vote into cold counters,
counters cleared on poll delete). `TestRateLimitFailsOpenWhenRedisIsUnavailable` closes the
Redis client outright and asserts voting still works.

**Verification.** Flush Redis, vote, reload — counts are correct and the hash is rebuilt.
Verified live, including a vote arriving while the counters were cold: the voter saw the
full tally (5) and Redis ended up holding 3 and 2, not a partial 1.

**Status.** `DONE`

---

## R15 — WebSocket resilience

**Requirement.** (Implied by "truly real-time" under real use.)

**Architecture.** Server: read/write deadlines, ping/pong keepalive, bounded per-client
send buffer with slow-client eviction, room cleanup on the last disconnect. Client:
exponential backoff (1→2→4→8→16 s, capped, jittered), explicit connection state, and a
results refetch after every successful reconnect.

**Files.** `backend/internal/websocket/*.go`, `frontend/src/hooks/usePollWebSocket.ts`

**Tests.** `websocket/hub_test.go`, `go test -race ./...`

**Verification.** Both halves were exercised in Phase 13.

*Server death*, done by hand with a results page open: the badge read `Live`, the backend
was killed, and the badge moved to `Reconnecting…` while the last known results stayed on
screen rather than the view blanking. On restart it returned to `Live`, and a vote cast
afterwards arrived live without a reload.

*Network loss*, scripted in `frontend/e2e/realtime.mjs` so it runs on every CI build: a
watching session is taken offline, a vote it cannot receive is cast, and on reconnecting it
refetches and recovers the missed vote — the gap Pub/Sub cannot close by itself, because a
message missed while disconnected is gone for good.

That second check found a real defect, now fixed: an idle socket does not notice a dead
network until a write fails or the server's ping goes unanswered, so the badge kept
claiming `Live` for up to a minute after the connection had gone. The hook now also listens
for the browser's `offline` event and closes the socket itself, which is the one thing a
badge whose entire job is honesty must not get wrong.

**Status.** `DONE`

---

## R16 — Responsive, accessible UI

**Requirement.** UI/UX and polish; the voting flow especially on mobile.

**Architecture.** Mobile-first CSS with design tokens, semantic HTML, labelled controls,
visible focus rings, ARIA live regions for result updates, and AA contrast.

**Files.** `frontend/src/index.css`, `frontend/src/components/**`, `frontend/src/pages/**`

**Tests.** `frontend/e2e/accessibility.mjs` walks nine screens × light and dark × 320 / 768 /
1440 px (54 combinations) and fails on text below the WCAG AA ratio for its size, horizontal
overflow, a page without exactly one `h1`, a missing `#main` skip target, or a page that did
not set its own title. `frontend/e2e/ux-states.mjs` produces each async state deliberately
and runs a keyboard-only vote.

**Verification.** All 54 combinations pass. A vote can be cast start to finish without a
mouse. Reduced motion is honoured (bars snap instead of animating).

**Status.** `DONE`

---

## R17 — Tests

**Requirement.** (Implied by code quality.)

**Architecture.** Unit tests for services/validation/websocket with no external
dependencies; integration tests against real Mongo and Redis, skipped automatically when
those aren't configured; race detector and browser checks in CI.

**Files.** `backend/**/*_test.go`, `backend/tests/integration/**`,
`frontend/e2e/{accessibility,ux-states,realtime}.mjs`, `.github/workflows/ci.yml`

**Verification.** All run in Phase 13 against live Mongo and Redis:

| Run | Result |
| --- | --- |
| `go vet ./...` / `go build ./...` | clean |
| `go test -count=1 ./...` (integration enabled) | pass, 26.7s in the integration suite |
| `go test -race -count=1 ./...` | pass, no data race reported, 272.7s |
| `npm run lint` / `npm run build` | clean |
| `node e2e/accessibility.mjs` | 54 screen/theme/width combinations pass |
| `node e2e/ux-states.mjs` | 18 checks pass |
| `node e2e/realtime.mjs` | 19 checks pass across four browser sessions |

A bare `go test ./...` without `TEST_MONGODB_URI` and `TEST_REDIS_URL` skips the
integration suite and still reports success, so CI sets both and then asserts the
suite actually ran rather than trusting a green tick.

**Status.** `DONE`

---

## R18 — Deployment

**Requirement.** A live, publicly reachable working link.

**Architecture.** Two deployable units — a scratch-based Go image and a static
bundle served by nginx — against managed MongoDB and Redis. The backend host must
support long-lived WebSocket connections, which is the constraint that rules out
serverless runtimes billed per request.

**Files.** `backend/Dockerfile`, `frontend/Dockerfile`, `frontend/nginx.conf`,
`frontend/security-headers.conf`, `docker-compose.prod.yml`, `render.yaml`,
`docs/deployment.md`

**Verification.** Everything short of provisioning has been done and checked
against a production build running in Docker, behind TLS, with the frontend and
API on separate origins — the same arrangement as a real deployment:

| Checked | Result |
| --- | --- |
| Backend image | builds; 43.5 MB; scratch, non-root (65532), no shell |
| Frontend image | builds; nginx serving the built bundle |
| Deep link `/polls/{id}` | 200 via the SPA fallback — the share link is the product |
| Session cookie | `HttpOnly; Secure; SameSite=None; Path=/; Max-Age=86399` |
| HSTS | `max-age=31536000; includeSubDomains` |
| CORS | exact origin echoed; an unlisted origin gets no allow-origin header |
| Gin mode | release (no debug output in the logs) |
| Startup refusals | short `JWT_SECRET`, missing `JWT_SECRET`, `COOKIE_SECURE=false`, `FRONTEND_URL=*` — each exits 1 |
| Realtime over WSS | all 19 realtime checks pass cross-origin against the production build |
| Accessibility / UX suites | pass against the production build |
| Redis wiped with `FLUSHALL` | results unchanged (3 votes, `[2, 1]`); counters rebuilt from MongoDB with a fresh 7-day TTL |

**Remaining.** Creating the hosting accounts — MongoDB Atlas, a Redis host and a
WebSocket-capable backend host — and running the five steps in
[`deployment.md`](./deployment.md). That needs credentials belonging to the
developer; it is not further code.

**Status.** `BLOCKED`

---

## R19 — Documentation

**Requirement.** README with how to run it and key decisions.

**Files.** `README.md`, `docs/architecture.md`, `docs/api.md`, `docs/realtime.md`,
`docs/database.md`, `docs/security.md`, `docs/deployment.md`, this file.

**Verification.** All seven exist and every internal link resolves. The API
examples are responses captured from a running server rather than written by hand,
and each document was checked against the code while being written — which caught
four inaccuracies that would otherwise have shipped: the option maximum is 10 and
not 20, `expiresAt` existed but was undocumented, the hub's channel types were
named wrongly in an architecture snippet, and the stated Go version was too low
(`golang.org/x/crypto` requires 1.26, which is why the directive is what it is).

**Status.** `DONE`

---

## R20 — Submission artifacts

**Requirement.** Public repo, live link, 3–5 minute video.

**Owner.** The developer, for two of the three.

| Artifact | State |
| --- | --- |
| Public GitHub repository | The code, tests, documentation and deployment config are pushed to `skrisharam-web/live-polling`. It needs to be made **public** before submission, and a pull request opened and merged |
| Live, publicly reachable link | Blocked on hosting accounts — see R18. Everything short of provisioning is verified |
| 3–5 minute video | **Not started, and mandatory.** It has to be recorded by the developer; nothing in this repository can produce it |

The brief is explicit that the video is not optional, and equally explicit about why: the
reviewers know candidates use AI, and the interview rounds test whether the developer
understands what was built. The parts of this system worth being able to explain without
notes are the unique `(pollId, voterId)` index standing in for a read-then-write check,
the Lua script that refuses to increment a cold key, one Redis subscription per process
rather than per browser, and full tallies rather than deltas on the wire. Each of those is
a decision with an alternative that looks reasonable and is wrong.

**Status.** `BLOCKED`
