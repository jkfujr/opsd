import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router';
import { api, unwrap } from '../api/client';
import { useOperations, operationStates, OperationStepStrip } from '../operations';
import {
  ChoiceField,
  date,
  Failure,
  Input,
  LoadingRows,
  PageTitle,
  Skeleton,
  StatusLabel,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../ui';
import { NodePublicAddresses } from '../network';
import { NodeSettings } from './NodeSettings';
import {
  firewallNames,
  overviewQuery,
  percent,
  ratio,
  readingNames,
  ResourceCards,
  speed,
  Trends,
  type OverviewNode,
} from '../monitoring';

function Status({ connected }: { connected: boolean }) {
  return (
    <StatusLabel state={connected ? 'online' : 'offline'}>
      {connected ? '在线' : '离线'}
    </StatusLabel>
  );
}

export function Overview() {
  const query = useQuery(overviewQuery);
  const operations = useOperations();
  const [search, setSearch] = useState('');
  const [state, setState] = useState('all');
  const [sort, setSort] = useState('status');
  const data = query.data;
  const pending = data?.pending_operations ?? [];
  const attention = new Set(
    pending
      .filter((op) => ['awaiting_confirmation', 'attention_required'].includes(op.state))
      .map((op) => op.node_id),
  );
  const nodes = data?.nodes ?? [];
  const online = nodes.filter((n) => n.node.connected).length;
  const fwCount = (value: string) =>
    nodes.filter((n) => n.reading.firewall === value).length;

  const visible = nodes
    .filter(
      ({ node }) =>
        node.name.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()) &&
        (state === 'all' || node.connected === (state === 'online')),
    )
    .sort((a, b) => {
      const read = (item: OverviewNode) =>
        sort === 'cpu'
          ? item.reading.metrics?.cpu_usage
          : sort === 'memory'
            ? ratio(item.reading.metrics?.memory_used, item.reading.metrics?.memory_total)
            : ratio(item.reading.metrics?.disk_used, item.reading.metrics?.disk_total);
      const priority = (item: OverviewNode) =>
        (item.node.connected ? 0 : 2) + (attention.has(item.node.id) ? 1 : 0);
      return (
        (sort === 'status'
          ? priority(b) - priority(a)
          : (read(b) ?? -1) - (read(a) ?? -1)) ||
        a.node.name.localeCompare(b.node.name, 'zh-CN')
      );
    });

  return (
    <div className="space-y-4">
      <PageTitle>概览</PageTitle>
      <Failure
        error={query.error}
        retry={() => void query.refetch()}
        busy={query.isFetching}
      />
      <div aria-busy={query.isPending} className="space-y-4">
        {/* Top Swiss Signal Status Strip */}
        <section className="swiss-strip grid-cols-1 divide-y divide-[var(--grid-rule)] lg:grid-cols-3 lg:divide-x lg:divide-y-0">
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2 p-4">
            <span className="text-sm font-bold text-[var(--ink-muted)]">
              节点{' '}
              <strong className="mono-num ml-1 inline-block text-xl font-extrabold text-[var(--ink-strong)]">
                {query.isPending ? (
                  <Skeleton className="h-6 w-6" />
                ) : data ? (
                  nodes.length
                ) : (
                  '—'
                )}
              </strong>
            </span>
            <button
              type="button"
              disabled={!data}
              className="mono-num cursor-pointer text-sm font-semibold text-[var(--signal-emerald)] hover:underline disabled:cursor-not-allowed"
              onClick={() => setState('online')}
            >
              在线 {data ? online : '—'}
            </button>
            <button
              type="button"
              disabled={!data}
              className="mono-num cursor-pointer text-sm font-semibold text-[var(--signal-red)] hover:underline disabled:cursor-not-allowed"
              onClick={() => setState('offline')}
            >
              离线 {data ? nodes.length - online : '—'}
            </button>
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 p-4 text-sm">
            <span className="font-bold text-[var(--ink-muted)]">防火墙</span>
            {([
              ['enabled', '启用'],
              ['disabled', '停用'],
              ['none', '未安装'],
              ['unknown', '未知'],
            ] as const).map(([value, label]) => (
              <span key={value} className="mono-num whitespace-nowrap">
                {label} <strong>{data ? fwCount(value) : '—'}</strong>
              </span>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2 p-4 text-sm">
            <button
              type="button"
              disabled={!data}
              className="mono-num cursor-pointer font-semibold hover:underline disabled:cursor-not-allowed"
              onClick={() => operations.show()}
            >
              待确认{' '}
              <strong className="text-[var(--signal-red)]">
                {data
                  ? pending.filter((op) => op.state === 'awaiting_confirmation').length
                  : '—'}
              </strong>
            </button>
            <button
              type="button"
              disabled={!data}
              className="mono-num cursor-pointer font-semibold hover:underline disabled:cursor-not-allowed"
              onClick={() => operations.show()}
            >
              需处理{' '}
              <strong className="text-[var(--signal-amber)]">
                {data
                  ? pending.filter((op) => op.state === 'attention_required').length
                  : '—'}
              </strong>
            </button>
            <span className="mono-num ml-auto text-xs text-[var(--ink-muted)]">
              {query.isError ? '最近读数' : '更新于'}{' '}
              {data
                ? new Date(data.updated_at).toLocaleTimeString('zh-CN', {
                    hour12: false,
                  })
                : '—'}
            </span>
          </div>
        </section>

        {/* Unboxed Resource Telemetry Band */}
        <ResourceCards
          metrics={data?.summary.metrics}
          coverage={data?.summary.valid_nodes}
          loading={query.isPending}
        />

        {/* Middle Split: Trends & Persistent Action Queue */}
        <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(18rem,1fr)]">
          <Trends />
          <section className="swiss-panel min-w-0">
            <div className="mb-3 flex items-center justify-between border-b border-[var(--grid-rule)] pb-3">
              <h2 className="text-base font-bold text-[var(--ink-strong)]">待处理事项</h2>
              <span className="mono-num text-xs text-[var(--ink-muted)]">
                {pending.length} 项队列
              </span>
            </div>
            <ul className="max-h-80 divide-y divide-[var(--grid-rule)] overflow-y-auto text-sm">
              {nodes
                .filter(
                  (n) =>
                    !n.node.connected ||
                    n.reading.state !== 'ready' ||
                    n.reading.firewall_error,
                )
                .map(({ node, reading }) => (
                  <li key={node.id}>
                    <Link
                      to={`/nodes/${node.id}`}
                      className="block px-2 py-3 hover:bg-[var(--bg-elevated)]"
                    >
                      <span className="block truncate font-bold text-[var(--ink-strong)]">
                        {node.name}
                      </span>
                      <span className="mt-1 block text-xs text-[var(--signal-red-ink)]">
                        {!node.connected
                          ? '节点离线'
                          : reading.error ||
                            readingNames[reading.state] ||
                            reading.firewall_error}
                      </span>
                    </Link>
                  </li>
                ))}
              {pending.map((op) => (
                <li key={op.id}>
                  <button
                    type="button"
                    onClick={() => operations.show(op.id)}
                    className="w-full cursor-pointer px-2 py-3 text-left hover:bg-[var(--bg-elevated)]"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate font-bold text-[var(--ink-strong)]">
                        {nodes.find((n) => n.node.id === op.node_id)?.node.name ??
                          '节点操作'}
                      </span>
                      <StatusLabel state={op.state}>
                        {operationStates[op.state]}
                      </StatusLabel>
                    </div>
                    <span className="mono-num mt-1 block text-xs text-[var(--ink-muted)]">
                      {op.result_pending_sync ? '结果待同步' : date(op.created_at)}
                    </span>
                    <OperationStepStrip op={op} />
                  </button>
                </li>
              ))}
            </ul>
            {query.isPending && <Skeleton className="my-6 h-8 w-full" />}
            {!query.isError &&
              data &&
              !pending.length &&
              !nodes.some(
                (n) =>
                  !n.node.connected ||
                  n.reading.state !== 'ready' ||
                  n.reading.firewall_error,
              ) && (
                <p className="py-6 text-sm text-[var(--ink-muted)]">暂无待处理事项</p>
              )}
          </section>
        </div>

        {/* Peer Node Matrix (with identity plates from pop-culture-shelf raise) */}
        <section className="swiss-board min-w-0 overflow-hidden">
          <div className="flex flex-wrap items-end gap-3 border-b border-[var(--grid-strong)] bg-[var(--bg-elevated)] p-4">
            <h2 className="mr-auto self-center text-base font-bold text-[var(--ink-strong)]">
              集群节点
            </h2>
            <Input
              aria-label="搜索节点"
              placeholder="搜索节点"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full sm:w-48"
            />
            <div className="w-32">
              <ChoiceField
                label="状态"
                value={state}
                options={[
                  ['all', '全部'],
                  ['online', '在线'],
                  ['offline', '离线'],
                ]}
                onChange={setState}
              />
            </div>
            <div className="w-36">
              <ChoiceField
                label="排序"
                value={sort}
                options={[
                  ['status', '状态优先'],
                  ['cpu', 'CPU 使用率'],
                  ['memory', '内存使用率'],
                  ['disk', '磁盘使用率'],
                ]}
                onChange={setSort}
              />
            </div>
          </div>
          <Table className="overview-table min-w-240" hoverableRows>
            <TableHeader>
              <TableRow>
                <TableHead>节点</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">CPU</TableHead>
                <TableHead className="text-right">内存</TableHead>
                <TableHead className="text-right">磁盘</TableHead>
                <TableHead className="text-right">网络收发</TableHead>
                <TableHead>防火墙</TableHead>
                <TableHead>最近采集</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.isPending ? (
                <LoadingRows columns={8} />
              ) : (
                visible.map(({ node, reading }) => (
                  <TableRow
                    key={node.id}
                    className={!node.connected ? 'opacity-70' : undefined}
                  >
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-2">
                        <Link
                          to={`/nodes/${node.id}`}
                          className="max-w-56 truncate font-bold text-[var(--ink-strong)] hover:underline"
                          title={node.name}
                        >
                          {node.name}
                        </Link>
                        {node.local && (
                          <span className="mono-num border border-[var(--grid-strong)] bg-[var(--bg-elevated)] px-1.5 py-0.5 text-[11px] font-bold text-[var(--ink-strong)]">
                            本机
                          </span>
                        )}
                        {node.region && (
                          <span className="mono-num border border-[var(--grid-rule)] px-1.5 py-0.5 text-[11px] text-[var(--ink-muted)]">
                            {node.region}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Status connected={node.connected} />
                      {reading.state !== 'ready' && reading.state !== 'offline' && (
                        <div className="mt-1 text-xs text-[var(--ink-muted)]">
                          {readingNames[reading.state]}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="mono-num text-right font-semibold">
                      {percent(reading.metrics?.cpu_usage)}
                    </TableCell>
                    <TableCell className="mono-num text-right font-semibold">
                      {percent(
                        ratio(
                          reading.metrics?.memory_used,
                          reading.metrics?.memory_total,
                        ),
                      )}
                    </TableCell>
                    <TableCell className="mono-num text-right font-semibold">
                      {percent(
                        ratio(reading.metrics?.disk_used, reading.metrics?.disk_total),
                      )}
                    </TableCell>
                    <TableCell className="mono-num whitespace-nowrap text-right text-xs">
                      <span className="block font-medium">
                        ↓ {speed(reading.metrics?.network_rx)}
                      </span>
                      <span className="block text-[var(--ink-muted)]">
                        ↑ {speed(reading.metrics?.network_tx)}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Link
                        to={`/firewall?node=${node.id}`}
                        className="whitespace-nowrap font-medium hover:underline"
                      >
                        {firewallNames[reading.firewall]}
                      </Link>
                    </TableCell>
                    <TableCell className="mono-num whitespace-nowrap text-xs text-[var(--ink-muted)]">
                      {date(reading.metrics?.collected_at)}
                    </TableCell>
                  </TableRow>
                ))
              )}
              {!query.isError && data && !visible.length && (
                <TableRow>
                  <TableCell
                    colSpan={8}
                    className="py-8 text-center text-[var(--ink-muted)]"
                  >
                    {nodes.length ? '没有匹配的节点' : '暂无节点'}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </section>
      </div>
    </div>
  );
}

export function NodeDetail() {
  const { id = '' } = useParams();
  return <NodeDetailContent key={id} id={id} />;
}

function NodeDetailContent({ id }: { id: string }) {
  const node = useQuery({
    queryKey: ['nodes', id],
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/nodes/{id}', { signal, params: { path: { id } } })),
  });
  const overview = useQuery(overviewQuery);
  const reading = overview.data?.nodes.find((item) => item.node.id === id)?.reading;

  return (
    <div className="space-y-4">
      <PageTitle>节点详情</PageTitle>
      <Failure
        error={node.error}
        retry={() => void node.refetch()}
        busy={node.isFetching}
      />
      <Failure
        error={overview.error}
        retry={() => void overview.refetch()}
        busy={overview.isFetching}
      />
      <section
        className="swiss-board flex flex-wrap items-center gap-4 p-5"
        aria-busy={node.isPending}
      >
        <div className="mr-auto min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="break-all text-lg font-extrabold text-[var(--ink-strong)]">
              {node.isPending ? <Skeleton className="h-7 w-48" /> : node.data?.name ?? '—'}
            </h2>
            {node.data?.local && (
              <span className="mono-num border border-[var(--grid-strong)] bg-[var(--bg-elevated)] px-2 py-0.5 text-xs font-bold">
                本机
              </span>
            )}
            {node.data?.region && (
              <span className="mono-num border border-[var(--grid-rule)] px-2 py-0.5 text-xs text-[var(--ink-muted)]">
                {node.data.region}
              </span>
            )}
          </div>
          <p className="mono-num mt-1 text-xs text-[var(--ink-muted)]">
            最近连接 {date(node.data?.last_seen_at)}
          </p>
        </div>
        {node.data && <Status connected={node.data.connected} />}
        <Link
          to={`/tools?node=${id}`}
          className="text-sm font-semibold text-[var(--ink-strong)] hover:underline"
        >
          终端与任务
        </Link>
        <Link
          to={`/firewall?node=${id}`}
          className="text-sm font-semibold text-[var(--signal-emerald)] hover:underline"
        >
          防火墙{reading ? ` · ${firewallNames[reading.firewall]}` : ''}
        </Link>
      </section>
      {node.data?.revoked_at ? (
        <p role="status" className="swiss-panel text-sm font-semibold">
          节点已撤销
        </p>
      ) : (
        <>
          {reading && reading.state !== 'ready' && (
            <p role="status" className="text-sm text-[var(--ink-muted)]">
              {reading.error || readingNames[reading.state]}
              {reading.metrics
                ? `，最近采集 ${date(reading.metrics.collected_at)}`
                : ''}
            </p>
          )}
          {reading?.metrics?.errors.map((error) => (
            <p key={error} role="status" className="text-sm text-[var(--ink-muted)]">
              {error}
            </p>
          ))}
          {reading?.firewall_error && (
            <p role="status" className="text-sm text-[var(--ink-muted)]">
              {reading.firewall_error}
            </p>
          )}
          <ResourceCards metrics={reading?.metrics} loading={overview.isPending} />
          <NodeSettings key={id} id={id} />
          <NodePublicAddresses key={`public/${id}`} id={id} />
          <Trends nodeID={id} />
        </>
      )}
    </div>
  );
}
