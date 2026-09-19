# Pulse — live polling

Create a poll, share a link, and watch the results move as people vote. No
account needed to vote; no refresh needed to see the numbers change.

Built with React, Go (Gin), MongoDB and Redis, where each of those four does a
job that would be noticeably harder without it.

---

## What it does

1. Sign up, write a question and two or more options.
2. Get a share link. Anyone who opens it can vote — no account, no sign-in.
3. Every screen showing that poll updates the moment a vote lands: the owner's
   dashboard, the results page on the projector, the phone of the person who just
   voted.
4. Close the poll when you're done. Voting stops; the results stay readable.

---

## Why these four technologies

The brief named the stack. What follows is what each one is actually doing here,
because a stack where two of the pieces are decorative is worse than a smaller
one.

**MongoDB** is the durable source of truth. Every vote is a document, and a vote
is acknowledged only once that write succeeds. Polls are a natural document
shape: a question with its options embedded, read whole, almost never joined.

The interesting part is that duplicate voting is prevented by a **unique index on
`(pollId, voterId)`**, not by checking before writing. Reading first and writing
second is two operations with a gap between them, and a double-tapped button on a
phone lands squarely in that gap. The index has no gap. Proven by test: 40
simultaneous inserts for one voter leave exactly one vote.

**Redis** does three separate jobs, and the live counters are the one worth
explaining. Results come from a Redis hash, not from counting votes in MongoDB —
an aggregation over every vote document is the wrong thing to run on the hot path
of a room full of people voting at once. Redis also carries the Pub/Sub channel
that fans an update out to every backend process, and the fixed-window rate
limiter shared across them.

A derived cache raises an obvious question: what happens when it is empty? The
increment is a Lua script that **refuses to increment a key that does not exist**
and reports that instead. A plain `HINCRBY` on a cold key would cheerfully create
a hash containing `{option: 1}` — a tally of one, published to every watcher as if
it were the truth. Instead the tally is rebuilt from MongoDB and written back in a
transaction. See [`docs/realtime.md`](docs/realtime.md).

**Go with Gin** serves the API and holds the WebSocket connections. Goroutines and
channels are the reason the realtime layer is as simple as it is: the hub is a
single goroutine owning all the connection state, so there are no mutexes to get
wrong. One long-lived Redis subscription per process feeds it, rather than one per
browser.

**React** keeps the vote and the results in one view. Submitting replaces the
ballot with the standing in place — no navigation, no reload — because the moment
after voting is exactly when someone wants to see where things stand.

---

## Running it locally

You need Go 1.26+, Node 20+, and Docker for MongoDB and Redis.

(The Go version is not arbitrary: `golang.org/x/crypto`, which provides the
bcrypt implementation, requires it. With the default `GOTOOLCHAIN=auto` the Go
command fetches the right toolchain by itself.)

```bash
git clone https://github.com/skrisharam-web/live-polling.git
cd live-polling

docker compose up -d                 # MongoDB on 27017, Redis on 6379

cp .env.example backend/.env         # then set JWT_SECRET
cd backend && go run ./cmd/server    # http://localhost:8080

cd ../frontend
cp .env.example .env                 # points the app at the API above
npm install && npm run dev           # http://localhost:5173
```

`JWT_SECRET` is the only value you must supply; every other default points at the
Docker services above. Generate one with `openssl rand -base64 48`.

Check it came up:

```bash
curl -s localhost:8080/health
# {"success":true,"data":{"status":"ok","services":{"mongodb":{"status":"ok"},"redis":{"status":"ok"}}}}
```

The health check reports each dependency separately, because "the API is up but
Redis is unreachable" and "everything is fine" should not look the same.

---

## Testing

```bash
cd backend
TEST_MONGODB_URI=mongodb://localhost:27017 \
TEST_REDIS_URL=redis://localhost:6379/1 \
  go test ./...           # unit + integration
go test -race ./...       # the concurrency claims

cd ../frontend
npm run lint && npm run build
node e2e/realtime.mjs     # four browser sessions, one poll — see e2e/README.md
```

**Set those two environment variables.** Without them the integration suite skips
itself and `go test ./...` still exits 0, which is the most misleading kind of
green there is. CI sets them and then asserts the suite actually ran.

The tests worth reading are the ones that pin behaviour that is easy to break
without noticing: concurrent duplicate votes, the Lua script refusing a cold key,
WebSocket fan-out and slow-client eviction, and the browser check that three
separate sessions all see a vote arrive without reloading.

---

## Layout

```
backend/
  cmd/server/          composition root — everything is constructed here
  internal/
    handlers/          HTTP only: bind, call a service, write a response
    services/          business rules; no Gin types anywhere in here
    repositories/      persistence; the only place that speaks MongoDB
    redis/             counters, publisher, subscriber, rate limiter
    websocket/         hub, client, manager
    middleware/        auth, CORS, rate limit, body limit, security headers
    models/ validation/ config/ apperr/ response/ events/
  tests/integration/   real MongoDB and Redis, real HTTP, real sockets
frontend/
  src/
    api/               transport; the only place that knows about fetch
    hooks/             data access, including the WebSocket hook
    components/        presentation
    pages/             composition
    index.css          design tokens — no Tailwind, no component library
  e2e/                 browser checks, including the realtime proof
docs/                  architecture, API, realtime, database, security, deployment
```

---

## Documentation

| Document | What it covers |
| --- | --- |
| [architecture.md](docs/architecture.md) | Layering, the path of a request, and the decisions behind both |
| [api.md](docs/api.md) | Every endpoint, with real captured responses |
| [realtime.md](docs/realtime.md) | The vote-to-screen chain, and what happens when each link breaks |
| [database.md](docs/database.md) | Collections, indexes, and why each index exists |
| [security.md](docs/security.md) | What is defended, how, and what is not |
| [deployment.md](docs/deployment.md) | Configuration, hosting requirements, going to production |
| [requirements-traceability.md](docs/requirements-traceability.md) | Each requirement mapped to code, tests and evidence |
| [final-compliance-report.md](docs/final-compliance-report.md) | The final sign-off: status of all 20 requirements, test results, what the process caught |

---

## Known limitations

Stated here rather than buried, because they are consequences of the design
rather than things left undone.

- **Duplicate voting is best-effort.** A voter is a signed cookie, so a private
  window is a new voter. No cookie-based scheme can prevent that; genuine
  one-vote-per-person needs accounts, which the brief excludes for the audience.
- **Rate limiting is per IP**, so a lecture hall behind one NAT shares an
  allowance.
- **No password reset and no e-mail verification.** Both are real features in
  their own right and neither is in scope.
- **Results are eventually consistent by a few milliseconds.** MongoDB is written
  first and acknowledged; Redis and the broadcast follow.

[`docs/security.md`](docs/security.md) has the complete list with reasoning.
