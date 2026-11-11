// 主题与窄屏截图：node scripts/shoot2.mjs <outDir>
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const out = process.argv[2] ?? '.impeccable/review';
mkdirSync(out, { recursive: true });
const base = process.env.BASE ?? 'http://127.0.0.1:5290';
const browser = await chromium.launch({ channel: 'msedge' });

const routes = ['/', '/nodes', '/containers', '/firewall', '/databases/db-sh'];

const dark = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: 'dark' });
for (const route of routes) {
  await dark.goto(base + route, { waitUntil: 'networkidle' });
  await dark.waitForTimeout(500);
  const name = route === '/' ? 'overview' : route.replace(/^\//, '').replace(/\//g, '-');
  await dark.screenshot({ path: `${out}/dark-${name}.png`, fullPage: true });
  console.log('dark', route);
}
await dark.close();

const mobile = await browser.newPage({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true });
for (const route of ['/', '/nodes', '/firewall']) {
  await mobile.goto(base + route, { waitUntil: 'networkidle' });
  await mobile.waitForTimeout(500);
  const name = route === '/' ? 'overview' : route.replace(/^\//, '');
  await mobile.screenshot({ path: `${out}/mobile-${name}.png`, fullPage: true });
  console.log('mobile', route);
}
await mobile.close();
await browser.close();
