import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router';
import { Progress } from '@appica/ui-react/progress';
import { Skeleton } from '@appica/ui-react/skeleton';
import { Icon } from './icons';
import { series, type NodeItem } from './data';
import { useDemo } from './store';
import { Btn, Cell, Choice, Copy, DataTable, Empty, IconButton, LoadingRows, Panel, Row, SearchBox, Status } from './components';

export function summarize(nodes: NodeItem[]) {
  const online = nodes.filter(n => n.online);
  const ram = online.reduce((sum, n) => sum + n.ram, 0), disk = online.reduce((sum, n) => sum + n.capacity, 0), cores = online.reduce((sum, n) => sum + n.cores, 0);
  return { online, cores, ram, disk, cpu: cores ? online.reduce((sum, n) => sum + n.cpu * n.cores, 0) / cores : 0, memory: ram ? online.reduce((sum, n) => sum + n.memory * n.ram, 0) / ram : 0, diskUsed: disk ? online.reduce((sum, n) => sum + n.disk * n.capacity, 0) / disk : 0, rx: online.reduce((sum, n) => sum + n.rx, 0), tx: online.reduce((sum, n) => sum + n.tx, 0) };
}
export function HealthRail({ nodes }: { nodes: NodeItem[] }) {
  return <span className="health-rail" role="img" aria-label={`${nodes.filter(n => n.online).length} 个在线，${nodes.filter(n => !n.online).length} 个离线`}>{nodes.map(n => <span key={n.id} title={`${n.name} · ${n.online ? '在线' : '离线'}`} className={n.online ? 'healthy' : 'disconnected'}/>)}</span>;
}
export function MetricsStrip({ nodes }: { nodes: NodeItem[] }) {
  const { scene } = useDemo();
  const s = summarize(nodes), available = s.online.length > 0;
  const metrics = [
    { name: 'CPU 使用率', value: s.cpu.toFixed(1), unit: '%', note: `${s.cores} 个逻辑核心`, icon: 'cpu' as const },
    { name: '内存使用率', value: s.memory.toFixed(1), unit: '%', note: `${(s.ram * s.memory / 100).toFixed(1)} / ${s.ram} GiB`, icon: 'memory' as const },
    { name: '磁盘使用率', value: s.diskUsed.toFixed(1), unit: '%', note: `${(s.disk * s.diskUsed / 100).toFixed(0)} / ${s.disk} GiB`, icon: 'storage' as const },
    { name: '网络接收', value: s.rx.toFixed(1), unit: 'MiB/s', note: `发送 ${s.tx.toFixed(1)} MiB/s`, icon: 'network' as const },
  ];
  return <section className="metrics-strip" aria-label="资源汇总" aria-busy={scene === 'loading'}>{metrics.map(item => <div key={item.name} className="metric-cell"><div className="metric-name">{item.name}<Icon name={item.icon} size={16}/></div>{scene === 'loading' ? <Skeleton className="metric-skeleton"/> : <div className="metric-reading">{available ? item.value : '—'}{available && <span className="metric-unit">{item.unit}</span>}</div>}<div className="metric-note">{available ? item.note : '暂无有效读数'}</div></div>)}</section>;
}

