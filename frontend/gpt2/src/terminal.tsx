import { createContext, useContext, useEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router';
import type { Terminal } from '@xterm/xterm';
import type { FitAddon } from '@xterm/addon-fit';
import type { SearchAddon } from '@xterm/addon-search';
import { useTheme } from '@appica/ui-react/hooks/use-theme';
import { DropdownMenu, DropdownMenuContent, DropdownMenuEmpty, DropdownMenuFilterProvider, DropdownMenuInput, DropdownMenuItem, DropdownMenuList, DropdownMenuSeparator, DropdownMenuTrigger } from '@appica/ui-react/dropdown-menu';
import { Popover, PopoverContent, PopoverTitle } from '@appica/ui-react/popover';
import { ArrowDown, ArrowUp, Check, ChevronDown, Clipboard, Copy, Ellipsis, Eraser, Maximize, Plus, Search, Settings2, SquareTerminal, Unplug, X } from 'lucide-react';
import { useUI } from './store';
import { useQuery } from '@tanstack/react-query';
import { api, unwrap } from './api/client';
import { nodesQuery } from './pages/Nodes';
import { terminalFonts } from './theme';
import { Btn, Choice, Empty, Field, IconButton, Status } from './ui';
import '@xterm/xterm/css/xterm.css';

type Session = { id: string; nodeId: string; name: string; ordinal: number; state: 'loading' | 'load_error' | 'connecting' | 'connected' | 'disconnected'; attempt: number; message?: string; reloadPage?: boolean };
type Runtime = { term: Terminal; fit: FitAddon; search: SearchAddon; transport: TerminalTransport; dispose: () => void };
function reloadFailedTerminalFonts() {
  const failed = new Set<string>();
  document.fonts.forEach(face => { if (face.status === 'error') failed.add(face.family.replaceAll(/["']/g, '')); });
  const reload = (sheet: CSSStyleSheet | CSSGroupingRule, base: string) => {
    for (let index = 0; index < sheet.cssRules.length; index++) {
      const rule = sheet.cssRules[index];
      if (rule instanceof CSSGroupingRule) reload(rule, base);
      if (!(rule instanceof CSSFontFaceRule)) continue;
      const family = rule.style.getPropertyValue('font-family').replaceAll(/["']/g, '').trim();
      if (!failed.has(family) || !['JetBrains Mono', 'Sarasa Fixed SC'].includes(family)) continue;
      // CSS 字体失败会缓存错误；重新创建字体规则，才会发起新的资源读取。
      const css = rule.cssText.replace(/url\((['"]?)(.*?)\1\)/g, (_, _quote, path: string) => {
        const url = new URL(path, base); url.searchParams.set('opsd_font_retry', String(Date.now())); return `url("${url.href}")`;
      });
      sheet.deleteRule(index); sheet.insertRule(css, index);
    }
  };
  for (const sheet of document.styleSheets) if (!sheet.href || new URL(sheet.href).origin === location.origin) reload(sheet, sheet.href ?? location.href);
}
function terminalAddress(id: string | undefined, search: string, hash: string) {
  const params = new URLSearchParams(search);
  params.delete('node');
  if (id) params.set('session', id); else params.delete('session');
  return `/tools/terminal${params.size ? `?${params}` : ''}${hash}`;
}
function useSessions() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeId, setActiveId] = useState('');
  const runtimes = useRef(new Map<string, Runtime>());
  const sequence = useRef(0);
  const nodeSequence = useRef(new Map<string, number>());
  const location = useLocation(), navigate = useNavigate();
  function create(nodeId: string, name: string) {
    const id = `session-${++sequence.current}`;
    const ordinal = (nodeSequence.current.get(nodeId) ?? 0) + 1;
    nodeSequence.current.set(nodeId, ordinal);
    setSessions(old => [...old, { id, nodeId, name, ordinal, state: 'loading', attempt: 0 }]);
    setActiveId(id);
    return id;
  }
  function close(id: string) {
    const currentId = location.pathname === '/tools/terminal' ? new URLSearchParams(location.search).get('session') ?? activeId : activeId;
    const index = sessions.findIndex(session => session.id === id);
    const replacement = sessions[index + 1] ?? sessions[index - 1];
    runtimes.current.get(id)?.dispose();
    runtimes.current.delete(id);
    setSessions(old => old.filter(session => session.id !== id));
    if (currentId === id) {
      setActiveId(replacement?.id ?? '');
      if (location.pathname === '/tools/terminal') navigate(terminalAddress(replacement?.id, location.search, location.hash), { replace: true });
    }
  }
  function state(id: string, value: Session['state'], message?: string, reloadPage = false) { setSessions(old => old.map(session => session.id === id ? { ...session, state: value, message, reloadPage } : session)); }
  useEffect(() => {
    const release = () => { runtimes.current.forEach(runtime => runtime.dispose()); runtimes.current.clear(); };
    window.addEventListener('opsd:session-clearing', release);
    return () => { window.removeEventListener('opsd:session-clearing', release); release(); };
  }, []);
  const reload = (id: string) => {
    const session = sessions.find(value => value.id === id);
    if (session?.reloadPage) {
      // 浏览器会缓存失败的模块导入；重新打开当前节点入口后再创建会话。
      const url = new URL(window.location.href); url.searchParams.delete('session'); url.searchParams.set('node', session.nodeId); window.location.assign(url.href); return;
    }
    reloadFailedTerminalFonts(); setSessions(old => old.map(session => session.id === id ? { ...session, state: 'loading', message: undefined, attempt: session.attempt + 1 } : session));
  };
  return { sessions, runtimes, create, close, state, activeId, setActiveId, reload };
}
const TerminalContext = createContext<ReturnType<typeof useSessions> | null>(null);
export function TerminalProvider({ children }: { children: ReactNode }) { return <TerminalContext value={useSessions()}>{children}</TerminalContext>; }
export function useTerminals() { const value = useContext(TerminalContext); if (!value) throw new Error('终端工作区尚未初始化'); return value; }

// 每个连接单独申请一次性票据；输入只发送到当前已连接的 WebSocket。
class TerminalTransport {
  connected = false;
  private disposed = false;
  private socket?: WebSocket;
  private controller?: AbortController;
  private cols = 80;
  private rows = 24;
  private message = '';
  constructor(private nodeID: string, private output: (bytes: Uint8Array) => void, private onState: (state: Session['state'], message?: string) => void) { queueMicrotask(() => void this.connect()); }
  private async connect() {
    if (this.disposed) return;
    const controller = new AbortController(); this.controller = controller;
    this.message = ''; this.onState('connecting');
    try {
      const ticket = unwrap(await api.POST('/nodes/{node_id}/terminal', { signal: controller.signal, params: { path: { node_id: this.nodeID } } }));
      if (this.disposed || controller.signal.aborted) return;
      if (new Date(ticket.expires_at).getTime() <= Date.now()) throw new Error('终端票据已过期，请重新连接');
      const url = new URL(ticket.url, location.origin);
      if (url.origin !== location.origin) throw new Error('终端地址无效');
      url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const socket = new WebSocket(url); this.socket = socket;
      socket.onopen = () => { if (this.disposed) { socket.close(); return; } this.connected = true; this.onState('connected'); this.resize(this.cols, this.rows); };
      socket.onmessage = event => { if (this.disposed) return; try { const frame = JSON.parse(event.data); if (frame.type === 'output' && typeof frame.data === 'string') this.output(Uint8Array.from(atob(frame.data), char => char.charCodeAt(0))); else if (frame.type === 'exit') { this.message = frame.message || '进程已退出'; this.connected = false; this.onState('disconnected', this.message); socket.close(); } } catch { this.message = '终端消息无效，请重新连接'; socket.close(); } };
      socket.onerror = () => { this.message ||= new Date(ticket.expires_at).getTime() <= Date.now() ? '终端票据已过期，请重新连接' : '终端连接失败，请重新连接'; };
      socket.onclose = () => { if (this.socket !== socket || this.disposed) return; this.connected = false; this.onState('disconnected', this.message || '连接已断开'); };
    } catch (error) { if (!this.disposed && !controller.signal.aborted) this.onState('disconnected', error instanceof Error ? error.message : '终端连接失败'); }
  }
  write(data: string) {
    if (!this.connected || this.socket?.readyState !== WebSocket.OPEN) return;
    const bytes = new TextEncoder().encode(data);
    for (let offset = 0; offset < bytes.length; offset += 32768) this.socket.send(JSON.stringify({ type: 'input', data: btoa(String.fromCharCode(...bytes.subarray(offset, offset + 32768))) }));
  }
  resize(cols: number, rows: number) { this.cols = Math.max(2, Math.min(500, cols)); this.rows = Math.max(2, Math.min(300, rows)); if (this.connected && this.socket?.readyState === WebSocket.OPEN) this.socket.send(JSON.stringify({ type: 'resize', cols: this.cols, rows: this.rows })); }
  disconnect() { this.controller?.abort(); this.message = '已主动断开'; this.connected = false; this.socket?.close(); this.onState('disconnected', this.message); }
  reconnect() { if (this.connected || this.disposed) return; this.controller?.abort(); this.socket?.close(); this.socket = undefined; void this.connect(); }
  dispose() { this.disposed = true; this.connected = false; this.controller?.abort(); this.socket?.close(); }
}

function terminalTheme() {
  const css = getComputedStyle(document.documentElement);
  const color = (name: string) => css.getPropertyValue(name).trim();
  return { background: color('--terminal'), foreground: color('--terminal-text'), cursor: color('--terminal-text'), selectionBackground: color('--terminal-selection'), black: color('--terminal-black'), brightBlack: color('--terminal-muted'), red: color('--terminal-red'), brightRed: color('--terminal-red'), green: color('--terminal-green'), brightGreen: color('--terminal-green'), yellow: color('--terminal-yellow'), brightYellow: color('--terminal-yellow'), blue: color('--terminal-blue'), brightBlue: color('--terminal-blue'), magenta: color('--terminal-magenta'), brightMagenta: color('--terminal-magenta'), cyan: color('--terminal-cyan'), brightCyan: color('--terminal-cyan'), white: color('--terminal-text'), brightWhite: color('--terminal-text') };
}

function TerminalSurface({ session, visible, onSearch }: { session: Session; visible: boolean; onSearch: () => void }) {
  const element = useRef<HTMLDivElement>(null);
  const { runtimes, state } = useTerminals();
  const { preferences } = useUI();
  const { resolvedTheme } = useTheme();
  const current = useRef({ preferences, visible, onSearch });
  current.current = { preferences, visible, onSearch };
  useEffect(() => {
    const host = element.current!;
    delete host.dataset.ready;
    let cancelled = false, moduleFailure = false, runtime: Runtime | undefined;
    const fonts = Promise.all([document.fonts.load('400 16px "JetBrains Mono"'), document.fonts.load('700 16px "JetBrains Mono"'), document.fonts.load('400 16px "Sarasa Fixed SC"', '中文'), document.fonts.load('700 16px "Sarasa Fixed SC"', '中文')]);
    const moduleError = (error: unknown): never => { moduleFailure = true; throw error; };
    const resources = Promise.all([fonts, import('@xterm/xterm').catch(moduleError), import('@xterm/addon-fit').catch(moduleError), import('@xterm/addon-search').catch(moduleError)]);
    void resources.then(([, { Terminal }, { FitAddon }, { SearchAddon }]) => {
      if (cancelled) return;
      const { preferences } = current.current;
      const term = new Terminal({ fontFamily: terminalFonts[preferences.terminalFont].family, fontSize: preferences.terminalSize, lineHeight: 1.25, fontWeight: '400', fontWeightBold: '700', theme: terminalTheme(), cursorBlink: true, scrollback: 5000, screenReaderMode: true, allowProposedApi: false });
      const fit = new FitAddon(), search = new SearchAddon();
      term.loadAddon(fit); term.loadAddon(search); term.open(host);
      const transport = new TerminalTransport(session.nodeId, bytes => term.write(bytes), (value, message) => state(session.id, value, message));
      const data = term.onData(value => transport.write(value));
      const resize = term.onResize(({ cols, rows }) => { host.dataset.cols = String(cols); host.dataset.rows = String(rows); transport.resize(cols, rows); });
      term.attachCustomKeyEventHandler(event => { if (event.type === 'keydown' && (event.ctrlKey || event.metaKey) && event.shiftKey && event.key.toLowerCase() === 'f') { event.preventDefault(); current.current.onSearch(); return false; } return true; });
      let frame = 0;
      const fitVisible = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(() => { if (!cancelled && host.clientWidth && host.clientHeight) { fit.fit(); host.dataset.cols = String(term.cols); host.dataset.rows = String(term.rows); } }); };
      const observer = new ResizeObserver(fitVisible); observer.observe(host); fitVisible();
      let disposed = false;
      runtime = { term, fit, search, transport, dispose: () => { if (disposed) return; disposed = true; cancelAnimationFrame(frame); observer.disconnect(); data.dispose(); resize.dispose(); transport.dispose(); term.dispose(); } };
      runtimes.current.set(session.id, runtime); host.dataset.ready = 'true';
      if (current.current.visible) term.focus();
    }).catch(() => { if (!cancelled) { state(session.id, 'load_error', '终端资源载入失败', moduleFailure); } });
    return () => { cancelled = true; if (runtimes.current.get(session.id) === runtime) runtimes.current.delete(session.id); runtime?.dispose(); };
  }, [session.id, session.attempt]);
  useEffect(() => {
    const runtime = runtimes.current.get(session.id);
    if (!runtime) return;
    runtime.term.options.fontFamily = terminalFonts[preferences.terminalFont].family;
    runtime.term.options.fontSize = preferences.terminalSize;
    const frame = requestAnimationFrame(() => {
      runtime.term.options.theme = terminalTheme();
      if (visible && element.current?.clientWidth) { runtime.fit.fit(); runtime.term.focus(); }
    });
    return () => cancelAnimationFrame(frame);
  }, [preferences, resolvedTheme, visible, session.id, runtimes]);
  return <div className="terminal-surface" hidden={!visible} data-session-id={session.id} ref={element}></div>;
}

function sessionName(session: Session, sessions: Session[]) {
  return session.ordinal > 1 || sessions.some(other => other.nodeId === session.nodeId && other.id !== session.id) ? `${session.name} · ${session.ordinal}` : session.name;
}

export function SessionNavigation({ onNavigate }: { onNavigate: () => void }) {
  const { sessions, activeId, setActiveId, close } = useTerminals();
  const { pathname, search, hash } = useLocation();
  if (!sessions.length) return null;
  const selectedId = new URLSearchParams(search).get('session') ?? activeId;
  return <section className="sidebar-sessions" aria-label="终端会话"><h2>会话</h2><nav aria-label="会话导航">{sessions.map(session => <div key={session.id} className={`sidebar-session ${pathname === '/tools/terminal' && selectedId === session.id ? 'selected' : ''}`}><Link to={terminalAddress(session.id, search, hash)} aria-current={pathname === '/tools/terminal' && selectedId === session.id ? 'page' : undefined} title={sessionName(session, sessions)} onClick={() => { setActiveId(session.id); onNavigate(); }}><SquareTerminal size={18} aria-hidden/><span>{sessionName(session, sessions)}</span></Link><IconButton icon={X} label={`关闭 ${sessionName(session, sessions)}`} onClick={() => close(session.id)}/></div>)}</nav></section>;
}

export function TerminalPreferences({ container, terminal = false }: { container?: RefObject<HTMLDivElement | null>; terminal?: boolean }) {
  const { preferences, setPreferences } = useUI();
  return <div className="terminal-preferences"><div className="preference-field"><span>字体</span><Choice label="终端字体" value={preferences.terminalFont} onChange={value => setPreferences(old => ({ ...old, terminalFont: value as 'jetbrains' | 'sarasa' }))} options={Object.entries(terminalFonts).map(([id, font]) => [id, font.label] as const)} popupClassName={terminal ? 'terminal-popup' : ''} container={container}/></div><div className="preference-field"><span>字号</span><Choice label="终端字号" value={String(preferences.terminalSize)} onChange={value => setPreferences(old => ({ ...old, terminalSize: +value }))} options={Array.from({ length: 13 }, (_, i) => [String(i + 12), `${i + 12}px`] as const)} popupClassName={terminal ? 'terminal-popup' : ''} container={container}/></div></div>;
}

function NewSessionMenu({ onCreate, container, empty = false }: { onCreate: (id: string) => void; container: RefObject<HTMLDivElement | null>; empty?: boolean }) {
  const query = useQuery(nodesQuery);
  const online = query.data?.items.filter(node => node.connected && !node.revoked_at) ?? [];
  const trigger = useRef<HTMLButtonElement>(null);
  return <DropdownMenuFilterProvider autoHighlight><DropdownMenu><DropdownMenuTrigger render={empty ? <Btn ref={trigger} icon={Plus}>连接节点</Btn> : <IconButton ref={trigger} icon={Plus} label="新建会话"/>}/><DropdownMenuContent className="action-menu terminal-popup node-picker" align="end" container={container} finalFocus={trigger} aria-label="选择终端节点"><DropdownMenuInput aria-label="搜索终端节点" placeholder="搜索节点…"/><DropdownMenuEmpty>{online.length ? '没有匹配的节点' : '暂无在线节点'}</DropdownMenuEmpty><DropdownMenuList>{online.map(node => <DropdownMenuItem key={node.id} label={`${node.name} ${node.address} ${node.region}`} onClick={() => onCreate(node.id)}><SquareTerminal size={18} aria-hidden/><span><strong>{node.name}</strong><small>{node.region} · {node.address}</small></span></DropdownMenuItem>)}</DropdownMenuList></DropdownMenuContent></DropdownMenu></DropdownMenuFilterProvider>;
}

export function TerminalWorkspace({ active }: { active: boolean }) {
  const model = useTerminals();
  const { setToast } = useUI();
  const nodesQueryResult = useQuery(nodesQuery), nodes = nodesQueryResult.data?.items ?? [];
  const [params] = useSearchParams(), location = useLocation(), navigate = useNavigate();
  const [searchOpen, setSearchOpen] = useState(false), [query, setQuery] = useState(''), [found, setFound] = useState<boolean | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false), [fullscreen, setFullscreen] = useState(false);
  const searchInput = useRef<HTMLInputElement>(null), workspace = useRef<HTMLDivElement>(null), moreButton = useRef<HTMLButtonElement>(null);
  const openingNode = useRef<string | null>(null);
  const selected = model.sessions.find(session => session.id === (params.get('session') ?? model.activeId)) ?? model.sessions[0];
  const runtime = () => selected && model.runtimes.current.get(selected.id);
  const activate = (id: string) => { model.setActiveId(id); navigate(terminalAddress(id, location.search, location.hash), { replace: true }); };
  function create(id: string) { const node = nodes.find(node => node.id === id && node.connected); if (!node) { setToast('节点离线，无法创建终端'); return; } activate(model.create(node.id, node.name)); }
  useEffect(() => {
    if (!active || !nodesQueryResult.data) return;
    const target = params.get('node');
    if (!target) { openingNode.current = null; return; }
    if (openingNode.current === target) return;
    openingNode.current = target;
    const node = nodes.find(node => node.id === target && node.connected);
    if (!node) { setToast('节点离线，无法创建终端'); navigate(terminalAddress(undefined, location.search, location.hash), { replace: true }); return; }
    const session = model.sessions.find(session => session.nodeId === target && session.state !== 'disconnected');
    activate(session?.id ?? model.create(node.id, node.name));
  }, [active, params.get('node'), nodesQueryResult.data]);
  useEffect(() => { if (searchOpen && active) searchInput.current?.focus(); }, [searchOpen, active]);
  useEffect(() => { setFound(null); }, [selected?.id]);
  useEffect(() => { if (!active) { setSearchOpen(false); setSettingsOpen(false); } }, [active]);
  useEffect(() => { const changed = () => setFullscreen(document.fullscreenElement === workspace.current); document.addEventListener('fullscreenchange', changed); return () => document.removeEventListener('fullscreenchange', changed); }, []);
  function find(previous = false) { const search = runtime()?.search; setFound(query && search ? (previous ? search.findPrevious(query) : search.findNext(query)) : null); }
  function copy() {
    const text = runtime()?.term.getSelection();
    if (!text) { setToast('请先选择终端内容'); return; }
    void navigator.clipboard.writeText(text).then(() => setToast('已复制')).catch(() => setToast('复制失败，请使用 Ctrl+Shift+C'));
  }
  function paste() { void navigator.clipboard.readText().then(text => { runtime()?.term.paste(text); runtime()?.term.focus(); }).catch(() => setToast('请在终端中按 Ctrl+Shift+V 粘贴')); }
  const stateLabel = { loading: '加载中', load_error: '载入失败', connecting: '连接中', connected: '已连接', disconnected: '已断开' };
  return <div className="terminal-workspace" ref={workspace} hidden={!active}>
    <header className="terminal-toolbar"><div className="terminal-identity">{selected ? <><h1><span className="terminal-session-name" title={sessionName(selected, model.sessions)}>{sessionName(selected, model.sessions)}</span><DropdownMenu><DropdownMenuTrigger className="terminal-session-switch" aria-label="切换终端会话"><span>{sessionName(selected, model.sessions)}</span><ChevronDown size={16} aria-hidden/></DropdownMenuTrigger><DropdownMenuContent className="action-menu terminal-popup session-picker" align="start" container={workspace}>{model.sessions.map(session => <DropdownMenuItem key={session.id} onClick={() => activate(session.id)}><span>{sessionName(session, model.sessions)}</span>{selected.id === session.id && <Check size={16} aria-hidden/>}</DropdownMenuItem>)}</DropdownMenuContent></DropdownMenu></h1><span className="terminal-os">{nodes.find(node => node.id === selected.nodeId)?.region}</span><Status tone={selected.state === 'connected' ? 'success' : selected.state === 'load_error' ? 'error' : 'neutral'}>{stateLabel[selected.state]}</Status></> : <h1>终端</h1>}</div><div className="terminal-actions"><NewSessionMenu onCreate={create} container={workspace}/><IconButton icon={Search} label="搜索终端 Ctrl+Shift+F" disabled={!selected} aria-pressed={searchOpen} onClick={() => setSearchOpen(value => !value)}/><IconButton icon={Maximize} label={fullscreen ? '退出终端全屏' : '终端全屏'} onClick={() => { void (document.fullscreenElement ? document.exitFullscreen() : workspace.current?.requestFullscreen())?.catch(() => setToast('浏览器未允许全屏')); }}/><Popover open={settingsOpen} onOpenChange={setSettingsOpen}><DropdownMenu><DropdownMenuTrigger render={<IconButton ref={moreButton} icon={Ellipsis} label="终端更多操作"/>}/><DropdownMenuContent className="action-menu terminal-popup" align="end" container={workspace} finalFocus={settingsOpen ? false : moreButton}><DropdownMenuItem disabled={!selected} onClick={copy}><Copy size={18} aria-hidden/>复制选中内容</DropdownMenuItem><DropdownMenuItem disabled={selected?.state !== 'connected'} onClick={paste}><Clipboard size={18} aria-hidden/>粘贴</DropdownMenuItem><DropdownMenuItem disabled={!selected} onClick={() => { runtime()?.term.clear(); runtime()?.term.focus(); }}><Eraser size={18} aria-hidden/>清屏</DropdownMenuItem><DropdownMenuSeparator/><DropdownMenuItem onClick={() => setSettingsOpen(true)}><Settings2 size={18} aria-hidden/>终端设置</DropdownMenuItem><DropdownMenuSeparator/><DropdownMenuItem disabled={selected?.state !== 'connected'} onClick={() => runtime()?.transport.disconnect()}><Unplug size={18} aria-hidden/>断开会话</DropdownMenuItem>{selected && <DropdownMenuItem onClick={() => model.close(selected.id)}><X size={18} aria-hidden/>关闭会话</DropdownMenuItem>}</DropdownMenuContent></DropdownMenu><PopoverContent arrow={false} className="terminal-popup terminal-settings" anchor={moreButton} align="end" container={workspace} finalFocus={moreButton} aria-label="终端设置"><div className="terminal-settings-heading"><PopoverTitle>终端设置</PopoverTitle><IconButton icon={X} label="关闭终端设置" onClick={() => setSettingsOpen(false)}/></div><TerminalPreferences container={workspace} terminal/></PopoverContent></Popover></div></header>
    {selected ? <>
      {searchOpen && <form className="terminal-search" onSubmit={event => { event.preventDefault(); find(); }} onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setSearchOpen(false); runtime()?.term.focus(); } }}><Field ref={searchInput} label="查找内容" value={query} onChange={event => { setQuery(event.target.value); setFound(null); }} placeholder="搜索当前会话…"/><IconButton icon={ArrowUp} label="上一个匹配" onClick={() => find(true)}/><IconButton icon={ArrowDown} label="下一个匹配" type="submit"/><span className="meta" role="status">{found === false ? '未找到' : found === true ? '已选中匹配内容' : ''}</span><IconButton icon={X} label="关闭搜索" onClick={() => { setSearchOpen(false); runtime()?.term.focus(); }}/></form>}
      <div className="terminal-viewport">{model.sessions.map(session => <div className="terminal-panel" key={session.id} role="region" aria-label={`${sessionName(session, model.sessions)} 终端输出`} hidden={!active || session.id !== selected.id}><TerminalSurface session={session} visible={active && session.id === selected.id} onSearch={() => setSearchOpen(true)}/></div>)}</div>
      {['disconnected', 'load_error'].includes(selected.state) && <div className="terminal-disconnected"><span role="status">{selected.message || '连接已断开'}</span><Btn icon={Plus} onClick={() => selected.state === 'load_error' ? model.reload(selected.id) : runtime()?.transport.reconnect()}>{selected.state === 'load_error' ? '重新加载' : '重新连接'}</Btn></div>}
    </> : <div className="terminal-welcome"><Empty icon={SquareTerminal} title="选择节点开始会话" action={<NewSessionMenu onCreate={create} container={workspace} empty/>}/></div>}
  </div>;
}
