import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const { chromium } = createRequire(new URL('../web/package.json', import.meta.url))('playwright-core');
const [dashboard, key, root, ...children] = process.argv.slice(2);
assert(dashboard && key && root && children.length > 0, 'expected: dashboard-url key root-id child-id ...');
const ids = [root, ...children];
const url = new URL(dashboard);
assert.equal(url.hostname, '127.0.0.1', 'this fixture only opens its local CPA');
const browser = await chromium.launch({ channel: 'chrome', headless: true });
try {
  const errors = [];
  for (const selected of [root, children[0]]) {
    const page = await browser.newPage();
    page.on('pageerror', error => errors.push(error.message));
    url.searchParams.set('session', selected);
    await page.goto(url.href);
    await page.locator('#key').fill(key);
    await page.locator('#refresh').click();
    await page.locator('.cpa-port-tabs button').nth(1).click();
    for (const id of ids) await page.locator(`.lc-agent-card[data-agent="${id}"]`).waitFor();
    assert.deepEqual((await page.locator('.lc-agent-card').evaluateAll(nodes => nodes.map(n => n.dataset.agent))).sort(), [...ids].sort());
    assert.equal(await page.locator('path.lc-agents-link').count(), ids.length - 1);
    await page.close();
  }
  assert.deepEqual(errors, []);
  console.log(`Fleet DOM: ${ids.length} cards, ${ids.length - 1} edges from root and child`);
} finally {
  await browser.close();
}
