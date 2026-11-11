import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useSearchParams } from 'react-router';
import { api, idempotency, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { sequence, stamp } from '../lib/format';
import { dockerStates, terminalStates } from '../lib/labels';
import { Icon } from '../components/Icon';
import {
  Button,
  Confirm,
  Dialog,
  Drawer,
  Empty,
  Failure,
  Led,
  Notice,
  PageHead,
  Select,
  Sheet,
  Skeleton,
  Stamp,
} from '../components/ui';

type Item = Schema['ContainerListItem'];

const containerLabels: Record<string, string> = {
  running: '运行中',
  exited: '已退出',
  restarting: '重启中',
  paused: '已暂停',
  created: '已创建',
  dead: '已失效',
  removing: '移除中',
};

export function Containers() {
  const [params, setParams] = useSearchParams();
  const nodeFilter = params.get('node') ?? '';
  const [state, setState] = useState('');
  const [search, setSearch] = useState('');
  const [engine, setEngine] = useState<{ node: string; id: string; engine: string; name: string } | null>(null);
  const [logs, setLogs] = useState<{ node: string; id: string; engine: string; name: string } | null>(null);
  const [pending, setPending] = useState<{ item: Item; action: 'start' | 'stop' | 'restart' } | null>(null);

  const query = useQuery({
    queryKey: ['containers', nodeFilter, state, search],
    queryFn: async ({ signal }) =>
      value(
        await api.GET('/containers', {
          signal,
          params: { query: { node_id: nodeFilter || undefined, state: state || undefined, search: search || undefined } },
        }),
      ),
    refetchInterval: 5000,
  });

  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const names = useMemo(() => new Map(nodes.data?.items.map((node) => [node.id, node.name]) ?? []), [nodes.data]);

  const action = useMutation({
    mutationFn: async ({ item, action: kind }: { item: Item; action: 'start' | 'stop' | 'restart' }) =>
      value(
        await api.POST('/nodes/{id}/containers/{container_id}/actions', {
          params: { path: { id: item.node_id, container_id: item.container_id }, header: idempotency() },
          body: { engine_id: item.engine_id, action: kind },
        }),
      ),
    onSuccess: () => {
      setPending(null);
      void queryClient.invalidateQueries({ queryKey: ['containers'] });
      void queryClient.invalidateQueries({ queryKey: ['operations'] });
    },
  });

  const items = query.data?.items ?? [];
  const dockerNodes = query.data?.nodes ?? [];
  const unavailable = dockerNodes.filter((node) => node.state !== 'ready');
  const running = items.filter((item) => item.state === 'running').length;

  return (
    <div className="page">
      <PageHead
        title="容器"
        tools={
          <>
            <Button size="sm" icon="refresh" busy={query.isFetching} onClick={() => void query.refetch()}>
              刷新
            </Button>
          </>
        }
      />

      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      {unavailable.length ? (
        <Notice>
          {unavailable.map((node) => `${node.node_name}：${dockerStates[node.state]}`).join(' · ')}
        </Notice>
      ) : null}

      <Sheet
        title={`容器清单 ${items.length || ''}`.trim()}

        flush
        tools={
          <>
            <Select
              aria-label="节点"
              value={nodeFilter}
              onChange={(event) => {
                const next = new URLSearchParams(params);
                if (event.target.value) next.set('node', event.target.value);
                else next.delete('node');
                setParams(next, { replace: true });
              }}
              options={[['', '全部节点'], ...(nodes.data?.items.map((node) => [node.id, node.name] as const) ?? [])]}
              style={{ height: 30, width: 140, fontSize: 12 }}
            />
            <Select
              aria-label="容器状态"
              value={state}
              onChange={(event) => setState(event.target.value)}
              options={[['', '全部状态'], ['running', '运行中'], ['exited', '已退出'], ['restarting', '重启中'], ['paused', '已暂停']]}
              style={{ height: 30, width: 120, fontSize: 12 }}
            />
            <input
              className="input"
              style={{ height: 30, width: 150, fontSize: 12 }}
              placeholder="搜索容器"
              aria-label="搜索容器"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </>
        }
      >
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th scope="col" style={{ width: 46 }}>#</th>
                <th scope="col">容器</th>
                <th scope="col">状态</th>
                <th scope="col">节点</th>
                <th scope="col">镜像</th>
                <th scope="col">端口</th>
                <th scope="col" className="right">操作</th>
              </tr>
            </thead>
            <tbody>
              {query.isPending
                ? Array.from({ length: 5 }, (_, index) => (
                    <tr key={index}>
                      {Array.from({ length: 7 }, (_, cell) => (
                        <td key={cell}>
                          <Skeleton height={11} width={cell === 1 ? 130 : 52} />
                        </td>
                      ))}
                    </tr>
                  ))
                : items.map((item, index) => (
                    <tr key={`${item.node_id}-${item.container_id}`} style={{ cursor: 'pointer' }} onClick={() => setEngine({ node: item.node_id, id: item.container_id, engine: item.engine_id, name: item.name })}>
                      <td className="table__serial">{sequence(index)}</td>
                      <td>
                        <div className="table__name">
                          {item.name}
                          {item.project ? <span className="sub">{item.project}{item.service ? ` / ${item.service}` : ''}</span> : null}
                          {item.read_only_reason ? <Stamp state="unsupported">只读</Stamp> : null}
                        </div>
                        <div className="cell-sub">{item.container_id.slice(0, 12)}</div>
                      </td>
                      <td>
                        <span className="row gap-6">
                          <Led state={item.state === 'running' ? 'on' : item.state === 'restarting' ? 'warn' : item.state === 'paused' ? 'idle' : 'off'} />
                          <span style={{ fontSize: 12 }}>{containerLabels[item.state] ?? item.state}</span>
                          {item.health ? <span className="cell-sub">{item.health}</span> : null}
                        </span>
                      </td>
                      <td className="mono" style={{ fontSize: 11, color: 'var(--ink-soft)' }}>{item.node_name}</td>
                      <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)', maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis' }}>{item.image}</td>
                      <td>
                        <span className="port-list">
                          {item.ports.length === 0 ? <span className="port">无发布</span> : null}
                          {item.ports.slice(0, 3).map((port, portIndex) => (
                            <span key={portIndex} className="port">
                              {port.host_port}→{port.container_port}/{port.protocol}
                            </span>
                          ))}
                          {item.ports.length > 3 ? <span className="port">+{item.ports.length - 3}</span> : null}
                        </span>
                      </td>
                      <td className="right" onClick={(event) => event.stopPropagation()}>
                        <span className="row gap-4" style={{ justifyContent: 'flex-end' }}>
                          <Button size="sm" variant="ghost" disabled={Boolean(item.read_only_reason)} onClick={() => setPending({ item, action: item.state === 'running' ? 'stop' : 'start' })}>
                            {item.state === 'running' ? '停止' : '启动'}
                          </Button>
                          <Button size="sm" variant="ghost" disabled={Boolean(item.read_only_reason)} onClick={() => setPending({ item, action: 'restart' })}>
                            重启
                          </Button>
                          <Button size="sm" variant="ghost" icon="logs" onClick={() => setLogs({ node: item.node_id, id: item.container_id, engine: item.engine_id, name: item.name })} aria-label={`${item.name} 日志`} />
                        </span>
                      </td>
                    </tr>
                  ))}
              {!query.isPending && items.length === 0 ? (
                <tr>
                  <td colSpan={7}>
                    <Empty>没有匹配的容器</Empty>
                  </td>
                </tr>
              ) : null}
            </tbody>
            <tfoot>
              <tr>
                <td className="table-foot" colSpan={7}>
                  共 {items.length} 个 · 运行中 {running} · {dockerNodes.filter((node) => node.state === 'ready').length} 个节点可用
                </td>
              </tr>
            </tfoot>
          </table>
        </div>
      </Sheet>

      <div className="node-states">
        {dockerNodes.map((node) => (
          <span key={node.node_id} className="node-state">
            <Led state={node.state === 'ready' ? 'on' : node.state === 'offline' ? 'off' : 'warn'} />
            {node.node_name}
            <span style={{ color: 'var(--ink-mute)' }}>{dockerStates[node.state]}</span>
            {node.version ? <span style={{ color: 'var(--ink-mute)' }}>{node.version}</span> : null}
          </span>
        ))}
      </div>

      <ContainerDrawer
        target={engine}
        nodeName={engine ? names.get(engine.node) : undefined}
        onClose={() => setEngine(null)}
        onLogs={() => {
          if (engine) setLogs(engine);
          setEngine(null);
        }}
        onAction={(kind) => {
          const item = items.find((entry) => entry.container_id === engine?.id);
          if (item) setPending({ item, action: kind });
        }}
      />

      <LogsDialog target={logs} onClose={() => setLogs(null)} />

      <Confirm
        open={Boolean(pending)}
        title={`${pending?.action === 'start' ? '启动' : pending?.action === 'stop' ? '停止' : '重启'} ${pending?.item.name ?? ''}？`}
        description={
          <>
            操作由节点 {pending ? names.get(pending.item.node_id) ?? pending.item.node_id : ''} 受理后执行，
            应答丢失时在操作队列查询原操作。
          </>
        }
        confirmLabel={pending?.action === 'start' ? '启动容器' : pending?.action === 'stop' ? '停止容器' : '重启容器'}
        busy={action.isPending}
        error={action.error}
        onCancel={() => setPending(null)}
        onConfirm={() => pending && action.mutate(pending)}
      />
    </div>
  );
}

