import { useState } from 'react';
import { motion } from 'motion/react';
import { useQuery } from '@tanstack/react-query';
import { ListChecks, RefreshCw } from 'lucide-react';
import { enc, get } from '../api/http';
import type { Operation } from '../api/types';
import { useNodes, useOperationsHistory } from '../app/queries';
import { decide } from '../app/ops';
import { confirm } from '../app/confirm';
import { toast } from '../app/toast';
import { errorText } from '../api/http';
import { ago, duration, fullTime } from '../lib/format';
import { isTerminal, operationStateLabel, operationTitle, operationTone, stepStateTone } from '../lib/status';
import { Badge, Button, Card, CardHead, CopyText, Dot, Empty, ErrorNotice, IconButton, Notice, PageHead, Segmented, Select, Sheet, Skeleton, Spinner, Tag } from '../ui/kit';
import { OperationLogs } from '../ui/OperationLogs';

const modules = [
  { value: '', label: '全部' }, { value: 'containers', label: '容器' }, { value: 'firewall', label: '防火墙' }, { value: 'databases', label: '数据库' },
  { value: 'tasks', label: '任务' }, { value: 'compose', label: 'Compose' }, { value: 'nodes', label: '节点' },
] as const;

export function Operations() {
  const nodes = useNodes();
  const [module, setModule] = useState<(typeof modules)[number]['value']>('');
  const [node, setNode] = useState('');
  const [open, setOpen] = useState<string | null>(null);
  const q = useOperationsHistory({ module: module || undefined, node_id: node || undefined });
  const names = new Map((nodes.data?.items ?? []).map((n) => [n.id, n.name]));
  return (
    <div className="page">
      <PageHead title="操作记录" sub="所有持久受理的操作，按受理时间倒序；结果未知时只查询原记录，不会重放。" />
      <div className="toolbar">
        <Segmented value={module} onChange={setModule} options={modules.map((m) => ({ value: m.value, label: m.label }))} />
        <span className="spacer" />
        <Select value={node} onChange={(e) => setNode(e.target.value)} style={{ width: 200 }}><option value="">全部节点</option>{(nodes.data?.items ?? []).map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}</Select>
        <IconButton label="刷新" onClick={() => void q.refetch()}>{q.isFetching ? <Spinner /> : <RefreshCw />}</IconButton>
      </div>
      {!!q.data?.unavailable?.length && <Notice tone="warn">{q.data.unavailable.length} 个节点的记录暂时无法读取</Notice>}
      {q.isError && <ErrorNotice error={q.error} retry={() => void q.refetch()} />}
      <Card>
        {q.isPending ? <div className="stack" style={{ padding: 20 }}>{Array.from({ length: 6 }, (_, i) => <Skeleton key={i} height={40} />)}</div> : !q.data?.items.length ? <Empty icon={<ListChecks />} title="没有操作记录" /> : (
          <motion.div className="list" initial="hidden" animate="show" variants={{ hidden: {}, show: { transition: { staggerChildren: 0.02 } } }}>
            {q.data.items.map((o) => (
              <motion.button key={o.id} type="button" className="list-item clickable" style={{ width: '100%', textAlign: 'left' }} onClick={() => setOpen(o.id)} variants={{ hidden: { opacity: 0, y: 4 }, show: { opacity: 1, y: 0 } }}>
                <Dot tone={operationTone(o.state)} live={!isTerminal(o)} />
                <div className="li-main">
                  <span className="li-title">{operationTitle(o)}{o.target_name && <span className="muted" style={{ fontWeight: 400 }}> · {o.target_name}</span>}</span>
                  <span className="li-sub">{names.get(o.node_id) ?? o.node_id.slice(0, 10)} · {ago(o.created_at)}{o.actor ? ` · ${o.actor}` : ''}</span>
                </div>
                {o.result_pending_sync && <Tag>待同步</Tag>}
                <Badge tone={operationTone(o.state)}>{operationStateLabel[o.state]}</Badge>
              </motion.button>
            ))}
          </motion.div>
        )}
      </Card>
      <OperationSheet id={open} onClose={() => setOpen(null)} names={names} />
    </div>
  );
}

