/**
 * 演示数据：只在开发构建里接管 /api/v1。
 * 结构与 api/openapi.json 一致，生产构建直接请求真实节点，不包含本文件。
 */

type Json = Record<string, unknown>;

const now = () => new Date();
const iso = (offsetMs = 0) => new Date(Date.now() + offsetMs).toISOString();
const minutes = (n: number) => n * 60_000;

export const nodeSeed = [
  { id: 'n-sh-01', name: 'sh-01', region: '上海', local: true, connected: true, cpu: 31.2, mem: 42.6, disk: 61.2, rx: 2.4e6, tx: 0.9e6, fw: 'enabled', state: 'ready', cores: 8, memTotal: 16 * 1024 ** 3, diskTotal: 160 * 1024 ** 3 },
  { id: 'n-sh-02', name: 'sh-02', region: '上海', local: false, connected: true, cpu: 12.8, mem: 38.1, disk: 44.0, rx: 6.1e5, tx: 2.2e5, fw: 'enabled', state: 'ready', cores: 8, memTotal: 16 * 1024 ** 3, diskTotal: 160 * 1024 ** 3 },
  { id: 'n-hk-01', name: 'hk-01', region: '香港', local: false, connected: true, cpu: 55.4, mem: 71.9, disk: 78.5, rx: 3.3e6, tx: 1.4e6, fw: 'enabled', state: 'ready', cores: 4, memTotal: 8 * 1024 ** 3, diskTotal: 120 * 1024 ** 3 },
  { id: 'n-hk-02', name: 'hk-02', region: '香港', local: false, connected: true, cpu: 23.0, mem: 51.2, disk: 39.8, rx: 1.1e6, tx: 4.4e5, fw: 'disabled', state: 'ready', cores: 4, memTotal: 8 * 1024 ** 3, diskTotal: 120 * 1024 ** 3 },
  { id: 'n-tk-01', name: 'tk-01', region: '东京', local: false, connected: true, cpu: 76.9, mem: 88.4, disk: 52.1, rx: 8.8e5, tx: 3.1e5, fw: 'enabled', state: 'ready', cores: 2, memTotal: 4 * 1024 ** 3, diskTotal: 80 * 1024 ** 3 },
  { id: 'n-sg-01', name: 'sg-01', region: '新加坡', local: false, connected: true, cpu: 9.4, mem: 27.0, disk: 33.7, rx: 4.2e5, tx: 1.9e5, fw: 'enabled', state: 'ready', cores: 4, memTotal: 8 * 1024 ** 3, diskTotal: 160 * 1024 ** 3 },
  { id: 'n-de-01', name: 'de-01', region: '法兰克福', local: false, connected: true, cpu: 41.7, mem: 63.5, disk: 93.4, rx: 1.7e6, tx: 7.2e5, fw: 'enabled', state: 'ready', cores: 8, memTotal: 32 * 1024 ** 3, diskTotal: 500 * 1024 ** 3 },
  { id: 'n-us-01', name: 'us-01', region: '洛杉矶', local: false, connected: false, cpu: 0, mem: 0, disk: 0, rx: 0, tx: 0, fw: 'unknown', state: 'offline', cores: 4, memTotal: 8 * 1024 ** 3, diskTotal: 120 * 1024 ** 3 },
  { id: 'n-cd-01', name: 'cd-01', region: '成都', local: false, connected: true, cpu: 18.2, mem: 33.3, disk: 47.9, rx: 5.5e5, tx: 2.0e5, fw: 'none', state: 'partial', cores: 4, memTotal: 8 * 1024 ** 3, diskTotal: 200 * 1024 ** 3 },
];

