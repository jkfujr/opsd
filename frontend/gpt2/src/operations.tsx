import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { History, ArrowLeft } from 'lucide-react';
import { api, targetNode, allPages, unwrap, unknownResult, readResourceReference } from './api/client';
import type { components } from './api/types';
import { forgetRequest, requestName, usePendingRequests, type PendingRequest } from './api/requests';
import { queryClient } from './state';
import { Btn, Choice, Copy, Empty, IconButton, Sheet, Status } from './ui';
import { Confirm, date, Failure, newRequestKey } from './business-ui';
import { operationSubjectNames, operationTarget } from './operation-summary';

type Operation = components['schemas']['Operation'];
export const operationStates: Record<Operation['state'], string> = { accepted: '已受理', prepared: '准备执行', applying: '应用中', awaiting_confirmation: '待确认', reverting: '撤销中', attention_required: '需处理', succeeded: '已成功', rejected: '未执行', failed: '未完成且无残留', cancelled: '已取消', reverted: '已撤销', resolved_manually: '已人工处理' };
const pending = new Set<Operation['state']>(['accepted', 'prepared', 'applying', 'awaiting_confirmation', 'reverting', 'attention_required']);
const Context = createContext<{ show: (id?: string) => void; open: boolean; count: number; requestCount: number; items: Operation[]; nodeName: (id: string) => string } | null>(null);
export function useOperations() { const value = useContext(Context); if (!value) throw new Error('操作记录尚未初始化'); return value; }
export function invalidateOperations() { void queryClient.invalidateQueries({ queryKey: ['operations'] }); void queryClient.invalidateQueries({ queryKey: ['overview'] }); }
export function OperationsProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false), [selected, setSelected] = useState('');
  const [node, setNode] = useState(''), [state, setState] = useState(''), [cursor, setCursor] = useState('');
  const requests = usePendingRequests();
  const active = useQuery({ queryKey: ['operations', 'pending'], queryFn: async ({ signal }) => allPages(cursor => api.GET('/operations', { signal, params: { query: { pending: true, cursor, limit: 100 } } }).then(unwrap)), refetchInterval: 3000 });
  const history = useQuery({ queryKey: ['operations', 'history', node, state, cursor], enabled: open && !selected, queryFn: async ({ signal }) => unwrap(await api.GET('/operations', { signal, params: { query: { node_id: node || undefined, state: (state || undefined) as Operation['state'] | undefined, cursor: cursor || undefined, limit: 50 } } })) });
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: async ({ signal }) => allPages(cursor => api.GET('/nodes', { signal, params: { query: { cursor, limit: 100 } } }).then(unwrap)) });
  const show = (id?: string) => { if (id !== undefined) setSelected(id); setOpen(true); };
  return <Context value={{ show, open, requestCount: requests.length, nodeName: id => nodes.data?.items.find(node => node.id === id)?.name ?? id, items: active.data?.items ?? [], count: (active.data?.items.length ?? 0) + requests.length }}>{children}<Sheet open={open} onClose={() => setOpen(false)} title="操作记录">
    {selected ? <><IconButton icon={ArrowLeft} label="返回操作列表" onClick={() => setSelected('')}/><OperationDetail id={selected}/></> : <div className="operation-list">
      {requests.length > 0 && <section className="request-recovery"><h2>结果待核对</h2>{requests.map(request => <RequestRecovery key={request.key} request={request} onOperation={show}/>)}</section>}
      <div className="filter-group"><Choice resource label="操作节点" value={node} onChange={value => { setNode(value); setCursor(''); }} options={[[ '', '全部节点' ], ...(nodes.data?.items.map(item => [item.id, item.name] as const) ?? [])]}/><Choice label="操作状态" value={state} onChange={value => { setState(value); setCursor(''); }} options={[[ '', '全部状态' ], ...Object.entries(operationStates)]}/></div>
      <Failure error={history.error} retry={() => void history.refetch()}/>
      {history.isPending && <p role="status">正在读取操作…</p>}
      {!!history.data?.items.length && <div className="operation-rows">{history.data.items.map(operation => { const target = operationTarget(operation, queryClient); return <button type="button" className="operation-row" data-operation-id={operation.id} key={operation.id} onClick={() => setSelected(operation.id)}><span className="operation-row-heading"><strong>{operationSubjectNames[operation.subject]}</strong><Status tone={pending.has(operation.state) ? 'warning' : operation.state === 'succeeded' ? 'success' : ['failed', 'rejected'].includes(operation.state) ? 'error' : 'neutral'}>{operationStates[operation.state]}</Status></span>{target && <span className="operation-target">{target}</span>}<span className="operation-meta">{nodes.data?.items.find(item => item.id === operation.node_id)?.name ?? operation.node_id} · {date(operation.created_at)}</span></button>; })}</div>}
      {history.data?.items.length === 0 && <Empty title="暂无操作记录"/>}
      <div className="heading-actions">{cursor && <Btn onClick={() => setCursor('')}>返回最新</Btn>}{history.data?.next_cursor && <Btn onClick={() => setCursor(history.data!.next_cursor!)}>更早记录</Btn>}</div>
    </div>}
  </Sheet></Context>;
}
export function OperationsButton() { const value = useOperations(); return <div className="operations-control"><IconButton icon={History} label={value.count ? `操作记录，${value.count} 项待处理` : '操作记录'} onClick={() => value.show(value.requestCount ? '' : undefined)}/>{value.count > 0 && <span className="operation-count" aria-hidden>{value.count}</span>}</div>; }
function RequestRecovery({ request, onOperation }: { request: PendingRequest; onOperation: (id: string) => void }) {
  const read = useMutation({ mutationFn: async () => request.resource ? readResourceReference(request.path, request.method) : unwrap(await api.GET('/requests/{request_key}', { params: { path: { request_key: request.key }, query: { node_id: request.nodeID } } })), onSuccess: receipt => { if ('operation_id' in receipt && receipt.operation_id) { forgetRequest(request.key); onOperation(receipt.operation_id); } else if ('resource' in receipt || receipt.state === 'completed') void queryClient.invalidateQueries({ type: 'active' }); } });
  const receipt = read.data;
  const resourceURL = receipt && !('resource' in receipt) ? receipt.resource_url : undefined;
  const original = useMutation({ mutationFn: () => readResourceReference(resourceURL!), onSuccess: () => void queryClient.invalidateQueries({ type: 'active' }) });
  return <article className="operation-item"><strong>{requestName(request.path)}</strong><span className="object-meta">{request.nodeID} · {request.path}</span><Failure error={read.error ?? original.error}/>{receipt && <p>{'resource' in receipt ? receipt.removed ? '资源已删除。' : '已读取当前资源，请到原页面核对。' : receipt.secret_unavailable ? '原请求已生成秘密值，完整值无法再次读取；请到原页面重新生成或撤销。' : receipt.state === 'completed' ? '原请求已完成，请重新读取资源。' : '原请求尚未完成，请继续查询。'}</p>}{original.data && <p>已读取当前资源，请到原页面核对。</p>}<div className="heading-actions"><Btn disabled={read.isPending} onClick={() => read.mutate()}>{request.resource ? '读取原资源' : '查询原请求'}</Btn>{resourceURL && <Btn disabled={original.isPending} onClick={() => original.mutate()}>读取原资源</Btn>}{receipt && ('resource' in receipt || receipt.state === 'completed') && <Btn onClick={() => forgetRequest(request.key)}>已核对</Btn>}</div></article>;
}

