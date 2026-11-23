import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Link, NavLink, useLocation, useNavigate } from 'react-router';
import { motion } from 'motion/react';
import { LogOut, Menu as MenuIcon, Moon, PanelLeftClose, PanelLeftOpen, Search, Sun, UserRound, ExternalLink, KeyRound, MonitorSmartphone } from 'lucide-react';
import { useQueryClient } from '@tanstack/react-query';
import { call } from '../api/http';
import type { NodeList } from '../api/types';
import { usePrefs, setPrefs, toggleTheme, resolvedTheme } from '../lib/prefs';
import { isTyping } from '../lib/hooks';
import { endSession, useSession } from './session';
import { useEventStream, useLink } from './events';
import { navGroups, settingsItem, titleFor, type NavItem } from './nav';
import { Palette, openPalette } from './Palette';
import { Dock } from './Dock';
import { ConfirmHost, ToastHost } from './Hosts';
import { Logo } from '../ui/logo';
import { Dot, IconButton, Kbd, Menu, spring } from '../ui/kit';
import { initials } from '../lib/format';

export function Shell({ children }: { children: ReactNode }) {
  const session = useSession();
  const prefs = usePrefs();
  const location = useLocation();
  const navigate = useNavigate();
  const [hover, setHover] = useState(false);
  const [mobile, setMobile] = useState(false);
  const hoverTimer = useRef(0);
  useEventStream(session.session.id);

  const open = prefs.pinned || hover || mobile;
  useEffect(() => { setMobile(false); document.getElementById('scroll')?.scrollTo({ top: 0 }); }, [location.pathname]);
  useEffect(() => {
    const item = titleFor(location.pathname);
    document.title = `${item.label} · OPSD`;
  }, [location.pathname]);

  // “G 然后 字母”快速跳转。
  useEffect(() => {
    let armed = 0;
    const map: Record<string, string> = { o: '/', n: '/nodes', m: '/monitoring', c: '/containers', d: '/databases', f: '/firewall', t: '/tools/terminal', s: '/settings', k: '/tools/tasks', p: '/tools/operations' };
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || isTyping(e.target)) return;
      const k = e.key.toLowerCase();
      if (k === 'g') { armed = Date.now(); return; }
      if (armed && Date.now() - armed < 900 && map[k]) { e.preventDefault(); navigate(map[k]!); }
      armed = 0;
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [navigate]);

  const enter = () => { window.clearTimeout(hoverTimer.current); hoverTimer.current = window.setTimeout(() => setHover(true), 140); };
  const leave = () => { window.clearTimeout(hoverTimer.current); setHover(false); };

  return (
    <div className="app" data-pinned={prefs.pinned}>
      <div className="rail-slot">
        <nav className="rail" data-open={open} onPointerEnter={(e) => { if (e.pointerType === 'mouse') enter(); }} onPointerLeave={leave} aria-label="主导航">
          <Link to="/" className="brand" aria-label="OPSD 概览">
            <span className="brand-mark"><Logo size={32} /></span>
            <span className="brand-word nav-label">OPSD<small>对等节点控制台</small></span>
          </Link>
          <div className="nav nav-scroll">
            {navGroups.map((g) => (
              <div key={g.label} className="nav">
                <div className="nav-group-label">{g.label}</div>
                {g.items.map((item) => <RailLink key={item.path} item={item} />)}
              </div>
            ))}
          </div>
          <div className="rail-foot">
            <LinkState />
            <RailLink item={settingsItem} />
            <button type="button" className="nav-link" onClick={() => setPrefs({ pinned: !prefs.pinned })} aria-label={prefs.pinned ? '收起侧栏' : '固定侧栏'}>
              {prefs.pinned ? <PanelLeftClose /> : <PanelLeftOpen />}
              <span className="nav-label">{prefs.pinned ? '收起侧栏' : '固定侧栏'}</span>
            </button>
          </div>
        </nav>
      </div>
      {mobile && <div className="overlay" style={{ zIndex: 25 }} onClick={() => setMobile(false)} />}
      <div className="main">
        <header className="topbar">
          <IconButton label="打开导航" className="mobile-only" onClick={() => setMobile(true)}><MenuIcon /></IconButton>
          <Crumbs />
          <button type="button" className="search-trigger" onClick={() => openPalette()}>
            <Search />
            <span className="grow">搜索节点、容器，或执行命令…</span>
            <Kbd>{navigator.platform.toLowerCase().includes('mac') ? '⌘' : 'Ctrl'}</Kbd><Kbd>K</Kbd>
          </button>
          <div className="top-actions">
            <IconButton label={resolvedTheme(prefs) === 'dark' ? '切换浅色' : '切换深色'} onClick={(e) => toggleTheme({ x: e.clientX, y: e.clientY })}>
              {resolvedTheme(prefs) === 'dark' ? <Sun /> : <Moon />}
            </IconButton>
            <AccountMenu />
          </div>
        </header>
        <div className="scroll" id="scroll">
          <motion.div key={location.pathname.split('/')[1] || 'root'} className="route" initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.38, ease: [0.22, 1, 0.36, 1] }}>
            {children}
          </motion.div>
        </div>
      </div>
      <Palette />
      <Dock />
      <ToastHost />
      <ConfirmHost />
    </div>
  );
}

