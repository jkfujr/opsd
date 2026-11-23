import { useEffect, useMemo, useRef, useState } from 'react';
import { motion } from 'motion/react';
import { useQuery } from '@tanstack/react-query';
import { CalendarClock, History, Ellipsis, Pencil, Play, Plus, ChevronLeft } from 'lucide-react';
import { call, enc, get, getVersioned, newKey } from '../api/http';
import type { Operation, ScheduledTaskDefinition, ScheduledTaskMetadata, ScheduledTaskRuns } from '../api/types';
import { useNodes, useTasks } from '../app/queries';
import { useCommand } from '../app/commands';
import { confirm } from '../app/confirm';
import { useSession } from '../app/session';
import { ago, duration, fullTime } from '../lib/format';
import { operationStateLabel, operationTone } from '../lib/status';
import { Badge, Button, Card, Dot, Empty, ErrorNotice, Field, IconButton, Input, Menu, Notice, PageHead, SearchInput, Select, Sheet, Skeleton, Switch, Tag } from '../ui/kit';
import { OperationLogs } from '../ui/OperationLogs';

export function Tasks() {
  const q = useTasks();
  const [search, setSearch] = useState('');
  const [editing, setEditing] = useState<ScheduledTaskMetadata | 'new' | null>(null);
  const [runs, setRuns] = useState<ScheduledTaskMetadata | null>(null);
  const items = (q.data?.items ?? []).filter((t) => { const s = search.trim().toLowerCase(); return !s || `${t.name} ${t.schedule} ${t.node_name ?? ''}`.toLowerCase().includes(s); });
  return (
    <div className="page">
      <PageHead title="定时任务" sub="按五字段 cron 与时区在目标节点以 root 执行；错过的计划不会补跑，同一任务不会重入。" actions={<Button variant="primary" icon={<Plus />} onClick={() => setEditing('new')}>新建任务</Button>} />
      <div className="toolbar"><SearchInput className="search" value={search} onChange={setSearch} placeholder="名称、计划或节点" /></div>
      {!!q.data?.unavailable.length && <Notice tone="warn" title="部分节点无法读取">{q.data.unavailable.map((u) => `${u.node_name}：${u.message}`).join('；')}</Notice>}
      {q.isError && <ErrorNotice error={q.error} retry={() => void q.refetch()} />}
      <Card>
        {q.isPending ? <div className="stack" style={{ padding: 20 }}>{Array.from({ length: 4 }, (_, i) => <Skeleton key={i} height={40} />)}</div> : items.length === 0 ? (
          <Empty icon={<CalendarClock />} title="还没有定时任务" action={<Button variant="primary" icon={<Plus />} onClick={() => setEditing('new')}>新建任务</Button>}>适合日志清理、证书续期、备份触发等周期性工作。</Empty>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead><tr><th>任务</th><th>计划</th><th className="hide-sm">下次执行</th><th>状态</th><th className="actions-cell" /></tr></thead>
              <tbody>{items.map((t) => <TaskRow key={`${t.node_id}/${t.id}`} t={t} onEdit={() => setEditing(t)} onRuns={() => setRuns(t)} />)}</tbody>
            </table>
          </div>
        )}
      </Card>
      <TaskEditor open={editing !== null} task={editing === 'new' ? undefined : editing ?? undefined} onClose={() => setEditing(null)} />
      <RunsSheet task={runs} onClose={() => setRuns(null)} />
    </div>
  );
}