function ContainerDrawer({
  target,
  nodeName,
  onClose,
  onLogs,
  onAction,
}: {
  target: { node: string; id: string; engine: string; name: string } | null;
  nodeName?: string;
  onClose: () => void;
  onLogs: () => void;
  onAction: (action: 'start' | 'stop' | 'restart') => void;
}) {
  const detail = useQuery({
    queryKey: ['container', target?.node, target?.id],
    enabled: Boolean(target),
    queryFn: async ({ signal }) =>
      value(
        await api.GET('/nodes/{id}/containers/{container_id}', {
          signal,
          params: { path: { id: target!.node, container_id: target!.id }, query: { engine_id: target!.engine } },
        }),
      ),
  });
  return (
    <Drawer
      open={Boolean(target)}
      title={target?.name ?? ''}
      onClose={onClose}
      footer={
        <>
          <Button size="sm" icon="logs" onClick={onLogs}>最近日志</Button>
          <span className="grow" />
          <Button size="sm" onClick={() => onAction('restart')}>重启</Button>
          <Button size="sm" variant="danger" onClick={() => onAction('stop')}>停止</Button>
        </>
      }
    >
      <Failure error={detail.error} retry={() => void detail.refetch()} busy={detail.isFetching} />
      {detail.isPending ? <Skeleton height={120} /> : null}
      {detail.data ? (
        <div className="stack gap-16">
          <dl className="facts">
            <dt>容器 ID</dt>
            <dd className="mono">{detail.data.container_id.slice(0, 24)}</dd>
            <dt>镜像</dt>
            <dd className="mono">{detail.data.image}</dd>
            <dt>状态</dt>
            <dd>
              <Stamp state={detail.data.state === 'running' ? 'ready' : 'exited'}>{containerLabels[detail.data.state] ?? detail.data.state}</Stamp>
              {detail.data.health ? <span className="mono" style={{ marginLeft: 8, fontSize: 10.5, color: 'var(--ink-mute)' }}>{detail.data.health}</span> : null}
            </dd>
            <dt>重启策略</dt>
            <dd className="mono">{detail.data.restart_policy} · 已重启 {detail.data.restart_count} 次</dd>
            <dt>创建</dt>
            <dd className="mono">{stamp(detail.data.created_at)}</dd>
            <dt>启动</dt>
            <dd className="mono">{detail.data.started_at ? stamp(detail.data.started_at) : '未启动'}</dd>
            <dt>节点</dt>
            <dd>{nodeName ?? target?.node}</dd>
            {detail.data.project ? (
              <>
                <dt>Compose</dt>
                <dd className="mono">{detail.data.project}{detail.data.service ? ` / ${detail.data.service}` : ''}</dd>
              </>
            ) : null}
          </dl>

          <div className="stack gap-6">
            <span className="kicker">端口映射</span>
            {detail.data.ports.length === 0 ? <span style={{ fontSize: 12, color: 'var(--ink-mute)' }}>没有发布端口</span> : null}
            <div className="port-list">
              {detail.data.ports.map((port, index) => (
                <span key={index} className="port">
                  {port.host_ip}:{port.host_port} → {port.container_port}/{port.protocol}
                </span>
              ))}
            </div>
          </div>

          <div className="stack gap-6">
            <span className="kicker">挂载</span>
            {detail.data.mounts.length === 0 ? <span style={{ fontSize: 12, color: 'var(--ink-mute)' }}>没有挂载</span> : null}
            <div className="stack gap-4">
              {detail.data.mounts.map((mount, index) => (
                <div key={index} className="spread mono" style={{ fontSize: 10.5 }}>
                  <span style={{ color: 'var(--ink-soft)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{mount.source}</span>
                  <span style={{ color: 'var(--ink-mute)' }}>→ {mount.destination}{mount.read_only ? ' · 只读' : ''}</span>
                </div>
              ))}
            </div>
          </div>

          <div className="stack gap-6">
            <span className="kicker">网络</span>
            <div className="stack gap-4">
              {detail.data.networks.map((network) => (
                <div key={network.name} className="spread mono" style={{ fontSize: 10.5 }}>
                  <span style={{ color: 'var(--ink-soft)' }}>{network.name}</span>
                  <span style={{ color: 'var(--ink-mute)' }}>{network.ipv4 || network.ipv6 || '—'}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      ) : null}
    </Drawer>
  );
}

function LogsDialog({ target, onClose }: { target: { node: string; id: string; engine: string; name: string } | null; onClose: () => void }) {
  const [tail, setTail] = useState('200');
  const [follow, setFollow] = useState(false);
  const query = useQuery({
    queryKey: ['logs', target?.node, target?.id, tail],
    enabled: Boolean(target),
    queryFn: async ({ signal }) =>
      value(
        await api.GET('/nodes/{id}/containers/{container_id}/logs', {
          signal,
          params: { path: { id: target!.node, container_id: target!.id }, query: { engine_id: target!.engine, tail: Number(tail) } },
        }),
      ),
    refetchInterval: follow ? 2000 : false,
  });
  useEffect(() => {
    if (!target) {
      setFollow(false);
      setTail('200');
    }
  }, [target]);

  return (
    <Dialog
      open={Boolean(target)}
      title={`${target?.name ?? ''} · 日志`}
      width="wide"
      onClose={onClose}
      footer={
        <>
          <Select aria-label="行数" value={tail} onChange={(event) => setTail(event.target.value)} options={[['100', '100 行'], ['200', '200 行'], ['500', '500 行'], ['1000', '1000 行']]} style={{ height: 28, width: 108, fontSize: 12 }} />
          <Button size="sm" aria-pressed={follow} variant={follow ? 'solid' : 'line'} onClick={() => setFollow((state_) => !state_)}>
            {follow ? '跟随中' : '每 2 秒跟随'}
          </Button>
          <span className="grow" />
          <Button size="sm" icon="refresh" busy={query.isFetching} onClick={() => void query.refetch()}>刷新</Button>
        </>
      }
    >
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      {query.isPending ? <Skeleton height={200} /> : null}
      {query.data ? (
        <>
          <pre className="logs">{query.data.text || '（无输出）'}</pre>
          <div className="row gap-14 mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>
            <span>读取于 {stamp(query.data.checked_at)}</span>
            <span>{query.data.truncated ? '已截断' : '完整'}</span>
          </div>
        </>
      ) : null}
    </Dialog>
  );
}

export { terminalStates, Icon };



