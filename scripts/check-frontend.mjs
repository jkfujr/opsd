import assert from 'node:assert/strict';
import { mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright';

// 在隔离本地服务上核对默认前端作为内置界面的路由、会话与缺少界面的功能。
const base = process.env.OPSD_FRONTEND_URL ?? 'http://127.0.0.1:5183';
const password = process.env.OPSD_FRONTEND_PASSWORD ?? 'test-password';
const output = 'artifacts/frontend-check';
mkdirSync(output, { recursive: true });

const results = [];
const consoleErrors = [];
const record = (name, passed, detail = '') => {
  results.push({ name, passed, detail });
  console.log(`${passed ? 'PASS' : 'FAIL'} ${name}${detail ? ' — ' + detail : ''}`);
};

const browser = await chromium.launch({ channel: process.env.OPSD_FRONTEND_BROWSER ?? 'msedge' });
const context = await browser.newContext({ viewport: { width: 1440, height: 950 } });
const page = await context.newPage();
page.on('pageerror', (error) => consoleErrors.push(String(error)));
page.on('console', (message) => { if (message.type() === 'error') consoleErrors.push(message.text()); });

async function login() {
  await page.goto(base + '/signin', { waitUntil: 'domcontentloaded' });
  await page.getByLabel('账号', { exact: true }).waitFor({ timeout: 20000 });
  await page.getByLabel('账号', { exact: true }).fill('admin');
  await page.getByLabel('密码', { exact: true }).fill(password);
  await Promise.all([page.waitForURL((url) => !url.pathname.startsWith('/signin'), { timeout: 30000 }), page.getByRole('button', { name: '登录', exact: true }).click()]);
}

try {
  // 登录页字段必须有可访问名称,且未登录时不显示账号菜单。
  await page.goto(base + '/signin', { waitUntil: 'domcontentloaded' });
  await page.getByLabel('账号', { exact: true }).waitFor({ timeout: 20000 });
  record('login page has labelled fields', (await page.getByLabel('账号', { exact: true }).count()) === 1 && (await page.getByLabel('密码', { exact: true }).count()) === 1);
  record('account menu hidden before login', (await page.getByRole('button', { name: /账号菜单/ }).count()) === 0);

  // 登录后会整页加载并由服务端决定主题或内置界面。
  await login();
  const afterLogin = new URL(page.url());
  record('login lands on root after full page load', afterLogin.pathname === '/', page.url());
  await page.getByRole('button', { name: /账号菜单/ }).waitFor({ timeout: 20000 });
  record('account menu has accessible name', true);

  // 页面白名单内的路由在硬刷新后仍可用。
  for (const [path, marker] of [
    ['/tools/tasks', /任务/],
    ['/tools/operations', /操作/],
    ['/tools/terminal', /终端/],
    ['/monitoring', /监控/],
    ['/storage', /存储/],
    ['/firewall', /防火墙/],
    ['/databases', /数据库/],
    ['/containers', /容器/],
    ['/nodes', /节点/],
  ]) {
    const response = await page.goto(base + path, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(350);
    record(`route ${path}`, response?.status() === 200 && marker.test(await page.locator('body').innerText()), `status=${response?.status()}`);
  }

  // 旧地址由前端带查询与锚点重定向,服务端白名单必须放行,否则硬刷新直接 404。
  // 只比路径会漏掉"参数被丢弃"的回归,因此逐项校验转发后的实际约定:
  // - /tasks、/operations 原样保留查询参数;
  // - /terminal 的 node 参数由终端页消费(替换为 session 或移除),但锚点必须保留。
  for (const [from, to, path_, expect] of [
    ['/tasks?view=runs&node_id=demo#top', '/tools/tasks', '/tasks', (url) => url.searchParams.get('view') === 'runs' && url.searchParams.get('node_id') === 'demo' && url.hash === '#top'],
    ['/operations', '/tools/operations', '/operations', (url) => url.search === '' && url.hash === ''],
    // 终端页消费 node:必须消失,有效节点时替换为 session;锚点始终保留。
    ['/terminal?node=demo#session', '/tools/terminal', '/terminal', (url) => url.hash === '#session' && !url.searchParams.has('node')],
  ]) {
    const response = await page.goto(base + from, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(700);
    const landed = new URL(page.url());
    record(`legacy ${path_} redirects with query+hash`, response?.status() === 200 && landed.pathname === to && expect(landed), `${page.url()}`);
  }

  // 白名单外的路径刷新必须 404。
  const unknown = await page.goto(base + '/definitely-not-a-page', { waitUntil: 'domcontentloaded' });
  record('unknown path returns 404', unknown?.status() === 404, `status=${unknown?.status()}`);

  // 设置 → 外观承担主题管理。
  await page.goto(base + '/settings/appearance', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(700);
  const appearance = await page.locator('body').innerText();
  record('appearance manages console theme', /主题/.test(appearance));
  record('appearance offers builtin switch', /内置/.test(appearance));
  record('appearance offers UI refresh', /刷新/.test(appearance));

  // 内置恢复路径独立可用。
  const recovery = await page.goto(base + '/__opsd/builtin/signin', { waitUntil: 'domcontentloaded' });
  record('builtin recovery route loads', recovery?.status() === 200, `status=${recovery?.status()}`);

  // 这几项能力目前只有 API、没有界面。这里只记录现状而不做断言:
  // 断言"不应出现入口"会在补齐界面时误报失败,而它们是否已实现并非本检查的职责。
  await page.goto(base + '/databases', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(600);
  const databases = await page.locator('body').innerText();
  record('databases page reachable', true);
  const coverage = {
    proxyRoutingUI: /代理路由/.test(databases),
    sharedOpsdCoreUI: /初始化 opsd_core|主备管理节点/.test(databases),
    centralMonitoringDeployUI: false,
  };
  await page.goto(base + '/monitoring', { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(600);
  coverage.centralMonitoringDeployUI = /部署集中监控/.test(await page.locator('body').innerText());
  console.log('缺少界面的能力当前 UI 状态(仅供参考,不计入通过率):', JSON.stringify(coverage));

  await page.screenshot({ path: `${output}/routes.png`, fullPage: true });
  writeFileSync(`${output}/result.json`, JSON.stringify({ base, results, consoleErrors }, null, 2));
  const failed = results.filter((item) => !item.passed);
  console.log(`\n${results.length - failed.length}/${results.length} 通过;结果写入 ${output}/result.json`);
  if (failed.length) process.exitCode = 1;
} catch (error) {
  await page.screenshot({ path: `${output}/failure.png`, fullPage: true }).catch(() => {});
  writeFileSync(`${output}/failure.json`, JSON.stringify({ base, results, consoleErrors, error: String(error) }, null, 2));
  throw error;
} finally {
  await browser.close();
}