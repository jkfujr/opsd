import { useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router';
import { AnimatePresence, motion } from 'motion/react';
import { useQueryClient } from '@tanstack/react-query';
import { ArrowRight, Boxes, CornerDownLeft, Database, LogOut, Moon, PanelLeft, Plus, Search, Server, SquareTerminal, ListChecks, Palette as PaletteIcon, ArrowUpDown } from 'lucide-react';
import { createStore, useStore } from '../lib/store';
import { fuzzy } from '../lib/format';
import { useHotkey } from '../lib/hooks';
import { setPrefs, prefs, toggleTheme } from '../lib/prefs';
import { allNav } from './nav';
import { openDock } from './ops';
import { useContainers, useDatabases, useNodes } from './queries';
import { endSession, useSession } from './session';
import { call } from '../api/http';
import { containerLabel } from '../lib/status';
import { Kbd, useLayer } from '../ui/kit';

const state = createStore({ open: false, query: '' });
export const openPalette = (query = '') => state.set({ open: true, query });
const closePalette = () => state.set((s) => ({ ...s, open: false }));

interface Entry {
  id: string;
  group: string;
  title: string;
  sub?: string;
  icon: ReactNode;
  keywords?: string;
  hint?: ReactNode;
  run: () => void;
}

export function Palette() {
  const { open } = useStore(state);
  useLayer(open, closePalette);
  useHotkey('mod+k', () => (state.get().open ? closePalette() : openPalette()));
  useHotkey('/', () => openPalette());
  return createPortal(
    <AnimatePresence>
      {open && (
        <>
          <motion.div key="o" className="overlay" style={{ zIndex: 89 }} initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 0.18 }} onClick={closePalette} />
          <div key="l" className="palette-layer">
            <motion.div
              className="palette"
              role="dialog"
              aria-label="命令面板"
              initial={{ opacity: 0, y: -14, scale: 0.97 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: -8, scale: 0.98, transition: { duration: 0.14 } }}
              transition={{ type: 'spring', stiffness: 520, damping: 36 }}
            >
              <PaletteBody />
            </motion.div>
          </div>
        </>
      )}
    </AnimatePresence>,
    document.body,
  );
}