export function TrendChart({ nodes, range: externalRange, metric: externalMetric, onRangeChange, onMetricChange, scope, onClear, layout = 'default' }: { nodes: NodeItem[]; range?: string; metric?: string; onRangeChange?: (value: string) => void; onMetricChange?: (value: string) => void; scope?: string; onClear?: () => void; layout?: 'default' | 'overview' }) {
  const [ownRange, setOwnRange] = useState('24h');
  const [ownMetric, setOwnMetric] = useState('resources');
  const range = externalRange ?? ownRange, metric = externalMetric ?? ownMetric;
  const setRange = onRangeChange ?? setOwnRange, setMetric = onMetricChange ?? setOwnMetric;
  const [hover, setHover] = useState<number | null>(null);
  const { scene } = useDemo();
  const id = useId().replaceAll(':', '');
  const summary = summarize(nodes);
  const svg = useRef<SVGSVGElement>(null);
  const [size, setSize] = useState({ width: 760, height: 202 });
  useEffect(() => {
    if (!svg.current) return;
    const observer = new ResizeObserver(([entry]) => {
      const width = Math.max(240, Math.round(entry.contentRect.width));
      const height = Math.max(150, Math.round(entry.contentRect.height));
      setSize(old => old.width === width && old.height === height ? old : { width, height });
    });
    observer.observe(svg.current);
    return () => observer.disconnect();
  }, [scene, summary.online.length]);
  const points = useMemo(() => series.map((p, i) => ({ ...p, cpu: Math.max(0, p.cpu + summary.cpu - series.at(-1)!.cpu + (range === '7d' ? Math.sin(i * 0.25) * 9 : 0)), memory: Math.max(0, p.memory + summary.memory - series.at(-1)!.memory), network: p.network / 20 * summary.rx })), [summary.cpu, summary.memory, summary.rx, range]);
  const { width, height } = size;
  const left = 32, right = 12, top = 15, bottom = 22;
  const plotW = width - left - right, plotH = height - top - bottom;
  const max = metric === 'resources' ? 100 : Math.ceil(Math.max(...points.map(p => p.network), 1) / 10) * 10;
  const x = (i: number) => left + i / (points.length - 1) * plotW;
  const y = (v: number) => top + (1 - Math.min(v, max) / max) * plotH;
  const keys = metric === 'resources' ? ['cpu', 'memory'] as const : ['network'] as const;
  const path = (key: typeof keys[number]) => points.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p[key]).toFixed(1)}`).join(' ');
  const labels = range === '24h' ? ['00:00', '04:00', '08:00', '12:00', '16:00', '20:00', '24:00'] : ['11/04', '11/05', '11/06', '11/07', '11/08', '11/09', '11/10'];
  const selected = hover === null ? null : points[hover];
  const rangeControl = <div className="segment" aria-label="时间范围">{[['24h', '24 小时'], ['7d', '7 天']].map(([v, l]) => <button key={v} type="button" aria-pressed={range === v} onClick={() => { setRange(v); setHover(null); }}>{l}</button>)}</div>;
  const metricControl = <div className="chart-metric-select"><button aria-pressed={metric === 'resources'} className={metric === 'resources' ? 'selected' : ''} onClick={() => setMetric('resources')}>CPU 与内存</button><button aria-pressed={metric === 'network'} className={metric === 'network' ? 'selected' : ''} onClick={() => setMetric('network')}>网络</button></div>;
  const scopeControl = scope && <div className="chart-scope"><strong title={scope}>{scope}</strong>{onClear && <Btn variant="ghost" size="sm" onClick={onClear}>全部节点</Btn>}</div>;
  const coverage = <span className="chart-coverage">{summary.online.length} 个有效节点</span>;
  const chart = <>
    {scene === 'loading' ? <Skeleton className="chart-skeleton"/> : !summary.online.length ? <Empty title={nodes.length ? '节点离线' : '暂无监控数据'} icon="activity"/> : <div className="chart-wrap" onMouseLeave={() => setHover(null)}>
      <svg ref={svg} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`${range === '24h' ? '24 小时' : '7 天'}${metric === 'resources' ? ' CPU 与内存使用率' : '网络接收速度'}趋势，可使用左右方向键查看读数`} tabIndex={0} className="trend-chart" onKeyDown={event => { if (['ArrowLeft', 'ArrowRight'].includes(event.key)) { event.preventDefault(); setHover(h => Math.max(0, Math.min(points.length - 1, (h ?? 24) + (event.key === 'ArrowRight' ? 1 : -1)))); } }} onPointerMove={event => { const box = event.currentTarget.getBoundingClientRect(); setHover(Math.max(0, Math.min(points.length - 1, Math.round(((event.clientX - box.left) / box.width * width - left) / plotW * (points.length - 1))))); }}>
        <title>{metric === 'resources' ? '演示使用率历史，百分比刻度 0 至 100' : `演示网络历史，刻度 0 至 ${max} MiB/s`}</title>
        <defs><clipPath id={`plot-${id}`}><rect x={left} y={top} width={plotW} height={plotH}/></clipPath></defs>
        {[0, 25, 50, 75, 100].map(p => <g key={p}><line x1={left} y1={y(max * p / 100)} x2={width - right} y2={y(max * p / 100)} className="chart-grid"/><text x={left - 10} y={y(max * p / 100) + 3} textAnchor="end" className="chart-axis">{Math.round(max * p / 100)}</text></g>)}
        <g clipPath={`url(#plot-${id})`}><path d={`${path(keys[0])} L${x(48)},${y(0)} L${x(0)},${y(0)} Z`} className="chart-area"/>{keys.map((key, i) => <path key={key} d={path(key)} className={`chart-line series-${i}`} vectorEffect="non-scaling-stroke"/>)}</g>
        {labels.map((label, i) => width < 450 && i % 2 ? null : <text key={label} x={left + plotW * i / 6} y={height - 2} textAnchor={i === 0 ? 'start' : i === 6 ? 'end' : 'middle'} className="chart-axis">{label}</text>)}
        {hover !== null && <g><line x1={x(hover)} x2={x(hover)} y1={top} y2={height - bottom} className="chart-crosshair"/>{keys.map((key, i) => <circle key={key} cx={x(hover)} cy={y(points[hover][key])} r="4" className={`chart-point series-${i}`}/>)}</g>}
      </svg>
      {selected && <div className="chart-tooltip" role="status" style={{ left: Math.max(8, Math.min(width - 148, hover! / 48 * width)) }}><strong>{range === '24h' ? `${String(Math.floor(selected.time)).padStart(2, '0')}:${selected.time % 1 ? '30' : '00'}` : labels[Math.min(6, Math.floor(hover! / 8))]}</strong>{metric === 'resources' ? <><span>CPU <b>{selected.cpu.toFixed(1)}%</b></span><span>内存 <b>{selected.memory.toFixed(1)}%</b></span></> : <span>接收 <b>{selected.network.toFixed(1)} MiB/s</b></span>}</div>}
    </div>}
    <div className="chart-legend">{metric === 'resources' ? <><span><i/>CPU</span><span><i className="blue"/>内存</span></> : <span><i/>网络接收</span>}{layout === 'overview' ? <span className="chart-meta">{coverage}<span className="legend-note">每 5 分钟采集</span></span> : <span className="legend-note">每 5 分钟采集</span>}</div>
  </>;
  return layout === 'overview' ? <section className="trend-panel overview-trend" aria-labelledby={`trend-title-${id}`}>
    <div className="overview-trend-toolbar"><div className="overview-trend-title"><h2 id={`trend-title-${id}`} className="trend-heading" tabIndex={-1}>资源趋势</h2>{scopeControl}</div><div className="overview-trend-controls">{metricControl}{rangeControl}</div></div>
    {chart}
  </section> : <Panel title="资源趋势" className="trend-panel" actions={rangeControl}>{scopeControl}<div className="chart-topline">{metricControl}{coverage}</div>{chart}</Panel>;
}
export function Usage({ value, offline = false }: { value: number; offline?: boolean }) {
  return <div className={`usage ${value >= 85 ? 'high' : ''}`}><span>{offline ? '—' : `${value.toFixed(1)}%`}</span>{!offline && <Progress value={value} thickness={3} className="usage-bar" indicatorColor={value >= 85 ? 'var(--warning)' : 'var(--primary)'}/>}</div>;
}

