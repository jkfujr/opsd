import { useQuery } from '@tanstack/react-query';
import { LayoutDashboard, Server, Box, Database, HardDrive, Shield, Wrench, Settings2 } from 'lucide-react';
import { api, unwrap } from './api/client';
import type { components } from './api/types';
import { useSession } from './auth';
export const navigation = [
  { id: 'overview', path: '/', label: '概览', Icon: LayoutDashboard },
  { id: 'nodes', path: '/nodes', label: '节点', Icon: Server },
  { id: 'containers', path: '/containers', label: '容器', Icon: Box },
  { id: 'databases', path: '/databases', label: '数据库', Icon: Database },
  { id: 'storage', path: '/storage', label: '存储', Icon: HardDrive },
  { id: 'firewall', path: '/firewall', label: '防火墙', Icon: Shield },
  { id: 'tools', path: '/tools', label: '工具', Icon: Wrench },
  { id: 'settings', path: '/settings', label: '设置', Icon: Settings2 },
] satisfies { id: components['schemas']['NavigationItem']; path: string; label: string; Icon: typeof Server }[];
export const defaultNavigationOrder = navigation.map(item => item.id);
export const settingsNavigation = [['account', '账号'], ['sessions', '登录会话'], ['api', 'API'], ['appearance', '外观'], ['version', '版本']] as const;
export const toolsNavigation = [['terminal', '终端'], ['tasks', '定时任务']] as const;
export function useNavigation() { const session = useSession(); return useQuery({ queryKey: ['navigation', session.local_node_id], queryFn: async ({ signal }) => unwrap(await api.GET('/settings/navigation', { signal })) }); }
