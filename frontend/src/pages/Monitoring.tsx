import { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router';
import { AnimatePresence, motion } from 'motion/react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { Activity } from 'lucide-react';
import { get } from '../api/http';
import type { MonitoringSeriesResult, Schemas } from '../api/types';
import { useMonitoringStatus, useNodes } from '../app/queries';
import { ago, bytes, rate } from '../lib/format';
import { AreaChart, palette, type Series } from '../ui/charts';
import { Badge, Card, CardHead, Dot, Empty, ErrorNotice, Notice, PageHead, Segmented, Select, Skeleton, Spinner } from '../ui/kit';

type History = Schemas['MonitoringHistory'];
type Metric = 'cpu' | 'mem' | 'disk' | 'net';

const metricDefs: Record<Metric, { label: string; format: (v: number) => string; max?: number }> = {
  cpu: { label: 'CPU 使用率', format: (v) => `${v.toFixed(0)}%`, max: 100 },
  mem: { label: '内存使用率', format: (v) => `${v.toFixed(0)}%`, max: 100 },
  disk: { label: '磁盘使用率', format: (v) => `${v.toFixed(0)}%`, max: 100 },
  net: { label: '网络吞吐', format: (v) => rate(v) },
};

function value(m: Schemas['Metrics'], k: Metric): number | null {
  if (k === 'cpu') return m.cpu_usage ?? null;
  if (k === 'mem') return m.memory_used !== undefined && m.memory_total ? (m.memory_used / m.memory_total) * 100 : null;
  if (k === 'disk') return m.disk_used !== undefined && m.disk_total ? (m.disk_used / m.disk_total) * 100 : null;
  return m.network_rx !== undefined || m.network_tx !== undefined ? (m.network_rx ?? 0) + (m.network_tx ?? 0) : null;
}

export function Monitoring() {
  const [params, setParams] = useSearchParams();
  const tab = (params.get('tab') as 'trend' | 'series' | 'status') || 'trend';
  return (
    <div className="page">
      <PageHead title="监控" sub="五分钟采样保留七天；启用集中监控后可查询秒级归档（本地 30 天，S3 365 天）。" actions={
        <Segmented value={tab} onChange={(t) => setParams({ tab: t }, { replace: true })} options={[{ value: 'trend', label: '趋势' }, { value: 'series', label: '秒级归档' }, { value: 'status', label: '采集状态' }]} />
      } />
      <AnimatePresence mode="wait">
        <motion.div key={tab} className="stack" initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }} transition={{ duration: 0.2 }}>
          {tab === 'trend' && <Trend />}
          {tab === 'series' && <SeriesView />}
          {tab === 'status' && <ArchiveStatus />}
        </motion.div>
      </AnimatePresence>
    </div>
  );
}

function Trend() {
  const [range, setRange] = useState<'24h' | '7d'>('24h');
  const nodes = useNodes();
  const names = new Map((nodes.data?.items ?? []).map((n) => [n.id, n.name]));
  const q = useQuery({
    queryKey: ['monitoring', 'history', range],
    queryFn: ({ signal }) => get<History>('/monitoring/history', { signal, query: { range, group_by: 'node' } }),
    placeholderData: keepPreviousData,
    refetchInterval: 5 * 60_000,
  });
  const timeFmt = (t: number) => new Date(t).toLocaleString('zh-CN', range === '24h' ? { hour: '2-digit', minute: '2-digit', hour12: false } : { month: '2-digit', day: '2-digit', hour: '2-digit', hour12: false });
  const build = (k: Metric): Series[] => {
    const per = q.data?.nodes ?? [];
    if (per.length) return per.map((n, i) => ({ name: names.get(n.node_id) ?? n.node_id.slice(0, 10), color: palette[i % palette.length]!, points: n.items.map((m) => ({ t: Date.parse(m.collected_at), v: value(m, k) })) }));
    return [{ name: '集群', color: palette[0]!, points: (q.data?.items ?? []).map((p) => ({ t: Date.parse(p.collected_at), v: value(p.summary.metrics, k) })) }];
  };
  return (
    <>
      <div className="toolbar">
        <Segmented value={range} onChange={setRange} options={[{ value: '24h', label: '24 小时' }, { value: '7d', label: '7 天' }]} />
        {q.isFetching && <Spinner />}
        {!!q.data?.unavailable?.length && <Badge tone="warn">{q.data.unavailable.length} 个节点无历史</Badge>}
      </div>
      {q.isError && <ErrorNotice error={q.error} retry={() => void q.refetch()} title="历史存储不可用" />}
      <div className="chart-grid">
        {(Object.keys(metricDefs) as Metric[]).map((k) => {
          const series = build(k);
          return (
            <Card key={k}>
              <CardHead title={metricDefs[k].label} sub={q.data ? `每 ${q.data.interval_minutes} 分钟` : undefined} />
              <div className="card-body">
                {!q.data ? <Skeleton height={220} /> : <AreaChart series={series} height={220} yMax={metricDefs[k].max} format={metricDefs[k].format} timeFormat={timeFmt} empty={<span className="muted">暂无数据</span>} />}
                {series.length > 1 && <div className="legend" style={{ marginTop: 10 }}>{series.map((s) => <span key={s.name}><i style={{ background: s.color }} />{s.name}</span>)}</div>}
              </div>
            </Card>
          );
        })}
      </div>
    </>
  );
}

