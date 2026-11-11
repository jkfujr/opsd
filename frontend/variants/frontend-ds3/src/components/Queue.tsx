import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { operationStates, operationSubjects, stepStates } from '../lib/labels';
import { duration, serial, stamp } from '../lib/format';
import { useWorld } from '../lib/world';
import { Button, Confirm, Failure, Skeleton, Stamp, useSession } from './ui';

type Operation = Schema['Operation'];

type QueueApi = {
  open: (id: string) => void;
  pendingCount: number;
  items: Operation[];
  flash: Set<string>;
  expanded: string;
  onToggle: (id: string) => void;
};

const QueueContext = createContext<QueueApi>({ open: () => undefined, pendingCount: 0, items: [], flash: new Set(), expanded: '', onToggle: () => undefined });
export function useQueue() {
  return useContext(QueueContext);
}

const pendingQuery = {
  queryKey: ['operations', 'pending'],
  queryFn: async ({ signal }: { signal: AbortSignal }) => value(await api.GET('/operations/pending', { signal })),
};

export function QueueProvider({ children }: { children: ReactNode }) {
  const [expanded, setExpanded] = useState('');
  const pending = useQuery({
    ...pendingQuery,
    refetchInterval: 2000,
  });
  const items = useMemo(() => {
    const list = pending.data?.items ?? [];
    const rank = (op: Operation) => ['awaiting_confirmation', 'attention_required'].indexOf(op.state);
    return [...list].sort((a, b) => {
      const ar = rank(a);
      const br = rank(b);
      if (ar !== br) return br === -1 ? -1 : ar === -1 ? 1 : ar - br;
      return 0;
    });
  }, [pending.data]);

  const previous = useRef(new Map<string, string>());
  const [flash, setFlash] = useState<Set<string>>(new Set());
  useEffect(() => {
    const next = new Set<string>();
    for (const op of items) {
      const before = previous.current.get(op.id);
      if (before !== undefined && before !== op.state) next.add(op.id);
      previous.current.set(op.id, op.state);
    }
    if (next.size) {
      setFlash(next);
      const timer = window.setTimeout(() => setFlash(new Set()), 1600);
      return () => window.clearTimeout(timer);
    }
    return undefined;
  }, [items]);

  return (
    <QueueContext.Provider value={{ open: setExpanded, pendingCount: items.length, items, flash, expanded, onToggle: (id) => setExpanded(expanded === id ? '' : id) }}>
      {children}
    </QueueContext.Provider>
  );
}

export function QueuePane() {
  const session = useSession();
  const world = useWorld();
  const { items, flash, expanded, onToggle } = useQueue();
  const pending = useQuery({ ...pendingQuery, staleTime: 4000 });
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const names = useMemo(() => new Map(nodes.data?.items.map((node) => [node.id, node.name]) ?? []), [nodes.data]);

  return (
    <aside className="queue" aria-label="操作队列">
      <div className="queue__head">
        <h2>操作队列</h2>
        <span className="queue__count">{String(items.length).padStart(2, '0')} 未决</span>
        <span className="grow" />
        <Button size="sm" variant="ghost" icon="refresh" aria-label="刷新队列" busy={pending.isFetching} onClick={() => void pending.refetch()} />
      </div>
      <div className="queue__body">
        <Failure error={pending.error} retry={() => void pending.refetch()} busy={pending.isFetching} />
        {pending.isPending ? <Skeleton height={70} /> : null}
        {!pending.isPending && items.length === 0 ? <div className="queue__empty">队列空闲，没有未决操作</div> : null}
        {items.map((op) => (
          <QueueItem
            key={op.id}
            operation={op}
            nodeName={names.get(op.node_id)}
            world={world}
            flash={flash.has(op.id)}
            expanded={expanded === op.id}
            onToggle={() => onToggle(op.id)}
          />
        ))}
        <p className="mono" style={{ fontSize: 9.5, color: 'var(--ink-mute)', textAlign: 'center', paddingTop: 4 }}>
          本机 {session.local_node_id.slice(0, 8)}
        </p>
      </div>
    </aside>
  );
}

