import assert from 'node:assert/strict';

export async function checkLayout(env) {
  const { page, go, step, capture, wait, metrics, fixtures, op } = env;
  const near = (actual, expected) => assert(Math.abs(actual - expected) < 1, `${actual} != ${expected}`);
  const original = structuredClone(fixtures.database);
  const member = original.members[0];
  const at = '2026-11-12T00:00:00Z', later = '2026-11-12T00:00:20Z';
  function database(state, data = true) {
    const value = structuredClone(original);
    value.checked_at = '2026-11-12T23:59:00Z';
    for (const item of [...value.members, ...value.proxies]) {
      item.status = data ? { ...item.status, state, read: { state: ['ready', 'unavailable', 'mismatch'].includes(state) ? 'ready' : state, data_at: at, attempted_at: at } } : { state, checked_at: at, read: { state } };
      if (['offline', 'error', 'timeout'].includes(state)) item.status.read.error = { code: 'dependency_unavailable', message: `${item.name} 读取故障` };
      if (state === 'unavailable') item.status.ready = false;
      if (state === 'mismatch') item.status.error = { code: 'conflict', message: '配置关联与实际 Galera 集群身份不同' };
    }
    return value;
  }

  await step('数据库首次读取、旧数据、部分待确认和真实故障展示', async () => {
    try {
      for (const [state, label, count, data] of [['queued', '—', '— / 1', false], ['reading', '—', '— / 1', false], ['stale', '可用', '1 / 1', true], ['offline', '节点离线', '1 / 1', true], ['timeout', '读取超时', '1 / 1', true], ['error', '读取失败', '1 / 1', true], ['unavailable', '不可用', '0 / 1', true], ['mismatch', '关联不一致', '0 / 1', true], ['ready', '可用', '1 / 1', true]]) {
        fixtures.database = database(state, data);
        await go('/databases');
        const row = page.getByRole('row').filter({ has: page.getByRole('link', { name: original.cluster.name, exact: true }) });
        await row.waitFor(); assert((await row.locator('td').nth(1).innerText()).includes(label));
        assert((await row.locator('td').nth(2).innerText()).includes(count));
        assert(!/等待读取|读取中|待更新|上次结果|待确认/.test(await row.innerText()));
        await row.getByRole('link', { name: original.cluster.name, exact: true }).click();
        if (label === '—') assert.equal(await page.locator('.page-meta .status').count(), 0); else await wait(async () => (await page.locator('.page-meta').innerText()).includes(label), `${state} 列表与详情不一致`);
        if (state === 'stale') { assert((await page.locator('.page-meta').innerText()).includes('可用')); await capture('layout-database-stale'); }
        if (state === 'queued') await capture('layout-database-first');
        if (data) {
          const expected = await page.evaluate(at => new Date(at).toLocaleString('zh-CN', { hour12: false }), at);
          const collected = page.locator('dl > div').filter({ has: page.locator('dt', { hasText: '数据采集时间' }) });
          assert.equal(await collected.locator('dd').innerText(), expected);
        }
      }
      const partial = database('ready');
      partial.members.push({ ...member, node_id: 'waiting-member', instance_id: 'waiting', status: { state: 'queued', checked_at: at, read: { state: 'queued' } } });
      fixtures.database = partial;
      await go('/databases'); await page.getByRole('link', { name: original.cluster.name, exact: true }).waitFor();
      assert((await page.locator('tbody').innerText()).includes('— / 2')); assert(!(await page.locator('tbody').innerText()).includes('待确认'));
      await page.getByRole('link', { name: original.cluster.name, exact: true }).click(); await page.getByRole('tab', { name: '概览', exact: true }).waitFor(); assert.equal(await page.getByText('1 个待确认', { exact: true }).count(), 0); await capture('layout-database-partial');
    } finally { fixtures.database = original; }
  });

  await step('数据库分类错误独立、列表详情缓存更新和旧响应后到', async () => {
    const path = url => url.pathname === '/api/v2/database-clusters/ui-db/status';
    let failCategory = false, failHealth = false, detailValue;
    await page.route(path, route => {
      if (failHealth && new URL(route.request().url()).searchParams.get('view') === 'health') return route.fulfill({ status: 503, json: { detail: '健康读取失败', code: 'dependency_unavailable' } });
      if (failCategory && new URL(route.request().url()).searchParams.get('view') !== 'health') return route.fulfill({ status: 503, json: { detail: '库分类读取失败', code: 'dependency_unavailable' } });
      return detailValue ? route.fulfill({ json: detailValue }) : route.fallback();
    });
    try {
      fixtures.database = database('ready');
      await go('/databases'); await page.getByRole('link', { name: original.cluster.name, exact: true }).waitFor();
      detailValue = database('ready'); detailValue.members[0].status.connections = 29; detailValue.members[0].status.read.data_at = later; detailValue.members[0].status.read.attempted_at = later;
      await page.getByRole('link', { name: original.cluster.name, exact: true }).click();
      await wait(async () => (await page.locator('.tab-content').innerText()).includes('29'), '较新详情未显示');
      await page.getByRole('link', { name: '返回数据库', exact: true }).click();
      assert((await page.locator('tbody').innerText()).includes('29'), '详情未更新已有列表缓存');
      await page.getByRole('button', { name: '刷新', exact: true }).click();
      assert((await page.locator('tbody').innerText()).includes('29'), '后到的旧列表覆盖了新详情');
      await page.getByRole('link', { name: original.cluster.name, exact: true }).click();
      failCategory = true;
      await page.getByRole('tab', { name: '库', exact: true }).click(); await page.getByRole('alert').filter({ hasText: '库分类读取失败' }).waitFor();
      assert((await page.locator('.page-meta').innerText()).includes('可用'));
      assert(await page.getByRole('button', { name: '创建库', exact: true }).isDisabled());
      await capture('layout-database-category-error');
      failCategory = false; failHealth = true;
      await page.getByRole('button', { name: '刷新', exact: true }).click(); await page.getByRole('alert').filter({ hasText: '健康读取失败' }).waitFor();
      await page.getByRole('tab', { name: '概览', exact: true }).click(); assert((await page.locator('.tab-content').innerText()).includes('29'), '健康 HTTP 失败丢失已保留数值');
      await capture('layout-database-health-error');
      const listPath = url => url.pathname === '/api/v2/database-clusters';
      await page.route(listPath, route => route.fulfill({ status: 503, json: { detail: '集群列表读取失败', code: 'dependency_unavailable' } }));
      try {
        await page.getByRole('link', { name: '返回数据库', exact: true }).click(); await page.getByRole('button', { name: '刷新', exact: true }).click();
        await page.getByRole('alert').filter({ hasText: '集群列表读取失败' }).waitFor();
        assert((await page.locator('tbody').innerText()).includes('29'), '列表 HTTP 失败丢失已保留数值');
      } finally { await page.unroute(listPath); }
      metrics.push({ check: 'database-cache', detailToList: true, oldListRejected: true, categoryIndependent: true });
    } finally { fixtures.database = original; await page.unroute(path); }
  });

  await step('数据库快速重查限制、分类健康查询和隐藏页面停止刷新', async () => {
    const path = url => url.pathname === '/api/v2/database-clusters/ui-db/status';
    const reads = [];
    await page.route(path, route => { reads.push({ at: Date.now(), view: new URL(route.request().url()).searchParams.get('view') }); return route.fulfill({ json: fixtures.database }); });
    try {
      fixtures.database = database('queued', false); await go('/databases/ui-db'); await wait(() => reads.length > 0, '健康读取未开始'); assert.equal(await page.getByText('等待读取', { exact: true }).count(), 0);
      await page.waitForTimeout(5800); const bounded = reads.length; assert(bounded >= 3 && bounded <= 12, `快速读取次数 ${bounded}`);
      await page.waitForTimeout(1100); assert.equal(reads.length, bounded, '超过五秒仍快速读取');
      await page.getByRole('button', { name: '刷新', exact: true }).click(); await page.waitForTimeout(650); assert(reads.length > bounded + 1, '手动刷新未重新开始快速读取');
      await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); document.dispatchEvent(new Event('visibilitychange')); });
      await page.waitForTimeout(600); const hidden = reads.length; await page.waitForTimeout(1100); assert.equal(reads.length, hidden, '隐藏后仍周期刷新');
      await page.evaluate(() => { delete document.visibilityState; document.dispatchEvent(new Event('visibilitychange')); });
      fixtures.database = database('ready');
      await go('/databases/ui-db'); await page.getByRole('tab', { name: '库', exact: true }).click();
      const start = reads.length; await page.waitForTimeout(10300);
      const health = reads.slice(start).filter(item => item.view === 'health'); assert(health.length >= 1 && health.length <= 2, `分类页健康查询 ${health.length}`);
      assert(reads.every(item => ['health', 'schemas'].includes(item.view)), '出现逐成员附加查询');
      metrics.push({ check: 'database-polling', boundedFastReads: bounded, hiddenStopped: true, categoryHealthReads: health.length });
    } finally { fixtures.database = original; await page.unroute(path); }
  });

  await step('共享间距、页签高度、筛选换行和节点字段布局', async () => {
    for (const width of [320, 639, 640, 768, 900, 901, 1279, 1280, 1440]) {
      await page.setViewportSize({ width, height: 1000 });
      for (const path of [`/nodes/${env.id}`, '/containers', `/firewall?node=${env.id}`, '/tools/tasks', '/databases/ui-db']) {
        await go(path); await page.evaluate(() => document.fonts.ready);
        const values = await page.evaluate(() => {
          const root = document.querySelector('.page-content > .page-stack'), header = root.querySelector('.page-heading'), next = header.nextElementSibling;
          const tabs = [...root.querySelectorAll('[role="tab"]')].filter(el => el.getBoundingClientRect().height > 0);
          const list = root.querySelector('.list-filters');
          return { spacing: next.getBoundingClientRect().top - header.getBoundingClientRect().bottom, heights: tabs.map(el => el.getBoundingClientRect().height), inner: tabs.map(el => getComputedStyle(el.querySelector('[data-slot="tabs-trigger-inner"]')).paddingBlock), columns: list ? getComputedStyle(list).gridTemplateColumns.split(' ').length : null, tables: [...root.querySelectorAll('.panel [data-slot="table"]')].map(el => ({ border: getComputedStyle(el).borderWidth, padding: getComputedStyle(el).padding })), scroll: document.documentElement.scrollWidth, width: innerWidth };
        });
        near(values.spacing, width < 640 ? 20 : 24); values.heights.forEach(height => near(height, 48)); assert(values.inner.every(value => value === '0px'));
        assert(values.scroll <= width + 1, `${path} ${width}px 横向溢出`);
        assert(values.tables.every(value => value.border === '0px' && value.padding === '0px'), '表格面板重复添加外层边界或内距');
        if (path === '/containers') assert.equal(values.columns, width < 640 ? 1 : width < 1280 ? 2 : 3);
        metrics.push({ check: 'spacing', path, width, ...values });
        if (width === 320 && path === '/databases/ui-db') {
          await page.getByRole('tab', { name: '概览', exact: true }).focus(); await page.keyboard.press('End');
          await wait(() => page.getByRole('tab', { name: '备份', exact: true }).evaluate(el => el === document.activeElement), '方向键未移动页签焦点');
          const focused = await page.getByRole('tab', { name: '备份', exact: true }).evaluate(el => { const tab = el.getBoundingClientRect(), list = el.closest('[role="tablist"]').getBoundingClientRect(); return tab.left >= list.left - 1 && tab.right <= list.right + 1; });
          assert(focused, '窄屏当前页签不在可见区域');
        }
      }
      await go(`/nodes/${env.id}`);
      const fields = await page.locator('.panel-titled').first().evaluate(panel => {
        const heading = panel.querySelector('.panel-heading'), body = panel.querySelector('.panel-body'), dt = body.querySelector('dt'), dd = body.querySelector('dd');
        return { headingGap: body.getBoundingClientRect().top - heading.getBoundingClientRect().bottom, valueGap: dd.getBoundingClientRect().top - dt.getBoundingClientRect().bottom, padding: getComputedStyle(body.querySelector('.detail-grid')).padding, columns: getComputedStyle(document.querySelector('.node-metrics')).gridTemplateColumns.split(' ').length };
      });
      near(fields.headingGap, 16); near(fields.valueGap, 8); assert.equal(fields.padding, '0px'); if (width < 640) assert.equal(fields.columns, 1);
    }
    await page.setViewportSize({ width: 1440, height: 1000 });
  });

  await step('长会话、二级导航、侧栏切换和资源菜单键盘操作', async () => {
    const originalNodes = fixtures.nodes;
    const longName = 'C001_' + '很长的服务器资源名称'.repeat(10);
    fixtures.nodes = { ...originalNodes, items: [{ ...originalNodes.items[0], name: longName, address: 'https://[2001:db8:1234:5678:9012:3456:7890:abcd]:8444', public_key: 'a'.repeat(256) }, ...Array.from({ length: 40 }, (_, i) => ({ ...originalNodes.items[0], id: `long-${i}`, name: `C${String(i + 2).padStart(3, '0')}_` + '资源名称'.repeat(12) }))] };
    try {
      await go(`/tools/terminal?node=${env.id}`); await page.locator('.terminal-surface[data-ready]').waitFor(); await page.getByText('已连接', { exact: true }).waitFor();
      const tickets = env.tickets();
      const measure = async selector => page.locator(selector).first().evaluate(row => { const box = row.getBoundingClientRect(), close = row.querySelector('button').getBoundingClientRect(), link = row.querySelector('a').getBoundingClientRect(); return { width: box.width, close: close.width, right: close.right - box.right, linkRight: link.right - close.left, scroll: row.scrollWidth, full: row.querySelector('a').title }; });
      const row = await measure('.sidebar .sidebar-session'); near(row.close, 44); assert(row.right <= 1 && row.linkRight <= 1 && row.scroll <= row.width + 1); assert(row.full.includes(longName));
      const colors = await page.evaluate(() => ({ parent: getComputedStyle(document.querySelector('.sidebar .category-active > a')).backgroundColor, child: getComputedStyle(document.querySelector('.sidebar .subnav [aria-current="page"]')).backgroundColor, session: getComputedStyle(document.querySelector('.sidebar-session.selected')).backgroundColor }));
      assert.equal(colors.parent, 'rgba(0, 0, 0, 0)'); assert.notEqual(colors.child, colors.session);
      await page.getByRole('button', { name: '收起侧栏', exact: true }).click(); assert(await page.locator('.sidebar .subnav').isHidden()); await page.getByRole('button', { name: '展开侧栏', exact: true }).click();
      await page.setViewportSize({ width: 900, height: 950 }); await page.getByRole('button', { name: '打开导航', exact: true }).click();
      const mobile = await measure('.mobile-navigation .sidebar-session'); near(mobile.close, 44); assert(mobile.right <= 1 && mobile.scroll <= mobile.width + 1);
      await page.keyboard.press('Escape'); await wait(() => page.getByRole('button', { name: '打开导航', exact: true }).evaluate(el => el === document.activeElement), '抽屉未恢复入口焦点');
      await page.setViewportSize({ width: 901, height: 950 }); await page.setViewportSize({ width: 320, height: 900 }); await page.setViewportSize({ width: 1440, height: 1000 });
      assert.equal(env.tickets(), tickets, '响应式切换重建了终端');
      await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('link', { name: '容器', exact: true }).click();
      await page.getByLabel('容器节点', { exact: true }).click(); await page.getByRole('listbox').waitFor();
      await page.locator('.resource-select').evaluate(async el => { await Promise.all(el.getAnimations().map(animation => animation.finished.catch(() => {}))); });
      const menu = await page.locator('.resource-select').evaluate(el => { const box = el.getBoundingClientRect(); return { width: box.width, left: box.left, right: box.right, top: box.top, bottom: box.bottom, viewport: { width: innerWidth, height: innerHeight }, arrows: [...el.querySelectorAll('[data-slot^="select-scroll-"]')].map(el => el.dataset.slot), optionWhiteSpace: getComputedStyle(el.querySelector('[role="option"]')).whiteSpace }; });
      assert(menu.width >= 351); assert(menu.left >= 0 && menu.right <= 1440 && menu.top >= 0 && menu.bottom <= 1000); assert.equal(menu.optionWhiteSpace, 'normal');
      const rows = await page.locator('.resource-select [role="option"]').evaluateAll(items => items.map(el => { const box = el.getBoundingClientRect(); return { top: box.top, bottom: box.bottom }; }));
      assert(rows.every((row, index) => index === 0 || row.top >= rows[index - 1].bottom), '资源菜单选项文字重叠');
      await capture('layout-resource-menu'); await page.keyboard.type('C040'); await page.keyboard.press('Enter');
      await wait(async () => (await page.getByLabel('容器节点', { exact: true }).innerText()).includes('C040'), '资源菜单类型搜索失效');
      await wait(() => page.getByLabel('容器节点', { exact: true }).evaluate(el => el === document.activeElement), '资源菜单未恢复入口焦点');
      for (const width of [320, 768]) {
        await page.setViewportSize({ width, height: 850 }); await page.getByLabel('容器节点', { exact: true }).click();
        await page.locator('.resource-select').evaluate(async el => { await Promise.all(el.getAnimations().map(animation => animation.finished.catch(() => {}))); });
        const bounds = await page.locator('.resource-select').boundingBox(); assert(bounds.x >= 0 && bounds.x + bounds.width <= width + 1 && bounds.y + bounds.height <= 850);
        await capture(`layout-resource-menu-${width}`); await page.keyboard.press('Escape'); await page.getByRole('listbox').waitFor({ state: 'hidden' });
      }
      await page.setViewportSize({ width: 1440, height: 1000 }); await page.getByLabel('容器状态', { exact: true }).click();
      const status = await page.locator('.ui-select-popup[data-open]').boundingBox(); assert(status.width < 352, '短选项菜单未保持紧凑'); await page.keyboard.press('Escape');
      for (const width of [320, 768, 1440]) {
        await page.setViewportSize({ width, height: 1000 }); await go(`/nodes/${env.id}`);
        const overflow = await page.locator('.page-content').evaluate(el => ({ document: document.documentElement.scrollWidth > innerWidth + 1, fields: [...el.querySelectorAll('.detail-grid dd')].some(el => el.scrollWidth > el.clientWidth + 1) }));
        assert(!overflow.document && !overflow.fields, '长节点名称、地址或公钥溢出'); await capture(`layout-long-node-${width}`);
      }
      metrics.push({ check: 'navigation', row, mobile, colors, menu, terminalPreserved: true });
    } finally { fixtures.nodes = originalNodes; }
  });

  await step('节点危险操作分区、任务表单间距和长内容仅正文滚动', async () => {
    const node = [];
    for (const theme of ['dark', 'light']) {
      await page.evaluate(theme => localStorage.setItem('opsd.frontend-gpt2.theme', theme), theme);
      for (const width of [320, 768, 1440]) {
        await page.setViewportSize({ width, height: 950 }); await go('/nodes');
        await page.getByRole('button', { name: '编辑 本地验收节点', exact: true }).click(); await page.getByRole('button', { name: '保存', exact: true }).waitFor();
        const layout = await page.locator('.node-editor').evaluate(el => {
          const footer = el.querySelector('.modal-footer'), revoke = footer.querySelector('.node-revoke'), save = footer.querySelector('[type="submit"]');
          return { separate: !revoke.closest('form'), type: revoke.type, saveForm: save.form.id, footerWidth: footer.clientWidth, scrollWidth: footer.scrollWidth, background: getComputedStyle(revoke).backgroundColor, buttons: [...footer.querySelectorAll('button')].map(button => { const box = button.getBoundingClientRect(); return { text: button.textContent, left: box.left, top: box.top, right: box.right, height: box.height }; }) };
        });
        assert(layout.separate); assert.equal(layout.type, 'button'); assert.equal(layout.saveForm, 'rename-node'); assert.equal(layout.background, 'rgba(0, 0, 0, 0)'); assert(layout.scrollWidth <= layout.footerWidth);
        for (const button of layout.buttons) { near(button.top, layout.buttons[0].top); assert(button.height >= 44); assert(button.left >= 0 && button.right <= width); }
        assert(layout.buttons[0].right < layout.buttons[1].left && layout.buttons[1].right < layout.buttons[2].left);
        node.push({ theme, width, ...layout }); await capture(`layout-node-editor-${theme}-${width}`);
        if (theme === 'dark' && width === 320) {
          await page.getByLabel('名称', { exact: true }).fill('尚未保存的节点名称');
          await page.getByRole('button', { name: '撤销授权', exact: true }).click(); await page.getByRole('alertdialog').waitFor(); await page.getByRole('alertdialog').getByRole('button', { name: '取消', exact: true }).click();
          assert.equal(await page.getByLabel('名称', { exact: true }).inputValue(), '尚未保存的节点名称');
        }
        await page.getByRole('button', { name: '关闭弹窗', exact: true }).click();
      }
    }
    await go('/tools/tasks'); await page.getByRole('button', { name: '编辑 每日检查', exact: true }).click(); await page.getByLabel('脚本', { exact: true }).waitFor();
    const gap = await page.locator('#task-editor fieldset').evaluate(el => getComputedStyle(el).rowGap); assert.equal(gap, '20px');
    await page.setViewportSize({ width: 320, height: 450 });
    const scroll = await page.locator('.ui-modal').evaluate(el => { const body = el.querySelector('[data-slot="dialog-body"]'), footer = el.querySelector('[data-slot="dialog-footer"]'); return { outer: getComputedStyle(el).overflowY, body: getComputedStyle(body).overflowY, bodyHeight: body.clientHeight, bodyScroll: body.scrollHeight, footerBottom: footer.getBoundingClientRect().bottom, height: innerHeight }; });
    assert.equal(scroll.outer, 'hidden'); assert.equal(scroll.body, 'auto'); assert(scroll.bodyScroll > scroll.bodyHeight); assert(scroll.footerBottom <= scroll.height);
    await capture('layout-task-editor-short'); await page.getByRole('button', { name: '关闭弹窗', exact: true }).click();
    metrics.push({ check: 'editors', node, gap, scroll }); await page.setViewportSize({ width: 1440, height: 1000 });
    await go('/settings/account'); assert.equal(await page.locator('form fieldset').evaluate(el => getComputedStyle(el).rowGap), '20px');
    await go('/databases/ui-db'); await page.getByRole('tab', { name: '账号', exact: true }).click(); await page.getByRole('button', { name: '创建账号', exact: true }).click();
    assert.equal(await page.locator('#database-change fieldset').evaluate(el => getComputedStyle(el).rowGap), '20px');
    await capture('layout-database-editor'); await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click();
  });

  await step('操作业务摘要、完整技术详情、现有资源名称和记录分页', async () => {
    const titles = { task_run: '执行任务', task_save: '保存任务', name: '修改节点名称', config: '修改节点配置', account: '修改节点账号', firewall: '应用防火墙变更', firewall_config: '更新防火墙配置', container: '容器操作', component: '组件操作', compose: 'Compose 项目操作', compose_register: '登记 Compose 项目', database: '数据库变更', database_step: '数据库执行步骤' };
    const items = Object.keys(titles).map(subject => { const item = op(subject); item.idempotency_key = 'request-' + 'a'.repeat(100); if (subject === 'task_run') item.resource_id = 'daily'; else if (subject === 'task_save') item.resource_id = 'recording-move'; else if (subject === 'container') item.resource_id = env.containerID; else if (subject.startsWith('compose')) item.resource_id = 'web'; else if (subject.startsWith('database')) item.resource_id = 'ui-db'; return item; });
    await go('/tools/tasks');
    await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('link', { name: '容器', exact: true }).click(); await page.getByRole('button', { name: '重启', exact: true }).waitFor();
    await page.getByRole('tab', { name: 'Compose 项目', exact: true }).click(); await page.getByRole('button', { name: '查看 web 配置', exact: true }).waitFor();
    await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('link', { name: '数据库', exact: true }).click(); await page.getByRole('link', { name: original.cluster.name, exact: true }).waitFor();
    const path = url => url.pathname === '/api/v2/operations';
    await page.route(path, route => { const url = new URL(route.request().url()); return url.searchParams.has('pending') ? route.fallback() : route.fulfill({ json: url.searchParams.has('cursor') ? { items: [items[0]] } : { items, next_cursor: 'older-page' } }); });
    try {
      await page.getByRole('button', { name: /^操作记录/ }).click();
      for (const item of items) { const row = page.locator(`.operation-row[data-operation-id="${item.id}"]`); await row.waitFor(); assert((await row.innerText()).includes(titles[item.subject])); assert(!(await row.innerText()).includes(item.id)); }
      const task = page.locator(`.operation-row[data-operation-id="${items[0].id}"]`); assert((await task.innerText()).includes('每日检查'));
      assert((await page.locator(`.operation-row[data-operation-id="${items[1].id}"]`).innerText()).includes('recording-move'));
      assert((await page.locator(`.operation-row[data-operation-id="${items.find(item => item.subject === 'container').id}"]`).innerText()).includes('web'));
      await task.click(); await page.locator('.operation-detail').waitFor(); assert.equal(await page.locator('.operation-detail h2').innerText(), '执行任务');
      assert(await page.locator('.operation-technical').evaluate(el => !el.open)); await page.locator('.operation-technical summary').click();
      assert.notEqual(await page.locator('.operation-technical summary').evaluate(el => getComputedStyle(el).listStyleType), 'none');
      assert((await page.locator('.operation-technical').innerText()).includes(items[0].idempotency_key)); assert((await page.locator('.operation-technical').innerText()).includes(items[0].id));
      await capture('layout-operation-detail'); await page.getByRole('button', { name: '返回操作列表', exact: true }).click();
      await page.getByRole('button', { name: '更早记录', exact: true }).click(); await wait(async () => await page.locator('.operation-row').count() === 1, '操作记录未分页');
      await page.getByRole('button', { name: '返回最新', exact: true }).click(); await wait(async () => await page.locator('.operation-row').count() === items.length, '操作记录未回到最新');
      await page.getByRole('button', { name: '关闭侧栏', exact: true }).click(); metrics.push({ check: 'operation-summary', types: items.length, pagination: true, technicalDetails: true });
    } finally { await page.unroute(path); }
  });
}
