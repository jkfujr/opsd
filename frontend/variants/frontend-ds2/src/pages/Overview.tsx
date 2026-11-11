import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router';
import { api, value, type Schema } from '../api/client';
import { bytes, clock, level as levelOf, percent, ratio, speed, stamp } from '../lib/format';
import { firewallStates, readingStates, terminalStates } from '../lib/labels';
import { Docket, useOperations } from '../components/Operations';
import { Icon } from '../components/Icon';
import {
  Button,
  Empty,
  Failure,
  Led,
  Meter,
  PageHead,
  Select,
  Sheet,
  Skeleton,
  Sparkline,
  Trace,
} from '../components/ui';

export const overviewQuery = {
  queryKey: ['overview'],
  queryFn: async ({ signal }: { signal: AbortSignal }) => value(await api.GET('/overview', { signal })),
  refetchInterval: 3000,
};

function Metric({
  label,
  value: reading,
  unit,
  detail,
  coverage,
  spark,
  level: usage,
}: {
  label: string;
  value: string;
  unit?: string;
  detail: string;
  coverage?: number;
  spark?: number[];
  level?: number;
}) {
  return (
    <article className="metric">
      <div className="metric__top">
        <span className="kicker">{label}</span>
        {coverage !== undefined ? <span className="metric__cover">{coverage} 台有效</span> : null}
      </div>
      <div className="metric__value">
        {reading}
        {unit ? <small>{unit}</small> : null}
      </div>
      {usage !== undefined ? <Meter value={usage} levelHint={levelOf(usage)} /> : null}
      <div className="metric__foot">
        <div className="metric__detail">
          <span>{detail}</span>
        </div>
        {spark && spark.length > 2 ? (
          <div className="metric__spark">
            <Sparkline values={spark} width={84} height={30} />
          </div>
        ) : null}
      </div>
    </article>
  );
}

function LiveStrip() {
  const navigate = useNavigate();
  const query = useQuery(overviewQuery);
  const nodes = query.data?.nodes ?? [];
  return (
    <section className="live" aria-label="节点实时状态" aria-busy={query.isPending}>
      {query.isPending
        ? Array.from({ length: 9 }, (_, index) => (
            <div key={index} className="live__cell">
              <Skeleton height={12} width={44} />
              <Skeleton height={8} width={30} />
            </div>
          ))
        : nodes.map(({ node, reading }) => {
            const cpu = reading.metrics?.cpu_usage;
            const state = !node.connected ? 'off' : reading.state === 'ready' ? 'ok' : 'warn';
            return (
              <button
                key={node.id}
                type="button"
                className="live__cell"
                data-state={state}
                onClick={() => navigate(`/nodes/${node.id}`)}
                title={`${node.name} · ${node.region || '未设地域'} · ${node.connected ? readingStates[reading.state] : '离线'}`}
              >
                <span className="live__name">{node.name}</span>
                <span className="live__region">{node.region || '未设地域'}</span>
                <span className="live__bar">
                  <i style={{ width: `${Math.max(3, Math.min(100, cpu ?? (state === 'off' ? 100 : 0)))}%` }} />
                </span>
              </button>
            );
          })}
    </section>
  );
}

type SortKey = 'status' | 'cpu' | 'memory' | 'disk';

