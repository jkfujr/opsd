// 截图工具：用本机 Edge 打开开发服务器，按页面清单出图。
// node scripts/shoot.mjs <outDir> [routes...]
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const out = process.argv[2] ?? '.impeccable/review';
const routes = process.argv.slice(3);
const base = process.env.BASE ?? 'http://127.0.0.1:5290';
const list = routes.length
  ? routes
  : ['/', '/nodes', '/containers', '/databases', '/firewall', '/settings'];

mkdirSync(out, { recursive: true });

const browser = await chromium.launch({ channel: 'msedge' });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });

for (const route of list) {
  const name = route === '/' ? 'overview' : route.replace(/^\//, '').replace(/[/?=&]/g, '-');
  await page.goto(base + route, { waitUntil: 'networkidle' });
  await page.waitForTimeout(700);
  await page.screenshot({ path: `${out}/${name}.png`, fullPage: true });
  console.log('shot', route, '->', `${out}/${name}.png`);
}

await browser.close();
