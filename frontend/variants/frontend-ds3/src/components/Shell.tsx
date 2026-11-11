import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { NavLink, Outlet, useLocation } from 'react-router';
import { api, value } from '../api/client';
import { queryClient } from '../state';
import { navById, visibleOrder } from '../lib/nav';
import { setTheme, setWorld, useTheme, useWorld, worlds } from '../lib/world';
import { Icon } from './Icon';
import { Button, Clock, Led, useSession } from './ui';
import { QueuePane, QueueProvider, useQueue } from './Queue';

function Rail({ onNavigate }: { onNavigate?: () => void }) {
  const session = useSession();
  const location = useLocation();
  const preferences = useQuery({
    queryKey: ['navigation', session.local_node_id],
    queryFn: async ({ signal }) => value(await api.GET('/settings/navigation', { signal })),
    staleTime: 60_000,
  });
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const me = nodes.data?.items.find((node) => node.id === session.local_node_id);
  const order = visibleOrder(preferences.data?.order);

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
            <NavLink key={id} to={item.path} end={item.path === '/'} className="rail__item" aria-current={active ? 'page' : undefined} onClick={onNavigate}>
              <Icon name={item.icon} />
              <span>{item.label}</span>
            </NavLink>
          );
        })}
      </nav>
      <div className="rail__foot">
        <span>本机</span>
        <b title={me?.name ?? session.local_node_id}>{me?.name ?? session.local_node_id}</b>
        <span className="mono" style={{ fontSize: 10, letterSpacing: '.05em' }}>
          {me?.region || '未设地域'} · {me?.connected ? '在线' : '离线'}
        </span>
      </div>
    </>
  );
}

function WorldSwitch({ compact }: { compact?: boolean }) {
  const world = useWorld();
  const theme = useTheme();
  return (
    <div className="topbar__world">
      {compact ? null : (
        <button
          type="button"
          className="btn btn--ghost btn--sm"
          aria-label={`主题：${theme === 'dark' ? '深色' : '浅色'}，点击切换`}
          title={theme === 'dark' ? '切到浅色' : '切到深色'}
          onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
        >
          <Icon name={theme === 'dark' ? 'moon' : 'sun'} />
        </button>
      )}
      <div className="world-switch" role="group" aria-label="视觉世界">
        {worlds.map((item) => (
          <button key={item.id} type="button" aria-pressed={world === item.id} title={item.hint} onClick={() => setWorld(item.id)}>
            {item.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function Topbar({ onMenu, onQueue }: { onMenu: () => void; onQueue: () => void }) {
  const session = useSession();
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const local = nodes.data?.items.find((node) => node.id === session.local_node_id);
  const queue = useQueue();
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
      <WorldSwitch />
      <Button variant="line" size="sm" className="queue-toggle" icon="terminal" onClick={onQueue}>
        <span className="mono">{String(queue.pendingCount).padStart(2, '0')}</span>
      </Button>
      <Clock />
      <Button variant="ghost" size="sm" busy={logout.isPending} onClick={() => logout.mutate()}>
        退出
      </Button>
    </div>
  );
}

export function Layout() {
  const [menu, setMenu] = useState(false);
  const [queue, setQueue] = useState(false);
  const location = useLocation();
  useEffect(() => setMenu(false), [location.pathname]);
  return (
    <div className="shell" data-menu={menu ? 'open' : 'closed'} data-queue={queue ? 'open' : 'closed'}>
      <aside className="rail">
        <Rail onNavigate={() => setMenu(false)} />
      </aside>
      {menu ? <div className="rail-scrim" onClick={() => setMenu(false)} /> : null}
      <div className="main">
        <QueueProvider>
          <Topbar onMenu={() => setMenu(true)} onQueue={() => setQueue(true)} />
          <div className="panes">
            <div className="pane-center">
              <Outlet />
            </div>
            <QueuePane />
          </div>
        </QueueProvider>
      </div>
      {queue ? <div className="queue-scrim" onClick={() => setQueue(false)} /> : null}
    </div>
  );
}