const metricsFor = (seed: (typeof nodeSeed)[number], at = now()) => ({
  collected_at: at.toISOString(),
  cpu_cores: seed.cores,
  cpu_usage: seed.connected ? seed.cpu : undefined,
  memory_used: seed.connected ? (seed.memTotal * seed.mem) / 100 : undefined,
  memory_total: seed.memTotal,
  disk_used: seed.connected ? (seed.diskTotal * seed.disk) / 100 : undefined,
  disk_total: seed.diskTotal,
  network_rx: seed.connected ? seed.rx : undefined,
  network_tx: seed.connected ? seed.tx : undefined,
  errors: [],
});

export const nodes = () =>
  nodeSeed.map((seed) => ({
    id: seed.id,
    name: seed.name,
    public_key: `pk_${seed.id}_9f2c`,
    connected: seed.connected,
    writable: seed.connected,
    local: seed.local,
    region: seed.region,
    address: seed.connected ? `http://${seed.name}.mesh:8444` : '',
    console_enabled: seed.local,
    last_seen_at: iso(-minutes(seed.connected ? 0.2 : 42)),
    storage_address: seed.region === '上海' ? 'http://10.8.0.4:5984' : '',
    write_blocked_reason: seed.connected ? undefined : '节点离线',
  }));

export const readings = () =>
  nodeSeed.map((seed) => ({
    node_id: seed.id,
    state: seed.state,
    metrics: seed.connected ? metricsFor(seed) : undefined,
    error: seed.state === 'partial' ? '无法读取磁盘挂载点 /data' : undefined,
    firewall: seed.fw,
    firewall_error: seed.state === 'partial' ? '防火墙读取超时' : undefined,
    read: { state: seed.connected ? 'ready' : 'offline', data_at: iso(-minutes(0.3)) },
  }));

export const summary = () => {
  const live = nodeSeed.filter((seed) => seed.connected);
  const total = (key: 'cpu' | 'mem' | 'disk') => live.reduce((sum, seed) => sum + seed[key], 0) / live.length;
  return {
    metrics: {
      collected_at: iso(),
      cpu_cores: live.reduce((sum, seed) => sum + seed.cores, 0),
      cpu_usage: Number(total('cpu').toFixed(1)),
      memory_used: live.reduce((sum, seed) => sum + (seed.memTotal * seed.mem) / 100, 0),
      memory_total: live.reduce((sum, seed) => sum + seed.memTotal, 0),
      disk_used: live.reduce((sum, seed) => sum + (seed.diskTotal * seed.disk) / 100, 0),
      disk_total: live.reduce((sum, seed) => sum + seed.diskTotal, 0),
      network_rx: live.reduce((sum, seed) => sum + seed.rx, 0),
      network_tx: live.reduce((sum, seed) => sum + seed.tx, 0),
      errors: [],
    },
    valid_nodes: { cpu: live.length, memory: live.length, disk: live.length - 1, network: live.length },
  };
};

export type Op = Json & { id: string; state: string; revision: number };
let seq = 1041;

const operation = (over: Pdf): Op => {
  seq += 1;
  return {
    id: `op-${seq.toString(16)}-${Math.random().toString(16).slice(2, 6)}`,
    node_id: 'n-sh-01',
    executor: 'node',
    subject: 'firewall',
    state: 'succeeded',
    revision: 3,
    result_pending_sync: false,
    decision: 'undecided',
    steps: [],
    created_at: iso(-minutes(20)),
    updated_at: iso(-minutes(19)),
    ...over,
  } as Op;
};
type Pdf = Json;

