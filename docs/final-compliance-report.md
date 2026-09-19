# Final compliance report

Written at the end of Phase 17, against the code as it stands rather than as it
was planned. Every claim here was produced by running something; where something
could not be verified, that is said rather than glossed.

---

## Verdict

**18 of 20 requirements are DONE. Two are BLOCKED on things outside this
repository**, and neither can be closed by writing more code:

- **R18 — a live public link** needs hosting accounts belonging to the developer.
- **R20 — the submission video** is mandatory and has to be recorded by the
  developer.

The project is **not complete** until those two are done. Everything that can be
finished in a repository is finished, tested and documented.

---

## Requirement status

| # | Requirement | Status |
| --- | --- | --- |
| R1 | Poll creation | `DONE` |
| R2 | Share link | `DONE` |
| R3 | Audience voting without an account | `DONE` |
| R4 | Live results with no refresh | `DONE` |
| R5 | Redis does real work | `DONE` |
| R6 | MongoDB is the durable source of truth | `DONE` |
| R7 | Authentication before creating/managing polls | `DONE` |
| R8 | Authorization on poll management | `DONE` |
| R9 | Backend validation | `DONE` |
| R10 | Duplicate vote prevention | `DONE` |
| R11 | Separation of concerns | `DONE` |
| R12 | Security | `DONE` |
| R13 | MongoDB indexes | `DONE` |
| R14 | Redis failure recovery | `DONE` |
| R15 | WebSocket resilience | `DONE` |
| R16 | Responsive, accessible UI | `DONE` |
| R17 | Tests | `DONE` |
| R18 | Deployment — live link | `BLOCKED` |
| R19 | Documentation | `DONE` |
| R20 | Submission artifacts | `BLOCKED` |

Evidence for each is in
[`requirements-traceability.md`](./requirements-traceability.md).

---

## The stack requirement

The brief named four technologies. A stack where two of them are decorative would
meet the letter of that and not the point, so:

| Technology | What it does here | What would break without it |
| --- | --- | --- |
| **MongoDB** | Durable source of truth. Every vote is a document; a vote is acknowledged only after the write | Votes would not survive a restart. The unique `(pollId, voterId)` index *is* the duplicate-vote guarantee |
| **Redis** | Live counters, Pub/Sub fan-out, and the shared rate limiter | Results would mean aggregating every vote document on the hot path; a vote on one instance would never reach a viewer on another |
| **Go (Gin)** | API and WebSocket connections; one goroutine owns the hub, one Redis subscription per process | — |
| **React** | Vote and results in one view, updating from the socket without navigation | — |

---

## Test results

Run on the final code. Both suites were run twice: against a development build,
and again against the production Docker images behind TLS with the frontend and
API on separate origins.

| Run | Result |
| --- | --- |
| `go vet ./...` | clean |
| `go test -count=1 ./...` with real MongoDB and Redis | pass — 112 Go test functions, 26.8s in the integration suite |
| `go test -race -count=1 ./...` | pass, no data race — 272.4s |
| `npm run lint` | clean |
| `npm run build` (includes `tsc -b`) | clean |
| `e2e/accessibility.mjs` | 54 screen/theme/width combinations pass |
| `e2e/ux-states.mjs` | 18/18 |
| `e2e/realtime.mjs` | 19/19, including over WSS cross-origin |
| `govulncheck` | clean in CI (cannot run in the sandbox — `vuln.go.dev` is blocked there) |

**Continuous integration is green on the current head** (`567809b`): all four
jobs — backend (gofmt, vet, tests, race), frontend (lint, build, audit),
vulnerability scan, and browser checks including the three-session realtime
test. The browser job had never actually executed until this point, because it
depends on the backend job and that had been failing since CI was introduced.

**A warning about the green tick.** A bare `go test ./...` without
`TEST_MONGODB_URI` and `TEST_REDIS_URL` skips the entire integration suite and
still exits 0. CI sets both and then asserts the suite ran, because a tick that
can be earned by skipping is worse than no tick at all.

---

## The realtime requirement, proven rather than asserted

`e2e/realtime.mjs` runs four independent browser sessions against one poll: the
owner watching results, a voter, a bystander who never votes, and a second voter.
Two details make it evidence rather than decoration — every page wraps
`WebSocket` before any application code runs and records what arrives, so a pass
cannot be explained away by a background refetch; and every navigation is
counted, because "updates without a refresh" is not true if something quietly
reloaded.

