import { chromium } from 'playwright-core'

const [url, key] = process.argv.slice(2)
const browser = await chromium.launch({ channel: 'chrome' })
const page = await browser.newPage({ viewport: { width: 1500, height: 950 } })
const errors = []
page.on('pageerror', e => errors.push(`[pageerror] ${e.message}`))
page.on('response', r => { if (r.status() >= 400) errors.push(`[${r.status()}] ${r.url().slice(0, 120)}`) })

await page.goto(url, { waitUntil: 'networkidle' })
await page.fill('#key', key)
await page.click('#refresh')
await page.waitForTimeout(2500)

for (const [index, name] of ['context', 'fleet', 'insights'].entries()) {
  const buttons = await page.$$('.cpa-port-tabs button')
  if (buttons[index]) await buttons[index].click()
  await page.waitForTimeout(1800)
  await page.screenshot({ path: `shots/tab-${name}.png` })
  const text = await page.evaluate(() => document.body.innerText.replace(/\s+/g, ' ').slice(0, 500))
  console.log(`--- ${name} ---`)
  console.log(text)
}

console.log('--- errors ---')
console.log([...new Set(errors)].slice(0, 12).join('\n') || '(clean)')
await browser.close()
