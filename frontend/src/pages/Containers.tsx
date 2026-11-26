import { useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router';
import { AnimatePresence, motion } from 'motion/react';
import { Boxes, Download, FileText, Layers, Ellipsis, Play, RefreshCw, RotateCw, Square, ArrowDownToLine, Info, PackageCheck, Trash2, OctagonX } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { call, enc, get } from '../api/http';
import type { ComposeProjectItem, ContainerActionRequest, ContainerDetail, ContainerListItem, ContainerLogs, Operation } from '../api/types';
import { useCompose, useContainers, useNodes } from '../app/queries';
import { useCommand } from '../app/commands';
import { confirm } from '../app/confirm';
import { ops } from '../app/ops';
import { useStore } from '../lib/store';
import { ago, fullTime, stripAnsi } from '../lib/format';
import { containerActionLabel, containerLabel, containerTone, isTerminal, readProblem } from '../lib/status';
import { useVisible } from '../lib/hooks';
import { Badge, Button, Card, CardHead, CopyText, Dot, Empty, ErrorNotice, IconButton, Menu, Notice, PageHead, SearchInput, Segmented, Select, Sheet, Skeleton, Spinner, Tabs, Tag } from '../ui/kit';

type StateFilter = 'all' | 'running' | 'stopped' | 'problem';
const ref = (c: { node_id: string; engine_id: string; container_id: string }) => `${c.node_id}/${c.engine_id}/${c.container_id}`;

export function Containers() {
  const [params, setParams] = useSearchParams();
  const node = params.get('node') ?? '';
  const view = params.get('view') === 'compose' ? 'compose' : 'containers';
  const open = params.get('open');
  const setParam = (k: string, v?: string) => { const next = new URLSearchParams(params); if (v) next.set(k, v); else next.delete(k); setParams(next, { replace: true }); };
  const nodes = useNodes();
  return (
    <div className="page">
      <PageHead title="容器" sub="各节点 Docker Engine 中的容器与 Compose 项目；操作在目标节点持久受理。" actions={
        <Segmented value={view} onChange={(v) => setParam('view', v === 'compose' ? 'compose' : undefined)} options={[{ value: 'containers', label: '容器', icon: <Boxes width={15} height={15} /> }, { value: 'compose', label: 'Compose', icon: <Layers width={15} height={15} /> }]} />
      } />
      <AnimatePresence mode="wait">
        <motion.div key={view} initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }} transition={{ duration: 0.2 }} className="stack">
          {view === 'containers'
            ? <ContainerTable node={node} setNode={(v) => setParam('node', v)} nodes={nodes.data?.items ?? []} openRef={open} setOpen={(v) => setParam('open', v)} />
            : <ComposeList node={node} setNode={(v) => setParam('node', v)} nodes={nodes.data?.items ?? []} />}
        </motion.div>
      </AnimatePresence>
    </div>
  );
}