export function OperationDetail({ id }: { id: string }) {
  const query = useQuery({ queryKey: ['operations', id], queryFn: async ({ signal }) => unwrap(await api.GET('/operations/{id}', { signal, params: { path: { id } } })), refetchInterval: query => query.state.data && pending.has(query.state.data.state) ? 1000 : false });
  const [resolve, setResolve] = useState(false), [cancel, setCancel] = useState(false);
  const keys = useRef<Record<string, string>>({});
  const mutation = useMutation({ mutationFn: async (action: 'confirm' | 'recover' | 'resolve' | 'cancel') => { keys.current[action] ||= newRequestKey(); return unwrap(await api.POST('/operations/{id}/actions', { ...targetNode(query.data!.node_id), params: { path: { id }, header: { 'Idempotency-Key': keys.current[action] } }, body: { action, expected_revision: query.data!.revision, ...(action === 'resolve' ? { accept_current: true } : {}) } })); }, onSuccess: () => { keys.current = {}; setResolve(false); setCancel(false); invalidateOperations(); void queryClient.invalidateQueries({ type: 'active' }); }, onError: (error, action) => { if (!unknownResult(error)) delete keys.current[action]; void query.refetch(); } });
  const context = useOperations();
  const locate = useMutation({ mutationFn: async () => unwrap(await api.GET('/requests/{request_key}', { params: { path: { request_key: keys.current[mutation.variables ?? ''] }, query: { node_id: query.data!.node_id } } })), onSuccess: receipt => { void query.refetch(); if (receipt.state === 'completed') { mutation.reset(); keys.current = {}; } } });
  const operation = query.data;
  if (!operation) return <Failure error={query.error} retry={() => void query.refetch()}/>;
  const locked = query.isError || mutation.isPending || operation.result_pending_sync || (mutation.isError && unknownResult(mutation.error));
  const steps: Record<string, string> = { pending: '待执行', intent: '准备发送', applying: '应用中', applied: '已读回', reverting: '撤销中', reverted: '已撤销', completed: '已完成', failed: '未完成', unknown: '待核对' };
  const target = operationTarget(operation, queryClient);
  return <article className="operation-detail"><div className="operation-detail-heading"><h2>{operationSubjectNames[operation.subject]}</h2><Status tone={pending.has(operation.state) ? 'warning' : operation.state === 'succeeded' ? 'success' : ['failed', 'rejected'].includes(operation.state) ? 'error' : 'neutral'}>{operationStates[operation.state]}</Status></div><dl className="detail-fields">{target && <div><dt>目标</dt><dd>{target}</dd></div>}<div><dt>节点</dt><dd>{context.nodeName(operation.node_id)}</dd></div><div><dt>发生时间</dt><dd>{date(operation.created_at)}</dd></div><div><dt>更新时间</dt><dd>{date(operation.updated_at)}</dd></div></dl>{operation.result_pending_sync && <Status tone="warning">结果待同步</Status>}{operation.state === 'awaiting_confirmation' && <p>剩余 {operation.confirmation_remaining_seconds ?? 0} 秒</p>}{operation.error && <p role="alert" className="text-error">{operation.error.message}</p>}<ol>{operation.steps.map((step, index) => <li key={index}><span>{step.name}</span><span>{steps[step.state] ?? step.state}</span></li>)}</ol><Failure error={query.error ?? mutation.error ?? locate.error} retry={query.isError ? () => void query.refetch() : undefined} busy={query.isFetching}/>{mutation.isError && unknownResult(mutation.error) && <Btn disabled={locate.isPending} onClick={() => locate.mutate()}>查询原请求</Btn>}<div className="heading-actions">
    {(operation.state === 'accepted' || operation.subject === 'task_run' && pending.has(operation.state)) && <Btn disabled={locked} onClick={() => operation.state === 'accepted' ? mutation.mutate('cancel') : setCancel(true)}>{operation.state === 'accepted' ? '取消排队' : '取消执行'}</Btn>}
    {operation.state === 'awaiting_confirmation' && <Btn variant="primary" disabled={locked || !operation.confirmation_remaining_seconds} onClick={() => mutation.mutate('confirm')}>确认保留</Btn>}
    {['attention_required', 'prepared'].includes(operation.state) && <Btn disabled={locked} onClick={() => mutation.mutate('recover')}>{operation.subject === 'container' ? '核对状态' : '重新处理'}</Btn>}
    {operation.state === 'attention_required' && <Btn disabled={locked} onClick={() => setResolve(true)}>接受现状</Btn>}
    </div><ExecutionLog id={id}/><details className="operation-technical"><summary>技术详情</summary><dl className="detail-fields">{[['操作 ID', operation.id], ['资源 ID', operation.resource_id], ['请求键', operation.idempotency_key], ['节点 ID', operation.node_id], ['执行身份', operation.actor], ['计划 ID', operation.plan_id], ['队列序号', operation.queue_sequence?.toString()]].filter(([, value]) => value !== undefined && value !== '').map(([label, value]) => <div key={label}><dt>{label}</dt><dd><Copy value={value!}/></dd></div>)}</dl></details><Confirm open={cancel} title="取消任务执行？" description={`${context.nodeName(operation.node_id)} · ${operation.resource_id || operation.id}`} confirmLabel="取消执行" busy={mutation.isPending} error={mutation.error} onCancel={() => setCancel(false)} onConfirm={() => mutation.mutate('cancel')}/><Confirm open={resolve} title="接受当前状态并结束操作？" description={operation.id} confirmLabel="接受现状" busy={mutation.isPending} error={mutation.error} onCancel={() => setResolve(false)} onConfirm={() => mutation.mutate('resolve')}/></article>;
}
export function ExecutionLog({ id }: { id: string }) {
  const [offset, setOffset] = useState(0), [pages, setPages] = useState<{ id: string; chunks: Map<number, Uint8Array> }>({ id, chunks: new Map() });
  const query = useQuery({ queryKey: ['operations', id, 'logs', offset], queryFn: async ({ signal }) => unwrap(await api.GET('/operations/{id}/logs', { signal, params: { path: { id }, query: { offset, limit: 65536 } } })) });
  useEffect(() => { setOffset(0); }, [id]);
  useEffect(() => { if (!query.data) return; const bytes = Uint8Array.from(atob(query.data.data), value => value.charCodeAt(0)); setPages(old => { const chunks = new Map(old.id === id ? old.chunks : []); chunks.set(offset, bytes); return { id, chunks }; }); }, [id, offset, query.data]);
  const text = useMemo(() => { if (pages.id !== id) return ''; const chunks = [...pages.chunks].sort(([a], [b]) => a - b).map(([, value]) => value), buffer = new Uint8Array(chunks.reduce((n, chunk) => n + chunk.length, 0)); let at = 0; for (const chunk of chunks) { buffer.set(chunk, at); at += chunk.length; } return new TextDecoder().decode(buffer, { stream: query.data?.has_more }); }, [id, pages, query.data?.has_more]);
  return <section className="execution-log"><div className="page-toolbar"><h3>执行日志</h3><Btn onClick={() => void query.refetch()}>刷新日志</Btn></div><Failure error={query.error} retry={() => void query.refetch()}/><pre className="log-output">{text || (query.isPending ? '正在读取…' : '暂无日志')}</pre>{query.data?.truncated && <p className="meta">日志已截断</p>}{query.data?.has_more && <Btn onClick={() => setOffset(query.data!.next_offset)}>加载更多</Btn>}</section>;
}