export function Overview() {
  const navigate = useNavigate();
  const operations = useOperations();
  const query = useQuery(overviewQuery);
  const history = useQuery({
    queryKey: ['monitoring', 'history', '24h', ''],
    queryFn: async ({ signal }) => value(await api.GET('/monitoring/history', { signal, params: { query: { range: '24h' } } })),
    refetchInterval: 180_000,
  });
  const [search, setSearch] = useState('');
  const [state, setState] = useState('all');
  const [sort, setSort] = useState<SortKey>('status');
  const [range, setRange] = useState<'24h' | '7d'>('24h');

  const data = query.data;
  const nodes = data?.nodes ?? [];
  const pending = data?.pending_operations ?? [];
  const online = nodes.filter((item) => item.node.connected).length;

  const points = history.data?.items ?? [];
  const spark = useMemo(() => {
    const tail = points.slice(-40);
    const pick = (read: (m: Schema['Metrics']) => number | undefined) =>
      tail.flatMap((point) => {
        const v = read(point.summary.metrics);
        return typeof v === 'number' ? [v] : [];
      });
    return {
      cpu: pick((m) => m.cpu_usage),
      mem: pick((m) => ratio(m.memory_used, m.memory_total)),
      disk: pick((m) => ratio(m.disk_used, m.disk_total)),
      net: pick((m) => m.network_rx),
    };
  }, [points]);

  const attention = nodes
    .filter((item) => !item.node.connected || item.reading.state !== 'ready' || item.reading.firewall_error)
    .map((item) => ({
      node: item.node,
      reason: !item.node.connected ? '节点离线' : item.reading.error || readingStates[item.reading.state] || item.reading.firewall_error,
    }));
  const pendingSorted = [...pending].sort(
    (a, b) => ['awaiting_confirmation', 'attention_required'].indexOf(a.state) - ['awaiting_confirmation', 'attention_required'].indexOf(b.state),
  );

  const rows = nodes
    .filter(({ node }) => node.name.toLowerCase().includes(search.trim().toLowerCase()))
    .filter(({ node }) => (state === 'all' ? true : node.connected === (state === 'online')))
    .sort((a, b) => {
      const priority = (item: Schema['OverviewNode']) =>
        (item.node.connected ? 0 : 2) + (pending.some((op) => op.node_id === item.node.id && !terminalStates.has(op.state)) ? 1 : 0);
      if (sort === 'status') return priority(b) - priority(a) || a.node.name.localeCompare(b.node.name, 'zh-CN');
      const read = (item: Schema['OverviewNode']) =>
        sort === 'cpu'
          ? item.reading.metrics?.cpu_usage
          : sort === 'memory'
            ? ratio(item.reading.metrics?.memory_used, item.reading.metrics?.memory_total)
            : ratio(item.reading.metrics?.disk_used, item.reading.metrics?.disk_total);
      return (read(b) ?? -1) - (read(a) ?? -1) || a.node.name.localeCompare(b.node.name, 'zh-CN');
    });

  const metrics = data?.summary.metrics;
  const coverage = data?.summary.valid_nodes;
  const memRatio = ratio(metrics?.memory_used, metrics?.memory_total);
  const diskRatio = ratio(metrics?.disk_used, metrics?.disk_total);

  return (
    <div className="page">
      <PageHead
        title="概览"
        meta={[
          <>集群 {nodes.length} 台</>,
          <>在线 {online}</>,
          <>未决 {pending.length}</>,
          <span key="t" className="row gap-6">
            <Led state={query.isError ? 'off' : query.isFetching ? 'warn' : 'on'} />
            {data ? `实时 · 更新于 ${clock(data.updated_at)}` : '读取中'}
          </span>,
        ]}
        tools={
          <Button size="sm" icon="refresh" busy={query.isFetching} onClick={() => void query.refetch()}>
            刷新
          </Button>
        }
      />

      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />

      <LiveStrip />

      <section className="metrics" aria-busy={query.isPending}>
        <Metric
          label="CPU"
          value={metrics?.cpu_usage !== undefined ? metrics.cpu_usage.toFixed(1) : '—'}
          unit="%"
          detail={`${metrics?.cpu_cores ?? '—'} 核 · ${coverage?.cpu ?? 0} 台有效`}
          coverage={coverage?.cpu}
          spark={spark.cpu}
          level={metrics?.cpu_usage}
        />
        <Metric
          label="内存"
          value={memRatio !== undefined ? memRatio.toFixed(1) : '—'}
          unit="%"
          detail={`${bytes(metrics?.memory_used)} / ${bytes(metrics?.memory_total)}`}
          coverage={coverage?.memory}
          spark={spark.mem}
          level={memRatio}
        />
        <Metric
          label="磁盘"
          value={diskRatio !== undefined ? diskRatio.toFixed(1) : '—'}
          unit="%"
          detail={`${bytes(metrics?.disk_used)} / ${bytes(metrics?.disk_total)}`}
          coverage={coverage?.disk}
          spark={spark.disk}
          level={diskRatio}
        />
        <Metric
          label="网络"
          value={metrics?.network_rx !== undefined ? bytes(metrics.network_rx).split(' ')[0] : '—'}
          unit={metrics?.network_rx !== undefined ? `${bytes(metrics.network_rx).split(' ')[1]}/s` : ''}
          detail={`发送 ${speed(metrics?.network_tx)}`}
          coverage={coverage?.network}
          spark={spark.net}
        />
      </section>

      <div className="overview__grid">
        <Sheet
          title="集群节点"
         
          flush
          tools={
            <>
              <input
                className="input"
                style={{ height: 30, width: 168 }}
                placeholder="搜索节点"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                aria-label="搜索节点"
              />
              <Select
                aria-label="状态"
                value={state}
                onChange={(event) => setState(event.target.value)}
                options={[
                  ['all', '全部状态'],
                  ['online', '在线'],
                  ['offline', '离线'],
                ]}
                style={{ height: 30, width: 108 }}
              />
              <Select
                aria-label="排序"
                value={sort}
                onChange={(event) => setSort(event.target.value as SortKey)}
                options={[
                  ['status', '状态优先'],
                  ['cpu', 'CPU'],
                  ['memory', '内存'],
                  ['disk', '磁盘'],
                ]}
                style={{ height: 30, width: 104 }}
              />
            </>
          }
        >
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">节点</th>
                  <th scope="col">状态</th>
                  <th scope="col" className="right">CPU</th>
                  <th scope="col" className="right">内存</th>
                  <th scope="col" className="right">磁盘</th>
                  <th scope="col" className="right">网络</th>
                  <th scope="col">防火墙</th>
                  <th scope="col" />
                </tr>
              </thead>
              <tbody>
                {query.isPending
                  ? Array.from({ length: 6 }, (_, index) => (
                      <tr key={index}>
                        {Array.from({ length: 8 }, (_, cell) => (
                          <td key={cell}>
                            <Skeleton height={11} width={cell === 0 ? 130 : 44} />
                          </td>
                        ))}
                      </tr>
                    ))
                  : rows.map(({ node, reading }) => (
                      <tr key={node.id} onClick={() => navigate(`/nodes/${node.id}`)} style={{ cursor: 'pointer' }}>
                        <td>
                          <div className="table__name">
                            <Link to={`/nodes/${node.id}`} onClick={(event) => event.stopPropagation()}>
                              {node.name}
                            </Link>
                            {node.local ? <span className="sub">本机</span> : null}
                          </div>
                          <div className="cell-sub">{node.region || '未设地域'}</div>
                        </td>
                        <td>
                          <span className="row gap-6">
                            <Led state={!node.connected ? 'off' : reading.state === 'ready' ? 'on' : 'warn'} />
                            <span style={{ fontSize: 12.5 }}>{node.connected ? readingStates[reading.state] : '离线'}</span>
                          </span>
                        </td>
                        <td className="right">
                          <span className="stack" style={{ alignItems: 'flex-end', gap: 4 }}>
                            <span className="num" style={{ fontSize: 12.5 }}>{percent(reading.metrics?.cpu_usage)}</span>
                            <span style={{ width: 52 }}><Meter value={reading.metrics?.cpu_usage} /></span>
                          </span>
                        </td>
                        <td className="right">
                          <span className="stack" style={{ alignItems: 'flex-end', gap: 4 }}>
                            <span className="num" style={{ fontSize: 12.5 }}>{percent(ratio(reading.metrics?.memory_used, reading.metrics?.memory_total))}</span>
                            <span style={{ width: 52 }}>
                              <Meter value={ratio(reading.metrics?.memory_used, reading.metrics?.memory_total)} />
                            </span>
                          </span>
                        </td>
                        <td className="right">
                          <span className="stack" style={{ alignItems: 'flex-end', gap: 4 }}>
                            <span className="num" style={{ fontSize: 12.5 }}>{percent(ratio(reading.metrics?.disk_used, reading.metrics?.disk_total))}</span>
                            <span style={{ width: 52 }}>
                              <Meter value={ratio(reading.metrics?.disk_used, reading.metrics?.disk_total)} />
                            </span>
                          </span>
                        </td>
                        <td className="right mono" style={{ fontSize: 10.5, lineHeight: 1.5 }}>
                          <div>↓ {speed(reading.metrics?.network_rx)}</div>
                          <div style={{ color: 'var(--ink-3)' }}>↑ {speed(reading.metrics?.network_tx)}</div>
                        </td>
                        <td>
                          <Link to={`/firewall?node=${node.id}`} onClick={(event) => event.stopPropagation()} style={{ fontSize: 12.5 }}>
                            {firewallStates[reading.firewall]}
                          </Link>
                        </td>
                        <td className="right">
                          <Icon name="chevron" width={14} height={14} style={{ color: 'var(--ink-3)' }} />
                        </td>
                      </tr>
                    ))}
                {!query.isPending && rows.length === 0 ? (
                  <tr>
                    <td colSpan={8}>
                      <Empty>{nodes.length ? '没有匹配的节点' : '还没有节点'}</Empty>
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </Sheet>

        <div className="stack gap-20">
          <Sheet
            title={`待签收 ${pending.length || ''}`.trim()}
           
            tools={
              <Button size="sm" variant="ghost" icon="arrow" onClick={() => operations.open()}>
                队列
              </Button>
            }
          >
            <div className="queue-list">
              {pendingSorted.length === 0 ? (
                <Empty>没有未决操作</Empty>
              ) : (
                pendingSorted.slice(0, 4).map((operation) => (
                  <Docket
                    key={operation.id}
                    operation={operation}
                    nodeName={nodes.find((item) => item.node.id === operation.node_id)?.node.name}
                    dense
                    onOpen={() => operations.open(operation.id)}
                  />
                ))
              )}
            </div>
          </Sheet>

          <Sheet title={`需要处理 ${attention.length || ''}`.trim()}>
            {attention.length === 0 ? (
              <Empty>没有需要处理的事项</Empty>
            ) : (
              <ul className="stack gap-2">
                {attention.slice(0, 6).map(({ node, reason }) => (
                  <li key={node.id}>
                    <Link
                      to={`/nodes/${node.id}`}
                      className="spread"
                      style={{ padding: '9px 2px', borderBottom: '1px solid var(--line)', fontSize: 12.5 }}
                    >
                      <span className="row gap-8" style={{ minWidth: 0 }}>
                        <Led state={node.connected ? 'warn' : 'off'} />
                        <b style={{ fontWeight: 500 }}>{node.name}</b>
                      </span>
                      <span style={{ fontSize: 11.5, color: 'var(--ink-3)' }}>{reason}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </Sheet>
        </div>
      </div>

      <Sheet
        title="资源趋势"
       
        tools={
          <div className="segmented" role="group" aria-label="时间范围">
            {(['24h', '7d'] as const).map((item) => (
              <button key={item} type="button" aria-pressed={range === item} onClick={() => setRange(item)}>
                {item === '24h' ? '24 小时' : '7 天'}
              </button>
            ))}
          </div>
        }
      >
        <TrendBody range={range} />
      </Sheet>
    </div>
  );
}

function TrendBody({ range }: { range: '24h' | '7d' }) {
  const query = useQuery({
    queryKey: ['monitoring', 'history', range, ''],
    queryFn: async ({ signal }) => value(await api.GET('/monitoring/history', { signal, params: { query: { range } } })),
    refetchInterval: 180_000,
  });
  const points = query.data?.items ?? [];
  const first = points[0]?.collected_at;
  const last = points[points.length - 1]?.collected_at;
  const label: [string, string] = [first ? stamp(first).slice(5, 16) : '', last ? stamp(last).slice(5, 16) : ''];
  const cpu = points.map((point) => point.summary.metrics.cpu_usage);
  const mem = points.map((point) => ratio(point.summary.metrics.memory_used, point.summary.metrics.memory_total));
  const rx = points.map((point) => point.summary.metrics.network_rx);
  const tx = points.map((point) => point.summary.metrics.network_tx);

  if (query.isPending) return <Skeleton height={230} />;
  if (!points.length) return <Empty>暂无监控数据</Empty>;

  return (
    <div className="stack gap-16">
      <div className="trends">
        <div className="chart">
          <div className="chart__head">
            <h3>CPU 使用率</h3>
            <span className="legend">
              <span>
                <i />
                当前 {percent(cpu[cpu.length - 1])}
              </span>
            </span>
          </div>
          <Trace series={[{ values: cpu, color: 'var(--ink)' }]} fill xLabels={label} aria="CPU 使用率趋势" height={92} ceiling={100} />
        </div>
        <div className="chart">
          <div className="chart__head">
            <h3>内存使用率</h3>
            <span className="legend">
              <span>
                <i />
                当前 {percent(mem[mem.length - 1])}
              </span>
            </span>
          </div>
          <Trace series={[{ values: mem, color: 'var(--ink)' }]} fill xLabels={label} aria="内存使用率趋势" height={92} ceiling={100} />
        </div>
        <div className="chart trends__wide">
          <div className="chart__head">
            <h3>网络吞吐</h3>
            <span className="legend">
              <span>
                <i className="in" />
                接收
              </span>
              <span>
                <i className="out" />
                发送
              </span>
            </span>
          </div>
          <Trace
            series={[
              { values: rx, color: 'var(--accent)' },
              { values: tx, color: 'var(--ink-3)', dashed: true },
            ]}
            xLabels={label}
            aria="网络吞吐趋势"
            height={100}
            yFormat={(v) => speed(v)}
          />
        </div>
      </div>
      <div className="row gap-14 mono" style={{ fontSize: 10.5, color: 'var(--ink-3)' }}>
        <span>{points.length} 点</span>
        <span>{query.data?.interval_minutes ?? 5} 分钟间隔</span>
      </div>
    </div>
  );
}