export const operations: Op[] = [
  operation({
    node_id: 'n-de-01',
    subject: 'firewall_config',
    state: 'awaiting_confirmation',
    decision: 'keep',
    created_at: iso(-minutes(2)),
    updated_at: iso(-minutes(1)),
    last_observed_at: iso(-minutes(1)),
    confirmation_deadline: iso(minutes(2)),
    confirmation_remaining_seconds: 102,
    queue_sequence: 41,
    actor: 'admin',
    steps: [
      { name: '读取当前规则', state: 'completed', observed_at: iso(-minutes(2)) },
      { name: '应用端口放行', state: 'completed', observed_at: iso(-minutes(1.6)) },
      { name: '读回规则', state: 'completed', observed_at: iso(-minutes(1.2)) },
      { name: '等待确认', state: 'pending' },
    ],
  }),
  operation({
    node_id: 'n-tk-01',
    subject: 'container',
    state: 'attention_required',
    created_at: iso(-minutes(14)),
    updated_at: iso(-minutes(11)),
    last_observed_at: iso(-minutes(11)),
    queue_sequence: 40,
    actor: 'admin',
    result_pending_sync: true,
    steps: [
      { name: '发送重启', state: 'applied', observed_at: iso(-minutes(13)) },
      { name: '读回状态', state: 'unknown' },
    ],
  }),
  operation({
    node_id: 'n-hk-01',
    subject: 'database',
    state: 'accepted',
    decision: 'commit',
    created_at: iso(-minutes(4)),
    updated_at: iso(-minutes(4)),
    queue_sequence: 42,
    actor: 'admin',
    steps: [{ name: '排队等待执行', state: 'pending' }],
  }),
  operation({
    node_id: 'n-sh-01',
    subject: 'name',
    state: 'succeeded',
    created_at: iso(-minutes(48)),
    updated_at: iso(-minutes(47)),
    steps: [{ name: '保存名称', state: 'completed' }],
  }),
  operation({
    node_id: 'n-hk-02',
    subject: 'firewall',
    state: 'succeeded',
    created_at: iso(-minutes(96)),
    updated_at: iso(-minutes(95)),
    steps: [{ name: '添加规则', state: 'completed' }],
  }),
  operation({
    node_id: 'n-sg-01',
    subject: 'config',
    state: 'failed',
    created_at: iso(-minutes(130)),
    updated_at: iso(-minutes(128)),
    error: { code: 'listen_in_use', message: '监听地址已被占用：0.0.0.0:8444' },
    steps: [{ name: '停止节点服务', state: 'failed' }],
  }),
  operation({
    node_id: 'n-cd-01',
    subject: 'component',
    state: 'reverted',
    created_at: iso(-minutes(180)),
    updated_at: iso(-minutes(170)),
    steps: [{ name: '应用', state: 'reverted' }],
  }),
  operation({
    node_id: 'n-sh-02',
    subject: 'container',
    state: 'succeeded',
    created_at: iso(-minutes(220)),
    updated_at: iso(-minutes(219)),
    steps: [{ name: '启动', state: 'completed' }],
  }),
];

