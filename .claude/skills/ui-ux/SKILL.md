---
name: ui-ux
description: UI/UX design rules for the Pulse live-polling frontend (React + TypeScript + plain CSS in frontend/src). Use this skill whenever touching anything the user sees — building or editing a page, component, form, results view, layout or CSS; choosing colour, spacing or type; wiring loading/empty/error/success states; or reviewing a frontend change before calling it done. Use it even when the request sounds purely functional ("add a close-poll button", "show the vote count", "make it responsive"), because those are exactly the moments the UI drifts into generic AI-generated SaaS styling. Not for backend, Go, MongoDB, Redis or WebSocket protocol work.
---

# Pulse UI/UX

Pulse is a live-polling tool. Someone creates a poll, shares a link, an audience votes, and
everyone watches results move in real time. The interface has one job: make the live result
the most compelling thing on the screen. Every design decision is judged against that.

The failure mode to avoid is not ugliness — it is *genericness*. A gradient hero, three
rounded cards in a row, a purple glow and a fade-in on everything reads instantly as
machine-generated and tells a reviewer that nobody made a decision. Restraint is the signal
of deliberate design.

## The design system already exists — use it

`frontend/src/index.css` defines the tokens. Never hard-code a colour, radius, shadow or
spacing value in a component; if something is missing, add a token there so it stays
reusable.

- Colour: `--color-brand`, `--color-brand-strong`, `--color-brand-soft`, `--color-bg`,
  `--color-surface`, `--color-surface-muted`, `--color-border`, `--color-text`,
  `--color-text-muted`, `--color-success`, `--color-danger`, `--color-warning`
  (each status colour has a `-soft` background pair), and `--color-series-1..6` for result bars.
- Spacing: `--space-1` … `--space-7`. Compose layouts from these only; an arbitrary `13px`
  is a bug.
- Radius: `--radius-sm|md|lg|pill`. Shadow: `--shadow-sm|md|lg`. Motion: `--transition`.
- Dark mode is a token override, not a second stylesheet. Anything new must be checked in
  both themes before it is done.

## Visual hierarchy

Each screen has exactly one primary thing. Name it before writing markup, then make
everything else visibly quieter.

| Screen | The one thing |
| --- | --- |
| `/` landing | The "Create a poll" action |
| `/dashboard` | The list of the user's polls |
| `/polls/new` | The question field |
| `/polls/:id` | The options and the vote button — then, after voting, the results |
| `/polls/:id/results` | The live bars |
| `/polls/:id/manage` | The share link and the close-poll control |

Build hierarchy with size, weight, spacing and colour-contrast in that order. Reach for a
border or a background tint before a shadow; reach for a shadow before a card. Most
groupings need nothing more than whitespace.

## Typography

One family (the `--font-sans` stack; `--font-mono` only for the share URL and IDs).

- Page title: `h1`, `clamp()`-scaled, weight 650, tight tracking.
- Section title: `h2`. Card/row title: `h3` at ~1.1rem.
- Body: 1rem / 1.55. Secondary text: 0.875rem in `--color-text-muted` — never smaller than
  0.8125rem, and never muted text on a muted background.
- Poll questions are content, not chrome: render them large (1.25–1.75rem) and let them wrap
  rather than truncating. Option labels must never be clipped or ellipsised — a voter has to
  read the whole option to choose it.
- Numbers that change live (counts, percentages) use `font-variant-numeric: tabular-nums` so
  digits do not jitter as they update.

## Spacing and layout

- Mobile-first. Write the base rules for a 360px-wide phone, then add `min-width` queries.
  The public voting page is the most-used screen and will mostly be opened on a phone.
- Page gutter: `--space-4` on mobile, `--space-6` above 768px. Content column capped at
  `--content-width`; long-form text capped around 65 characters.
- Vertical rhythm: `--space-5` between sections, `--space-3` between related rows. Related
  things sit closer together than unrelated things — that proximity *is* the grouping, so
  you rarely need a divider as well.
- Layout with flex/grid and `gap`. No margin stacking hacks, no absolute positioning for
  layout.
- Never let a layout shift when live data arrives. Reserve the space (fixed bar track height,
  tabular numerals, skeletons matching final dimensions).

## Colour usage

