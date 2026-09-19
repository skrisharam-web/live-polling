# Browser checks

Three scripts that drive a real Chromium against a running stack. They are not
unit tests: they exercise the things that only exist in a browser — contrast as
actually rendered, focus order, whether a skeleton flickers, whether a double tap
creates two polls, and whether three separate people watching one poll all see a
vote arrive.

## Running them

Start the stack first (`docker compose up -d`, then the backend and frontend):

```bash
npm install                     # playwright is a devDependency
npx playwright install chromium # once, unless CHROMIUM_PATH points at one
node e2e/accessibility.mjs      # contrast, overflow, landmarks, titles
node e2e/ux-states.mjs          # loading / empty / error / success, keyboard, motion
node e2e/realtime.mjs           # three sessions, one vote, live updates
```

All three need a signed-in session, supplied as a Playwright `storageState`:

```bash
STATE="$(cat state.json)"       # a storageState holding an lp_session cookie
POLL_ID="<an existing poll id>" # accessibility.mjs only
CHROMIUM_PATH=/path/to/chrome   # optional; omit to use Playwright's own browser
```

`ux-states.mjs` and `realtime.mjs` create the polls they need, because a script
whose assertions depend on some poll that already exists starts failing the
moment that poll is voted in or deleted.

## What each one asserts

**`accessibility.mjs`** walks nine screens across two colour schemes and three
viewport widths (320 / 768 / 1440) and fails on: text below the WCAG AA contrast
ratio for its size, horizontal overflow, a page without exactly one `h1`, a
missing `#main` for the skip link, or a page that did not set its own title.

**`ux-states.mjs`** deliberately produces each async state — an unreachable
server, a 1.2 second response, a poll with no votes, a successful vote — and
checks the interface says something useful in each, then runs a keyboard-only
vote and checks reduced motion is honoured.

**`realtime.mjs`** is the end-to-end proof, and the one worth reading. Four
independent browser sessions share a poll: the owner watching results, a voter,
a bystander who never votes, and a second voter. It asserts that a vote reaches
every screen, and two things that make that assertion mean something:

- every page wraps `WebSocket` before any application code runs and records what
  arrives, so a passing check cannot be explained away by a background refetch;
- every navigation is counted, because "updates without a refresh" is not true
  if something quietly reloaded.

It then pulls the bystander's network out from under it, casts a vote while it is
offline, and checks that reconnecting recovers the missed vote — the gap that
Pub/Sub cannot close on its own, since a message missed while disconnected is
gone for good.
