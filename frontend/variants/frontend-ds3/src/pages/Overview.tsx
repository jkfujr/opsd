import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router';
import { api, value, type Schema } from '../api/client';
import { bytes, clock, percent, ratio, speed, stamp } from '../lib/format';
import { firewallStates, readingStates } from '../lib/labels';
import { useWorld } from '../lib/world';
import { FleetList } from '../components/Fleet';
import { Icon } from '../components/Icon';
import { Button, Failure, Led, Sheet, Skeleton, Trace, useSession } from '../components/ui';

const overviewQuery = {
  queryKey: ['overview'],
  queryFn: async ({ signal }: { signal: AbortSignal }) => value(await api.GET('/overview', { signal })),
  refetchInterval: 3000,
};

function useNodeName(nodeId: string) {
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  return nodes.data?.items.find((node) => node.id === nodeId)?.name ?? nodeId.slice(0, 8);
}
void useNodeName;

function MetricCell({ value, kind }: { value?: number; kind: 'cpu' | 'mem' | 'disk' }) {
  const world = useWorld();
  if (world === 'board') {
    const cls = value === undefined ? '' : value >= 90 ? 'board-val--bad' : value >= 75 ? 'board-val--warn' : 'board-val--ok';
    return <span className={`board-val ${cls}`}>{percent(value, 0)}</span>;
  }
  if (world === 'lab') {
    const hi = value !== undefined && value >= 90;
    const warn = value !== undefined && value >= 75 && value < 90;
    return (
      <span className={`labval${hi ? ' labval--hi' : warn ? ' labval--warn' : ''}`}>
        {percent(value, 0)}
        <span className="arrow">{hi ? '↑' : warn ? '△' : '—'}</span>
        <span className="ref">{kind === 'cpu' ? '<90' : kind === 'mem' ? '<90' : '<75'}</span>
      </span>
    );
  }
  return (
    <span className="num" style={{ fontSize: 12 }}>
      {percent(value, 0)}
    </span>
  );
}

