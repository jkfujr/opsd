import { Activity, Boxes, CalendarClock, Database, HardDrive, LayoutGrid, ListChecks, Server, Settings, ShieldCheck, SquareTerminal, type LucideIcon } from 'lucide-react';

export interface NavItem { path: string; label: string; icon: LucideIcon; hint?: string; keywords?: string }
export interface NavGroup { label: string; items: NavItem[] }

export const navGroups: NavGroup[] = [
  { label: '运行', items: [
    { path: '/', label: '概览', icon: LayoutGrid, hint: 'G O', keywords: 'overview dashboard 首页 集群' },
    { path: '/nodes', label: '节点', icon: Server, hint: 'G N', keywords: 'nodes server 服务器 成员' },
    { path: '/monitoring', label: '监控', icon: Activity, hint: 'G M', keywords: 'monitoring metrics 指标 历史' },
  ] },
  { label: '资源', items: [
    { path: '/containers', label: '容器', icon: Boxes, hint: 'G C', keywords: 'docker containers compose 镜像' },
    { path: '/databases', label: '数据库', icon: Database, hint: 'G D', keywords: 'database galera proxysql mariadb mysql' },
    { path: '/storage', label: '存储', icon: HardDrive, keywords: 's3 storage 对象存储 归档' },
  ] },
  { label: '安全', items: [
    { path: '/firewall', label: '防火墙', icon: ShieldCheck, hint: 'G F', keywords: 'firewall ufw nftables firewalld 互信 规则' },
  ] },
  { label: '工具', items: [
    { path: '/tools/terminal', label: '终端', icon: SquareTerminal, hint: 'G T', keywords: 'terminal shell ssh root' },
    { path: '/tools/tasks', label: '定时任务', icon: CalendarClock, keywords: 'cron tasks schedule 计划' },
    { path: '/tools/operations', label: '操作记录', icon: ListChecks, keywords: 'operations history 审计 记录' },
  ] },
];
export const settingsItem: NavItem = { path: '/settings', label: '设置', icon: Settings, hint: 'G S', keywords: 'settings account api 账号 令牌 外观' };
export const allNav = [...navGroups.flatMap((g) => g.items), settingsItem];

export function titleFor(pathname: string) {
  const match = allNav.filter((n) => n.path !== '/' && (pathname === n.path || pathname.startsWith(n.path + '/'))).sort((a, b) => b.path.length - a.path.length)[0];
  return match ?? allNav[0]!;
}
