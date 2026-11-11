// 交互态截图：对话框、抽屉、日志。
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const out = process.argv[2] ?? '.impeccable/review';
const base = process.env.BASE ?? 'http://127.0.0.1:5290';
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ channel: 'msedge' });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });

const shot = async (name) => {
  await page.waitForTimeout(400);
  await page.screenshot({ path: `${out}/${name}.png` });
  console.log('shot', name);
};

// 添加节点
await page.goto(`${base}/nodes`, { waitUntil: 'networkidle' });
await page.getByRole('button', { name: '添加节点' }).click();
await page.getByLabel('节点名称').fill('hk-03');
await page.getByLabel('地域').fill('香港');
await shot('flow-add-node');
await page.getByRole('button', { name: '生成安装命令' }).click();
await page.waitForTimeout(600);
await shot('flow-add-node-command');
await page.getByRole('button', { name: '完成' }).click();

// 操作队列
await page.goto(`${base}/`, { waitUntil: 'networkidle' });
await page.getByRole('button', { name: /操作队列/ }).first().click();
await page.waitForTimeout(500);
await shot('flow-queue');
const first = page.locator('.drawer .docket').first();
if (await first.count()) {
  await first.click();
  await page.waitForTimeout(600);
  await shot('flow-queue-detail');
}
await page.keyboard.press('Escape');

// 容器日志
await page.goto(`${base}/containers`, { waitUntil: 'networkidle' });
await page.getByRole('button', { name: /日志/ }).first().click();
await page.waitForTimeout(600);
await shot('flow-logs');
await page.keyboard.press('Escape');

// 防火墙规则编辑
await page.goto(`${base}/firewall?node=n-sh-01`, { waitUntil: 'networkidle' });
await page.getByRole('button', { name: '添加规则' }).click();
await page.waitForTimeout(400);
await shot('flow-rule');

await browser.close();

