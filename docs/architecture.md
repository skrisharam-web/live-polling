# Architecture

How the pieces fit, and why they are arranged this way rather than another.

---

## The shape of it

```
Browser ──HTTP──▶ Gin ──▶ handler ──▶ service ──▶ repository ──▶ MongoDB
   ▲                                      │
   │                                      ├──▶ Redis hash      (live counters)
   │                                      └──▶ Redis PUBLISH   (fan-out)
   │                                                 │
   └──WebSocket── hub ◀── subscriber ◀───────────────┘
```

A vote travels down the left side and comes back to *every* connected browser
along the bottom. Those are two different paths on purpose: the voter's own
response is synchronous and returns the new standing directly, while everyone
else finds out through Redis. The voter therefore never waits on the fan-out to
see their own vote land.

---

## Layers

Four of them in the backend, each allowed to know only about the one below.

| Layer | Job | Never does |
| --- | --- | --- |
| `handlers` | Bind the request, call one service, write a response | Business rules, database calls |
| `services` | Business rules, orchestration, validation | HTTP; there is no Gin import in the package |
| `repositories` | Persistence | Decide what is *allowed*, only what is *stored* |
| `database` / `redis` | Connections, indexes, scripts | Know what a poll means |

The rule that keeps this honest is that **services are unit-tested without Gin**.
That is not a coincidence, it is the test: if a business rule needed a
`*gin.Context`, the layering would already be broken and the test would not
compile.

The frontend mirrors it — `api/` for transport, `hooks/` for data access,
`components/` for presentation, `pages/` for composition. A component never calls
`fetch`; a page never parses a response.

---

## Everything is constructed in one place

`cmd/server/main.go` is the only place that builds anything. Clients, repositories,
services, handlers and the router are created there and passed down as arguments.
There are no `init()` side effects and no global database or Redis handle. (The
only package-level values in the backend are two compiled Lua scripts, which are
immutable handles rather than state.)

This is what makes the integration tests possible: they build the same object
graph with test databases and get a real server, rather than a special test mode
of the real one.

It also closed two real bugs in this project, both of the same shape. A piece of
wiring existed only in `main.go` — first the rate limiter, then the poll-closed
announcement — and everything still compiled, because the thing being forgotten
was an assignment rather than an argument. The second time it happened, the
callback became a constructor parameter:

```go
// Before: a field somebody has to remember to set.
svc.OnPollClosed = announcer.Announce

// After: an argument the compiler insists on.
svc := services.NewPollService(polls, votes, results, announcer)
```

A callback compiles fine when forgotten. A constructor argument does not. The
general lesson, and the reason the composition root is worth its verbosity: if
forgetting something produces a program that runs but is silently wrong, move the
thing into a position where forgetting it will not compile.

---

## The path of a vote

Worth following once end to end, because most of the design decisions in this
project are visible along it.

1. **`POST /api/polls/:id/vote`** arrives. Middleware has already attached a
   request ID, set security headers, applied CORS, enforced a 64 KB body limit,
   required `application/json`, and checked the rate limiter.

2. **Voter identity.** The `lp_voter` cookie is read and its signature checked; if
   it is missing or forged, a new 128-bit identity is minted and set. The request
   body has no voter field — identity is not something a client gets to assert.

3. **The service** loads the poll, refuses if it is closed, and confirms the
   option belongs to *this* poll. All of this happens before anything is written.

4. **The repository** inserts the vote. The unique `(pollId, voterId)` index
   decides whether this is a duplicate; a duplicate-key error becomes
   `ALREADY_VOTED`. This is the whole duplicate-prevention mechanism — there is no
   read-then-write check to race against.

5. **Redis** is incremented by a Lua script that refuses to touch a key that does
   not exist, returning `-1` instead. On `-1` the tally is rebuilt from MongoDB and
   written back inside a transaction. See [realtime.md](./realtime.md) for why
   this matters more than it looks.

6. **The event is published** to `poll:{id}:updates` carrying the complete tally,
   not a delta.

7. **The response** returns the new standing, so the voter's screen can move to
   results without a second request.

Meanwhile every backend process is subscribed to `poll:*:updates` and pushes the
event into its WebSocket hub, which delivers it to the room for that poll.

---

## Decisions worth defending

**MongoDB is authoritative; Redis is derived.** A vote is acknowledged only after
the MongoDB write. Redis can be flushed entirely without losing a vote — the
counters rebuild on next read. Every design choice in the Redis layer follows
from the discipline that it must never be the only place something is true.

**Full tallies, not deltas, on the wire.** A delta requires the receiver to have
seen every earlier message. A client that reconnects, or whose message was
dropped, would then be permanently and silently wrong. A full tally makes every
message self-sufficient and every dropped message harmless.

**One Redis subscription per process, not per browser.** A thousand viewers of a
poll are a thousand WebSocket connections into one process — and exactly one
`PSUBSCRIBE`. Subscribing per browser would multiply Redis connections by the
audience size, which is precisely the load this application is supposed to handle.

**The hub is a single goroutine.** Register, unregister, broadcast and inspect all
arrive as messages on channels, and one goroutine owns the room map. Nothing else
reads or writes it, so there is no lock to acquire and no lock ordering to get
wrong. The package's one piece of synchronisation is a `sync.Once` guarding a
client's channel against being closed twice — which is a lifecycle concern, not
shared mutable state. Broadcasting
never blocks: a client whose buffer is full is disconnected rather than allowed to
stall everyone else watching the same poll.

**Errors are a domain vocabulary, not HTTP status codes.** Services return
`apperr` values (`ALREADY_VOTED`, `POLL_CLOSED`, `NOT_FOUND`…) and exactly one
place maps those to status codes. A service that returned `409` would be making
an HTTP decision from the wrong layer, and would be untestable without inventing
a request.

**Validation lives in the service layer**, after binding and before any repository
call, so there is no path into the database that skips it. Frontend validation
exists for the person typing and is not a control.

---

## What is deliberately not here

- **No `/api` prefix on the WebSocket route.** It is not a REST resource, and the
  JSON and body-size middleware make no sense for a hijacked connection.
- **No ORM.** The driver is already the right level of abstraction for four
  collections, and an ORM would hide the index behaviour this design depends on.
- **No CSS framework.** Design tokens in `index.css`, written by hand. See
  `.claude/skills/ui-ux/SKILL.md` for the rules the interface is held to.
- **No message broker.** Redis Pub/Sub is at-most-once and that is acceptable
  here, because a missed message costs a few seconds of staleness and the
  reconnect path refetches. A durable queue would be weight without a matching
  requirement.
