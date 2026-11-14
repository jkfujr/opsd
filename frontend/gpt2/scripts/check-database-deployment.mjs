import assert from 'node:assert/strict';

export async function checkDatabaseDeployment({ page, go, step, capture, closeDrawers, fixtures, id, op }) {
  await step('数据库创建接入扩容、原请求查询与窄屏', async () => {
    const writes = [], previews = [], scans = [];
    let lost = false, blocked = false;
    const match = url => /^\/api\/v2\/database-clusters\/(discovery|deployment-previews|deployments|ui-db\/(members|proxies)\/actions)$/.test(url.pathname);
    const originalNodes = fixtures.nodes;
    const second = { ...originalNodes.items[0], id: 'deployment-node', name: '数据库候选节点', online: true, state: 'online' };
    fixtures.nodes = { ...originalNodes, items: [...originalNodes.items, second] };
    const instance = { id: 'found-galera', node_id: id, name: 'MariaDB', kind: 'galera', address: '127.0.0.1:3306', username: 'root', credentials_available: true, running: true, revision: 0, protected_users: [] };
    await page.route(match, async route => {
      const path = new URL(route.request().url()).pathname, body = route.request().postDataJSON();
      if (path.endsWith('/discovery')) {
        scans.push(body);
        return route.fulfill({ json: { groups: [{ cluster_id: 'found-cluster', name: '已发现集群', registered_id: '', members: [{ instance, status: { cluster_id: 'found-cluster', primary: true, ready: true, synced: true, member_address: '192.0.2.10:3306' } }], proxies: [] }], unresolved: [{ instance: { ...instance, id: 'missing-password', name: '待补充凭据' }, status: { error: { code: 'unavailable', message: '请补充管理凭据' } } }], problems: [] } });
      }
      if (path.endsWith('/deployment-previews')) {
        previews.push(body);
        return route.fulfill({ json: { id: 'deployment-preview', coordinator: id, cluster_id: 'ui-db', revision: 'revision', token: blocked ? '' : 'encrypted-preview', expires_at: new Date(Date.now() + 600000).toISOString(), targets: [{ node_id: id, name: '数据库节点', address: '192.0.2.10', directory: '/var/lib/opsd/databases/new', image: 'mariadb:11.8.9', docker_state: 'not_installed', free_bytes: 10 * 1024 ** 3, ports: [3306, 4567, 4568, 4444], problems: [] }], network: [{ node_id: id, source: '192.0.2.11', protocol: 'tcp', ports: '3306,4444,4567,4568' }], problems: blocked ? ['TCP 端口被占用，请调整端口'] : [] } });
      }
      const value = op('database'); value.action = 'create_cluster'; value.node_id = id;
      const key = route.request().headers()['idempotency-key']; writes.push({ path, body, key });
      if (lost) { fixtures.receipts.set(key, value); return route.abort('failed'); }
      return route.fulfill({ status: 202, json: value });
    });
    try {
      await page.setViewportSize({ width: 1440, height: 900 });
      await go('/databases'); await page.getByRole('button', { name: '新建集群', exact: true }).click();
      await page.getByLabel('集群名称', { exact: true }).fill('单节点创建');
      await page.getByRole('dialog').getByRole('checkbox').first().check();
      await page.getByRole('button', { name: '预览变更', exact: true }).click();
      await page.getByRole('heading', { name: '确认新建集群', exact: true }).waitFor();
      assert.equal(previews.at(-1).targets.length, 1);
      assert.equal(await page.getByText('安装', { exact: true }).count(), 1);
      await capture('database-create-preview');
      lost = true;
      await page.getByRole('button', { name: '确认新建集群', exact: true }).click();
      await page.getByRole('button', { name: '查询原操作', exact: true }).click();
      await page.locator('.operation-detail').waitFor(); assert.equal(writes.length, 1); await closeDrawers(); lost = false;

      await go('/databases'); await page.getByRole('button', { name: '接入已有集群', exact: true }).click();
      await page.getByLabel('管理密码', { exact: true }).fill('temporary-secret');
      await page.getByRole('button', { name: '重新读取', exact: true }).click();
      await page.getByRole('button', { name: '重新读取', exact: true }).waitFor({ state: 'visible' });
      await page.getByRole('button', { name: '预览变更', exact: true }).click();
      await page.getByRole('heading', { name: '确认接入已有集群', exact: true }).waitFor();
      assert.equal(previews.at(-1).galera_id, 'found-cluster'); assert(scans.some(scan => scan.credentials.some(item => item.password === 'temporary-secret')));
      await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click(); assert.equal(writes.length, 1);

      await page.setViewportSize({ width: 320, height: 780 });
      await go('/databases/ui-db'); await page.getByRole('tab', { name: '成员', exact: true }).click();
      await page.getByRole('button', { name: '添加节点', exact: true }).click();
      await page.getByRole('dialog').getByRole('checkbox').first().check(); blocked = true;
      await page.getByRole('button', { name: '预览变更', exact: true }).click();
      await page.getByText('TCP 端口被占用，请调整端口', { exact: true }).waitFor();
      assert(await page.getByRole('button', { name: '确认添加节点', exact: true }).isDisabled());
      assert.equal(previews.at(-1).mode, 'members');
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
      await capture('database-members-narrow');
      await page.getByRole('button', { name: '返回修改', exact: true }).click(); blocked = false;
      await page.getByRole('button', { name: '预览变更', exact: true }).click();
      await page.getByRole('button', { name: '确认添加节点', exact: true }).click(); await page.locator('.operation-detail').waitFor(); await closeDrawers();
      assert(writes.at(-1).path.endsWith('/members/actions'));
      await page.setViewportSize({ width: 1440, height: 900 });
      await go('/databases/ui-db'); await page.getByRole('tab', { name: '代理', exact: true }).click();
      await page.getByRole('button', { name: '添加代理', exact: true }).click();
      await page.getByRole('dialog').getByRole('checkbox').last().check();
      await page.getByRole('button', { name: '预览变更', exact: true }).click();
      await page.getByRole('button', { name: '确认添加代理', exact: true }).click(); await page.locator('.operation-detail').waitFor(); await closeDrawers();
      assert(writes.at(-1).path.endsWith('/proxies/actions')); assert.equal(previews.at(-1).targets[0].node_id, 'deployment-node');
    } finally { fixtures.nodes = originalNodes; await page.unroute(match); await page.setViewportSize({ width: 1440, height: 900 }); }
  });
}
