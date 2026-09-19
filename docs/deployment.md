# Deployment

What this application needs from a host, how to configure it, and the specific
ways a deployment of *this* design goes wrong.

---

## What the host must provide

| Piece | Requirement | Why it is not negotiable |
| --- | --- | --- |
| Backend | A host that supports **long-lived WebSocket connections** | The entire realtime path is a persistent connection. A platform that terminates idle connections at 30 seconds, or a serverless runtime billed per request, cannot run this |
| MongoDB | Any MongoDB 7 (Atlas free tier is enough) | Durable source of truth |
| Redis | Any Redis 7 with **Pub/Sub and Lua scripting** | Managed Redis with scripting disabled will not work; the counters depend on `EVAL` |
| Frontend | Any static host | It is a built bundle of HTML, CSS and JS |

The backend and frontend deploy separately and talk over HTTPS and WSS across
origins. That is why cookies are `SameSite=None; Secure` in production, and why
the CORS allow-list has to be exact.

---

## Configuration

Every value is read once, at startup, by `internal/config`. Nothing else in the
codebase reads the environment, so this table is the complete surface.

| Variable | Default | Notes |
| --- | --- | --- |
| `APP_ENV` | `development` | `production` turns on the strict checks below |
| `PORT` | `8080` | Most platforms set this for you |
| `MONGODB_URI` | `mongodb://localhost:27017` | Atlas SRV string in production |
| `MONGODB_DATABASE` | `livepolling` | |
| `REDIS_URL` | `redis://localhost:6379/0` | `rediss://` for TLS |
| `JWT_SECRET` | *(none)* | **Required.** ≥32 characters in production. `openssl rand -base64 48` |
| `JWT_EXPIRES_IN` | `24h` | |
| `FRONTEND_URL` | `http://localhost:5173` | Comma-separated exact origins. Never `*` |
| `COOKIE_SECURE` | `true` in production | |
| `COOKIE_DOMAIN` | *(empty)* | Only if API and frontend share a parent domain |
| `TRUSTED_PROXIES` | *(empty)* | The platform's proxy range, if you want `X-Forwarded-For` believed |

Frontend build-time values (baked into the bundle, so **never** put a secret
here):

| Variable | Example |
| --- | --- |
| `VITE_API_URL` | `https://api.example.com` |
| `VITE_WS_URL` | `wss://api.example.com` |

### Production refuses to start when misconfigured

Rather than starting in a weakened state and looking healthy. Each of these exits
with status 1 and an explanatory message — verified, not assumed:

- `JWT_SECRET` missing or shorter than 32 characters
- `COOKIE_SECURE=false`
- `FRONTEND_URL=*`

A misconfigured deployment that refuses to boot is a failed deploy, which somebody
notices. One that boots with authentication cookies sent over plain HTTP is a
security incident nobody notices.

---

## Going to production

1. **Provision MongoDB and Redis.** Note both connection strings. Indexes are
   created by the application at startup, so there is no migration step.

2. **Deploy the backend** with the environment above and `APP_ENV=production`.
   Point the platform's health check at `/health`, which reports MongoDB and Redis
   separately — an instance that cannot reach its database should not be counted
   healthy just because it can answer HTTP.

3. **Build and deploy the frontend** with `VITE_API_URL` and `VITE_WS_URL` set to
   the backend's public origin:

   ```bash
   cd frontend && npm ci && npm run build   # → dist/
   ```

   Serve `dist/` with SPA fallback: unknown paths must return `index.html`, or a
   shared link opened directly (`/polls/abc123`) returns 404 — which would break
   the single most important URL in the product.

4. **Set `FRONTEND_URL`** on the backend to the frontend's exact origin and
   redeploy. This is both the CORS allow-list and the WebSocket `Origin` check.

5. **Verify against the real deployment**, not localhost:

   ```bash
   curl -s https://api.example.com/health
   ```

   Then open the app in two browsers and vote in one. If the other does not move,
   the WebSocket is not connecting — see below.

---

## The failure you will actually hit

**The WebSocket doesn't connect in production, and everything else works.**

It is the most likely deployment problem here, because a socket has three ways to
fail that ordinary requests do not:

- **`VITE_WS_URL` uses `ws://` on an HTTPS page.** Browsers block mixed content.
  It must be `wss://`.
- **The origin is not in `FRONTEND_URL`.** WebSocket upgrades bypass CORS
  entirely, so the server checks `Origin` itself and refuses an unlisted one. A
  trailing slash or `http://` versus `https://` is enough to miss.
