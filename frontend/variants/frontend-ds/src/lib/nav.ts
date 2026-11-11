import type { IconName } from '../components/Icon';

export type NavId = 'overview' | 'nodes' | 'containers' | 'databases' | 'storage' | 'firewall' | 'settings';

export const navigation: { id: NavId; path: string; label: string; icon: IconName; hint: string }[] = [
  { id: 'overview', path: '/', label: '概览', icon: 'overview', hint: '全舰队读数' },
  { id: 'nodes', path: '/nodes', label: '节点', icon: 'nodes', hint: '接入与身份' },
  { id: 'containers', path: '/containers', label: '容器', icon: 'containers', hint: 'Docker 运行态' },
  { id: 'databases', path: '/databases', label: '数据库', icon: 'database', hint: 'Galera 与代理' },
  { id: 'storage', path: '/storage', label: '存储', icon: 'storage', hint: 'Silo 未接入' },
  { id: 'firewall', path: '/firewall', label: '防火墙', icon: 'firewall', hint: '规则与互信' },
  { id: 'settings', path: '/settings', label: '设置', icon: 'settings', hint: '账号与接口' },
];

export const navById = new Map(navigation.map((item) => [item.id, item]));

/** 后端菜单顺序含一个尚未渲染色工具位；保存时保持它的原位置。 */
export function visibleOrder(order: string[] | undefined): NavId[] {
  const ids = (order ?? []).filter((id): id is NavId => id !== 'tools' && navById.has(id as NavId));
  return ids.length ? ids : navigation.map((item) => item.id);
}

export function mergeOrder(server: string[] | undefined, next: NavId[]): string[] {
  if (!server) return [...next];
  const queue = [...next];
  return server.map((id) => (id === 'tools' ? id : (queue.shift() ?? id)));
}