export function NodeIdentity({ node, link = true }: { node: NodeItem; link?: boolean }) {
  return <div className="node-identity"><span className={'node-code ' + (!node.online ? 'node-code-offline' : '')}>{node.code}</span><div className="node-identity-text">{link ? <Link to={'/nodes/' + node.id} className="node-name" title={node.name}>{node.name}</Link> : <span className="node-name" title={node.name}>{node.name}</span>}<span className="node-address"><Copy value={node.address}/><span>{node.region}</span></span></div></div>;
}
export type NodeFilters = { search: string; state: string; sort: string };
export function filterNodes(nodes: NodeItem[], filters: NodeFilters) {
  return nodes.filter(n => (n.name + ' ' + n.address + ' ' + n.region).toLowerCase().includes(filters.search.toLowerCase()) && (filters.state === 'all' || n.online === (filters.state === 'online'))).sort((a, b) => filters.sort === 'cpu' ? Number(b.online) - Number(a.online) || b.cpu - a.cpu : filters.sort === 'disk' ? Number(b.online) - Number(a.online) || b.disk - a.disk : a.name.localeCompare(b.name));
}
export function NodeTable({ nodes, title, compact = false, filters, onFiltersChange, onPreview, selectedId }: { nodes: NodeItem[]; title?: string; compact?: boolean; filters?: NodeFilters; onFiltersChange?: (value: NodeFilters) => void; onPreview?: (id: string) => void; selectedId?: string }) {
  const [ownFilters, setOwnFilters] = useState<NodeFilters>({ search: '', state: 'all', sort: 'name' });
  const value = filters ?? ownFilters;
  const change = (patch: Partial<NodeFilters>) => (onFiltersChange ?? setOwnFilters)({ ...value, ...patch });
  const { scene } = useDemo();
  const visible = filterNodes(nodes, value);
  const headings = ['节点', ...(!compact ? ['地域'] : []), '状态', <button className="table-sort" aria-pressed={value.sort === 'cpu'} onClick={() => change({ sort: value.sort === 'cpu' ? 'name' : 'cpu' })}>CPU<Icon name="sort" size={12}/></button>, '内存', <button className="table-sort" aria-pressed={value.sort === 'disk'} onClick={() => change({ sort: value.sort === 'disk' ? 'name' : 'disk' })}>磁盘<Icon name="sort" size={12}/></button>, '网络 ↓ / ↑', onPreview ? '趋势' : ''];
  return <Panel className={'node-table-panel ' + (compact ? 'compact-nodes' : '')}>
    <div className="node-list-toolbar">{title && <h2>{title}</h2>}<span className="muted node-count">{visible.length} 个节点</span><div className="node-filters"><SearchBox value={value.search} onChange={search => change({ search })}/><Choice label="节点状态" value={value.state} onChange={state => change({ state })} options={[["all", "全部状态"], ["online", "在线"], ["offline", "离线"]]}/><Choice label="节点排序" value={value.sort} onChange={sort => change({ sort })} options={[["name", "按名称"], ["cpu", "CPU 优先"], ["disk", "磁盘优先"]]}/></div></div>
    <DataTable className="nodes-table" headings={headings}>
      {scene === 'loading' ? <LoadingRows columns={headings.length}/> : visible.map(node => <Row key={node.id} className={selectedId === node.id ? 'node-selected' : ''}><Cell><NodeIdentity node={node}/></Cell>{!compact && <Cell><span className="region-cell">{node.region}<span>{node.country}</span></span></Cell>}<Cell><Status tone={node.online ? 'success' : 'muted'}>{node.online ? '在线' : '离线'}</Status>{scene === 'partial' && node.id === 'tyo-01' && <span className="subtle-text">采集超时</span>}</Cell><Cell><Usage value={node.cpu} offline={!node.online}/></Cell><Cell><Usage value={node.memory} offline={!node.online}/></Cell><Cell><Usage value={node.disk} offline={!node.online}/></Cell><Cell><span className="network-cell">{node.online ? node.rx.toFixed(1) + ' / ' + node.tx.toFixed(1) : '—'}{node.online && <small>MiB/s</small>}</span></Cell><Cell>{onPreview ? <IconButton icon="activity" label={'查看 ' + node.name + ' 趋势'} aria-pressed={selectedId === node.id} onClick={() => onPreview(node.id)}/> : <Link to={'/nodes/' + node.id} className="row-open" aria-label={'查看 ' + node.name}><Icon name="chevron" size={16}/></Link>}</Cell></Row>)}
    </DataTable>
    <ul className="node-mobile-list" aria-label="节点资源列表">{scene === 'loading' ? Array.from({ length: 3 }, (_, i) => <li key={i}><Skeleton className="mobile-node-skeleton"/></li>) : visible.map(node => <li key={node.id} className={selectedId === node.id ? 'node-selected' : ''}><div className="mobile-node-head"><NodeIdentity node={node}/><Status tone={node.online ? 'success' : 'muted'}>{node.online ? '在线' : '离线'}</Status></div><dl className="mobile-node-resources">{[['CPU', node.cpu], ['内存', node.memory], ['磁盘', node.disk]].map(([label, reading]) => <div key={label}><dt>{label}</dt><dd className={Number(reading) >= 85 ? 'warning-value' : ''}>{node.online ? Number(reading).toFixed(1) + '%' : '—'}</dd></div>)}</dl><div className="mobile-node-footer"><span>网络 ↓ {node.online ? node.rx.toFixed(1) : '—'} / ↑ {node.online ? node.tx.toFixed(1) + ' MiB/s' : '—'}</span>{onPreview ? <Btn icon="activity" variant="ghost" aria-pressed={selectedId === node.id} onClick={() => onPreview(node.id)}>查看趋势</Btn> : <span>{node.country}</span>}</div>{scene === 'partial' && node.id === 'tyo-01' && <span className="subtle-text">采集超时 · 最近读数 14:32:18</span>}</li>)}</ul>
    {scene !== 'loading' && !visible.length && <Empty title={nodes.length ? '没有匹配的节点' : '暂无节点'} action={value.search || value.state !== 'all' ? <Btn onClick={() => change({ search: '', state: 'all' })}>清除筛选</Btn> : undefined}/>}
    <div className="table-footer"><span>{visible.filter(node => node.online).length} 个在线</span><span>最近采集 14:32:18</span></div>
  </Panel>;
}
