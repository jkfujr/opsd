import assert from 'node:assert/strict';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
import { chromium, request } from 'playwright';
const root = fileURLToPath(new URL('../', import.meta.url));
const artifacts = join(root, '../../artifacts/frontend/gpt2');
const base = process.env.OPSD_GPT2_URL ?? 'http://127.0.0.1:5183';
assert(['127.0.0.1', 'localhost'].includes(new URL(base).hostname), '此检查只操作本地验收实例');
const password = process.env.OPSD_GPT2_PASSWORD ?? 'test-password';
const mode = process.argv.includes('--recovery') ? 'recovery' : process.argv.includes('--builtin') ? 'builtin' : 'theme';
const prefix = mode === 'recovery' ? '/__opsd/builtin' : '';
const entry = process.env.OPSD_GPT2_ENTRANCE ?? '/signin';
const resultDirectory = mode === 'theme' ? 'integration' : `integration-${mode}`;
const { version } = JSON.parse(await readFile(join(root, '../../package.json'), 'utf8'));
const forced = (process.argv.find(arg => arg.startsWith('--rerun='))?.slice(8) ?? '').split('|');
const resume = process.argv.includes('--resume'), replace = process.argv.includes('--replace'), resourceState = process.argv.includes('--resource-state');
let previous = { results: [] }; if (resume) { try { previous = JSON.parse(await readFile(join(artifacts, resultDirectory, 'result.json'), 'utf8')); } catch {} }
const output = join(artifacts, resultDirectory); await mkdir(output, { recursive: true });
const results = [], errors = [], captures = [], counts = {}, receipts = new Map(), operations = new Map();
const browser = await chromium.launch({ channel: process.env.OPSD_GPT2_BROWSER ?? 'msedge' });
const apiContext = await request.newContext({ baseURL: base });
let session, id, installation, context, page;
const checked = new Date().toISOString(), rev = 'a'.repeat(64), containerID = 'b'.repeat(64);
const ready = { state: 'ready', data_at: checked, attempted_at: checked };
async function api(path, method = 'GET', data, extra = {}) { const response = await apiContext.fetch('/api/v2' + path, { method, data, headers: { Origin: base, ...(session ? { 'X-CSRF-Token': session.csrf_token } : {}), ...extra } }); const body = response.status() === 204 ? null : await response.json(); assert(response.ok(), `${method} ${path}: ${response.status()} ${body?.detail ?? ''}`); return { body, response }; }
async function step(name, action) { if (resume && name !== '真实主题 ZIP 安装、启用与主题登录' && previous.results.some(item => item.name === name && item.passed) && !forced.some(value => value && name.includes(value)) && !(resourceState && name === '任务元数据、定义编辑启停、执行、取消与分页 UTF-8 日志')) { results.push({ name, passed: true, reused: true }); console.log(`复用此前结果：${name}`); return; } try { await action(); results.push({ name, passed: true }); console.log(`通过：${name}`); } catch (error) { results.push({ name, passed: false, error: String(error) }); throw error; } }
async function login(p) { await p.goto(base + entry + (mode === 'recovery' ? '?builtin=1' : '')); await p.getByLabel('账号', { exact: true }).fill('admin'); await p.getByLabel('密码', { exact: true }).fill(password); await p.getByRole('button', { name: '登录', exact: true }).click(); await p.waitForURL(base + prefix + '/'); await p.getByRole('heading', { name: '概览', exact: true }).waitFor(); }
async function go(path) { await page.goto(base + prefix + path); await page.locator('.page-content h1, .terminal-identity h1').first().waitFor(); }
async function wait(check, message) { for (let n = 0; n < 100; n++) { if (await check()) return; await page.waitForTimeout(100); } throw Error(message); }
async function closeDrawers() { const close = page.getByRole('button', { name: '关闭侧栏', exact: true }); if (await close.isVisible()) await close.click(); }
async function capture(name) { const prior = resume && previous.captures?.find(item => item.name === name); if (prior) { captures.push({ ...prior, reused: true }); return; } await page.evaluate(async () => { await document.fonts.ready; await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))); }); await page.screenshot({ path: join(output, name + '.png'), fullPage: true, animations: 'disabled' }); const size = await page.evaluate(() => ({ width: innerWidth, scrollWidth: document.documentElement.scrollWidth })); assert(size.scrollWidth <= size.width + 1, `${name} 横向溢出 ${JSON.stringify(size)}`); captures.push({ name, ...size }); }
function op(subject = 'container', state = 'succeeded') { const value = { id: `ui-${operations.size + 1}`, node_id: id, executor: 'node', subject, state, revision: 1, result_pending_sync: false, created_at: checked, updated_at: checked, decision: 'undecided', steps: [{ name: '执行并读取', state: state === 'succeeded' ? 'completed' : 'pending' }], ...(state === 'awaiting_confirmation' ? { confirmation_remaining_seconds: 17, confirmation_deadline: new Date(Date.now() + 17000).toISOString() } : {}) }; operations.set(value.id, value); return value; }
let overview, nodeList, container, project, task, databaseValue, firewall, config;
const historyQueries = [];
let operationReadError = false, paging = false, pageAvailable = false, nextPageReads = 0, lostDatabase = false, missingReceipt = false, conflictFirewall = false;
const receiptTargets = [];
let resourceMode = 'normal', definitionReads = 0, taskSaves = [], actions = [], operationLists = 0;
async function installFixtures(c) {
  await c.route('**/api/v2/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname.slice('/api/v2'.length), method = req.method();
    counts[`${method} ${path}`] = (counts[`${method} ${path}`] ?? 0) + 1;
    const json = (body, status = 200, headers = {}) => route.fulfill({ status, json: body, headers });
    const accepted = (subject, state = 'succeeded') => { const value = op(subject, state), key = req.headers()['idempotency-key']; if (key) receipts.set(key, value); return json(value, 202); };
    if (path === '/overview') return json(overview);
    if (path === '/nodes') return json(nodeList);
    if (path === '/monitoring/history') { historyQueries.push(url.searchParams.get('node_ids')); return json({ interval_minutes: 5, items: Array.from({ length: 10 }, (_, n) => ({ collected_at: new Date(Date.now() - (9 - n) * 300000).toISOString(), summary: overview.summary })), unavailable: [] }); }
    if (path === '/containers' && paging) {
      const node = { node_id: 'page-node', node_name: '分页节点', read: pageAvailable ? ready : { state: 'offline' } };
      if (!url.searchParams.has('cursor')) return json({ items: [container], nodes: [{ node_id: id, node_name: container.node_name, read: ready }, node], unavailable: [{ node_id: 'page-node', node_name: '分页节点', message: '离线' }], next_cursor: 'page-offline' });
      nextPageReads++;
      if (!pageAvailable) return json({ detail: '后续页暂时不可用', code: 'dependency_unavailable', outcome: 'not_accepted' }, 503);
      return json({ items: [{ ...container, container_id: 'c'.repeat(64), name: 'worker' }], nodes: [node], unavailable: [] });
    }
    if (path === '/containers') { if (resourceMode === 'error') return json({ detail: '容器读取失败', code: 'dependency_unavailable' }, 503); const read = resourceMode === 'offline' ? { state: 'offline' } : ready; return json({ items: resourceMode === 'empty' ? [] : [container], nodes: [{ node_id: id, node_name: nodeList.items[0].name, read }], unavailable: resourceMode === 'partial' ? [{ node_id: 'offline', node_name: '离线节点', message: '离线' }] : [] }); }
    if (path === `/nodes/${id}/engines/docker/containers/${containerID}`) return json({ ...container, created_at: checked, started_at: checked, restart_policy: 'unless-stopped', restart_count: 0, mounts: [{ type: 'bind', source: '/data', destination: '/app/data', read_only: true }], networks: [], tty: false, checked_at: checked });
    if (path.endsWith(`/containers/${containerID}/logs`)) return json({ engine_id: 'docker', container_id: containerID, text: '服务已启动\n中文 UTF-8 正常\n', truncated: false, checked_at: checked });
    if (path.endsWith(`/containers/${containerID}/actions`)) { actions.push({ kind: 'container', body: req.postDataJSON(), key: req.headers()['idempotency-key'] }); return accepted('container'); }
    if (path === '/compose-projects') return json({ items: [project], nodes: [{ node_id: id, node_name: nodeList.items[0].name, read: ready }], unavailable: [] });
    if (path.endsWith('/compose-projects/web/registration')) { if (method === 'GET') return json(project, 200, { ETag: '"compose:1"' }); project.registered = true; actions.push({ kind: 'compose-register', headers: req.headers() }); return accepted('compose_register'); }
    if (path.endsWith('/compose-projects/web/actions')) { actions.push({ kind: 'compose', body: req.postDataJSON() }); return accepted('compose'); }
    if (path === '/tasks') return json({ items: [task], nodes: [{ node_id: id, node_name: task.node_name, read: ready }], unavailable: [] });
    if (path === `/nodes/${id}/tasks/daily/definition`) { if (method === 'GET') { definitionReads++; return json({ name: task.name, schedule: task.schedule, timezone: task.timezone, directory: task.directory, enabled: task.enabled, script: 'printf "原脚本"\n' }, 200, { ETag: '"task:1"' }); } const body = req.postDataJSON(); taskSaves.push({ body, headers: req.headers() }); task = { ...task, ...body, script: undefined, revision: task.revision + 1 }; return accepted('task_save'); }
    if (/^\/nodes\/[^/]+\/tasks\/[^/]+\/definition$/.test(path) && method === 'PUT') { taskSaves.push({ body: req.postDataJSON(), headers: req.headers() }); return accepted('task_save'); }
    if (path === `/nodes/${id}/tasks/daily/executions`) { if (method === 'GET') return json({ items: [...operations.values()].filter(o => o.subject === 'task_run') }); actions.push({ kind: 'task-run', body: req.postDataJSON() }); return accepted('task_run', 'applying'); }
    if (path === '/database-clusters' && method === 'GET') return json({ items: [databaseValue] });
    if (path === '/database-clusters/ui-db/status') return json(databaseValue);
    if (path === '/database-clusters/ui-db/backups') return json({ items: [{ node_id: id, instance_id: 'galera', items: [{ name: 'daily', schedule: '每天 03:00', state: 'failed', last_run: checked, artifacts: [], error: '备份目录空间不足' }] }] });
    if (path === '/database-clusters/ui-db/changes') {
      actions.push({ kind: 'database', body: req.postDataJSON() });
      if (lostDatabase) { const value = op('database'); value.node_id = databaseValue.cluster.coordinator; receipts.set(req.headers()['idempotency-key'], value); return route.abort('failed'); }
      return accepted('database');
    }
    if (path === `/nodes/${id}/firewall/status`) return json(firewall);
    if (path === `/nodes/${id}/firewall/configuration`) return json({ backend: firewall.backend, revision: firewall.revision, rules: firewall.rules, default_policies: firewall.default_policies }, 200, { ETag: `"firewall:${firewall.revision}"` });
    if (path === `/nodes/${id}/firewall/changes`) { if (conflictFirewall) { conflictFirewall = false; firewall.revision++; return json({ detail: '防火墙版本已经改变', code: 'precondition_failed', outcome: 'not_accepted' }, 412); } actions.push({ kind: 'firewall', body: req.postDataJSON() }); return accepted('firewall_config', 'awaiting_confirmation'); }
    if (path === `/nodes/${id}/configuration`) { if (method === 'GET') return json(config, 200, { ETag: `"config:${config.revision}"` }); actions.push({ kind: 'config', body: req.postDataJSON() }); return accepted('config'); }
    if (path === '/operations') { operationLists++; const items = [...operations.values()].filter(o => !url.searchParams.has('pending') || ['accepted', 'prepared', 'applying', 'awaiting_confirmation', 'attention_required'].includes(o.state)); return json({ items }); }
    const operation = path.match(/^\/operations\/(ui-\d+)$/)?.[1]; if (operation && operations.has(operation)) return operationReadError ? json({ detail: '操作状态读取失败', code: 'dependency_unavailable' }, 503) : json(operations.get(operation));
    const logID = path.match(/^\/operations\/(ui-\d+)\/logs$/)?.[1]; if (logID) { const bytes = Buffer.from('日志中文完成\n'), offset = Number(url.searchParams.get('offset') ?? 0), chunk = offset ? bytes.subarray(4) : bytes.subarray(0, 4); return json({ data: chunk.toString('base64'), next_offset: offset ? bytes.length : 4, has_more: !offset, truncated: false }); }
    const actionID = path.match(/^\/operations\/(ui-\d+)\/actions$/)?.[1]; if (actionID) { const previous = operations.get(actionID), body = req.postDataJSON(); actions.push({ kind: 'operation', body }); const value = { ...previous, revision: previous.revision + 1, state: body.action === 'cancel' ? 'cancelled' : 'succeeded' }; operations.set(actionID, value); return json(value); }
    const requestKey = path.match(/^\/requests\/(.+)$/)?.[1];
    if (requestKey && receipts.has(requestKey)) {
      const value = receipts.get(requestKey); receiptTargets.push(url.searchParams.get('node_id'));
      if (missingReceipt) { missingReceipt = false; return json({ detail: '原请求暂未找到', code: 'not_found' }, 404); }
      assert.equal(url.searchParams.get('node_id'), value.node_id, '查询原请求使用了错误的所属节点');
      return json({ key: requestKey, owner: value.node_id, actor: 'browser-admin', state: 'completed', operation_id: value.id, created_at: checked });
    }
    return route.continue();
  });
}
try {
  await step('真实主题 ZIP 安装、启用与主题登录', async () => {
    await api('/auth/sessions', 'POST', { entrance: entry, username: 'admin', password }); session = (await api('/auth/sessions/current')).body; id = session.local_node_id;
    const nav = await api('/settings/navigation'); await api('/settings/navigation', 'PUT', { order: ['overview','nodes','containers','databases','storage','firewall','tools','settings'] }, { 'If-Match': nav.response.headers().etag });
    const existing = (await api('/console/theme-installations')).body.items.filter(item => item.manifest.id === 'frontend-gpt2' && item.manifest.version === version);
    if (existing.length && (!resume || replace)) {
      const selected = await api('/console/theme-selection');
      if (existing.some(item => item.installation_id === selected.body.installation_id)) await api('/console/theme-selection', 'PUT', { installation_id: 'builtin' }, { 'If-Match': selected.response.headers().etag });
      for (const item of existing) { const current = await api(`/console/theme-installations/${item.installation_id}`); await api(`/console/theme-installations/${item.installation_id}`, 'DELETE', undefined, { 'If-Match': current.response.headers().etag }); }
    }
    const installed = resume && !replace && existing.length ? { body: existing[0] } : await api('/console/theme-installations', 'POST', await readFile(join(artifacts, `opsd-frontend-gpt2-${version}.zip`)), { 'Content-Type': 'application/zip', 'Idempotency-Key': crypto.randomUUID() }); installation = installed.body;
    const selection = await api('/console/theme-selection'); await api('/console/theme-selection', 'PUT', { installation_id: mode === 'builtin' ? 'builtin' : installation.installation_id }, { 'If-Match': selection.response.headers().etag });
    context = await browser.newContext({ viewport: { width: 1440, height: 1000 } }); page = await context.newPage(); page.on('pageerror', e => errors.push(e.message)); await login(page);
    assert.deepEqual(await page.getByRole('navigation', { name: '主导航', exact: true }).locator('.nav-link').allTextContents(), ['概览', '节点', '容器', '数据库', '存储', '防火墙', '工具', '设置']);
    await go('/settings/version'); await page.reload(); await page.getByRole('heading', { name: '版本', exact: true }).waitFor();
    const font = await page.evaluate(async () => { await document.fonts.load('400 16px "Inter"'); await document.fonts.load('400 16px "Source Han Sans SC"', '中文'); return document.fonts.check('400 16px "Inter"') && document.fonts.check('400 16px "Source Han Sans SC"', '中文'); }); assert(font);
  });
  await step('真实菜单排序、跨会话 SSE、冲突保留草稿和刷新保持', async () => {
    await go('/settings/appearance'); const other = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    try { const second = await other.newPage(); second.on('pageerror', e => errors.push(e.message)); await login(second); await second.goto(base + prefix + '/settings/appearance');
      await second.getByRole('button', { name: '节点下移', exact: true }).click(); const draft = await second.getByRole('list', { name: '菜单顺序' }).locator('li > span').allTextContents();
      await page.getByRole('button', { name: '存储上移', exact: true }).click(); await page.getByRole('button', { name: '保存', exact: true }).click();
      await wait(async () => (await second.getByRole('navigation', { name: '主导航', exact: true }).locator('.nav-link').allTextContents())[3] === '存储', '另一会话未收到导航更新');
      assert.deepEqual(await second.getByRole('list', { name: '菜单顺序' }).locator('li > span').allTextContents(), draft);
      await second.getByRole('button', { name: '保存', exact: true }).click(); await second.getByRole('button', { name: '重新读取', exact: true }).click(); assert.deepEqual(await second.getByRole('list', { name: '菜单顺序' }).locator('li > span').allTextContents(), draft);
      await second.getByRole('button', { name: '保存', exact: true }).click(); await wait(async () => JSON.stringify((await api('/settings/navigation')).body.order) === JSON.stringify(['overview','containers','nodes','databases','storage','firewall','tools','settings']), '冲突后重读未使用新版本保存');
      await page.reload(); await wait(async () => JSON.stringify(await page.getByRole('navigation', { name: '主导航', exact: true }).locator('.nav-link').allTextContents()) === JSON.stringify(draft), '刷新丢失已保存的顺序');
      await page.getByRole('button', { name: '恢复默认', exact: true }).click(); await page.getByRole('button', { name: '保存', exact: true }).click(); await wait(async () => (await api('/settings/navigation')).body.order[1] === 'nodes', '菜单未恢复');
    } finally { await other.close(); }
  });
  await step('真实接入码生成、安装命令、API 令牌与存储检查', async () => {
    await go('/nodes'); await page.getByRole('button', { name: '添加节点', exact: true }).click(); await page.getByLabel('名称（可选）', { exact: true }).fill('本地浏览器检查'); await page.getByRole('button', { name: '生成安装命令', exact: true }).click(); await page.getByLabel('一次性安装命令', { exact: true }).waitFor(); assert((await page.getByLabel('一次性安装命令', { exact: true }).inputValue()).includes('sudo sh'));
    await page.getByRole('button', { name: '关闭', exact: true }).first().click(); await go('/settings/api'); await page.getByRole('button', { name: '创建令牌', exact: true }).click(); await page.getByRole('button', { name: '生成令牌', exact: true }).click(); const field = page.getByLabel('令牌（仅显示一次）', { exact: true }); await field.waitFor(); const secret = await field.inputValue(); const stores = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } })); assert(secret && !stores.includes(secret)); await page.getByRole('button', { name: '完成', exact: true }).click();
    await go('/storage'); assert(await page.getByRole('heading', { name: '未接入', exact: true }).isVisible());
  });
  await step('真实写入应答丢失后查询原键且只发送一次', async () => {
    let posts = 0, reads = 0, originalKey = '';
    const path = `/api/v2/nodes/${id}`; await page.route(url => url.pathname === path, async route => { if (route.request().method() !== 'PATCH') return route.continue(); posts++; originalKey = route.request().headers()['idempotency-key']; const response = await route.fetch(); assert.equal(response.status(), 202); await route.abort('failed'); });
    page.on('request', req => { if (req.url().includes('/api/v2/requests/') && req.url().includes(originalKey)) reads++; });
    await go('/nodes'); await page.getByRole('button', { name: '编辑 本地验收节点', exact: true }).click(); await page.getByLabel('名称', { exact: true }).fill('本地验收节点'); await page.getByRole('button', { name: '保存名称', exact: true }).click(); await page.getByRole('button', { name: '取消', exact: true }).click(); await page.getByRole('button', { name: /^操作记录/ }).click(); await page.getByRole('button', { name: '查询原请求', exact: true }).click(); await wait(async () => reads > 0 && (await page.locator('.operation-detail').count()) > 0, '未查询原请求'); assert.equal(posts, 1); assert(originalKey); await closeDrawers(); await page.unroute(url => url.pathname === path);
  });
  await step('真实秘密值丢失后的查询与显式重新生成', async () => {
    await go('/settings/api'); await page.getByRole('button', { name: '创建令牌', exact: true }).click();
    const name = `秘密丢失检查-${Date.now()}`;
    await page.getByLabel('名称', { exact: true }).fill(name);
    let writes = 0; const match = url => url.pathname === `/api/v2/nodes/${id}/api-tokens`;
    await page.route(match, async route => { if (route.request().method() !== 'POST') return route.continue(); writes++; const saved = await route.fetch(); assert.equal(saved.status(), 201); await route.abort('failed'); });
    await page.getByRole('button', { name: '生成令牌', exact: true }).click();
    await page.getByRole('button', { name: '查询原请求', exact: true }).click();
    await page.getByText('令牌已创建，秘密值无法重新读取。请关闭表单后显式重新生成。', { exact: true }).waitFor();
    assert.equal(writes, 1); assert(await page.getByRole('button', { name: '生成令牌', exact: true }).isDisabled());
    await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click(); await page.unroute(match);
    const row = page.getByRole('row').filter({ hasText: name });
    await row.getByRole('button', { name: '更多', exact: true }).click(); await page.getByRole('menuitem', { name: '重新生成', exact: true }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: '重新生成', exact: true }).click();
    const value = await page.getByLabel('令牌（仅显示一次）', { exact: true }).inputValue();
    assert(value && !await page.evaluate(secret => Object.values(sessionStorage).concat(Object.values(localStorage)).some(item => item.includes(secret)), value));
    await page.getByRole('button', { name: '完成', exact: true }).click();
  });
  await step('Problem 字段错误、请求 ID 和 429 等待', async () => {
    const limited = await browser.newContext();
    try {
      const p = await limited.newPage(); p.on('pageerror', error => errors.push(error.message));
      await p.goto(base + entry + (mode === 'recovery' ? '?builtin=1' : ''));
      let calls = 0; await p.route('**/api/v2/auth/sessions', route => { if (route.request().method() !== 'POST') return route.continue(); calls++; return route.fulfill({ status: 429, headers: { 'Content-Type': 'application/problem+json', 'Retry-After': '2', 'X-Request-ID': 'rate-browser' }, json: { type: 'urn:opsd:problem:rate_limited', title: '请求过于频繁', status: 429, detail: '请稍后重试', code: 'rate_limited', request_id: 'rate-browser', instance: 'urn:opsd:request:rate-browser', errors: [{ location: 'body', pointer: '/username', code: 'limited', message: '账号暂时受限' }] } }); });
      await p.getByLabel('账号', { exact: true }).fill('admin'); await p.getByLabel('密码', { exact: true }).fill(password); await p.getByRole('button', { name: '登录', exact: true }).click();
      await p.getByText('/username：账号暂时受限', { exact: true }).waitFor(); await p.getByText('请求 ID：rate-browser', { exact: true }).waitFor();
      assert(await p.getByRole('button', { name: '登录', exact: true }).isDisabled()); assert.equal(calls, 1);
      await p.getByRole('button', { name: '登录', exact: true }).waitFor({ state: 'visible' });
      for (let n = 0; n < 40 && await p.getByRole('button', { name: '登录', exact: true }).isDisabled(); n++) await p.waitForTimeout(100);
      assert(await p.getByRole('button', { name: '登录', exact: true }).isEnabled()); assert.equal(calls, 1);
    } finally { await limited.close(); }
  });
  config = (await api(`/nodes/${id}/configuration`)).body;
  const realNodes = (await api('/nodes')).body; nodeList = { ...realNodes, items: [{ ...realNodes.items[0], name: '本地验收节点', connected: true, writable: true }, { ...realNodes.items[0], id: 'offline-node', name: '远端离线', region: '其他', connected: false, writable: false }] };
  const metrics = { collected_at: checked, cpu_cores: 4, cpu_usage: 32.4, memory_used: 2 * 1073741824, memory_total: 4 * 1073741824, disk_used: 20 * 1073741824, disk_total: 80 * 1073741824, network_rx: 1048576, network_tx: 524288, errors: [] };
  overview = { updated_at: checked, nodes: [{ node: nodeList.items[0], reading: { node_id: id, state: 'ready', metrics, firewall: 'enabled', read: ready } }, { node: nodeList.items[1], reading: { node_id: 'offline-node', state: 'offline', firewall: 'unknown', read: { state: 'offline' } } }], summary: { metrics, valid_nodes: { cpu: 1, memory: 1, disk: 1, network: 1 } }, pending_operations: [], unavailable: [] };
  container = { node_id: id, node_name: nodeList.items[0].name, engine_id: 'docker', container_id: containerID, name: 'web', image: 'nginx:stable', state: 'running', health: 'healthy', ports: [], project: 'web', service: 'web', revision: rev };
  project = { id: 'web', engine_id: 'docker', name: 'web', directory: '/srv/web', files: ['/srv/web/compose.yaml'], environment_files: ['/srv/web/.env'], registered: false, services: [], checked_at: checked, node_id: id, node_name: nodeList.items[0].name, revision: rev };
  task = { id: 'daily', node_id: id, node_name: nodeList.items[0].name, revision: 1, name: '每日检查', schedule: '0 3 * * *', timezone: 'Asia/Shanghai', directory: '/', enabled: true, next_at: checked };
  const member = { node_id: id, instance_id: 'galera', name: 'MariaDB', address: '127.0.0.1:3306' }, proxy = { node_id: id, instance_id: 'proxy', name: 'ProxySQL', address: '127.0.0.1:6032' };
  const databaseState = { read: ready, state: 'ready', version: '11.8.9-MariaDB', cluster_id: 'local-test', cluster_size: 1, primary: true, ready: true, synced: true, read_only: false, connections: 12, receive_queue: 0, send_queue: 0, flow_control: 0, checked_at: checked, databases: [{ name: 'mysql', charset: 'utf8mb4', collation: 'utf8mb4_general_ci', bytes: 4096, protected: true }, { name: 'business', charset: 'utf8mb4', collation: 'utf8mb4_unicode_ci', bytes: 1048576, protected: false }], users: [{ name: 'root', host: 'localhost', protected: true, grants: [] }, { name: 'business', host: '%', protected: false, grants: [{ database: 'business', privileges: ['SELECT'] }] }], servers: [], pools: [], proxy_users: [], pending_changes: false };
  databaseValue = { cluster: { id: 'ui-db', name: '业务 Galera', coordinator: id, cluster_id: 'local-test', revision: 1, members: [member], proxies: [proxy] }, checked_at: checked, members: [{ ...member, status: databaseState }], proxies: [{ ...proxy, status: { ...databaseState, version: '3.0.3', servers: [{ hostgroup: 10, host: '127.0.0.1', port: 3306, state: 'ONLINE' }], pools: [], proxy_users: [] } }] };
  const capabilities = Object.fromEntries(['read','edit_rules','reorder','default_policy','enable','install','docker_ports','peer_trust_host_ipv4','peer_trust_host_ipv6','peer_trust_docker_ipv4','peer_trust_docker_ipv6'].map(key => [key, { supported: true }]));
  firewall = { backend: 'ufw', revision: 1, confirmed_revision: 1, state: 'in_sync', service_active: true, checked_at: checked, rules: [], default_policies: [], capabilities };
  await installFixtures(context);
  // 资源场景由通知驱动读取；前面的菜单跨会话场景使用真实后端 SSE。
  await context.addInitScript(() => {
    const events = []; window.__gpt2Events = events;
    window.EventSource = class extends EventTarget {
      constructor() { super(); events.push(this); queueMicrotask(() => this.dispatchEvent(new Event('ready'))); }
      close() { events.splice(events.indexOf(this), 1); }
    };
  });

  await step('资源历史节点集合、节点配置草稿和支持的写入', async () => {
    await go('/'); await page.getByLabel('地域', { exact: true }).click(); await page.getByRole('option', { name: '本地', exact: true }).click(); await wait(async () => historyQueries.includes(id), '地域筛选未使用节点集合查询历史');
    await go(`/nodes/${id}?tab=configuration`); await page.getByLabel('名称', { exact: true }).fill('保留草稿'); await page.getByRole('button', { name: '刷新', exact: true }).first().click(); assert.equal(await page.getByLabel('名称', { exact: true }).inputValue(), '保留草稿'); await page.getByRole('button', { name: '保存', exact: true }).click(); await page.locator('.operation-detail').waitFor(); assert(actions.some(a => a.kind === 'config' && a.body.name === '保留草稿')); await closeDrawers();
  });
  await step('容器读取、详情日志、操作与错误及离线只读', async () => {
    await go('/containers'); await page.getByRole('button', { name: '查看 web 详情与日志', exact: true }).click(); await page.getByRole('tab', { name: '最近日志', exact: true }).click(); await page.getByText('中文 UTF-8 正常', { exact: false }).waitFor(); await page.getByRole('button', { name: '关闭弹窗', exact: true }).click(); await page.getByRole('button', { name: '重启', exact: true }).click(); await page.getByRole('alertdialog').getByRole('button', { name: '重启', exact: true }).click(); await page.locator('.operation-detail').waitFor(); assert(actions.some(a => a.kind === 'container' && a.body.expected_revision === rev)); await closeDrawers();
    resourceMode = 'offline'; await page.getByRole('button', { name: '刷新', exact: true }).click(); await wait(() => page.getByRole('button', { name: '重启', exact: true }).isDisabled(), '离线容器仍可写'); resourceMode = 'empty'; await page.getByRole('button', { name: '刷新', exact: true }).click(); await page.getByRole('heading', { name: '暂无匹配容器', exact: true }).waitFor(); resourceMode = 'error'; await page.getByRole('button', { name: '刷新', exact: true }).click(); await page.getByRole('alert').filter({ hasText: '容器读取失败' }).waitFor(); resourceMode = 'normal'; await page.getByRole('button', { name: '重试', exact: true }).click();
  });
  await step('Compose 发现配置、条件登记与受支持操作', async () => {
    await go('/containers?tab=compose'); await page.getByRole('button', { name: '查看 web 配置', exact: true }).click(); await page.getByRole('button', { name: '登记项目', exact: true }).click(); await page.locator('.operation-detail').waitFor(); assert(actions.some(a => a.kind === 'compose-register' && a.headers['if-none-match'] === '*')); await closeDrawers(); await page.getByRole('button', { name: '拉取镜像', exact: true }).click(); await page.getByRole('alertdialog').getByRole('button', { name: '拉取镜像', exact: true }).click(); await page.locator('.operation-detail').waitFor(); assert(actions.some(a => a.kind === 'compose' && a.body.action === 'pull')); await closeDrawers();
  });
  await step('任务元数据、定义编辑启停、执行、取消与分页 UTF-8 日志', async () => {
    await go('/tools/tasks'); assert.equal(definitionReads, 0); await page.getByRole('button', { name: '编辑 每日检查', exact: true }).click(); await page.getByLabel('脚本', { exact: true }).fill('printf "中文脚本"\n'); await page.getByRole('switch', { name: '启用任务', exact: true }).click(); await page.getByRole('button', { name: '保存', exact: true }).click(); await page.locator('.operation-detail').waitFor(); assert(definitionReads >= 1); assert.equal(taskSaves.at(-1).body.enabled, false); assert.equal(taskSaves.at(-1).headers['if-match'], '"task:1"'); await closeDrawers();
    await page.getByRole('button', { name: '立即执行', exact: true }).click(); await page.getByRole('alertdialog').getByRole('button', { name: '立即执行', exact: true }).click(); await page.locator('.operation-detail').waitFor(); await page.getByRole('button', { name: '取消执行', exact: true }).click(); await page.getByRole('alertdialog').getByRole('button', { name: '取消执行', exact: true }).click(); await wait(() => page.locator('.operation-detail h2').innerText().then(t => t === '已取消'), '任务未显示取消'); await page.getByRole('button', { name: '加载更多', exact: true }).click(); await page.getByText('日志中文完成', { exact: false }).waitFor(); assert(!(await page.locator('.log-output').innerText()).includes('�')); await closeDrawers();
  });
  await step('数据库六页签、保护账号与受支持变更', async () => {
    await go('/databases/ui-db'); for (const name of ['概览','成员','库','账号','代理','备份']) { const tab = page.getByRole('tab', { name, exact: true }); if (await tab.getAttribute('aria-selected') !== 'true') await tab.click(); }
    await page.getByRole('tab', { name: '账号', exact: true }).click(); const protectedRow = page.getByRole('row').filter({ hasText: 'root' }); assert.equal(await protectedRow.getByRole('button').count(), 0);
    await page.getByRole('tab', { name: '库', exact: true }).click(); await page.getByRole('button', { name: '创建库', exact: true }).click(); await page.getByLabel('库名', { exact: true }).fill('browser_check'); await page.getByRole('dialog').getByRole('button', { name: '创建库', exact: true }).click(); await page.locator('.operation-detail').waitFor(); assert(actions.some(a => a.kind === 'database' && a.body.kind === 'create_database')); await closeDrawers();
  });
  await step('防火墙能力、规则写入、服务端确认期限和操作跨页保留', async () => {
    await go(`/firewall?node=${id}`); await page.getByRole('button', { name: '添加规则', exact: true }).click(); await page.getByLabel('端口 / 范围', { exact: true }).fill('8080'); await page.getByRole('button', { name: '预览', exact: true }).click(); await page.getByRole('button', { name: '保存', exact: true }).click(); await page.locator('.operation-detail').waitFor(); await page.getByText('剩余 17 秒', { exact: true }).waitFor(); await page.getByRole('button', { name: '关闭侧栏', exact: true }).click(); await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('link', { name: '节点', exact: true }).click(); await page.getByRole('button', { name: /^操作记录/ }).click(); await page.getByText('剩余 17 秒', { exact: true }).waitFor(); await page.getByRole('button', { name: '确认保留', exact: true }).click(); await wait(() => page.locator('.operation-detail h2').innerText().then(t => t === '已成功'), '确认未显示成功'); await closeDrawers();
    firewall.capabilities.edit_rules.supported = false; await go(`/firewall?node=${id}`); await wait(() => page.getByRole('button', { name: '添加规则', exact: true }).isDisabled(), '缺少能力仍可编辑'); firewall.capabilities.edit_rules.supported = true;
  });
  await step('防火墙版本冲突后保留草稿并重新读取', async () => {
    firewall.capabilities.edit_rules.supported = true; conflictFirewall = true;
    await go(`/firewall?node=${id}`); await page.getByRole('button', { name: '添加规则', exact: true }).click();
    await page.getByLabel('端口 / 范围', { exact: true }).fill('8443'); await page.getByRole('button', { name: '预览', exact: true }).click(); await page.getByRole('button', { name: '保存', exact: true }).click();
    await page.getByRole('button', { name: '重新读取', exact: true }).click();
    assert.equal(await page.getByLabel('端口 / 范围', { exact: true }).inputValue(), '8443');
    await wait(() => page.getByRole('button', { name: '保存', exact: true }).isEnabled(), '重读后未恢复保存');
    await page.getByRole('button', { name: '保存', exact: true }).click(); await page.locator('.operation-detail').waitFor();
    assert.equal(actions.filter(action => action.kind === 'firewall').at(-1).body.expected_revision, firewall.revision); await closeDrawers();
  });
  await step('节点断连时保留资源内容并禁止写入', async () => {
    const node = nodeList.items[0]; nodeList.items[0] = { ...node, connected: false, writable: false }; project.registered = true;
    try { await go('/containers'); await wait(() => page.getByRole('button',{name:'重启',exact:true}).isDisabled(),'节点断连后容器仍可写'); await go('/containers?tab=compose'); await wait(() => page.getByRole('button',{name:'拉取镜像',exact:true}).isDisabled(),'节点断连后 Compose 仍可写'); await go('/tools/tasks'); await wait(() => page.getByRole('button',{name:'立即执行',exact:true}).isDisabled(),'节点断连后任务仍可执行'); } finally { nodeList.items[0] = node; }
    const pending = op('firewall_config','awaiting_confirmation'); await go('/tools/tasks'); await page.getByRole('button',{name:/^操作记录/}).click(); await page.locator('.operation-row').filter({hasText:pending.id}).click(); await page.getByText('剩余 17 秒',{exact:true}).waitFor(); operationReadError = true; try { await page.getByRole('alert').filter({hasText:'操作状态读取失败'}).waitFor(); assert(await page.getByRole('button',{name:'确认保留',exact:true}).isDisabled()); } finally { operationReadError=false; } await page.getByRole('button',{name:'重试',exact:true}).click(); await wait(() => page.getByRole('button',{name:'确认保留',exact:true}).isEnabled(),'重新读取后未恢复操作'); await closeDrawers();
  });
  await step('部分分页失败保留内容并从原游标继续读取', async () => {
    paging = true; pageAvailable = false; nextPageReads = 0;
    try {
      await go('/containers'); await page.getByRole('button', { name: '继续读取', exact: true }).waitFor();
      await page.getByText('后续页暂时不可用', { exact: true }).waitFor(); assert.equal(nextPageReads, 1);
      assert(await page.getByRole('button', { name: '查看 web 详情与日志', exact: true }).isVisible());
      pageAvailable = true; await page.getByRole('button', { name: '继续读取', exact: true }).click();
      await page.getByRole('button', { name: '查看 worker 详情与日志', exact: true }).waitFor();
      assert.equal(nextPageReads, 2); assert.equal(await page.getByRole('button', { name: '继续读取', exact: true }).count(), 0);
      assert(!(await page.locator('.notice').allTextContents()).join('').includes('分页节点：离线'));
    } finally { paging = false; }
  });
  await step('弹窗内断连限制与 SSE 所属节点过滤', async () => {
    const node = nodeList.items[0]; project.registered = true;
    const notify = async (resource, nodeID, revision) => page.evaluate(change => { for (const events of window.__gpt2Events) events.dispatchEvent(new MessageEvent('invalidate', { data: JSON.stringify({ changes: [change] }) })); }, { resource, node_id: nodeID, revision });
    try {
      await go('/containers?tab=compose'); await page.getByRole('button', { name: '查看 web 配置', exact: true }).click();
      await wait(() => page.getByRole('button', { name: '更新登记', exact: true }).isEnabled(), '登记表单未读取配置');
      nodeList.items[0] = { ...node, connected: false, writable: false }; await notify('nodes', id, 101);
      await wait(() => page.getByRole('button', { name: '更新登记', exact: true }).isDisabled(), '已打开的登记表单断连后仍可写');
      await page.getByRole('button', { name: '关闭弹窗', exact: true }).click();
      nodeList.items[0] = node; await notify('nodes', id, 102);
      await go('/tools/tasks'); await page.getByRole('button', { name: '编辑 每日检查', exact: true }).click();
      await wait(() => page.getByRole('button', { name: '保存', exact: true }).isEnabled(), '任务定义未读取');
      nodeList.items[0] = { ...node, connected: false, writable: false }; await notify('nodes', id, 103);
      await wait(() => page.getByRole('button', { name: '保存', exact: true }).isDisabled(), '已打开的任务表单断连后仍可写');
      await page.getByRole('button', { name: '关闭弹窗', exact: true }).click();
      nodeList.items[0] = node; await notify('nodes', id, 104);
      const before = counts['GET /settings/navigation']; await notify('navigation', 'other-node', 1); await page.waitForTimeout(150);
      assert.equal(counts['GET /settings/navigation'], before, '其他节点的菜单通知刷新了当前账号');
    } finally { nodeList.items[0] = node; }
  });
  await step('跨节点写入未知应答保留原目标与暂时 404', async () => {
    const remote = 'coordinator-remote', previous = databaseValue, nodes = nodeList.items;
    nodeList.items = [...nodes, { ...nodes[0], id: remote, name: '远端协调节点' }];
    databaseValue = { ...databaseValue, cluster: { ...databaseValue.cluster, coordinator: remote }, members: databaseValue.members.map(member => ({ ...member, node_id: remote })) };
    lostDatabase = true; missingReceipt = true;
    const before = actions.filter(action => action.kind === 'database').length;
    try {
      await go('/databases/ui-db'); await page.getByRole('tab', { name: '库', exact: true }).click(); await page.getByRole('button', { name: '创建库', exact: true }).click();
      await page.getByLabel('库名', { exact: true }).fill('unknown_remote'); await page.getByRole('dialog').getByRole('button', { name: '创建库', exact: true }).click();
      await page.getByRole('dialog').getByRole('button', { name: '查询原请求', exact: true }).click(); const recovery = page.locator('.request-recovery');
      await recovery.getByRole('button', { name: '查询原请求', exact: true }).click(); await recovery.getByText('原请求暂未找到', { exact: true }).waitFor();
      assert.equal(actions.filter(action => action.kind === 'database').length, before + 1);
      await recovery.getByRole('button', { name: '查询原请求', exact: true }).click(); await page.locator('.operation-detail').waitFor(); assert.equal(receiptTargets.at(-1), remote);
      assert.equal(actions.filter(action => action.kind === 'database').length, before + 1); await closeDrawers();
      await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click();
    } finally { lostDatabase = false; missingReceipt = false; nodeList.items = nodes; databaseValue = previous; }
  });
  let tickets = 0, socketsClosed = 0, terminalMode = 'normal'; const inputs = [], sizes = [], sockets = [];
  await context.route('**/api/v2/nodes/*/terminal', route => { tickets++; if (terminalMode === 'rate') return route.fulfill({ status: 429, json: { detail: '终端连接过多，请稍后重试' } }); return route.fulfill({ status: 201, json: { url: `/api/v2/terminals/browser-${tickets}`, expires_at: new Date(Date.now() + (terminalMode === 'expired' ? -1000 : 30000)).toISOString() } }); });
  await context.routeWebSocket('**/api/v2/terminals/*', socket => { sockets.push(socket); const bytes = Buffer.from('\u001b[32m中文 UTF-8 正常\u001b[0m\r\n'); for (let n = 0; n < bytes.length; n += 5) socket.send(JSON.stringify({ type: 'output', data: bytes.subarray(n, n + 5).toString('base64') })); socket.onMessage(message => { const frame = JSON.parse(String(message)); if (frame.type === 'input') { inputs.push(Buffer.from(frame.data, 'base64').toString('utf8')); socket.send(JSON.stringify({ type: 'output', data: frame.data })); } else if (frame.type === 'resize') sizes.push(frame); }); socket.onClose(() => socketsClosed++); });
  await step('条件写入丢失应答后的资源恢复与单次请求', async () => {
    let lost = false, writes = 0; const match = url => url.pathname === '/api/v2/settings/navigation';
    await page.route(match, async route => { if (route.request().method() !== 'PUT' || lost) return route.continue(); lost = true; writes++; const response = await route.fetch(); assert(response.ok()); await route.abort('failed'); });
    await go('/settings/appearance'); await page.getByRole('button',{name:'存储上移',exact:true}).click(); await page.getByRole('button',{name:'保存',exact:true}).click(); await page.getByRole('button',{name:'重新读取',exact:true}).waitFor();
    await page.getByRole('button',{name:/^操作记录/}).click(); await page.getByRole('button',{name:'读取原资源',exact:true}).click(); await page.getByText('已读取当前资源，请到原页面核对。',{exact:true}).waitFor(); await page.getByRole('button',{name:'已核对',exact:true}).click(); await closeDrawers();
    await page.getByRole('button',{name:'重新读取',exact:true}).click(); assert.equal(writes,1); await page.unroute(match); await page.getByRole('button',{name:'恢复默认',exact:true}).click(); await page.getByRole('button',{name:'保存',exact:true}).click(); await wait(async()=>(await api('/settings/navigation')).body.order[3]==='databases','恢复后未保存默认顺序');
  });
  await step('终端字节、输入尺寸、深层别名、跨页输出、重连及票据错误', async () => {
    await go(`/terminal?node=${id}`); await page.waitForURL(base + prefix + '/tools/terminal?session=*'); await page.locator('.terminal-surface[data-ready]').waitFor(); await page.getByText('已连接', { exact: true }).waitFor(); await page.locator('.xterm-accessibility-tree').getByText('中文 UTF-8 正常', { exact: false }).waitFor(); await page.locator('.xterm-helper-textarea').focus(); await page.keyboard.type('echo '); await page.locator('.xterm-helper-textarea').evaluate(async el => { const text = '输入中文'; el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true })); el.value += text; el.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: text })); await new Promise(resolve => setTimeout(resolve, 10)); el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: text })); await new Promise(resolve => setTimeout(resolve, 10)); }); await page.keyboard.press('Enter'); await wait(async () => inputs.join('').includes('输入中文') && sizes.length > 0, `终端输入或尺寸未发送 ${JSON.stringify({inputs,sizes})}`); assert.equal(tickets, 1); const before = inputs.length;
    await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('link', { name: '节点', exact: true }).click(); assert.equal(socketsClosed, 0); await page.getByRole('navigation', { name: '会话导航', exact: true }).getByRole('link').first().click(); assert.equal(tickets, 1); await page.locator('.xterm-accessibility-tree').getByText('输入中文', { exact: false }).waitFor();
    await page.getByRole('button',{name:'新建会话',exact:true}).click(); await page.getByRole('menuitem',{name:/本地验收节点/}).click(); await page.locator('.node-picker').waitFor({ state: 'hidden' }); await wait(async()=>tickets===2 && await page.locator('.sidebar-session').count()===2,'第二个会话未独立创建'); await page.getByText('已连接',{exact:true}).waitFor(); await page.getByRole('button',{name:'终端更多操作',exact:true}).click(); await page.getByRole('menuitem',{name:'关闭会话',exact:true}).click(); assert.equal(await page.locator('.sidebar-session').count(),1);
    await page.getByRole('button', { name: '终端更多操作', exact: true }).click(); await page.getByRole('menuitem', { name: '断开会话', exact: true }).click(); await page.getByRole('button', { name: '重新连接', exact: true }).click(); await page.getByText('已连接', { exact: true }).waitFor(); assert.equal(tickets, 3); assert.equal(inputs.length, before);
    await page.getByRole('button', { name: '终端更多操作', exact: true }).click(); await page.getByRole('menuitem', { name: '断开会话', exact: true }).click(); terminalMode = 'expired'; await page.getByRole('button', { name: '重新连接', exact: true }).click(); await page.getByText('终端票据已过期，请重新连接', { exact: true }).waitFor(); terminalMode = 'rate'; await page.getByRole('button', { name: '重新连接', exact: true }).click(); await page.getByText('终端连接过多，请稍后重试', { exact: true }).waitFor(); terminalMode = 'normal'; await page.getByRole('button', { name: '重新连接', exact: true }).click(); await page.getByText('已连接', { exact: true }).waitFor(); sockets.at(-1).send(JSON.stringify({type:'exit',message:'进程已退出（测试）'})); await page.getByText('进程已退出（测试）',{exact:true}).waitFor(); await page.getByRole('button',{name:'重新连接',exact:true}).click(); await page.getByText('已连接',{exact:true}).waitFor();
  });
  await step('深浅模式、320/768/1440px、200% 等效视口与键盘焦点', async () => {
    const routes = [['overview','/'], ['nodes','/nodes'], ['node',`/nodes/${id}`], ['containers','/containers'], ['compose','/containers?tab=compose'], ['database','/databases/ui-db'], ['storage','/storage'], ['firewall',`/firewall?node=${id}`], ['tasks','/tools/tasks'], ['appearance','/settings/appearance'], ['sessions','/settings/sessions'], ['api','/settings/api']];
    if (!(resume && previous.captures?.some(item => item.name === 'light-1440-terminal'))) for (const theme of ['dark','light']) { await page.evaluate(theme => localStorage.setItem('opsd.frontend-gpt2.theme', theme), theme); for (const width of [320,768,1440]) { await page.setViewportSize({ width, height: 950 }); for (const [name,path] of routes) { await go(path); await capture(`${theme}-${width}-${name}`); } await go('/tools/terminal'); await capture(`${theme}-${width}-terminal`); assert.equal(await page.locator('.terminal-workspace').evaluate(e => getComputedStyle(e).backgroundColor), 'rgb(0, 0, 0)'); } }
    await page.setViewportSize({ width:720, height:450 }); await go('/settings/appearance'); await capture('zoom200-appearance'); await go('/tools/terminal'); await capture('zoom200-terminal'); await page.getByRole('button', { name:'终端更多操作',exact:true }).focus(); await page.keyboard.press('Enter'); await page.keyboard.press('Escape'); await wait(() => page.getByRole('button', { name:'终端更多操作',exact:true }).evaluate(e => e === document.activeElement), 'Escape 后未恢复菜单入口焦点');
    await page.setViewportSize({ width:320, height:900 }); await page.getByRole('button', { name:'打开导航',exact:true }).click(); await page.getByRole('navigation', { name:'主导航',exact:true }).getByRole('link',{name:'设置',exact:true}).click(); await page.getByRole('button', { name:'打开导航',exact:true }).click(); assert(await page.getByRole('navigation',{name:'设置分类',exact:true}).getByRole('link',{name:'外观',exact:true}).isVisible()); await page.getByRole('button',{name:'关闭侧栏',exact:true}).click();
  });
  await step('账号菜单、退出释放终端与会话失效', async () => {
    await page.setViewportSize({ width:1440,height:1000 }); await go(`/tools/terminal?node=${id}`); await page.getByText('已连接',{exact:true}).waitFor(); await page.getByRole('button',{name:'账号菜单 admin',exact:true}).click(); assert(await page.getByRole('menuitem',{name:'账号设置',exact:true}).isVisible()); assert(await page.getByRole('menuitem',{name:'内置界面',exact:true}).isVisible()); const before = socketsClosed; await page.getByRole('menuitem',{name:'退出',exact:true}).click(); await page.waitForURL(base+entry+(mode === 'recovery' ? '?builtin=1' : '')); await wait(async()=>socketsClosed>before,'退出未释放终端'); const stores=await page.evaluate(()=>JSON.stringify({...sessionStorage})); assert(!stores.includes('opsd.frontend-gpt2.requests.'));
    await login(page); const current=(await(await context.request.get(base+'/api/v2/auth/sessions/current')).json()); await context.request.delete(base+'/api/v2/auth/sessions/current',{headers:{Origin:base,'X-CSRF-Token':current.csrf_token}}); await page.evaluate(() => { for (const events of window.__gpt2Events) events.dispatchEvent(new Event('ready')); }); await page.waitForURL(base+entry+(mode === 'recovery' ? '?builtin=1' : ''));
  });
  await step('主题本地安装与内置恢复入口', async () => {
    const recovered = await apiContext.get(base+'/__opsd/builtin/settings/appearance'); assert.equal(recovered.status(),200); assert(!(await recovered.text()).includes(installation.installation_id)); const missing=await apiContext.get(base+`/__opsd/themes/${installation.installation_id}/missing.js`);assert.equal(missing.status(),404); const current = await context.request.get(base+'/api/v2/auth/sessions/current'); if (current.ok()) { const value = await current.json(); await context.request.delete(base+'/api/v2/auth/sessions/current',{headers:{Origin:base,'X-CSRF-Token':value.csrf_token}}); } await page.goto(base+entry+'?builtin=1'); await page.getByRole('heading',{name:'登录',exact:true}).waitFor();
  });
  assert.deepEqual(errors, []);
} catch (error) { console.error(error); if(page) await page.screenshot({path:join(output,'failure.png'),fullPage:true,mask:[page.getByLabel('令牌（仅显示一次）',{exact:true}),page.getByLabel('验证命令',{exact:true}),page.getByLabel('Prometheus 配置',{exact:true}),page.getByLabel('一次性安装命令',{exact:true})]}).catch(()=>{}); process.exitCode=1; }
finally { await writeFile(join(output,'result.json'),JSON.stringify({ mode, results: [...results, ...previous.results.filter(item => !results.some(r => r.name === item.name))], errors, captures: [...(previous.captures ?? []), ...captures], counts: {...(previous.counts ?? {}),...counts}, actions: [...(previous.actions ?? []), ...actions], taskSaves: [...(previous.taskSaves ?? []), ...taskSaves], operationLists, installation },null,2)); await browser.close(); await apiContext.dispose(); }
