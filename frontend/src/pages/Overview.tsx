import { useMemo, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router';
import { AnimatePresence, motion } from 'motion/react';
import { ArrowDown, ArrowUp, Cpu, HardDrive, MemoryStick, Network, Plus, Server, ShieldAlert, WifiOff } from 'lucide-react';
import type { OverviewNode } from '../api/types';
import { useLive, useOverview } from '../app/queries';
import { useSession } from '../app/session';
import { openDock } from '../app/ops';
import { bytes, initials, pct, rate, ratio, ago, fullTime } from '../lib/format';
import { operationTitle, usageTone, readLabel } from '../lib/status';
import { useNow } from '../lib/hooks';
import { Spark } from '../ui/charts';
import { Badge, Button, Card, Dot, Empty, ErrorNotice, Meter, Notice, SearchInput, Segmented, Skeleton, Ticker, itemVariants, listVariants } from '../ui/kit';

type Sort = 'name' | 'cpu' | 'mem' | 'state';

export function Overview() {
  const query = useOverview();
  const session = useSession();
  const navigate = useNavigate();
  const [sort, setSort] = useState<Sort>('state');
  const [search, setSearch] = useState('');
  const now = useNow(1000);
  const data = query.data;
  const cluster = useLive('__cluster');

  const nodes = useMemo(() => {
    const list = (data?.nodes ?? []).filter((n) => !n.node.revoked_at);
    const q = search.trim().toLowerCase();
    const filtered = q ? list.filter((n) => `${n.node.name} ${n.node.region ?? ''} ${n.node.address ?? ''}`.toLowerCase().includes(q)) : list;
    const m = (n: OverviewNode) => n.reading.metrics;
    const memP = (n: OverviewNode) => ratio(m(n)?.memory_used, m(n)?.memory_total) ?? -1;
    return [...filtered].sort((a, b) => {
      if (sort === 'name') return a.node.name.localeCompare(b.node.name, 'zh-CN');
      if (sort === 'cpu') return (m(b)?.cpu_usage ?? -1) - (m(a)?.cpu_usage ?? -1);
      if (sort === 'mem') return memP(b) - memP(a);
      return Number(a.node.connected) - Number(b.node.connected) || Number(!!b.node.local) - Number(!!a.node.local) || a.node.name.localeCompare(b.node.name, 'zh-CN');
    });
  }, [data, sort, search]);

  const total = data?.nodes.filter((n) => !n.node.revoked_at).length ?? 0;
  const online = data?.nodes.filter((n) => !n.node.revoked_at && n.node.connected).length ?? 0;
  const s = data?.summary.metrics;
  const v = data?.summary.valid_nodes;
  const memP = ratio(s?.memory_used, s?.memory_total);
  const diskP = ratio(s?.disk_used, s?.disk_total);
  const urgent = (data?.pending_operations ?? []).filter((o) => o.state === 'awaiting_confirmation' || o.state === 'attention_required');
  const offline = (data?.nodes ?? []).filter((n) => !n.node.revoked_at && !n.node.connected);
  const cachedCount = data?.nodes.filter(({ node, reading }) => !node.revoked_at && reading.metrics && (!node.connected || reading.read.state !== 'ready')).length ?? 0;
  const greeting = greet(now);

  return (
    <div className="page">
      <motion.section className="hero" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.5, ease: [0.22, 1, 0.36, 1] }}>
        <div className="hero-title">
          <h1>{greeting}，{session.session.username}</h1>
          <p>
            {data ? (
              <>集群 <span className="strong num">{online}</span> / <span className="num">{total}</span> 个节点在线{data.pending_operations.length ? <> · <button type="button" className="strong" style={{ color: 'var(--accent)' }} onClick={() => openDock()}>{data.pending_operations.length} 项操作进行中</button></> : ' · 没有进行中的操作'}</>
            ) : '正在读取集群状态…'}
          </p>
        </div>
        <div className="row">
          <span className="live-indicator"><Dot tone={query.isError ? 'bad' : 'ok'} live={!query.isError} />{data ? `更新于 ${ago(data.updated_at, now)}` : '连接中'}</span>
          <Button icon={<Plus />} onClick={() => navigate('/nodes?add=1')}>添加节点</Button>
        </div>
      </motion.section>

      {query.isError && !data && <ErrorNotice error={query.error} retry={() => void query.refetch()} />}
      {cachedCount > 0 && <div className="row"><Badge tone="warn">{cachedCount} 个节点使用上次读数</Badge></div>}

      <motion.div className="kpis" variants={listVariants} initial="hidden" animate="show">
        <Kpi icon={<Cpu />} label="CPU" value={s?.cpu_usage} format={(x) => x.toFixed(1)} unit="%" sub={v ? `${v.cpu} 个节点 · ${s?.cpu_cores ?? 0} 核` : undefined} spark={cluster?.map((p) => p.cpu ?? null)} max={100} loading={!data} />
        <Kpi icon={<MemoryStick />} label="内存" value={memP} format={(x) => x.toFixed(1)} unit="%" sub={s ? `${bytes(s.memory_used)} / ${bytes(s.memory_total)}` : undefined} spark={cluster?.map((p) => p.mem ?? null)} max={100} loading={!data} />
        <Kpi icon={<HardDrive />} label="磁盘" value={diskP} format={(x) => x.toFixed(1)} unit="%" sub={s ? `${bytes(s.disk_used)} / ${bytes(s.disk_total)}` : undefined} spark={cluster?.map((p) => p.disk ?? null)} max={100} loading={!data} />
        <Kpi icon={<Network />} label="网络" value={s?.network_rx !== undefined || s?.network_tx !== undefined ? (s?.network_rx ?? 0) + (s?.network_tx ?? 0) : undefined} format={(x) => rate(x)} sub={s ? <><span><ArrowDown width={12} height={12} /> {rate(s.network_rx)}</span><span><ArrowUp width={12} height={12} /> {rate(s.network_tx)}</span></> : undefined} spark={cluster?.map((p) => p.rx === undefined && p.tx === undefined ? null : (p.rx ?? 0) + (p.tx ?? 0))} loading={!data} />
      </motion.div>

      <AnimatePresence>
        {(urgent.length > 0 || offline.length > 0) && (
          <motion.div className="attention" initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: 'auto' }} exit={{ opacity: 0, height: 0 }}>
            {urgent.map((op) => (
              <Notice key={op.id} tone="warn" icon={<ShieldAlert />} title={`${operationTitle(op)}${op.target_name ? ` · ${op.target_name}` : ''}`} actions={<Button size="sm" variant="primary" onClick={() => openDock(op.id)}>{op.state === 'awaiting_confirmation' ? '去确认' : '去处理'}</Button>}>
                {op.state === 'awaiting_confirmation' ? '变更已应用，等待确认；到期未确认将自动恢复。' : op.error?.message ?? '操作需要人工处理。'}
              </Notice>
            ))}
            {offline.length > 0 && (
              <Notice tone="idle" icon={<WifiOff />} title={`${offline.length} 个节点离线`}>
                {offline.slice(0, 6).map((n) => n.node.name).join('、')}{offline.length > 6 ? ' 等' : ''}
                {offline[0]?.node.last_seen_at && <> · 最近在线 {ago(offline[0].node.last_seen_at, now)}</>}
              </Notice>
            )}
          </motion.div>
        )}
      </AnimatePresence>

      <section className="stack">
        <div className="toolbar">
          <h2 className="grow">节点</h2>
          <SearchInput className="search" value={search} onChange={setSearch} placeholder="筛选节点" />
          <Segmented value={sort} onChange={setSort} options={[{ value: 'state', label: '状态' }, { value: 'cpu', label: 'CPU' }, { value: 'mem', label: '内存' }, { value: 'name', label: '名称' }]} />
        </div>
        {!data ? (
          <div className="fleet">{Array.from({ length: 6 }, (_, i) => <Card key={i} pad><div className="stack"><Skeleton width="50%" height={18} /><Skeleton height={36} /><Skeleton width="70%" /></div></Card>)}</div>
        ) : nodes.length === 0 ? (
          <Card><Empty icon={<Server />} title={search ? '没有匹配的节点' : '还没有节点'} action={!search && <Button variant="primary" icon={<Plus />} onClick={() => navigate('/nodes?add=1')}>添加第一个节点</Button>}>{search ? '换个关键词试试。' : '生成接入命令后，在目标服务器以 root 执行即可加入集群。'}</Empty></Card>
        ) : (
          <motion.div className="fleet" variants={listVariants} initial="hidden" animate="show" layout>
            <AnimatePresence initial={false}>
              {nodes.map((n) => <NodeCard key={n.node.id} item={n} now={now} onOpen={() => navigate(`/nodes/${encodeURIComponent(n.node.id)}`)} />)}
            </AnimatePresence>
          </motion.div>
        )}
      </section>
    </div>
  );
}