function OperationSheet({ id, onClose, names }: { id: string | null; onClose: () => void; names: Map<string, string> }) {
  const q = useQuery({ queryKey: ['operations', 'detail', id], queryFn: ({ signal }) => get<Operation>(`/operations/${enc(id!)}`, { signal }), enabled: !!id, refetchInterval: (query) => (query.state.data && !isTerminal(query.state.data) ? 2000 : false) });
  const o = q.data;
  const [busy, setBusy] = useState<string | null>(null);
  const act = async (action: 'confirm' | 'cancel' | 'recover' | 'resolve', title: string, body: string) => {
    if (!o || !(await confirm({ title, body, confirmLabel: '继续', danger: action === 'cancel' }))) return;
    setBusy(action);
    try { await decide(o, action); void q.refetch(); } catch (e) { toast(errorText(e), 'bad'); } finally { setBusy(null); }
  };
  return (
    <Sheet open={!!id} onClose={onClose} width={760} title={o ? operationTitle(o) : '操作'} sub={o ? `${names.get(o.node_id) ?? o.node_id} · ${fullTime(o.created_at)}` : undefined} icon={o && <Dot tone={operationTone(o.state)} live={!isTerminal(o)} />}
      footer={o && !isTerminal(o) ? (
        <>
          {o.state === 'awaiting_confirmation' && <Button variant="primary" busy={busy === 'confirm'} onClick={() => void act('confirm', '确认保留此变更？', '确认后将不再自动恢复。')}>确认保留</Button>}
          {o.state === 'attention_required' && <><Button busy={busy === 'recover'} onClick={() => void act('recover', '重新处理？', '只继续尚未完成的步骤。')}>重新处理</Button><Button busy={busy === 'resolve'} onClick={() => void act('resolve', '接受当前状态？', '操作以“已人工处理”结束。')}>接受现状</Button></>}
          {(o.state === 'accepted' || o.state === 'prepared' || (o.subject === 'task_run' && o.state === 'applying')) && <Button variant="danger" busy={busy === 'cancel'} onClick={() => void act('cancel', '取消操作？', '尚未完成的部分不会继续执行。')}>取消</Button>}
        </>
      ) : undefined}>
      {q.isError && <ErrorNotice error={q.error} retry={() => void q.refetch()} />}
      {!o ? <Skeleton height={200} /> : (
        <>
          <div className="row wrap">
            <Badge tone={operationTone(o.state)} dot>{operationStateLabel[o.state]}</Badge>
            {o.target_name && <Tag>{o.target_name}</Tag>}
            <span className="muted">耗时 {duration((Date.parse(o.updated_at) - Date.parse(o.created_at)) / 1000)}</span>
          </div>
          {o.error && <Notice tone="bad" title={o.error.code}>{o.error.message}</Notice>}
          {o.waiting_reason && <Notice tone="info">{o.waiting_reason}</Notice>}
          {o.steps.length > 0 && (
            <Card>
              <CardHead title="步骤" />
              <div className="list">{o.steps.map((s, i) => (
                <div key={i} className="list-item">
                  <Dot tone={stepStateTone(s.state)} live={s.state === 'applying' || s.state === 'intent'} />
                  <div className="li-main"><span className="li-title">{s.name}</span>{s.message && <span className="li-sub">{s.message}</span>}</div>
                  {s.observed_at && <span className="dim" style={{ fontSize: 12 }}>{ago(s.observed_at)}</span>}
                </div>
              ))}</div>
            </Card>
          )}
          {o.supports_logs && <Card><CardHead title="执行日志" /><div className="card-body"><OperationLogs op={o} height={320} /></div></Card>}
          <details>
            <summary className="muted" style={{ cursor: 'pointer', fontSize: 13 }}>技术详情</summary>
            <dl className="kv" style={{ marginTop: 12, fontSize: 13 }}>
              <dt>操作 ID</dt><dd><CopyText value={o.id} /></dd>
              <dt>版本</dt><dd className="num">{o.revision}</dd>
              {o.idempotency_key && <><dt>请求键</dt><dd><CopyText value={o.idempotency_key} /></dd></>}
              {o.resource && <><dt>资源</dt><dd className="mono">{o.resource.kind} · {o.resource.id}</dd></>}
              {o.queue_sequence !== undefined && <><dt>队列序号</dt><dd className="num">{o.queue_sequence}</dd></>}
              {o.coordinator && <><dt>协调节点</dt><dd className="mono">{o.coordinator}</dd></>}
              {o.actor && <><dt>发起者</dt><dd className="mono">{o.actor}</dd></>}
              <dt>决定</dt><dd>{o.decision}</dd>
            </dl>
          </details>
        </>
      )}
    </Sheet>
  );
}
