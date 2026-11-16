import assert from 'node:assert/strict';

export async function checkMonitoring({ page, go, step, capture, closeDrawers, id, checked, op }) {
  await step('S3 凭据、监控配置、秒级缩放、缺口与故障状态', async () => {
    const prefix = `opsd/metrics/${'c'.repeat(32)}/`;
    let config = { revision: 1, enabled: false, node_id: '', storage_target_id: '', prefix, directory: '/var/lib/opsd-monitoring', disk_budget_bytes: 80 * 1073741824, local_days: 30, archive_days: 365, state: 'disabled' };
    let target, failure = false;
    const queries = [], saves = [];
    const handler = async route => {
      const request = route.request(), url = new URL(request.url()), path = url.pathname.slice('/api/v2'.length), method = request.method();
      const json = (body, headers = {}, status = 200) => route.fulfill({ status, json: body, headers });
      if (path === '/storage/targets') return json({ items: target ? [target] : [] });
      if (/^\/storage\/targets\/[^/]+$/.test(path)) {
        if (method === 'GET') return json(target, { ETag: `"storage:${target.revision}"` });
        const body = request.postDataJSON();
        assert(request.headers()['idempotency-key']);
        assert(request.headers()['if-none-match'] === '*' || request.headers()['if-match']);
        saves.push({ path, body });
        const { credentials, ...safe } = body;
        target = { ...safe, id: path.split('/').at(-1), revision: (target?.revision ?? 0) + 1, credentials_available: true, credential_ref: 'private-reference' };
        return json(target);
      }
      if (/^\/storage\/targets\/[^/]+\/check$/.test(path)) {
        assert.equal(request.postDataJSON().prefix, prefix);
        assert(request.headers()['idempotency-key'] && request.headers()['if-match']);
        target.check = { prefix, target_revision: target.revision, state: 'ready', checked_at: checked, prefix_restricted: true, lifecycle_checked: true };
        return json(target.check);
      }
      if (path === '/monitoring/config') {
        if (method === 'GET') return json(config, { ETag: `"monitoring:${config.revision}"` });
        const body = request.postDataJSON();
        assert(request.headers()['idempotency-key'] && request.headers()['if-match']);
        assert.equal(body.node_id, id); assert.equal(body.storage_target_id, target.id);
        assert.equal(body.local_days, 30); assert.equal(body.archive_days, 365);
        const operation = op('monitoring_config');
        operation.action = 'configure_monitoring'; operation.action_title = '配置集中监控';
        config = { ...body, revision: config.revision + 1, state: 'ready', operation_id: operation.id };
        return json(operation, {}, 202);
      }
      if (path === '/monitoring/series') {
        queries.push(Object.fromEntries(url.searchParams));
        if (failure) return json({ code: 'dependency_unavailable', detail: 'S3 历史读取失败', outcome: 'not_accepted' }, {}, 503);
        const from = new Date(url.searchParams.get('from')), to = new Date(url.searchParams.get('to')), seconds = Number(url.searchParams.get('step_seconds') || 2162);
        return json({ metric: url.searchParams.get('metric'), from: from.toISOString(), to: to.toISOString(), step_seconds: seconds, source_resolution_seconds: seconds === 1 ? 0 : 300, items: [{ node_id: id, points: Array.from({ length: 5 }, (_, i) => ({ at: new Date(from.getTime() + i * seconds * 1000).toISOString(), value: i === 2 ? null : i * 12 })) }], missing: [{ node_id: id, from: from.getTime() + seconds * 2000, to: from.getTime() + seconds * 3000, reason: 'no_data' }], warnings: [], partial: false });
      }
      if (path === '/monitoring/status') return json({ items: [{ node_id: id, config: { ...config, enabled: true, node_id: id }, buffer: { batches: 5, bytes: 23040, oldest: Date.now() - 5000, latest: Date.now(), gaps: [{ node_id: id, from: Date.now() - 3600000, to: Date.now() - 3500000, reason: 'buffer_expired' }] }, last_sample: checked, last_upload: checked, last_receive: checked, paused: true, streams: 100, deployment: { state: 'starting', components: ['receive', 'query', 'store', 'compact'].map(name => ({ name, state: name === 'compact' ? 'exited' : 'running', ready: name !== 'compact' })), disk_used_bytes: 75 * 1073741824, disk_available_bytes: 5 * 1073741824, archive_pending_blocks: 4, checked_at: checked } }], unavailable: [] });
      return route.fallback();
    };
    await page.route('**/api/v2/**', handler);
    try {
      await go('/storage'); await page.getByRole('button', { name: '新增 S3', exact: true }).click();
      let dialog = page.getByRole('dialog');
      await dialog.getByLabel('名称', { exact: true }).fill('跨地域监控归档');
      await dialog.getByLabel('所属节点', { exact: true }).click(); await page.getByRole('option').first().click();
      await dialog.getByLabel('Endpoint', { exact: true }).fill('https://s3.example.test');
      await dialog.getByLabel('Bucket', { exact: true }).fill('shared-files');
      await dialog.getByLabel('Region', { exact: true }).fill('ap-northeast-1');
      for (let i = 0; i < 3; i++) {
        await dialog.getByLabel('Access Key', { exact: true }).nth(i).fill('role-' + i);
        await dialog.getByLabel('Secret Key', { exact: true }).nth(i).fill('secret-' + i);
      }
      for (const width of [320, 768, 1440]) { await page.setViewportSize({ width, height: 950 }); await capture(`monitoring-s3-editor-${width}`); }
      await dialog.getByRole('button', { name: '保存', exact: true }).click();
      await page.getByText('跨地域监控归档', { exact: true }).waitFor();
      assert.equal(saves.length, 1); assert.equal(saves[0].body.credentials.store.secret_key, 'secret-1');
      const stores = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }));
      assert(!stores.includes('secret-1'));
      await page.getByRole('button', { name: '连接检查', exact: true }).click();
      await page.getByRole('button', { name: '检查连接', exact: true }).click();
      await page.getByRole('dialog').getByText('连接正常', { exact: true }).waitFor();
      await page.getByRole('dialog').getByText(prefix, { exact: true }).waitFor();
      assert.equal(await page.getByRole('dialog').getByText('检查时间', { exact: true }).count(), 1);
      await page.getByRole('button', { name: '关闭', exact: true }).click();
      await page.getByRole('button', { name: '编辑 跨地域监控归档', exact: true }).click();
      dialog = page.getByRole('dialog'); await dialog.getByLabel('名称', { exact: true }).waitFor();
      assert.equal(await dialog.getByLabel('Secret Key', { exact: true }).count(), 0);
      await dialog.getByRole('button', { name: '取消', exact: true }).click();
      const previousCheck = target.check;
      for (const [state, label, tone] of [['checking', '检查中', 'warning'], ['failed', '检查失败', 'error']]) {
        target.check = { ...previousCheck, state };
        await go('/storage');
        const row = page.getByRole('row').filter({ hasText: target.name }); await row.getByText(label, { exact: true }).waitFor();
        assert(await row.locator('.status').evaluate((el, tone) => el.classList.contains('status-' + tone), tone));
        assert((await row.innerText()).includes(prefix));
      }
      target.check = previousCheck; target.revision++;
      await go('/storage'); await page.getByRole('row').filter({ hasText: target.name }).getByText('待检查', { exact: true }).waitFor(); target.revision--;
      await go('/monitoring'); await page.getByRole('button', { name: '集中监控配置', exact: true }).click();
      dialog = page.getByRole('dialog'); await dialog.getByRole('switch').click();
      await dialog.getByLabel('监控服务宿主', { exact: true }).click(); await page.getByRole('option').first().click();
      await dialog.getByLabel('S3 存储目标', { exact: true }).click(); await page.getByRole('option', { name: '跨地域监控归档', exact: true }).click();
      await capture('monitoring-config');
      await dialog.getByRole('button', { name: '保存并应用', exact: true }).click();
      await page.locator('.operation-detail').waitFor(); await closeDrawers();
      await page.getByRole('heading', { name: '数据缺口', exact: true }).waitFor();
      const chart = page.locator('.chart-canvas svg.recharts-surface'); await chart.waitFor();
      const zoom = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/monitoring/series') && new URL(response.url()).searchParams.get('step_seconds') === '1');
      await chart.scrollIntoViewIfNeeded(); const box = await chart.boundingBox(); await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2); await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2); await zoom;
      await page.waitForFunction(() => document.querySelector('input[type="number"]')?.value === '1');
      assert(queries.some(query => query.step_seconds === '1'));
      const choose = async (label, value) => { await page.getByLabel(label, { exact: true }).click(); await page.getByRole('option', { name: value, exact: true }).click(); };
      await choose('指标', '容器运行状态'); await choose('汇总', '平均');
      await page.getByRole('combobox', { name: '节点', exact: true }).click(); await page.getByRole('option', { name: '本地验收节点', exact: true }).click();
      await page.getByLabel('资源 ID（可选）', { exact: true }).fill('restored-resource');
      const submitted = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/monitoring/series') && new URL(response.url()).searchParams.get('resource_id') === 'restored-resource');
      await page.getByRole('button', { name: '查询', exact: true }).click(); await submitted;
      const savedQuery = new URL(page.url()).search;
      assert.equal(new URL(page.url()).searchParams.get('node'), id); assert.equal(new URL(page.url()).searchParams.get('step'), '1');
      await page.reload(); await page.getByLabel('资源 ID（可选）', { exact: true }).waitFor();
      assert.equal(new URL(page.url()).search, savedQuery); assert.equal(await page.getByLabel('资源 ID（可选）', { exact: true }).inputValue(), 'restored-resource');
      assert((await page.getByLabel('指标', { exact: true }).innerText()).includes('容器运行状态'));
      assert((await page.getByLabel('汇总', { exact: true }).innerText()).includes('平均'));
      for (const theme of ['dark', 'light']) for (const width of [320, 768, 1440]) {
        await page.evaluate(theme => document.documentElement.classList.toggle('dark', theme === 'dark'), theme); await page.setViewportSize({ width, height: 950 }); await page.evaluate(() => scrollTo(0, 0)); await capture(`monitoring-history-${theme}-${width}`);
      }
      const before = queries.length; failure = true;
      await page.getByRole('button', { name: '查询', exact: true }).click();
      await page.getByText('S3 历史读取失败', { exact: true }).first().waitFor(); assert(queries.length > before);
      failure = false; await page.getByRole('button', { name: '查询', exact: true }).click();
      await page.getByRole('tab', { name: '采集与归档', exact: true }).click(); await page.getByText('已暂停', { exact: true }).waitFor();
      assert.equal(new URL(page.url()).searchParams.get('tab'), 'status'); await page.reload(); await page.getByRole('tab', { name: '采集与归档', exact: true, selected: true }).waitFor();
      for (const width of [320, 768, 1440]) { await page.setViewportSize({ width, height: 950 }); await capture(`monitoring-status-${width}`); }
      await page.setViewportSize({ width: 1440, height: 1000 });
    } finally { await page.unroute('**/api/v2/**', handler); }
  });
}
