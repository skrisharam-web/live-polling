import { chromium } from 'playwright'

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
const pollId = process.env.POLL_ID
const browser = await launch()

/** WCAG relative luminance and contrast ratio, computed in the page. */
const CONTRAST_FN = `
  const lum = (rgb) => {
    const [r, g, b] = rgb.map((v) => {
      const c = v / 255
      return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4)
    })
    return 0.2126 * r + 0.7152 * g + 0.0722 * b
  }
  const parse = (value) => (value.match(/\\d+(\\.\\d+)?/g) || []).slice(0, 3).map(Number)
  const bgOf = (el) => {
    let node = el
    while (node) {
      const bg = getComputedStyle(node).backgroundColor
      const parsed = parse(bg)
      if (bg !== 'rgba(0, 0, 0, 0)' && bg !== 'transparent' && parsed.length === 3) return parsed
      node = node.parentElement
    }
    return [255, 255, 255]
  }
  const ratio = (a, b) => {
    const [l1, l2] = [lum(a), lum(b)].sort((x, y) => y - x)
    return (l1 + 0.05) / (l2 + 0.05)
  }
`

const screens = [
  ['landing', '/', {}],
  ['login', '/login', {}],
  ['register', '/register', {}],
  ['dashboard', '/dashboard', { storageState: state }],
  ['create', '/polls/new', { storageState: state }],
  ['ballot', `/polls/${pollId}`, {}],
  ['results', `/polls/${pollId}/results`, {}],
  ['manage', `/polls/${pollId}/manage`, { storageState: state }],
  ['not-found', '/nope', {}],
]

let failures = 0
for (const [name, path, extra] of screens) {
  for (const theme of ['light', 'dark']) {
    for (const width of [320, 768, 1440]) {
      const ctx = await browser.newContext({
        ...extra,
        colorScheme: theme,
        viewport: { width, height: 800 },
      })
      const page = await ctx.newPage()
      await page.goto(APP + path, { waitUntil: 'networkidle' })
      await page.waitForTimeout(200)

      const report = await page.evaluate(`(() => {
        ${CONTRAST_FN}
        const problems = []
        document.querySelectorAll('h1,h2,h3,p,span,label,button,a,legend,li').forEach((el) => {
          const text = (el.textContent || '').trim()
          if (!text || el.children.length > 0) return
          const style = getComputedStyle(el)
          if (style.visibility === 'hidden' || style.display === 'none') return
          if (el.closest('.sr-only') || el.classList.contains('sr-only')) return
          const size = parseFloat(style.fontSize)
          const weight = Number(style.fontWeight) || 400
          const isLarge = size >= 24 || (size >= 18.66 && weight >= 700)
          const required = isLarge ? 3 : 4.5
          const r = ratio(parse(style.color), bgOf(el))
          if (r < required) problems.push(text.slice(0, 28) + ' :: ' + r.toFixed(2) + ' < ' + required)
        })
        return {
          contrast: problems,
          overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
          h1Count: document.querySelectorAll('h1').length,
          mainCount: document.querySelectorAll('main').length,
          skipTarget: Boolean(document.querySelector('#main')),
          title: document.title,
        }
      })()`)

      const issues = []
      if (report.contrast.length) issues.push(`contrast: ${report.contrast.join(' | ')}`)
      if (report.overflow) issues.push('horizontal overflow')
      if (report.h1Count !== 1) issues.push(`h1 count = ${report.h1Count}`)
      if (!report.skipTarget) issues.push('no #main for the skip link')
      if (!report.title || report.title === 'Pulse') issues.push(`title = "${report.title}"`)

      if (issues.length) {
        failures += 1
        console.log(`✗ ${name} @ ${theme} ${width}px — ${issues.join('; ')}`)
      }
      await ctx.close()
    }
  }
}

console.log(failures === 0 ? '\nall screens pass: contrast, overflow, one h1, skip target, unique title' : `\n${failures} screen/theme/width combinations have issues`)
await browser.close()