export function Overview() {
  const navigate = useNavigate();
  const world = useWorld();
  const session = useSession();
  const [range, setRange] = useState<'24h' | '7d'>('24h');
  const query = useQuery(overviewQuery);
  const history = useQuery({
    queryKey: ['monitoring', 'history', '24h', ''],
    queryFn: async ({ signal }) => value(await api.GET('/monitoring/history', { signal, params: { query: { range: '24h' } } })),
    refetchInterval: 180_000,
  });

  const data = query.data;
  const nodes = data?.nodes ?? [];
  const pending = data?.pending_operations ?? [];

  const previousOrder = useRef<string[]>([]);
  const [moved, setMoved] = useState<Set<string>>(new Set());
  const rows = useMemo(() => {
    const attention = (item: Schema['OverviewNode']) =>
      (item.node.connected ? 0 : 2) + (pending.some((op) => op.node_id === item.node.id && ['awaiting_confirmation', 'attention_required'].includes(op.state)) ? 1 : 0);
    return [...nodes].sort(
      (a, b) =>
        attention(b) - attention(a) ||
        Math.max(
          ratio(b.reading.metrics?.memory_used, b.reading.metrics?.memory_total) ?? 0,
          b.reading.metrics?.cpu_usage ?? 0,
        ) -
          Math.max(
            ratio(a.reading.metrics?.memory_used, a.reading.metrics?.memory_total) ?? 0,
            a.reading.metrics?.cpu_usage ?? 0,
          ) ||
        a.node.name.localeCompare(b.node.name, 'zh-CN'),
    );
  }, [nodes, pending]);

  useMemo(() => {
    const order = rows.map((row) => row.node.id);
    if (previousOrder.current.length && previousOrder.current.join() !== order.join()) {
      const before = new Map(previousOrder.current.map((id, index) => [id, index]));
      const set = new Set<string>();
      order.forEach((id, index) => {
        const old = before.get(id);
        if (old !== undefined && Math.abs(old - index) > 0) set.add(id);
      });
      if (set.size) {
        setMoved(set);
        window.setTimeout(() => setMoved(new Set()), 600);
      }
    }
    previousOrder.current = order;
    return null;
  }, [rows]);

  const metrics = data?.summary.metrics;
  const memRatio = ratio(metrics?.memory_used, metrics?.memory_total);
  const diskRatio = ratio(metrics?.disk_used, metrics?.disk_total);

  return (
    <div className="page">
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      <div className="split">
        <FleetList selected={session.local_node_id} onSelect={(id) => navigate(`/nodes?node=${id}`)} />

        <div className="stack gap-14">
          <section className="stats" aria-busy={query.isPending}>
            <div className="stat">
              <span>CPU</span>
              <b>{metrics?.cpu_usage !== undefined ? metrics.cpu_usage.toFixed(1) : '—'}<small>%</small></b>
              <i>{metrics?.cpu_cores ?? '—'} 核</i>
            </div>
            <div className="stat">
              <span>内存</span>
              <b>{memRatio !== undefined ? memRatio.toFixed(1) : '—'}<small>%</small></b>
              <i>{bytes(metrics?.memory_used)} / {bytes(metrics?.memory_total)}</i>
            </div>
            <div className="stat">
              <span>磁盘</span>
              <b>{diskRatio !== undefined ? diskRatio.toFixed(1) : '—'}<small>%</small></b>
              <i>{bytes(metrics?.disk_used)} / {bytes(metrics?.disk_total)}</i>
            </div>
            <div className="stat">
              <span>网络</span>
              <b>{metrics?.network_rx !== undefined ? bytes(metrics.network_rx).split(' ')[0] : '—'}<small>{metrics?.network_rx !== undefined ? `${bytes(metrics.network_rx).split(' ')[1]}/s` : ''}</small></b>
              <i>发送 {speed(metrics?.network_tx)}</i>
            </div>
          </section>

          <Sheet
            title="舰队"
            className={`world-${world}`}
            flush
            tools={
              <>
                <span className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>
                  {nodes.length} 台 · {data ? clock(data.updated_at) : ''}
                </span>
                <Button size="sm" icon="refresh" busy={query.isFetching} onClick={() => void query.refetch()}>
                  刷新
                </Button>
              </>
            }
          >
            {world === 'timetable' ? <TickRail /> : null}
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    {world === 'board' ? <th scope="col" className="num">排名</th> : null}
                    <th scope="col">节点</th>
                    <th scope="col">状态</th>
                    <th scope="col" className="right">CPU</th>
                    <th scope="col" className="right">内存</th>
                    <th scope="col" className="right">磁盘</th>
                    <th scope="col" className="right">网络 ↓ / ↑</th>
                    <th scope="col">防火墙</th>
                    <th scope="col" />
                  </tr>
                </thead>
                <tbody>
                  {query.isPending
                    ? Array.from({ length: 6 }, (_, index) => (
                        <tr key={index}>
                          {Array.from({ length: world === 'board' ? 9 : 8 }, (_, cell) => (
                            <td key={cell}>
                              <Skeleton height={11} width={cell === 1 ? 110 : 46} />
                            </td>
                          ))}
                        </tr>
                      ))
                    : rows.map(({ node, reading }, index) => {
                        const attention = pending.some((op) => op.node_id === node.id && ['awaiting_confirmation', 'attention_required'].includes(op.state));
                        const offline = !node.connected;
                        const load = Math.max(reading.metrics?.cpu_usage ?? 0, ratio(reading.metrics?.memory_used, reading.metrics?.memory_total) ?? 0);
                        const disk = ratio(reading.metrics?.disk_used, reading.metrics?.disk_total);
                        return (
                          <tr key={node.id} className={moved.has(node.id) ? 'board-move' : ''} style={{ cursor: 'pointer' }} onClick={() => navigate(`/nodes?node=${node.id}`)}>
                            {world === 'board' ? (
                              <td>
                                <span className={`rank ${offline || attention ? 'rank--hot' : load >= 90 ? 'rank--warm' : ''}`}>
                                  {String(index + 1).padStart(2, '0')}
                                </span>
                              </td>
                            ) : null}
                            <td>
                              <div className="table__name">
                                <Link to={`/nodes?node=${node.id}`} onClick={(event) => event.stopPropagation()}>
                                  {node.name}
                                </Link>
                                {node.local ? <span className="sub">本机</span> : null}
                              </div>
                            </td>
                            <td>
                              <span className="row gap-6">
                                <Led state={node.connected ? (reading.state === 'ready' ? 'on' : 'warn') : 'off'} />
                                <span style={{ fontSize: 11.5 }}>{node.connected ? readingStates[reading.state] : '离线'}</span>
                              </span>
                            </td>
                            <td className="right">
                              <MetricCell value={reading.metrics?.cpu_usage} kind="cpu" />
                            </td>
                            <td className="right">
                              <MetricCell value={ratio(reading.metrics?.memory_used, reading.metrics?.memory_total)} kind="mem" />
                            </td>
                            <td className="right">
                              <MetricCell value={disk} kind="disk" />
                            </td>
                            <td className="right mono" style={{ fontSize: 10, lineHeight: 1.5 }}>
                              <div>↓ {speed(reading.metrics?.network_rx)}</div>
                              <div style={{ color: 'var(--ink-mute)' }}>↑ {speed(reading.metrics?.network_tx)}</div>
                            </td>
                            <td>
                              <span style={{ fontSize: 11.5, color: reading.firewall === 'enabled' ? 'var(--ink)' : 'var(--ink-mute)' }}>
                                {firewallStates[reading.firewall]}
                              </span>
                            </td>
                            <td className="right">
                              <Icon name="chevron" width={12} height={12} style={{ color: 'var(--ink-mute)' }} />
                            </td>
                          </tr>
                        );
                      })}
                </tbody>
                <tfoot>
                  <tr>
                    <td className="table-foot" colSpan={world === 'board' ? 9 : 8}>
                      按需处理优先排序 · 读数每 3 秒刷新
                    </td>
                  </tr>
                </tfoot>
              </table>
            </div>
          </Sheet>

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
      </div>
    </div>
  );
}