function TaskRow({ t, onEdit, onRuns }: { t: ScheduledTaskMetadata; onEdit: () => void; onRuns: () => void }) {
  const run = useCommand<void, Operation>({
    label: `执行任务 ${t.name}`,
    node: t.node_id ?? '',
    run: async (_v, key) => (await call<Operation>('POST', `/nodes/${enc(t.node_id!)}/tasks/${enc(t.id)}/executions`, { key, body: { expected_revision: t.revision } })).data,
  });
  return (
    <tr>
      <td>
        <div className="col" style={{ gap: 0 }}>
          <strong style={{ fontWeight: 580 }}>{t.name}</strong>
          <span className="muted" style={{ fontSize: 12 }}>{t.node_name} · <span className="mono">{t.directory}</span></span>
        </div>
      </td>
      <td><div className="col" style={{ gap: 2 }}><span className="mono" style={{ fontSize: 13 }}>{t.schedule}</span><span className="dim" style={{ fontSize: 11.5 }}>{describeCron(t.schedule)} · {t.timezone}</span></div></td>
      <td className="hide-sm">{t.enabled && t.next_at ? <span title={fullTime(t.next_at)}>{ago(t.next_at)}</span> : <span className="dim">—</span>}</td>
      <td>{t.enabled ? <Badge tone="ok" dot>启用</Badge> : <Badge dot>停用</Badge>}</td>
      <td className="actions-cell">
        <div className="row-actions" style={{ opacity: 1 }}>
          <IconButton size="sm" label="立即执行" disabled={run.busy} onClick={async () => { if (await confirm({ title: `立即执行 ${t.name}？`, body: `脚本将以 root 在 ${t.node_name} 上运行。`, confirmLabel: '执行' })) void run.run(); }}><Play /></IconButton>
          <Menu trigger={(p) => <IconButton size="sm" label="更多" {...p}><Ellipsis /></IconButton>} items={[
            { label: '编辑', icon: <Pencil />, onSelect: onEdit },
            { label: '执行记录', icon: <History />, onSelect: onRuns },
          ]} />
        </div>
      </td>
    </tr>
  );
}

const presets = [
  { label: '每 5 分钟', value: '*/5 * * * *' },
  { label: '每小时', value: '0 * * * *' },
  { label: '每天 03:00', value: '0 3 * * *' },
  { label: '每周一 04:00', value: '0 4 * * 1' },
  { label: '每月 1 日', value: '0 5 1 * *' },
];

export function describeCron(expr: string) {
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) return '格式无效';
  const [min, hour, dom, mon, dow] = parts as [string, string, string, string, string];
  const week = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
  const at = (h: string, m: string) => `${h.padStart(2, '0')}:${m.padStart(2, '0')}`;
  if (min.startsWith('*/') && hour === '*' && dom === '*' && mon === '*' && dow === '*') return `每 ${min.slice(2)} 分钟`;
  if (/^\d+$/.test(min) && hour === '*' && dom === '*' && mon === '*' && dow === '*') return `每小时第 ${min} 分`;
  if (/^\d+$/.test(min) && hour.startsWith('*/') && dom === '*' && mon === '*' && dow === '*') return `每 ${hour.slice(2)} 小时`;
  if (/^\d+$/.test(min) && /^\d+$/.test(hour)) {
    if (dom === '*' && mon === '*' && dow === '*') return `每天 ${at(hour, min)}`;
    if (dom === '*' && mon === '*' && /^[0-7](,[0-7])*$/.test(dow)) return `每${dow.split(',').map((d) => week[Number(d) % 7]).join('、')} ${at(hour, min)}`;
    if (/^\d+$/.test(dom) && mon === '*' && dow === '*') return `每月 ${dom} 日 ${at(hour, min)}`;
  }
  return '自定义计划';
}

