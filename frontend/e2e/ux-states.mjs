import { chromium } from 'playwright'

const EXE = '/opt/pw-browsers/chromium-1194/chrome-linux/chrome'
const APP = 'http://localhost:5173'
const API = 'http://localhost:8080'
const state = JSON.parse(process.env.STATE ?? '{}')
const pollId = process.env.POLL_ID
const browser = await chromium.launch({ executablePath: EXE })
const results = []
const check = (label, ok, detail = '') => {
  results.push(`${ok ? '✓' : '✗'} ${label}${detail ? ` — ${detail}` : ''}`)
  return ok
}

// ---- EMPTY ------------------------------------------------------------------
// A poll created for this check alone, so the assertion does not depend on what
// any earlier check voted for.
let freshPollId
{
  const ctx = await browser.newContext({ storageState: state })
  const p = await ctx.newPage()
  await p.goto(`${APP}/polls/new`, { waitUntil: 'networkidle' })
  await p.getByLabel('Question').fill('A poll nobody has voted in yet?')
  await p.getByLabel('Option 1').fill('Left')
  await p.getByLabel('Option 2').fill('Right')
  await p.getByRole('button', { name: 'Create poll' }).click()
  await p.getByText('Your poll is live').waitFor({ timeout: 10000 })
  freshPollId = (await p.locator('.share__url').innerText()).split('/polls/')[1]
  await ctx.close()
}
{
  const ctx = await browser.newContext()
  const p = await ctx.newPage()
  await p.goto(`${APP}/polls/${freshPollId}/results`, { waitUntil: 'networkidle' })
  const text = await p.locator('main').innerText()
  check('empty: a poll with no votes explains itself', text.includes('Waiting for the first vote'))
  check('empty: every option is still listed at zero', (await p.locator('.result').count()) === 2)
  await ctx.close()
}
{
  // A brand-new account has no polls.
  const ctx = await browser.newContext()
  const p = await ctx.newPage()
  await p.goto(`${APP}/register`, { waitUntil: 'networkidle' })
  await p.getByLabel('Name').fill('Empty State')
  await p.getByLabel('E-mail address').fill(`empty+${Date.now()}@example.com`)
  await p.getByLabel('Password').fill('correct-horse-battery')
  await p.getByRole('button', { name: 'Create account' }).click()
  await p.waitForURL('**/polls/new')
  await p.goto(`${APP}/dashboard`, { waitUntil: 'networkidle' })
  const text = await p.locator('main').innerText()
  check('empty: the dashboard offers the action that resolves it',
    text.includes('No polls yet') && (await p.getByRole('link', { name: 'Create a poll' }).count()) > 0)
  await ctx.close()
}

// ---- ERROR ------------------------------------------------------------------
{
  const ctx = await browser.newContext()
  const p = await ctx.newPage()
  // The server is unreachable, as in a dropped connection.
  await p.route(`${API}/**`, (route) => route.abort('connectionfailed'))
  await p.goto(`${APP}/polls/${pollId}/results`, { waitUntil: 'domcontentloaded' })
  await p.getByRole('heading').first().waitFor({ timeout: 10000 })
  const text = await p.locator('main').innerText()
  check('error: a dead server is explained in plain language',
    text.includes('could not load') || text.includes('did not answer'), text.split('\n')[0])
  check('error: a recovery action is offered', (await p.getByRole('button', { name: 'Try again' }).count()) > 0)
  check('error: no status code or stack trace is shown', !/\b(500|502|TypeError|fetch)\b/.test(text))
  await ctx.close()
}
{
  const ctx = await browser.newContext()
  const p = await ctx.newPage()
  await p.goto(`${APP}/polls/000000000000000000000000/results`, { waitUntil: 'networkidle' })
  const text = await p.locator('main').innerText()
  check('error: a missing poll is distinguished from a failure', text.includes('does not exist'))
  await ctx.close()
}

