import { useEffect, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { createBrowserRouter, Navigate, NavLink, Outlet, useLocation } from 'react-router';
import { RouterProvider } from 'react-router/dom';
import { Button } from '@appica/ui-react/button';
import { useTheme } from '@appica/ui-react/hooks/use-theme';
import { Drawer, DrawerContent, DrawerHeader, DrawerBody, DrawerTitle } from '@appica/ui-react/drawer';
import { api, unwrap } from './api/client';
import { AuthenticatedEntry, SessionBoundary, useSession } from './auth';
import { queryClient } from './state';
import { BusyButton, Failure, UnsavedChangesProvider } from './ui';
import { Nodes } from './pages/Nodes';
import { Overview, NodeDetail } from './pages/Overview';
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
  return (
    <UnsavedChangesProvider>
      <OperationsProvider key={scope}>
        <LayoutContent />
      </OperationsProvider>
    </UnsavedChangesProvider>
  );
}

const settings = [
  ['account', '账号'],
  ['sessions', '会话'],
  ['api', 'API'],
  ['appearance', '外观'],
  ['version', '版本'],
] as const;

function NavIcon({ name }: { name: string }) {
  const paths: Record<string, React.ReactNode> = {
    overview: (
      <>
        <rect x="3" y="3" width="7.5" height="7.5" rx="1.75" />
        <rect x="13.5" y="3" width="7.5" height="7.5" rx="1.75" />
        <rect x="3" y="13.5" width="7.5" height="7.5" rx="1.75" />
        <rect x="13.5" y="13.5" width="7.5" height="7.5" rx="1.75" />
      </>
    ),
    nodes: (
      <>
        <rect x="3" y="4" width="18" height="6.5" rx="2" />
        <rect x="3" y="13.5" width="18" height="6.5" rx="2" />
        <circle cx="7" cy="7.25" r="0.8" fill="currentColor" />
        <circle cx="7" cy="16.75" r="0.8" fill="currentColor" />
        <path d="M11.5 7.25h5.5M11.5 16.75h5.5" />
      </>
    ),
    firewall: (
      <>
        <path d="M12 2.75 4.5 5.75v5.5c0 4.95 7.5 9.75 7.5 9.75s7.5-4.8 7.5-9.75v-5.5z" />
        <path d="M9.25 11.75h5.5M12 9v5.5" />
      </>
    ),
    containers: (
      <>
        <path d="m12 2.75 8.5 4.75v9L12 21.25 3.5 16.5v-9z" />
        <path d="m3.5 7.5 8.5 4.75 8.5-4.75M12 12.25v9M7.75 5.15l8.5 4.75" />
      </>
    ),
    databases: (
      <>
        <ellipse cx="12" cy="5.5" rx="8" ry="3" />
        <path d="M4 5.5v13c0 3.5 16 3.5 16 0v-13M4 12c0 3.5 16 3.5 16 0" />
      </>
    ),
    storage: (
      <>
        <path d="M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2z" />
        <path d="M4 14h16M8 17h.01M12 17h4" />
      </>
    ),
    settings: (
      <>
        <path d="M4 7h16M4 17h16" />
        <circle cx="9" cy="7" r="2.75" />
        <circle cx="15" cy="17" r="2.75" />
      </>
    ),
  };
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      className="size-4.5 shrink-0"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {paths[name]}
    </svg>
  );
}