const blank: ScheduledTaskDefinition = { name: '', schedule: '0 3 * * *', timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai', script: '#!/bin/bash\nset -euo pipefail\n\n', directory: '/root', enabled: true };

function TaskEditor({ open, task, onClose }: { open: boolean; task?: ScheduledTaskMetadata; onClose: () => void }) {
  const session = useSession();
  const nodes = useNodes();
  const [node, setNode] = useState(session.local_node_id);
  const [taskId, setTaskId] = useState('');
  const [def, setDef] = useState<ScheduledTaskDefinition>(blank);
  const existing = useQuery({
    queryKey: ['tasks', 'definition', task?.node_id, task?.id],
    queryFn: ({ signal }) => getVersioned<ScheduledTaskDefinition>(`/nodes/${enc(task!.node_id!)}/tasks/${enc(task!.id)}/definition`, { signal }),
    enabled: open && !!task,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: false,
  });
  useEffect(() => {
    if (!open) return;
    if (!task) { setDef(blank); setNode(session.local_node_id); setTaskId(`task-${newKey('x').slice(-8).toLowerCase().replace(/[^a-z0-9]/g, '0')}`); }
  }, [open, task, session.local_node_id]);
  // 只在打开时载入一次定义；之后的重读（如版本冲突）只更新提交条件，保留草稿。
  const loaded = useRef<string | null>(null);
  useEffect(() => { if (!open) loaded.current = null; }, [open]);
  useEffect(() => {
    if (open && task && existing.data && loaded.current !== task.id) { loaded.current = task.id; setDef(existing.data.value); }
  }, [open, task, existing.data]);
  const target = task?.node_id ?? node;
  const id = task?.id ?? taskId;
  const save = useCommand<void, Operation>({
    label: task ? '保存任务' : '创建任务',
    node: target,
    run: async (_v, key) => (await call<Operation>('PUT', `/nodes/${enc(target)}/tasks/${enc(id)}/definition`, { key, body: def, ...(task ? { ifMatch: existing.data?.etag } : { ifNoneMatch: '*' }) })).data,
    onSuccess: onClose,
    onConflict: () => void existing.refetch(),
  });
  const lines = useMemo(() => Math.max(12, def.script.split('\n').length), [def.script]);
  const set = <K extends keyof ScheduledTaskDefinition>(k: K, v: ScheduledTaskDefinition[K]) => setDef((d) => ({ ...d, [k]: v }));
  const valid = def.name.trim() && def.schedule.trim().split(/\s+/).length === 5 && def.directory.startsWith('/') && /^[a-z0-9][a-z0-9_-]{0,63}$/.test(id);
  return (
    <Sheet open={open} onClose={onClose} width={820} title={task ? `编辑 ${task.name}` : '新建定时任务'} sub={task ? `${task.node_name} · ${task.id}` : '脚本经 /bin/bash -lc 以 root 执行'}
      footer={<><Button variant="ghost" onClick={onClose}>取消</Button><Button variant="primary" busy={save.busy} disabled={!valid || (!!task && !existing.data)} onClick={() => void save.run()}>{task ? '保存' : '创建'}</Button></>}>
      {existing.isError && <ErrorNotice error={existing.error} retry={() => void existing.refetch()} title="无法读取任务定义（需要 tasks.manage 权限）" />}
      {task && !existing.data && !existing.isError ? <Skeleton height={300} /> : (
        <motion.div className="stack" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
          <div className="form-grid">
            <Field label="名称"><Input value={def.name} onChange={(e) => set('name', e.target.value)} autoFocus /></Field>
            {!task ? (
              <Field label="节点"><Select value={node} onChange={(e) => setNode(e.target.value)}>{(nodes.data?.items ?? []).filter((n) => !n.revoked_at).map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}</Select></Field>
            ) : <Field label="节点"><Input value={task.node_name ?? ''} disabled /></Field>}
            <Field label="计划" hint={describeCron(def.schedule)}><Input mono value={def.schedule} onChange={(e) => set('schedule', e.target.value)} /></Field>
            <Field label="时区" hint="IANA 名称"><Input mono value={def.timezone} onChange={(e) => set('timezone', e.target.value)} /></Field>
            <div className="span-2 cron-preview">{presets.map((p) => <button key={p.value} type="button" className="badge outline" style={{ cursor: 'pointer', height: 26 }} onClick={() => set('schedule', p.value)}>{p.label}</button>)}</div>
            <Field label="工作目录"><Input mono value={def.directory} onChange={(e) => set('directory', e.target.value)} /></Field>
            {!task ? <Field label="任务 ID" hint="小写字母、数字、- 或 _"><Input mono value={taskId} onChange={(e) => setTaskId(e.target.value)} /></Field> : <Field label="启用"><div style={{ height: 36, display: 'flex', alignItems: 'center' }}><Switch checked={def.enabled} onChange={(v) => set('enabled', v)} label="启用" /></div></Field>}
            {!task && <Field label="启用"><div style={{ height: 36, display: 'flex', alignItems: 'center' }}><Switch checked={def.enabled} onChange={(v) => set('enabled', v)} label="启用" /></div></Field>}
          </div>
          <Field label="脚本">
            <div className="code-editor">
              <div className="gutter" aria-hidden>{Array.from({ length: lines }, (_, i) => i + 1).join('\n')}</div>
              <textarea value={def.script} rows={lines} spellCheck={false} onChange={(e) => set('script', e.target.value)} onKeyDown={(e) => {
                if (e.key === 'Tab') {
                  e.preventDefault();
                  const el = e.currentTarget, s = el.selectionStart, end = el.selectionEnd;
                  const next = def.script.slice(0, s) + '  ' + def.script.slice(end);
                  set('script', next);
                  requestAnimationFrame(() => { el.selectionStart = el.selectionEnd = s + 2; });
                }
              }} />
            </div>
          </Field>
          <Notice tone="warn">任务以 root 身份执行任意命令；每个节点最多同时运行 8 个任务，日志最多保存 16 MiB。</Notice>
        </motion.div>
      )}
    </Sheet>
  );
}

function RunsSheet({ task, onClose }: { task: ScheduledTaskMetadata | null; onClose: () => void }) {
  const [selected, setSelected] = useState<Operation | null>(null);
  // 开始翻页即冻结当时的首页与游标：轮询继续，但不再改写已展示的页边界，
  // 否则新记录会把最后一条挤出首页、又不落在更早页里，导致漏行。
  const [frozen, setFrozen] = useState<{ items: Operation[]; ids: string[]; next?: string } | null>(null);
  const [older, setOlder] = useState<Operation[]>([]);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [paging, setPaging] = useState<{ busy: boolean; error: unknown }>({ busy: false, error: null });
  const abort = useRef<AbortController | null>(null);
  const key = task ? `${task.node_id}/${task.id}` : '';
  const keyRef = useRef(key);
  useEffect(() => {
    keyRef.current = task ? `${task.node_id}/${task.id}` : '';
  }, [task?.node_id, task?.id]);
  const q = useQuery({
    queryKey: ['tasks', 'runs', task?.node_id, task?.id],
    queryFn: ({ signal }) => get<ScheduledTaskRuns>(`/nodes/${enc(task!.node_id!)}/tasks/${enc(task!.id!)}/executions`, { signal, query: { limit: 50 } }),
    enabled: !!task,
    refetchInterval: 5000,
  });
  // 切换任务或关闭面板：取消在途翻页，丢弃其结果与游标。
  useEffect(() => {
    setSelected(null);
    setFrozen(null);
    setOlder([]);
    setCursor(undefined);
    setPaging({ busy: false, error: null });
    abort.current?.abort();
    abort.current = null;
  }, [task?.node_id, task?.id]);
  const latest = () => {
    abort.current?.abort();
    abort.current = null;
    setFrozen(null);
    setOlder([]);
    setCursor(undefined);
    setPaging({ busy: false, error: null });
  };
  // 按 ID 取最新状态，同时保证被挤出首页的记录不丢：
  // 冻结快照留底 → 更早页 → 轮询首页（最新状态覆盖前两者）。
  const byId = useMemo(() => {
    const map = new Map<string, Operation>();
    for (const o of frozen?.items ?? []) map.set(o.id, o);
    for (const o of older) map.set(o.id, o);
    for (const o of q.data?.items ?? []) map.set(o.id, o);
    return map;
  }, [frozen, older, q.data]);
  // 冻结只影响"显示哪些、按什么顺序"，记录本身仍跟随轮询更新状态。
  const items = useMemo(() => {
    if (!frozen) return q.data?.items ?? [];
    const seen = new Set<string>();
    return frozen.ids
      .map((id) => byId.get(id))
      .filter((o): o is Operation => !!o && !seen.has(o.id) && !!seen.add(o.id));
  }, [frozen, byId]);
  const current = selected ? byId.get(selected.id) ?? selected : null;
  // 冻结后只读 cursor：末页返回空游标即隐藏入口，不得退回最初游标重复请求。
  const more = frozen ? cursor : q.data?.next_cursor;
  const loadOlder = async () => {
    if (!task || !more || paging.busy) return;
    const controller = new AbortController();
    abort.current?.abort();
    abort.current = controller;
    const startedAt = key;
    // 点击即固定边界：保留快照对象（防止被轮询挤出首页后找不到），
    // 并把初始游标写入 cursor，冻结期间只读 cursor，末页返回空游标即关闭入口。
    if (!frozen) {
      const snapshot = q.data?.items ?? [];
      setFrozen({ items: snapshot, ids: snapshot.map((o) => o.id), next: more });
      setCursor(more);
    }
    setPaging({ busy: true, error: null });
    try {
      const page = await get<ScheduledTaskRuns>(`/nodes/${enc(task.node_id!)}/tasks/${enc(task.id)}/executions`, { signal: controller.signal, query: { limit: 50, cursor: more } });
      // 迟到的应答不得写进另一个任务，也不得覆盖新一轮请求的结果。
      if (controller.signal.aborted || abort.current !== controller || keyRef.current !== startedAt) return;
      setOlder((list) => [...list, ...page.items]);
      setFrozen((f) => (f ? { ...f, ids: [...f.ids, ...page.items.map((o) => o.id)] } : f));
      // 末页返回空游标时 cursor 变空，按钮随之隐藏，不会退回首页游标。
      setCursor(page.next_cursor);
      setPaging({ busy: false, error: null });
    } catch (reason) {
      if (controller.signal.aborted) return;
      // 游标绑定任务与授权范围；失效时提示并保留重试入口，不静默停在旧位置。
      setPaging({ busy: false, error: reason });
    }
  };
  return (
    <Sheet open={!!task} onClose={onClose} width={860} title={task ? `${task.name} · 执行记录` : ''} sub={task?.node_name} actions={<>
        {current && <Button size="sm" variant="ghost" icon={<ChevronLeft />} onClick={() => setSelected(null)}>返回列表</Button>}
        {!current && frozen && <Button size="sm" variant="ghost" onClick={latest}>返回最新</Button>}
      </>}>
      {q.isError && <ErrorNotice error={q.error} retry={() => void q.refetch()} />}
      {!current && !!paging.error && <ErrorNotice error={paging.error} retry={() => void loadOlder()} />}
      {current ? (
        <motion.div className="stack" key={current.id} initial={{ opacity: 0, x: 12 }} animate={{ opacity: 1, x: 0 }}>
          <div className="row wrap"><Badge tone={operationTone(current.state)} dot>{operationStateLabel[current.state]}</Badge><span className="muted">{fullTime(current.created_at)}</span><span className="muted">耗时 {duration((Date.parse(current.updated_at) - Date.parse(current.created_at)) / 1000)}</span>{current.actor && <Tag>{current.actor}</Tag>}</div>
          {current.error && <Notice tone="bad">{current.error.message}</Notice>}
          <OperationLogs op={current} height="calc(100vh - 300px)" />
        </motion.div>
      ) : !q.data ? <Skeleton height={200} /> : items.length === 0 ? <Empty icon={<History />} title="还没有执行记录" /> : (
        <Card>
          <div className="list">
            {items.map((o) => (
              <button key={o.id} type="button" className="list-item clickable" style={{ width: '100%', textAlign: 'left' }} onClick={() => setSelected(o)}>
                <Dot tone={operationTone(o.state)} live={o.state === 'applying'} />
                <div className="li-main">
                  <span className="li-title">{fullTime(o.created_at)}</span>
                  <span className="li-sub">{o.actor?.startsWith('task:') || !o.actor ? '计划触发' : `手动 · ${o.actor}`} · 耗时 {duration((Date.parse(o.updated_at) - Date.parse(o.created_at)) / 1000)}</span>
                </div>
                <Badge tone={operationTone(o.state)}>{operationStateLabel[o.state]}</Badge>
              </button>
            ))}
          </div>
          {more && (
            <div className="row" style={{ justifyContent: "center", paddingTop: 12 }}>
              <Button size="sm" variant="ghost" busy={paging.busy} onClick={() => void loadOlder()}>更早记录</Button>
            </div>
          )}
        </Card>
      )}
    </Sheet>
  );
}
