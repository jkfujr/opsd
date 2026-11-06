import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Button } from '@appica/ui-react/button';
import { Skeleton } from '@appica/ui-react/skeleton';
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle, DrawerBody } from '@appica/ui-react/drawer';
import { api, unwrap } from './api/client';
import type { components } from './api/schema.gen';
import { queryClient } from './state';
import { nodesQuery } from './pages/Nodes';
import { BusyButton, ChoiceField, Confirm, date, Failure, StatusLabel } from './ui';

type Operation = components['schemas']['Operation'];
type State = components['schemas']['OperationState'];
export const operationStates: Record<State, string> = { accepted: '已受理', prepared: '准备执行', applying: '应用中', awaiting_confirmation: '待确认', reverting: '撤销中', attention_required: '需处理', succeeded: '已成功', rejected: '未执行', failed: '未完成且无残留', reverted: '已撤销', resolved_manually: '已人工处理' };
export const terminal = (state: State) => ['succeeded', 'rejected', 'failed', 'reverted', 'resolved_manually'].includes(state);
export const pendingQuery = { queryKey: ['operations', 'pending'], queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/operations/pending', { signal })) };
const OperationsContext = createContext<{ show: (id?: string) => void } | null>(null);
export function useOperations() { return useContext(OperationsContext)!; }

export function OperationsProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState('');
  return <OperationsContext value={{ show: (id = '') => { setSelected(id); setOpen(true); } }}>
    {children}<OperationsDrawer open={open} onOpenChange={setOpen} selected={selected} onSelect={setSelected} />
  </OperationsContext>;
}
export function OperationsButton() {
  const pending = useQuery(pendingQuery);
  const operations = useOperations();
  return <Button size="sm" variant="outline" onClick={() => operations.show()}>操作{pending.data && !pending.error ? ` (${pending.data.items.length})` : ''}</Button>;
}
export function invalidateOperations() {
  void queryClient.invalidateQueries({ queryKey: ['operations'] });
}
function OperationsDrawer({ open, onOpenChange, selected, onSelect }: { open: boolean; onOpenChange: (open: boolean) => void; selected: string; onSelect: (id: string) => void }) {
  const [node, setNode] = useState('');
  const [state, setState] = useState('');
  const [cursor, setCursor] = useState('');
  const nodes = useQuery(nodesQuery);
  const pending = useQuery({ ...pendingQuery, refetchInterval: (query) => open && query.state.data?.items.some((op) => op.state === 'awaiting_confirmation') ? 1000 : false });
  const history = useQuery({ queryKey: ['operations', 'history', node, state, cursor], enabled: open, queryFn: async ({ signal }) => unwrap(await api.GET('/operations', { signal, params: { query: { node_id: node || undefined, state: (state || undefined) as State | undefined, cursor: cursor || undefined, limit: 50 } } })) });
  const detail = useQuery({ queryKey: ['operations', 'detail', selected], enabled: open && Boolean(selected), queryFn: async ({ signal }) => unwrap(await api.GET('/operations/{id}', { signal, params: { path: { id: selected } } })), refetchInterval: (query) => open && query.state.data && !terminal(query.state.data.state) ? 1000 : false });
  const detailID = detail.data?.id;
  const detailRevision = detail.data?.revision;
  const detailNode = detail.data?.node_id;
  const detailSubject = detail.data?.subject;
  useEffect(() => {
    if (!detailNode || !detailID) return;
    const key = detailSubject === 'container' ? ['containers'] : detailSubject === 'firewall_config' || detailSubject === 'firewall' || detailSubject === 'component' ? ['firewall', detailNode] : ['nodes', detailNode, detailSubject === 'account' ? 'account' : 'settings'];
    void queryClient.invalidateQueries({ queryKey: key });
  }, [detailID, detailRevision, detailNode, detailSubject]);
  const names = new Map(nodes.data?.items.map((n) => [n.id, n.name]) ?? []);
  const list = (items: Operation[]) => <ul className="divide-y divide-border">{items.map((op) => <li key={op.id}><button className={`w-full rounded-md px-3 py-3 text-left hover:bg-background-muted ${selected === op.id ? 'bg-background-muted' : ''}`} onClick={() => onSelect(op.id)}>
    <span className="flex flex-wrap items-center justify-between gap-2"><span className="font-medium text-foreground-intense">{names.get(op.node_id) ?? op.node_id}</span><StatusLabel state={op.state}>{operationStates[op.state]}</StatusLabel></span>
    <span className="mt-1 block text-xs text-foreground-muted">{date(op.created_at)}{op.result_pending_sync ? ' · 结果待同步' : ''}</span>
  </button></li>)}</ul>;
  return <Drawer side="right" open={open} onOpenChange={onOpenChange}><DrawerContent frame={false} closeLabel="关闭操作" className="h-full w-full max-w-2xl">
    <DrawerHeader><DrawerTitle>操作</DrawerTitle></DrawerHeader><DrawerBody className="min-h-0 space-y-6 overflow-y-auto">
      <section><h2 className="mb-3 font-semibold">未决操作</h2><Failure error={pending.error} retry={() => void pending.refetch()} busy={pending.isFetching} />{pending.isPending && <OperationPlaceholder />}{pending.data && (pending.data.items.length ? list([...pending.data.items].sort((a, b) => ['awaiting_confirmation', 'attention_required'].indexOf(b.state) - ['awaiting_confirmation', 'attention_required'].indexOf(a.state))) : !pending.isError && <p className="text-sm text-foreground-muted">暂无未决操作</p>)}</section>
      {selected && <section className="border-y border-border py-6"><Failure error={detail.error} retry={() => void detail.refetch()} busy={detail.isFetching} />{detail.isPending && <OperationPlaceholder />}{detail.data && <OperationDetail key={selected} operation={detail.data} />}</section>}
      <section><h2 className="mb-4 font-semibold">历史记录</h2><div className="mb-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <ChoiceField label="节点" value={node} options={[[ '', '全部节点' ], ...(nodes.data?.items.map((n) => [n.id, n.name] as const) ?? [])]} onChange={(v) => { setNode(v); setCursor(''); }} />
        <ChoiceField label="状态" value={state} options={[[ '', '全部状态' ], ...Object.entries(operationStates)]} onChange={(v) => { setState(v); setCursor(''); }} />
      </div><Failure error={history.error} retry={() => void history.refetch()} busy={history.isFetching} />{history.isPending && <OperationPlaceholder />}{history.data && (history.data.items.length ? list(history.data.items) : !history.isError && <p className="text-sm text-foreground-muted">暂无记录</p>)}
      <div className="mt-4 flex gap-3">{cursor && <Button variant="outline" size="sm" onClick={() => setCursor('')}>返回最新</Button>}{history.data?.next_cursor && <Button variant="outline" size="sm" onClick={() => setCursor(history.data!.next_cursor!)}>更早记录</Button>}</div></section>
    </DrawerBody>
  </DrawerContent></Drawer>;
}
function OperationPlaceholder() { return <div className="space-y-3 py-3" aria-busy="true"><Skeleton className="h-5 w-2/3" /><Skeleton className="h-4 w-full" /><Skeleton className="h-4 w-1/3" /></div>; }
function OperationDetail({ operation: op }: { operation: Operation }) {
  const [resolve, setResolve] = useState(false);
  const mutation = useMutation({ mutationFn: async (kind: 'confirm' | 'recover' | 'resolve' | 'cancel') => {
    const params = { path: { id: op.id }, header: { 'X-CSRF-Token': '' } };
    if (kind === 'cancel') return unwrap(await api.POST('/operations/{id}/cancel', { params }));
    if (kind === 'resolve') return unwrap(await api.POST('/operations/{id}/resolve', { params, body: { accept_current: true, expected_revision: op.revision } }));
    if (kind === 'confirm') return unwrap(await api.POST('/operations/{id}/confirm', { params, body: { expected_revision: op.revision } }));
    return unwrap(await api.POST('/operations/{id}/recover', { params, body: { expected_revision: op.revision } }));
  }, onSuccess: () => { setResolve(false); invalidateOperations(); }, onError: () => invalidateOperations() });
  const left = op.confirmation_remaining_seconds;
  const steps: Record<string, string> = { pending: '待执行', applying: '应用中', applied: '已读回', reverting: '撤销中', reverted: '已撤销', completed: '已完成', failed: '未完成', unknown: '待核对' };
  return <div className="space-y-4">
    <div className="flex flex-wrap justify-between gap-3"><h2 className="font-semibold text-foreground-intense">{operationStates[op.state]}</h2><span className="text-sm text-foreground-muted">{op.result_pending_sync ? '结果待同步' : date(op.last_observed_at)}</span></div>
    {op.state === 'awaiting_confirmation' && <p className="text-sm">{op.result_pending_sync ? '等待节点核对确认状态' : left !== undefined && left > 0 ? `剩余 ${left} 秒` : '正在核对超时结果'}</p>}
    {op.error && <p className="break-words text-sm text-foreground-intense">{op.error.message}</p>}
    {op.steps.length > 0 && <ol className="space-y-2 text-sm">{op.steps.map((step, index) => <li key={index} className="flex flex-wrap justify-between gap-2"><span>{step.name}</span><span className="text-foreground-muted">{steps[step.state] ?? step.state}</span></li>)}</ol>}
    <Failure error={mutation.error} />
    <div className="flex flex-wrap gap-3">
      {op.state === 'accepted' && <BusyButton variant="outline" busy={mutation.isPending && mutation.variables === 'cancel'} disabled={mutation.isPending || op.result_pending_sync} onClick={() => mutation.mutate('cancel')}>取消排队</BusyButton>}
      {op.state === 'awaiting_confirmation' && <BusyButton busy={mutation.isPending && mutation.variables === 'confirm'} disabled={mutation.isPending || op.result_pending_sync || !left || left <= 0} onClick={() => mutation.mutate('confirm')}>确认保留</BusyButton>}
      {['attention_required', 'prepared'].includes(op.state) && <BusyButton variant="outline" busy={mutation.isPending && mutation.variables === 'recover'} disabled={mutation.isPending || op.result_pending_sync} onClick={() => mutation.mutate('recover')}>{op.subject === 'container' ? '核对状态' : '重新处理'}</BusyButton>}
      {op.state === 'attention_required' && <Button variant="ghost" disabled={mutation.isPending || op.result_pending_sync} onClick={() => setResolve(true)}>接受现状</Button>}
    </div>
    <Confirm open={resolve} title={op.subject === 'container' ? '接受当前容器状态并结束操作？' : '接受当前规则并结束操作？'} description="将记录为已人工处理。" busy={mutation.isPending} error={mutation.error} confirmLabel="接受现状" onCancel={() => setResolve(false)} onConfirm={() => mutation.mutate('resolve')} />
  </div>;
}
