import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router';
import { api, value, type Schema } from '../api/client';
import { bytes, clock, level as levelOf, percent, ratio, speed, stamp } from '../lib/format';
import { firewallStates, readingStates, terminalStates, operationStates } from '../lib/labels';
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

export function Gauge({ value: reading, size = 'md' }: { value?: number; size?: 'sm' | 'md' }) {
  const lvl = levelOf(reading);
  return (
    <span className="stack" style={{ gap: 3, alignItems: 'flex-end' }}>
      <span className="num" style={{ fontSize: size === 'sm' ? 12 : 12.5 }}>
        {percent(reading)}
      </span>
      <span style={{ width: size === 'sm' ? 34 : 44 }}>
        <Meter value={reading} levelHint={lvl} />
      </span>
    </span>
  );
}

function Metric({
  label,
  value: reading,
  unit,
  detail,
  spark,
  level: usage,
}: {
  label: string;
  value: string;
  unit?: string;
  detail: string;
  spark?: number[];
  level?: number;
}) {
  return (
    <article className="metric">
      <div className="metric__top">
        <span className="kicker">{label}</span>
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
            <Sparkline values={spark} width={74} height={26} />
          </div>
        ) : null}
      </div>
    </article>
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

  const historyPoints = history.data?.items ?? [];
  const spark = useMemo(() => {
    const tail = historyPoints.slice(-40);
    const pick = (read: (m: Schema['Metrics']) => number | undefined) => tail.flatMap((point) => {
      const v = read(point.summary.metrics);
      return typeof v === 'number' ? [v] : [];
    });
    return {
      cpu: pick((m) => m.cpu_usage),
      mem: pick((m) => ratio(m.memory_used, m.memory_total)),
      disk: pick((m) => ratio(m.disk_used, m.disk_total)),
      net: pick((m) => m.network_rx),
    };
  }, [historyPoints]);

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
        tools={
          <Button size="sm" icon="refresh" busy={query.isFetching} onClick={() => void query.refetch()}>
            刷新读数
          </Button>
        }
      />

      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />

      <section className="metrics" aria-busy={query.isPending}>
        <Metric
          label="CPU"
          value={metrics?.cpu_usage !== undefined ? metrics.cpu_usage.toFixed(1) : '—'}
          unit="%"
          detail={`${metrics?.cpu_cores ?? '—'} 核 · ${coverage?.cpu ?? 0} 台有效`}
          spark={spark.cpu}
          level={metrics?.cpu_usage}
        />
        <Metric
          label="内存"
          value={memRatio !== undefined ? memRatio.toFixed(1) : '—'}
          unit="%"
          detail={`${bytes(metrics?.memory_used)} / ${bytes(metrics?.memory_total)}`}
          spark={spark.mem}
          level={memRatio}
        />
        <Metric
          label="磁盘"
          value={diskRatio !== undefined ? diskRatio.toFixed(1) : '—'}
          unit="%"
          detail={`${bytes(metrics?.disk_used)} / ${bytes(metrics?.disk_total)}`}
          spark={spark.disk}
          level={diskRatio}
        />
        <Metric
          label="网络"
          value={metrics?.network_rx !== undefined ? bytes(metrics.network_rx).split(' ')[0] : '—'}
          unit={metrics?.network_rx !== undefined ? `${bytes(metrics.network_rx).split(' ')[1]}/s` : ''}
          detail={`发送 ${speed(metrics?.network_tx)}`}
          spark={spark.net}
        />
      </section>

      <div className="overview__grid">
        <Sheet
          title="集群节点"

          flush
          tools={
            <>
              <div className="row gap-6">
                <input
                  className="input"
                  style={{ height: 28, width: 150, fontSize: 12 }}
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
                    ['all', '全部'],
                    ['online', '在线'],
                    ['offline', '离线'],
                  ]}
                  style={{ height: 28, width: 84, fontSize: 12 }}
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
                  style={{ height: 28, width: 96, fontSize: 12 }}
                />
              </div>
            </>
          }
        >
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">节点</th>
                  <th scope="col">地域</th>
                  <th scope="col">状态</th>
                  <th scope="col" className="right">CPU</th>
                  <th scope="col" className="right">内存</th>
                  <th scope="col" className="right">磁盘</th>
                  <th scope="col" className="right">网络 收 / 发</th>
                  <th scope="col">防火墙</th>
                  <th scope="col">采集</th>
                  <th scope="col" />
                </tr>
              </thead>
              <tbody>
                {query.isPending
                  ? Array.from({ length: 5 }, (_, index) => (
                      <tr key={index}>
                        {Array.from({ length: 10 }, (_, cell) => (
                          <td key={cell}>
                            <Skeleton height={11} width={cell === 0 ? 120 : 46} />
                          </td>
                        ))}
                      </tr>
                    ))
                  : rows.map(({ node, reading }) => (
                      <tr key={node.id} onClick={() => navigate(`/nodes/${node.id}`)} style={{ cursor: 'pointer' }}>
                        <td>
                          <div className="table__name">
                            <Link to={`/nodes/${node.id}`} onClick={(e) => e.stopPropagation()}>
                              {node.name}
                            </Link>
                            {node.local ? <span className="sub">本机</span> : null}
                          </div>
                        </td>
                        <td className="mono" style={{ fontSize: 11, color: 'var(--ink-mute)' }}>
                          {node.region || '—'}
                        </td>
                        <td>
                          <span className="row gap-6">
                            <Led state={node.connected ? (reading.state === 'ready' ? 'on' : 'warn') : 'off'} />
                            <span style={{ fontSize: 12 }}>{node.connected ? readingStates[reading.state] : '离线'}</span>
                          </span>
                        </td>
                        <td className="right">
                          <Gauge value={reading.metrics?.cpu_usage} />
                        </td>
                        <td className="right">
                          <Gauge value={ratio(reading.metrics?.memory_used, reading.metrics?.memory_total)} />
                        </td>
                        <td className="right">
                          <Gauge value={ratio(reading.metrics?.disk_used, reading.metrics?.disk_total)} />
                        </td>
                        <td className="right mono" style={{ fontSize: 10.5, lineHeight: 1.5 }}>
                          <div>↓ {speed(reading.metrics?.network_rx)}</div>
                          <div style={{ color: 'var(--ink-mute)' }}>↑ {speed(reading.metrics?.network_tx)}</div>
                        </td>
                        <td>
                          <Link
                            to={`/firewall?node=${node.id}`}
                            onClick={(e) => e.stopPropagation()}
                            style={{ fontSize: 12, color: reading.firewall === 'enabled' ? 'var(--ink)' : 'var(--ink-mute)' }}
                          >
                            {firewallStates[reading.firewall]}
                          </Link>
                        </td>
                        <td className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>
                          {reading.metrics ? clock(reading.metrics.collected_at) : '—'}
                        </td>
                        <td className="right">
                          <Icon name="chevron" width={13} height={13} style={{ color: 'var(--ink-mute)' }} />
                        </td>
                      </tr>
                    ))}
                {!query.isPending && rows.length === 0 ? (
                  <tr>
                    <td colSpan={10}>
                      <Empty>
                        {nodes.length ? '没有匹配的节点' : '还没有节点'}
                      </Empty>
                    </td>
                  </tr>
                ) : null}
              </tbody>
              <tfoot>
                <tr>
                  <td className="table-foot" colSpan={10}>
                    共 {nodes.length} 台 · 在线 {online} · 未决 {pending.length}
                  </td>
                </tr>
              </tfoot>
            </table>
          </div>
        </Sheet>

        <div className="stack gap-12">
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
              <ul className="stack gap-1">
                {attention.slice(0, 6).map(({ node, reason }) => (
                  <li key={node.id}>
                    <Link
                      to={`/nodes/${node.id}`}
                      className="spread"
                      style={{ padding: '7px 2px', borderBottom: '1px solid var(--rule-soft)', fontSize: 12.5 }}
                    >
                      <span className="row gap-8" style={{ minWidth: 0 }}>
                        <Led state={node.connected ? 'warn' : 'off'} />
                        <b style={{ fontWeight: 500 }}>{node.name}</b>
                      </span>
                      <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
                        {reason}
                      </span>
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

  if (query.isPending) return <Skeleton height={220} />;
  if (!points.length) return <Empty>暂无监控数据</Empty>;

  return (
    <div className="stack gap-16">
      <div className="trends">
        <div className="chart">
          <div className="chart__head">
            <h3>CPU 使用率</h3>
            <span className="legend">
              <span><i />当前 {percent(cpu[cpu.length - 1])}</span>
            </span>
          </div>
          <Trace series={[{ values: cpu, color: 'var(--ink)' }]} fill xLabels={label} aria="CPU 使用率趋势" height={86} ceiling={100} />
        </div>
        <div className="chart">
          <div className="chart__head">
            <h3>内存使用率</h3>
            <span className="legend">
              <span><i />当前 {percent(mem[mem.length - 1])}</span>
            </span>
          </div>
          <Trace series={[{ values: mem, color: 'var(--ink)' }]} fill xLabels={label} aria="内存使用率趋势" height={86} ceiling={100} />
        </div>
        <div className="chart trends__wide">
          <div className="chart__head">
            <h3>网络吞吐</h3>
            <span className="legend">
              <span><i className="in" />接收</span>
              <span><i className="out" />发送</span>
            </span>
          </div>
          <Trace
            series={[
              { values: rx, color: 'var(--carbon)' },
              { values: tx, color: 'var(--ink)', dashed: true },
            ]}
            xLabels={label}
            aria="网络吞吐趋势"
            height={96}
            yFormat={(v) => speed(v)}
          />
        </div>
      </div>
      <div className="row gap-14 mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>
        <span>采样 {points.length} 点</span>
        <span>间隔 {query.data?.interval_minutes ?? 5} 分钟</span>
      </div>
    </div>
  );
}

export { operationStates };





