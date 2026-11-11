/** 开发用接口替身：由 vite.config.ts 挂到 /api/v1 上。 */
import {
  bumpFirewall,
  compose,
  containers,
  databaseClusters,
  databasesFor,
  dbStatus,
  dockerNodes,
  firewallState,
  history,
  nextId,
  nodeSeed,
  nodes,
  operations,
  peerTrust,
  readings,
  sessions,
  summary,
  tasks,
  tokens,
  type Op,
} from './fixtures';

type Reply = { status: number; body: unknown };

const reply = (body: unknown, status = 200): Reply => ({ status, body });
const iso = (offset = 0) => new Date(Date.now() + offset).toISOString();
const minutes = (n: number) => n * 60_000;
const hours = (n: number) => n * 3_600_000;

const terminal = new Set(['succeeded', 'cancelled', 'rejected', 'failed', 'reverted', 'resolved_manually']);

export function mockApi(method: string, route: string, query: URLSearchParams, rawBody?: string): Reply | null {
  const path = route.replace(/\/+$/, '') || '/';
  let body: Record<string, unknown> | undefined;
  try {
    body = rawBody ? JSON.parse(rawBody) : undefined;
  } catch {
    body = undefined;
  }

  if (path === '/auth/session' && method === 'GET') {
    return reply({
      session: { id: 's-1', username: 'admin', created_at: iso(-hours(2)), expires_at: iso(hours(20)), last_seen_at: iso(), remote_address: '10.8.0.2', current: true },
      csrf_token: 'demo-csrf',
      login_address: '/',
      local_node_id: 'n-sh-01',
    });
  }
  if (path === '/auth/session' && method === 'DELETE') return { status: 204, body: undefined };
  if (path === '/auth/sessions') return reply({ items: sessions });
  if (path.startsWith('/auth/sessions/') && method === 'DELETE') return { status: 204, body: undefined };

  if (path === '/overview') {
    const all = nodes();
    const reads = readings();
    return reply({
      updated_at: iso(),
      summary: summary(),
      nodes: all.map((node, index) => ({ node, reading: reads[index] })),
      pending_operations: operations.filter((op) => !terminal.has(op.state)),
      unavailable: [],
    });
  }

  if (path === '/nodes') return reply({ items: nodes() });
  if (/^\/nodes\/[^/]+$/.test(path)) {
    const id = path.split('/')[2];
    const node = nodes().find((item) => item.id === id) ?? nodes()[0];
    if (method === 'PATCH') return reply({ ...node, name: String(body?.name ?? node.name) });
    if (method === 'DELETE') return reply({ id: nextId(), state: 'accepted' });
    return reply(node);
  }
  if (/^\/nodes\/[^/]+\/revoke$/.test(path)) return reply({ id: nextId(), state: 'accepted' });

  if (path === '/enrollment-tokens' && method === 'GET') return reply({ items: [] });
  if (path === '/enrollment-tokens' && method === 'POST')
    return reply({
      token: 'e3f9c2a4b8d7e1f0c5a9b3d6e2f7a1c4',
      record: { id: 'et-1', created_at: iso(), expires_at: iso(hours(24)) },
      peer_address: 'http://203.0.113.7:8444',
      peer_public_key: 'pk_peer_7c1e9a',
      version: '0.1.0-dev',
      installer_url: 'http://203.0.113.7:8444/install.sh',
    });

  if (path === '/operations/pending') return reply({ items: operations.filter((op) => !terminal.has(op.state)) });
  if (path === '/operations' && method === 'GET') {
    const state = query.get('state');
    const node = query.get('node_id');
    return reply({
      items: operations.filter((op) => (!state || op.state === state) && (!node || op.node_id === node)),
    });
  }
  if (/^\/operations\/[^/]+$/.test(path) && method === 'GET') {
    const id = path.split('/')[2];
    return reply(operations.find((op) => op.id === id) ?? operations[0]);
  }
  if (/^\/operations\/[^/]+\/(confirm|recover|cancel|resolve)$/.test(path)) {
    const [, , id, action] = path.split('/');
    const op = operations.find((item) => item.id === id) as (Op & { revision: number }) | undefined;
    if (op) {
      if (action === 'cancel') op.state = 'cancelled';
      else if (action === 'resolve') op.state = 'resolved_manually';
      else op.state = 'succeeded';
      op.revision += 1;
      op.steps = (op.steps as { name: string; state: string }[]).map((step) => ({ ...step, state: step.state === 'pending' ? 'completed' : step.state }));
    }
    return reply(op ?? {});
  }

  if (path === '/monitoring/history') return reply(history(query.get('range') ?? '24h'));
  if (path === '/monitoring/current') return reply({ nodes: [], checked_at: iso() });

  if (path === '/containers') {
    const nodeFilter = query.get('node_id');
    const stateFilter = query.get('state');
    const search = (query.get('search') ?? '').toLowerCase();
    const items = containers
      .filter((item) => !nodeFilter || item.n === nodeFilter)
      .filter((item) => !stateFilter || item.state === stateFilter)
      .filter((item) => !search || item.name.toLowerCase().includes(search))
      .map((item) => ({
        engine_id: `engine-${item.n}`,
        container_id: `${item.id}9f3c1a7b2d4e8c60b7e5d3a1f0c2b4d6e8a0c1f2b3d4e5a6`.slice(0, 64),
        name: item.name,
        image: item.image,
        state: item.state,
        health: item.health,
        ports: item.ports,
        project: item.project,
        service: item.service,
        node_id: item.n,
        node_name: nodeSeed.find((seed) => seed.id === item.n)?.name ?? item.n,
      }));
    return reply({ items, nodes: dockerNodes() });
  }
  if (/^\/nodes\/[^/]+\/containers\/[^/]+$/.test(path) && method === 'GET') {
    const id = path.split('/')[4];
    const found = containers.find((item) => item.id === id) ?? containers[0];
    return reply({
      engine_id: `engine-${found.n}`,
      container_id: `${found.id}9f3c1a7b2d4e8c60b7e5d3a1f0c2b4d6e8a0c1f2b3d4e5a6`.slice(0, 64),
      name: found.name,
      image: found.image,
      state: found.state,
      health: found.health,
      ports: found.ports,
      project: found.project,
      service: found.service,
      created_at: iso(-hours(80)),
      started_at: iso(-hours(3)),
      restart_policy: found.project ? 'unless-stopped' : 'no',
      restart_count: found.state === 'restarting' ? 4 : 0,
      mounts: [
        { type: 'volume', source: `/var/lib/docker/volumes/${found.name}_data`, destination: '/var/lib/data', read_only: false },
        { type: 'bind', source: '/etc/localtime', destination: '/etc/localtime', read_only: true },
      ],
      networks: [{ name: found.project || 'bridge', ipv4: '172.18.0.4', ipv6: '' }],
      tty: false,
      checked_at: iso(-minutes(0.5)),
    });
  }
  if (/^\/nodes\/[^/]+\/containers\/[^/]+\/logs$/.test(path)) {
    const t = () => new Date().toISOString();
    const lines = [
      `${t()} [info] server listening on :5984`,
      `${t()} [info] replication check ok`,
      `${t()} [warn] slow request 1.24s GET /app/_all_docs`,
      `${t()} [info] checkpoint cluster_sh -> 4731`,
      `${t()} [info] compaction of db/app complete`,
    ];
    return reply({ engine_id: 'engine', container_id: 'c'.repeat(64), text: lines.join('\n'), truncated: false, checked_at: iso() });
  }
  if (/^\/nodes\/[^/]+\/containers\/[^/]+\/actions$/.test(path)) return reply({ id: nextId(), state: 'accepted' });

  if (path === '/databases/clusters' && method === 'GET') return reply({ items: dbStatus().items });
  if (path === '/databases/clusters' && method === 'POST') return reply({ ...databaseClusters[0], id: 'db-new' });
  if (/^\/databases\/clusters\/[^/]+$/.test(path) && method === 'GET') {
    const id = path.split('/')[3];
    const found = dbStatus().items.find((item) => item.cluster.id === id) ?? dbStatus().items[0];
    const detail = databasesFor(found.cluster.id);
    return reply({
      ...found,
      members: found.members.map((member) => ({ ...member, status: { ...member.status, ...detail } })),
      proxies: found.proxies.map((proxy) => ({ ...proxy, status: { ...proxy.status, ...detail } })),
    });
  }
  if (/^\/databases\/clusters\/[^/]+$/.test(path) && method === 'PATCH') return reply(databaseClusters[0]);
  if (/^\/databases\/clusters\/[^/]+\/changes$/.test(path)) return reply({ id: nextId(), state: 'accepted' });
  if (/^\/databases\/clusters\/[^/]+\/backups$/.test(path)) {
    return reply({
      checked_at: iso(),
      items: [
        {
          node_id: 'n-sh-01',
          instance_id: 'galera-1',
          items: [
            { name: 'nightly', schedule: '每天 03:00', state: 'ready', last_run: iso(-hours(10)), last_success: iso(-hours(10)), artifacts: ['/backup/app-20261110.sql.gz'] },
            { name: 'weekly', schedule: '每周日 04:00', state: 'ready', last_run: iso(-hours(52)), last_success: iso(-hours(52)), artifacts: [] },
          ],
        },
        { node_id: 'n-hk-01', instance_id: 'galera-3', items: [{ name: 'nightly', schedule: '每天 03:00', state: 'failed', artifacts: [], error: '目标目录空间不足' }] },
      ],
    });
  }
  if (/^\/nodes\/[^/]+\/databases\/discover$/.test(path))
    return reply({
      items: [
        { node_id: 'n-sh-01', name: 'galera-1', kind: 'galera', address: '10.8.0.11:3306', socket: '/run/mysqld/mysqld.sock', running: true, discovered: true },
        { node_id: 'n-sh-01', name: 'proxysql', kind: 'proxysql', address: '127.0.0.1:6032', socket: '', running: true, discovered: true },
      ],
    });

  if (path === '/api-tokens' && method === 'GET') return reply({ items: tokens });
  if (path === '/api-tokens' && method === 'POST')
    return reply({ token: { ...tokens[0], id: 'tok-new' }, value: 'opsd_ro_9f2c1e7a4b8d3f6a3c5e7b9d' });
  if (/^\/api-tokens\/[^/]+$/.test(path) && method === 'GET') return reply(tokens[0]);
  if (/^\/api-tokens\/[^/]+$/.test(path) && method === 'PATCH') return reply(tokens[0]);
  if (/^\/api-tokens\/[^/]+\/regenerate$/.test(path)) return reply({ token: tokens[0], value: 'opsd_ro_new_1a2b3c4d5e6f7a8b' });
  if (/^\/api-tokens\/[^/]+$/.test(path) && method === 'DELETE') return { status: 204, body: undefined };

  if (path === '/tasks') return reply(tasks);
  if (path === '/compose-projects') return reply(compose);

  if (path === '/peer-trust') return reply(peerTrust.policy);
  if (path === '/peer-trust/status') return reply(peerTrust);

  if (/^\/nodes\/[^/]+\/firewall$/.test(path)) {
    if (method === 'PATCH') {
      bumpFirewall();
      return reply({ configuration: firewallState(), operation_id: nextId() });
    }
    return reply(firewallState());
  }
  if (/^\/nodes\/[^/]+\/firewall\/(sync|install|peer-trust)$/.test(path)) return reply({ id: nextId(), state: 'accepted' });

  if (path === '/settings/account') {
    if (method === 'PATCH') return reply({ username: String(body?.username ?? 'admin') });
    return reply({ username: 'admin' });
  }
  if (path === '/settings/version') return reply({ node_version: '0.1.0-dev', frontend_version: '0.1.0-dev' });
  if (path === '/settings/navigation') {
    if (method === 'PATCH') return reply(body);
    return reply({ order: ['overview', 'nodes', 'containers', 'databases', 'storage', 'firewall', 'tools', 'settings'], revision: 2 });
  }

  if (/^\/nodes\/[^/]+\/settings$/.test(path)) {
    const id = path.split('/')[2];
    const seed = nodeSeed.find((item) => item.id === id) ?? nodeSeed[0];
    if (method === 'PATCH') return reply({ id: nextId(), state: 'accepted' });
    return reply({
      revision: 4,
      name: seed.name,
      address: seed.region,
      region: seed.region,
      listen: ':8444',
      console: true,
      console_listen: ':65533',
      preferred_storage: 'n-sh-01',
      interval_minutes: 5,
      retention_days: 7,
      storage: { enabled: seed.region === '上海', address: '10.8.0.4', port: 5984 },
      cached: false,
      address_mode: 'auto',
      api: true,
    });
  }
  if (/^\/nodes\/[^/]+\/account$/.test(path)) {
    if (method === 'PATCH') return reply({ id: nextId(), state: 'accepted' });
    return reply({ username: 'admin', entrance: '/console', revision: 3 });
  }
  if (/^\/nodes\/[^/]+\/public-addresses$/.test(path)) {
    if (method === 'PATCH')
      return reply({
        node_id: 'n-sh-01',
        revision: 5,
        ipv4: { mode: 'auto', addresses: ['203.0.113.7'], state: 'confirmed', observations: [] },
        ipv6: { mode: 'auto', addresses: ['2001:db8::7'], state: 'confirmed', observations: [] },
      });
    return reply({
      node_id: path.split('/')[2],
      revision: 4,
      cached: false,
      ipv4: { mode: 'auto', addresses: ['203.0.113.7'], state: 'confirmed', observations: [{ address: '203.0.113.7', source: 'public', checked_at: iso(-minutes(3)) }], updated_at: iso(-minutes(3)) },
      ipv6: { mode: 'manual', addresses: ['2001:db8::7'], state: 'pending', observations: [], updated_at: iso(-minutes(9)) },
    });
  }
  if (/^\/nodes\/[^/]+\/storage$/.test(path))
    return reply({ enabled: true, selected: 'n-sh-01', pending: 2, state: 'pending', replications: [{ id: 'app', state: 'active', error_count: 0 }] });
  if (/^\/nodes\/[^/]+\/executions\/[^/]+\/logs$/.test(path)) return reply({ items: [] });
  if (path === '/metrics') return { status: 200, body: '# metrics demo\n' };

  return null;
}


