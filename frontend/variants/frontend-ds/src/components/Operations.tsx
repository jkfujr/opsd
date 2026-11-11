import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, message, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { operationStates, operationSubjects, stepStates, terminalStates } from '../lib/labels';
import { duration, serial, stamp } from '../lib/format';
import { Icon } from './Icon';
import { Button, CopyLine, Dialog, Drawer, Empty, Failure, Select, Sheet, Skeleton, Stamp, Tabs, useSession } from './ui';

type Operation = Schema['Operation'];

export const pendingQuery = {
  queryKey: ['operations', 'pending'],
  queryFn: async ({ signal }: { signal: AbortSignal }) => value(await api.GET('/operations/pending', { signal })),
};

const OperationsContext = createContext<{ open: (id?: string) => void; error?: unknown } | null>(null);

export function useOperations() {
  const context = useContext(OperationsContext);
  if (!context) throw new Error('操作队列尚未就绪');
  return context;
}

export function Docket({
  operation,
  nodeName,
  onOpen,
  dense,
}: {
  operation: Operation;
  nodeName?: string;
  onOpen?: () => void;
  dense?: boolean;
}) {
  const attention = operation.state === 'awaiting_confirmation' || operation.state === 'attention_required';
  return (
    <article
      className={`docket${attention ? ' docket--attention' : ''}`}
      onClick={onOpen}
      style={onOpen ? { cursor: 'pointer' } : undefined}
      role={onOpen ? 'button' : undefined}
      tabIndex={onOpen ? 0 : undefined}
      onKeyDown={onOpen ? (event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onOpen(); } } : undefined}
    >
      <div className="docket__head">
        <span className="docket__id">NO.{serial(operation.node_id, operation.id)}</span>
        <span className="docket__subject">{operationSubjects[operation.subject] ?? operation.subject}</span>
        <span className="docket__stamp">
          <Stamp state={operation.state}>{operationStates[operation.state]}</Stamp>
        </span>
      </div>
      <div className="docket__meta">
        <span>{nodeName ?? operation.node_id.slice(0, 8)}</span>
        <span>{stamp(operation.created_at)}</span>
        {operation.result_pending_sync ? <span style={{ color: 'var(--amber)' }}>结果待同步</span> : null}
        {!terminalStates.has(operation.state) && operation.confirmation_remaining_seconds ? (
          <span style={{ color: 'var(--stamp)' }}>剩余 {duration(operation.confirmation_remaining_seconds)}</span>
        ) : null}
        {operation.actor ? <span>操作人 {operation.actor}</span> : null}
      </div>
      {!dense && operation.steps.length ? (
        <div className="docket__steps">
          {operation.steps.map((step, index) => (
            <span key={index} className="docket__step" data-state={step.state}>
              <i />
              {step.name}
              <span style={{ opacity: 0.7 }}>{stepStates[step.state]}</span>
            </span>
          ))}
        </div>
      ) : null}
      {operation.error ? <p style={{ fontSize: 12, color: 'var(--stamp)' }}>{operation.error.message}</p> : null}
      {onOpen ? (
        <div className="docket__hint mono">
          {attention ? '处理这张单据' : '查看记录'}
          <Icon name="chevron" width={11} height={11} />
        </div>
      ) : null}
    </article>
  );
}

export function OperationsButton() {
  const operations = useOperations();
  const pending = useQuery(pendingQuery);
  const count = pending.data?.items.length ?? 0;
  return (
    <Button variant="line" size="sm" icon="terminal" onClick={() => operations.open()}>
      <span className="label">操作队列</span>
      <span className="mono" style={{ color: count ? 'var(--stamp)' : 'var(--ink-mute)', marginLeft: 2 }}>
        {pending.isPending ? '·' : String(count).padStart(2, '0')}
      </span>
    </Button>
  );
}

export function OperationsProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState('');
  return (
    <OperationsContext.Provider value={{ open: (id = '') => { setSelected(id); setOpen(true); } }}>
      {children}
      <OperationsDrawer
        open={open}
        selected={selected}
        onSelect={setSelected}
        onClose={() => setOpen(false)}
      />
    </OperationsContext.Provider>
  );
}

