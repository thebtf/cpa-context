// Opens the stand's dashboard in a real browser, types the management key, and
// saves a screenshot plus whatever the console reported. Used to check the
// ported UI renders instead of trusting the build.
import { chromium } from 'playwright-core'
import { mkdir } from 'node:fs/promises'

const url = process.argv[2]
const key = process.argv[3]
const out = process.argv[4] ?? 'shot.png'

if (!url || !key) {
  throw new Error('usage: node shoot.mjs <url> <management-key> [out.png]')
}

const browser = await chromium.launch({
  executablePath: process.env.CHROME ?? undefined,
  channel: 'chrome',
})
const page = await browser.newPage({ viewport: { width: 1500, height: 950 } })

const messages = []
page.on('console', m => messages.push(`[${m.type()}] ${m.text()}`))
page.on('pageerror', e => messages.push(`[pageerror] ${e.message}`))
page.on('response', r => { if (r.status() >= 400) messages.push(`[${r.status()}] ${r.url()}`) })

await page.goto(url, { waitUntil: 'networkidle' })
await page.fill('#key', key)
await page.click('#refresh')
await page.waitForTimeout(2500)

await mkdir('shots', { recursive: true })
await page.screenshot({ path: `shots/${out}`, fullPage: false })

const text = await page.evaluate(() => document.body.innerText.slice(0, 1200))
console.log('--- console ---')
console.log(messages.slice(0, 25).join('\n') || '(clean)')
console.log('--- visible text ---')
console.log(text)

await browser.close()