function Sidebar({ close, brand = true }: { close?: () => void; brand?: boolean }) {
  const { pathname } = useLocation();
  const preferences = useNavigation();
  const items = preferences.data?.order?.flatMap((id) => navigation.filter((item) => item.id === id)) ?? navigation;

  return (
    <div className="flex h-full flex-col">
      {brand && (
        <NavLink
          to="/"
          onClick={close}
          className="flex h-15 shrink-0 items-center gap-3 border-b border-border px-5 font-semibold tracking-tight text-foreground-intense transition-colors hover:text-primary"
        >
          <span className="flex size-7 items-center justify-center rounded-lg bg-primary text-xs font-bold text-primary-foreground shadow-xs">
            OP
          </span>
          <span className="text-base font-bold tracking-tight">OPSD</span>
        </NavLink>
      )}
      <nav aria-label="主导航" aria-busy={preferences.isPending} className="flex-1 space-y-1 p-3">
        {items.map(({ path, label, id }) => {
          const isSettings = path === '/settings';
          const settingsExpanded = isSettings && pathname.startsWith('/settings');
          return (
            <div key={path}>
              <NavLink to={path} end={path === '/'} onClick={close} className="sidebar-link">
                <NavIcon name={id} />
                <span>{label}</span>
              </NavLink>
              {settingsExpanded && (
                <div className="ml-5 mt-1 space-y-0.5 border-l border-border pl-3">
                  {settings.map(([key, title]) => (
                    <NavLink key={key} to={`/settings/${key}`} onClick={close} className="settings-link block">
                      {title}
                    </NavLink>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </nav>
    </div>
  );
}

function ThemeToggle() {
  const { resolvedTheme, setTheme, mounted } = useTheme();
  if (!mounted) return null;
  const isDark = resolvedTheme === 'dark';
  return (
    <Button
      variant="ghost"
      size="sm"
      aria-label={isDark ? '切换浅色模式' : '切换深色模式'}
      onClick={() => setTheme(isDark ? 'light' : 'dark')}
      className="px-2.5 text-foreground-muted hover:text-foreground-intense"
    >
      {isDark ? (
        <svg aria-hidden="true" viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
        </svg>
      ) : (
        <svg aria-hidden="true" viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
        </svg>
      )}
    </Button>
  );
}

function LayoutContent() {
  const session = useSession();
  const location = useLocation();
  const [menu, setMenu] = useState(false);

  useEffect(() => setMenu(false), [location.pathname]);

  const title =
    location.pathname === '/firewall/settings'
      ? '防火墙设置'
      : location.pathname.startsWith('/settings/')
        ? settings.find(([path]) => location.pathname === `/settings/${path}`)?.[1] ?? '设置'
        : location.pathname.startsWith('/databases/')
          ? '数据库详情'
          : location.pathname.startsWith('/nodes/')
            ? '节点详情'
            : navigation.find(({ path }) => path === location.pathname)?.label ?? '设置';

  const logout = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.DELETE('/auth/session', {
          params: { header: { 'X-CSRF-Token': session.csrf_token } },
        }),
      ),
    onSuccess: () => {
      queryClient.clear();
      window.location.assign(session.login_address);
    },
  });

  return (
    <div className="min-h-screen bg-background-muted text-foreground">
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 overflow-y-auto border-r border-border bg-background lg:block">
        <Sidebar />
      </aside>
      <Drawer side="left" open={menu} onOpenChange={setMenu}>
        <DrawerContent frame={false} closeLabel="关闭菜单" className="h-full w-64">
          <DrawerHeader>
            <DrawerTitle className="flex items-center gap-2.5">
              <span className="flex size-6 items-center justify-center rounded-md bg-primary text-xs font-bold text-primary-foreground">
                OP
              </span>
              <span>OPSD</span>
            </DrawerTitle>
          </DrawerHeader>
          <DrawerBody className="px-0">
            <Sidebar brand={false} close={() => setMenu(false)} />
          </DrawerBody>
        </DrawerContent>
      </Drawer>
      <div className="lg:pl-60">
        <header className="sticky top-0 z-20 flex h-15 items-center gap-3 border-b border-border bg-background/90 px-4 backdrop-blur-md sm:px-6">
          <Button
            variant="ghost"
            size="sm"
            aria-label="打开菜单"
            aria-expanded={menu}
            onClick={() => setMenu(true)}
            className="lg:hidden"
          >
            <svg aria-hidden="true" viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth="1.75">
              <path d="M4 6h16M4 12h16M4 18h16" />
            </svg>
          </Button>
          <span className="text-base font-semibold tracking-tight text-foreground-intense">{title}</span>
          <div className="ml-auto flex items-center gap-2 sm:gap-3">
            <OperationsButton />
            <ThemeToggle />
            <div className="hidden h-4 w-px bg-border sm:block" />
            <span className="hidden max-w-40 truncate text-sm font-medium text-foreground-muted sm:block">
              {session.session.username}
            </span>
            <BusyButton variant="ghost" size="sm" busy={logout.isPending} onClick={() => logout.mutate()}>
              退出
            </BusyButton>
          </div>
        </header>
        <main className="mx-auto max-w-[1600px] min-w-0 p-4 sm:p-6">
          <Failure error={logout.error} />
          <Outlet />
        </main>
      </div>
    </div>
  );
}

function SettingsLayout() {
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-background p-1.5 shadow-2xs">
        {settings.map(([key, title]) => (
          <NavLink
            key={key}
            to={`/settings/${key}`}
            className={({ isActive }) =>
              `rounded-lg px-3.5 py-1.5 text-sm font-medium transition-colors ${
                isActive
                  ? 'bg-primary text-primary-foreground shadow-2xs'
                  : 'text-foreground-muted hover:bg-background-subtle hover:text-foreground-intense'
              }`
            }
          >
            {title}
          </NavLink>
        ))}
      </div>
      <section className="panel">
        <Outlet />
      </section>
    </div>
  );
}

const router = createBrowserRouter([
  {
    Component: SessionBoundary,
    children: [
      {
        Component: Layout,
        children: [
          { index: true, Component: Overview },
          { path: 'nodes', Component: Nodes },
          { path: 'nodes/:id', Component: NodeDetail },
          { path: 'firewall', Component: Firewall },
          { path: 'containers', Component: Containers },
          { path: 'databases', Component: Databases },
          { path: 'databases/:id', Component: DatabaseDetail },
          { path: 'storage', Component: Storage },
          { path: 'firewall/settings', Component: FirewallSettings },
          {
            path: 'settings',
            Component: SettingsLayout,
            children: [
              { index: true, element: <Navigate to="account" replace /> },
              { path: 'account', Component: Account },
              { path: 'sessions', Component: Sessions },
              { path: 'api', Component: APISettings },
              { path: 'nodes', element: <Navigate to="/nodes" replace /> },
              { path: 'appearance', Component: Appearance },
              { path: 'version', Component: Version },
            ],
          },
        ],
      },
      { path: '*', Component: AuthenticatedEntry },
    ],
  },
]);

export function App() {
  return <RouterProvider router={router} />;
}