export const containers = [
  { n: 'n-sh-01', id: 'a1', name: 'opsd-couchdb', image: 'couchdb:3.3', state: 'running', health: 'healthy', ports: [{ host_ip: '10.8.0.4', host_port: 5984, container_port: 5984, protocol: 'tcp' }], project: '', service: '' },
  { n: 'n-sh-01', id: 'a2', name: 'galera-1', image: 'mariadb:11.4', state: 'running', health: 'healthy', ports: [{ host_ip: '10.8.0.11', host_port: 3306, container_port: 3306, protocol: 'tcp' }], project: 'galera', service: 'db1' },
  { n: 'n-sh-01', id: 'a3', name: 'proxysql', image: 'proxysql:2.6', state: 'running', health: '', ports: [{ host_ip: '0.0.0.0', host_port: 6033, container_port: 6033, protocol: 'tcp' }], project: 'galera', service: 'proxy' },
  { n: 'n-sh-02', id: 'b1', name: 'galera-2', image: 'mariadb:11.4', state: 'running', health: 'healthy', ports: [{ host_ip: '10.8.0.12', host_port: 3306, container_port: 3306, protocol: 'tcp' }], project: 'galera', service: 'db2' },
  { n: 'n-sh-02', id: 'b2', name: 'caddy', image: 'caddy:2.8', state: 'running', health: '', ports: [{ host_ip: '0.0.0.0', host_port: 80, container_port: 80, protocol: 'tcp' }, { host_ip: '0.0.0.0', host_port: 443, container_port: 443, protocol: 'tcp' }], project: '', service: '' },
  { n: 'n-hk-01', id: 'c1', name: 'galera-3', image: 'mariadb:11.4', state: 'running', health: 'healthy', ports: [{ host_ip: '10.8.0.13', host_port: 3306, container_port: 3306, protocol: 'tcp' }], project: 'galera', service: 'db3' },
  { n: 'n-hk-01', id: 'c2', name: 'opsd-couchdb', image: 'couchdb:3.3', state: 'running', health: 'starting', ports: [{ host_ip: '10.8.0.5', host_port: 5984, container_port: 5984, protocol: 'tcp' }], project: '', service: '' },
  { n: 'n-hk-02', id: 'd1', name: 'uptime-kuma', image: 'louislam/uptime-kuma:1', state: 'running', health: 'healthy', ports: [{ host_ip: '0.0.0.0', host_port: 3001, container_port: 3001, protocol: 'tcp' }], project: '', service: '' },
  { n: 'n-tk-01', id: 'e1', name: 'bot-worker', image: 'ghcr.io/acme/bot:2026.11', state: 'restarting', health: '', ports: [], project: '', service: '' },
  { n: 'n-tk-01', id: 'e2', name: 'redis', image: 'redis:7.4', state: 'running', health: 'healthy', ports: [{ host_ip: '127.0.0.1', host_port: 6379, container_port: 6379, protocol: 'tcp' }], project: '', service: '' },
  { n: 'n-sg-01', id: 'f1', name: 'minio', image: 'minio/minio:2026', state: 'exited', health: '', ports: [{ host_ip: '0.0.0.0', host_port: 9000, container_port: 9000, protocol: 'tcp' }], project: '', service: '' },
  { n: 'n-de-01', id: 'g1', name: 'influxdb', image: 'influxdb:2.7', state: 'running', health: 'healthy', ports: [{ host_ip: '0.0.0.0', host_port: 8086, container_port: 8086, protocol: 'tcp' }], project: '', service: '' },
  { n: 'n-de-01', id: 'g2', name: 'grafana', image: 'grafana/grafana:11', state: 'running', health: '', ports: [{ host_ip: '0.0.0.0', host_port: 3000, container_port: 3000, protocol: 'tcp' }], project: '', service: '' },
];

export const dockerNodes = () =>
  nodeSeed.map((seed) => ({
    node_id: seed.id,
    node_name: seed.name,
    engine_id: `engine-${seed.id}`,
    version: seed.connected && seed.fw !== 'none' ? '27.4.1' : '',
    state: !seed.connected ? 'offline' : seed.fw === 'none' ? 'not_installed' : 'ready',
    checked_at: iso(-minutes(0.4)),
    read: { state: seed.connected ? 'ready' : 'offline' },
  }));

const firewallRules = [
  { id: 'r-1', spec: { family: 'ipv4', source: '0.0.0.0/0', destination: '', protocol: 'tcp', port: '22', action: 'allow', comment: '维护入口' }, chain: 'ufw-user-input', zone: '', priority: 10 },
  { id: 'r-2', spec: { family: 'ipv4', source: '0.0.0.0/0', destination: '', protocol: 'tcp', port: '80,443', action: 'allow', comment: 'Caddy 入口' }, chain: 'ufw-user-input', zone: '', priority: 20 },
  { id: 'r-3', spec: { family: 'ipv4', source: '10.8.0.0/24', destination: '', protocol: 'any', port: '', action: 'allow', comment: '组网内互信' }, chain: 'ufw-user-input', zone: '', priority: 30 },
  { id: 'r-4', spec: { family: 'ipv4', source: '0.0.0.0/0', destination: '', protocol: 'tcp', port: '3306', action: 'deny', comment: '数据库仅组网可达' }, chain: 'ufw-user-input', zone: '', priority: 40 },
  { id: 'r-5', spec: { family: 'ipv6', source: '::/0', destination: '', protocol: 'tcp', port: '22', action: 'allow', comment: '' }, chain: 'ufw6-user-input', zone: '', priority: 10 },
  { id: 'r-6', spec: { family: 'ipv4', source: '203.0.113.0/24', destination: '', protocol: 'tcp', port: '5984', action: 'reject', comment: '扫描来源' }, chain: 'ufw-user-input', zone: '', priority: 50 },
];

