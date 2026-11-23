import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router';
import { AnimatePresence, motion } from 'motion/react';
import { Plus, RotateCw, SquareTerminal, X, Minus, Server } from 'lucide-react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { call, enc, errorText } from '../api/http';
import type { TerminalTicket } from '../api/types';
import { useNodes } from '../app/queries';
import { confirm } from '../app/confirm';
import { createStore, useStore } from '../lib/store';
import { decodeBase64, encodeBase64 } from '../lib/format';
import { prefs, setPrefs, usePrefs } from '../lib/prefs';
import { Badge, Button, Empty, IconButton, Menu, Spinner } from '../ui/kit';

type State = 'connecting' | 'connected' | 'closed' | 'error';
interface Session { id: string; nodeId: string; nodeName: string; state: State; message?: string }
interface Runtime { term: XTerm; fit: FitAddon; host: HTMLDivElement; ws?: WebSocket; disposers: (() => void)[]; opened: boolean }

const sessions = createStore<{ list: Session[]; active?: string }>({ list: [] });
const runtimes = new Map<string, Runtime>();
let seq = 0;

function patch(id: string, p: Partial<Session>) {
  sessions.set((s) => ({ ...s, list: s.list.map((x) => (x.id === id ? { ...x, ...p } : x)) }));
}

function termTheme() {
  const css = getComputedStyle(document.documentElement);
  const v = (name: string) => css.getPropertyValue(name).trim();
  return {
    background: '#111317',
    foreground: '#d9dde6',
    cursor: v('--accent') || '#5ef1c2',
    cursorAccent: '#0b0c0f',
    selectionBackground: 'rgba(120, 160, 255, 0.28)',
    black: '#1b1e25', red: '#ff6b6b', green: '#5ef1a6', yellow: '#ffd166', blue: '#6aa6ff', magenta: '#d68bff', cyan: '#5fe3e3', white: '#d9dde6',
    brightBlack: '#5b6270', brightRed: '#ff8a8a', brightGreen: '#8af7c0', brightYellow: '#ffe08a', brightBlue: '#94c0ff', brightMagenta: '#e5adff', brightCyan: '#8af0f0', brightWhite: '#ffffff',
  };
}