function NodeSelect({ value, onChange, nodes }: { value: string; onChange: (v?: string) => void; nodes: { id: string; name: string; revoked_at?: string }[] }) {
  return (
    <Select value={value} onChange={(e) => onChange(e.target.value || undefined)} style={{ width: 200 }} aria-label="节点">
      <option value="">全部节点</option>
      {nodes.filter((n) => !n.revoked_at).map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
    </Select>
  );
}

function useActiveOp(kind: 'container' | 'compose', id: string) {
  return useStore(ops, (s) => Object.values(s.items).find((t) => !isTerminal(t.op) && t.op.resource?.kind === kind && t.op.resource.id === id)?.op);
}

function ContainerTable({ node, setNode, nodes, openRef, setOpen }: { node: string; setNode: (v?: string) => void; nodes: { id: string; name: string }[]; openRef: string | null; setOpen: (v?: string) => void }) {
  const q = useContainers(node || undefined);
  const [search, setSearch] = useState('');
  const [state, setState] = useState<StateFilter>('all');
  const items = q.data?.items ?? [];
  const problem = (c: ContainerListItem) => c.health === 'unhealthy' || c.state === 'dead' || c.state === 'restarting';
  const filtered = items.filter((c) => {
    if (state === 'running' && c.state !== 'running') return false;
    if (state === 'stopped' && c.state === 'running') return false;
    if (state === 'problem' && !problem(c)) return false;
    const s = search.trim().toLowerCase();
    return !s || `${c.name} ${c.image} ${c.project} ${c.service} ${c.container_id}`.toLowerCase().includes(s);
  });
  const groups = useMemo(() => {
    const map = new Map<string, { name: string; items: ContainerListItem[] }>();
    for (const c of filtered) {
      const g = map.get(c.node_id) ?? { name: c.node_name, items: [] };
      g.items.push(c);
      map.set(c.node_id, g);
    }
    for (const g of map.values()) g.items.sort((a, b) => Number(b.state === 'running') - Number(a.state === 'running') || a.name.localeCompare(b.name));
    return [...map.entries()].sort((a, b) => a[1].name.localeCompare(b[1].name, 'zh-CN'));
  }, [filtered]);
  const issues = (q.data?.nodes ?? []).map((n) => ({ n, msg: readProblem(n.read) ?? (n.error?.message) ?? (n.state === 'not_installed' ? '未安装 Docker' : n.state === 'offline' ? '节点离线' : n.state === 'unavailable' ? 'Docker 不可用' : undefined) })).filter((x) => x.msg);
  const selected = openRef ? items.find((c) => ref(c) === openRef) : undefined;

  return (
    <>
      <div className="toolbar">
        <SearchInput className="search" value={search} onChange={setSearch} placeholder="名称、镜像、项目或 ID" />
        <Segmented value={state} onChange={setState} options={[
          { value: 'all', label: '全部', count: items.length },
          { value: 'running', label: '运行中', count: items.filter((c) => c.state === 'running').length },
          { value: 'stopped', label: '已停止', count: items.filter((c) => c.state !== 'running').length },
          { value: 'problem', label: '异常', count: items.filter(problem).length },
        ]} />
        <span className="spacer" />
        <NodeSelect value={node} onChange={setNode} nodes={nodes} />
        <IconButton label="刷新" onClick={() => void q.refetch()}>{q.isFetching ? <Spinner /> : <RefreshCw />}</IconButton>
      </div>
      {issues.length > 0 && <Notice tone="warn" title="部分节点无法读取">{issues.map((x) => `${x.n.node_name}：${x.msg}`).join('；')}</Notice>}
      {q.isError && <ErrorNotice error={q.error} retry={() => void q.refetch()} />}
      <Card>
        {q.isPending ? (
          <div className="stack" style={{ padding: 20 }}>{Array.from({ length: 6 }, (_, i) => <Skeleton key={i} height={38} />)}</div>
        ) : filtered.length === 0 ? (
          <Empty icon={<Boxes />} title={items.length ? '没有匹配的容器' : '没有容器'}>{items.length ? '调整筛选条件试试。' : '节点上的 Docker 还没有容器，或尚未读取完成。'}</Empty>
        ) : (
          <div className="table-wrap">
            <table className="table container-table">
              <thead><tr><th>容器</th><th>状态</th><th className="hide-sm">项目</th><th className="hide-sm">端口</th><th className="actions-cell" /></tr></thead>
              <tbody>
                {groups.map(([nodeId, g]) => (
                  <GroupRows key={nodeId} name={g.name} items={g.items} onOpen={(c) => setOpen(ref(c))} showGroup={!node} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      <ContainerSheet item={selected} onClose={() => setOpen(undefined)} />
    </>
  );
}

function GroupRows({ name, items, onOpen, showGroup }: { name: string; items: ContainerListItem[]; onOpen: (c: ContainerListItem) => void; showGroup: boolean }) {
  return (
    <>
      {showGroup && <tr className="group-row"><td colSpan={3}>{name} · {items.length}</td><td className="hide-sm" colSpan={2} /></tr>}
      {items.map((c) => <ContainerRow key={ref(c)} c={c} onOpen={() => onOpen(c)} />)}
    </>
  );
}

function ContainerActions({ c, active, onOpen }: { c: ContainerListItem; active: boolean; onOpen?: () => void }) {
  const command = useCommand<ContainerActionRequest, Operation>({
    label: ({ action }) => containerActionLabel[action],
    node: c.node_id,
    run: async (body, key) => (await call<Operation>('POST', `/nodes/${enc(c.node_id)}/engines/${enc(c.engine_id)}/containers/${enc(c.container_id)}/actions`, { key, body })).data,
  });
  const disabled = command.busy || active || !!c.read_only_reason;
  const run = async (action: ContainerActionRequest['action']) => {
    const text = {
      stop: ['停止', `${c.name} 将停止运行。`],
      kill: ['强制停止', `${c.name} 将立即停止，未保存的数据可能丢失。`],
      restart: ['重启', `${c.name} 将重新启动，服务会短暂中断。`],
      remove: ['删除', `将删除 ${c.name} 及其可写层，保留挂载卷和绑定目录。`],
      update: ['更新', `将拉取 ${c.image} 的最新镜像并重建 ${c.name}，保留卷与网络配置。`],
    };
    if (action !== 'start') {
      const [confirmLabel, body] = text[action];
      if (!await confirm({ title: `${containerActionLabel[action]}？`, body, confirmLabel, danger: action === 'stop' || action === 'kill' || action === 'remove' })) return;
    }
    void command.run({ action, expected_revision: c.revision });
  };
  const running = c.state === 'running';
  const stopped = c.state === 'created' || c.state === 'exited' || c.state === 'dead';
  return (
    <Menu trigger={(props) => <IconButton size="sm" label={onOpen ? '更多' : '容器操作'} {...props}><Ellipsis /></IconButton>} items={[
      ...(onOpen ? [{ label: '详情与日志', icon: <FileText />, onSelect: onOpen }] : []),
      ...(running ? [
        { label: '停止', icon: <Square />, disabled, onSelect: () => void run('stop') },
        { label: '强制停止', icon: <OctagonX />, disabled, danger: true, onSelect: () => void run('kill') },
        { label: '重启', icon: <RotateCw />, disabled, onSelect: () => void run('restart') },
      ] : []),
      ...(stopped ? [{ label: '启动', icon: <Play />, disabled, onSelect: () => void run('start') }] : []),
      { label: '更新镜像', icon: <Download />, disabled, onSelect: () => void run('update') },
      ...(stopped ? [{ label: '删除', icon: <Trash2 />, disabled, danger: true, onSelect: () => void run('remove') }] : []),
    ]} />
  );
}

function ContainerRow({ c, onOpen }: { c: ContainerListItem; onOpen: () => void }) {
  const active = useActiveOp('container', c.container_id);
  const tone = containerTone(c.state, c.health);
  const running = c.state === 'running';
  return (
    <motion.tr layout="position" className="clickable" onClick={onOpen} initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
      <td>
        <div className="ct-name">
          <Dot tone={tone} live={running && tone === 'ok'} />
          <div className="col" style={{ minWidth: 0 }}>
            <strong>{c.name}</strong>
            <span className="mono">{c.image}</span>
          </div>
        </div>
      </td>
      <td>
        <AnimatePresence mode="wait" initial={false}>
          {active ? (
            <motion.span key="op" initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }}><Badge tone="info"><Spinner size={12} />{active.state === 'accepted' ? '排队中' : '执行中'}</Badge></motion.span>
          ) : (
            <motion.span key={c.state} initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }}><Badge tone={tone}>{containerLabel(c.state)}{c.health && c.health !== 'none' ? ` · ${c.health === 'healthy' ? '健康' : c.health === 'unhealthy' ? '不健康' : c.health === 'starting' ? '启动中' : c.health}` : ''}</Badge></motion.span>
          )}
        </AnimatePresence>
      </td>
      <td className="hide-sm">{c.project ? <span className="row" style={{ gap: 6 }}><Tag>{c.project}</Tag>{c.service && <span className="muted" style={{ fontSize: 12 }}>{c.service}</span>}</span> : <span className="dim">—</span>}</td>
      <td className="hide-sm"><div className="ports">{c.ports.filter((p) => p.host_port).slice(0, 3).map((p, i) => <Tag key={i}>{p.host_port}→{p.container_port}/{p.protocol}</Tag>)}{c.ports.filter((p) => p.host_port).length > 3 && <Tag>+{c.ports.filter((p) => p.host_port).length - 3}</Tag>}</div></td>
      <td className="actions-cell" onClick={(e) => e.stopPropagation()}>
        <div className="row-actions">
          <ContainerActions c={c} active={!!active} onOpen={onOpen} />
        </div>
      </td>
    </motion.tr>
  );
}

function ContainerSheet({ item, onClose }: { item?: ContainerListItem; onClose: () => void }) {
  const [tab, setTab] = useState<'logs' | 'info'>('logs');
  const lastItem = useRef(item);
  if (item) lastItem.current = item;
  const c = item ?? lastItem.current;
  const active = useActiveOp('container', c?.container_id ?? '');
  return (
    <Sheet open={!!item} onClose={onClose} width={860} title={c?.name ?? ''} sub={c ? `${c.node_name} · ${c.image}` : ''} icon={c && <Dot tone={containerTone(c.state, c.health)} live={c.state === 'running'} />}
      actions={c && <ContainerActions key={ref(c)} c={c} active={!!active} />}>
      {c?.read_only_reason && <Notice tone="info">{c.read_only_reason}</Notice>}
      <Tabs value={tab} onChange={setTab} options={[{ value: 'logs', label: '日志' }, { value: 'info', label: '详情' }]} />
      {c && (tab === 'logs' ? <ContainerLogView c={c} /> : <ContainerInfo c={c} />)}
    </Sheet>
  );
}

function ContainerInfo({ c }: { c: ContainerListItem }) {
  const q = useQuery({ queryKey: ['containers', 'detail', ref(c)], queryFn: ({ signal }) => get<ContainerDetail>(`/nodes/${enc(c.node_id)}/engines/${enc(c.engine_id)}/containers/${enc(c.container_id)}`, { signal }) });
  const d = q.data;
  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  if (!d) return <div className="stack">{Array.from({ length: 8 }, (_, i) => <Skeleton key={i} />)}</div>;
  return (
    <div className="stack">
      <dl className="kv">
        <dt>容器 ID</dt><dd><CopyText value={d.container_id} display={d.container_id.slice(0, 24)} /></dd>
        <dt>镜像</dt><dd className="mono">{d.image}</dd>
        <dt>状态</dt><dd><Badge tone={containerTone(d.state, d.health)}>{containerLabel(d.state)}</Badge></dd>
        <dt>创建</dt><dd>{fullTime(d.created_at)}</dd>
        {d.started_at && <><dt>启动</dt><dd>{fullTime(d.started_at)}（{ago(d.started_at)}）</dd></>}
        <dt>重启策略</dt><dd>{d.restart_policy || '—'} · 已重启 {d.restart_count} 次</dd>
        {d.project && <><dt>Compose</dt><dd>{d.project} / {d.service}</dd></>}
        <dt>TTY</dt><dd>{d.tty ? '是' : '否'}</dd>
      </dl>
      {d.ports.length > 0 && <Card><CardHead title="端口" /><div className="card-body ports">{d.ports.map((p, i) => <Tag key={i}>{p.host_ip ? `${p.host_ip}:` : ''}{p.host_port || '—'} → {p.container_port}/{p.protocol}</Tag>)}</div></Card>}
      {d.mounts.length > 0 && (
        <Card><CardHead title="挂载" />
          <div className="list">{d.mounts.map((m, i) => <div key={i} className="list-item"><div className="li-main"><span className="li-title mono" style={{ fontSize: 12.5 }}>{m.destination}</span><span className="li-sub mono">{m.type} · {m.source}</span></div>{m.read_only && <Badge>只读</Badge>}</div>)}</div>
        </Card>
      )}
      {d.networks.length > 0 && (
        <Card><CardHead title="网络" />
          <div className="list">{d.networks.map((n, i) => <div key={i} className="list-item"><div className="li-main"><span className="li-title">{n.name}</span><span className="li-sub mono">{[n.ipv4, n.ipv6].filter(Boolean).join(' · ') || '—'}</span></div></div>)}</div>
        </Card>
      )}
    </div>
  );
}

function ContainerLogView({ c }: { c: ContainerListItem }) {
  const visible = useVisible();
  const [tail, setTail] = useState<'200' | '1000'>('200');
  const [follow, setFollow] = useState(true);
  const [find, setFind] = useState('');
  const box = useRef<HTMLPreElement>(null);
  const atBottom = useRef(true);
  const q = useQuery({
    queryKey: ['containers', 'logs', ref(c), tail],
    queryFn: ({ signal }) => get<ContainerLogs>(`/nodes/${enc(c.node_id)}/engines/${enc(c.engine_id)}/containers/${enc(c.container_id)}/logs`, { signal, query: { tail } }),
    refetchInterval: follow && visible ? 2000 : false,
    placeholderData: (prev) => prev,
  });
  const text = useMemo(() => stripAnsi(q.data?.text ?? ''), [q.data?.text]);
  useEffect(() => {
    const el = box.current;
    if (el && follow && atBottom.current) el.scrollTop = el.scrollHeight;
  }, [text, follow]);
  const content = useMemo(() => {
    if (!find.trim()) return text;
    const needle = find.trim();
    const parts = text.split(needle);
    return parts.flatMap((p, i) => (i === 0 ? [p] : [<mark key={i}>{needle}</mark>, p]));
  }, [text, find]);
  return (
    <div className="stack" style={{ flex: 1, minHeight: 0 }}>
      <div className="toolbar">
        <SearchInput className="search" value={find} onChange={setFind} placeholder="在已加载日志中查找" />
        <Segmented size="sm" value={tail} onChange={setTail} options={[{ value: '200', label: '200 行' }, { value: '1000', label: '1000 行' }]} />
        <span className="spacer" />
        <Button size="sm" variant={follow ? 'primary' : 'default'} icon={<ArrowDownToLine />} onClick={() => setFollow((v) => !v)}>{follow ? '跟随中' : '跟随'}</Button>
        <IconButton size="sm" label="刷新" onClick={() => void q.refetch()}>{q.isFetching ? <Spinner size={14} /> : <RefreshCw />}</IconButton>
      </div>
      {q.isError && <ErrorNotice error={q.error} retry={() => void q.refetch()} title="日志读取失败" />}
      {q.data?.truncated && <span className="muted" style={{ fontSize: 12 }}>仅显示最近部分日志（已截断）</span>}
      <pre ref={box} className="logs-view" style={{ height: 'calc(100vh - 330px)' }} onScroll={(e) => { const el = e.currentTarget; atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40; }}>
        {q.isPending ? '正在读取日志…' : text ? content : <span style={{ opacity: 0.5 }}>暂无日志</span>}
      </pre>
    </div>
  );
}

// ── Compose ─────────────────────────────────────────
function ComposeList({ node, setNode, nodes }: { node: string; setNode: (v?: string) => void; nodes: { id: string; name: string }[] }) {
  const q = useCompose(node || undefined);
  const [search, setSearch] = useState('');
  const items = (q.data?.items ?? []).filter((p) => { const s = search.trim().toLowerCase(); return !s || `${p.name} ${p.directory} ${p.node_name}`.toLowerCase().includes(s); });
  return (
    <>
      <div className="toolbar">
        <SearchInput className="search" value={search} onChange={setSearch} placeholder="项目名称或目录" />
        <span className="spacer" />
        <NodeSelect value={node} onChange={setNode} nodes={nodes} />
      </div>
      {q.isError && <ErrorNotice error={q.error} retry={() => void q.refetch()} />}
      {q.isPending ? <Card pad><Skeleton height={80} /></Card> : items.length === 0 ? (
        <Card><Empty icon={<Layers />} title="没有 Compose 项目">节点上通过 docker compose 启动的项目会自动出现在这里。</Empty></Card>
      ) : (
        <motion.div className="db-grid" initial="hidden" animate="show" variants={{ hidden: {}, show: { transition: { staggerChildren: 0.04 } } }}>
          {items.map((p) => <ComposeCard key={`${p.node_id}/${p.engine_id}/${p.id}`} p={p} />)}
        </motion.div>
      )}
    </>
  );
}

function ComposeCard({ p }: { p: ComposeProjectItem }) {
  const base = `/nodes/${enc(p.node_id)}/engines/${enc(p.engine_id)}/compose-projects/${enc(p.id)}`;
  const active = useActiveOp('compose', p.id);
  const action = useCommand<'pull' | 'start' | 'stop' | 'recreate' | 'update', Operation>({
    label: (a) => ({ pull: '拉取项目镜像', start: '启动项目', stop: '停止项目', recreate: '重建项目', update: '更新项目' })[a],
    node: p.node_id,
    run: async (a, key) => (await call<Operation>('POST', `${base}/actions`, { key, body: { action: a, expected_revision: p.revision } })).data,
  });
  const register = useCommand<void, Operation>({
    label: '登记 Compose 项目',
    node: p.node_id,
    run: async (_v, key) => (await call<Operation>('PUT', `${base}/registration`, { key, ifNoneMatch: '*' })).data,
  });
  const running = p.services.filter((s) => s.state === 'running').length;
  const busy = action.busy || !!active;
  const run = async (a: 'pull' | 'start' | 'stop' | 'recreate' | 'update') => {
    if (a !== 'pull' && a !== 'start' && !(await confirm({ title: { stop: '停止项目？', recreate: '重建项目？', update: '拉取并更新项目？' }[a], body: '操作会影响项目中的全部服务。', danger: a === 'stop', confirmLabel: '继续' }))) return;
    void action.run(a);
  };
  return (
    <motion.div variants={{ hidden: { opacity: 0, y: 8 }, show: { opacity: 1, y: 0 } }}>
      <Card className="db-card" style={{ cursor: 'default' }}>
        <div className="row-3">
          <span className="avatar"><Layers /></span>
          <div className="col grow" style={{ gap: 0 }}>
            <strong className="ellipsis" style={{ fontWeight: 600 }}>{p.name}</strong>
            <span className="muted mono ellipsis" style={{ fontSize: 12 }}>{p.node_name} · {p.directory}</span>
          </div>
          {active ? <Badge tone="info"><Spinner size={12} />执行中</Badge> : p.registered ? <Badge tone="ok"><PackageCheck width={12} height={12} />已登记</Badge> : <Badge>未登记</Badge>}
        </div>
        <div className="db-members">
          {p.services.map((s) => <span key={s.container_id} className="db-member"><Dot tone={containerTone(s.state, s.health)} />{s.service || s.name}</span>)}
        </div>
        <div className="row muted" style={{ fontSize: 12.5 }}><span>{running}/{p.services.length} 运行中</span><span>·</span><span>{p.files.join(', ')}</span></div>
        {p.read_only_reason && <span className="muted" style={{ fontSize: 12.5 }}><Info width={12} height={12} /> {p.read_only_reason}</span>}
        <div className="row wrap">
          {!p.registered ? (
            <Button size="sm" variant="primary" busy={register.busy} onClick={() => void register.run()}>登记后管理</Button>
          ) : (
            <>
              <Button size="sm" icon={<Download />} disabled={busy} onClick={() => void run('pull')}>拉取</Button>
              {running < p.services.length && <Button size="sm" icon={<Play />} disabled={busy} onClick={() => void run('start')}>启动</Button>}
              {running > 0 && <Button size="sm" icon={<Square />} disabled={busy} onClick={() => void run('stop')}>停止</Button>}
              <Menu trigger={(t) => <IconButton size="sm" label="更多" {...t}><Ellipsis /></IconButton>} items={[
                { label: '重建', icon: <RotateCw />, disabled: busy, onSelect: () => void run('recreate') },
                { label: '拉取并更新', icon: <Download />, disabled: busy, onSelect: () => void run('update') },
              ]} />
            </>
          )}
        </div>
      </Card>
    </motion.div>
  );
}