function TickRail() {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 30_000);
    return () => window.clearInterval(timer);
  }, []);
  const hours = now.getHours() + now.getMinutes() / 60;
  return (
    <div className="tick-rail" style={{ margin: 12, borderRadius: 0 }}>
      <span className="t0">00</span>
      <span className="t12">12</span>
      <span className="t24">24</span>
      <i className="now" style={{ left: `${(hours / 24) * 100}%` }} />
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

  if (query.isPending) return <Skeleton height={200} />;
  if (!points.length) return <div className="empty">暂无监控数据</div>;

  return (
    <div className="stack gap-14">
      <div className="trends">
        <div className="chart">
          <div className="chart__head">
            <h3>CPU 使用率</h3>
            <span className="legend">
              <span><i />当前 {percent(cpu[cpu.length - 1])}</span>
            </span>
          </div>
          <Trace series={[{ values: cpu, color: 'var(--ink)' }]} fill xLabels={label} aria="CPU 使用率趋势" height={80} ceiling={100} />
        </div>
        <div className="chart">
          <div className="chart__head">
            <h3>内存使用率</h3>
            <span className="legend">
              <span><i />当前 {percent(mem[mem.length - 1])}</span>
            </span>
          </div>
          <Trace series={[{ values: mem, color: 'var(--ink)' }]} fill xLabels={label} aria="内存使用率趋势" height={80} ceiling={100} />
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
              { values: rx, color: 'var(--info)' },
              { values: tx, color: 'var(--ink)', dashed: true },
            ]}
            xLabels={label}
            aria="网络吞吐趋势"
            height={86}
            yFormat={(value) => speed(value)}
          />
        </div>
      </div>
    </div>
  );
}

export { overviewQuery };

