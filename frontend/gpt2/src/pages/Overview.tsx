import { useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { ArrowUpRight, Cpu, MemoryStick, Network, Plus, Server, ServerOff, HardDrive, Clock3 } from 'lucide-react';
import { aggregate, bytes, collectedAt, readingError, nodeMetrics, overviewQuery, percent, ratio, speed, type OverviewNode } from '../monitoring';
import { Btn, Cell, Choice, DataTable, Empty, Heading, IconLink, LoadingRows, Meter, Panel, Refresh, Row, SearchBox, Skeleton, Status, TabSet } from '../ui';
import { Failure, date } from '../business-ui';
import { TrendChart } from '../charts';
import { EnrollmentDialog } from '../enrollment';
import { useOperations } from '../operations';
export function Overview() {
  const query = useQuery(overviewQuery), [params, setParams] = useSearchParams(), [add, setAdd] = useState<'add' | 'tokens' | null>(null), trigger = useRef<HTMLButtonElement>(null);
  const region = params.get('region') ?? 'all', nodeID = params.get('node') ?? 'all', range = params.get('range') === '7d' ? '7d' : '24h';
  const nodes = query.data?.nodes ?? [], scoped = nodes.filter(({ node }) => (region === 'all' || (node.region ?? '') === region) && (nodeID === 'all' || node.id === nodeID));
  const failures = scoped.flatMap(item => { const error = readingError(item); return error ? [`${item.node.name}：${error}`] : []; });
  const summary = aggregate(scoped), online = scoped.filter(n => n.node.connected).length;
  const setParam = (key: string, value: string) => setParams(old => { const next = new URLSearchParams(old); if (value === 'all' || value === '24h') next.delete(key); else next.set(key, value); return next; });
  const cards = [
    { name: '在线节点', Icon: Server, value: String(online), unit: `/ ${scoped.length}`, note: scoped.length ? `${scoped.length - online} 个离线` : '暂无节点' },
    { name: '平均 CPU', Icon: Cpu, value: percent(summary.cpu_usage), note: summary.cpu_cores ? `${summary.cpu_cores} 个有效逻辑核心` : '暂无有效读数' },
    { name: '内存使用', Icon: MemoryStick, value: percent(ratio(summary.memory_used, summary.memory_total)), note: `${bytes(summary.memory_used)} / ${bytes(summary.memory_total)}` },
    { name: '网络流量', Icon: Network, value: speed(summary.network_rx), note: `发送 ${speed(summary.network_tx)}` },
  ];
  return <div className="page-stack"><Heading title="概览" meta={<><span>{online} / {scoped.length} 个节点在线</span><span className="meta-divider"/><span>采集于 {date(collectedAt(scoped))}</span></>}><Choice label="地域" value={region} onChange={v => { setParam('region', v); }} options={[["all", "全部地域"], ...Array.from(new Set(nodes.map(n => n.node.region).filter((v): v is string => !!v))).map(v => [v, v] as const)]}/><Choice resource label="监控节点" value={nodeID} onChange={v => setParam('node', v)} options={[["all", "全部节点"], ...nodes.map(n => [n.node.id, n.node.name] as const)]}/><Refresh/><Btn ref={trigger} variant="primary" icon={Plus} onClick={() => setAdd('add')}>添加节点</Btn></Heading>
  <Failure error={query.error} retry={() => void query.refetch()}/>{failures.length > 0 && <div className="notice" role="alert">{failures.join('；')}</div>}
  <section className="metrics-grid" aria-label="资源汇总">{cards.map(({ name, Icon, value, unit, note }) => <Panel key={name} className="metric-card"><div className="metric-label"><Icon size={18} aria-hidden/>{name}</div>{query.isPending ? <Skeleton className="metric-skeleton"/> : <div className="metric-value">{value}<span className="metric-unit">{unit}</span></div>}<div className="metric-note">{note}</div></Panel>)}</section>
  <section className="overview-main-grid"><Panel title="网络流量" className="network-panel" actions={<Choice label="趋势时间范围" value={range} onChange={v => setParam('range', v)} options={[["24h", "24 小时"], ["7d", "7 天"]]}/>}><div className="network-readings"><span>接收 <strong>{speed(summary.network_rx)}</strong></span><span>发送 <strong>{speed(summary.network_tx)}</strong></span></div><TrendChart nodes={scoped.map(n => n.node)} range={range}/></Panel><ResourceRanking nodes={scoped}/></section>
  <section className="overview-secondary-grid"><Panel title="资源趋势" className="resource-panel"><TrendChart nodes={scoped.map(n => n.node)} range={range} metric="resources" compact/></Panel><Panel title="需关注" className="attention-panel"><Attention nodes={scoped}/></Panel></section>
  <NodeComparison nodes={scoped} loading={query.isPending}/>{add && <EnrollmentDialog mode={add} trigger={trigger} onClose={() => setAdd(null)} onManage={() => setAdd('tokens')}/>}</div>;
}
function metricValue(item: OverviewNode, field: string) { const m = nodeMetrics(item); return field === 'cpu' ? m?.cpu_usage : field === 'memory' ? ratio(m?.memory_used, m?.memory_total) : ratio(m?.disk_used, m?.disk_total); }
function ResourceRanking({ nodes }: { nodes: OverviewNode[] }) {
  const [metric, setMetric] = useState('cpu'), sorted = [...nodes].filter(n => metricValue(n, metric) !== undefined).sort((a, b) => metricValue(b, metric)! - metricValue(a, metric)!);
  return <Panel title="资源排行" className="ranking-panel" actions={<IconLink to="/nodes" icon={ArrowUpRight} label="查看全部节点"/>}><TabSet context="ranking" className="ranking-tabs" options={[["cpu", "CPU"], ["memory", "内存"], ["disk", "磁盘"]]} value={metric} onChange={setMetric}><ol className="rank-list">{sorted.map((item, i) => <li key={item.node.id}><Link to={`/nodes/${item.node.id}`} className="rank-link"><span className="rank-number">{i + 1}</span><span className="rank-name">{item.node.name}</span><strong>{percent(metricValue(item, metric))}</strong></Link><Meter value={metricValue(item, metric)!} label={`${item.node.name} 使用率`}/></li>)}</ol>{!sorted.length && <Empty title="暂无排行数据"/>}</TabSet></Panel>;
}
function Attention({ nodes }: { nodes: OverviewNode[] }) {
  const operations = useOperations(), offline = nodes.filter(n => !n.node.connected), high = nodes.filter(n => (metricValue(n, 'disk') ?? 0) >= 80), pending = operations.items.filter(op => ['awaiting_confirmation', 'attention_required'].includes(op.state));
  return <div className="attention-list">{offline.map(({ node }) => <Link className="attention-row" to={`/nodes/${node.id}`} key={node.id}><ServerOff size={19} aria-hidden/><div><strong>节点离线</strong><span>{node.name}</span></div><ArrowUpRight size={17} aria-hidden/></Link>)}{high.map(n => <Link className="attention-row" to={`/nodes/${n.node.id}`} key={n.node.id}><HardDrive size={19} aria-hidden/><div><strong>磁盘 {percent(metricValue(n, 'disk'))}</strong><span>{n.node.name}</span></div><ArrowUpRight size={17} aria-hidden/></Link>)}{pending.map(op => <button className="attention-row" key={op.id} onClick={() => operations.show(op.id)}><Clock3 size={19} aria-hidden/><div><strong>{op.state === 'awaiting_confirmation' ? '等待确认' : '结果待核对'}</strong><span>{op.id}</span></div><ArrowUpRight size={17} aria-hidden/></button>)}{!offline.length && !high.length && !pending.length && <Empty title="当前无待处理事项"/>}</div>;
}
function NodeComparison({ nodes, loading }: { nodes: OverviewNode[]; loading: boolean }) {
  const [query, setQuery] = useState(''), filtered = useMemo(() => nodes.filter(({ node }) => `${node.name} ${node.address ?? ''} ${node.region ?? ''}`.toLowerCase().includes(query.toLowerCase())), [nodes, query]);
  return <Panel title="节点对照" className="comparison-panel" actions={<SearchBox value={query} onChange={setQuery}/>}><DataTable headings={['节点', '状态', 'CPU', '内存', '磁盘', '网络 ↓ / ↑', '']}>
  {loading ? <LoadingRows columns={7}/> : filtered.map(item => <Row key={item.node.id}><Cell><Link className="object-name" to={`/nodes/${item.node.id}`}>{item.node.name}</Link><span className="object-meta">{item.node.region || '—'} · {item.node.address || '—'}</span></Cell><Cell><Status tone={item.node.connected ? 'success' : 'neutral'}>{item.node.connected ? '在线' : '离线'}</Status></Cell>{['cpu', 'memory', 'disk'].map(field => <Cell key={field}>{percent(metricValue(item, field))}</Cell>)}<Cell>{speed(nodeMetrics(item)?.network_rx)} / {speed(nodeMetrics(item)?.network_tx)}</Cell><Cell><IconLink to={`/nodes/${item.node.id}`} icon={ArrowUpRight} label={`查看 ${item.node.name}`}/></Cell></Row>)}</DataTable>{!loading && !filtered.length && <Empty title="暂无匹配节点"/>}</Panel>;
}
