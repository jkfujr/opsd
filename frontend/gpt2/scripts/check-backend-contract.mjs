import assert from 'node:assert/strict';

export async function checkBackendContract({ page, go, step, capture, closeDrawers, id, checked, rev, op, actions, counts, fixtures }) {
  const sizes = async name => {
    for (const theme of ['dark', 'light']) for (const width of [320, 768, 1440]) {
      await page.evaluate(theme => { localStorage.setItem('opsd.frontend-gpt2.theme', theme); document.documentElement.classList.toggle('dark', theme === 'dark'); }, theme);
      await page.setViewportSize({ width, height: 950 });
      await capture(`${name}-${theme}-${width}`);
    }
    await page.setViewportSize({ width: 1440, height: 1000 });
  };
  await step('受管代理预览、未知应答恢复、连接预算与窄屏', async () => {
    const previous = fixtures.database;
    const group = { writer: 10, backup_writer: 20, reader: 30, offline: 40 };
    const proxy = structuredClone(previous.proxies[0]);
    proxy.status = { ...proxy.status, proxy_hostgroups: group, servers: [{ hostgroup: 10, host: '100.100.201.1', port: 3306, state: 'SHUNNED', weight: 990, max_connections: 100 }, { hostgroup: 20, host: '100.100.201.1', port: 3306, state: 'ONLINE', weight: 990, max_connections: 100 }], pools: [{ hostgroup: 10, host: '100.100.201.1', port: 3306, used: 1, free: 3, peak_used: 7, errors: 0 }] };
    fixtures.database = { ...previous, members: previous.members.map(member => ({ ...member, status: { ...member.status, max_connections: 100 } })), proxies: [proxy, { ...proxy, instance_id: 'second-proxy', name: '第二代理' }] };
    fixtures.routingUnknown = true;
    fixtures.routingManaged = false;
    try {
      await go('/databases/ui-db'); await page.getByRole('tab', { name: '代理', exact: true }).click();
      await page.getByText('备用写入', { exact: true }).first().waitFor();
      await page.getByRole('button', { name: '配置路由', exact: true }).click();
      const dialog = page.getByRole('dialog');
      await dialog.getByText('移除路由', { exact: true }).first().waitFor();
      await dialog.getByLabel('SQL 主机地址', { exact: true }).fill('100.100.201.1');
      assert(await dialog.getByRole('button', { name: '应用配置', exact: true }).isDisabled());
      await dialog.getByRole('button', { name: '重新预览', exact: true }).click();
      await dialog.getByText('移除路由', { exact: true }).first().waitFor();
      await sizes('contract-proxy-preview');
      const before = actions.filter(action => action.kind === 'proxy-routing').length;
      await dialog.getByRole('button', { name: '应用配置', exact: true }).click();
      await dialog.getByRole('button', { name: '查询原操作', exact: true }).waitFor();
      assert.equal(actions.filter(action => action.kind === 'proxy-routing').length, before + 1);
      await dialog.getByRole('button', { name: '查询原操作', exact: true }).click();
      await page.locator('.operation-detail').waitFor();
      assert.equal(await page.locator('.operation-detail h2').innerText(), '应用代理路由');
      await closeDrawers();
      await page.getByText('配置超额', { exact: true }).waitFor();
      await sizes('contract-proxy-status');
    } finally { fixtures.database = previous; fixtures.routingUnknown = false; fixtures.routingManaged = false; }
  });
  await step('防火墙接纳当前配置及原请求查询', async () => {
    const previous = fixtures.firewall;
    fixtures.firewall = { ...previous, backend: 'iptables', state: 'failed', error: { code: 'conflict', message: '防火墙后端已经改变' }, rules: [], default_policies: [] };
    fixtures.adoptionUnknown = true;
    try {
      await go('/firewall'); await page.getByRole('button', { name: '预览当前配置', exact: true }).click();
      const dialog = page.getByRole('dialog');
      await dialog.getByRole('heading', { name: '接纳当前防火墙配置', exact: true }).waitFor();
      assert.equal(await dialog.getByText('iptables', { exact: true }).count(), 1);
      await sizes('contract-firewall-adoption');
      const before = actions.filter(action => action.kind === 'firewall-adoption').length;
      await dialog.getByRole('button', { name: '接纳当前配置', exact: true }).click();
      await dialog.getByRole('button', { name: '查询原操作', exact: true }).click();
      await page.locator('.operation-detail').waitFor();
      assert.equal(await page.locator('.operation-detail h2').innerText(), '接纳当前防火墙配置');
      assert.equal(actions.filter(action => action.kind === 'firewall-adoption').length, before + 1);
      await closeDrawers();
    } finally { fixtures.firewall = previous; fixtures.adoptionUnknown = false; }
  });
  await step('操作历史保存原名称、动作和完整目标', async () => {
    const record = op('container');
    Object.assign(record, { action: 'stop', target_name: '原容器名称', resource: { kind: 'container', node_id: id, engine_id: '原 Engine', id: 'c'.repeat(64) }, supports_logs: false });
    await go('/containers'); await page.getByRole('button', { name: /^操作记录/ }).click();
    const back = page.getByRole('button', { name: '返回操作列表', exact: true }); if (await back.isVisible()) await back.click();
    await page.locator(`[data-operation-id="${record.id}"]`).click();
    await page.getByRole('heading', { name: '停止容器', exact: true }).waitFor();
    const detail = page.locator('.operation-detail');
    assert.equal(await detail.getByText('原容器名称', { exact: true }).count(), 1);
    assert.equal(await detail.getByRole('button', { name: '查看执行日志', exact: true }).count(), 0);
    await detail.locator('summary').click(); await detail.getByText('原 Engine', { exact: true }).waitFor();
    await capture('contract-operation-description'); await closeDrawers();
    const path = `GET /operations/${record.id}/logs`, before = counts[path] ?? 0;
    await go(`/tools?view=logs&operation=${record.id}`); await page.getByRole('heading', { name: '此操作没有执行日志', exact: true }).waitFor();
    assert.equal(counts[path] ?? 0, before);
  });
}
