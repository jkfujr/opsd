import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router';
import { api, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { navigation, navById, visibleOrder } from '../lib/nav';
import { Icon, type IconName } from './Icon';
import { Button, Clock, Led, useSession } from './ui';
import { OperationsButton, useOperations } from './Operations';

type Cmd = { id: string; label: string; hint?: string; icon: IconName; run: () => void; group: string };

function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const operations = useOperations();
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 15_000,
    enabled: open,
  });

  useEffect(() => {
    if (!open) return;
    setQuery('');
    setActive(0);
    const timer = window.setTimeout(() => inputRef.current?.focus(), 30);
    return () => window.clearTimeout(timer);
  }, [open]);

  const commands = useMemo<Cmd[]>(() => {
    const pages = visibleOrder(undefined).map((id) => {
      const item = navById.get(id)!;
      return { id: `page:${id}`, label: item.label, hint: item.hint, icon: item.icon, group: '页面', run: () => navigate(item.path) };
    });
    const nodeCommands = (nodes.data?.items ?? []).map((node) => ({
      id: `node:${node.id}`,
      label: node.name,
      hint: `${node.region || '未设地域'} · ${node.connected ? '在线' : '离线'}`,
      icon: 'nodes' as IconName,
      group: '节点',
      run: () => navigate(`/nodes/${node.id}`),
    }));
    return [
      { id: 'act:queue', label: '打开操作队列', hint: '未决变更', icon: 'terminal', group: '操作', run: () => operations.open() },
      { id: 'act:refresh', label: '刷新全部数据', hint: '重新读取', icon: 'refresh', group: '操作', run: () => void queryClient.invalidateQueries() },
      ...pages,
      ...nodeCommands,
    ];
  }, [navigate, nodes.data, operations]);

  const filtered = commands.filter((command) => {
    if (!query.trim()) return true;
    const needle = query.trim().toLowerCase();
    return `${command.label}${command.hint ?? ''}`.toLowerCase().includes(needle);
  });

  useEffect(() => setActive(0), [query]);

  useEffect(() => {
    if (!open) return;
    const handler = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
      if (event.key === 'ArrowDown') {
        event.preventDefault();
        setActive((index) => Math.min(index + 1, filtered.length - 1));
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault();
        setActive((index) => Math.max(index - 1, 0));
      }
      if (event.key === 'Enter') {
        event.preventDefault();
        const picked = filtered[active];
        if (picked) {
          picked.run();
          onClose();
        }
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [open, filtered, active, onClose]);

  if (!open) return null;
  let lastGroup = '';
  return (
    <div className="palette" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <div className="palette__panel" role="dialog" aria-modal="true" aria-label="搜索页面与节点">
        <input
          ref={inputRef}
          className="palette__input"
          placeholder="搜索页面、节点或操作…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          aria-label="搜索"
        />
        <div className="palette__list" role="listbox">
          {filtered.length === 0 ? <p className="empty">没有匹配项</p> : null}
          {filtered.slice(0, 40).map((command, index) => {
            const header = command.group !== lastGroup ? command.group : null;
            lastGroup = command.group;
            return (
              <div key={command.id}>
                {header ? <div className="palette__group">{header}</div> : null}
                <button
                  type="button"
                  role="option"
                  aria-selected={index === active}
                  className="palette__item"
                  onMouseEnter={() => setActive(index)}
                  onClick={() => {
                    command.run();
                    onClose();
                  }}
                >
                  <Icon name={command.icon} />
                  <span>{command.label}</span>
                  {command.hint ? <span className="hint">{command.hint}</span> : null}
                </button>
              </div>
            );
          })}
        </div>
        <div className="palette__foot">
          <span>↑↓ 选择</span>
          <span>↵ 打开</span>
          <span>esc 关闭</span>
        </div>
      </div>
    </div>
  );
}

export function useTheme() {
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
  return { theme, setTheme };
}

function ThemeButton() {
  const { theme, setTheme } = useTheme();
  const next = theme === 'dark' ? 'light' : theme === 'light' ? 'system' : 'dark';
  const label = theme === 'dark' ? '深色' : theme === 'light' ? '浅色' : '跟随系统';
  return (
    <Button
      variant="ghost"
      size="sm"
      className="btn--icon"
      aria-label={`外观：${label}，点击切换`}
      title={`外观：${label}`}
      onClick={() => {
        if (next === 'system') localStorage.removeItem('opsd.theme');
        else localStorage.setItem('opsd.theme', next);
        setTheme(next);
      }}
    >
      <Icon name={theme === 'dark' ? 'moon' : 'sun'} />
    </Button>
  );
}

export function Layout({ children }: { children?: ReactNode }) {
  const session = useSession();
  const location = useLocation();
  const [palette, setPalette] = useState(false);
  const [sheet, setSheet] = useState(false);

  useEffect(() => setSheet(false), [location.pathname]);

  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        setPalette((open) => !open);
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  const logout = useMutation({
    mutationFn: async () => value(await api.DELETE('/auth/session', { params: { header: { 'X-CSRF-Token': session.csrf_token } } })),
    onSuccess: () => {
      queryClient.clear();
      window.location.assign(session.login_address);
    },
  });

  return (
    <div className="shell">
      <header className="topbar">
        <NavLink to="/" className="brand" aria-label="OPSD 概览">
          <span className="brand__mark">OP</span>
          <span className="brand__text">
            <b>OPSD</b>
            <span>工作台</span>
          </span>
        </NavLink>

        <nav className="nav" aria-label="主导航">
          {visibleOrder(undefined).map((id) => {
            const item = navById.get(id)!;
            const active = item.path === '/' ? location.pathname === '/' : location.pathname.startsWith(item.path);
            return (
              <NavLink key={id} to={item.path} end={item.path === '/'} aria-current={active ? 'page' : undefined}>
                <Icon name={item.icon} />
                {item.label}
              </NavLink>
            );
          })}
        </nav>

        <Button
          variant="ghost"
          size="sm"
          className="nav-toggle btn--icon"
          aria-label="导航菜单"
          aria-expanded={sheet}
          onClick={() => setSheet((open) => !open)}
        >
          <Icon name="menu" />
        </Button>

        <div className="topbar__right">
          <button type="button" className="search-trigger" onClick={() => setPalette(true)} aria-label="打开搜索命令面板">
            <Icon name="search" width={14} height={14} />
            <span className="search-label">搜索</span>
            <kbd>⌘K</kbd>
          </button>
          <OperationsButton />
          <ThemeButton />
          <button type="button" className="avatar" onClick={() => logout.mutate()} title={`${session.session.username} · 点击退出`}>
            {session.session.username.slice(0, 1).toUpperCase()}
          </button>
        </div>
      </header>

      {sheet ? (
        <>
          <div className="navsheet-scrim" onClick={() => setSheet(false)} />
          <nav className="navsheet" aria-label="主导航（窄屏）">
            {visibleOrder(undefined).map((id) => {
              const item = navById.get(id)!;
              const active = item.path === '/' ? location.pathname === '/' : location.pathname.startsWith(item.path);
              return (
                <NavLink key={id} to={item.path} end={item.path === '/'} aria-current={active ? 'page' : undefined}>
                  <Icon name={item.icon} width={16} height={16} />
                  {item.label}
                </NavLink>
              );
            })}
          </nav>
        </>
      ) : null}

      <div className="main">{children ?? <Outlet />}</div>
      <CommandPalette open={palette} onClose={() => setPalette(false)} />
    </div>
  );
}

export type CurrentSession = Schema['CurrentSession'];
export { navigation, Clock, Led };
