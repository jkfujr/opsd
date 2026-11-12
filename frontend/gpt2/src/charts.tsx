import { useId, useMemo } from 'react';
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { useQuery } from '@tanstack/react-query';
import { api, unwrap } from './api/client';
import type { components } from './api/types';
import { ratio } from './monitoring';
import { Failure } from './business-ui';
import { Empty, Skeleton } from './ui';

export type ChartMetric = 'network' | 'resources' | 'cpu' | 'memory';
const labels: Record<string, string> = { rx: '接收', tx: '发送', cpu: 'CPU', memory: '内存' };
const colors: Record<string, string> = { rx: 'var(--chart-network)', tx: 'var(--chart-cpu)', cpu: 'var(--chart-cpu)', memory: 'var(--chart-memory)' };
const timeFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', hour: '2-digit', minute: '2-digit', hour12: false });
const dateFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', month: 'numeric', day: 'numeric' });
const tooltipFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false });
function Readout({ active, payload, label, network }: { active?: boolean; payload?: { dataKey?: string | number; value?: number | string }[]; label?: number | string; network: boolean }) {
  if (!active || !payload?.length) return null;
  return <div className="chart-tooltip"><time>{tooltipFormat.format(Number(label))}</time>{payload.map(entry => <div key={entry.dataKey}><span><i style={{ background: colors[String(entry.dataKey)] }}/>{labels[String(entry.dataKey)]}</span><strong>{Number(entry.value).toFixed(1)}<small>{network ? ' MiB/s' : '%'}</small></strong></div>)}</div>;
}

export function TrendChart({ nodes, range = '24h', metric = 'network', compact = false }: { nodes: components['schemas']['NodeStatus'][]; range?: string; metric?: ChartMetric; compact?: boolean }) {
  const nodeIDs = nodes.map(node => node.id).sort().join(',');
  const query = useQuery({ queryKey: ['monitoring', 'history', range, nodeIDs], enabled: !!nodes.length, queryFn: async ({ signal }) => unwrap(await api.GET('/monitoring/history', { signal, params: { query: { range: range === '7d' ? '7d' : '24h', node_ids: nodeIDs } } })), refetchInterval: q => (q.state.data?.interval_minutes ?? 5) * 60000 });
  const id = useId().replaceAll(':', '');
  const points = useMemo(() => (query.data?.items ?? []).map(point => { const m = point.summary.metrics; return { time: new Date(point.collected_at).getTime(), rx: m.network_rx === undefined ? null : m.network_rx / 1048576, tx: m.network_tx === undefined ? null : m.network_tx / 1048576, cpu: m.cpu_usage ?? null, memory: ratio(m.memory_used, m.memory_total) ?? null }; }), [query.data]);
  const network = metric === 'network';
  const keys = metric === 'network' ? ['rx', 'tx'] : metric === 'resources' ? ['cpu', 'memory'] : [metric];
  const last = query.data?.items.at(-1)?.summary.valid_nodes;
  const usable = network ? last?.network ?? 0 : metric === 'memory' ? last?.memory ?? 0 : last?.cpu ?? 0;
  const height = compact ? 226 : 282;
  const unavailable = query.data?.unavailable?.map(id => nodes.find(node => node.id === id)?.name ?? id) ?? [];
  const errors = <><Failure error={query.error} retry={() => void query.refetch()}/>{unavailable.length > 0 && <p className="notice" role="alert">历史数据读取失败：{unavailable.join('、')}</p>}</>;
  if (query.isPending && nodes.length > 0) return <div className="chart-loading" style={{ height }}><Skeleton/><Skeleton/><Skeleton/></div>;
  if (!points.length || !points.some(point => keys.some(key => point[key as 'rx' | 'tx' | 'cpu' | 'memory'] !== null))) return <div className="chart-empty" style={{ minHeight: height }}>{errors}{!query.error && !unavailable.length && <Empty title="暂无监控数据"/>}</div>;
  return <>
    {errors}
    <div className="chart-canvas" style={{ height }} role="group" aria-label={`${range === '24h' ? '24 小时' : '7 天'}${network ? '网络流量' : metric === 'resources' ? '资源使用率' : labels[metric]}趋势，左右方向键查看读数`}>
      <ResponsiveContainer width="100%" height="100%" minWidth={0}>
        <AreaChart data={points} accessibilityLayer margin={{ top: 16, right: 10, bottom: 4, left: 0 }}>
          <defs>{keys.map(key => <linearGradient key={key} id={`${id}-${key}`} x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor={colors[key]} stopOpacity={network ? .2 : .13}/><stop offset="100%" stopColor={colors[key]} stopOpacity={.025}/></linearGradient>)}</defs>
          <CartesianGrid vertical={false} stroke="var(--chart-grid)" strokeDasharray="0"/>
          <XAxis type="number" dataKey="time" domain={['dataMin', 'dataMax']} scale="time" tickCount={compact ? 5 : 7} axisLine={false} tickLine={false} minTickGap={32} tickMargin={12} tick={{ fill: 'var(--foreground-muted)', fontSize: 13 }} tickFormatter={time => range === '7d' ? dateFormat.format(time) : timeFormat.format(time)}/>
          <YAxis domain={network ? [0, 'auto'] : [0, 100]} tickCount={5} axisLine={false} tickLine={false} tickMargin={10} tick={{ fill: 'var(--foreground-muted)', fontSize: 13 }} tickFormatter={value => network ? String(value) : `${value}%`}/>
          <Tooltip content={<Readout network={network}/>} cursor={{ stroke: 'var(--border-emphasis)', strokeDasharray: '3 3' }} isAnimationActive={false}/>
          {keys.map(key => <Area key={key} type="linear" dataKey={key} name={labels[key]} stroke={colors[key]} strokeWidth={1.8} fill={`url(#${id}-${key})`} activeDot={{ r: 4, stroke: 'var(--background)', strokeWidth: 2 }} isAnimationActive={false} connectNulls={false}/>)}
        </AreaChart>
      </ResponsiveContainer>
    </div>
    <div className="chart-footer"><div className="chart-legend">{keys.map(key => <span key={key}><i style={{ background: colors[key] }}/>{labels[key]}{network && <small>MiB/s</small>}</span>)}</div><span className="meta">{usable} 个有效节点<span className="chart-frequency"> · 每 {query.data?.interval_minutes ?? '—'} 分钟采集</span></span></div>
  </>;
}