- **A proxy in front of the backend is not forwarding the upgrade.** Some CDNs and
  load balancers need WebSockets explicitly enabled.

The connection badge tells you which world you are in: `Reconnecting…` that never
becomes `Live` means the socket is being refused, while `Live` with results that
never move means the socket is fine and something behind it is not.

**Other things worth checking on a first deploy:**

- *Votes work but nobody else's screen updates* — Redis Pub/Sub is not reaching
  other instances. Check every instance has the same `REDIS_URL`, and that the
  managed Redis has not disabled Pub/Sub.
- *Everything 401s after login* — cookies are being dropped. In production they
  are `SameSite=None; Secure`, which requires HTTPS on both ends, and the frontend
  must send `credentials: 'include'` (it does).
- *All users share one rate-limit bucket* — every request looks like it comes from
  the platform's proxy. Set `TRUSTED_PROXIES` to its range.

---

## Scaling

Nothing here is single-instance. Run as many backends as you like:

- Live counters are in Redis, shared.
- Every instance holds one `PSUBSCRIBE poll:*:updates`, so a vote handled by one
  reaches viewers connected to any other.
- Rate-limit counters are in Redis, so the limit is global rather than per
  instance.
- Sessions are JWTs, so there is no session store to share and no sticky
  sessions to configure.

The one thing to watch is **connection count**, not CPU: each viewer holds a
socket for as long as they are watching. Scale on connections.

---

## Backups

MongoDB is the only thing worth backing up. Redis holds derived state — losing it
costs one slower request per poll while the counters rebuild, and nothing else.
That is the whole point of the arrangement described in
[realtime.md](./realtime.md).

---

## The images

Both are in the repository and both build.

**`backend/Dockerfile`** compiles a static binary and copies it into `scratch`
with nothing but CA certificates for company — 43.5 MB, no shell, no package
manager, no Go toolchain, running as UID 65532. There is nothing in it to exploit
because there is nothing in it. `CGO_ENABLED=0` is what makes that possible: with
cgo the binary would link against the build image's libc, which is not there at
runtime.

It deliberately declares no `HEALTHCHECK`, because there is no shell in the image
to run one with. The platform should check `GET /health` over HTTP.

**`frontend/Dockerfile`** builds the bundle and serves it with nginx. The API
origin is baked in at build time, so build one image per environment — and never
pass a secret as a build argument, because every one of them ends up in readable
JavaScript.

One nginx detail is worth knowing, because it fails silently. `add_header`
directives are not merged across levels: a `location` block that declares any
`add_header` of its own discards every `add_header` inherited from the `server`
block. Both locations here set their own `Cache-Control`, so server-level
security headers never reached the browser until they were moved into
`security-headers.conf` and included in each location. The headers looked right
in the config file and were absent from every response.

## Self-hosting on one machine

```bash
JWT_SECRET=$(openssl rand -base64 48) docker compose -f docker-compose.prod.yml up --build
```

Everything in production mode on one box. Good for trying a production build;
not the recommended topology, because MongoDB and Redis in single containers give
you no backups and no failover.

Note that `COOKIE_SECURE` is true, so this needs a TLS-terminating proxy in
front. Over plain `http://localhost` the browser refuses to store the session
cookie — which is the setting working, not a bug in it.

## A worked example

`render.yaml` deploys the API, the static frontend and Redis as a Render
blueprint, with `JWT_SECRET` generated by the platform and the Atlas connection
string entered in the dashboard rather than committed. Render is used because it
supports WebSockets and managed Redis; Fly.io and Railway work on the same terms.

## Status of the live deployment

The public URL required by the brief is **not yet provisioned**, because it needs
hosting accounts belonging to the developer — MongoDB Atlas, a Redis host and a
WebSocket-capable backend host.

Everything short of that has been verified against the production images running
in Docker behind TLS, with the frontend and API on separate origins — the same
cross-origin arrangement as a real deployment. That run confirmed the production
cookie attributes (`HttpOnly; Secure; SameSite=None`), HSTS, the exact CORS
allow-list, Gin in release mode, all four startup refusals exiting 1, the SPA
fallback on a deep link, and all three browser suites passing — including the
full realtime path over **WSS across origins**, which is the part most likely to
break in production.

It also confirmed the recovery claim this architecture rests on: `FLUSHALL`
against a live Redis left the results unchanged and rebuilt the counters from
MongoDB with a fresh TTL.

This is recorded honestly in
[requirements-traceability.md](./requirements-traceability.md) under R18 rather
than marked complete.
