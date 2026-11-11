import { useEffect, useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { NavLink, Outlet, useLocation } from 'react-router';
import { api, message, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { navigation, navById, visibleOrder } from '../lib/nav';
import { Icon } from './Icon';
import { Button, Clock, Led, useSession } from './ui';
import { OperationsButton, useOperations } from './Operations';

function Rail({ onNavigate }: { onNavigate?: () => void }) {
  const session = useSession();
  const location = useLocation();
  const preferences = useQuery({
    queryKey: ['navigation', session.local_node_id],
    queryFn: async ({ signal }) => value(await api.GET('/settings/navigation', { signal })),
    staleTime: 60_000,
  });
  const order = visibleOrder(preferences.data?.order);
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const me = nodes.data?.items.find((node) => node.id === session.local_node_id);
  const operations = useOperations();
  const pending = useQuery({
    queryKey: ['operations', 'pending'],
    queryFn: async ({ signal }) => value(await api.GET('/operations/pending', { signal })),
  });

  return (
    <>
      <div className="rail__mark">
        <strong>OPSD</strong>
        <span>调度台</span>
      </div>
      <nav className="rail__nav" aria-label="主导航">
        {order.map((id) => {
          const item = navById.get(id);
          if (!item) return null;
          const active = item.path === '/' ? location.pathname === '/' : location.pathname.startsWith(item.path);
          return (
            <NavLink
              key={id}
              to={item.path}
              end={item.path === '/'}
              className="rail__item"
              aria-current={active ? 'page' : undefined}
              onClick={onNavigate}
            >
              <Icon name={item.icon} />
              <span>{item.label}</span>
            </NavLink>
          );
        })}
      </nav>
      <div className="rail__foot">
        <div className="rail__node">
          <span className="kicker" style={{ color: 'var(--rail-mute)' }}>
            本机
          </span>
          <b title={me?.name ?? session.local_node_id}>{me?.name ?? session.local_node_id}</b>
          <span className="mono" style={{ fontSize: 10, letterSpacing: '0.06em' }}>
            {me?.region || '未设地域'} · {me?.connected ? '在线' : '离线'}
          </span>
        </div>
        <button type="button" className="rail__ghost" onClick={() => operations.open()} style={{ justifyContent: 'space-between' }}>
          <span>未决单据</span>
          <span className="mono" style={{ color: pending.data?.items.length ? 'var(--rail-text)' : 'var(--rail-mute)' }}>
            {String(pending.data?.items.length ?? 0).padStart(2, '0')}
          </span>
        </button>
        <div className="rail__actions">
          <ThemeToggle />
        </div>
      </div>
    </>
  );
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<'light' | 'dark' | 'system'>(() => (localStorage.getItem('opsd.theme') as 'light' | 'dark' | null) ?? 'system');
  useEffect(() => {
    const apply = () => {
      const resolved = theme === 'system' ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light') : theme;
      document.documentElement.dataset.theme = resolved;
    };
    apply();
    if (theme === 'system') {
      const media = window.matchMedia('(prefers-color-scheme: dark)');
      media.addEventListener('change', apply);
      return () => media.removeEventListener('change', apply);
    }
    return undefined;
  }, [theme]);
  const next = theme === 'dark' ? 'light' : theme === 'light' ? 'system' : 'dark';
  const label = theme === 'dark' ? '深色' : theme === 'light' ? '浅色' : '跟随系统';
  return (
    <button
      type="button"
      className="rail__ghost"
      onClick={() => {
        if (next === 'system') localStorage.removeItem('opsd.theme');
        else localStorage.setItem('opsd.theme', next);
        setTheme(next);
      }}
      aria-label={`外观：${label}，点击切换`}
    >
      <Icon name={theme === 'dark' ? 'moon' : 'sun'} width={13} height={13} />
      <span className="label">{label}</span>
    </button>
  );
}

function Topbar({ onMenu }: { onMenu: () => void }) {
  const session = useSession();
  const operations = useOperations();
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const local = nodes.data?.items.find((node) => node.id === session.local_node_id);
  const logout = useMutation({
    mutationFn: async () => value(await api.DELETE('/auth/session', { params: { header: { 'X-CSRF-Token': session.csrf_token } } })),
    onSuccess: () => {
      queryClient.clear();
      window.location.assign(session.login_address);
    },
  });
  return (
    <div className="topbar">
      <Button variant="ghost" size="sm" className="rail-toggle btn--icon" aria-label="打开菜单" onClick={onMenu}>
        <Icon name="menu" />
      </Button>
      <div className="topbar__where">
        <Led state={local?.connected ? 'on' : 'off'} />
        <b>{local?.name ?? session.local_node_id}</b>
        <span>·</span>
        <span>{local?.region || '未设地域'}</span>
        <span>·</span>
        <span>{session.session.username}</span>
      </div>
      <div className="topbar__right">
        <Clock />
        <OperationsButton />
        <ThemeToggle />
        <Button variant="ghost" size="sm" busy={logout.isPending} onClick={() => logout.mutate()}>
          退出
        </Button>
      </div>
      {operations.error ? <span className="sr-only" role="alert">{message(operations.error)}</span> : null}
    </div>
  );
}

export function Layout({ children }: { children?: ReactNode }) {
  const [menu, setMenu] = useState(false);
  const location = useLocation();
  useEffect(() => setMenu(false), [location.pathname]);
  return (
    <div className="shell" data-menu={menu ? 'open' : 'closed'}>
      <aside className="rail">
        <Rail onNavigate={() => setMenu(false)} />
      </aside>
      {menu ? <div className="rail-scrim" onClick={() => setMenu(false)} /> : null}
      <div className="main">
        <Topbar onMenu={() => setMenu(true)} />
        {children ?? <Outlet />}
      </div>
    </div>
  );
}

export type CurrentSession = Schema['CurrentSession'];
export { navigation };