function greet(now: number) {
  const h = new Date(now).getHours();
  if (h < 5) return '夜深了';
  if (h < 11) return '早上好';
  if (h < 14) return '中午好';
  if (h < 18) return '下午好';
  return '晚上好';
}

function Kpi({ icon, label, value, format, unit, sub, spark, max, loading }: { icon: ReactNode; label: string; value?: number; format: (v: number) => string; unit?: string; sub?: ReactNode; spark?: (number | null)[]; max?: number; loading?: boolean }) {
  const tone = unit === '%' ? usageTone(value) : 'accent';
  const color = tone === 'bad' ? 'var(--bad)' : tone === 'warn' ? 'var(--warn)' : 'var(--accent)';
  return (
    <motion.div variants={itemVariants}>
      <Card className="kpi" hoverable>
        <div className="kpi-top">{icon}{label}</div>
        {loading ? <Skeleton width="60%" height={34} style={{ marginTop: 6 }} /> : (
          <div className="kpi-value"><Ticker value={value} format={format} />{unit && value !== undefined && <small>{unit}</small>}</div>
        )}
        <div className="kpi-sub">{loading ? <Skeleton width="80%" /> : sub ?? '—'}</div>
        <div className="kpi-spark"><Spark values={pad(spark)} max={max} height={44} tone={color} /></div>
      </Card>
    </motion.div>
  );
}

