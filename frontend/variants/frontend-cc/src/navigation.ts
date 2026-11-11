import { useQuery } from '@tanstack/react-query';
import { api, unwrap } from './api/client';
import type { components } from './api/schema.gen';
import { useSession } from './auth';

export type NavigationItem = components['schemas']['NavigationItem'];

export const navigation: { id: NavigationItem; path: string; label: string }[] = [
  { id: 'overview', path: '/', label: '概览' },
  { id: 'nodes', path: '/nodes', label: '节点' },
  { id: 'containers', path: '/containers', label: '容器' },
  { id: 'databases', path: '/databases', label: '数据库' },
  { id: 'storage', path: '/storage', label: '存储' },
  { id: 'firewall', path: '/firewall', label: '防火墙' },
  { id: 'tools', path: '/tools', label: '工具' },
  { id: 'settings', path: '/settings', label: '设置' },
];

export const defaultNavigationOrder: NavigationItem[] = navigation.map((item) => item.id);

export function useNavigation() {
  const session = useSession();
  return useQuery({
    queryKey: ['navigation', session.local_node_id],
    queryFn: async ({ signal }) => unwrap(await api.GET('/settings/navigation', { signal })),
  });
}
