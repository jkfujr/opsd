import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router';
import { Input } from '@appica/ui-react/input';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { Skeleton } from '@appica/ui-react/skeleton';
import { api, unwrap } from '../api/client';
import { useOperations, operationStates } from '../operations';
import { ChoiceField, date, Failure, LoadingRows, PageTitle, StatusLabel } from '../ui';
import { NodePublicAddresses } from '../network';
import { NodeSettings } from './NodeSettings';
import { firewallNames, overviewQuery, percent, ratio, readingNames, ResourceCards, speed, Trends, type OverviewNode } from '../monitoring';

function Status({ connected }: { connected: boolean }) {
  return <StatusLabel state={connected ? 'online' : 'offline'}>{connected ? '在线' : '离线'}</StatusLabel>;
}

export function Overview() {
  const query = useQuery(overviewQuery);
  const operations = useOperations();
  const [search, setSearch] = useState('');
  const [state, setState] = useState('all');
  const [sort, setSort] = useState('status');
  const data = query.data;
  const pending = data?.pending_operations ?? [];
  const attention = new Set(pending.filter((op) => ['awaiting_confirmation', 'attention_required'].includes(op.state)).map((op) => op.node_id));
  const nodes = data?.nodes ?? [];
  const online = nodes.filter((n) => n.node.connected).length;
  const fwCount = (value: string) => nodes.filter((n) => n.reading.firewall === value).length;
  const visible = nodes
    .filter(({ node }) => node.name.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()) && (state === 'all' || node.connected === (state === 'online')))
    .sort((a, b) => {
      const read = (item: OverviewNode) =>
        sort === 'cpu'
          ? item.reading.metrics?.cpu_usage
          : sort === 'memory'
            ? ratio(item.reading.metrics?.memory_used, item.reading.metrics?.memory_total)
            : ratio(item.reading.metrics?.disk_used, item.reading.metrics?.disk_total);
      const priority = (item: OverviewNode) => (item.node.connected ? 0 : 2) + (attention.has(item.node.id) ? 1 : 0);
      return (sort === 'status' ? priority(b) - priority(a) : (read(b) ?? -1) - (read(a) ?? -1)) || a.node.name.localeCompare(b.node.name, 'zh-CN');
    });

  const attentionItems = nodes.filter((n) => !n.node.connected || n.reading.state !== 'ready' || n.reading.firewall_error);

  return (
    <div className="space-y-4">
      <PageTitle>概览</PageTitle>
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      <div aria-busy={query.isPending} className="space-y-4">
        <section className="surface grid gap-4 p-4 sm:p-5 lg:grid-cols-3">
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
            <span className="text-sm font-medium text-foreground-muted">
              节点{' '}
              <strong className="ml-1.5 inline-block text-xl font-semibold tabular-nums text-foreground-intense">
                {query.isPending ? <Skeleton className="h-6 w-6" /> : data ? nodes.length : '—'}
              </strong>
            </span>
            <button
              type="button"
              disabled={!data}
              className="rounded-md px-2 py-1 text-sm font-medium text-foreground transition-colors hover:bg-background-subtle hover:text-foreground-intense"
              onClick={() => setState('online')}
            >
              在线 <span className="tabular-nums font-semibold text-success-intense">{data ? online : '—'}</span>
            </button>
            <button
              type="button"
              disabled={!data}
              className="rounded-md px-2 py-1 text-sm font-medium text-foreground transition-colors hover:bg-background-subtle hover:text-foreground-intense"
              onClick={() => setState('offline')}
            >
              离线 <span className="tabular-nums font-semibold text-foreground-muted">{data ? nodes.length - online : '—'}</span>
            </button>
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm lg:border-l lg:border-border lg:pl-5">
            <span className="font-medium text-foreground-muted">防火墙</span>
            {([
              ['enabled', '启用'],
              ['disabled', '停用'],
              ['none', '未安装'],
              ['unknown', '未知'],
            ] as const).map(([value, label]) => (
              <span key={value} className="whitespace-nowrap rounded-md bg-background-muted px-2 py-0.5 text-xs font-medium tabular-nums">
                {label} <strong className="ml-0.5 text-foreground-intense">{data ? fwCount(value) : '—'}</strong>
              </span>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm lg:border-l lg:border-border lg:pl-5">
            <button
              type="button"
              disabled={!data}
              className="rounded-md px-2 py-1 font-medium transition-colors hover:bg-background-subtle hover:text-foreground-intense"
              onClick={() => operations.show()}
            >
              待确认 <span className="tabular-nums font-semibold text-warning-intense">{data ? pending.filter((op) => op.state === 'awaiting_confirmation').length : '—'}</span>
            </button>
            <button
              type="button"
              disabled={!data}
              className="rounded-md px-2 py-1 font-medium transition-colors hover:bg-background-subtle hover:text-foreground-intense"
              onClick={() => operations.show()}
            >
              需处理 <span className="tabular-nums font-semibold text-error-intense">{data ? pending.filter((op) => op.state === 'attention_required').length : '—'}</span>
            </button>
            <span className="ml-auto text-xs tabular-nums text-foreground-muted">
              {query.isError ? '最近读数' : '更新于'} {data ? new Date(data.updated_at).toLocaleTimeString('zh-CN', { hour12: false }) : '—'}
            </span>
          </div>
        </section>

        <ResourceCards metrics={data?.summary.metrics} coverage={data?.summary.valid_nodes} loading={query.isPending} />

        <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(18rem,1fr)]">
          <Trends />
          <section className="surface min-w-0 p-4 sm:p-5">
            <div className="mb-3 flex items-center justify-between gap-2">
              <h2 className="text-base font-semibold tracking-tight text-foreground-intense">待处理事项</h2>
              {data && (attentionItems.length + pending.length) > 0 && (
                <span className="rounded-full bg-warning-subtle px-2 py-0.5 text-xs font-semibold tabular-nums text-warning-intense">
                  {attentionItems.length + pending.length}
                </span>
              )}
            </div>
            <ul className="max-h-80 divide-y divide-border overflow-y-auto text-sm">
              {attentionItems.map(({ node, reading }) => (
                <li key={node.id}>
                  <Link to={`/nodes/${node.id}`} className="block rounded-lg px-2.5 py-2.5 transition-colors hover:bg-background-muted">
                    <span className="block truncate font-medium text-foreground-intense">{node.name}</span>
                    <span className="mt-0.5 block text-xs text-foreground-muted">
                      {!node.connected ? '节点离线' : reading.error || readingNames[reading.state] || reading.firewall_error}
                    </span>
                  </Link>
                </li>
              ))}
              {pending.map((op) => (
                <li key={op.id}>
                  <button
                    type="button"
                    onClick={() => operations.show(op.id)}
                    className="w-full rounded-lg px-2.5 py-2.5 text-left transition-colors hover:bg-background-muted"
                  >
                    <span className="block truncate font-medium text-foreground-intense">
                      {nodes.find((n) => n.node.id === op.node_id)?.node.name ?? '节点操作'}
                    </span>
                    <span className="mt-0.5 block text-xs text-foreground-muted">
                      {operationStates[op.state]}
                      {op.result_pending_sync ? ' · 结果待同步' : ''}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
            {query.isPending && <Skeleton className="my-6 h-8 w-full" />}
            {!query.isError && data && !pending.length && !attentionItems.length && (
              <p className="py-8 text-center text-sm text-foreground-muted">暂无待处理事项</p>
            )}
          </section>
        </div>

        <section className="surface min-w-0 overflow-hidden">
          <div className="flex flex-wrap items-end gap-3 border-b border-border p-4 sm:px-5">
            <h2 className="mr-auto self-center text-base font-semibold tracking-tight text-foreground-intense">集群节点</h2>
            <Input
              aria-label="搜索节点"
              placeholder="搜索节点"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full sm:w-52"
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
          <div className="overflow-x-auto">
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
                    <TableRow key={node.id}>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Link
                            to={`/nodes/${node.id}`}
                            className="block max-w-56 truncate font-medium text-foreground-intense hover:text-primary hover:underline"
                            title={node.name}
                          >
                            {node.name}
                          </Link>
                          {node.local && (
                            <span className="rounded bg-background-muted px-1.5 py-0.5 text-[11px] font-medium text-foreground-muted">
                              本机
                            </span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>
                        <Status connected={node.connected} />
                        {reading.state !== 'ready' && reading.state !== 'offline' && (
                          <div className="mt-1 text-xs text-foreground-muted">{readingNames[reading.state]}</div>
                        )}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs tabular-nums">{percent(reading.metrics?.cpu_usage)}</TableCell>
                      <TableCell className="text-right font-mono text-xs tabular-nums">
                        {percent(ratio(reading.metrics?.memory_used, reading.metrics?.memory_total))}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs tabular-nums">
                        {percent(ratio(reading.metrics?.disk_used, reading.metrics?.disk_total))}
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-right font-mono text-xs tabular-nums">
                        <span className="block">↓ {speed(reading.metrics?.network_rx)}</span>
                        <span className="block text-foreground-muted">↑ {speed(reading.metrics?.network_tx)}</span>
                      </TableCell>
                      <TableCell>
                        <Link to={`/firewall?node=${node.id}`} className="whitespace-nowrap font-medium text-foreground hover:text-primary hover:underline">
                          {firewallNames[reading.firewall]}
                        </Link>
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-xs tabular-nums text-foreground-muted">
                        {date(reading.metrics?.collected_at)}
                      </TableCell>
                    </TableRow>
                  ))
                )}
                {!query.isError && data && !visible.length && (
                  <TableRow>
                    <TableCell colSpan={8} className="py-10 text-center text-foreground-muted">
                      {nodes.length ? '没有匹配的节点' : '暂无节点'}
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
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
    queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{id}', { signal, params: { path: { id } } })),
  });
  const overview = useQuery(overviewQuery);
  const reading = overview.data?.nodes.find((item) => item.node.id === id)?.reading;

  return (
    <div className="space-y-4">
      <PageTitle>节点详情</PageTitle>
      <Failure error={node.error} retry={() => void node.refetch()} busy={node.isFetching} />
      <Failure error={overview.error} retry={() => void overview.refetch()} busy={overview.isFetching} />
      <section className="surface flex flex-wrap items-center gap-4 p-5" aria-busy={node.isPending}>
        <div className="mr-auto min-w-0">
          <div className="flex items-center gap-2.5">
            <h2 className="break-all text-lg font-semibold tracking-tight text-foreground-intense">
              {node.isPending ? <Skeleton className="h-7 w-48" /> : node.data?.name ?? '—'}
            </h2>
            {node.data?.local && (
              <span className="rounded bg-background-muted px-2 py-0.5 text-xs font-medium text-foreground-muted">本机</span>
            )}
          </div>
          <p className="mt-1 text-xs tabular-nums text-foreground-muted">最近连接 {date(node.data?.last_seen_at)}</p>
        </div>
        {node.data && <Status connected={node.data.connected} />}
        <Link
          to={`/firewall?node=${id}`}
          className="rounded-lg border border-border bg-background-muted px-3 py-1.5 text-sm font-medium text-foreground-intense transition-colors hover:bg-background-subtle"
        >
          防火墙{reading ? ` · ${firewallNames[reading.firewall]}` : ''}
        </Link>
      </section>
      {node.data?.revoked_at ? (
        <p role="status" className="rounded-xl border border-border bg-background p-4 text-sm text-foreground-muted">
          节点已撤销
        </p>
      ) : (
        <>
          {reading && reading.state !== 'ready' && (
            <p role="status" className="text-sm text-foreground-muted">
              {reading.error || readingNames[reading.state]}
              {reading.metrics ? `，最近采集 ${date(reading.metrics.collected_at)}` : ''}
            </p>
          )}
          {reading?.metrics?.errors.map((error) => (
            <p key={error} role="status" className="text-sm text-foreground-muted">
              {error}
            </p>
          ))}
          {reading?.firewall_error && (
            <p role="status" className="text-sm text-foreground-muted">
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