function OperationsDrawer({
  open,
  selected,
  onSelect,
  onClose,
}: {
  open: boolean;
  selected: string;
  onSelect: (id: string) => void;
  onClose: () => void;
}) {
  const [tab, setTab] = useState<'pending' | 'history'>('pending');
  const [node, setNode] = useState('');
  const [state, setState] = useState('');
  const [cursor, setCursor] = useState('');

  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const names = useMemo(() => new Map(nodes.data?.items.map((n) => [n.id, n.name]) ?? []), [nodes.data]);

  const pending = useQuery({
    ...pendingQuery,
    refetchInterval: open && tab === 'pending' ? 2000 : false,
  });
  const detail = useQuery({
    queryKey: ['operations', 'detail', selected],
    enabled: open && Boolean(selected),
    queryFn: async ({ signal }) => value(await api.GET('/operations/{id}', { signal, params: { path: { id: selected } } })),
    refetchInterval: (query) => (open && query.state.data && !terminalStates.has(query.state.data.state) ? 1500 : false),
  });
  const history = useQuery({
    queryKey: ['operations', 'history', node, state, cursor],
    enabled: open && tab === 'history',
    queryFn: async ({ signal }) =>
      value(
        await api.GET('/operations', {
          signal,
          params: { query: { node_id: node || undefined, state: (state || undefined) as Schema['OperationState'] | undefined, cursor: cursor || undefined, limit: 50 } },
        }),
      ),
  });

  const detailId = detail.data?.id;
  const detailRevision = detail.data?.revision;
  useEffect(() => {
    if (!detailId) return;
    void queryClient.invalidateQueries({ queryKey: ['operations'] });
    void queryClient.invalidateQueries({ queryKey: ['overview'] });
    void queryClient.invalidateQueries({ queryKey: ['nodes'] });
  }, [detailId, detailRevision]);

  const pendingItems = pending.data?.items ?? [];
  const ordered = [...pendingItems].sort(
    (a, b) => ['awaiting_confirmation', 'attention_required'].indexOf(a.state) - ['awaiting_confirmation', 'attention_required'].indexOf(b.state),
  );

  return (
    <Drawer
      open={open}
      title="操作队列"

      onClose={onClose}
      footer={
        <>
          <span className="grow" />
          <Button size="sm" icon="refresh" onClick={() => { void pending.refetch(); void detail.refetch(); void history.refetch(); }}>
            刷新
          </Button>
        </>
      }
    >
      <Tabs
        items={[
          { id: 'pending', label: '未决', count: pendingItems.length },
          { id: 'history', label: '历史' },
        ]}
        value={tab}
        onChange={(next) => setTab(next as 'pending' | 'history')}
        label="操作分类"
      />

      {tab === 'pending' ? (
        <div className="stack gap-10">
          <Failure error={pending.error} retry={() => void pending.refetch()} busy={pending.isFetching} />
          {pending.isPending ? <Skeleton height={90} /> : null}
          {!pending.isPending && !ordered.length && !pending.error ? (
            <Empty>没有未决操作</Empty>
          ) : null}
          {ordered.map((operation) => (
            <Docket
              key={operation.id}
              operation={operation}
              nodeName={names.get(operation.node_id)}
              onOpen={() => onSelect(operation.id)}
            />
          ))}
        </div>
      ) : (
        <div className="stack gap-10">
          <div className="toolbar">
            <Select
              label="节点"
              value={node}
              onChange={(event) => { setNode(event.target.value); setCursor(''); }}
              options={[['', '全部节点'], ...(nodes.data?.items.map((n) => [n.id, n.name] as const) ?? [])]}
            />
            <Select
              label="状态"
              value={state}
              onChange={(event) => { setState(event.target.value); setCursor(''); }}
              options={[['', '全部状态'], ...(Object.entries(operationStates) as [string, string][])]}
            />
          </div>
          <Failure error={history.error} retry={() => void history.refetch()} busy={history.isFetching} />
          {history.isPending ? <Skeleton height={70} /> : null}
          {history.data?.items.length === 0 ? <Empty>暂无记录</Empty> : null}
          {history.data?.items.map((operation) => (
            <Docket
              key={operation.id}
              operation={operation}
              nodeName={names.get(operation.node_id)}
              onOpen={() => onSelect(operation.id)}
              dense
            />
          ))}
          <div className="row gap-8">
            {cursor ? <Button size="sm" onClick={() => setCursor('')}>返回最新</Button> : null}
            {history.data?.next_cursor ? <Button size="sm" onClick={() => setCursor(history.data!.next_cursor!)}>更早记录</Button> : null}
          </div>
        </div>
      )}

      {selected ? (
        <Sheet title="单据详情">
          <Failure error={detail.error} retry={() => void detail.refetch()} busy={detail.isFetching} />
          {detail.isPending ? <Skeleton height={120} /> : null}
          {detail.data ? <OperationDetail operation={detail.data} nodeName={names.get(detail.data.node_id)} /> : null}
        </Sheet>
      ) : null}
    </Drawer>
  );
}

