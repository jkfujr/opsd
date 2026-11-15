import assert from 'node:assert/strict';

export async function checkSharedStorage({ page, go, step, capture, closeDrawers, fixtures, id, op }) {
  await step('共享库原请求恢复、代理状态、主备入口与协调转交', async () => {
    const originalNodes = fixtures.nodes, originalDatabase = fixtures.database;
    fixtures.nodes = { ...originalNodes, items: originalNodes.items.map(node => ({ ...node, coordinator_candidate: true })) };
    fixtures.database = structuredClone(originalDatabase);
    fixtures.database.cluster.coordinator = 'offline-node';
    fixtures.database.cluster.coordinator_epoch = 2;
    let bound = false, initializations = 0, recoveries = 0, transfers = 0, proxyError = true;
    let consoles = { revision: 0, primary: '', backup: '' };
    const binding = { cluster_id: 'ui-db', database: 'opsd_core', galera_id: 'local-test', revision: 1, issuer: id };
    const status = () => bound ? { binding, phase: 'active', state: proxyError ? 'error' : 'ready', proxy_id: 'local-proxy', proxy_ready: !proxyError, policy_version: 'a'.repeat(64), pending: 3, ...(proxyError ? { error: { code: 'dependency_unavailable', message: '代理暂时不可用' } } : {}) } : { phase: 'unbound', state: 'unbound', pending: 0, proxy_ready: false };
    const match = url => /^\/api\/v2\/(shared-storage(?:\/.*)?|nodes\/[^/]+\/shared-storage\/recovery|database-clusters\/ui-db\/coordinator)$/.test(url.pathname);
    await page.route(match, async route => {
      const request = route.request(), path = new URL(request.url()).pathname;
      if (path.endsWith('/initialize')) {
        initializations++;
        fixtures.receipts.set(request.headers()['idempotency-key'], op('shared_initialize', 'applying'));
        bound = true;
        return route.fulfill({ status: 504, json: { code: 'outcome_unknown', detail: '初始化应答丢失', outcome: 'unknown' } });
      }
      if (path.endsWith('/recovery')) { recoveries++; proxyError = false; return route.fulfill({ json: status() }); }
      if (path.endsWith('/consoles')) {
        if (request.method() === 'PUT') consoles = { ...request.postDataJSON(), revision: consoles.revision + 1 };
        return route.fulfill({ json: consoles });
      }
      if (path.endsWith('/coordinator')) {
        transfers++;
        fixtures.database.cluster.coordinator = request.postDataJSON().coordinator;
        fixtures.database.cluster.revision++;
        return route.fulfill({ json: fixtures.database.cluster });
      }
      return route.fulfill({ json: { local: status(), nodes: [{ node_id: id, name: '本机代理节点', status: status() }] } });
    });
    const choose = async (label, value) => {
      const trigger = page.getByLabel(label, { exact: true });
      await trigger.click();
      await page.getByRole('listbox').waitFor();
      const controls = await trigger.getAttribute('aria-controls');
      assert(controls);
      const popup = page.locator(`[id="${controls}"]`);
      await popup.getByRole('option', { name: value, exact: true }).click();
      await popup.waitFor({ state: 'hidden' });
    };
    try {
      await go('/databases');
      await choose('Galera 集群', '业务 Galera');
      await page.getByRole('button', { name: '初始化 opsd_core', exact: true }).click();
      await page.getByText('opsd_core', { exact: true }).waitFor();
      await page.getByRole('button', { name: '查询原操作', exact: true }).click();
      await page.locator('.operation-detail').waitFor();
      assert.equal(initializations, 1);
      await closeDrawers();
      await page.getByText('opsd_core', { exact: true }).waitFor();
      await choose('主用管理节点', originalNodes.items[0].name);
      await choose('备用管理节点', originalNodes.items[1].name);
      const saved = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/shared-storage/consoles') && response.request().method() === 'PUT');
      await page.getByRole('button', { name: '保存管理节点', exact: true }).click();
      await saved;
      assert.equal(consoles.primary, id);
      assert.equal(consoles.backup, 'offline-node');
      await page.getByRole('button', { name: '核对恢复', exact: true }).click();
      await page.getByRole('row').filter({ hasText: '本机代理节点' }).getByText('已连接', { exact: true }).waitFor();
      assert.equal(recoveries, 1);
      await page.setViewportSize({ width: 320, height: 780 });
      await capture('shared-storage-narrow');
      await go('/databases/ui-db');
      await page.getByRole('tab', { name: '库', exact: true }).click();
      assert(await page.getByRole('button', { name: '创建库', exact: true }).isEnabled());
      await page.getByRole('button', { name: '转交协调权', exact: true }).click();
      await choose('协调节点', originalNodes.items[0].name);
      await page.getByRole('button', { name: '转交', exact: true }).click();
      await page.getByRole('dialog').waitFor({ state: 'hidden' });
      assert.equal(transfers, 1);
      assert.equal(fixtures.database.cluster.coordinator, id);
    } finally {
      await page.unroute(match);
      fixtures.nodes = originalNodes;
      fixtures.database = originalDatabase;
      await page.setViewportSize({ width: 1440, height: 1000 });
    }
  });
}