function RailLink({ item }: { item: NavItem }) {
  const Icon = item.icon;
  return (
    <NavLink to={item.path} end={item.path === '/'} className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`} aria-label={item.label}>
      {({ isActive }) => (
        <>
          {isActive && <motion.span layoutId="nav-pill" className="nav-pill" transition={spring} />}
          <Icon aria-hidden />
          <span className="nav-label">{item.label}</span>
        </>
      )}
    </NavLink>
  );
}

function LinkState() {
  const state = useLink();
  const label = state.state === 'live' ? '实时连接' : state.state === 'connecting' ? '正在连接' : '重新连接中';
  return (
    <div className="nav-link" title={label} style={{ cursor: 'default' }}>
      <span style={{ width: 19, display: 'grid', placeItems: 'center' }}><Dot tone={state.state === 'live' ? 'ok' : 'warn'} live={state.state === 'live'} /></span>
      <span className="nav-label" style={{ fontSize: 13 }}>{label}</span>
    </div>
  );
}

function Crumbs() {
  const location = useLocation();
  const client = useQueryClient();
  const item = titleFor(location.pathname);
  const parts = location.pathname.split('/').filter(Boolean);
  let detail: string | undefined;
  if (parts[0] === 'nodes' && parts[1]) {
    const list = client.getQueryData<NodeList>(['nodes']);
    detail = list?.items.find((n) => n.id === decodeURIComponent(parts[1]!))?.name ?? '节点详情';
  } else if (parts[0] === 'databases' && parts[1]) detail = '集群详情';
  else if (parts[0] === 'settings' && parts[1]) detail = ({ account: '账号', sessions: '登录会话', api: 'API 令牌', appearance: '外观', about: '版本' } as Record<string, string>)[parts[1]];
  return (
    <div className="crumbs">
      {detail ? (
        <>
          <Link to={item.path}>{item.label}</Link>
          <span className="crumb-sep">/</span>
          <motion.span key={detail} className="crumb-now" initial={{ opacity: 0, x: -4 }} animate={{ opacity: 1, x: 0 }}>{detail}</motion.span>
        </>
      ) : (
        <motion.span key={item.path} className="crumb-now" initial={{ opacity: 0, x: -4 }} animate={{ opacity: 1, x: 0 }}>{item.label}</motion.span>
      )}
    </div>
  );
}

function AccountMenu() {
  const session = useSession();
  const navigate = useNavigate();
  const logout = async () => {
    try { await call('DELETE', '/auth/sessions/current'); } catch { /* 无论结果都回到登录入口 */ }
    endSession(session.login_address, false);
  };
  return (
    <Menu
      width={240}
      trigger={(p) => (
        <button type="button" className="row" style={{ height: 36, padding: '0 10px 0 4px', borderRadius: 10, gap: 8 }} {...p} aria-label={`账号菜单 ${session.session.username}`}>
          <span className="avatar" style={{ width: 28, height: 28, fontSize: 12, borderRadius: 8 }}>{initials(session.session.username)}</span>
          <span style={{ fontWeight: 560, fontSize: 13.5 }} className="ellipsis">{session.session.username}</span>
        </button>
      )}
      items={[
        { label: '账号与安全入口', icon: <UserRound />, onSelect: () => navigate('/settings/account') },
        { label: '登录会话', icon: <MonitorSmartphone />, onSelect: () => navigate('/settings/sessions') },
        { label: 'API 令牌', icon: <KeyRound />, onSelect: () => navigate('/settings/api') },
        { separator: true, label: '' },
        { label: '退出', icon: <LogOut />, danger: true, onSelect: logout },
      ]}
    />
  );
}
