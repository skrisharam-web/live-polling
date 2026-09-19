# Security

What this application defends against, how, and — just as importantly — what it
does not defend against.

Everything below was verified against a running system, not just read in the
code. The adversarial checks live in `backend/tests/integration/` and are listed
per section.

---

## Authentication

**Passwords** are hashed with bcrypt at cost 12 and never stored, logged or
returned. The cost is a field rather than a constant so tests can run at the
minimum while `TestProductionUsesAStrongBcryptCost` asserts the shipped value and
reads the cost back out of a real stored hash — the one thing standing between a
stolen database and the plaintext passwords in it should not be able to drift.

Input is capped at 72 bytes because bcrypt silently ignores anything beyond that.
Accepting a longer password would make two different long passwords
interchangeable, which is worse than refusing the input.

**Sessions** are JWTs (HS256) delivered in a cookie that is `HttpOnly`, `Secure`
in production, `SameSite=None` in production and `Lax` locally, scoped to `/`,
with `Max-Age` matched to the token's own expiry so the browser stops sending a
token the server would reject anyway.

The token is never returned in a response body. An integration test asserts that
no response contains the token or the string `password`, because the whole point
of `HttpOnly` is lost if the value is also handed to page JavaScript.

**Verification** pins three things that are each an attack on their own:

| Pinned | Attack it closes |
| --- | --- |
| `alg` must be HS256 | `alg: none` tokens, and algorithm confusion where an RS256 verifier is fed the public key as an HMAC secret |
| `iss` must be this application | A token minted by another service that happens to share the secret |
| `exp` must be present | A token with no expiry, valid forever |

Verified live: garbage cookies, unsigned `alg: none` tokens, tokens signed with
another key, tampered signatures and truncated tokens are all refused with 401.

**Account enumeration.** Every failed sign-in returns the same code, the same
message and no field detail, and an unknown address still pays for a bcrypt
comparison against a dummy hash so the timing matches. `TestLogin` asserts the
messages are identical; a test that allowed them to diverge would let an attacker
work out which addresses have accounts.

Registration is the deliberate exception: it tells you an address is already
taken, because the alternative — silently doing nothing — is a worse experience
for the overwhelmingly common case of someone who forgot they had an account.

---

## Authorization

Every management operation goes through `PollService.GetOwned`, which loads the
poll and compares its owner with the session user: 404 when it does not exist,
403 when it belongs to someone else. The repository *additionally* scopes its own
filter by `ownerId`, so a poll belonging to somebody else does not match even if
a future caller forgets the check.

**The owner is never read from a payload.** The create request type has no owner
field at all. `TestOwnershipCannotBeClaimedInThePayload` sends `ownerId`, `id` and
`status` anyway and asserts the server-generated values win.

Verified live: a second, fully authenticated user gets 403 from manage, patch,
close and delete, and the poll is confirmed unchanged afterwards.

---

## Voting identity and duplicate prevention

Voters have no account. Identity is 128 random bits in a signed, `HttpOnly`
cookie, and the vote endpoint reads the voter only from that cookie — the vote
request type has no voter field.

The signature matters: without it a client could hand itself a fresh identity on
every request and vote as often as it liked without even clearing cookies. Five
forgery shapes (invented value, no signature, tampered identity, tampered
signature, empty) are each rejected and replaced with a server-issued identity.

Duplicate prevention is the **unique `(pollId, voterId)` index**, not a
read-then-write check. The difference only shows up under concurrency, which is
exactly when it matters — a double-tapped button on a phone. Proven by test: 40
simultaneous inserts for one voter leave exactly one vote; 20 simultaneous
requests through the real HTTP stack produce one 201 and nineteen 409s.

### What this does not promise

Clearing cookies, opening a private window or using another device produces a new
identity. **No cookie-based scheme can prevent that.** The cookie stops the
realistic accident and casual abuse; genuine one-vote-per-person needs accounts,
which the brief deliberately excludes for the audience. Saying otherwise would be
the security equivalent of a padlock painted on a door.

---

## Input validation

Every client-supplied value is validated server-side, in the service layer, after
binding and before any repository call — there is no path into MongoDB that
bypasses it. Frontend validation exists for the person typing; it is not a
control.

Validated: e-mail format and length, password length, name, question length,
option count and length, duplicate options, option membership in *this* poll,
poll status, expiry bounds, and body size.

**Injection.** MongoDB is queried with typed structs and `bson.M` values, never
with strings assembled from input, so there is no query to inject into. The
classic NoSQL attack — sending `{"$ne": null}` where a string is expected — fails
at JSON decoding, verified for e-mail, password, option ID and poll ID.

A malformed poll ID answers 404 rather than 400, so the endpoint cannot be used
to learn which ID shapes are real.

**Output.** The API only ever serves `application/json`, with `nosniff` set.
Markup submitted as a question is stored verbatim and rendered by React as text —
verified in a real browser with `<img onerror>`, `<script>` and `<svg onload>`
payloads: no script ran, no dialog appeared, no element was injected, and the
payload is visible as text. There is no `dangerouslySetInnerHTML` anywhere in the
frontend.

---

## Cross-origin and CSRF

CORS uses an exact allow-list and echoes back only an origin on it. A wildcard is
rejected at startup, because pairing one with credentialed requests would let any
site on the internet drive the API as a logged-in user.

Production needs `SameSite=None` for a frontend on a different host, which gives
up SameSite as a CSRF defence. The replacement is a **required JSON content
type** on every state-changing request: a browser can only send a cross-site form
as one of three content types, none of which is JSON, so the request becomes
"non-simple", needs a preflight, and the origin allow-list refuses it. Verified:
`application/x-www-form-urlencoded`, `multipart/form-data` and `text/plain` are
all refused.

