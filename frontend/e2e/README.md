# Browser checks

Two scripts that drive a real Chromium against a running stack. They are not unit
tests: they exercise the things that only exist in a browser — contrast as
actually rendered, focus order, whether a skeleton flickers, whether a double tap
creates two polls.

## Running them

Start the stack first (`docker compose up -d`, then the backend and frontend), then:

```bash
npm install playwright          # once
node e2e/accessibility.mjs      # contrast, overflow, landmarks, titles
node e2e/ux-states.mjs          # loading / empty / error / success, keyboard, motion
```

Both need a signed-in session and a poll to work with, supplied through the
environment:

```bash
STATE="$(cat state.json)"   # a Playwright storageState for a registered user
POLL_ID="<an existing poll id>"
```

`accessibility.mjs` walks nine screens across two colour schemes and three
viewport widths (320 / 768 / 1440) and fails on: text below the WCAG AA contrast
ratio for its size, horizontal overflow, a page without exactly one `h1`, a
missing `#main` for the skip link, or a page that did not set its own title.

`ux-states.mjs` deliberately produces each async state — an unreachable server, a
1.2 second response, a poll with no votes, a successful vote — and checks the
interface says something useful in each, then runs a keyboard-only vote and
checks reduced motion is honoured.