function OperationDetail({ operation, nodeName }: { operation: Operation; nodeName?: string }) {
  const [confirmResolve, setConfirmResolve] = useState(false);
  const session = useSession();
  const mutation = useMutation({
    mutationFn: async (kind: 'confirm' | 'recover' | 'resolve' | 'cancel') => {
      const params = { path: { id: operation.id }, header: { 'X-CSRF-Token': session.csrf_token, 'Idempotency-Key': crypto.randomUUID() } };
      if (kind === 'cancel') return value(await api.POST('/operations/{id}/cancel', { params }));
      if (kind === 'resolve') return value(await api.POST('/operations/{id}/resolve', { params, body: { accept_current: true, expected_revision: operation.revision } }));
      if (kind === 'confirm') return value(await api.POST('/operations/{id}/confirm', { params, body: { expected_revision: operation.revision } }));
      return value(await api.POST('/operations/{id}/recover', { params, body: { expected_revision: operation.revision } }));
    },
    onSuccess: () => {
      setConfirmResolve(false);
      void queryClient.invalidateQueries({ queryKey: ['operations'] });
    },
    onError: () => void queryClient.invalidateQueries({ queryKey: ['operations'] }),
  });
  const left = operation.confirmation_remaining_seconds;

  return (
    <div className="stack gap-14">
      <div className="spread">
        <Stamp state={operation.state} size="lg">{operationStates[operation.state]}</Stamp>
        <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
          {operation.result_pending_sync ? '结果待同步' : stamp(operation.last_observed_at)}
        </span>
      </div>

      <dl className="facts">
        <dt>节点</dt>
        <dd>{nodeName ?? operation.node_id}</dd>
        <dt>对象</dt>
        <dd>{operationSubjects[operation.subject] ?? operation.subject}</dd>
        <dt>受理</dt>
        <dd className="mono">{stamp(operation.created_at)}</dd>
        <dt>队列</dt>
        <dd className="mono">#{operation.queue_sequence ?? '—'} · 修订 {operation.revision}</dd>
        <dt>决策</dt>
        <dd>{operation.decision === 'keep' ? '保留变更' : operation.decision === 'revert' ? '撤销变更' : operation.decision === 'commit' ? '确认提交' : '未决'}</dd>
        {operation.actor ? (
          <>
            <dt>操作人</dt>
            <dd>{operation.actor}</dd>
          </>
        ) : null}
      </dl>

      {operation.state === 'awaiting_confirmation' ? (
        <p style={{ fontSize: 12.5, color: 'var(--ink-soft)' }}>
          {operation.result_pending_sync
            ? '等待节点核对确认状态。'
            : left && left > 0
              ? `已应用，等待确认保留；剩余 ${duration(left)}，超时后自动撤销。`
              : '确认窗口已结束，正在核对结果。'}
        </p>
      ) : null}

      {operation.steps.length ? (
        <ol className="stack gap-8">
          {operation.steps.map((step, index) => (
            <li key={index} className="spread" style={{ fontSize: 12.5 }}>
              <span className="row gap-8">
                <span className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>{String(index + 1).padStart(2, '0')}</span>
                {step.name}
              </span>
              <span className="row gap-8 mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
                {step.observed_at ? stamp(step.observed_at).slice(5) : ''}
                <Stamp state={step.state}>{stepStates[step.state]}</Stamp>
              </span>
            </li>
          ))}
        </ol>
      ) : null}

      {operation.error ? <Failure error={new Error(operation.error.message)} /> : null}
      <Failure error={mutation.error} />

      <div className="row gap-8 wrap">
        {operation.state === 'accepted' ? (
          <Button variant="line" busy={mutation.isPending && mutation.variables === 'cancel'} disabled={mutation.isPending || operation.result_pending_sync} onClick={() => mutation.mutate('cancel')}>
            取消排队
          </Button>
        ) : null}
        {operation.state === 'awaiting_confirmation' ? (
          <Button
            variant="solid"
            busy={mutation.isPending && mutation.variables === 'confirm'}
            disabled={mutation.isPending || operation.result_pending_sync || !left || left <= 0}
            onClick={() => mutation.mutate('confirm')}
          >
            确认保留
          </Button>
        ) : null}
        {['attention_required', 'prepared'].includes(operation.state) ? (
          <Button variant="line" icon="refresh" busy={mutation.isPending && mutation.variables === 'recover'} disabled={mutation.isPending} onClick={() => mutation.mutate('recover')}>
            {operation.subject === 'container' ? '核对状态' : '查询原操作'}
          </Button>
        ) : null}
        {operation.state === 'attention_required' ? (
          <Button variant="ghost" disabled={mutation.isPending} onClick={() => setConfirmResolve(true)}>
            接受现状
          </Button>
        ) : null}
        <span className="grow" />
      </div>

      <CopyLine value={operation.id} label="复制操作 ID" />

      <Dialog
        open={confirmResolve}
        title="接受当前状态并结束操作？"

        width="narrow"
        onClose={() => setConfirmResolve(false)}
        footer={
          <>
            <span className="grow" />
            <Button onClick={() => setConfirmResolve(false)} disabled={mutation.isPending}>取消</Button>
            <Button variant="danger" busy={mutation.isPending} onClick={() => mutation.mutate('resolve')}>接受现状</Button>
          </>
        }
      >
        <p style={{ fontSize: 13, color: 'var(--ink-soft)' }}>
          将记录为已人工处理，OPSD 不再重试这条操作。
        </p>
        <Failure error={mutation.error} />
      </Dialog>
    </div>
  );
}

export { message };


