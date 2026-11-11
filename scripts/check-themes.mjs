import assert from 'node:assert/strict';
import { mkdirSync, readFileSync, writeFileSync, existsSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { chromium } from 'playwright';

const output = resolve('artifacts/theme-ui');
const builtin = '/__opsd/builtin/';

async function buildThemes() {
  const { build } = await import('../frontend/gpt2/node_modules/vite/dist/node/index.js');
  const vue = resolve(process.env.OPSD_THEME_VUE ?? join(output, 'vue-runtime/node_modules/vue/dist/vue.runtime.esm-bundler.js'));
  assert(existsSync(vue), '请先安装 Vue 验证依赖：npm install --prefix artifacts/theme-ui/vue-runtime --no-save --package-lock=false vue@3.5.45');
  const version = JSON.parse(readFileSync('package.json', 'utf8')).version;
  const common = `
    async function request(path, options = {}) {
      const response = await fetch('/api/v2' + path, { credentials: 'same-origin', ...options });
      if (!response.ok) throw new Error((await response.json()).detail);
      return response.status === 204 ? null : response.json();
    }
    async function load() {
      const response = await fetch('/api/v2/auth/sessions/current', { credentials: 'same-origin' });
      if (response.status === 401) return { loading: false, session: null, names: [] };
      if (!response.ok) throw new Error('读取会话失败');
      const session = await response.json();
      const nodes = await request('/nodes');
      return { loading: false, session, names: nodes.items.map((item) => item.name) };
    }
    async function login(event, setError) {
      event.preventDefault();
      const fields = new FormData(event.currentTarget);
      try {
        await request('/auth/sessions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ entrance: location.pathname, username: fields.get('username'), password: fields.get('password') }) });
        location.assign('/');
      } catch (error) { setError(error.message); }
    }
    async function logout(session) {
      await request('/auth/sessions/current', { method: 'DELETE', headers: { 'X-CSRF-Token': session.csrf_token } });
      location.assign(session.login_address);
    }
  `;
  for (const framework of ['react', 'vue']) {
    const root = join(output, framework);
    mkdirSync(root, { recursive: true });
    const name = `${framework === 'react' ? 'React' : 'Vue'} 整包主题`;
    writeFileSync(join(root, 'theme.json'), JSON.stringify({ format_version: 1, id: `example-${framework}`, name, version: '1.0.0', api_version: 2, min_opsd_version: version, login: framework === 'vue' ? 'theme' : 'builtin' }, null, 2));
    writeFileSync(join(root, 'index.html'), '<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>OPSD 主题验证</title></head><body><main id="app"></main><script type="module" src="./main.js"></script></body></html>');
    const source = framework === 'react' ? `
      import React from 'react';
      import { createRoot } from 'react-dom/client';
      ${common}
      const h = React.createElement;
      function App() {
        const [state, setState] = React.useState({ loading: true, names: [] });
        React.useEffect(() => { load().then(setState).catch((error) => setState({ loading: false, names: [], error: error.message })); }, []);
        return h('section', null, h('h1', null, 'React 整包主题'), state.loading ? h('p', null, '正在读取') : h('ul', null, ...state.names.map((name) => h('li', { key: name }, name))),
          state.error && h('p', { role: 'alert' }, state.error), h('a', { href: '${builtin}settings/appearance' }, '内置外观'),
          state.session && h('button', { onClick: () => logout(state.session) }, '主题退出'));
      }
      createRoot(document.getElementById('app')).render(h(App));
    ` : `
      import { createApp, h, reactive, onMounted } from 'vue';
      ${common}
      createApp({ setup() {
        const state = reactive({ loading: true, session: null, names: [], error: '' });
        onMounted(() => load().then((next) => Object.assign(state, next)).catch((error) => { state.loading = false; state.error = error.message; }));
        return () => h('section', [h('h1', 'Vue 整包主题'), state.loading ? h('p', '正在读取') : state.session ? h('ul', state.names.map((name) => h('li', name))) : h('form', { onSubmit: (event) => login(event, (message) => state.error = message) }, [
          h('label', ['主题账号', h('input', { name: 'username', required: true, autocomplete: 'username' })]),
          h('label', ['主题密码', h('input', { name: 'password', type: 'password', required: true, autocomplete: 'current-password' })]),
          h('button', { type: 'submit' }, '主题登录')]),
          state.error && h('p', { role: 'alert' }, state.error), h('a', { href: '${builtin}settings/appearance' }, '内置外观'),
          state.session && h('button', { onClick: () => logout(state.session) }, '主题退出')]);
      }}).mount('#app');
    `;
    writeFileSync(join(root, 'main.js'), source);
    await build({ configFile: false, root, base: './', logLevel: 'warn', resolve: { alias: framework === 'react' ? [
      { find: /^react-dom\/client$/, replacement: resolve('frontend/gpt2/node_modules/react-dom/client.js') },
      { find: /^react$/, replacement: resolve('frontend/gpt2/node_modules/react/index.js') },
    ] : [{ find: /^vue$/, replacement: vue }] }, define: { __VUE_OPTIONS_API__: 'false', __VUE_PROD_DEVTOOLS__: 'false', __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: 'false' }, build: { outDir: 'dist', emptyOutDir: true } });
  }
  const broken = join(output, 'broken');
  mkdirSync(join(broken, 'dist'), { recursive: true });
  writeFileSync(join(broken, 'theme.json'), JSON.stringify({ format_version: 1, id: 'example-broken', name: '错误主题', version: '1.0.0', api_version: 2, min_opsd_version: version, login: 'theme' }));
  writeFileSync(join(broken, 'dist/index.html'), '<!doctype html><html><head><meta charset="UTF-8"></head><body><script src="./broken.js"></script></body></html>');
  writeFileSync(join(broken, 'dist/broken.js'), 'throw new Error("主题验证：预期的运行错误");');
  console.log('React、Vue 和错误主题已构建');
}

async function checkThemes() {
  const base = process.env.OPSD_THEME_URL;
  assert(base, '请通过 TestConsoleThemeBrowser 启动验证服务');
  const browser = await chromium.launch(process.env.OPSD_THEME_BROWSER ? { channel: process.env.OPSD_THEME_BROWSER } : {});
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  page.on('dialog', (dialog) => dialog.accept());
  const visible = async (locator) => { await locator.waitFor({ state: 'visible' }); };
  const goAppearance = async () => { await page.goto(base + builtin + 'settings/appearance'); await visible(page.getByRole('button', { name: '切回内置', exact: true })); };
  const submitBuiltinLogin = async () => {
    await page.getByLabel('账号', { exact: true }).fill('admin');
    await page.getByLabel('密码', { exact: true }).fill('test-password');
    await page.getByRole('button', { name: '登录', exact: true }).click();
  };
  const row = (name) => page.getByRole('row').filter({ hasText: name });
  const install = async (framework, name, waitForRow = true) => {
    await page.getByLabel('ZIP 文件（最大 100 MiB）', { exact: true }).setInputFiles(join(output, `${framework}.zip`));
    await page.getByRole('button', { name: '上传并安装', exact: true }).click();
    if (waitForRow) await visible(row(name));
  };
  const activate = async (name) => {
    const response = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v2/console/theme-selection' && r.request().method() === 'PUT');
    await row(name).getByRole('button', { name: '启用', exact: true }).click();
    const saved = await response;
    assert.equal(saved.status(), 200, await saved.text());
    await visible(page.getByRole('status').filter({ hasText: '已切换主题' }));
    await page.getByRole('button', { name: '刷新界面', exact: true }).click();
  };
  const checkDocumentation = async () => {
    const anonymous = await browser.newContext();
    try {
      assert.equal((await anonymous.request.get(base + '/api/v2/docs')).status(), 200);
      assert.equal((await anonymous.request.get(base + '/api/v2/openapi.json')).status(), 401);
    } finally { await anonymous.close(); }
    const docs = await context.newPage();
    docs.on('pageerror', (error) => errors.push(error.message));
    try {
      await docs.goto(base + '/api/v2/docs');
      const operation = async (id) => {
        const block = docs.locator(`[id$="-${id}"]`);
        await block.locator('.opblock-summary').click();
        await block.getByRole('button', { name: 'Try it out', exact: true }).click();
        return block;
      };
      const execute = async (block, method, path) => {
        const response = docs.waitForResponse((r) => new URL(r.url()).pathname === '/api/v2' + path && r.request().method() === method);
        await block.getByRole('button', { name: 'Execute', exact: true }).click();
        return response;
      };
      const identity = await execute(await operation('getAPIIdentity'), 'GET', '/');
      assert.equal(identity.status(), 200);
      const node = (await identity.json()).node_id;
      const selection = await context.request.get(base + '/api/v2/console/theme-selection');
      const write = await operation('activateTheme');
      await write.locator('tr[data-param-name="If-Match"] input').fill(selection.headers().etag);
      await write.locator('textarea.body-param__text').fill(JSON.stringify({ installation_id: 'builtin' }));
      const saved = await execute(write, 'PUT', '/console/theme-selection');
      assert.equal(saved.status(), 200, await saved.text());
      assert(saved.request().headers()['x-csrf-token'], '文档会话写入未附加 CSRF');

      const session = await (await context.request.get(base + '/api/v2/auth/sessions/current')).json();
      const tokenResponse = await context.request.post(base + `/api/v2/nodes/${node}/api-tokens`, { headers: { Origin: base, 'X-CSRF-Token': session.csrf_token, 'Idempotency-Key': 'documentation-token' }, data: { name: '在线文档', enabled: true, grants: [{ module: 'nodes', level: 'manage', scope: 'nodes', resources: [node] }, { module: 'monitoring', level: 'read', scope: 'nodes', resources: [node] }], source_ips: [] } });
      assert.equal(tokenResponse.status(), 201, await tokenResponse.text());
      const token = await tokenResponse.json();
      await docs.locator('#token').fill(token.value);
      await docs.getByRole('button', { name: '打开接口文档', exact: true }).click();
      // 新实例渲染后再展开，Bearer 始终覆盖同一浏览器的 Cookie。
      const tokenIdentity = await execute(await operation('getAPIIdentity'), 'GET', '/');
      assert.equal((await tokenIdentity.json()).principal.kind, 'token');
      const metrics = await operation('getMetrics');
      const metricsResponse = docs.waitForResponse((r) => new URL(r.url()).pathname === '/metrics');
      await metrics.getByRole('button', { name: 'Execute', exact: true }).click();
      const measured = await metricsResponse;
      assert.equal(measured.status(), 200, await measured.text());
      assert.match(await measured.text(), /opsd_node_up/);
      const headers = { Authorization: 'Bearer ' + token.value };
      const current = await context.request.get(base + `/api/v2/nodes/${node}`, { headers });
      const rename = await operation('renameNode');
      await rename.locator('tr[data-param-name="node_id"] input').fill(node);
      await rename.locator('tr[data-param-name="If-Match"] input').fill(current.headers().etag);
      await rename.locator('tr[data-param-name="Idempotency-Key"] input').fill('documentation-write');
      await rename.locator('textarea.body-param__text').fill(JSON.stringify({ name: '主题验收节点' }));
      const accepted = await execute(rename, 'PATCH', `/nodes/${node}`);
      assert.equal(accepted.status(), 202, await accepted.text());
      const original = await accepted.json();
      const receipt = await context.request.get(base + `/api/v2/requests/documentation-write?node_id=${node}`, { headers });
      assert.equal(receipt.status(), 200, await receipt.text());
      assert.equal((await receipt.json()).operation_id, original.id);
      assert.equal((await context.request.get(base + '/api/v2/tasks', { headers })).status(), 403);
      assert.equal(await docs.evaluate((token) => [...Object.values(localStorage), ...Object.values(sessionStorage)].some((value) => value.includes(token)), token.value), false);
    } finally { await docs.close(); }
  };
  try {
    await page.goto(base + '/signin?builtin=1');
    await submitBuiltinLogin();
    await page.waitForURL(base + builtin);
    await checkDocumentation();
    await goAppearance();
    // 服务端已完成安装但应答丢失时，只读取列表，不自动重传 ZIP。
    let uploads = 0;
    await page.route('**/api/v2/console/theme-installations', async (route) => {
      uploads++;
      await route.fetch();
      await route.abort();
      await page.unroute('**/api/v2/console/theme-installations');
    });
    await install('react', 'React 整包主题', false);
    await visible(page.getByRole('button', { name: '重新读取', exact: true }));
    assert(await page.getByRole('button', { name: '上传并安装', exact: true }).isDisabled());
    await page.getByRole('button', { name: '重新读取', exact: true }).click();
    await visible(row('React 整包主题'));
    assert.equal(uploads, 1);
    await activate('React 整包主题');
    await visible(page.getByRole('heading', { name: 'React 整包主题', exact: true }));
    await visible(page.getByText('主题验收节点', { exact: true }));
    await page.goto(base + '/react/deep/route');
    await visible(page.getByRole('heading', { name: 'React 整包主题', exact: true }));
    await page.getByRole('button', { name: '主题退出', exact: true }).click();
    await page.waitForURL(base + '/signin');
    await visible(page.getByRole('heading', { name: '登录', exact: true }));
    await submitBuiltinLogin();
    await page.waitForURL(base + '/');
    await visible(page.getByRole('heading', { name: 'React 整包主题', exact: true }));
    await page.getByRole('link', { name: '内置外观', exact: true }).click();
    await visible(page.getByRole('button', { name: '切回内置', exact: true }));
    assert.equal(new URL(page.url()).pathname, builtin + 'settings/appearance');
    await install('vue', 'Vue 整包主题');
    const selected = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v2/console/theme-selection' && r.request().method() === 'PUT');
    await page.getByRole('button', { name: '切回内置', exact: true }).click();
    assert.equal((await selected).status(), 200);
    await activate('Vue 整包主题');
    await visible(page.getByRole('heading', { name: 'Vue 整包主题', exact: true }));
    await visible(page.getByText('主题验收节点', { exact: true }));
    await page.goto(base + '/vue/deep/route');
    await visible(page.getByRole('heading', { name: 'Vue 整包主题', exact: true }));
    await page.getByRole('button', { name: '主题退出', exact: true }).click();
    await page.waitForURL(base + '/signin');
    await visible(page.getByLabel('主题账号', { exact: true }));
    await page.getByLabel('主题账号', { exact: true }).fill('admin');
    await page.getByLabel('主题密码', { exact: true }).fill('test-password');
    await page.getByRole('button', { name: '主题登录', exact: true }).click();
    await page.waitForURL(base + '/');
    await visible(page.getByText('主题验收节点', { exact: true }));
    await goAppearance();
    const screenshots = join(output, 'screenshots');
    mkdirSync(screenshots, { recursive: true });
    for (const mode of ['light', 'dark']) {
      await page.getByRole('radio', { name: mode === 'light' ? '浅色' : '深色', exact: true }).click();
      for (const width of [320, 768, 1440]) {
        await page.setViewportSize({ width, height: 1000 });
        await page.screenshot({ path: join(screenshots, `${mode}-${width}.png`), fullPage: true, animations: 'disabled' });
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${mode}-${width} 页面溢出`);
      }
    }
    await install('broken', '错误主题');
    const expectedError = page.waitForEvent('pageerror', { predicate: (error) => error.message.includes('主题验证：预期的运行错误') });
    await activate('错误主题');
    await expectedError;
    const sessionResponse = await context.request.get(base + '/api/v2/auth/sessions/current');
    const session = await sessionResponse.json();
    assert.equal((await context.request.delete(base + '/api/v2/auth/sessions/current', { headers: { Origin: base, 'X-CSRF-Token': session.csrf_token } })).status(), 204);
    await page.goto(base + '/signin?builtin=1');
    await submitBuiltinLogin();
    await page.waitForURL(base + builtin);
    await goAppearance();
    await page.getByRole('button', { name: '切回内置', exact: true }).click();
    await visible(page.getByRole('status').filter({ hasText: '已切换主题' }));
    await row('React 整包主题').getByRole('button', { name: '删除', exact: true }).click();
    await visible(page.getByRole('alertdialog'));
    await page.getByRole('alertdialog').getByRole('button', { name: '删除', exact: true }).click();
    await row('React 整包主题').waitFor({ state: 'detached' });
    // 使用实际内置 gpt2 页面修改安全入口并验证两种登录、退出及失效路径。
    await page.getByRole('button', { name: '刷新界面', exact: true }).click();
    await page.getByRole('heading', { name: '概览', exact: true }).waitFor();
    await page.goto(base + '/settings/account');
    await page.getByLabel('安全入口', { exact: true }).fill('/secure-opsd');
    await page.getByLabel('当前密码', { exact: true }).fill('test-password');
    await page.getByRole('button', { name: '保存并重新登录', exact: true }).click();
    await page.waitForURL(base + '/secure-opsd');
    assert.equal((await context.request.get(base + '/signin')).status(), 404);
    await submitBuiltinLogin(); await page.waitForURL(base + '/');
    await page.getByRole('button', { name: '账号菜单 admin', exact: true }).click();
    await page.getByRole('menuitem', { name: '退出', exact: true }).click();
    await page.waitForURL(base + '/secure-opsd');
    await page.goto(base + '/secure-opsd?builtin=1');
    await submitBuiltinLogin(); await page.waitForURL(base + builtin);
    const current = await (await context.request.get(base + '/api/v2/auth/sessions/current')).json();
    await page.goto(base + builtin + `nodes/${current.local_node_id}?tab=configuration#node-settings`);
    await page.getByRole('heading', { name: '节点设置', exact: true }).waitFor();
    await page.reload(); await page.getByRole('heading', { name: '节点设置', exact: true }).waitFor();
    assert.equal(new URL(page.url()).hash, '#node-settings');
    await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('link', { name: '工具', exact: true }).click();
    assert(new URL(page.url()).pathname.startsWith(builtin + 'tools/'));
    await page.goto(base + builtin + `terminal?node=${current.local_node_id}&keep=1#terminal-end`);
    await page.waitForURL(url => url.pathname === builtin + 'tools/terminal' && url.searchParams.has('session'));
    assert.equal(new URL(page.url()).searchParams.get('keep'), '1');
    assert.equal(new URL(page.url()).hash, '#terminal-end');
    await context.request.delete(base + '/api/v2/auth/sessions/current', { headers: { Origin: base, 'X-CSRF-Token': current.csrf_token } });
    await page.waitForURL(base + '/secure-opsd?builtin=1');
    assert.equal((await context.request.get(base + builtin + 'settings/appearance')).status(), 404);
    await submitBuiltinLogin(); await page.waitForURL(base + builtin);
    await page.goto(base + builtin + 'settings/account');
    await page.getByLabel('安全入口', { exact: true }).fill('/recover-opsd');
    await page.getByLabel('当前密码', { exact: true }).fill('test-password');
    await page.getByRole('button', { name: '保存并重新登录', exact: true }).click();
    await page.waitForURL(base + '/recover-opsd?builtin=1');
    await submitBuiltinLogin(); await page.waitForURL(base + builtin);
    await goAppearance();
    assert.deepEqual(errors.filter((message) => !message.includes('主题验证：预期的运行错误')), []);
    writeFileSync(join(output, 'result.json'), JSON.stringify({ frameworks: ['React', 'Vue'], checks: ['Swagger 会话与 Bearer', 'Swagger 条件写入及原请求查询', 'ZIP 安装', '未知应答后读取', '切换', '双登录模式', '深层路由', '内置恢复', '删除', '自定义入口与账号修改', '恢复页深层刷新和终端参数', '退出与会话失效', '深浅色与三种宽度'], unexpectedErrors: [], expectedRuntimeErrors: errors.length }, null, 2));
    console.log('主题浏览器验证通过');
  } catch (error) {
    await page.screenshot({ path: join(output, 'failure.png'), fullPage: true }).catch(() => {});
    const overflow = await page.evaluate(() => [...document.querySelectorAll('body *')].filter((element) => element.getBoundingClientRect().right > innerWidth + 1).slice(0, 12).map((element) => ({ tag: element.tagName, class: element.className, right: element.getBoundingClientRect().right }))).catch(() => []);
    writeFileSync(join(output, 'failure.json'), JSON.stringify({ url: page.url(), errors, message: error.message, overflow }, null, 2));
    throw error;
  } finally { await browser.close(); }
}

if (process.argv[2] === 'build') await buildThemes();
else if (process.argv[2] === 'check') await checkThemes();
else throw new Error('用法：node scripts/check-themes.mjs build|check');
