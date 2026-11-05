import assert from 'node:assert/strict';
import { mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright';

// 对 CI 中实际安装的生产程序检查页面和交互，截图写入 artifacts。
const base = process.env.OPSD_UI_URL ?? 'http://127.0.0.1:65533';
const password = process.env.OPSD_UI_PASSWORD;
assert(password, '请设置 OPSD_UI_PASSWORD');
const output = 'artifacts/ui';
mkdirSync(output, { recursive: true });
const browser = await chromium.launch();
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await context.newPage();
const errors = [];
page.on('pageerror', (error) => errors.push(error.message));
const capture = async (name) => {
  await page.waitForFunction(() => !document.querySelector('[role="dialog"][data-starting-style], [role="alertdialog"][data-starting-style]'));
  await page.evaluate(async () => {
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    await Promise.all(document.getAnimations().filter((a) => a.effect?.getComputedTiming().iterations !== Infinity).map((a) => a.finished.catch(() => {})));
  });
  await page.screenshot({ path: `${output}/${name}.png`, fullPage: true, animations: 'disabled' });
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${name} 页面横向溢出`);
};
try {
  await page.goto(`${base}/signin`);
  await page.evaluate(() => document.fonts.ready);
  await page.getByRole('heading', { name: '登录', exact: true }).waitFor();
  await capture('login');
  await page.getByLabel('账号', { exact: true }).fill('ui-check');
  await page.getByLabel('密码', { exact: true }).fill(password);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.waitForURL(`${base}/`);
  const nodes = await (await context.request.get(`${base}/api/v1/nodes`)).json();
  const id = nodes.items[0].id;
  const pages = [['overview', '/'], ['nodes', '/nodes'], ['detail', `/nodes/${id}`], ['firewall', `/firewall?node=${id}`], ['firewall-settings', '/firewall/settings'], ['account', '/settings/account'], ['sessions', '/settings/sessions'], ['appearance', '/settings/appearance'], ['version', '/settings/version']];
  for (const theme of ['light', 'dark']) {
    await page.goto(`${base}/settings/appearance`);
    await page.getByRole('radio', { name: theme === 'light' ? '浅色' : '深色', exact: true }).click();
    for (const width of [320, 768, 1440]) {
      await page.setViewportSize({ width, height: 1000 });
      for (const [name, path] of pages) {
        await page.goto(base + path);
        await page.locator('main h1').waitFor({ state: 'attached' });
        await page.locator('main [aria-busy="true"]').first().waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});
        await capture(`${theme}-${width}-${name}`);
      }
    }
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/nodes`);
  await page.getByRole('button', { name: '添加节点', exact: true }).click();
  await page.getByRole('dialog').waitFor();
  await capture('add-node');
  await page.getByLabel('名称（可选）', { exact: true }).fill("界面验收'节点");
  await page.getByRole('button', { name: '生成安装命令', exact: true }).click();
  const command = page.getByLabel('一次性安装命令', { exact: true });
  await command.waitFor();
  const shell = await command.inputValue();
  assert(shell.startsWith('sh -c '));
  assert(shell.includes('https://github.com/jkfujr/opsd/releases/download/v'));
  assert(shell.includes('/install.sh'));
  await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true }));
  await page.getByRole('button', { name: '复制命令', exact: true }).click();
  await page.getByRole('button', { name: '已复制', exact: true }).waitFor();
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).last().click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  await page.goto(`${base}/firewall/settings`);
  await page.getByRole('switch', { name: '集群互信' }).click();
  await page.getByRole('alertdialog').waitFor();
  await capture('trust-confirm');
  await page.getByRole('button', { name: '取消', exact: true }).click();
  assert.equal(await page.getByRole('switch', { name: '集群互信' }).isChecked(), false);
  await page.getByRole('switch', { name: '集群互信' }).click();
  await page.getByRole('button', { name: '开启互信', exact: true }).click();
  await page.getByRole('alertdialog').waitFor({ state: 'hidden' });
  assert.equal(await page.getByRole('switch', { name: '集群互信' }).isChecked(), true);
  await page.goto(`${base}/firewall?node=${id}&type=trust`);
  await page.getByRole('tab', { name: /^互信规则/ }).waitFor();
  await capture('trust-rules');
  await page.goto(`${base}/firewall/settings`);
  await page.getByRole('switch', { name: '集群互信' }).click();
  await page.getByRole('button', { name: '关闭互信', exact: true }).click();
  await page.getByRole('alertdialog').waitFor({ state: 'hidden' });
  assert.equal(await page.getByRole('switch', { name: '集群互信' }).isChecked(), false);
  await page.goto(`${base}/settings/account`);
  await page.getByLabel('账号', { exact: true }).fill('changed');
  await page.getByRole('navigation', { name: '主导航' }).getByRole('link', { name: '节点', exact: true }).click();
  await page.getByRole('alertdialog').waitFor();
  await capture('unsaved-confirm');
  await page.getByRole('button', { name: '取消', exact: true }).click();
  assert.equal(await page.getByLabel('账号', { exact: true }).inputValue(), 'changed');
  await page.getByRole('navigation', { name: '主导航' }).getByRole('link', { name: '节点', exact: true }).click();
  await page.getByRole('button', { name: '丢弃修改', exact: true }).click();
  await page.waitForURL(`${base}/nodes`);
  await page.goto(`${base}/settings/appearance`);
  await page.evaluate(() => { document.documentElement.style.zoom = '200%'; });
  await capture('zoom-200');
  await page.evaluate(() => { document.documentElement.style.zoom = ''; });
  await page.setViewportSize({ width: 320, height: 800 });
  await page.getByRole('button', { name: '打开菜单' }).click();
  await page.getByRole('dialog').waitFor();
  await capture('mobile-menu');
  await page.keyboard.press('Escape');
  assert.deepEqual(errors, [], '浏览器运行错误');
  writeFileSync(`${output}/result.json`, JSON.stringify({ pages: pages.length, themes: 2, widths: [320, 768, 1440], errors }, null, 2));
  console.log('界面验收通过：9 页面、深浅主题、3 种宽度、浮层与未提交修改。');
} catch (error) {
  await page.screenshot({ path: `${output}/failure.png`, fullPage: true, mask: [page.getByLabel('一次性安装命令', { exact: true })] }).catch(() => {});
  writeFileSync(`${output}/failure.txt`, `${page.url()}\n${error.stack}\n${errors.join('\n')}`);
  throw error;
} finally { await browser.close(); }
