// Checks that a dashboard already open in the browser picks up traffic that
// arrives afterwards, without a reload. A page that only renders what existed
// when it loaded is indistinguishable from a broken one to the operator.
import { chromium } from 'playwright-core'

const [url, key, cpaBase, clientKey] = process.argv.slice(2)
if (!url || !key) throw new Error('usage: node live.mjs <dashboard-url> <key> [cpa-base] [client-key]')

const browser = await chromium.launch({ channel: 'chrome' })
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } })
const errors = []
page.on('pageerror', e => errors.push(e.message))

await page.goto(url, { waitUntil: 'networkidle' })
await page.fill('#key', key)
await page.click('#refresh')
await page.waitForTimeout(2500)

const before = await page.evaluate(() => document.body.innerText.match(/(\d+)\s*req/)?.[1] ?? '0')
const beforeReqs = await page.evaluate(() => {
  const card = [...document.querySelectorAll('.cpa-port-row-meta')][0]
  return card?.innerText ?? ''
})
console.log(`before: ${beforeReqs}`)

// Send more traffic while the page stays open.
if (cpaBase && clientKey) {
  for (let i = 0; i < 4; i++) {
    await fetch(`${cpaBase}/v1/chat/completions`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', Authorization: `Bearer ${clientKey}`, 'x-session-id': 'stand-session-1' },
      body: JSON.stringify({
        model: 'claude-sonnet-4-5', max_tokens: 512,
        messages: [{ role: 'user', content: `live probe ${i}` }],
      }),
    }).then(r => r.text())
  }
}

// The page has to refresh itself. Refreshing by hand is what the button is for,
// so this waits without touching the page.
let after = beforeReqs
for (let attempt = 0; attempt < 20; attempt++) {
  await page.waitForTimeout(1500)
  after = await page.evaluate(() => document.querySelector('.cpa-port-row-meta')?.innerText ?? '')
  if (after !== beforeReqs) break
}

console.log(`after:  ${after}`)
console.log(`changed: ${after !== beforeReqs}`)
console.log(`errors: ${errors.length === 0 ? '(clean)' : errors.slice(0, 3).join(' | ')}`)

await browser.close()