function barcode(id: string): string {
  let hash = 0;
  for (let i = 0; i < id.length; i += 1) hash = (hash * 31 + id.charCodeAt(i)) >>> 0;
  const bars: string[] = [];
  let x = 0;
  let cursor = hash;
  while (x < 100) {
    const width = 1 + (cursor % 3);
    const gap = 2 + (cursor % 3);
    bars.push(`${x}px ${width}px`);
    x += width + gap;
    cursor = (cursor * 2654435761) >>> 0;
  }
  return `repeating-linear-gradient(90deg, currentColor 0 ${bars.join(', currentColor ')}, transparent 0)`;
}

function QueueItem({
  operation,
  nodeName,
  world,
  flash,
  expanded,
  onToggle,
}: {
  operation: Operation;
  nodeName?: string;
  world: string;
  flash: boolean;
  expanded: boolean;
  onToggle: () => void;
}) {
  const attention = operation.state === 'awaiting_confirmation' || operation.state === 'attention_required';
  const deadline = operation.confirmation_remaining_seconds;
  const id = serial(operation.node_id, operation.id);
  const stampEl = (
    <span className="qitem__stamp">
      <Stamp state={operation.state}>{operationStates[operation.state]}</Stamp>
    </span>
  );
  const keyboard = { role: 'button' as const, tabIndex: 0, onKeyDown: (event: React.KeyboardEvent) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onToggle(); } } };

  if (world === 'boarding') {
    return (
      <article className={`qitem qitem--boarding${attention ? ' qitem--attention' : ''}`} onClick={onToggle} aria-expanded={expanded} {...keyboard}>
        <div className="bp__row">
          <span className="bp__seq">{id}</span>
          <span className="bp__gate">
            {nodeName ?? operation.node_id.slice(0, 8)}
            <b>{operationSubjects[operation.subject] ?? operation.subject}</b>
          </span>
          <span className="bp__time">
            {deadline && deadline > 0 ? (
              <>
                签收截止 <b>{duration(deadline)}</b>
              </>
            ) : (
              <>
                受理 <b>{stamp(operation.created_at).slice(5)}</b>
              </>
            )}
          </span>
        </div>
        <span className="barcode" style={{ background: barcode(operation.id), backgroundRepeat: 'no-repeat' }} aria-hidden="true" />
        {stampEl}
        {expanded ? (
          <div className="qdetail">
            <OperationDetail operation={operation} nodeName={nodeName} />
          </div>
        ) : null}
      </article>
    );
  }

  if (world === 'timetable') {
    return (
      <article className={`qitem qitem--timetable${attention ? ' qitem--attention' : ''}`} onClick={onToggle} aria-expanded={expanded} {...keyboard}>
        <span className="qitem__id">{stamp(operation.created_at).slice(5)}</span>
        <span className="qitem__subject">{operationSubjects[operation.subject] ?? operation.subject}</span>
        {stampEl}
        {expanded ? (
          <div className="qdetail">
            <OperationDetail operation={operation} nodeName={nodeName} />
          </div>
        ) : null}
      </article>
    );
  }

  return (
    <article className={`qitem${attention ? ' qitem--attention' : ''}${flash ? ' board-move' : ''}`} onClick={onToggle} aria-expanded={expanded} {...keyboard}>
      <div className="qitem__head">
        <span className="qitem__id">{world === 'lab' ? `NO.${id}` : nodeName ?? operation.node_id.slice(0, 8)}</span>
        <span className="qitem__subject">{operationSubjects[operation.subject] ?? operation.subject}</span>
        {stampEl}
      </div>
      <div className="qitem__meta">
        {world === 'lab' ? <span>{nodeName ?? operation.node_id.slice(0, 8)}</span> : <span>NO.{id}</span>}
        <span>受理 {stamp(operation.created_at).slice(5)}</span>
        {attention && deadline && deadline > 0 ? <span style={{ color: 'var(--warn)' }}>复核 {duration(deadline)}</span> : null}
        {operation.result_pending_sync ? <span style={{ color: 'var(--warn)' }}>结果待同步</span> : null}
      </div>
      {expanded ? (
        <div className="qdetail">
          <OperationDetail operation={operation} nodeName={nodeName} />
        </div>
      ) : null}
    </article>
  );
}

