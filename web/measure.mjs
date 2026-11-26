import { chromium } from 'playwright-core'

const [url, key] = process.argv.slice(2)
const browser = await chromium.launch({ channel: 'chrome' })
const page = await browser.newPage({ viewport: { width: 1500, height: 950 } })
await page.goto(url, { waitUntil: 'networkidle' })
await page.fill('#key', key)
await page.click('#refresh')
await page.waitForTimeout(2500)

const report = await page.evaluate(() => {
  const box = (el) => {
    if (el === null) return null
    const r = el.getBoundingClientRect()
    const cs = getComputedStyle(el)
    return {
      w: Math.round(r.width), h: Math.round(r.height),
      x: Math.round(r.x), y: Math.round(r.y),
      display: cs.display, position: cs.position, flex: cs.flex, width: cs.width,
    }
  }
  const out = {}
  out.view = box(document.querySelector('.cpa-port-view'))
  out.root = box(document.querySelector('.lc-root'))
  out.firstCols = box(document.querySelector('.lc-cols'))
  const cards = [...document.querySelectorAll('.lc-card')].slice(0, 6)
  out.cards = cards.map(c => ({ cls: c.className.slice(0, 40), ...box(c) }))
  return out
})
console.log(JSON.stringify(report, null, 2))
await browser.close()
