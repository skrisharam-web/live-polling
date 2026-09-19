# Realtime

The chain from a vote to somebody else's screen, and what happens when each link
in it breaks.

---

## The chain

```
POST /api/polls/:id/vote
  └─▶ MongoDB insert          ← the vote is now durable; everything after is derived
      └─▶ Redis HINCRBY       ← via a script that refuses a cold key
          └─▶ Redis PUBLISH poll:{id}:updates
              └─▶ every backend process (one PSUBSCRIBE each)
                  └─▶ WebSocket hub ─▶ the room for that poll
                      └─▶ React updates the query cache
```

Ordering is the point. MongoDB is written and acknowledged first; everything
after it is derived state that can be rebuilt. If the process died between steps
one and two, no vote would be lost — the counters would simply be rebuilt on the
next read.

---

## Redis counters, and the cold-key problem

Results are served from a Redis hash (`poll:{id}:results`), not by aggregating
vote documents. Counting every vote in MongoDB on each read is the wrong thing to
run on the hot path of a room voting at once.

Which raises the question every cache has to answer: what happens when the key
isn't there? Redis has been restarted, the TTL lapsed, someone ran `FLUSHALL`.

The obvious answer is wrong. `HINCRBY` on a missing key **creates** it and returns
`1`. The poll had four hundred votes; Redis now says one, and that number is about
to be published to every watcher as though it were the truth. A cache miss would
have silently become a wrong answer that looks exactly like a right one.

So the increment is a script that refuses:

```lua
if redis.call('EXISTS', KEYS[1]) == 1 then
    local count = redis.call('HINCRBY', KEYS[1], ARGV[1], 1)
    redis.call('EXPIRE', KEYS[1], ARGV[2])
    return count
end
return -1
```

`-1` means "cold — do not trust me". The caller then rebuilds the whole tally from
MongoDB and writes it back. Three details matter:

- **The rebuild is a transaction.** `DEL` plus a full `HSET` inside `TxPipelined`,
  so no concurrent reader can observe a half-built hash. A partial tally is worse
  than no tally, because it is indistinguishable from a real one.
- **Zeroes are written explicitly.** An option with no votes still gets `0`. Store
  only non-zero counts and a poll where nobody has voted yet has no key at all —
  permanently cold, rebuilt on every single request.
- **The TTL is refreshed on every increment** (7 days). An active poll stays warm;
  an abandoned one eventually stops occupying memory and rebuilds if it comes
  back to life.

Redis being unavailable entirely is not fatal either: reads fall back to MongoDB.
Slower, still correct.

---

## Pub/Sub

One channel per poll, `poll:{id}:updates`. Each message carries the **complete
tally**, not a delta.

That choice is what makes every other part of this simple. A delta requires the
receiver to have seen every previous message, so one dropped frame leaves a
client permanently and silently wrong — and Redis Pub/Sub is explicitly
at-most-once. With a full tally, a dropped message costs one update's worth of
staleness and the next message repairs it completely.

Each backend process holds **one** `PSUBSCRIBE poll:*:updates`, not one
subscription per connected browser. A thousand people watching one poll are a
thousand WebSocket connections into a process and exactly one Redis subscription.
Subscribing per browser would scale Redis connections with the audience, which is
the load this application exists to handle.

This is also what makes horizontal scaling work: a vote handled by instance A
reaches a viewer connected to instance B, because both are subscribed to the same
pattern.

---

## The WebSocket hub

The hub is **one goroutine** owning all connection state. Register, unregister,
broadcast and inspect arrive as messages on channels; nothing else touches the
room map. There is no lock to acquire, and therefore no lock ordering to get
wrong. The only synchronisation in the package is a `sync.Once` making a client's
channel safe to close exactly once.

```go
type Hub struct {
    register   chan *Client
    unregister chan *Client
    broadcast  chan Broadcast
    inspect    chan inspection

    // rooms maps a poll ID to the clients watching it. Only Run touches it.
    rooms map[string]map[*Client]struct{}
    done  chan struct{}
}
```

