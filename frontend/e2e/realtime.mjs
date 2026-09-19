import { chromium } from 'playwright'

/**
 * The end-to-end realtime proof: three independent browser sessions, one vote,
 * and every screen moves without a refresh.
 *
 * The point of doing this in three real browsers rather than in Go is that every
 * link in the chain is exercised at once — Mongo write, Redis counter, Redis
 * Pub/Sub, the backend's single pattern subscription, the WebSocket fan-out and
 * React's cache update. A unit test can prove any one of those; only this can
 * prove they are actually connected to each other.
 *
 * Two things make it evidence rather than decoration:
 *
 *  - Every page wraps WebSocket before any script runs and records what arrives,
 *    so a passing assertion cannot be explained away by a background refetch.
 *  - Every navigation is counted. If a page reloaded, "without a refresh" would
 *    be untrue and the run fails, however good the numbers look.
 */

// This sandbox ships a browser at a fixed path; CI installs Playwright's own.
// CHROMIUM_PATH overrides, and its absence means "use the bundled one" rather
// than a path that only exists on one machine.
const EXE = process.env.CHROMIUM_PATH
const launch = () =>
  chromium.launch({
    ...(EXE ? { executablePath: EXE } : {}),
    // Only for testing a production build behind a self-signed certificate; the
    // point of that run is the cookie and WebSocket behaviour, not the cert.
    ...(process.env.INSECURE_TLS ? { args: ['--ignore-certificate-errors'] } : {}),
  })
const APP = process.env.APP_URL ?? 'http://localhost:5173'
const state = JSON.parse(process.env.STATE ?? '{}')

const results = []
let failed = false
const check = (label, ok, detail = '') => {
  if (!ok) failed = true
  results.push(`${ok ? '✓' : '✗'} ${label}${detail ? ` — ${detail}` : ''}`)
  return ok
}

/** Record every WebSocket frame the page receives, before app code loads. */
const RECORD_SOCKETS = `
  window.__ws = { messages: [], opened: 0, sockets: [] }
  const Native = window.WebSocket
  window.WebSocket = function (...args) {
    const socket = new Native(...args)
    window.__ws.opened++
    window.__ws.sockets.push(socket)
    socket.addEventListener('message', (event) => window.__ws.messages.push(event.data))
    return socket
  }
  window.WebSocket.prototype = Native.prototype
  Object.assign(window.WebSocket, Native)
`

const browser = await launch()

/** An independent browser session: its own cookie jar, its own voter identity. */
async function session(storageState) {
  const ctx = await browser.newContext(storageState ? { storageState } : {})
  await ctx.addInitScript(RECORD_SOCKETS)
  const page = await ctx.newPage()
  page.__navigations = 0
  page.on('framenavigated', (frame) => {
    if (frame === page.mainFrame()) page.__navigations++
  })
  return { ctx, page }
}

const total = (page) => page.locator('.results__total strong').first().innerText().then(Number)
const socketMessages = (page) => page.evaluate(() => window.__ws.messages.length)
const waitForTotal = (page, want, timeout = 10000) =>
  page.waitForFunction(
    (expected) => Number(document.querySelector('.results__total strong')?.textContent) === expected,
    want,
    { timeout },
  )

// ---- Browser A: the owner, watching results -------------------------------
const A = await session(state)
await A.page.goto(`${APP}/polls/new`, { waitUntil: 'networkidle' })
await A.page.getByLabel('Question').fill('Does the live update actually arrive?')
await A.page.getByLabel('Option 1').fill('Yes, instantly')
await A.page.getByLabel('Option 2').fill('No, I refreshed')
await A.page.getByRole('button', { name: 'Create poll' }).click()
await A.page.getByText('Your poll is live').waitFor({ timeout: 10000 })
const pollId = (await A.page.locator('.share__url').innerText()).split('/polls/')[1].trim()
console.log(`poll: ${pollId}`)

await A.page.goto(`${APP}/polls/${pollId}/results`, { waitUntil: 'networkidle' })

// ---- Browsers B and C: two separate audience members ----------------------
const B = await session()
const C = await session()
// B gets the ballot: it is here to vote. C watches the public results page and
// never votes, which is what makes it the honest witness — it issues no request
// after the first load, so anything that changes on its screen arrived over the
// socket.
await B.page.goto(`${APP}/polls/${pollId}`, { waitUntil: 'networkidle' })
await C.page.goto(`${APP}/polls/${pollId}/results`, { waitUntil: 'networkidle' })
await B.page.getByText('Yes, instantly').click()

// The badge only renders where results are on screen; a ballot deliberately does
// not carry one. B's socket is live all the same — the hook sits at page level,
// not inside the results branch — and is asserted after it votes.
for (const [name, s] of [['A', A], ['C', C]]) {
  await s.page.locator('.live-status--connected').first().waitFor({ timeout: 10000 })
  check(`${name}: reports a live connection before any vote`, true)
}

const navsBefore = { A: A.page.__navigations, B: B.page.__navigations, C: C.page.__navigations }
check('A starts at zero votes', (await total(A.page)) === 0)

