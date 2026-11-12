import { useEffect, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { createBrowserRouter, Link, NavLink, Navigate, Outlet, useLocation, type RouteObject } from 'react-router';
import { RouterProvider } from 'react-router/dom';
import { useTheme } from '@appica/ui-react/hooks/use-theme';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@appica/ui-react/dropdown-menu';
import { Activity, CircleCheck, LogOut, Menu, Moon, PanelLeftClose, PanelLeftOpen, Settings2, ShieldCheck, Sun, UserRound, X } from 'lucide-react';
import { api, unwrap } from './api/client';
import { finishSession, SessionBoundary, useSession } from './auth';
import { UIProvider, useUI } from './store';
import { navigation, settingsNavigation, toolsNavigation, useNavigation } from './navigation';
import { SessionNavigation, TerminalProvider, TerminalWorkspace } from './terminal';
import { OperationsProvider, OperationsButton } from './operations';
import { ActionLink, Empty, Heading, IconButton, Panel, Sheet } from './ui';
import { Failure, UnsavedChangesProvider } from './business-ui';
import { Overview } from './pages/Overview';
import { NodeDetail, Nodes } from './pages/Nodes';
import { Containers } from './pages/Containers';
import { Tasks } from './pages/Tasks';
import { Account, Appearance, Sessions, Version } from './pages/Settings';
import { APISettings } from './pages/APISettings';
import { Databases, DatabaseDetail } from './pages/Databases';
import { Firewall } from './pages/Firewall';
import { FirewallSettings } from './network';
import { ToolsIndex } from './logs';
import { builtinBase, inBuiltinConsole } from './location';
function Layout() { return <UIProvider><UnsavedChangesProvider><OperationsProvider><TerminalProvider><Shell/></TerminalProvider></OperationsProvider></UnsavedChangesProvider></UIProvider>; }
function Shell() {
  const { pathname, search } = useLocation(), session = useSession(), preferences = useNavigation(), ui = useUI(), { resolvedTheme, setTheme } = useTheme();
  const [collapsed, setCollapsed] = useState(false), [mobileOpen, setMobileOpen] = useState(false);
  const isLog = new URLSearchParams(search).get('view') === 'logs' && ['/tools', '/containers'].includes(pathname);
  const isTerminal = pathname === '/tools/terminal', title = navigation.find(item => item.path !== '/' && pathname.startsWith(item.path))?.label ?? '概览';
  const items = preferences.data?.order.flatMap(id => navigation.filter(item => item.id === id)) ?? navigation;
  useEffect(() => { setMobileOpen(false); document.title = `${title} · OPSD`; if (!isTerminal) window.scrollTo({ top: 0 }); }, [pathname, title, isTerminal]);
  const logout = useMutation({ retry: false, mutationFn: async () => unwrap(await api.DELETE('/auth/sessions/current')), onSuccess: () => finishSession(session.login_address), onError: () => { void api.GET('/auth/sessions/current'); } });
  const nav = <><div className="brand-area"><Link className="brand" to="/" aria-label="OPSD 概览"><span className="brand-mark"><Activity size={23} aria-hidden/></span><span className="brand-word">OPSD</span></Link></div><div className="sidebar-content"><nav className="nav-list" aria-label="主导航">{items.map(({ id, path, label, Icon }) => <div key={id} className={((id === "settings" && pathname.startsWith("/settings")) || (id === "tools" && pathname.startsWith("/tools"))) ? "nav-group category-active" : "nav-group"}><NavLink to={path} end={path === '/'} className={`nav-link ${id === "settings" || id === "tools" ? "nav-category" : ""}`} aria-current={id === "settings" || id === "tools" ? false : undefined} aria-label={label} title={collapsed ? label : undefined} onClick={() => setMobileOpen(false)}><Icon size={20} aria-hidden/><span>{label}</span></NavLink>{((id === 'settings' && pathname.startsWith('/settings')) || (id === 'tools' && pathname.startsWith('/tools'))) && <nav className="subnav" aria-label={`${label}分类`}>{(id === 'settings' ? settingsNavigation : toolsNavigation).map(([key, text]) => <NavLink key={key} to={`${path}/${key}`} onClick={() => setMobileOpen(false)}>{text}</NavLink>)}</nav>}</div>)}</nav><SessionNavigation onNavigate={() => setMobileOpen(false)}/></div></>;
  return <div className={`shell ${collapsed ? 'sidebar-collapsed' : ''} ${isTerminal ? 'terminal-route' : isLog ? 'log-route' : ''}`}><a className="skip-link" href={window.location.pathname + window.location.search + '#workspace'}>跳到主要内容</a><aside className="sidebar">{nav}</aside><Sheet open={mobileOpen} onClose={() => setMobileOpen(false)} title="OPSD"><div className="mobile-navigation">{nav}</div></Sheet><div className="shell-body"><header className="topbar"><div className="topbar-leading"><IconButton className="desktop-menu" icon={collapsed ? PanelLeftOpen : PanelLeftClose} label={collapsed ? '展开侧栏' : '收起侧栏'} onClick={() => setCollapsed(value => !value)}/><IconButton className="mobile-menu" icon={Menu} label="打开导航" onClick={() => setMobileOpen(true)}/></div><div className="topbar-actions"><IconButton icon={resolvedTheme === 'dark' ? Sun : Moon} label={resolvedTheme === 'dark' ? '切换浅色模式' : '切换深色模式'} onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}/><OperationsButton/><DropdownMenu><DropdownMenuTrigger className="account-trigger" aria-label={`账号菜单 ${session.session.username}`}><UserRound size={20} aria-hidden/><span>{session.session.username}</span></DropdownMenuTrigger><DropdownMenuContent className="action-menu account-menu" align="end"><DropdownMenuItem render={<Link to="/settings/account"/>}><Settings2 size={18} aria-hidden/>账号设置</DropdownMenuItem><DropdownMenuItem render={<Link to="/settings/sessions"/>}><ShieldCheck size={18} aria-hidden/>登录会话</DropdownMenuItem><DropdownMenuItem render={<a href={builtinBase + '/settings/appearance'}/>}><Activity size={18} aria-hidden/>内置界面</DropdownMenuItem><DropdownMenuSeparator/><DropdownMenuItem disabled={logout.isPending} onClick={() => logout.mutate()}><LogOut size={18} aria-hidden/>退出</DropdownMenuItem></DropdownMenuContent></DropdownMenu></div></header><main id="workspace" tabIndex={-1} className={isTerminal ? 'workspace terminal-main' : isLog ? 'workspace log-main' : 'workspace'}><Failure error={logout.error}/><div hidden={isTerminal} className="page-content"><Outlet/></div><TerminalWorkspace active={isTerminal}/></main></div>{ui.toast && <div className="toast" role="status"><CircleCheck size={18} aria-hidden/>{ui.toast}<IconButton icon={X} label="关闭提示" onClick={() => ui.setToast('')}/></div>}</div>;
}
function SettingsLayout() { return <Outlet/>; }
function Storage() { return <div className="page-stack"><Heading title="存储"/><Panel><Empty title="未接入"/></Panel></div>; }
function Missing() { return <div className="page-stack"><Heading title="页面不存在"/><Panel><Empty title="未找到这个页面" action={<ActionLink to="/">返回概览</ActionLink>}/></Panel></div>; }
function TerminalAlias() { const { search, hash } = useLocation(); return <Navigate to={`/tools/terminal${search}${hash}`} replace/>; }
const settingsPages = { account: Account, sessions: Sessions, api: APISettings, appearance: Appearance, version: Version };
const primaryPages = { overview: Overview, nodes: Nodes, containers: Containers, databases: Databases, storage: Storage, firewall: Firewall };
const routes: RouteObject[] = navigation.flatMap<RouteObject>(item => {
  if (item.id === 'settings') return [{ path: 'settings', Component: SettingsLayout, children: [{ index: true, element: <Navigate to="account" replace/> }, ...settingsNavigation.map(([path]) => ({ path, Component: settingsPages[path] }))] }];
  if (item.id === 'tools') return [{ path: 'tools', children: [{ index: true, Component: ToolsIndex }, ...toolsNavigation.map(([path]) => ({ path, Component: path === 'tasks' ? Tasks : () => null }))] }];
  return item.id === 'overview' ? [{ index: true, Component: Overview }] : [{ path: item.path.slice(1), Component: primaryPages[item.id] }];
});
const router = createBrowserRouter([{ Component: SessionBoundary, children: [{ Component: Layout, children: [...routes, { path: 'nodes/:id', Component: NodeDetail }, { path: 'databases/:id', Component: DatabaseDetail }, { path: 'firewall/settings', Component: FirewallSettings }, { path: 'terminal', Component: TerminalAlias }, { path: '*', Component: Missing }] }] }], { basename: inBuiltinConsole ? builtinBase : '/' });
export function App() { return <RouterProvider router={router}/>; }