Rooms are created on the first subscriber and deleted when the last one leaves, so
a server that has been running for a month does not carry a map entry for every
poll it has ever seen.

**Broadcast never blocks.** Each client has a 16-message buffer; if it is full,
the message is dropped and the client disconnected. The alternative — waiting for
a slow client — means one bad connection stalls everyone watching the same poll.
A disconnected client reconnects and refetches; a stalled room just stops working.

**Liveness.** The server pings every 54 seconds against a 60-second read
deadline, so a peer that has vanished without a close frame is detected rather
than accumulated. Writes have a 10-second deadline. The read limit is 512 bytes
on a socket clients have no reason to write to at all.

---

## The client

`usePollWebSocket` holds a small state machine: `connecting` → `connected` →
`reconnecting` → `disconnected`, and the badge on screen says which.

**Reconnection** is exponential backoff — 1s, 2s, 4s, 8s, 16s, capped, with
jitter, giving up after 8 attempts in favour of a manual retry. Jitter is not
decoration: without it, everyone watching a poll when the server restarts
reconnects in the same instant, and the thundering herd finishes what the restart
started.

Two browser signals short-circuit the backoff, because waiting out a timer when
you already know the answer is pointless: coming back online, and a tab becoming
visible again. Phones suspend sockets on background tabs, so without the second
one a viewer who switches apps returns to a frozen result.

A third signal was added in Phase 13 after the browser test caught it. An idle
socket does not notice a dead network — nothing fails until a write is attempted
or a ping goes unanswered — so the badge kept reading **"Live"** for up to a
minute after the connection was gone. The hook now listens for the browser's
`offline` event and closes the socket itself. A badge whose entire job is to be
honest about the connection should not be the last thing to find out.

**Reconnecting refetches.** This is the part that is easy to leave out and
impossible to notice by hand. Messages published while a client was disconnected
are gone — Pub/Sub does not replay. Resuming the socket without refetching leaves
the screen quietly stale until the next vote happens to arrive, which on a poll
that has finished could be never. So every successful reconnect triggers a fresh
`GET /results`.

**One writer to the cache.** Socket messages write into the same TanStack Query
cache entry the REST fetch populates, and `refetchOnWindowFocus` is deliberately
off. Two writers would mean a broken socket is invisible during development,
because polling would quietly cover for it.

---

## Failure modes

| What breaks | What happens |
| --- | --- |
| Redis key is cold | Script returns `-1`; tally is rebuilt from MongoDB in a transaction |
| Redis is unreachable | Reads fall back to MongoDB; rate limiter fails open; votes still recorded |
| A published message is dropped | Next message carries the full tally and repairs it |
| A client is slow | Buffer fills, client is disconnected, it reconnects and refetches |
| A client's network dies | `offline` closes the socket; badge shows `Reconnecting…`; refetch on return |
| The backend restarts | Clients back off with jitter and refetch on reconnect |
| A backend instance is added | It opens its own `PSUBSCRIBE`; no coordination needed |

---

## How this was verified

Not by reasoning about it. `frontend/e2e/realtime.mjs` runs four independent
browser sessions against one poll — the owner watching results, a voter, a
bystander who never votes, and a second voter — and asserts that a vote reaches
every screen.

Two things make that assertion mean something:

- every page wraps `WebSocket` before any application code runs and records what
  arrives, so a pass cannot be explained away by a background refetch;
- every navigation is counted, because "updates without a refresh" is not true if
  something quietly reloaded.

For one such run, every layer was checked rather than inferred from the screen:
MongoDB held 2 vote documents with 2 distinct voter ids; `HGETALL` returned `1`
and `1` with the TTL refreshed; a `PSUBSCRIBE` trace captured both events, each
with the full tally; the in-page recorder showed 5 frames on the owner's session
and 3 on the bystander's, neither of which voted; and the totals moved 0 → 1 → 2
with the navigation count unchanged.

The script then cuts the bystander's network, casts a vote it cannot receive, and
checks that reconnecting recovers the missed vote — the gap Pub/Sub cannot close
on its own. That check is what found the "Live" badge bug above.