export let firewallRevision = 7;

export const firewallState = () => ({
  backend: 'ufw',
  revision: firewallRevision,
  confirmed_revision: firewallRevision - 1,
  state: 'pending',
  rules: firewallRules,
  default_policies: [
    { family: 'ipv4', chain: 'input', action: 'deny' },
    { family: 'ipv6', chain: 'input', action: 'deny' },
  ],
  service_active: true,
  checked_at: iso(-minutes(0.5)),
  capabilities: {
    read: { supported: true },
    edit_rules: { supported: true },
    reorder: { supported: true },
    default_policy: { supported: true },
    enable: { supported: true },
    install: { supported: true },
    docker_ports: { supported: false, reason: 'Docker 发布端口策略尚未启用' },
    peer_trust_host_ipv4: { supported: true },
    peer_trust_host_ipv6: { supported: true },
    peer_trust_docker_ipv4: { supported: false },
    peer_trust_docker_ipv6: { supported: false },
  },
});

export const databaseClusters = [
  {
    id: 'db-sh',
    name: '上海主库',
    coordinator: 'n-sh-01',
    cluster_id: 'galera-sh',
    revision: 12,
    members: [
      { node_id: 'n-sh-01', instance_id: 'galera-1' },
      { node_id: 'n-sh-02', instance_id: 'galera-2' },
      { node_id: 'n-hk-01', instance_id: 'galera-3' },
    ],
    proxies: [
      { node_id: 'n-sh-01', instance_id: 'proxysql' },
      { node_id: 'n-sh-02', instance_id: 'proxysql' },
    ],
    workbench_url: '',
    deleted: false,
    updated_at: iso(-minutes(30)),
  },
  {
    id: 'db-hk',
    name: '香港只读',
    coordinator: 'n-hk-01',
    cluster_id: 'galera-hk',
    revision: 4,
    members: [
      { node_id: 'n-hk-01', instance_id: 'galera-3' },
      { node_id: 'n-hk-02', instance_id: 'galera-4' },
    ],
    proxies: [{ node_id: 'n-hk-02', instance_id: 'proxysql' }],
    workbench_url: '',
    deleted: false,
    updated_at: iso(-minutes(90)),
  },
];

export const databasesFor = (clusterId: string) => {
  const cluster = databaseClusters.find((item) => item.id === clusterId) ?? databaseClusters[0];
  return {
    databases: [
      { name: 'app', charset: 'utf8mb4', collation: 'utf8mb4_uca1400_ai_ci', bytes: 2.4e9, protected: false },
      { name: 'billing', charset: 'utf8mb4', collation: 'utf8mb4_uca1400_ai_ci', bytes: 8.1e8, protected: false },
      { name: 'mysql', charset: 'utf8mb3', collation: 'utf8mb3_general_ci', bytes: 3.2e7, protected: true },
      { name: 'performance_schema', charset: 'utf8mb4', collation: 'utf8mb4_0900_ai_ci', bytes: 0, protected: true },
    ],
    users: [
      { name: 'app_rw', host: '%', protected: false, grants: [{ database: 'app', privileges: ['SELECT', 'INSERT', 'UPDATE', 'DELETE'] }] },
      { name: 'app_ro', host: '%', protected: false, grants: [{ database: 'app', privileges: ['SELECT'] }] },
      { name: 'billing', host: '%', protected: false, grants: [{ database: 'billing', privileges: ['SELECT', 'INSERT', 'UPDATE'] }] },
      { name: 'backup', host: '10.8.0.%', protected: true, grants: [] },
    ],
    servers: [
      { hostgroup: 0, host: '10.8.0.11', port: 3306, state: 'ONLINE' },
      { hostgroup: 0, host: '10.8.0.12', port: 3306, state: 'ONLINE' },
      { hostgroup: 1, host: '10.8.0.13', port: 3306, state: 'ONLINE' },
    ],
    proxy_users: [
      { name: 'app_rw', active: true, hostgroup: 0, state: 'ONLINE', protected: false },
      { name: 'app_ro', active: true, hostgroup: 1, state: 'ONLINE', protected: false },
    ],
    version: '11.4.2-MariaDB',
    cluster_id: cluster.cluster_id,
    cluster_name: cluster.name,
    configured_members: 'galera-1,galera-2,galera-3',
    actual_members: 'galera-1,galera-2,galera-3',
    cluster_size: 3,
    connections: 42,
    receive_queue: 0,
    send_queue: 0,
    flow_control: 0.02,
  };
};

