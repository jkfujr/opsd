import { useMutation } from '@tanstack/react-query';
import { createBrowserRouter, Navigate, NavLink, Outlet } from 'react-router';
import { RouterProvider } from 'react-router/dom';
import { Button } from '@appica/ui-react/button';
import { api, unwrap } from './api/client';
import { AuthenticatedEntry, SessionBoundary, useSession } from './auth';
import { queryClient } from './state';
import { Failure } from './ui';
import { Home, Nodes } from './pages/Nodes';
import { Account, Appearance, Sessions, Version } from './pages/Settings';
import { Tokens } from './pages/Tokens';

function Layout() {
  const session = useSession();
  const logout = useMutation({ mutationFn: async () => unwrap(await api.DELETE('/auth/session', { params: { header: { 'X-CSRF-Token': session.csrf_token } } })), onSuccess: () => { queryClient.clear(); window.location.assign(session.login_address); } });
  return <div className="min-h-screen bg-background text-foreground">
    <header className="border-b border-border">
      <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-x-8 gap-y-4 px-5 py-4 sm:px-8">
        <NavLink to="/" className="mr-2 text-xl font-semibold tracking-tight text-foreground-intense">OPSD</NavLink>
        <nav aria-label="主导航" className="flex gap-6"><NavLink to="/" end className="nav-link">首页</NavLink><NavLink to="/settings" className="nav-link">设置</NavLink></nav>
        <div className="ml-auto flex items-center gap-4"><span className="max-w-32 truncate text-sm text-foreground-muted">{session.session.username}</span><Button variant="ghost" size="sm" disabled={logout.isPending} onClick={() => logout.mutate()}>退出</Button></div>
      </div>
    </header>
    <main className="mx-auto max-w-7xl px-5 py-8 sm:px-8 sm:py-12"><Failure error={logout.error} /><Outlet /></main>
  </div>;
}

function SettingsLayout() {
  return <div className="grid gap-8 md:grid-cols-[10rem_minmax(0,1fr)] md:gap-12">
    <nav aria-label="设置导航" className="flex flex-wrap content-start gap-2 md:flex-col">
      {[['account', '账号'], ['sessions', '会话'], ['nodes', '节点'], ['tokens', '接入码'], ['appearance', '外观'], ['version', '版本']].map(([path, label]) => <NavLink key={path} to={`/settings/${path}`} className="settings-link">{label}</NavLink>)}
    </nav>
    <section className="min-w-0"><Outlet /></section>
  </div>;
}

const router = createBrowserRouter([{ Component: SessionBoundary, children: [
  { Component: Layout, children: [
    { index: true, Component: Home },
    { path: 'settings', Component: SettingsLayout, children: [
      { index: true, element: <Navigate to="account" replace /> },
      { path: 'account', Component: Account },
      { path: 'sessions', Component: Sessions },
      { path: 'nodes', Component: Nodes },
      { path: 'tokens', Component: Tokens },
      { path: 'appearance', Component: Appearance },
      { path: 'version', Component: Version },
    ] },
  ] },
  { path: '*', Component: AuthenticatedEntry },
] }]);

export function App() { return <RouterProvider router={router} />; }