function pad(values?: (number | null)[], size = 32) {
  const v = values ?? [];
  if (v.length >= size) return v.slice(-size);
  return [...Array.from({ length: size - v.length }, () => null), ...v];
}

function NodeCard({ item, now, onOpen }: { item: OverviewNode; now: number; onOpen: () => void }) {
  const { node, reading } = item;
  const m = reading.metrics;
  const history = useLive(node.id);
  const mem = ratio(m?.memory_used, m?.memory_total);
  const disk = ratio(m?.disk_used, m?.disk_total);
  const state = reading.read.state;
  const failed = state === 'timeout' || state === 'error';
  const offline = !node.connected || state === 'offline';
  const cached = m && (offline || state !== 'ready');
  const problem = failed ? readLabel[state] : offline ? readLabel.offline : state === 'stale' ? '数据未更新' : state !== 'ready' ? readLabel[state] : reading.state === 'partial' ? '部分指标不可用' : undefined;
  const tone = failed ? 'bad' : offline ? 'idle' : problem ? 'warn' : 'ok';
  return (
    <motion.div layout variants={itemVariants} exit={{ opacity: 0, scale: 0.96 }} transition={{ layout: { type: 'spring', stiffness: 400, damping: 36 } }}>
      <Card hoverable className="node-card" data-offline={!node.connected} onClick={onOpen} role="link" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter') onOpen(); }}>
        <div className="nc-head">
          <span className="avatar">{initials(node.name)}</span>
          <div className="nc-name">
            <strong>{node.name}</strong>
            <span>{[node.region, node.address].filter(Boolean).join(' · ') || '未设置地域'}</span>
          </div>
          {node.local && <Badge outline>本机</Badge>}
          <Dot tone={tone} live={node.connected && !problem} />
        </div>
        {m ? (
          <>
            <div style={{ margin: '-4px -16px 0' }}><Spark values={pad(history?.map((p) => p.cpu ?? null), 28)} max={100} height={34} tone={usageTone(m.cpu_usage) === 'bad' ? 'var(--bad)' : usageTone(m.cpu_usage) === 'warn' ? 'var(--warn)' : 'var(--accent)'} /></div>
            <div className="nc-metrics">
              <MetricBar label="CPU" value={m.cpu_usage} />
              <MetricBar label="内存" value={mem} />
              <MetricBar label="磁盘" value={disk} />
            </div>
          </>
        ) : (
          <div className="muted" style={{ fontSize: 13, minHeight: 78, display: 'grid', placeItems: 'center' }}>{node.connected ? reading.error || problem || '等待采集' : `离线${node.last_seen_at ? ` · ${ago(node.last_seen_at, now)}` : ''}`}</div>
        )}
        {m && problem && <div className="row wrap">
          <Badge tone={tone} title={reading.error || undefined}>{problem}</Badge>
          {cached && m.collected_at && <span className="muted">采集于 <time dateTime={m.collected_at}>{fullTime(m.collected_at)}</time></span>}
        </div>}
        <div className="nc-foot">
          {m && <span className="net"><ArrowDown /> {rate(m.network_rx)}</span>}
          {m && <span className="net"><ArrowUp /> {rate(m.network_tx)}</span>}
          <span className="spacer" />
          {reading.firewall === 'enabled' && <Badge tone="ok">防火墙</Badge>}
          {reading.firewall === 'disabled' && <Badge tone="warn">防火墙未启用</Badge>}
          {m && <span className="num">{m.cpu_cores} 核</span>}
        </div>
      </Card>
    </motion.div>
  );
}

function MetricBar({ label, value }: { label: string; value?: number }) {
  return (
    <div className="nc-metric">
      <div className="label"><span>{label}</span><b>{pct(value)}</b></div>
      <Meter value={value} tone={usageTone(value)} thin />
    </div>
  );
}