export const dbStatus = () => ({
  items: databaseClusters.map((cluster) => ({
    cluster,
    members: cluster.members.map((member, index) => ({
      ...member,
      name: member.instance_id,
      address: `10.8.0.${11 + index}:3306`,
      status: {
        ...databasesFor(cluster.id),
        state: index === 2 ? 'stale' : 'ready',
        primary: index === 0,
        ready: index !== 2,
        synced: index !== 2,
        read_only: index !== 0,
        connections: 12 + index * 3,
        checked_at: iso(-minutes(0.6)),
        read: { state: 'ready' },
      },
    })),
    proxies: cluster.proxies.map((proxy, index) => ({
      ...proxy,
      name: proxy.instance_id,
      address: `10.8.0.2${index + 1}:6033`,
      status: {
        state: 'ready',
        version: '2.6.3',
        connections: 26 - index * 8,
        servers: [
          { hostgroup: 0, host: '10.8.0.11', port: 3306, state: 'ONLINE' },
          { hostgroup: 0, host: '10.8.0.12', port: 3306, state: 'ONLINE' },
        ],
        pools: [
          { hostgroup: 0, srv_host: '10.8.0.11', srv_port: 3306, status: 'ONLINE', conn_used: 18, conn_free: 82, latency_us: 340 },
          { hostgroup: 1, srv_host: '10.8.0.13', srv_port: 3306, status: 'ONLINE', conn_used: 4, conn_free: 96, latency_us: 880 },
        ],
        proxy_users: [
          { name: 'app_rw', active: true, hostgroup: 0, state: 'ONLINE', protected: false },
        ],
        checked_at: iso(-minutes(0.6)),
        read: { state: 'ready' },
      },
    })),
    checked_at: iso(-minutes(0.6)),
  })),
});

export const history = (range: string) => {
  const count = range === '7d' ? 336 : 288;
  const stepMs = range === '7d' ? 30 * 60_000 : 5 * 60_000;
  const items = Array.from({ length: count }, (_, index) => {
    const t = Date.now() - (count - 1 - index) * stepMs;
    const wave = Math.sin(index / 9) * 6 + Math.sin(index / 31) * 9;
    const base = 38 + wave;
    return {
      collected_at: new Date(t).toISOString(),
      summary: {
        metrics: {
          collected_at: new Date(t).toISOString(),
          cpu_cores: 46,
          cpu_usage: Math.max(4, Number((base + Math.sin(index / 3) * 4.2).toFixed(1))),
          memory_used: ((58 + wave * 0.8) / 100) * 92 * 1024 ** 3,
          memory_total: 92 * 1024 ** 3,
          disk_used: ((61 + wave * 0.12) / 100) * 1520 * 1024 ** 3,
          disk_total: 1520 * 1024 ** 3,
          network_rx: Math.max(1e4, 1.9e6 + Math.sin(index / 5) * 1.4e6 + Math.sin(index / 17) * 9e5),
          network_tx: Math.max(8e3, 7.4e5 + Math.sin(index / 6) * 5.2e5),
          errors: [],
        },
        valid_nodes: { cpu: 8, memory: 8, disk: 7, network: 8 },
      },
    };
  });
  return { items, interval_minutes: range === '7d' ? 30 : 5, checked_at: iso(), unavailable: [] };
};

