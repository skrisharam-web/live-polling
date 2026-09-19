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

The final sign-off lives in [`final-compliance-report.md`](./final-compliance-report.md).

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

**Status.** `DONE` (backend) — `POST /api/polls` behind `RequireAuth`, owner taken from the
session, option IDs minted server-side from crypto/rand, question/options/expiry validated.
The UI lands in Phase 10.

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

**Status.** `DONE` (backend) — read, vote and results are all public. The page lands in
Phase 10.

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

**Tests.** `websocket/hub_test.go` (fan-out, disconnect), `tests/integration/realtime_test.go`
(vote via HTTP → event arrives on two concurrent WebSocket clients), plus the manual
three-browser test in [`realtime.md`](./realtime.md).

**Verification.** Browser A (owner results), Browser B and C (public poll): a vote in B
updates A and C with no refresh, and vice versa.

**Status.** `PLANNED`

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

**Verification.** curl with an empty question, 1 option, 30 options, a 10 000-character
question, an option ID from a different poll — each is rejected with 422/400.

**Status.** `PLANNED`

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

**Verification.** Phase 17 review checklist.

**Status.** `PLANNED`

---

## R12 — Security

**Requirement.** No obvious shortcuts; clean, security-conscious code.

**Architecture.** bcrypt (cost 12), HTTP-only + `Secure` + `SameSite` cookies, no secrets
in the repo, strict CORS allow-list with credentials, Redis-backed rate limiting on auth
and vote endpoints, panic recovery, request IDs, structured logs that never contain
credentials, and error responses that never leak internals.

**Files.** `backend/internal/middleware/*.go`, `backend/internal/response/response.go`,
`backend/internal/config/config.go`, `docs/security.md`

**Tests.** `middleware/*_test.go`, `response/response_test.go`

**Verification.** Phase 12 audit, documented in `docs/security.md`.

**Status.** `PLANNED`

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

**Verification.** Kill the backend with the poll page open; the badge shows
`reconnecting`, then `connected`, and the results are correct afterwards.

**Status.** `PLANNED`

---

## R16 — Responsive, accessible UI

**Requirement.** UI/UX and polish; the voting flow especially on mobile.

**Architecture.** Mobile-first CSS with design tokens, semantic HTML, labelled controls,
visible focus rings, ARIA live regions for result updates, and AA contrast.

**Files.** `frontend/src/index.css`, `frontend/src/components/**`

**Tests.** Manual viewport checks at 375 / 768 / 1440 px; keyboard-only pass.

**Verification.** Phase 11 checklist.

**Status.** `PLANNED`

---

## R17 — Tests

**Requirement.** (Implied by code quality.)

**Architecture.** Unit tests for services/validation/websocket with no external
dependencies; integration tests against real Mongo and Redis, skipped automatically when
those aren't configured; race detector in CI.

**Files.** `backend/**/*_test.go`, `backend/tests/integration/**`, `.github/workflows/ci.yml`

**Verification.** `go test ./...`, `go test -race ./...`, `npm run lint`, `npm run build`.

**Status.** `PLANNED`

---

## R18 — Deployment

**Requirement.** A live, publicly reachable working link.

**Architecture.** Backend container (Go, distroless) on a host that supports WebSockets;
frontend static build on a CDN host; MongoDB Atlas; managed Redis. Production config is
environment-driven: secure cookies, strict CORS, `wss://`.

**Files.** `backend/Dockerfile`, `frontend/Dockerfile`, `infra/`, `docs/deployment.md`

**Verification.** Phase 16 production QA against the public URL.

**Status.** `PLANNED` — requires the developer's own hosting accounts; see
`docs/deployment.md`.

---

## R19 — Documentation

**Requirement.** README with how to run it and key decisions.

**Files.** `README.md`, `docs/architecture.md`, `docs/api.md`, `docs/realtime.md`,
`docs/database.md`, `docs/security.md`, `docs/deployment.md`, this file.

**Status.** `PLANNED`

---

## R20 — Submission artifacts

**Requirement.** Public repo, live link, 3–5 minute video.

**Owner.** The developer. The repo is produced here; the live link needs hosting accounts;
the video must be recorded by the developer and is **mandatory**.

**Status.** `PLANNED`
