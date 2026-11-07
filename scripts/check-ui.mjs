import assert from 'node:assert/strict';
import { mkdirSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { createServer } from 'node:net';
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
  await page.screenshot({ path: `${output}/${name}.png`, fullPage: true, animations: 'disabled', mask: [page.getByLabel('令牌（仅显示一次）', { exact: true }), page.getByLabel('验证命令', { exact: true }), page.getByLabel('Prometheus 配置', { exact: true })] });
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${name} 页面横向溢出`);
};
const checkFirewallHeader = async () => {
  const width = page.viewportSize().width;
  const [node, family, filters, row, status, refresh, settings] = await Promise.all([
    page.getByLabel('目标节点', { exact: true }), page.getByLabel('地址族', { exact: true }),
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
const checkContainerFilters = async () => {
  const width = page.viewportSize().width;
  const scale = await page.evaluate(() => Number.parseFloat(getComputedStyle(document.documentElement).zoom) || 1);
  const [node, state, name, refresh] = await Promise.all([
    page.getByLabel('节点', { exact: true }), page.getByLabel('容器状态', { exact: true }), page.getByLabel('名称', { exact: true }),
    page.locator('.container-filters').getByRole('button', { name: '刷新', exact: true }),
  ].map((locator) => locator.boundingBox()));
  assert(node && state && name && refresh, `${width}px 容器筛选控件缺失`);
  const near = (a, b, message) => assert(Math.abs(a - b) <= 1, `${width}px ${message}：${a} / ${b}`);
  for (const box of [node, state, name, refresh]) near(box.height, 40 * scale, '容器控件高度');
  if (width >= 1280) {
    for (const box of [state, name, refresh]) near(box.y, node.y, '容器控件未同排');
  } else if (width >= 640) {
    near(state.y, node.y, '容器第一排未对齐');
    near(refresh.y, name.y, '容器刷新未与名称对齐');
    assert(name.y > node.y + node.height, '容器筛选未排为两列');
  } else {
    assert(state.y >= node.y + node.height && name.y >= state.y + state.height && refresh.y >= name.y + name.height, '窄屏容器筛选未纵向排列');
    near(refresh.x, node.x, '窄屏刷新未左对齐');
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
  const navigationOrder = ['overview', 'nodes', 'containers', 'databases', 'storage', 'firewall', 'settings'];
  const navigationLabels = ['概览', '节点', '容器', '数据库', '存储', '防火墙', '设置'];
  const menuLabels = (target) => target.getByRole('navigation', { name: '主导航' }).locator('.sidebar-link').allTextContents().then((items) => items.map((item) => item.trim()));
  const listLabels = (target) => target.getByRole('list', { name: '菜单顺序' }).locator('li > span').allTextContents();
  assert.deepEqual((await api('/settings/navigation')).order, navigationOrder);
  await waitFor(() => menuLabels(page), (items) => JSON.stringify(items) === JSON.stringify(navigationLabels), '默认菜单顺序错误');
  await page.goto(`${base}/settings/appearance`);
  await waitFor(() => page.getByRole('button', { name: '存储上移', exact: true }).isEnabled(), Boolean, '菜单偏好未读取');
  const otherContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  try {
    const otherPage = await otherContext.newPage();
    otherPage.on('pageerror', (error) => errors.push(error.message));
    await otherPage.goto(`${base}/signin`);
    await otherPage.getByLabel('账号', { exact: true }).fill('ui-check');
    await otherPage.getByLabel('密码', { exact: true }).fill(password);
    const otherEvents = otherPage.waitForResponse((response) => response.url().endsWith('/api/v1/events'));
    await otherPage.getByRole('button', { name: '登录', exact: true }).click();
    await otherPage.waitForURL(`${base}/`);
    await otherEvents;
    const otherAppearanceEvents = otherPage.waitForResponse((response) => response.url().endsWith('/api/v1/events'));
    await otherPage.goto(`${base}/settings/appearance`);
    await otherAppearanceEvents;
    await waitFor(() => otherPage.getByRole('button', { name: '存储上移', exact: true }).isEnabled(), Boolean, '另一会话未读取菜单偏好');
    await page.getByRole('button', { name: '存储上移', exact: true }).click();
    const customLabels = ['概览', '节点', '容器', '存储', '数据库', '防火墙', '设置'];
    assert.deepEqual(await listLabels(page), customLabels);
    assert.deepEqual(await menuLabels(page), navigationLabels);
    await page.getByRole('button', { name: '保存', exact: true }).click();
    await page.getByRole('status').filter({ hasText: '已保存' }).waitFor();
    await waitFor(() => menuLabels(otherPage), (items) => JSON.stringify(items) === JSON.stringify(customLabels), '跨会话菜单未通过 SSE 更新');
    const reloadedEvents = page.waitForResponse((response) => response.url().endsWith('/api/v1/events'));
    await page.reload();
    await reloadedEvents;
    await waitFor(() => menuLabels(page), (items) => JSON.stringify(items) === JSON.stringify(customLabels), '刷新丢失菜单顺序');
    await otherPage.getByRole('button', { name: '节点下移', exact: true }).click();
    const dirtyLabels = ['概览', '容器', '节点', '存储', '数据库', '防火墙', '设置'];
    const updated = otherPage.waitForResponse((response) => response.url().endsWith('/api/v1/settings/navigation') && response.request().method() === 'GET');
    await page.getByRole('button', { name: '设置上移', exact: true }).click();
    await page.getByRole('button', { name: '保存', exact: true }).click();
    await updated;
    await page.getByRole('status').filter({ hasText: '已保存' }).waitFor();
    assert.deepEqual(await listLabels(otherPage), dirtyLabels, 'SSE 覆盖了未提交顺序');
    const conflict = otherPage.waitForResponse((response) => response.url().endsWith('/api/v1/settings/navigation') && response.request().method() === 'PATCH');
    await otherPage.getByRole('button', { name: '保存', exact: true }).click();
    assert.equal((await conflict).status(), 409);
    await otherPage.getByRole('button', { name: '重新读取', exact: true }).click();
    assert.deepEqual(await listLabels(otherPage), dirtyLabels);
    await waitFor(() => otherPage.getByRole('button', { name: '保存', exact: true }).isEnabled(), Boolean, '读取后未恢复保存');
    await otherPage.getByRole('button', { name: '保存', exact: true }).click();
    await otherPage.getByRole('status').filter({ hasText: '已保存' }).waitFor();
    await waitFor(() => menuLabels(page), (items) => JSON.stringify(items) === JSON.stringify(dirtyLabels), '另一会话保存后菜单未更新');
    await page.setViewportSize({ width: 320, height: 1000 });
    await capture('navigation-order-320');
    await page.getByRole('button', { name: '打开菜单', exact: true }).click();
    await page.getByRole('dialog').waitFor();
    assert.deepEqual(await menuLabels(page), dirtyLabels);
    await capture('navigation-custom-mobile');
    await page.keyboard.press('Escape');
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole('button', { name: '恢复默认', exact: true }).click();
    await page.getByRole('button', { name: '保存', exact: true }).click();
    await page.getByRole('status').filter({ hasText: '已保存' }).waitFor();
    await waitFor(() => menuLabels(otherPage), (items) => JSON.stringify(items) === JSON.stringify(navigationLabels), '恢复默认未同步');
    assert.deepEqual((await api('/settings/navigation')).order, navigationOrder);
    await otherPage.getByRole('button', { name: '退出', exact: true }).click();
    await otherPage.waitForURL(`${base}/signin`);
    await otherPage.getByLabel('账号', { exact: true }).fill('ui-check');
    await otherPage.getByLabel('密码', { exact: true }).fill(password);
    await otherPage.getByRole('button', { name: '登录', exact: true }).click();
    await otherPage.waitForURL(`${base}/`);
    await waitFor(() => menuLabels(otherPage), (items) => JSON.stringify(items) === JSON.stringify(navigationLabels), '重新登录丢失菜单偏好');
  } finally { await otherContext.close(); }
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
  // 数据库执行使用后端真实集群验收；页面在同一生产程序中注入各状态数据。
  const databaseID = 'ui-galera';
  const checked = new Date().toISOString();
  const member = { node_id: id, instance_id: 'galera', name: 'MariaDB', address: '127.0.0.1:3306' };
  const proxy = { node_id: id, instance_id: 'proxy', name: 'ProxySQL', address: '127.0.0.1:6032' };
  const databaseState = { state: 'ready', version: '11.8.9-MariaDB', cluster_id: 'ui-cluster', cluster_name: '业务集群', configured_members: 'gcomm://10.0.0.1,10.0.0.2', actual_members: '10.0.0.1:3306,10.0.0.2:3306', member_address: '10.0.0.1:3306', cluster_size: 2, primary: true, ready: true, synced: true, read_only: false, connections: 12, receive_queue: 0, send_queue: 0, flow_control: 0.01, checked_at: checked, databases: [{ name: 'mysql', charset: 'utf8mb4', collation: 'utf8mb4_general_ci', bytes: 4096, protected: true }, { name: 'business', charset: 'utf8mb4', collation: 'utf8mb4_unicode_ci', bytes: 1048576, protected: false }], users: [{ name: 'root', host: 'localhost', protected: true, grants: [] }, { name: 'business', host: '%', protected: false, grants: [{ database: 'business', privileges: ['SELECT', 'INSERT'] }] }], servers: [], pools: [], proxy_users: [], pending_changes: false };
  const definition = { id: databaseID, name: '业务 Galera', coordinator: id, cluster_id: 'ui-cluster', revision: 1, members: [member], proxies: [proxy], workbench_url: 'https://example.com/sql' };
  const databaseValue = { cluster: definition, checked_at: checked, members: [{ ...member, status: databaseState }], proxies: [{ ...proxy, status: { ...databaseState, version: '3.0.3', servers: [{ hostgroup: 10, host: '10.0.0.1', port: 3306, state: 'ONLINE' }], pools: [{ hostgroup: 10, host: '10.0.0.1', port: 3306, used: 3, free: 7, errors: 0 }], proxy_users: [{ name: 'business', active: true, hostgroup: 10, state: 'in_sync', protected: false }] } }] };
  const databasePath = `/databases/clusters/${databaseID}`;
  const databaseListRoute = (route) => route.request().method() === 'GET' ? route.fulfill({ json: { items: [databaseValue] } }) : route.continue();
  await page.route('**/api/v1/databases/clusters', databaseListRoute);
  await page.route(`**/api/v1${databasePath}`, (route) => route.fulfill({ json: databaseValue }));
  await page.route(`**/api/v1${databasePath}/backups`, (route) => route.fulfill({ json: { items: [{ node_id: id, instance_id: 'galera', items: [{ name: 'daily', schedule: '*-*-* 03:00:00', state: 'failed', last_run: checked, last_success: checked, artifacts: ['/backup/daily/business.sql.zst'], error: '备份目录空间不足' }] }] } }));
  const pages = [['overview', '/'], ['nodes', '/nodes'], ['detail', `/nodes/${id}`], ['containers', '/containers'], ['firewall', `/firewall?node=${id}`], ['firewall-settings', '/firewall/settings'], ['databases', '/databases'], ['database-detail', `/databases/${databaseID}`], ['storage', '/storage'], ['api', '/settings/api'], ['account', '/settings/account'], ['sessions', '/settings/sessions'], ['appearance', '/settings/appearance'], ['version', '/settings/version']];
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
    await page.getByLabel('目标节点', { exact: true }).getByText(longName, { exact: true }).waitFor();
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
    const loadingRequests = [];
    const loadingRoute = (route) => {
      const request = (async () => { await pending; await route.fulfill({ json: firewall }); })();
      loadingRequests.push(request);
      return request;
    };
    await page.route(`**/api/v1${firewallPath}`, loadingRoute);
    await page.setViewportSize({ width: 1000, height: 1000 });
    try {
      await page.reload({ waitUntil: 'domcontentloaded' });
      await page.locator('.firewall-status [data-slot="skeleton"]').waitFor();
      await checkFirewallHeader();
      await capture(`${theme}-1000-firewall-loading`);
    } finally { release(); await page.unroute(`**/api/v1${firewallPath}`, loadingRoute); await Promise.all(loadingRequests); }
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
  assert.deepEqual(await page.getByRole('columnheader').allTextContents(), ['容器', '状态', '节点', '镜像', '端口', '操作']);
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
        await checkContainerFilters();
        await capture(`${theme}-${width}-containers-${scenario}`);
        if (scenario === 'loading') { releaseContainerLoad(); await page.locator('main [aria-busy="true"]').first().waitFor({ state: 'hidden' }); }
      }
    }
    containerScenario = 'normal';
    for (const width of firewallWidths) {
      await page.setViewportSize({ width, height: 1000 });
      await page.goto(`${base}/containers`);
      await page.getByText('opsd-ui-container', { exact: true }).waitFor();
      await checkContainerFilters();
      await capture(`${theme}-${width}-containers-header`);
    }
    await page.goto(`${base}/containers?node=${id}`);
    await page.getByText('opsd-ui-container', { exact: true }).waitFor();
    assert.deepEqual(await page.getByRole('columnheader').allTextContents(), ['容器', '状态', '镜像', '端口', '操作']);
    await capture(`${theme}-containers-single-node`);
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.evaluate(() => { document.documentElement.style.zoom = '200%'; });
    await checkContainerFilters();
    await capture(`${theme}-containers-zoom-200`);
    await page.evaluate(() => { document.documentElement.style.zoom = ''; });
  }
  await page.unroute(containerPattern);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/firewall/settings`);
  await page.getByRole('switch', { name: '集群互信' }).click();
  await page.getByRole('alertdialog').waitFor();
  await page.getByText(/优先放行.*所有主机入站端口.*数据库端口/).waitFor();
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
  const nativeTrustReady = () => ['iptables-save', 'ip6tables-save'].every((save) => {
    const raw = execFileSync('sudo', [save, '-t', 'filter'], { encoding: 'utf8' });
    const inputs = raw.split('\n').filter((line) => line.startsWith('-A INPUT '));
    const hook = '-A INPUT -m comment --comment opsd-peer-trust -j OPSD_PEERS';
    return inputs[0] === hook && inputs.filter((line) => line === hook).length === 1;
  });
  await waitFor(async () => nativeTrustReady(), Boolean, '生产互信跳转未位于双栈 INPUT 首条');
  execFileSync('sudo', ['ufw', 'reload']);
  await waitFor(async () => nativeTrustReady(), Boolean, 'UFW 重载后互信顺序未恢复');
  execFileSync('sudo', ['systemctl', 'stop', 'opsd.service']);
  try {
    for (const command of ['iptables', 'ip6tables']) execFileSync('sudo', [command, '-D', 'INPUT', '-m', 'comment', '--comment', 'opsd-peer-trust', '-j', 'OPSD_PEERS']);
    execFileSync('sudo', ['systemctl', 'restart', 'opsd-executor.service']);
    await waitFor(async () => nativeTrustReady(), Boolean, '节点停止时执行器未从持久目标恢复互信');
  } finally { execFileSync('sudo', ['systemctl', 'start', 'opsd.service']); }
  await waitFor(() => api(trustPath).catch(() => null), (value) => value?.state === 'in_sync', '节点恢复后互信状态未同步');
  await page.goto(`${base}/firewall?node=${id}&type=trust`);
  await page.getByRole('tab', { name: '互信规则 2', exact: true }).waitFor();
  assert.deepEqual(await page.getByRole('columnheader').allTextContents(), ['来源节点', 'IPv4', 'IPv6', '操作']);
  assert.equal(await page.locator('main tbody tr').count(), 1, '同一来源节点的双栈地址未合并');
  for (const theme of ['light', 'dark']) {
    await page.goto(`${base}/settings/appearance`);
    await page.getByRole('radio', { name: theme === 'light' ? '浅色' : '深色', exact: true }).click();
    for (const width of firewallWidths) {
      await page.setViewportSize({ width, height: 1000 });
      await page.goto(`${base}/firewall?node=${id}&type=trust`);
      await page.getByRole('columnheader', { name: '来源节点', exact: true }).waitFor();
      await page.locator('main tbody').getByRole('link', { name: 'ui-check', exact: true }).waitFor();
      await capture(`${theme}-trust-rules-${width}`);
    }
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.evaluate(() => { document.documentElement.style.zoom = '200%'; });
    await capture(`${theme}-trust-zoom-200`);
    await page.evaluate(() => { document.documentElement.style.zoom = ''; });
  }
  const trustError = { code: 'conflict', message: '测试互信运行规则读取失败' };
  const partialTrust = { ...trust, state: 'failed', rules: trust.rules.map((rule) => rule.family === 'ipv4' ? { ...rule, state: 'failed', error: trustError.message } : rule), results: trust.results.map((result) => result.family === 'ipv4' && result.scope === 'runtime' ? { ...result, state: 'failed', error: trustError } : result) };
  await page.route(`**/api/v1${trustPath}`, (route) => route.fulfill({ json: partialTrust }));
  await page.reload();
  await page.getByText('部分生效', { exact: true }).first().waitFor();
  assert.equal(await page.getByText(trustError.message, { exact: true }).count(), 0, '互信列表重复展开了错误详情');
  await page.getByRole('button', { name: '查看', exact: true }).click();
  await page.getByRole('heading', { name: '互信详情', exact: true }).waitFor();
  await page.getByText('永久配置', { exact: true }).first().waitFor();
  await page.getByText('运行配置', { exact: true }).first().waitFor();
  assert.equal(await page.getByText(trustError.message, { exact: true }).count(), 1);
  await capture('trust-partial-detail');
  await page.getByRole('button', { name: '关闭互信详情', exact: true }).click();
  await page.getByLabel('地址族', { exact: true }).click();
  await page.getByRole('option', { name: 'IPv4', exact: true }).click();
  await page.getByRole('columnheader', { name: 'IPv6', exact: true }).waitFor({ state: 'hidden' });
  assert.deepEqual(await page.getByRole('columnheader').allTextContents(), ['来源节点', 'IPv4', '操作']);
  await capture('trust-partial-ipv4');
  await page.unroute(`**/api/v1${trustPath}`);
  const activePolicy = await api('/peer-trust');
  await page.route('**/api/v1/peer-trust/status', (route) => route.fulfill({ json: { policy: activePolicy, nodes: [partialTrust] } }));
  await page.goto(`${base}/firewall/settings`);
  await page.getByText('部分生效', { exact: true }).waitFor();
  assert.deepEqual(await page.getByRole('columnheader').allTextContents(), ['目标节点', 'IPv4', 'IPv6', '互信状态', '检查时间', '操作']);
  await capture('trust-partial-settings');
  await page.unroute('**/api/v1/peer-trust/status');
  const sharedTrust = { ...trust, rules: trust.rules.map((rule) => ({ ...rule, members: [...rule.members, { id: 'shared-node', name: '共享公网地址节点' }] })).concat([{ family: 'ipv4', members: [{ id: 'discovering-node', name: '待发现节点' }], state: 'pending' }, { family: 'ipv4', address: '9.9.9.9', members: [], state: 'removing' }]) };
  await page.route(`**/api/v1${trustPath}`, (route) => route.fulfill({ json: sharedTrust }));
  await page.goto(`${base}/firewall?node=${id}&type=trust`);
  await page.getByRole('link', { name: '共享公网地址节点', exact: true }).waitFor();
  assert.equal(await page.locator('main tbody tr').count(), 4, '共享地址、待发现与已移除节点的分组不符');
  await page.getByText('待发现', { exact: true }).waitFor();
  await page.getByText('已移除节点', { exact: true }).waitFor();
  await capture('trust-shared-discovery-removal');
  await page.unroute(`**/api/v1${trustPath}`);
  await page.reload();
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
  for (const theme of ['light', 'dark']) {
    await page.goto(`${base}/settings/appearance`);
    await page.getByRole('radio', { name: theme === 'light' ? '浅色' : '深色', exact: true }).click();
    for (const width of [320, 768, 1440]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.goto(`${base}/settings/api`);
      await page.getByRole('button', { name: '创建令牌', exact: true }).click();
      const form = page.getByRole('dialog');
      await form.getByLabel('监控范围', { exact: true }).click();
      await page.getByRole('option', { name: '指定节点', exact: true }).click();
      await form.getByRole('checkbox', { name: 'ui-check', exact: true }).check();
      await capture(`${theme}-${width}-api-token-form`);
      await page.getByRole('button', { name: '关闭令牌表单', exact: true }).click();
      await page.goto(`${base}/databases/${databaseID}`);
      for (const tab of ['概览', '成员', '库', '账号', '代理', '备份']) {
        const target = page.getByRole('tab', { name: tab, exact: true });
        if (await target.getAttribute('aria-selected') !== 'true') await target.click();
        if (tab === '备份') await page.getByText('备份目录空间不足', { exact: true }).waitFor();
        await capture(`${theme}-${width}-database-${tab}`);
      }
      await page.getByRole('tab', { name: '库', exact: true }).click();
      await page.getByRole('button', { name: '创建库', exact: true }).click();
      await page.getByLabel('库名', { exact: true }).fill('ui_business');
      await capture(`${theme}-${width}-database-create`);
      await page.getByRole('button', { name: '关闭数据库操作', exact: true }).click();
      await page.getByRole('tab', { name: '账号', exact: true }).click();
      await page.getByRole('button', { name: '创建账号', exact: true }).click();
      await page.getByRole('checkbox', { name: 'ProxySQL', exact: true }).check();
      await page.getByLabel('主机组', { exact: true }).fill('10');
      await capture(`${theme}-${width}-database-account`);
      await page.getByRole('button', { name: '关闭数据库操作', exact: true }).click();
      await page.getByRole('button', { name: '编辑集群', exact: true }).click();
      await capture(`${theme}-${width}-database-setup`);
      await page.getByRole('button', { name: '关闭数据库集群表单', exact: true }).click();
    }
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/settings/api`);
  await page.getByRole('button', { name: '创建令牌', exact: true }).click();
  await page.getByRole('dialog').getByLabel('名称', { exact: true }).fill('实际看板接入');
  await page.getByRole('button', { name: '生成令牌', exact: true }).click();
  const tokenValue = await page.getByLabel('令牌（仅显示一次）', { exact: true }).inputValue();
  const authorization = { Authorization: `Bearer ${tokenValue}` };
  const verify = await context.request.get(`${base}/api/v1/api-tokens/verify`, { headers: authorization });
  assert.equal(verify.status(), 200);
  const createdToken = await verify.json();
  const current = await context.request.get(`${base}/api/v1/monitoring/current`, { headers: authorization });
  assert.equal(current.status(), 200);
  assert.equal((await current.json()).items[0].node_id, id);
  const metrics = await context.request.get(`${base}/metrics`, { headers: authorization });
  assert.equal(metrics.status(), 200);
  assert.match(await metrics.text(), /opsd_node_up\{/);
  assert.match(await page.getByLabel('验证命令', { exact: true }).inputValue(), /api-tokens\/verify/);
  assert.match(await page.getByLabel('Prometheus 配置', { exact: true }).inputValue(), /scrape_interval: 15s/);
  await capture('api-connection-result');
  await page.getByRole('button', { name: '完成', exact: true }).click();

  let databaseOperation = { ...(await api('/operations?limit=1')).items[0], id: 'ui-database-operation', node_id: id, subject: 'database', resource_id: databaseID, state: 'attention_required', revision: 1, result_pending_sync: false, error: { code: 'dependency_unavailable', message: 'ProxySQL 目标暂不可达' }, steps: [{ name: 'Galera：创建业务库', state: 'completed' }, { name: 'ProxySQL：同步账号', state: 'unknown' }] };
  await page.route(`**/api/v1${databasePath}/changes`, (route) => route.fulfill({ status: 202, json: databaseOperation }));
  await page.route('**/api/v1/operations/ui-database-operation', (route) => route.fulfill({ json: databaseOperation }));
  await page.route('**/api/v1/operations/ui-database-operation/recover', (route) => { databaseOperation = { ...databaseOperation, revision: 2, state: 'succeeded', error: undefined, steps: databaseOperation.steps.map((step) => ({ ...step, state: 'completed' })) }; return route.fulfill({ json: databaseOperation }); });
  await page.goto(`${base}/databases/${databaseID}`);
  await page.getByRole('tab', { name: '库', exact: true }).click();
  await page.getByRole('button', { name: '创建库', exact: true }).click();
  await page.getByLabel('库名', { exact: true }).fill('ui_business');
  await page.getByRole('dialog').getByRole('button', { name: '创建库', exact: true }).click();
  await page.getByText('ProxySQL 目标暂不可达', { exact: true }).waitFor();
  await capture('database-operation-partial');
  await page.getByRole('button', { name: '重新处理', exact: true }).click();
  await page.getByRole('button', { name: '重新处理', exact: true }).waitFor({ state: 'hidden' });
  await capture('database-operation-recovered');
  await page.getByRole('button', { name: '关闭操作', exact: true }).click();
  await page.unroute(`**/api/v1${databasePath}/changes`);
  await page.unroute('**/api/v1/operations/ui-database-operation');
  await page.unroute('**/api/v1/operations/ui-database-operation/recover');
  await page.route(`**/api/v1${databasePath}`, (route) => route.fulfill({ status: 503, json: { code: 'dependency_unavailable', message: '数据库协调节点暂不可达' } }));
  await page.reload();
  await page.getByRole('alert').filter({ hasText: '数据库协调节点暂不可达' }).waitFor();
  await capture('database-read-error');
  await page.unroute(`**/api/v1${databasePath}`);
  await page.unroute('**/api/v1/databases/clusters');
  await page.unroute(`**/api/v1${databasePath}/backups`);

  const cli = (kind, enabled) => execFileSync('sudo', ['/opt/opsd/bin/opsd', kind, `--enabled=${enabled}`]);
  try {
    cli('console', false);
    await waitFor(() => context.request.get(`${base}/signin`), (r) => r.status() === 404, '控制台未关闭');
    assert.equal((await context.request.get(`${base}/api/v1/api-tokens/verify`, { headers: authorization })).status(), 200);
    cli('console', true);
    cli('api', false);
    await waitFor(() => context.request.get(`${base}/api/v1/api-tokens/verify`, { headers: authorization }), (r) => r.status() === 503, 'API 未独立关闭');
    assert.equal((await context.request.get(`${base}/signin`)).status(), 200);
    cli('console', false);
    await new Promise((resolve, reject) => { const listener = createServer(); listener.on('error', reject); listener.listen(Number(new URL(base).port), '127.0.0.1', () => listener.close(resolve)); });
  } finally { cli('console', true); cli('api', true); }
  await waitFor(() => api('/auth/session').catch(() => null), Boolean, 'Web 恢复后会话未保留');
  const deletedToken = await context.request.delete(`${base}/api/v1/api-tokens/${createdToken.id}?revision=${createdToken.revision}`, { headers: { Origin: base, 'X-CSRF-Token': session.csrf_token } });
  assert.equal(deletedToken.status(), 204);
  assert.equal((await context.request.get(`${base}/api/v1/api-tokens/verify`, { headers: authorization })).status(), 401);
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
  writeFileSync(`${output}/result.json`, JSON.stringify({ pages: pages.length, navigation: { accountScope: true, crossSession: true, sse: true, unsavedPreserved: true, conflict: true, restored: true }, themes: 2, widths: [320, 768, 1440], firewallWidths, firewallHeaderStates: ['normal', 'long_name', 'wrapped_status', 'loading', 'error'], api: { tokenCreatedInBrowser: true, authenticatedJSON: true, metrics: true, independentSwitches: true, releasedPort: true, revoked: true }, databases: { presentationData: 'injected', tabs: 6, operationRecovery: true, connectionForms: true }, containers: { native: true, restartRequests, logPolling: true, headerWidths: firewallWidths, singleNodeColumns: true, states: ['empty', 'partial', 'long', 'loading', 'error'] }, trustMigration: true, trustPriority: { ufwReload: true, executorRestartWithoutNode: true }, trustLayout: { groupedSources: true, partialFamilies: true, widths: firewallWidths, cssZoom: 200 }, errors }, null, 2));
  console.log(`界面验收通过：${pages.length} 页面、深浅主题、API 令牌接入与独立启停、数据库页面与操作恢复、容器、防火墙及互信。`);
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