const seriesMetrics = [
  { value: 'opsd_node_cpu_usage_percent', label: 'CPU %', format: (v: number) => `${v.toFixed(0)}%`, max: 100 },
  { value: 'opsd_node_memory_used_bytes', label: '内存已用', format: (v: number) => bytes(v) },
  { value: 'opsd_node_disk_used_bytes', label: '磁盘已用', format: (v: number) => bytes(v) },
  { value: 'opsd_node_network_receive_bytes_per_second', label: '网络接收', format: (v: number) => rate(v) },
  { value: 'opsd_node_network_transmit_bytes_per_second', label: '网络发送', format: (v: number) => rate(v) },
  { value: 'opsd_container_running', label: '运行中容器', format: (v: number) => v.toFixed(0) },
  { value: 'opsd_database_connections', label: '数据库连接', format: (v: number) => v.toFixed(0) },
  { value: 'opsd_galera_receive_queue', label: 'Galera 接收队列', format: (v: number) => v.toFixed(1) },
  { value: 'opsd_proxysql_connections', label: 'ProxySQL 连接', format: (v: number) => v.toFixed(0) },
] as const;
const ranges = { '1h': 3600, '6h': 21600, '24h': 86400, '7d': 604800, '30d': 2592000 } as const;

function SeriesView() {
  const nodes = useNodes();
  const [metric, setMetric] = useState<string>(seriesMetrics[0].value);
  const [range, setRange] = useState<keyof typeof ranges>('1h');
  const [node, setNode] = useState('');
  const def = seriesMetrics.find((m) => m.value === metric) ?? seriesMetrics[0];
  const names = new Map((nodes.data?.items ?? []).map((n) => [n.id, n.name]));
  const q = useQuery({
    queryKey: ['monitoring', 'series', metric, range, node],
    queryFn: ({ signal }) => {
      const to = new Date();
      const from = new Date(to.getTime() - ranges[range] * 1000);
      return get<MonitoringSeriesResult>('/monitoring/series', { signal, query: { metric, node_id: node || undefined, from: from.toISOString(), to: to.toISOString(), step_seconds: Math.max(1, Math.round(ranges[range] / 600)) } });
    },
    placeholderData: keepPreviousData,
    refetchInterval: range === '1h' ? 15_000 : 60_000,
    retry: 0,
  });
  const series: Series[] = (q.data?.items ?? []).map((s, i) => ({
    name: `${names.get(s.node_id) ?? s.node_id.slice(0, 10)}${s.resource_id ? ` · ${s.resource_id.slice(0, 12)}` : ''}`,
    color: palette[i % palette.length]!,
    points: s.points.map((p) => ({ t: Date.parse(p.at), v: p.value ?? null })),
  }));
  const timeFmt = (t: number) => new Date(t).toLocaleString('zh-CN', ranges[range] <= 86400 ? { hour: '2-digit', minute: '2-digit', hour12: false } : { month: '2-digit', day: '2-digit', hour: '2-digit', hour12: false });
  return (
    <>
      <div className="toolbar">
        <Select value={metric} onChange={(e) => setMetric(e.target.value)} style={{ width: 200 }}>{seriesMetrics.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}</Select>
        <Select value={node} onChange={(e) => setNode(e.target.value)} style={{ width: 180 }}><option value="">全部节点</option>{(nodes.data?.items ?? []).filter((n) => !n.revoked_at).map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}</Select>
        <Segmented value={range} onChange={setRange} options={(Object.keys(ranges) as (keyof typeof ranges)[]).map((r) => ({ value: r, label: r }))} />
        {q.isFetching && <Spinner />}
      </div>
      {q.isError && <Notice tone="info" title="秒级归档不可用">集中监控尚未启用或查询组件离线，可先使用“趋势”查看五分钟采样。</Notice>}
      {q.data?.partial && <Notice tone="warn" title="部分数据缺失">{q.data.warnings.join('；') || '部分来源读取失败'}</Notice>}
      <Card>
        <CardHead title={def.label} sub={q.data ? `步长 ${q.data.step_seconds} 秒 · 来源分辨率 ${q.data.source_resolution_seconds || '原始'}${q.data.source_resolution_seconds ? ' 秒' : ''}` : undefined} />
        <div className="card-body">
          {!q.data && !q.isError ? <Skeleton height={320} /> : <AreaChart series={series} height={320} yMax={'max' in def ? def.max : undefined} format={def.format} timeFormat={timeFmt} empty={<Empty icon={<Activity />} title="没有数据" />} />}
          {series.length > 1 && <div className="legend" style={{ marginTop: 10 }}>{series.map((s) => <span key={s.name}><i style={{ background: s.color }} />{s.name}</span>)}</div>}
        </div>
      </Card>
    </>
  );
}