export const sessions = [
  { id: 's-1', username: 'admin', created_at: iso(-hours(2)), expires_at: iso(hours(20)), last_seen_at: iso(), remote_address: '10.8.0.2', current: true },
  { id: 's-2', username: 'admin', created_at: iso(-hours(26)), expires_at: iso(hours(-2)), last_seen_at: iso(-hours(3)), remote_address: '198.51.100.24', current: false },
];

function hours(n: number) {
  return n * 3600_000;
}

export const tokens = [
  {
    id: 'tok-1',
    owner: 'sh-01',
    name: 'Grafana 只读',
    enabled: true,
    revision: 2,
    grants: [{ module: 'monitoring', level: 'read', scope: 'all', resources: [] }],
    source_ips: [],
    created_at: iso(-hours(72)),
  },
  {
    id: 'tok-2',
    owner: 'sh-01',
    name: '备份脚本',
    enabled: true,
    revision: 1,
    grants: [
      { module: 'databases', level: 'read', scope: 'clusters', resources: ['db-sh'] },
      { module: 'nodes', level: 'read', scope: 'nodes', resources: ['n-sh-01'] },
    ],
    source_ips: ['10.8.0.0/24'],
    created_at: iso(-hours(200)),
  },
];

export const tasks = {
  items: [
    { id: 'task-1', revision: 2, name: '每日备份校验', schedule: '0 3 * * *', timezone: 'Asia/Shanghai', script: '#!/bin/sh\nrclone check /backup remote:backup', directory: '/opt/opsd', enabled: true, next_at: iso(hours(9)), node_id: 'n-sh-01', node_name: 'sh-01' },
    { id: 'task-2', revision: 1, name: '证书续期', schedule: '0 4 * * 1', timezone: 'Asia/Shanghai', script: '#!/bin/sh\ncaddy reload', directory: '/opt/caddy', enabled: true, next_at: iso(hours(30)), node_id: 'n-sh-02', node_name: 'sh-02' },
  ],
  unavailable: [],
  nodes: [{ node_id: 'n-sh-01', node_name: 'sh-01', state: 'ready' }],
};

export const compose = {
  items: [
    { id: 'galera', engine_id: 'engine-n-sh-01', name: 'galera', directory: '/srv/galera', files: ['compose.yml'], environment_files: ['.env'], registered: true, services: [], checked_at: iso(-minutes(1)), node_id: 'n-sh-01', node_name: 'sh-01' },
  ],
  nodes: [{ node_id: 'n-sh-01', node_name: 'sh-01', state: 'ready' }],
};

export const peerTrust = {
  policy: { version: { revision: 3 }, enabled: true },
  nodes: nodeSeed.map((item) => ({
    node_id: item.id,
    enabled: true,
    policy_version: { revision: 3 },
    target_revision: 3,
    applied_revision: item.connected ? 3 : 2,
    state: item.connected ? 'in_sync' : 'offline',
    rules: [
      { address: '203.0.113.7', family: 'ipv4', members: [{ id: 'n-sh-01', name: 'sh-01' }, { id: 'n-sh-02', name: 'sh-02' }], state: item.connected ? 'in_sync' : 'pending' },
      { address: '2001:db8::7', family: 'ipv6', members: [{ id: 'n-hk-01', name: 'hk-01' }], state: 'in_sync' },
    ],
    results: [],
    checked_at: iso(-minutes(1)),
  })),
};

export function nextId(): string {
  seq += 1;
  return `op-${seq.toString(16)}-${Math.random().toString(16).slice(2, 6)}`;
}

export function bumpFirewall() {
  firewallRevision += 1;
  return firewallRevision;
}