function OperationDetail({ operation, nodeName }: { operation: Operation; nodeName?: string }) {
  const [confirmResolve, setConfirmResolve] = useState(false);
  const mutation = useMutation({
    mutationFn: async (kind: 'confirm' | 'recover' | 'resolve' | 'cancel') => {
      const params = { path: { id: operation.id }, header: { 'X-CSRF-Token': '', 'Idempotency-Key': crypto.randomUUID() } };
      if (kind === 'cancel') return value(await api.POST('/operations/{id}/cancel', { params }));
      if (kind === 'resolve') return value(await api.POST('/operations/{id}/resolve', { params, body: { accept_current: true, expected_revision: operation.revision } }));
      if (kind === 'confirm') return value(await api.POST('/operations/{id}/confirm', { params, body: { expected_revision: operation.revision } }));
      return value(await api.POST('/operations/{id}/recover', { params, body: { expected_revision: operation.revision } }));
    },
    onSuccess: () => {
      setConfirmResolve(false);
      void queryClient.invalidateQueries({ queryKey: ['operations'] });
      void queryClient.invalidateQueries({ queryKey: ['overview'] });
    },
    onError: () => void queryClient.invalidateQueries({ queryKey: ['operations'] }),
  });
  const left = operation.confirmation_remaining_seconds;

  return (
    <div className="stack gap-10">
      <dl className="facts">
        <dt>节点</dt>
        <dd>{nodeName ?? operation.node_id}</dd>
        <dt>受理</dt>
        <dd className="mono">{stamp(operation.created_at)}</dd>
        <dt>修订</dt>
        <dd className="mono">#{operation.queue_sequence ?? '—'} · {operation.revision}</dd>
      </dl>

      {operation.state === 'awaiting_confirmation' ? (
        <p style={{ fontSize: 11.5, color: 'var(--ink-soft)' }}>
          {operation.result_pending_sync ? '等待节点核对确认状态。' : left && left > 0 ? `已应用，剩余 ${duration(left)} 后自动撤销。` : '确认窗口已结束。'}
        </p>
      ) : null}

      {operation.steps.length ? (
        <ol className="stack gap-6">
          {operation.steps.map((step, index) => (
            <li key={index} className="spread" style={{ fontSize: 11.5 }}>
              <span className="row gap-6">
                <span className="mono" style={{ fontSize: 9.5, color: 'var(--ink-mute)' }}>{String(index + 1).padStart(2, '0')}</span>
                {step.name}
              </span>
              <Stamp state={step.state}>{stepStates[step.state]}</Stamp>
            </li>
          ))}
        </ol>
      ) : null}

      {operation.error ? <Failure error={new Error(operation.error.message)} /> : null}
      <Failure error={mutation.error} />

      <div className="row gap-6 wrap">
        {operation.state === 'accepted' ? (
          <Button size="sm" variant="line" busy={mutation.isPending && mutation.variables === 'cancel'} disabled={mutation.isPending || operation.result_pending_sync} onClick={() => mutation.mutate('cancel')}>
            取消排队
          </Button>
        ) : null}
        {operation.state === 'awaiting_confirmation' ? (
          <Button size="sm" variant="solid" busy={mutation.isPending && mutation.variables === 'confirm'} disabled={mutation.isPending || operation.result_pending_sync || !left || left <= 0} onClick={() => mutation.mutate('confirm')}>
            确认保留
          </Button>
        ) : null}
        {['attention_required', 'prepared'].includes(operation.state) ? (
          <Button size="sm" variant="line" icon="refresh" busy={mutation.isPending && mutation.variables === 'recover'} disabled={mutation.isPending} onClick={() => mutation.mutate('recover')}>
            {operation.subject === 'container' ? '核对状态' : '查询原操作'}
          </Button>
        ) : null}
        {operation.state === 'attention_required' ? (
          <Button size="sm" variant="ghost" disabled={mutation.isPending} onClick={() => setConfirmResolve(true)}>
            接受现状
          </Button>
        ) : null}
      </div>

      <Confirm
        open={confirmResolve}
        title="接受当前状态并结束操作？"
        description="目标节点当前状态将被记录为已人工处理，OPSD 不再重试这条操作。"
        confirmLabel="接受现状"
        busy={mutation.isPending}
        error={mutation.error}
        onCancel={() => setConfirmResolve(false)}
        onConfirm={() => mutation.mutate('resolve')}
      />
    </div>
  );
}

