import { useEffect, useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import {
  createBrowserRouter,
  Navigate,
  NavLink,
  Outlet,
  useLocation,
} from 'react-router';
import { RouterProvider } from 'react-router/dom';
import { api, unwrap } from './api/client';
import { AuthenticatedEntry, SessionBoundary, useSession } from './auth';
import { queryClient } from './state';
import {
  BusyButton,
  Button,
  Drawer,
  DrawerBody,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  Failure,
  UnsavedChangesProvider,
} from './ui';
import { Nodes, nodesQuery } from './pages/Nodes';
import { Overview, NodeDetail } from './pages/Overview';
import { Account, Appearance, Sessions, Version } from './pages/Settings';
import { Firewall } from './pages/Firewall';
import { Containers } from './pages/Containers';
import { APISettings } from './pages/APISettings';
import { Databases, DatabaseDetail } from './pages/Databases';
import { Storage } from './pages/Storage';
import { Tools } from './pages/Tools';
import { navigation, useNavigation } from './navigation';
import { FirewallSettings } from './network';
import { OperationsProvider, OperationsButton } from './operations';

function Layout() {
  const location = useLocation();
  const scope = `${location.pathname}/${
    new URLSearchParams(location.search).get('node') ?? ''
  }`;
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
  const paths: Record<string, ReactNode> = {
    overview: (
      <>
        <rect x="3" y="3" width="7" height="7" />
        <rect x="14" y="3" width="7" height="7" />
        <rect x="3" y="14" width="7" height="7" />
        <rect x="14" y="14" width="7" height="7" />
      </>
    ),
    nodes: (
      <>
        <rect x="3" y="4" width="18" height="6" />
        <rect x="3" y="14" width="18" height="6" />
        <path d="M7 7h.01M7 17h.01M12 7h5M12 17h5" />
      </>
    ),
    firewall: (
      <>
        <path d="M12 3 4 6v5c0 5 8 10 8 10s8-5 8-10V6z" />
        <path d="M9 12h6M12 9v6" />
      </>
    ),
    containers: (
      <>
        <path d="m12 3 9 5v8l-9 5-9-5V8zM3 8l9 5 9-5M12 13v8M7.5 5.5l9 5" />
      </>
    ),
    databases: (
      <>
        <ellipse cx="12" cy="5" rx="8" ry="3" />
        <path d="M4 5v14c0 4 16 4 16 0V5M4 12c0 4 16 4 16 0" />
      </>
    ),
    storage: (
      <>
        <rect x="3" y="4" width="18" height="7" />
        <rect x="3" y="13" width="18" height="7" />
        <path d="M7 7.5h.01M7 16.5h.01M12 7.5h5M12 16.5h5" />
      </>
    ),
    tools: (
      <>
        <rect x="3" y="4" width="18" height="16" />
        <path d="m7 9 3 3-3 3M12 15h5" />
      </>
    ),
    settings: (
      <>
        <path d="M4 7h16M4 17h16" />
        <circle cx="9" cy="7" r="2.5" />
        <circle cx="15" cy="17" r="2.5" />
      </>
    ),
  };

  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      className="size-4 shrink-0"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="square"
      strokeLinejoin="miter"
    >
      {paths[name] ?? paths.overview}
    </svg>
  );
}