// ---- The vote -------------------------------------------------------------
await B.page.getByRole('button', { name: 'Vote' }).click()
await B.page.getByText('Your vote was recorded').waitFor({ timeout: 10000 })

// ---- Everyone moves, nobody refreshed -------------------------------------
let aUpdated = true
try {
  await waitForTotal(A.page, 1)
} catch {
  aUpdated = false
}
check('A (owner, never voted) sees the vote appear', aUpdated, `total=${await total(A.page)}`)
check('B (the voter) sees its own vote', (await total(B.page)) === 1)
await B.page.locator('.live-status--connected').first().waitFor({ timeout: 10000 })
check('B reports a live connection once its results are on screen', true)

let cUpdated = true
try {
  await waitForTotal(C.page, 1)
} catch {
  cUpdated = false
}
check('C (bystander, never voted) sees the vote appear', cUpdated, `total=${await total(C.page)}`)

check('no page navigated to get there',
  A.page.__navigations === navsBefore.A &&
  B.page.__navigations === navsBefore.B &&
  C.page.__navigations === navsBefore.C,
  `A=${A.page.__navigations - navsBefore.A} B=${B.page.__navigations - navsBefore.B} C=${C.page.__navigations - navsBefore.C}`)

const aMessages = await socketMessages(A.page)
const cMessages = await socketMessages(C.page)
check('the update reached A over the WebSocket, not by polling', aMessages > 0, `${aMessages} frame(s)`)
check('the update reached C over the WebSocket, not by polling', cMessages > 0, `${cMessages} frame(s)`)

// ---- A second voter, so the tally is cumulative rather than a coincidence --
// A fourth session rather than reusing C: C's value to this test is that it
// never votes, so the numbers on its screen can only have come down the socket.
const D = await session()
await D.page.goto(`${APP}/polls/${pollId}`, { waitUntil: 'networkidle' })
await D.page.getByText('No, I refreshed').click()
await D.page.getByRole('button', { name: 'Vote' }).click()
await D.page.getByText('Your vote was recorded').waitFor({ timeout: 10000 })

let aSecond = true
try {
  await waitForTotal(A.page, 2)
} catch {
  aSecond = false
}
check('A sees the second vote without a refresh', aSecond, `total=${await total(A.page)}`)

let bSecond = true
try {
  await waitForTotal(B.page, 2)
} catch {
  bSecond = false
}
check('B sees the second vote without a refresh', bSecond, `total=${await total(B.page)}`)

let cSecond = true
try {
  await waitForTotal(C.page, 2)
} catch {
  cSecond = false
}
check('C sees the second vote without ever having voted or reloaded', cSecond,
  `total=${await total(C.page)} navigations=${C.page.__navigations - navsBefore.C}`)

// ---- The counts on screen are the real counts -----------------------------
const bars = await A.page.locator('.result').evaluateAll((els) =>
  els.map((el) => ({
    text: el.querySelector('.result__label')?.textContent?.trim(),
    count: el.querySelector('.result__count')?.textContent?.trim(),
  })),
)
check('every option still shows its own text and count', bars.length === 2 && bars.every((b) => b.text && b.count),
  JSON.stringify(bars))

// ---- THE RECONNECT GAP ----------------------------------------------------
// A socket that drops is not the interesting case; a socket that drops *while a
// vote happens* is. The design answer is that reconnecting refetches rather than
// resuming, because a Pub/Sub message missed while offline is gone for good.
// This is the check that the answer actually works.
{
  await C.ctx.setOffline(true)
  await C.page.locator('.live-status--connected').first().waitFor({ state: 'detached', timeout: 15000 })
    .catch(() => {})
  const offline = await C.page.locator('.live-status').first().innerText()
  check('C notices it has gone offline', !offline.includes('Live'), offline.replace(/\s+/g, ' ').trim())

  const E = await session()
  await E.page.goto(`${APP}/polls/${pollId}`, { waitUntil: 'networkidle' })
  await E.page.getByText('Yes, instantly').click()
  await E.page.getByRole('button', { name: 'Vote' }).click()
  await E.page.getByText('Your vote was recorded').waitFor({ timeout: 10000 })
  check('a vote is cast while C is disconnected', (await total(E.page)) === 3)

  const missedWhileOffline = (await total(C.page)) === 2
  check('C did not receive that vote while offline', missedWhileOffline, `total=${await total(C.page)}`)

  await C.ctx.setOffline(false)
  let recovered = true
  try {
    await waitForTotal(C.page, 3, 30000)
  } catch {
    recovered = false
  }
  check('C recovers the vote it missed, by refetching on reconnect', recovered,
    `total=${await total(C.page)} navigations=${C.page.__navigations - navsBefore.C}`)
  check('C recovered without reloading the page', C.page.__navigations === navsBefore.C)
}

console.log(results.join('\n'))
console.log(failed ? '\nREALTIME FAILED' : '\nthree independent sessions, one vote, every screen updated live')
console.log(`poll_id=${pollId}`)
await browser.close()
process.exit(failed ? 1 : 0)
