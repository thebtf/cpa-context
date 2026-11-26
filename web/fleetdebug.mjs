import { chromium } from 'playwright-core'

const [url, key] = process.argv.slice(2)
const browser = await chromium.launch({ channel: 'chrome' })
const page = await browser.newPage({ viewport: { width: 1500, height: 950 } })
await page.goto(url, { waitUntil: 'networkidle' })
await page.fill('#key', key)
await page.click('#refresh')
await page.waitForTimeout(2000)

const buttons = await page.$$('.cpa-port-tabs button')
await buttons[1].click()
await page.waitForTimeout(1800)

// Read what the fleet cards actually received, through the DOM it renders.
const report = await page.evaluate(() => {
  const text = document.body.innerText.replace(/\s+/g, ' ')
  const root = document.querySelector('.lc-root')
  return {
    hasRoot: root !== null,
    classes: root === null ? [] : [...root.querySelectorAll('[class]')].slice(0, 30).map(e => e.className).filter(c => typeof c === 'string'),
    excerpt: text.slice(0, 700),
  }
})
console.log(JSON.stringify(report, null, 1).slice(0, 2500))
await page.screenshot({ path: 'shots/fleet-debug.png' })
await browser.close()