function open(nodeId: string, nodeName: string) {
  const id = `t${++seq}`;
  const host = document.createElement('div');
  host.className = 'term-host';
  const term = new XTerm({
    fontFamily: "'JetBrains Mono Variable', 'JetBrains Mono', 'Sarasa Fixed SC', Consolas, monospace",
    fontSize: prefs.get().terminalSize,
    lineHeight: 1.15,
    cursorBlink: true,
    cursorStyle: 'bar',
    scrollback: 5000,
    theme: termTheme(),
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  const rt: Runtime = { term, fit, host, disposers: [], opened: false };
  runtimes.set(id, rt);
  sessions.set((s) => ({ list: [...s.list, { id, nodeId, nodeName, state: 'connecting' }], active: id }));
  void connect(id);
  return id;
}

async function connect(id: string) {
  const rt = runtimes.get(id);
  const s = sessions.get().list.find((x) => x.id === id);
  if (!rt || !s) return;
  rt.disposers.forEach((d) => d());
  rt.disposers = [];
  patch(id, { state: 'connecting', message: undefined });
  try {
    const ticket = (await call<TerminalTicket>('POST', `/nodes/${enc(s.nodeId)}/terminal`)).data;
    const url = new URL(ticket.url, window.location.origin);
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(url);
    rt.ws = ws;
    const send = (frame: object) => { if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(frame)); };
    const resize = () => send({ type: 'resize', cols: Math.max(2, Math.min(500, rt.term.cols)), rows: Math.max(2, Math.min(300, rt.term.rows)) });
    ws.onopen = () => {
      patch(id, { state: 'connected' });
      try { rt.fit.fit(); } catch { /* 宿主不可见时稍后再适配 */ }
      resize();
      rt.term.focus();
    };
    ws.onmessage = (event) => {
      try {
        const f = JSON.parse(String(event.data)) as { type: string; data?: string; message?: string };
        if (f.type === 'output' && f.data) rt.term.write(decodeBase64(f.data));
        else if (f.type === 'exit') { patch(id, { state: 'closed', message: f.message || '会话已结束' }); ws.close(); }
      } catch { /* 忽略无效帧 */ }
    };
    ws.onerror = () => patch(id, { state: 'error', message: '连接中断' });
    ws.onclose = () => { const cur = sessions.get().list.find((x) => x.id === id); if (cur && cur.state !== 'closed' && cur.state !== 'error') patch(id, { state: 'closed', message: '连接已关闭' }); };
    const data = rt.term.onData((text) => {
      const bytes = new TextEncoder().encode(text);
      for (let i = 0; i < bytes.length; i += 24 * 1024) send({ type: 'input', data: encodeBase64(bytes.subarray(i, i + 24 * 1024)) });
    });
    const size = rt.term.onResize(() => resize());
    rt.disposers.push(() => data.dispose(), () => size.dispose(), () => { ws.onclose = null; ws.onerror = null; ws.onmessage = null; try { ws.close(); } catch { /* 忽略 */ } });
  } catch (error) {
    patch(id, { state: 'error', message: errorText(error) });
  }
}

function close(id: string) {
  const rt = runtimes.get(id);
  if (rt) {
    try { if (rt.ws?.readyState === WebSocket.OPEN) rt.ws.send(JSON.stringify({ type: 'exit' })); } catch { /* 忽略 */ }
    rt.disposers.forEach((d) => d());
    rt.term.dispose();
    rt.host.remove();
    runtimes.delete(id);
  }
  sessions.set((s) => {
    const index = s.list.findIndex((x) => x.id === id);
    const list = s.list.filter((x) => x.id !== id);
    const active = s.active === id ? (list[Math.min(index, list.length - 1)]?.id) : s.active;
    return { list, active };
  });
}

// 主题或字号变化时同步到全部终端。
prefs.subscribe(() => {
  const size = prefs.get().terminalSize;
  window.setTimeout(() => {
    for (const rt of runtimes.values()) {
      rt.term.options.theme = termTheme();
      if (rt.term.options.fontSize !== size) { rt.term.options.fontSize = size; try { rt.fit.fit(); } catch { /* 忽略 */ } }
    }
  }, 460);
});

export function Terminal() {
  const state = useStore(sessions);
  const nodes = useNodes();
  const p = usePrefs();
  const [params] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const stage = useRef<HTMLDivElement>(null);
  const requested = params.get('node');
  const online = (nodes.data?.items ?? []).filter((n) => !n.revoked_at && n.connected);

  // 从其他页面带 node 参数进入时直接打开会话。
  const handled = useRef<string | null>(null);
  useEffect(() => {
    if (!requested || !nodes.data || handled.current === requested) return;
    handled.current = requested;
    const n = nodes.data.items.find((x) => x.id === requested);
    const next = new URLSearchParams(location.search);
    next.delete('node');
    if (n) next.set('session', open(n.id, n.name));
    const search = next.toString();
    navigate({ search: search ? `?${search}` : '', hash: location.hash }, { replace: true });
  }, [requested, nodes.data]); // eslint-disable-line react-hooks/exhaustive-deps

  const active = state.list.find((s) => s.id === state.active);
  useLayoutEffect(() => {
    const el = stage.current;
    if (!el) return;
    for (const [id, rt] of runtimes) {
      if (id === state.active) {
        if (rt.host.parentElement !== el) el.appendChild(rt.host);
        rt.host.style.display = '';
        if (!rt.opened) { rt.term.open(rt.host); rt.opened = true; }
      }
      else rt.host.style.display = 'none';
    }
    const rt = state.active ? runtimes.get(state.active) : undefined;
    if (rt) requestAnimationFrame(() => { try { rt.fit.fit(); } catch { /* 忽略 */ } rt.term.focus(); });
    return () => { for (const r of runtimes.values()) if (r.host.parentElement === el) el.removeChild(r.host); };
  }, [state.active, state.list.length]);

  useEffect(() => {
    const el = stage.current;
    if (!el) return;
    const observer = new ResizeObserver(() => { const rt = state.active ? runtimes.get(state.active) : undefined; try { rt?.fit.fit(); } catch { /* 忽略 */ } });
    observer.observe(el);
    return () => observer.disconnect();
  }, [state.active]);

  const pick = (fn: (id: string, name: string) => void) => online.map((n) => ({ label: n.name, icon: <Server />, onSelect: () => fn(n.id, n.name) }));

  return (
    <div className="term-shell">
      <div className="term-tabs" role="tablist">
        {state.list.map((s) => (
          <button key={s.id} type="button" role="tab" className="term-tab" aria-selected={s.id === state.active} onClick={() => sessions.set((x) => ({ ...x, active: s.id }))}>
            {s.id === state.active && <motion.span layoutId="term-tab" className="tab-bg" transition={{ type: 'spring', stiffness: 520, damping: 38 }} />}
            <span className={`dot tone-${s.state === 'connected' ? 'ok' : s.state === 'connecting' ? 'info' : s.state === 'error' ? 'bad' : 'idle'}`} style={{ width: 7, height: 7, boxShadow: 'none' }} />
            {s.nodeName}
            <span className="icon-btn sm" role="button" aria-label="关闭会话" onClick={async (e) => {
              e.stopPropagation();
              if (s.state === 'connected' || s.state === 'connecting') { if (!(await confirm({ title: `断开 ${s.nodeName} 的终端？`, body: '会话中的作业会随 root PTY 一并结束，输出将被清空。', danger: true, confirmLabel: '断开' }))) return; }
              close(s.id);
            }}><X /></span>
          </button>
        ))}
        <Menu align="start" width={240} trigger={(t) => <IconButton label="新建会话" {...t}><Plus /></IconButton>} items={online.length ? pick(open) : [{ label: '没有在线节点', disabled: true }]} />
        <span className="spacer" />
        {active && (
          <div className="row">
            {active.state !== 'connected' && <Badge tone={active.state === 'connecting' ? 'info' : active.state === 'error' ? 'bad' : 'idle'}>{active.state === 'connecting' ? '连接中' : active.message ?? '已断开'}</Badge>}
            <IconButton size="sm" label="缩小字号" onClick={() => setPrefs({ terminalSize: Math.max(11, p.terminalSize - 1) })}><Minus /></IconButton>
            <span className="num muted" style={{ fontSize: 12, width: 22, textAlign: 'center' }}>{p.terminalSize}</span>
            <IconButton size="sm" label="放大字号" onClick={() => setPrefs({ terminalSize: Math.min(22, p.terminalSize + 1) })}><Plus /></IconButton>
          </div>
        )}
      </div>
      <div className="term-stage" ref={stage}>
        <AnimatePresence>
          {!state.list.length && (
            <motion.div key="empty" className="term-overlay" style={{ background: 'transparent', backdropFilter: 'none' }} initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
              <Empty icon={<SquareTerminal />} title="打开一个 root 终端" action={
                <Menu align="start" width={260} trigger={(t) => <Button variant="primary" icon={<Plus />} {...t}>选择节点</Button>} items={online.length ? pick(open) : [{ label: '没有在线节点', disabled: true }]} />
              }>
                <span style={{ color: 'oklch(0.7 0.01 265)' }}>终端经认证节点通道直达目标节点的本机执行器；票据 30 秒有效且只能使用一次，输入输出不写入共享历史。</span>
              </Empty>
            </motion.div>
          )}
          {active && active.state !== 'connected' && (
            <motion.div key={`o-${active.id}-${active.state}`} className="term-overlay" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
              {active.state === 'connecting' ? <div className="row" style={{ color: 'oklch(0.85 0.01 265)' }}><Spinner /> 正在连接 {active.nodeName}…</div> : (
                <div className="col" style={{ alignItems: 'center', gap: 12, color: 'oklch(0.85 0.01 265)' }}>
                  <span>{active.message ?? '会话已结束'}</span>
                  <Button variant="primary" icon={<RotateCw />} onClick={() => { runtimes.get(active.id)?.term.reset(); void connect(active.id); }}>重新连接</Button>
                </div>
              )}
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  );
}
