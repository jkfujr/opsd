import { useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import { Icon } from '../icons';
import { useDemo } from '../store';
import { Btn, Choice, Notice, PageHeading, Refresh } from '../components';
import { filterNodes, HealthRail, MetricsStrip, NodeTable, summarize, TrendChart, type NodeFilters } from '../monitoring';
import type { NodeItem } from '../data';

export function Overview() {
  const demo = useDemo();
  const [params, setParams] = useSearchParams();
  const region = params.get('region') ?? 'all';
  const filters: NodeFilters = { search: params.get('q') ?? '', state: ['online', 'offline'].includes(params.get('state') ?? '') ? params.get('state')! : 'all', sort: ['cpu', 'disk'].includes(params.get('sort') ?? '') ? params.get('sort')! : 'name' };
  const scoped = demo.scene === 'empty' ? [] : demo.nodes.filter(node => region === 'all' || node.region === region);
  const visible = filterNodes(scoped, filters);
  const previewId = params.get('preview') ?? '';
  const preview = visible.find(node => node.id === previewId);
  const view = params.get('view') === 'trends' ? 'trends' : 'nodes';
  const monitor = useRef<HTMLDivElement>(null);
  const [pendingPreviewId, setPendingPreviewId] = useState('');
  const update = (patch: Record<string, string>) => setParams(old => {
    const next = new URLSearchParams(old);
    Object.entries(patch).forEach(([key, value]) => value ? next.set(key, value) : next.delete(key));
    return next;
  }, { replace: true });
  useEffect(() => {
    if (previewId && !preview) setParams(old => { const next = new URLSearchParams(old); next.delete('preview'); return next; }, { replace: true });
  }, [previewId, preview, setParams]);
  useEffect(() => {
    if (!pendingPreviewId || pendingPreviewId !== previewId || view !== 'trends') return;
    const heading = monitor.current?.querySelector<HTMLHeadingElement>('.trend-heading');
    if (!heading) return;
    heading.focus({ preventScroll: true });
    heading.scrollIntoView({ block: 'start', behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' });
    setPendingPreviewId('');
  }, [pendingPreviewId, previewId, view]);
  const summary = summarize(visible);
  return <>
    <PageHeading title="概览" metadata={<><HealthRail nodes={visible}/><span>{summary.online.length} / {visible.length} 个节点在线</span><span className="meta-separator"/>采集于 14:32:18</>}>
      <Choice label="地域" value={region} onChange={value => update({ region: value === 'all' ? '' : value })} options={[['all', '全部地域'], ...[...new Set(demo.nodes.map(node => node.region))].map(value => [value, value] as const)]}/><Refresh/>
    </PageHeading>
    {demo.scene === 'partial' && <Notice action={{ label: '重新读取', onClick: () => demo.setScene('normal') }}>东京节点采集超时，保留最近读数</Notice>}
    {demo.scene === 'unknown' && <Notice action={{ label: '查询原操作', onClick: () => demo.showOperation(demo.operations.find(operation => operation.state === 'unknown')?.id) }}>有操作结果待核对</Notice>}
    <MetricsStrip nodes={visible}/>
    <div className="overview-view-switch segment" aria-label="监控视图">{[['nodes', '节点'], ['trends', '趋势']].map(([value, label]) => <button key={value} aria-pressed={view === value} onClick={() => update({ view: value })}>{label}</button>)}</div>
    <div className={'monitor-workspace view-' + view}>
      <div ref={monitor} className="monitor-context" role="region" aria-label="趋势与异常">
        <TrendChart layout="overview" nodes={preview ? [preview] : visible} scope={preview?.name ?? '全部节点'} onClear={preview ? () => update({ preview: '' }) : undefined} range={params.get('range') === '7d' ? '7d' : '24h'} metric={params.get('metric') === 'network' ? 'network' : 'resources'} onRangeChange={value => update({ range: value })} onMetricChange={value => update({ metric: value })}/>
        <Attention nodes={visible}/>
      </div>
      <div className="monitor-nodes"><NodeTable title="节点对照" nodes={scoped} compact filters={filters} onFiltersChange={next => update({ q: next.search, state: next.state === 'all' ? '' : next.state, sort: next.sort === 'name' ? '' : next.sort })} selectedId={preview?.id} onPreview={id => { setPendingPreviewId(id); update({ preview: id, view: 'trends' }); }}/></div>
    </div>
  </>;
}

function Attention({ nodes }: { nodes: NodeItem[] }) {
  const { operations, showOperation } = useDemo();
  const pending = operations.filter(operation => ['confirm', 'unknown', 'running'].includes(operation.state) && nodes.some(node => node.id === operation.nodeId));
  const offline = nodes.filter(node => !node.online), highDisk = nodes.filter(node => node.online && node.disk >= 85);
  const count = pending.length + offline.length + highDisk.length;
  return <section className="attention-panel" aria-labelledby="overview-attention-title">
    <div className="panel-heading"><div className="panel-heading-title"><h2 id="overview-attention-title">需关注</h2><span className={'count-badge ' + (count ? 'attention' : '')}>{count}</span></div><button className="text-link" onClick={() => showOperation()}>操作记录<Icon name="chevron" size={13}/></button></div>
    <div className="attention-list" role="region" aria-label="需关注事项" tabIndex={0}>
      {offline.map(node => <Link key={node.id} to={'/nodes/' + node.id} className="attention-item"><Icon name="nodes" size={16} className="attention-icon error"/><div><strong>节点离线</strong><span title={node.name}>{node.name}</span><small>最后在线 14:21 · {node.region}</small></div><Icon name="chevron" size={14}/></Link>)}
      {highDisk.map(node => <Link key={node.id} to={'/nodes/' + node.id} className="attention-item"><Icon name="storage" size={16} className="attention-icon warning"/><div><strong>磁盘 {node.disk}%</strong><span title={node.name}>{node.name}</span><small>可用 {((1 - node.disk / 100) * node.capacity).toFixed(1)} GiB · {node.region}</small></div><Icon name="chevron" size={14}/></Link>)}
      {pending.map(operation => <button key={operation.id} className="attention-item" onClick={() => showOperation(operation.id)}><Icon name="clock" size={16} className="attention-icon warning"/><div><strong>{operation.state === 'confirm' ? '等待确认' : operation.state === 'unknown' ? '结果待核对' : '执行中'}</strong><span title={operation.title}>{operation.title}</span><small>{operation.id} · {operation.time}</small></div><Icon name="chevron" size={14}/></button>)}
      {!count && <div className="all-healthy"><Icon name="check" size={20}/><span>暂无待处理事项</span></div>}
    </div>
  </section>;
}

export function Nodes() {
  const { nodes, scene, setScene, setEnrollmentOpen } = useDemo();
  const [params, setParams] = useSearchParams();
  const region = params.get('region') ?? 'all';
  const visible = scene === 'empty' ? [] : nodes.filter(node => region === 'all' || node.region === region);
  return <><PageHeading title="节点" metadata={<><HealthRail nodes={nodes}/>{nodes.length} 个节点 · {nodes.filter(node => node.online).length} 个在线</>}>
    <Choice label="地域" value={region} onChange={value => setParams(value === 'all' ? {} : { region: value }, { replace: true })} options={[['all', '全部地域'], ...[...new Set(nodes.map(node => node.region))].map(value => [value, value] as const)]}/><Refresh/><Btn icon="plus" variant="primary" onClick={() => setEnrollmentOpen(true)}>添加节点</Btn>
    </PageHeading>{scene === 'partial' && <Notice action={{ label: '重新读取', onClick: () => setScene('normal') }}>部分节点采集失败，保留最近读数</Notice>}<NodeTable nodes={visible}/></>;
}
