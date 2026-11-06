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
const firewallWidths = [320, 639, 640, 768, 1000, 1440, 1920];
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
const checkFirewallHeader = async () => {
  const width = page.viewportSize().width;
  const [node, family, filters, row, status, refresh, settings] = await Promise.all([
    page.getByLabel('节点', { exact: true }), page.getByLabel('地址族', { exact: true }),
    page.locator('.firewall-filters'), page.locator('.firewall-status-row'), page.locator('.firewall-status'),
    page.getByRole('button', { name: '刷新', exact: true }), page.getByRole('link', { name: '设置', exact: true }).last(),
  ].map((locator) => locator.boundingBox()));
  assert(node && family && filters && row && status && refresh && settings, `${width}px 顶部控件缺失`);
  const near = (actual, expected, message) => assert(Math.abs(actual - expected) <= 1, `${width}px ${message}：${actual} / ${expected}`);
  for (const box of [node, family, refresh, settings]) near(box.height, 40, '控件高度');
  assert(node.width <= 384 && family.width <= 192, `${width}px 筛选框过度拉伸`);
  near(refresh.y, settings.y, '按钮未同排');
  near(row.y - filters.y - filters.height, 16, '两层之间留白');
  near(row.y + row.height - Math.max(status.y + status.height, refresh.y + refresh.height), 13, '分隔线前留白');
  if (width >= 640) {
    near(node.y, family.y, '筛选框未同排');
    near(status.y + status.height / 2, refresh.y + refresh.height / 2, '状态与操作未垂直居中');
    near(settings.x + settings.width, row.x + row.width, '操作未靠右');
  } else {
    assert(family.y >= node.y + node.height, `${width}px 筛选框未纵向排列`);
    near(refresh.x, status.x, '状态与操作未左对齐');
    near(refresh.y - status.y - status.height, 12, '状态与操作换行间距');
  }
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
  assert.equal(nodes.items[0].name, 'ui-check', '网络修改或重复安装改变了节点名称');
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
  const firewall = await api(firewallPath);
  const pages = [['overview', '/'], ['nodes', '/nodes'], ['detail', `/nodes/${id}`], ['containers', '/containers'], ['firewall', `/firewall?node=${id}`], ['firewall-settings', '/firewall/settings'], ['account', '/settings/account'], ['sessions', '/settings/sessions'], ['appearance', '/settings/appearance'], ['version', '/settings/version']];
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
    for (const width of firewallWidths) {
      await page.setViewportSize({ width, height: 1000 });
      await page.goto(`${base}/firewall?node=${id}&type=ip`);
      await page.getByRole('button', { name: '更多', exact: true }).first().waitFor();
      await checkFirewallHeader();
      await capture(`${theme}-${width}-firewall-header`);
    }
    assert.equal(await page.getByRole('columnheader', { name: '备注', exact: true }).count(), 0);
    assert.equal(await page.getByText('默认入站', { exact: true }).count(), 0);
    assert.equal(await page.getByText('已启用', { exact: true }).count(), 0);

    const longName = 'C091-GreenCloud-JP-TYO-长节点名称-'.repeat(3);
    await page.route('**/api/v1/nodes', (route) => route.fulfill({ json: { ...nodes, items: nodes.items.map((node) => node.id === id ? { ...node, name: longName } : node) } }));
    await page.setViewportSize({ width: 1000, height: 1000 });
    await page.reload();
    await page.getByLabel('节点', { exact: true }).getByText(longName, { exact: true }).waitFor();
    await checkFirewallHeader();
    await capture(`${theme}-1000-firewall-long-name`);
    await page.unroute('**/api/v1/nodes');

    await page.route(`**/api/v1${firewallPath}`, (route) => route.fulfill({ json: { ...firewall, service_active: false } }));
    await page.setViewportSize({ width: 320, height: 1000 });
    await page.reload();
    await page.getByText('已保存', { exact: true }).waitFor();
    const [backend, checked] = await Promise.all([page.locator('.firewall-status').getByText('UFW', { exact: true }), page.locator('.firewall-status .tabular-nums')].map((locator) => locator.boundingBox()));
    assert(backend && checked && checked.y >= backend.y + backend.height, '窄屏长状态未换行');
    await checkFirewallHeader();
    await capture(`${theme}-320-firewall-status-wrap`);
    await page.unroute(`**/api/v1${firewallPath}`);

    let release;
    const pending = new Promise((resolve) => { release = resolve; });
    await page.route(`**/api/v1${firewallPath}`, async (route) => { await pending; await route.fulfill({ json: firewall }); });
    await page.setViewportSize({ width: 1000, height: 1000 });
    try {
      await page.reload({ waitUntil: 'domcontentloaded' });
      await page.locator('.firewall-status [data-slot="skeleton"]').waitFor();
      await checkFirewallHeader();
      await capture(`${theme}-1000-firewall-loading`);
    } finally { release(); await page.unrouteAll({ behavior: 'wait' }); }
    await page.locator('.firewall-status').getByText('UFW', { exact: true }).waitFor();

    await page.route(`**/api/v1${firewallPath}`, (route) => route.fulfill({ status: 503, json: { code: 'dependency_unavailable', message: '防火墙读取失败' } }));
    await page.reload();
    const alert = page.getByRole('alert').filter({ hasText: '防火墙读取失败' });
    await alert.waitFor();
    await checkFirewallHeader();
    const [row, failure] = await Promise.all([page.locator('.firewall-status-row'), alert].map((locator) => locator.boundingBox()));
    assert(row && failure && failure.y >= row.y + row.height, '错误提示未独立占行');
    await capture(`${theme}-1000-firewall-error`);
    await page.unroute(`**/api/v1${firewallPath}`);
    await page.reload();
    await page.getByRole('button', { name: '更多', exact: true }).first().waitFor();
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
  assert(shell.startsWith('curl -fsSL '));
  assert(shell.includes('| sudo sh -s -- -e '));
  assert(!shell.includes('--expected-version'));
  assert(shell.includes("-n '界面验收'\\''节点'"));
  assert(shell.includes('https://github.com/jkfujr/opsd/releases/download/v'));
  assert(shell.includes('/install.sh'));
  await page.getByLabel('下载工具', { exact: true }).click();
  await page.getByRole('option', { name: 'wget', exact: true }).click();
  assert((await command.inputValue()).startsWith('wget -qO- '));
  await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true }));
  await page.getByRole('button', { name: '复制命令', exact: true }).click();
  await page.getByRole('button', { name: '已复制', exact: true }).waitFor();
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).last().click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/containers`);
  const containerList = await api('/containers');
  const nativeContainer = containerList.items.find((item) => item.name === 'opsd-ui-container');
  assert(nativeContainer, '实际 Docker 容器未显示');
  const containerPath = `/nodes/${nativeContainer.node_id}/containers/${nativeContainer.container_id}`;
  const containerRow = page.getByRole('row').filter({ has: page.getByText('opsd-ui-container', { exact: true }) });
  await containerRow.getByRole('button', { name: '查看', exact: true }).click();
  await page.getByRole('heading', { name: '容器详情', exact: true }).waitFor();
  await page.getByText(nativeContainer.container_id, { exact: true }).waitFor();
  await capture('container-detail');
  await page.getByRole('button', { name: '关闭容器详情', exact: true }).click();
  await containerRow.getByRole('button', { name: '更多', exact: true }).click();
  await page.getByRole('menuitem', { name: '停止', exact: true }).click();
  await page.getByRole('alertdialog').waitFor();
  await capture('container-stop');
  await page.getByRole('alertdialog').getByRole('button', { name: '停止', exact: true }).click();
  await waitFor(() => api('/containers'), (value) => value.items.some((item) => item.container_id === nativeContainer.container_id && item.state === 'exited'), '容器停止未读回');
  await page.getByRole('button', { name: '关闭操作', exact: true }).click();
  await containerRow.getByRole('button', { name: '更多', exact: true }).click();
  await page.getByRole('menuitem', { name: '启动', exact: true }).click();
  await waitFor(() => api('/containers'), (value) => value.items.some((item) => item.container_id === nativeContainer.container_id && item.state === 'running'), '容器启动未读回');
  await page.getByRole('button', { name: '关闭操作', exact: true }).click();
  let restartRequests = 0;
  const actionPattern = '**/api/v1/nodes/*/containers/*/actions';
  await page.route(actionPattern, async (route) => { restartRequests++; const response = await route.fetch(); assert.equal(response.status(), 202); await route.abort('failed'); });
  await containerRow.getByRole('button', { name: '更多', exact: true }).click();
  await page.getByRole('menuitem', { name: '重启', exact: true }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: '重启', exact: true }).click();
  await page.getByRole('button', { name: '查询原操作', exact: true }).waitFor();
  await capture('container-unknown-response');
  await page.getByRole('button', { name: '查询原操作', exact: true }).click();
  await page.getByRole('button', { name: '关闭操作', exact: true }).waitFor();
  assert.equal(restartRequests, 1, '应答丢失后重新发送了容器动作');
  await page.unroute(actionPattern);
  await page.getByRole('button', { name: '关闭操作', exact: true }).click();
  const nativeDetail = await api(`${containerPath}?engine_id=${encodeURIComponent(nativeContainer.engine_id)}`);
  assert.equal(nativeDetail.state, 'running');
  let logRequests = 0;
  const countLogs = (request) => { if (request.url().includes(`${containerPath}/logs`)) logRequests++; };
  page.on('request', countLogs);
  await containerRow.getByRole('button', { name: '日志', exact: true }).click();
  await page.locator('pre[aria-label="容器日志"]').filter({ hasText: '界面验收容器输出' }).waitFor();
  await capture('container-logs');
  await page.getByRole('switch', { name: '自动刷新', exact: true }).click();
  let previousLogRequests = logRequests;
  await page.waitForTimeout(2300);
  assert.equal(logRequests, previousLogRequests, '暂停日志后继续轮询');
  await page.getByRole('switch', { name: '自动刷新', exact: true }).click();
  await waitFor(async () => logRequests, (value) => value > previousLogRequests, '日志自动刷新未恢复');
  await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true }); document.dispatchEvent(new Event('visibilitychange')); });
  previousLogRequests = logRequests;
  await page.waitForTimeout(2300);
  assert.equal(logRequests, previousLogRequests, '页面隐藏后继续读取日志');
  await page.evaluate(() => { delete document.visibilityState; document.dispatchEvent(new Event('visibilitychange')); });
  await page.getByRole('button', { name: '关闭容器日志', exact: true }).click();
  await page.waitForTimeout(300);
  previousLogRequests = logRequests;
  await page.waitForTimeout(2300);
  assert.equal(logRequests, previousLogRequests, '关闭日志后继续轮询');
  page.off('request', countLogs);
  let containerScenario = 'empty';
  let releaseContainerLoad;
  let containerLoadGate;
  const containerPattern = '**/api/v1/containers*';
  await page.route(containerPattern, async (route) => {
    const scenario = containerScenario;
    if (scenario === 'loading') await containerLoadGate;
    if (scenario === 'error') { await route.fulfill({ status: 503, json: { code: 'dependency_unavailable', message: '容器读取失败，请重试' } }); return; }
    const response = await route.fetch();
    const value = await response.json();
    if (scenario === 'empty') value.items = [];
    if (scenario === 'partial') value.nodes.push({ node_id: 'offline-node', node_name: '远程节点', engine_id: '', version: '', state: 'offline', checked_at: new Date().toISOString(), error: { code: 'node_unavailable', message: '节点暂不可达' } });
    if (scenario === 'long') for (const item of value.items) { item.name = '很长的容器名称'.repeat(12); item.node_name = '很长的节点名称'.repeat(10); item.image = 'registry.example.com/namespace/'.repeat(5) + 'image:latest'; item.health = 'unhealthy'; }
    await route.fulfill({ response, json: value });
  });
  for (const theme of ['light', 'dark']) {
    await page.goto(`${base}/settings/appearance`);
    await page.getByRole('radio', { name: theme === 'light' ? '浅色' : '深色', exact: true }).click();
    for (const width of [320, 768, 1440]) {
      await page.setViewportSize({ width, height: 1000 });
      for (const scenario of ['empty', 'partial', 'long', 'loading', 'error']) {
        containerScenario = scenario;
        if (scenario === 'loading') containerLoadGate = new Promise((resolve) => { releaseContainerLoad = resolve; });
        await page.goto(`${base}/containers`);
        if (scenario === 'loading') await page.locator('main [aria-busy="true"]').first().waitFor();
        else if (scenario === 'empty') await page.getByText('暂无容器', { exact: true }).waitFor();
        else if (scenario === 'partial') await page.getByText('节点离线', { exact: true }).waitFor();
        else if (scenario === 'long') await page.getByText('很长的容器名称'.repeat(12), { exact: true }).first().waitFor();
        else await page.getByRole('alert').filter({ hasText: '容器读取失败' }).waitFor();
        await capture(`${theme}-${width}-containers-${scenario}`);
        if (scenario === 'loading') { releaseContainerLoad(); await page.locator('main [aria-busy="true"]').first().waitFor({ state: 'hidden' }); }
      }
    }
  }
  await page.unroute(containerPattern);
  await page.setViewportSize({ width: 1440, height: 1000 });
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
  writeFileSync(`${output}/result.json`, JSON.stringify({ pages: pages.length, themes: 2, widths: [320, 768, 1440], firewallWidths, firewallHeaderStates: ['normal', 'long_name', 'wrapped_status', 'loading', 'error'], containers: { native: true, restartRequests, logPolling: true, states: ['empty', 'partial', 'long', 'loading', 'error'] }, trustMigration: true, errors }, null, 2));
  console.log('界面验收通过：10 页面、深浅主题、容器真实操作与日志及 5 种页面状态、防火墙布局、互信迁移、浮层与未提交修改。');
} catch (error) {
  try {
    const response = await context.request.get(`${base}/api/v1/nodes`);
    const nodes = await response.json();
    const id = nodes.items.find((node) => node.local)?.id;
    if (id) {
      const paths = [`/nodes/${id}/firewall`, `/nodes/${id}/firewall/peer-trust`, '/operations/pending', `/operations?node_id=${id}&limit=20`];
      const states = {};
      for (const path of paths) { const result = await context.request.get(`${base}/api/v1${path}`); const text = await result.text(); let body; try { body = JSON.parse(text); } catch { body = text; } states[path] = { status: result.status(), body }; }
      writeFileSync(`${output}/failure-state.json`, JSON.stringify(states, null, 2));
    }
  } catch { /* 保留原始失败及页面截图。 */ }
  await page.screenshot({ path: `${output}/failure.png`, fullPage: true, mask: [page.getByLabel('一次性安装命令', { exact: true })] }).catch(() => {});
  writeFileSync(`${output}/failure.txt`, `${page.url()}\n${error.stack}\n${errors.join('\n')}`);
  throw error;
} finally { await browser.close(); }