For one run, every layer was checked rather than inferred from the screen:

| Layer | Evidence |
| --- | --- |
| MongoDB | 2 vote documents, 2 distinct voter ids |
| Redis counters | `HGETALL` → `1`, `1`, TTL refreshed |
| Redis Pub/Sub | `PSUBSCRIBE` captured both events, each with the full tally |
| WebSocket | frames recorded in-page on two sessions that never voted |
| React | totals 0 → 1 → 2 with the navigation count unchanged |

---

## What the process actually caught

Listed because a compliance report that only records successes is not evidence of
a process, and because each of these would have shipped.

1. **`HINCRBY` on a cold Redis key** would have created a hash containing `{option:
   1}` and published a tally of one to every watcher as though it were the truth.
   The increment is now a script that refuses a cold key and rebuilds from MongoDB.
2. **Wiring that existed only in `main.go`.** Twice — the rate limiter, then the
   poll-closed announcement — a callback was forgotten elsewhere and everything
   still compiled. The second became a constructor argument: a callback compiles
   fine when forgotten, an argument does not.
3. **A green test suite that proved nothing.** The integration suite was skipping
   itself; the 0.010s runtime gave it away.
4. **The connection badge lied.** An idle socket does not notice a dead network,
   so the badge read "Live" for up to a minute after the connection was gone. The
   hook now listens for the browser's `offline` event.
5. **nginx dropped every security header.** `add_header` in a `location` block
   discards the ones inherited from `server`. The headers were in the config file
   and in no response.
6. **A double tap created two polls** — a stale closure in the submit guard.
7. **A handler parsing `bson.ObjectID`**, found in the Phase 17 layering review.
8. **Documentation that was confidently wrong**: the option maximum, an
   undocumented `expiresAt` feature, and a Go version that would not have built.
9. **CI built with a vulnerable toolchain.** `go.mod` declares `go 1.26.0` as the
   minimum language version; `actions/setup-go` read that as an exact version and
   installed the first 1.26 release, whose standard library carries 22 known
   vulnerabilities fixed across 1.26.1 to 1.26.6. Local runs never saw it because
   `GOTOOLCHAIN=auto` had been fetching go1.26.8 all along.
10. **A reachable advisory in a transitive dependency** — QPACK memory exhaustion
    in `quic-go`, pulled in by Gin for HTTP/3 this application never uses. It was
    hidden behind the toolchain findings until those cleared.
11. **The browser suite had never actually run in CI.** It depends on the backend
    job, which had been failing, so it was skipped every time. On its first real
    run it failed: `frontend/.env` supplies the API origin locally and is
    gitignored, CI never set it, so the app called its own origin for `/api` and
    every session check failed.

---

## Security

24 adversarial checks pass: authentication bypass (5), authorization bypass (4),
injection (5), mass assignment (2), output safety (3), error leakage (2),
transport (3). Stored XSS payloads render as inert text in a real browser.
Production refuses to start misconfigured — four cases, each exiting 1.

Full detail, including the limitations, in [`security.md`](./security.md). The
honest headline: **duplicate voting is best-effort**, because a voter is a signed
cookie and a private window is a new voter. No cookie-based scheme can prevent
that; genuine one-vote-per-person needs accounts, which the brief excludes for the
audience.

---

## Known limitations

1. Duplicate voting is best-effort, as above.
2. Rate limiting is per IP, so one NAT shares an allowance.
3. No password reset and no e-mail verification.
4. Results are eventually consistent by a few milliseconds: MongoDB is written
   and acknowledged first, then Redis and the broadcast.
5. **`govulncheck` could not run in the build environment** — its database host is
   blocked by the network policy. It runs in CI.

---

## What remains before submission

| # | Item | Owner |
| --- | --- | --- |
| 1 | Make the repository public and merge the branch | Developer |
| 2 | Provision MongoDB Atlas, Redis and a WebSocket-capable host; deploy | Developer |
| 3 | Record the 3–5 minute video | Developer |
| 4 | Send the repo link, live link and video to `devhiring@hclguvi.com` | Developer |

Steps 1 and 3 need no infrastructure. Step 2 is the five steps in
[`deployment.md`](./deployment.md), against config that has already been
exercised against the production images.