function Sidebar({
  close,
  brand = true,
}: {
  close?: () => void;
  brand?: boolean;
}) {
  const { pathname } = useLocation();
  const session = useSession();
  const preferences = useNavigation();
  const nodes = useQuery(nodesQuery);
  const localNode = nodes.data?.items.find((n) => n.id === session.local_node_id);
  const onlineCount = nodes.data?.items.filter((n) => n.connected).length;
  const totalCount = nodes.data?.items.length;

  const orderedIds = preferences.data?.order ?? navigation.map((item) => item.id);
  const seen = new Set<string>();
  const items = [
    ...orderedIds.flatMap((id) => {
      const found = navigation.find((item) => item.id === id);
      if (found && !seen.has(found.id)) {
        seen.add(found.id);
        return [found];
      }
      return [];
    }),
    ...navigation.filter((item) => !seen.has(item.id)),
  ];

  return (
    <div className="flex h-full flex-col bg-[var(--rail-bg)] text-[var(--rail-text)]">
      {brand && (
        <NavLink
          to="/"
          onClick={close}
          className="flex h-13 items-center justify-between border-b border-[var(--rail-border)] px-4 text-lg font-extrabold tracking-tight text-white"
        >
          <span>OPSD</span>
          <span className="mono-num bg-[var(--signal-red)] px-1.5 py-0.5 text-[11px] font-bold text-white">
            P2P
          </span>
        </NavLink>
      )}
      <nav
        aria-label="主导航"
        aria-busy={preferences.isPending}
        className="flex-1 space-y-0.5 py-2"
      >
        {items.map(({ path, label, id }) => (
          <div key={path}>
            <NavLink
              to={path}
              end={path === '/'}
              onClick={close}
              className="rail-link sidebar-link"
            >
              <NavIcon name={id} />
              <span>{label}</span>
            </NavLink>
            {path === '/settings' && pathname.startsWith('/settings') && (
              <div className="ml-5 my-1 space-y-0.5 border-l border-[var(--rail-border)] pl-2">
                {settings.map(([key, title]) => (
                  <NavLink
                    key={key}
                    to={`/settings/${key}`}
                    onClick={close}
                    className="settings-sublink"
                  >
                    {title}
                  </NavLink>
                ))}
              </div>
            )}
          </div>
        ))}
      </nav>
      <div className="mono-num border-t border-[var(--rail-border)] px-4 py-3 text-[11px] leading-relaxed text-[var(--rail-muted)]">
        <div className="truncate font-semibold text-[var(--rail-text)]">
          {localNode?.name ?? session.local_node_id}
        </div>
        <div className="mt-0.5 flex items-center justify-between">
          <span>NOISE MESH</span>
          <span className="text-[var(--signal-emerald)]">
            {onlineCount !== undefined && totalCount !== undefined
              ? `${onlineCount}/${totalCount}`
              : '—'}
          </span>
        </div>
      </div>
    </div>
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
        ? settings.find(([path]) => location.pathname === `/settings/${path}`)?.[1] ??
          '设置'
        : location.pathname.startsWith('/databases/')
          ? '数据库详情'
          : location.pathname.startsWith('/nodes/')
            ? '节点详情'
            : navigation.find(({ path }) => path === location.pathname)?.label ??
              '设置';

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
    <div className="min-h-screen bg-[var(--bg-canvas)] text-[var(--ink-body)]">
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-52 overflow-y-auto border-r border-[var(--grid-strong)] bg-[var(--rail-bg)] lg:block">
        <Sidebar />
      </aside>
      <Drawer side="left" open={menu} onOpenChange={setMenu}>
        <DrawerContent
          closeLabel="关闭菜单"
          className="h-full w-64 bg-[var(--rail-bg)] text-[var(--rail-text)]"
        >
          <DrawerHeader>
            <DrawerTitle>OPSD</DrawerTitle>
          </DrawerHeader>
          <DrawerBody className="flex-1 p-0">
            <Sidebar brand={false} close={() => setMenu(false)} />
          </DrawerBody>
        </DrawerContent>
      </Drawer>
      <div className="lg:pl-52">
        <header className="sticky top-0 z-20 flex h-13 items-center gap-3 border-b border-[var(--grid-strong)] bg-[var(--bg-elevated)] px-4 sm:px-6">
          <Button
            variant="ghost"
            size="sm"
            aria-label="打开菜单"
            aria-expanded={menu}
            onClick={() => setMenu(true)}
            className="lg:hidden"
          >
            <svg
              aria-hidden="true"
              viewBox="0 0 24 24"
              className="size-5"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
            >
              <path d="M4 6h16M4 12h16M4 18h16" />
            </svg>
          </Button>
          <span className="text-base font-extrabold tracking-tight text-[var(--ink-strong)]">
            {title}
          </span>
          <div className="ml-auto flex items-center gap-2 sm:gap-3">
            <OperationsButton />
            <span className="mono-num hidden max-w-40 truncate text-xs font-semibold text-[var(--ink-muted)] sm:block">
              {session.session.username}
            </span>
            <BusyButton
              variant="ghost"
              size="sm"
              busy={logout.isPending}
              onClick={() => logout.mutate()}
            >
              退出
            </BusyButton>
          </div>
        </header>
        <main className="min-w-0 p-4 sm:p-6">
          <Failure error={logout.error} />
          <Outlet />
        </main>
      </div>
    </div>
  );
}

function SettingsLayout() {
  return (
    <section className="swiss-panel">
      <Outlet />
    </section>
  );
}

export const routes = [
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
          { path: 'tools', Component: Tools },
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
];

const router = createBrowserRouter(routes);

export function App() {
  return <RouterProvider router={router} />;
}
