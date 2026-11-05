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
  const session = await (await context.request.get(`${base}/api/v1/auth/session`)).json();
  const api = async (path, method = 'GET', data) => {
    const response = await context.request.fetch(`${base}/api/v1${path}`, { method, data, headers: { Origin: base, 'X-CSRF-Token': session.csrf_token, 'Idempotency-Key': crypto.randomUUID() } });
    const body = await response.json();
    assert(response.ok(), `${path}：${response.status()} ${body.message ?? ''}`);
    return body;
  };
  const waitFor = async (read, check, message) => {
    for (let attempt = 0; attempt < 120; attempt++) {
      const value = await read();
      if (check(value)) return value;
      await page.waitForTimeout(500);
    }
    throw new Error(message);
  };
  const firewallPath = `/nodes/${id}/firewall`;
  const addRule = async (rule) => {
    const current = await api(firewallPath);
    const saved = await api(firewallPath, 'PATCH', { expected_revision: current.revision, change: { kind: 'add_rule', rule } });
    await waitFor(async () => {
      const operation = await api(`/operations/${saved.operation_id}`);
      if (operation.state === 'awaiting_confirmation') await api(`/operations/${operation.id}/confirm`, 'POST', { expected_revision: operation.revision });
      return api(firewallPath);
    }, (value) => value.state === 'in_sync', '测试规则未完成应用');
  };
  await waitFor(() => api(firewallPath), (value) => value.backend === 'ufw' && value.state === 'in_sync', '防火墙未完成初始导入');
  const publicValue = await api(`/nodes/${id}/public-addresses`);
  await api(`/nodes/${id}/public-addresses`, 'PATCH', { expected_revision: publicValue.revision, ipv4: { mode: 'manual', addresses: ['8.8.8.8'] }, ipv6: { mode: 'manual', addresses: ['2606:4700:4700::1111'] } });
  await addRule({ family: 'ipv4', source: '8.8.8.8/32', protocol: 'any', action: 'allow' });
  await addRule({ family: 'ipv4', source: '100.64.0.0/10', protocol: 'any', action: 'allow' });
  await addRule({ family: 'ipv4', source: '0.0.0.0/0', protocol: 'tcp', port: '22', action: 'allow' });
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
    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.goto(`${base}/firewall?node=${id}&type=ip`);
    await page.getByRole('button', { name: '更多', exact: true }).first().waitFor();
    const controls = await Promise.all([page.getByLabel('节点', { exact: true }), page.getByLabel('地址族', { exact: true }), page.getByRole('button', { name: '刷新', exact: true }), page.getByRole('link', { name: '设置', exact: true }).last()].map((locator) => locator.boundingBox()));
    assert(controls.every((box) => box && Math.abs(box.y - controls[0].y) <= 1 && Math.abs(box.height - 40) <= 1), '筛选与操作控件未对齐');
    assert(controls[0].width <= 384 && controls[1].width <= 192, '筛选框过度拉伸');
    assert.equal(await page.getByRole('columnheader', { name: '备注', exact: true }).count(), 0);
    assert.equal(await page.getByText('默认入站', { exact: true }).count(), 0);
    assert.equal(await page.getByText('已启用', { exact: true }).count(), 0);
    await capture(`${theme}-1920-firewall`);
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole('button', { name: '更多', exact: true }).first().press('ArrowDown');
  await page.getByRole('menu').waitFor();
  await page.getByRole('menuitem', { name: '编辑', exact: true }).press('Enter');
  await page.getByRole('dialog').waitFor();
  await capture('firewall-edit');
  await page.keyboard.press('Escape');
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true }));
  await page.getByRole('button', { name: '复制 8.8.8.8/32', exact: true }).click();
  await page.getByRole('status').filter({ hasText: '已复制' }).waitFor();
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
  const migrated = await waitFor(() => api(firewallPath), (value) => value.migration?.state === 'in_sync', '普通 IP 规则未转交互信');
  assert(!migrated.rules.some((rule) => rule.spec.source === '8.8.8.8/32'));
  assert(migrated.rules.some((rule) => rule.spec.source === '100.64.0.0/10'));
  assert(migrated.rules.some((rule) => rule.spec.port === '22'));
  const trustPath = `/nodes/${id}/firewall/peer-trust`;
  const trust = await api(trustPath);
  assert.equal(trust.rules.filter((rule) => rule.address).length, 2);
  assert(trust.rules.every((rule) => rule.members.some((member) => member.id === id)));
  await page.goto(`${base}/firewall?node=${id}&type=trust`);
  await page.getByRole('tab', { name: '互信规则 2', exact: true }).waitFor();
  for (const width of [320, 768, 1440, 1920]) {
    await page.setViewportSize({ width, height: 1000 });
    await capture(`trust-rules-${width}`);
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  for (const state of ['pending', 'failed']) {
    await page.route(`**/api/v1${trustPath}`, (route) => route.fulfill({ json: { ...trust, migration: { state, operation_id: migrated.migration.operation_id, ...(state === 'failed' ? { error: { code: 'conflict', message: '测试迁移失败' } } : {}) } } }));
    await page.reload();
    await page.getByText(state === 'failed' ? '迁移失败' : '迁移中', { exact: true }).waitFor();
    assert.equal(await page.getByRole('button', { name: '查看操作', exact: true }).count(), 1);
    if (state === 'failed') await page.getByRole('button', { name: '重试', exact: true }).waitFor();
    await capture(`trust-migration-${state}`);
    await page.unroute(`**/api/v1${trustPath}`);
  }
  await page.goto(`${base}/firewall/settings`);
  await page.getByRole('switch', { name: '集群互信' }).click();
  await page.getByRole('button', { name: '关闭互信', exact: true }).click();
  await page.getByRole('alertdialog').waitFor({ state: 'hidden' });
  assert.equal(await page.getByRole('switch', { name: '集群互信' }).isChecked(), false);
  await waitFor(() => api(trustPath), (value) => value.state === 'disabled' && value.rules.length === 0, '关闭后互信规则未撤除');
  assert(!(await api(firewallPath)).rules.some((rule) => rule.spec.source === '8.8.8.8/32'));
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
  writeFileSync(`${output}/result.json`, JSON.stringify({ pages: pages.length, themes: 2, widths: [320, 768, 1440], firewallWidths: [320, 768, 1440, 1920], trustMigration: true, errors }, null, 2));
  console.log('界面验收通过：9 页面、深浅主题、防火墙 4 种宽度、互信迁移、浮层与未提交修改。');
} catch (error) {
  await page.screenshot({ path: `${output}/failure.png`, fullPage: true, mask: [page.getByLabel('一次性安装命令', { exact: true })] }).catch(() => {});
  writeFileSync(`${output}/failure.txt`, `${page.url()}\n${error.stack}\n${errors.join('\n')}`);
  throw error;
} finally { await browser.close(); }
