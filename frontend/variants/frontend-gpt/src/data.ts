export type NodeItem = {
  id: string; name: string; code: string; region: string; country: string; address: string; ipv6: string;
  online: boolean; cpu: number; memory: number; disk: number; cores: number; ram: number; capacity: number;
  rx: number; tx: number; os: string; uptime: string;
  couch?: boolean; autoAddress?: boolean; couchBinding?: string; couchPort?: string;
};
export const initialNodes: NodeItem[] = [
  { id: 'tyo-01', name: 'edge-tokyo-01', code: 'TYO', region: '东京', country: '日本', address: '203.0.113.21', ipv6: '2001:db8:1200:0:2a0:98ff:fe12:34ab', online: true, cpu: 32.4, memory: 61.2, disk: 48.3, cores: 8, ram: 16, capacity: 160, rx: 12.4, tx: 6.8, os: 'Debian 12', uptime: '32 天 14 小时' },
  { id: 'sgp-01', name: 'core-singapore-01', code: 'SIN', region: '新加坡', country: '新加坡', address: '203.0.113.42', ipv6: '2001:db8:2400::42', online: true, cpu: 18.6, memory: 43.8, disk: 36.7, cores: 8, ram: 16, capacity: 160, rx: 8.2, tx: 4.3, os: 'Ubuntu 24.04', uptime: '18 天 6 小时' },
  { id: 'fra-01', name: 'worker-frankfurt-production-01', code: 'FRA', region: '法兰克福', country: '德国', address: '198.51.100.18', ipv6: '2001:db8:3300:10:abcd:12ef:3456:7890', online: true, cpu: 46.8, memory: 72.4, disk: 87.2, cores: 4, ram: 8, capacity: 80, rx: 3.6, tx: 2.1, os: 'Debian 12', uptime: '46 天 2 小时' },
  { id: 'lax-01', name: 'edge-losangeles-01', code: 'LAX', region: '洛杉矶', country: '美国', address: '198.51.100.57', ipv6: '', online: true, cpu: 11.3, memory: 34.6, disk: 29.4, cores: 4, ram: 8, capacity: 80, rx: 5.1, tx: 2.7, os: 'Ubuntu 24.04', uptime: '12 天 21 小时' },
  { id: 'hkg-01', name: 'backup-hongkong-01', code: 'HKG', region: '香港', country: '中国', address: '192.0.2.35', ipv6: '2001:db8:5500::35', online: false, cpu: 0, memory: 0, disk: 0, cores: 4, ram: 8, capacity: 160, rx: 0, tx: 0, os: 'Debian 12', uptime: '—' },
];
export type ContainerItem = { id: string; name: string; nodeId: string; image: string; status: 'running' | 'stopped'; ports: string; project: string; managed?: boolean };
export const initialContainers: ContainerItem[] = [
  { id: 'c1', name: 'gateway', nodeId: 'tyo-01', image: 'nginx:1.28-alpine', status: 'running', ports: '80 → 80 / 443 → 443', project: 'edge-stack' },
  { id: 'c2', name: 'mariadb-01', nodeId: 'tyo-01', image: 'mariadb:12.2', status: 'running', ports: '3306 → 3306', project: 'database' },
  { id: 'c3', name: 'redis-cache', nodeId: 'sgp-01', image: 'redis:8-alpine', status: 'running', ports: '6379 → 6379', project: 'core-services' },
  { id: 'c4', name: 'couchdb', nodeId: 'sgp-01', image: 'couchdb:3.5', status: 'running', ports: '5984 → 5984', project: 'opsd-data', managed: true },
  { id: 'c5', name: 'worker', nodeId: 'fra-01', image: 'example/worker:2.4.0', status: 'running', ports: '—', project: 'workers' },
  { id: 'c6', name: 'prometheus', nodeId: 'lax-01', image: 'prom/prometheus:v3.9.1', status: 'running', ports: '9090 → 9090', project: 'monitoring' },
  { id: 'c7', name: 'grafana', nodeId: 'lax-01', image: 'grafana/grafana:12.3.0', status: 'running', ports: '3000 → 3000', project: 'monitoring' },
  { id: 'c8', name: 'backup-runner', nodeId: 'tyo-01', image: 'alpine:3.22', status: 'stopped', ports: '—', project: 'backups' },
];
export type OperationItem = { id: string; title: string; nodeId: string; state: 'success' | 'running' | 'confirm' | 'unknown' | 'cancelled'; time: string; detail: string; containerUpdate?: { id: string; image: string } };
export const initialOperations: OperationItem[] = [
  { id: 'OP-1042', title: '更新防火墙规则', nodeId: 'tyo-01', state: 'confirm', time: '14:32', detail: '新的入站规则已写入。' },
  { id: 'OP-1041', title: '重启 gateway', nodeId: 'tyo-01', state: 'success', time: '14:28', detail: '容器已重新启动，状态读取完成。' },
  { id: 'OP-1040', title: '同步数据库代理', nodeId: 'sgp-01', state: 'success', time: '14:16', detail: '运行配置与持久配置读取一致。' },
];
export type RuleItem = { id: string; nodeId?: string; name: string; port: string; protocol: string; source: string; action: '允许' | '拒绝'; enabled: boolean; family: 'IPv4' | 'IPv6' };
export const initialRules: RuleItem[] = [
  { id: 'r1', name: 'SSH 管理', port: '22', protocol: 'TCP', source: '203.0.113.0/24', action: '允许', enabled: true, family: 'IPv4' },
  { id: 'r2', name: 'HTTP', port: '80', protocol: 'TCP', source: '任意来源', action: '允许', enabled: true, family: 'IPv4' },
  { id: 'r3', name: 'HTTPS', port: '443', protocol: 'TCP', source: '任意来源', action: '允许', enabled: true, family: 'IPv4' },
  { id: 'r4', name: '节点通信', port: '8444', protocol: 'TCP', source: '任意来源', action: '允许', enabled: true, family: 'IPv4' },
  { id: 'r5', name: 'HTTPS IPv6', port: '443', protocol: 'TCP', source: '::/0', action: '允许', enabled: true, family: 'IPv6' },
  { id: 'r6', name: '限制来源', port: '全部', protocol: '全部', source: '192.0.2.0/24', action: '拒绝', enabled: true, family: 'IPv4' },
];
export type TaskItem = { id: string; nodeId: string; name: string; command: string; schedule: string; lastRun: string; state: 'success' | 'idle' | 'running' | 'cancelled' };
export const initialTasks: TaskItem[] = [
  { id: 't1', nodeId: 'tyo-01', name: '检查磁盘使用情况', command: 'df -h', schedule: '手动', lastRun: '14:20', state: 'success' },
  { id: 't2', nodeId: 'tyo-01', name: '查询服务运行状态', command: 'systemctl status nginx', schedule: '手动', lastRun: '13:45', state: 'success' },
];
export const series = Array.from({ length: 49 }, (_, i) => ({
  time: i / 2,
  cpu: Math.round((25 + Math.sin(i * 0.38) * 7 + Math.sin(i * 1.7) * 3 + (i > 28 && i < 34 ? 20 : 0)) * 10) / 10,
  memory: Math.round((48 + Math.sin(i * 0.14) * 6 + Math.cos(i * 0.7) * 2 + i * 0.14) * 10) / 10,
  network: Math.round((18 + Math.sin(i * 0.31) * 8 + Math.cos(i * 0.9) * 3) * 10) / 10,
}));
export const logs = [
  '2026-11-10 14:32:18 [info] 服务健康检查通过',
  '2026-11-10 14:32:17 [info] GET /health 200 0.8ms',
  '2026-11-10 14:32:15 [info] GET /api/status 200 12.4ms',
  '2026-11-10 14:32:12 [info] 已建立上游连接',
  '2026-11-10 14:32:10 [warn] 上游响应耗时 842ms',
  '2026-11-10 14:32:06 [info] GET /assets/app.js 200 2.1ms',
  '2026-11-10 14:32:03 [info] GET / 200 4.7ms',
  '2026-11-10 14:31:58 [info] 已刷新本地配置',
];
export type ClusterItem = { id: string; name: string; coordinator: string };
export type DatabaseItem = { id: string; clusterId: string; name: string; charset: string; size: string };
export type AccountItem = { id: string; clusterId: string; name: string; host: string; database: string; permission: string; protected: boolean };
export const initialDatabases: DatabaseItem[] = [
  { id: 'db1', clusterId: 'primary', name: 'application', charset: 'utf8mb4', size: '2.4 GiB' },
  { id: 'db2', clusterId: 'primary', name: 'analytics', charset: 'utf8mb4', size: '842 MiB' },
  { id: 'db3', clusterId: 'primary', name: 'events', charset: 'utf8mb4', size: '1.1 GiB' },
];
export const initialAccounts: AccountItem[] = [
  { id: 'a1', clusterId: 'primary', name: 'app_writer', host: '%', database: 'application', permission: '读写', protected: false },
  { id: 'a2', clusterId: 'primary', name: 'analytics_reader', host: '%', database: 'analytics', permission: '只读', protected: false },
  { id: 'a3', clusterId: 'primary', name: 'monitor', host: 'localhost', database: '系统', permission: '监控', protected: true },
  { id: 'a4', clusterId: 'primary', name: 'root', host: 'localhost', database: '全部', permission: '管理员', protected: true },
];