- Brand colour is for action and for the leading result — not for decoration. If more than
  about 10% of a screen is brand-coloured, it is being used decoratively.
- Result bars cycle `--color-series-1..6`, but colour is never the only carrier of meaning:
  every bar also shows its option text, its count and its percentage.
- Status colours mean state, never emphasis: success for a recorded vote, danger for a real
  failure, warning for "poll closed". Do not tint a card green just to look lively.
- Check contrast: body text ≥ 4.5:1, large text and UI borders ≥ 3:1, in both themes.

## Do not build these

These patterns are how AI-generated frontends announce themselves. Avoid them here.

- Gradient backgrounds, gradient text, gradient buttons. Flat brand colour is the house style.
- Glassmorphism, backdrop blur, neon glows, animated blobs, floating orbs.
- A full-viewport hero with a giant centred headline. The landing page states what Pulse does
  in a sentence and gets out of the way.
- Everything in a rounded, shadowed card. Cards are for genuinely repeating objects (a poll in
  the dashboard list). A form is not a card. A page section is not a card.
- A three-up feature grid of icon + heading + lorem paragraph.
- Emoji as UI iconography, decorative illustrations, or icons next to every label.
- Stacked shadows, `--radius-lg` on small controls, or a border *and* a shadow *and* a tint on
  the same element.
- Entrance animations on page load, staggered list reveals, hover lift on everything.

If a proposed element cannot be justified in one sentence that names what the user learns or
does because of it, delete it.

## Interaction states

Every interactive element needs all five, and they must be distinguishable without colour alone:

- **Rest** — clear affordance (filled for primary, bordered for secondary, underlined for links).
- **Hover** — a small, honest change: `--color-brand-strong`, or a `--color-surface-muted`
  background. No scaling, no lifting.
- **Focus-visible** — the global 2px brand outline from `index.css`. Never remove it, never
  replace it with a colour change only.
- **Active** — instantly perceptible (slightly darker). Keep it, it is what makes a tap feel real.
- **Disabled** — reduced contrast plus `cursor: not-allowed` plus `aria-disabled`, and always
  paired with text explaining why (e.g. "You have already voted in this poll").

Touch targets are at least 44×44px, with at least `--space-2` between adjacent ones. Vote
options are the critical case: make the whole option row the target, not just a radio dot.

## Forms and validation

- Every input has a visible `<label>` (never placeholder-as-label). Placeholders show format
  examples only.
- Validate on blur and on submit, not on every keystroke — mid-typing errors feel like nagging.
- Errors sit under the field, are referenced by `aria-describedby`, set `aria-invalid`, and say
  what to do ("Add at least 2 options"), not what failed ("Invalid input").
- The backend is the authority on validation. When it returns field errors, map them onto the
  matching inputs rather than dumping one banner; move focus to the first offending field.
- The submit button disables and switches to a working label ("Creating poll…") while in
  flight, so a double-tap cannot create two polls.

## Async states

Every async surface needs four designed states. A missing empty or error state is an
unfinished feature.

- **Loading** — skeletons shaped like the real content for first loads; an inline spinner in
  the button for actions. Nothing that blocks the whole page. Do not show a spinner for
  anything under ~300ms; it reads as a flash.
- **Empty** — one sentence of context plus the action that resolves it. Dashboard with no
  polls: "No polls yet. Create your first one and share the link." plus the create button.
  A poll with no votes: show the options at 0 with a "Waiting for the first vote" line — the
  zero state *is* live data, not an error.
- **Error** — plain language, the recovery action ("Try again"), and no status codes or driver
  text. Distinguish "not found" (this poll does not exist) from "failed" (we could not reach
  the server).
- **Success** — inline and specific: after voting, the button area becomes "Your vote was
  recorded" and the results appear. After creating a poll, the share link appears with a copy
  button already focused. No toast that disappears before it is read.

## Voting interaction

The vote flow is the product. Optimise it hard.

- Options are a real radio group (`role="radiogroup"`, arrow-key navigable, one tab stop).
  The whole row is clickable and shows a clear selected state — border colour *and* a mark,
  not just a tint.
- The submit button stays disabled until an option is selected, with the reason available to
  screen readers.