**WebSockets are not covered by CORS or the same-origin policy**, so the upgrade
checks `Origin` against the same allow-list. Without it, any page could open
sockets here. Clients that send no `Origin` — curl, tests, a mobile app — are
allowed, since the policy protects browsers and refusing them would only break
tooling.

---

## Rate limiting

Redis-backed fixed-window counters, shared across backend instances, scoped per
endpoint group and client IP:

| Scope | Limit |
| --- | --- |
| Sign in / register | 20 per 15 minutes |
| Voting | 60 per minute |
| Poll creation | 30 per hour |

The window's expiry is set only when the counter is created. Refreshing it on
every request would slide the window forward forever and lock a busy client out
permanently — a rate limiter that quietly became a ban.

`X-Forwarded-For` is believed only from proxies named in `TRUSTED_PROXIES`, which
is empty by default. Trusting an unnamed proxy lets any client spoof its address
and walk past the limiter.

**The limiter fails open.** If Redis cannot answer, the request is allowed and the
failure is logged. That is a deliberate trade: failing closed would turn a Redis
blip into a total outage of voting and sign-in, and this limiter exists to slow
down abuse, not to decide whether the site works.

---

## Response headers

Set on every response by `middleware.SecurityHeaders`:

| Header | Why |
| --- | --- |
| `X-Content-Type-Options: nosniff` | Stops a browser deciding a JSON response is really HTML |
| `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'; base-uri 'none'` | This origin serves JSON and a WebSocket; a response from it should never load a script, image or stylesheet |
| `X-Frame-Options: DENY` | The same framing protection for browsers predating CSP |
| `Referrer-Policy: strict-origin-when-cross-origin` | A share link is a capability; the `Referer` header should not hand poll IDs to third parties |
| `Strict-Transport-Security` (production only) | Sending it from a development server over plain HTTP would pin localhost to HTTPS for a year |

The frontend is served separately and needs its own CSP; see
[`deployment.md`](./deployment.md).

---

## Error handling and logging

Domain errors carry a message written for a person. Anything else becomes a
generic 500 and the real error goes to the log. Verified: responses contain no
driver text, no `.go:` line references, no goroutine dumps and no status codes
embedded in prose.

Request logs record method, path, status, duration and request ID — deliberately
no bodies, no cookies and no headers, so passwords and session tokens cannot end
up in log storage. Panics are recovered, logged with a stack trace, and answered
with the standard error envelope.

---

## Configuration and secrets

No secret is committed: `.env` is ignored, only `.env.example` is tracked, and a
repository-wide search for assigned secrets finds nothing but placeholders.

Production **refuses to start** when misconfigured, rather than starting in a
weakened state. Verified live, each exiting with status 1:

- `JWT_SECRET` missing or shorter than 32 characters
- `COOKIE_SECURE=false`
- `FRONTEND_URL=*`

---

## Resource limits

- Request bodies capped at 64 KB, answered with 413 rather than being read.
- WebSocket read limit of 512 bytes per message on a socket clients have no
  reason to write to.
- `ReadHeaderTimeout` of 10 seconds, so a connection that never finishes sending
  headers cannot hold a goroutine open.
- Per-connection write deadlines and ping/pong, so a dead peer is detected rather
  than accumulated.
- A slow WebSocket client is disconnected rather than waited for, so one bad
  connection cannot stall everyone watching the same poll.
- The dashboard query is bounded, so no request can ask for an unbounded result set.

---

## Known limitations

Stated plainly, because a security document that lists only strengths is not one.

1. **Duplicate voting is best-effort**, as described above. This is inherent to
   voting without accounts.
2. **No e-mail verification**, so an address can be registered by someone who does
   not own it. It gates poll creation, not anything belonging to the address.
3. **No password reset**, and no account lockout beyond the rate limit. Both are
   real products in their own right and neither is part of the brief.
4. **Rate limiting is per IP**, so users behind one NAT share an allowance and a
   distributed attacker gets one per address. It raises the cost of abuse; it does
   not stop a determined one.
5. **A fixed window is coarse**: a client can send the limit at the end of one
   window and again at the start of the next. A sliding window would cost more per
   request for a benefit this application does not need.
6. **`govulncheck` cannot run in the development sandbox** — its database host,
   `vuln.go.dev`, is denied by that environment's network policy. It runs in CI,
   in its own job, and a finding fails the build.

   That scan earned its place immediately: it failed, and the cause was the
   toolchain rather than this code. `go.mod` declares `go 1.26.0` as the minimum
   language version, and `actions/setup-go` with `go-version-file` installs
   exactly that — the first 1.26 release, whose standard library carries 22 known
   vulnerabilities fixed across 1.26.1 to 1.26.6 (`html/template` escaping
   bypasses, an `os` root escape via symlink, `net` DNS crashes, and others).
   CI now installs the newest 1.26.x instead, so patches arrive without anyone
   remembering to bump a pin.

   Worth stating plainly: **the shipped binary was never affected.** The
   production image builds on `golang:1.26-alpine`, which is go1.26.8 — ahead of
   every fix version above. The vulnerable toolchain existed only in CI, which is
   exactly the sort of thing a scanner is for and a code review is not.

   `npm audit` reports no vulnerabilities in production dependencies.

---

## Audit results

24 adversarial checks, all passing: authentication bypass (5), authorization
bypass (4), injection (5), mass assignment (2), output safety (3), error leakage
(2), and transport (3). Plus, in a real browser: stored XSS payloads render as
inert text, and production cookies carry `HttpOnly; Secure; SameSite=None`.