function ArchiveStatus() {
  const q = useMonitoringStatus();
  const nodes = useNodes();
  const names = new Map((nodes.data?.items ?? []).map((n) => [n.id, n.name]));
  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  if (!q.data) return <Card pad><Skeleton height={200} /></Card>;
  if (!q.data.items.length) return <Card><Empty icon={<Activity />} title="未启用集中监控">启用后各节点每秒采集主机、容器和数据库指标并归档到 S3。</Empty></Card>;
  return (
    <Card>
      <div className="table-wrap">
        <table className="table">
          <thead><tr><th>节点</th><th>采集</th><th>上传</th><th className="num-cell">缓冲</th><th className="hide-sm">组件</th></tr></thead>
          <tbody>{q.data.items.map((s) => (
            <tr key={s.node_id}>
              <td><strong style={{ fontWeight: 560 }}>{names.get(s.node_id) ?? s.node_id.slice(0, 12)}</strong>{s.paused && <Badge tone="warn">已暂停</Badge>}</td>
              <td>{s.sampling_error ? <span className="field-error">{s.sampling_error}</span> : <span className="row"><Dot tone={s.last_sample ? 'ok' : 'idle'} live={!!s.last_sample} />{s.last_sample ? ago(s.last_sample) : '—'}</span>}</td>
              <td>{s.upload_error ? <span className="field-error">{s.upload_error}</span> : s.last_upload ? ago(s.last_upload) : '—'}</td>
              <td className="num-cell">{bytes(s.buffer.bytes)} · {s.buffer.batches} 批</td>
              <td className="hide-sm"><div className="row wrap" style={{ gap: 4 }}>{s.deployment.components.map((c) => <Badge key={c.name} tone={c.ready ? 'ok' : c.error ? 'bad' : 'idle'}>{c.name}</Badge>)}{!s.deployment.components.length && <span className="dim">—</span>}</div></td>
            </tr>
          ))}</tbody>
        </table>
      </div>
      {!!q.data.unavailable.length && <div className="card-body"><Notice tone="warn">{q.data.unavailable.map((u) => `${u.node_name}：${u.message}`).join('；')}</Notice></div>}
    </Card>
  );
}