function PaletteBody() {
  const navigate = useNavigate();
  const session = useSession();
  const client = useQueryClient();
  const { query } = useStore(state);
  const [active, setActive] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);
  const nodes = useNodes();
  const containers = useContainers();
  const databases = useDatabases();

  const go = (path: string) => () => { closePalette(); navigate(path); };

  const entries = useMemo<Entry[]>(() => {
    const list: Entry[] = [];
    for (const n of allNav) {
      const Icon = n.icon;
      list.push({ id: `nav:${n.path}`, group: '跳转', title: n.label, icon: <Icon />, keywords: n.keywords, hint: n.hint && <span className="pi-hint">{n.hint.split(' ').map((k) => <Kbd key={k}>{k}</Kbd>)}</span>, run: go(n.path) });
    }
    list.push(
      { id: 'act:add-node', group: '命令', title: '添加节点', sub: '生成一次性接入命令', icon: <Plus />, keywords: 'enroll join 接入 新增', run: go('/nodes?add=1') },
      { id: 'act:ops', group: '命令', title: '打开操作坞', sub: '查看进行中与待确认的操作', icon: <ListChecks />, keywords: 'operations dock 操作', run: () => { closePalette(); openDock(); } },
      { id: 'act:theme', group: '命令', title: '切换浅色 / 深色', icon: <Moon />, keywords: 'theme dark light 主题', run: () => { closePalette(); toggleTheme({ x: innerWidth / 2, y: 80 }); } },
      { id: 'act:pin', group: '命令', title: prefs.get().pinned ? '收起侧栏' : '固定侧栏', icon: <PanelLeft />, keywords: 'sidebar rail', run: () => { closePalette(); setPrefs({ pinned: !prefs.get().pinned }); } },
      { id: 'act:accent', group: '命令', title: '调整强调色与密度', icon: <PaletteIcon />, keywords: 'accent color density 外观', run: go('/settings/appearance') },
      { id: 'act:logout', group: '命令', title: '退出登录', icon: <LogOut />, keywords: 'logout signout', run: async () => { closePalette(); try { await call('DELETE', '/auth/sessions/current'); } catch { /* 忽略 */ } endSession(session.login_address, false); } },
    );
    for (const n of nodes.data?.items ?? []) {
      if (n.revoked_at) continue;
      list.push({ id: `node:${n.id}`, group: '节点', title: n.name, sub: [n.region, n.address].filter(Boolean).join(' · ') || n.id, icon: <Server />, keywords: `${n.id} ${n.region ?? ''} ${n.address ?? ''}`, hint: <span className="pi-hint">{n.connected ? '在线' : '离线'}</span>, run: go(`/nodes/${encodeURIComponent(n.id)}`) });
      list.push({ id: `term:${n.id}`, group: '终端', title: `在 ${n.name} 打开终端`, icon: <SquareTerminal />, keywords: `terminal shell ${n.id}`, run: go(`/tools/terminal?node=${encodeURIComponent(n.id)}`) });
    }
    for (const c of containers.data?.items ?? []) {
      list.push({ id: `ct:${c.node_id}/${c.container_id}`, group: '容器', title: c.name, sub: `${c.node_name} · ${c.image}`, icon: <Boxes />, keywords: `${c.image} ${c.project} ${c.service} ${c.container_id}`, hint: <span className="pi-hint">{containerLabel(c.state)}</span>, run: go(`/containers?open=${encodeURIComponent(`${c.node_id}/${c.engine_id}/${c.container_id}`)}`) });
    }
    for (const d of databases.data?.items ?? []) {
      list.push({ id: `db:${d.cluster.id}`, group: '数据库', title: d.cluster.name, sub: `${d.members.length} 个成员 · ${d.proxies.length} 个代理`, icon: <Database />, keywords: d.cluster.cluster_id, run: go(`/databases/${encodeURIComponent(d.cluster.id)}`) });
    }
    return list;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nodes.data, containers.data, databases.data, client]);

  const results = useMemo(() => {
    const q = query.trim();
    if (!q) {
      const pick = entries.filter((e) => e.group === '跳转' || e.group === '命令' || e.group === '节点').slice(0, 24);
      return pick.map((e) => ({ e, hits: [] as number[], score: 0 }));
    }
    return entries
      .map((e) => {
        const m = fuzzy(q, e.title);
        const k = !m ? fuzzy(q, `${e.sub ?? ''} ${e.keywords ?? ''}`) : null;
        if (!m && !k) return null;
        return { e, hits: m?.hits ?? [], score: (m?.score ?? 0) * 2 + (k?.score ?? 0) + (e.group === '跳转' ? 6 : 0) };
      })
      .filter((x): x is { e: Entry; hits: number[]; score: number } => !!x)
      .sort((a, b) => b.score - a.score)
      .slice(0, 40);
  }, [entries, query]);

  const grouped = useMemo(() => {
    const groups: { name: string; items: typeof results }[] = [];
    for (const r of results) {
      const g = groups.find((x) => x.name === r.e.group);
      if (g) g.items.push(r);
      else groups.push({ name: r.e.group, items: [r] });
    }
    return groups;
  }, [results]);
  const flat = grouped.flatMap((g) => g.items);

  useEffect(() => { setActive(0); }, [query]);
  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [active]);

  const onKey = (e: ReactKeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); closePalette(); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); setActive((a) => (flat.length ? (a + 1) % flat.length : 0)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((a) => (flat.length ? (a - 1 + flat.length) % flat.length : 0)); }
    else if (e.key === 'Enter') { e.preventDefault(); flat[active]?.e.run(); }
  };

  let index = -1;
  return (
    <>
      <div className="palette-input">
        <Search />
        <input autoFocus value={query} placeholder="输入节点、容器、页面或命令" onChange={(e) => state.set((s) => ({ ...s, query: e.target.value }))} onKeyDown={onKey} spellCheck={false} aria-label="搜索" />
        <Kbd>Esc</Kbd>
      </div>
      <div className="palette-list" ref={listRef} role="listbox">
        {flat.length === 0 && <div className="empty" style={{ padding: 36 }}><p>没有匹配“{query}”的结果</p></div>}
        {grouped.map((g) => (
          <div key={g.name}>
            <div className="palette-group">{g.name}</div>
            {g.items.map(({ e, hits }) => {
              index++;
              const i = index;
              const isActive = i === active;
              return (
                <button key={e.id} type="button" role="option" aria-selected={isActive} data-index={i} data-active={isActive} className="palette-item" onPointerMove={() => setActive(i)} onClick={() => e.run()}>
                  {isActive && <motion.span layoutId="palette-sel" className="pi-sel" transition={{ type: 'spring', stiffness: 700, damping: 45 }} />}
                  <span className="pi-icon">{e.icon}</span>
                  <span className="pi-main">
                    <span className="pi-title">{highlight(e.title, hits)}</span>
                    {e.sub && <span className="pi-sub">{e.sub}</span>}
                  </span>
                  {e.hint}
                  {isActive && <ArrowRight width={15} height={15} style={{ color: 'var(--text-3)' }} />}
                </button>
              );
            })}
          </div>
        ))}
      </div>
      <div className="palette-foot">
        <span><ArrowUpDown width={13} height={13} /> 选择</span>
        <span><CornerDownLeft width={13} height={13} /> 打开</span>
        <span className="right">G + 字母 快速跳转</span>
      </div>
    </>
  );
}

function highlight(text: string, hits: number[]) {
  if (!hits.length) return text;
  const set = new Set(hits);
  return Array.from(text).map((ch, i) => (set.has(i) ? <mark key={i}>{ch}</mark> : <span key={i}>{ch}</span>));
}
