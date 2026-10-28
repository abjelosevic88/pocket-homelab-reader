// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true seeds a few weeks of feeds, saved pages and highlights):
//
//   DATA_DIR=/tmp/reader-demo DEMO_DATA=true ADMIN_EMAIL=demo@example.com ADMIN_PASSWORD=demodemo1 ADMIN_NAME=Maya PORT=18306 ./pocket-reader
//   CDP=http://127.0.0.1:19271 BASE=http://127.0.0.1:18306 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                  # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18306'
const shots = process.env.SHOTS !== '0'
const dir = new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 3 : 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
    userAgent: phone ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' : undefined,
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  await page.goto(base + '/login')
  const res = await page.evaluate(() => fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'demo@example.com', password: 'demodemo1' }) }).then(r => r.status))
  assert.equal(res, 200, 'demo sign-in')
  return { ctx, page }
}
async function go(page, path, wait = 1500, still = true) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  // headless Chrome freezes CSS fade-ins: show everything at once
  if (still) await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
async function shot(page, name) {
  if (!shots) return
  // headless Chrome sometimes refuses the first capture after a big layout change: try again
  for (let i = 0; ; i++) {
    try { await page.screenshot({ path: dir + name + '.png' }); return } catch (e) { if (i === 3) throw e; await page.waitForTimeout(800) }
  }
}
// ---- desktop ----
{
  const { ctx, page } = await session()
  await go(page, '/')
  assert.ok((await page.content()).includes('Unread'), 'the unread list')
  const first = page.locator('.art-row').first()
  await first.click()
  await page.waitForSelector('.reader-content', { timeout: 8000 })
  await page.waitForTimeout(900)
  await shot(page, 'desktop-home')
  await go(page, '/saved')
  await page.locator('.art-row').first().click()
  await page.waitForSelector('mark.hl', { timeout: 8000 })
  await page.waitForTimeout(700)
  await shot(page, 'desktop-saved')
  await go(page, '/highlights')
  await shot(page, 'desktop-highlights')
  await go(page, '/feeds')
  await shot(page, 'desktop-feeds')
  await go(page, '/feeds/rules')
  await shot(page, 'desktop-rules')
  await go(page, '/settings/tokens')
  await shot(page, 'desktop-phone-apps')
  await go(page, '/')
  await page.keyboard.press('a')
  await page.waitForSelector('.add-modal input')
  await page.waitForTimeout(300)
  await shot(page, 'desktop-add')
  for (const p of ['/starred', '/history', '/archive', '/all', '/folder/1', '/tag/outdoors', '/search?q=bread', '/settings/reading', '/settings/feeds', '/settings/notifications', '/settings/apps', '/settings/ai', '/settings/data', '/settings/about']) await go(page, p, 500)
  await ctx.close()
}

// ---- phone ----
{
  const { ctx, page } = await session({ phone: true, dark: true })
  await go(page, '/')
  await shot(page, 'phone-home')
  await page.locator('.art-row').first().click()
  await page.waitForSelector('.reader-content', { timeout: 8000 })
  await page.waitForTimeout(800)
  await shot(page, 'phone-article')
  await go(page, '/saved')
  await shot(page, 'phone-saved')
  await ctx.close()
}

await browser.close()
if (errors.length) {
  console.error('browser errors:\n' + errors.join('\n'))
  process.exit(1)
}
console.log('ok')
