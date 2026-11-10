import { useEffect, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { createBrowserRouter, Navigate, NavLink, Outlet, useLocation } from 'react-router';
import { RouterProvider } from 'react-router/dom';
import { Button } from '@appica/ui-react/button';
import { api, unwrap } from './api/client';
import { AuthenticatedEntry, SessionBoundary, useSession } from './auth';
import { queryClient } from './state';
import { BusyButton, Failure, UnsavedChangesProvider } from './ui';
import { Nodes } from './pages/Nodes';
import { Overview, NodeDetail } from './pages/Overview';
import { Drawer, DrawerContent, DrawerHeader, DrawerBody, DrawerTitle } from '@appica/ui-react/drawer';
import { Account, Appearance, Sessions, Version } from './pages/Settings';
import { Firewall } from './pages/Firewall';
import { Containers } from './pages/Containers';
import { APISettings } from './pages/APISettings';
import { Databases, DatabaseDetail } from './pages/Databases';
import { Storage } from './pages/Storage';
import { navigation, useNavigation } from './navigation';
import { FirewallSettings } from './network';
import { OperationsProvider, OperationsButton } from './operations';

function Layout() {
  const location = useLocation();
  const scope = `${location.pathname}/${new URLSearchParams(location.search).get('node') ?? ''}`;
  return <UnsavedChangesProvider><OperationsProvider key={scope}><LayoutContent /></OperationsProvider></UnsavedChangesProvider>;
}

const settings = [['account', '账号'], ['sessions', '会话'], ['api', 'API'], ['appearance', '外观'], ['version', '版本']];
function NavIcon({ name }: { name: string }) {
  const paths: Record<string, React.ReactNode> = {
    overview: <><rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" /><rect x="3" y="14" width="7" height="7" rx="1" /><rect x="14" y="14" width="7" height="7" rx="1" /></>,
    nodes: <><rect x="3" y="3" width="18" height="7" rx="2" /><rect x="3" y="14" width="18" height="7" rx="2" /><path d="M7 6.5h.01M7 17.5h.01M12 6.5h5M12 17.5h5" /></>,
    firewall: <><path d="M12 3 4 6v5c0 5 8 10 8 10s8-5 8-10V6z" /><path d="M9 12h6M12 9v6" /></>,
    containers: <><path d="m12 3 9 5v8l-9 5-9-5V8zM3 8l9 5 9-5M12 13v8M7.5 5.5l9 5" /></>,
    databases: <><ellipse cx="12" cy="5" rx="8" ry="3" /><path d="M4 5v14c0 4 16 4 16 0V5M4 12c0 4 16 4 16 0" /></>,
    storage: <><rect x="3" y="4" width="18" height="7" rx="2" /><rect x="3" y="13" width="18" height="7" rx="2" /><path d="M7 7.5h.01M7 16.5h.01M12 7.5h5M12 16.5h5" /></>,
    settings: <><path d="M4 7h16M4 17h16" /><circle cx="9" cy="7" r="3" /><circle cx="15" cy="17" r="3" /></>,
  };
  return <svg aria-hidden="true" viewBox="0 0 24 24" className="size-4.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">{paths[name]}</svg>;
}
function Sidebar({ close, brand = true }: { close?: () => void; brand?: boolean }) {
  const { pathname } = useLocation();
  const preferences = useNavigation();
  const items = preferences.data?.order.flatMap((id) => navigation.filter((item) => item.id === id)) ?? navigation;
  return <>{brand && <NavLink to="/" onClick={close} className="flex h-14 items-center border-b border-border px-6 text-xl font-semibold tracking-tight text-foreground-intense">OPSD</NavLink>}
    <nav aria-label="主导航" aria-busy={preferences.isPending} className="space-y-1 p-3">{items.map(({ path, label, id }) => <div key={path}>
      <NavLink to={path} end={path === '/'} onClick={close} className="sidebar-link"><NavIcon name={id} /><span>{label}</span></NavLink>
      {path === '/settings' && pathname.startsWith('/settings') && <div className="ml-5 mt-1 space-y-1 border-l border-border pl-3">{settings.map(([key, title]) => <NavLink key={key} to={`/settings/${key}`} onClick={close} className="settings-link block">{title}</NavLink>)}</div>}
    </div>)}</nav></>;
}
function LayoutContent() {
  const session = useSession();
  const location = useLocation();
  const [menu, setMenu] = useState(false);
  useEffect(() => setMenu(false), [location.pathname]);
  const title = location.pathname === '/firewall/settings' ? '防火墙设置' : location.pathname.startsWith('/settings/') ? settings.find(([path]) => location.pathname === `/settings/${path}`)?.[1] ?? '设置' : location.pathname.startsWith('/databases/') ? '数据库详情' : location.pathname.startsWith('/nodes/') ? '节点详情' : navigation.find(({ path }) => path === location.pathname)?.label ?? '设置';
  const logout = useMutation({ mutationFn: async () => unwrap(await api.DELETE('/auth/session', { params: { header: { 'X-CSRF-Token': session.csrf_token } } })), onSuccess: () => { queryClient.clear(); window.location.assign(session.login_address); } });
  return <div className="min-h-screen bg-background-muted text-foreground">
    <aside className="fixed inset-y-0 left-0 z-30 hidden w-56 overflow-y-auto border-r border-border bg-background lg:block"><Sidebar /></aside>
    <Drawer side="left" open={menu} onOpenChange={setMenu}><DrawerContent frame={false} closeLabel="关闭菜单" className="h-full w-64"><DrawerHeader><DrawerTitle>OPSD</DrawerTitle></DrawerHeader><DrawerBody className="px-0"><Sidebar brand={false} close={() => setMenu(false)} /></DrawerBody></DrawerContent></Drawer>
    <div className="lg:pl-56"><header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b border-border bg-background px-4 sm:px-6">
      <Button variant="ghost" size="sm" aria-label="打开菜单" aria-expanded={menu} onClick={() => setMenu(true)} className="lg:hidden"><svg aria-hidden="true" viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth="1.7"><path d="M4 6h16M4 12h16M4 18h16" /></svg></Button>
      <span className="font-semibold text-foreground-intense">{title}</span>
      <div className="ml-auto flex items-center gap-2 sm:gap-4"><OperationsButton /><span className="hidden max-w-40 truncate text-sm text-foreground-muted sm:block">{session.session.username}</span><BusyButton variant="ghost" size="sm" busy={logout.isPending} onClick={() => logout.mutate()}>退出</BusyButton></div>
    </header><main className="min-w-0 p-4 sm:p-6"><Failure error={logout.error} /><Outlet /></main></div>
  </div>;
}
function SettingsLayout() { return <section className="panel"><Outlet /></section>; }

const router = createBrowserRouter([{ Component: SessionBoundary, children: [
  { Component: Layout, children: [
    { index: true, Component: Overview },
    { path: 'nodes', Component: Nodes },
    { path: 'nodes/:id', Component: NodeDetail },
    { path: 'firewall', Component: Firewall },
    { path: 'containers', Component: Containers },
    { path: 'databases', Component: Databases },
    { path: 'databases/:id', Component: DatabaseDetail },
    { path: 'storage', Component: Storage },
    { path: 'firewall/settings', Component: FirewallSettings },
    { path: 'settings', Component: SettingsLayout, children: [
      { index: true, element: <Navigate to="account" replace /> },
      { path: 'account', Component: Account },
      { path: 'sessions', Component: Sessions },
      { path: 'api', Component: APISettings },
      { path: 'nodes', element: <Navigate to="/nodes" replace /> },
      { path: 'appearance', Component: Appearance },
      { path: 'version', Component: Version },
    ] },
  ] },
  { path: '*', Component: AuthenticatedEntry },
] }]);

export function App() { return <RouterProvider router={router} />; }