- Submitting moves straight into results in the same view. No navigation, no reload.
- A duplicate vote (409 from the backend) is not an error scream: show a calm "You have
  already voted in this poll" and display the results.
- A closed poll shows results with a "Voting has closed" badge and no vote controls.

## Live results

- Bars animate their width over ~400ms with an ease-out; counts change immediately. That one
  transition is the whole motion budget for the results view — it makes the change legible.
  Do not also fade, slide or pulse the row.
- The leading option is visually distinct (brand-coloured bar, heavier label). If the lead
  changes, the reorder must be understandable — animate position or do not reorder at all.
- Always show total votes, and per option: text, count, percentage. Percentages round to whole
  numbers and are labelled as such; do not let rounding make the bars look wrong.
- Wrap the results region in `aria-live="polite"` on a container that announces a concise
  summary, not every individual number, or a screen reader will be flooded during a burst
  of votes.
- Respect `prefers-reduced-motion`: snap bars to their new width instead of animating.

## Realtime connection status

Honest, small, and out of the way — a text-plus-dot badge near the results heading, not a
banner across the page.

- `connected` → a small dot in `--color-success` with the word "Live".
- `connecting` / `reconnecting` → `--color-warning`, "Reconnecting…". Keep showing the last
  known results underneath; do not blank the screen.
- `disconnected` → `--color-text-muted`, "Offline", plus a manual "Refresh results" control.

The dot must never be the only indicator (colour-blind users, and a lone dot means nothing).
Do not animate the dot while connected — a permanently pulsing element is visual noise.

## Motion

Motion explains change; it does not decorate. The complete budget for this app:

1. Result bar width transitions (~400ms, ease-out).
2. State changes on controls (~150ms via `--transition`).
3. A short, single fade for content that genuinely replaces other content (vote form → results).

Everything else is stillness. All of it is disabled under `prefers-reduced-motion`.

## Navigation and information architecture

- One header: product name links home; on the right, either Login/Register or the user's name
  plus Dashboard and Logout. Public poll pages keep the header minimal — a voter arriving from
  a shared link should not be pushed to sign up.
- The current page is marked with `aria-current="page"`.
- Poll owner actions live on the manage page; the dashboard row exposes only the common ones
  (open, results, copy link).
- The share link is a first-class UI object: shown in full in `--font-mono`, selectable, with
  a copy button that confirms in place ("Copied") and reverts after a few seconds.

## Screen-specific notes

**Dashboard.** A list, not a card grid. Each row: question, status badge (active/closed),
total votes, created date, actions. Sort newest first. Show the loading skeleton as rows.

**Create poll.** Question field first and largest. Options as a simple ordered list of inputs
with add/remove; removing is disabled at 2 options and says why. Keyboard: Enter in the last
option adds another. On success, show the share link — that is the moment the product delivers.

**Public poll.** Minimal chrome, question dominant, options thumb-reachable, one primary
button. Works on a 360px screen with no horizontal scroll.

**Results.** Bars are the page. Total votes and the live badge sit above them, secondary.

**Manage.** Share link, live results, and the close control. Closing is destructive-ish:
confirm inline ("Close this poll? Voting stops immediately.") rather than with a browser
`confirm()`.

## Review before calling a feature done

After implementing any frontend feature, do this pass and fix what it turns up. It is part of
the work, not an optional extra.

1. **Both themes** — light and dark, no invisible text, no token left hard-coded.
2. **360px, 768px, 1440px** — no horizontal scroll, no clipped text, no cramped targets.
3. **Keyboard only** — tab through it: logical order, always-visible focus, no trap, Enter/Space
   activate what they should.
4. **The four async states** — deliberately trigger loading, empty, error and success. Any state
   you cannot produce is a state you have not designed.
5. **Generic-UI check** — re-read the "Do not build these" list against the diff. Remove anything
   that matches.
6. **Consistency check** — do the new spacings, radii, weights and button variants match what the
   rest of the app already uses? Two buttons that differ by 2px of padding is the kind of drift
   that makes a UI feel machine-assembled.
7. **Lint and build** — `npm run lint` and `npm run build` must pass before the feature is done.

State briefly what the pass found and what you changed. "Reviewed, no issues" without having
actually exercised the states is worse than not reviewing.