// ---- LOADING ----------------------------------------------------------------
{
  const ctx = await browser.newContext()
  const p = await ctx.newPage()
  // A slow server: the skeleton should appear.
  await p.route(`${API}/api/polls/**`, async (route) => {
    await new Promise((r) => setTimeout(r, 1200))
    await route.continue()
  })
  await p.goto(`${APP}/polls/${pollId}/results`, { waitUntil: 'commit' })
  await p.waitForTimeout(600)
  check('loading: a slow request shows a skeleton', (await p.locator('.skeleton').count()) > 0)
  await ctx.close()
}
{
  const ctx = await browser.newContext()
  const p = await ctx.newPage()
  // A fast server: nothing should flash.
  let sawSkeleton = false
  await p.goto(`${APP}/polls/${pollId}/results`, { waitUntil: 'commit' })
  for (let i = 0; i < 6; i++) {
    await p.waitForTimeout(40)
    if ((await p.locator('.skeleton').count()) > 0) sawSkeleton = true
  }
  check('loading: a fast request shows no flicker', !sawSkeleton)
  await ctx.close()
}

// ---- SUCCESS ----------------------------------------------------------------
{
  const ctx = await browser.newContext()
  const p = await ctx.newPage()
  await p.goto(`${APP}/polls/${pollId}`, { waitUntil: 'networkidle' })
  await p.getByText('The one with tests').click()
  await p.getByRole('button', { name: 'Vote' }).click()
  await p.getByText('Your vote was recorded').waitFor({ timeout: 8000 })
  check('success: confirmation is inline and specific', true)
  check('success: the results replace the ballot in the same view',
    (await p.locator('.vote-option').count()) === 0 && (await p.locator('.result').count()) === 2)
  await ctx.close()
}

// ---- KEYBOARD ONLY ----------------------------------------------------------
{
  const ctx = await browser.newContext()
  const p = await ctx.newPage()
  await p.goto(`${APP}/polls/${pollId}`, { waitUntil: 'networkidle' })

  await p.keyboard.press('Tab') // skip link
  const skip = await p.evaluate(() => document.activeElement?.textContent?.trim())
  check('keyboard: the skip link comes first', skip === 'Skip to content')

  await p.keyboard.press('Tab') // brand
  await p.keyboard.press('Tab') // radio group
  await p.keyboard.press('ArrowDown') // move within the group
  const chosen = await p.evaluate(() => document.activeElement && "checked" in document.activeElement ? document.activeElement.checked : false)
  check('keyboard: arrow keys select within the radio group', Boolean(chosen))

  const focusVisible = await p.evaluate(() => {
    const el = document.activeElement?.closest('.vote-option')
    if (!el) return false
    return getComputedStyle(el).outlineStyle !== 'none'
  })
  check('keyboard: focus is visible on the option row', focusVisible)

  await p.keyboard.press('Tab')
  const onSubmit = await p.evaluate(() => document.activeElement?.textContent?.trim())
  check('keyboard: the submit button is next in order', onSubmit === 'Vote', onSubmit)

  await p.keyboard.press('Enter')
  await p.getByText('Your vote was recorded').waitFor({ timeout: 8000 })
  check('keyboard: the whole vote can be cast without a mouse', true)
  await ctx.close()
}

// ---- REDUCED MOTION ---------------------------------------------------------
{
  const ctx = await browser.newContext({ reducedMotion: 'reduce' })
  const p = await ctx.newPage()
  await p.goto(`${APP}/polls/${pollId}/results`, { waitUntil: 'networkidle' })
  const duration = await p.locator('.result__bar').first().evaluate((el) => getComputedStyle(el).transitionDuration)
  const seconds = parseFloat(duration)
  check('reduced motion: bars snap rather than animate', seconds < 0.05, duration)
  await ctx.close()
}

// ---- DOUBLE SUBMIT ----------------------------------------------------------
{
  const ctx = await browser.newContext({ storageState: state })
  const p = await ctx.newPage()
  await p.goto(`${APP}/polls/new`, { waitUntil: 'networkidle' })
  await p.getByLabel('Question').fill('Can this be submitted twice?')
  await p.getByLabel('Option 1').fill('Yes')
  await p.getByLabel('Option 2').fill('No')
  let creates = 0
  p.on('request', (r) => { if (r.method() === 'POST' && r.url().endsWith('/api/polls')) creates += 1 })
  const button = p.getByRole('button', { name: 'Create poll' })
  await button.click()
  await button.click({ force: true, timeout: 1000 }).catch(() => {})
  await p.getByText('Your poll is live').waitFor({ timeout: 10000 })
  check('a double-tap cannot create two polls', creates === 1, `${creates} request(s)`)
  await ctx.close()
}

console.log(results.join('\n'))
console.log(results.some((r) => r.startsWith('✗')) ? '\nSOME CHECKS FAILED' : '\nall async-state, keyboard and motion checks pass')
await browser.close()
