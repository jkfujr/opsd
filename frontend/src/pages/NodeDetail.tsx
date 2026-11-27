import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router';
import { AnimatePresence, motion } from 'motion/react';
import { ArrowDown, ArrowUp, Boxes, Eye, Globe, KeyRound, RefreshCw, Save, ShieldCheck, ShieldOff, SquareTerminal, Pencil } from 'lucide-react';
import { call, enc, errorText } from '../api/http';
import type { NodeAccount, NodeConfig, NodeInfo, NodePassword, Operation, PublicAddresses, PublicAddress } from '../api/types';
import { useLive, useNode, useNodeAccount, useNodeConfig, useNodeStatus, useOverview, usePublicAddresses } from '../app/queries';
import { useCommand } from '../app/commands';
import { confirm } from '../app/confirm';
import { invalidateResource } from '../app/query';
import { ago, bytes, fullTime, initials, pct, rate, ratio } from '../lib/format';
import { usageTone } from '../lib/status';
import { AreaChart, Spark } from '../ui/charts';
import { Badge, Button, Card, CardHead, CopyText, Dialog, Dot, ErrorNotice, Field, Input, Notice, Ring, Skeleton, Switch, Tabs, Tag, Textarea } from '../ui/kit';

type Tab = 'overview' | 'config' | 'network' | 'account';

export function NodeDetail() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const rawTab = params.get('tab');
  const tab = (rawTab === 'configuration' ? 'config' : (rawTab as Tab)) || 'overview';
  const status = useNodeStatus(id);
  const info = useNode(id);
  const overview = useOverview();
  const reading = overview.data?.nodes.find((n) => n.node.id === id)?.reading;
  const node = status.data;
  const name = node?.name ?? info.data?.value.name ?? '节点';
  const [renaming, setRenaming] = useState(false);

  return (
    <div className="page">
      <motion.div className="node-hero" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }}>
        <div className="nh-title">
          <span className="avatar">{initials(name)}</span>
          <div className="col" style={{ gap: 4, minWidth: 0 }}>
            <div className="row">
              <h1 className="ellipsis">{name}</h1>
              <button type="button" className="icon-btn sm" aria-label="修改名称" onClick={() => setRenaming(true)}><Pencil /></button>
              {node && (node.connected ? <Badge tone={node.writable ? 'ok' : 'warn'} dot>{node.writable ? '在线' : '只读'}</Badge> : <Badge dot>离线</Badge>)}
              {node?.local && <Badge outline>本机</Badge>}
            </div>
            <div className="row muted wrap" style={{ fontSize: 13, gap: 12 }}>
              {node?.region && <span>{node.region}</span>}
              {node?.address && <span className="mono">{node.address}</span>}
              <CopyText value={id} display={id.slice(0, 16) + (id.length > 16 ? '…' : '')} />
            </div>
          </div>
        </div>
        <div className="row wrap">
          <Button icon={<SquareTerminal />} disabled={!node?.connected} onClick={() => navigate(`/tools/terminal?node=${enc(id)}`)}>终端</Button>
          <Button icon={<ShieldCheck />} onClick={() => navigate(`/firewall?node=${enc(id)}`)}>防火墙</Button>
          <Button icon={<Boxes />} onClick={() => navigate(`/containers?node=${enc(id)}`)}>容器</Button>
        </div>
      </motion.div>

      {status.isError && <ErrorNotice error={status.error} retry={() => void status.refetch()} />}
      {node?.write_blocked_reason && <Notice tone="warn" title="节点当前只读">{node.write_blocked_reason}</Notice>}

      <Gauges id={id} reading={reading} />

      <Tabs value={tab} onChange={(t) => setParams({ tab: t }, { replace: true })} options={[
        { value: 'overview', label: '概况' },
        { value: 'config', label: '配置' },
        { value: 'network', label: '公网地址' },
        { value: 'account', label: '节点账号' },
      ]} />
      <AnimatePresence mode="wait">
        <motion.div key={tab} initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -4 }} transition={{ duration: 0.22 }}>
          {tab === 'overview' && <OverviewTab id={id} />}
          {tab === 'config' && <ConfigTab id={id} />}
          {tab === 'network' && <NetworkTab id={id} />}
          {tab === 'account' && <AccountTab id={id} />}
        </motion.div>
      </AnimatePresence>

      <RenameDialog open={renaming} onClose={() => setRenaming(false)} id={id} info={info.data} />
    </div>
  );
}

function Gauges({ id, reading }: { id: string; reading?: { metrics?: NodeConfigMetrics } }) {
  const m = reading?.metrics;
  const history = useLive(id);
  const mem = ratio(m?.memory_used, m?.memory_total);
  const disk = ratio(m?.disk_used, m?.disk_total);
  const items = [
    { label: 'CPU', value: m?.cpu_usage, sub: m ? `${m.cpu_cores} 核` : '—', key: 'cpu' as const },
    { label: '内存', value: mem, sub: m ? `${bytes(m.memory_used)} / ${bytes(m.memory_total)}` : '—', key: 'mem' as const },
    { label: '磁盘', value: disk, sub: m ? `${bytes(m.disk_used)} / ${bytes(m.disk_total)}` : '—', key: 'disk' as const },
  ];
  return (
    <div className="gauges">
      {items.map((g) => (
        <Card key={g.label} className="gauge">
          <Ring value={g.value} size={58} stroke={6} tone={usageTone(g.value)}><span className="num" style={{ fontSize: 12, fontWeight: 650 }}>{g.value === undefined ? '—' : Math.round(g.value)}</span></Ring>
          <div className="g-text grow">
            <span>{g.label}</span>
            <strong>{pct(g.value)}</strong>
            <span className="ellipsis">{g.sub}</span>
          </div>
          <div style={{ width: 80 }}><Spark values={(history ?? []).map((p) => p[g.key] ?? null).slice(-24)} max={100} height={32} /></div>
        </Card>
      ))}
      <Card className="gauge">
        <div className="g-text grow">
          <span>网络</span>
          <strong className="row" style={{ fontSize: 15, gap: 4 }}><ArrowDown width={14} height={14} style={{ color: 'var(--accent)' }} />{rate(m?.network_rx)}</strong>
          <strong className="row" style={{ fontSize: 15, gap: 4 }}><ArrowUp width={14} height={14} style={{ color: 'var(--info)' }} />{rate(m?.network_tx)}</strong>
        </div>
        <div style={{ width: 90 }}><Spark values={(history ?? []).map((p) => (p.rx ?? 0) + (p.tx ?? 0)).slice(-24)} height={40} tone="var(--info)" /></div>
      </Card>
    </div>
  );
}
type NodeConfigMetrics = { cpu_usage?: number; cpu_cores: number; memory_used?: number; memory_total?: number; disk_used?: number; disk_total?: number; network_rx?: number; network_tx?: number };

function OverviewTab({ id }: { id: string }) {
  const status = useNodeStatus(id);
  const info = useNode(id);
  const history = useLive(id);
  const n = status.data;
  const fallbacks = n?.endpoints?.filter((endpoint) => endpoint !== n?.address) ?? [];
  const series = [
    { name: 'CPU', color: 'var(--accent)', points: (history ?? []).map((p) => ({ t: p.t, v: p.cpu ?? null })) },
    { name: '内存', color: 'oklch(0.74 0.15 250)', points: (history ?? []).map((p) => ({ t: p.t, v: p.mem ?? null })) },
  ];
  return (
    <div className="split">
      <Card>
        <CardHead title="实时负载" sub="本页打开期间每 3 秒采样" />
        <div className="card-body">
          <AreaChart series={series} height={220} yMax={100} format={(v) => `${Math.round(v)}%`} timeFormat={(t) => new Date(t).toLocaleTimeString('zh-CN', { hour12: false })} empty={<span className="muted">等待采样…</span>} />
          <div className="legend" style={{ marginTop: 8 }}>{series.map((s) => <span key={s.name}><i style={{ background: s.color }} />{s.name}</span>)}</div>
        </div>
      </Card>
      <Card>
        <CardHead title="节点信息" />
        <div className="card-body">
          {!n ? <div className="stack">{Array.from({ length: 5 }, (_, i) => <Skeleton key={i} />)}</div> : (
            <dl className="kv">
              <dt>节点 ID</dt><dd><CopyText value={n.id} /></dd>
              <dt>公钥</dt><dd><CopyText value={n.public_key} display={n.public_key.slice(0, 24) + '…'} /></dd>
              <dt>通信地址</dt><dd className="mono">{n.address || '—'}</dd>
              {fallbacks.length > 0 && <><dt>备用端点</dt><dd className="row wrap">{fallbacks.map((e) => <Tag key={e}>{e}</Tag>)}</dd></>}
              <dt>最近在线</dt><dd>{n.connected ? '当前在线' : n.last_seen_at ? `${ago(n.last_seen_at)}（${fullTime(n.last_seen_at)}）` : '—'}</dd>
              <dt>控制台</dt><dd>{n.console_enabled ? <Badge tone="ok">已开启</Badge> : <Badge>未开启</Badge>}</dd>
              <dt>授权版本</dt><dd className="num">{n.api_authorization_version ?? '—'}</dd>
              <dt>协调候选</dt><dd>{n.coordinator_candidate ? '是' : '否'}</dd>
              {n.shared_proxy && <><dt>OPSD 数据</dt><dd><Badge tone={n.shared_proxy.proxy_ready ? 'ok' : 'warn'} dot>{n.shared_proxy.proxy_ready ? '代理就绪' : n.shared_proxy.state || n.shared_proxy.phase}</Badge></dd></>}
              {info.data && <><dt>记录版本</dt><dd className="num">{info.data.value.revision}</dd></>}
            </dl>
          )}
        </div>
      </Card>
      <DangerZone id={id} info={info.data} local={!!n?.local} />
    </div>
  );
}

function DangerZone({ id, info, local }: { id: string; info?: { value: NodeInfo; etag?: string }; local: boolean }) {
  const navigate = useNavigate();
  const revoke = useCommand<void, NodeInfo>({
    label: '撤销节点',
    node: id,
    run: async (_v, key) => (await call<NodeInfo>('POST', `/nodes/${enc(id)}/actions`, { key, body: { action: 'revoke', expected_revision: info!.value.revision } })).data,
    onSuccess: () => { invalidateResource('nodes'); navigate('/nodes'); },
  });
  if (local) return null;
  return (
    <Card style={{ boxShadow: 'inset 0 0 0 1px color-mix(in oklch, var(--bad) 28%, transparent)' }}>
      <CardHead title="撤销节点" sub="撤销后其他成员拒绝该节点的请求，并清理其数据库账号；不可撤回。" actions={
        <Button variant="danger" icon={<ShieldOff />} busy={revoke.busy} disabled={!info} onClick={async () => {
          if (await confirm({ title: `撤销 ${info?.value.name ?? '节点'}？`, body: '该节点将无法再与集群通信。离线节点会在恢复连接后得知撤销。', danger: true, confirmLabel: '撤销节点', typeToConfirm: info?.value.name })) void revoke.run();
        }}>撤销</Button>
      } />
    </Card>
  );
}

function RenameDialog({ open, onClose, id, info }: { open: boolean; onClose: () => void; id: string; info?: { value: NodeInfo; etag?: string } }) {
  const [name, setName] = useState('');
  useEffect(() => { if (open) setName(info?.value.name ?? ''); }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const rename = useCommand<string, Operation>({
    label: '修改节点名称',
    node: id,
    run: async (value, key) => (await call<Operation>('PATCH', `/nodes/${enc(id)}`, { key, ifMatch: info?.etag, body: { name: value } })).data,
    onSuccess: onClose,
    onConflict: () => invalidateResource('nodes'),
  });
  return (
    <Dialog open={open} onClose={onClose} title="修改节点名称" width={440} footer={<><Button variant="ghost" onClick={onClose}>取消</Button><Button variant="primary" busy={rename.busy} disabled={!name.trim() || name.trim() === info?.value.name} onClick={() => void rename.run(name.trim())}>保存</Button></>}>
      <Field label="名称"><Input value={name} onChange={(e) => setName(e.target.value)} autoFocus maxLength={64} onKeyDown={(e) => { if (e.key === 'Enter' && name.trim()) void rename.run(name.trim()); }} /></Field>
    </Dialog>
  );
}

function ConfigTab({ id }: { id: string }) {
  const config = useNodeConfig(id);
  const [draft, setDraft] = useState<Partial<NodeConfig>>({});
  useEffect(() => setDraft({}), [config.data?.etag]);
  const value = config.data?.value;
  const merged = value ? { ...value, ...draft } : undefined;
  const endpointText = merged?.endpoints?.join('\n') ?? '';
  const endpoints = endpointText.split('\n').map((endpoint) => endpoint.trim()).filter(Boolean);
  const endpointError = endpoints.length > 7 && (draft.endpoints !== undefined || draft.address !== undefined) ? '最多可保存 7 个备用端点' : undefined;
  const dirty = Object.keys(draft).length > 0;
  const save = useCommand<void, Operation>({
    label: '保存节点配置',
    node: id,
    run: async (_v, key) => {
      const body = {
        ...draft,
        ...(draft.address === undefined ? {} : { address_mode: 'manual' }),
        ...(draft.endpoints === undefined ? {} : { endpoints }),
      };
      return (await call<Operation>('PATCH', `/nodes/${enc(id)}/configuration`, { key, ifMatch: config.data?.etag, body })).data;
    },
    onSuccess: () => setDraft({}),
    onConflict: () => void config.refetch(),
  });
  const set = <K extends keyof NodeConfig>(k: K, v: NodeConfig[K]) => setDraft((d) => { const next: Partial<NodeConfig> = { ...d }; if (value && value[k] === v) delete next[k]; else next[k] = v; return next; });
  if (config.isError) return <ErrorNotice error={config.error} retry={() => void config.refetch()} />;
  if (!merged) return <Card pad><div className="stack">{Array.from({ length: 6 }, (_, i) => <Skeleton key={i} height={36} />)}</div></Card>;
  return (
    <div className="stack">
      {value?.cached && <Notice tone="warn" title="显示的是缓存配置">节点当前不可达，{value.last_synced_at ? `最后同步于 ${ago(value.last_synced_at)}` : ''}；恢复连接前无法保存。</Notice>}
      {value?.network_error && <Notice tone="warn" title="监听配置不一致">{value.network_error.message}</Notice>}
      <Card>
        <CardHead title="节点设置" />
        <div className="card-body form-grid" id="node-settings">
          <Field label="名称"><Input value={merged.name} onChange={(e) => set('name', e.target.value)} /></Field>
          <Field label="地域"><Input value={merged.region} onChange={(e) => set('region', e.target.value)} /></Field>
          <Field label="通信地址" hint="其他成员连接本节点的 URL"><Input mono value={merged.address} onChange={(e) => set('address', e.target.value)} /></Field>
          <Field label="通信监听" hint="修改监听需要在节点本机重启服务"><Input mono value={merged.listen} onChange={(e) => set('listen', e.target.value)} /></Field>
          <Field className="span-2" label="备用端点" hint={<span id="node-endpoints-hint">每行一个 URL，最多 7 个</span>}
            error={endpointError && <span id="node-endpoints-hint" role="alert">{endpointError}</span>}>
            <Textarea mono rows={3} value={endpointText} aria-describedby="node-endpoints-hint" aria-invalid={!!endpointError} onChange={(e) => {
              const text = e.target.value;
              set('endpoints', text === (value?.endpoints?.join('\n') ?? '') ? value?.endpoints : text.split('\n'));
            }} />
          </Field>
        </div>
      </Card>
      <Card>
        <CardHead title="控制台与 API" />
        <div className="list">
          <ToggleRow title="Web 控制台" sub={`监听 ${merged.console_listen}`} checked={merged.console} onChange={(v) => set('console', v)} />
          <ToggleRow title="外部 API" sub="允许使用 Bearer 令牌访问 /api/v2" checked={!!merged.api} onChange={(v) => set('api', v)} />
          <ToggleRow title="协调候选" sub="可接管数据库等共享流程的协调" checked={!!merged.coordinator_candidate} onChange={(v) => set('coordinator_candidate', v)} />
        </div>
        <div className="card-body form-grid" style={{ borderTop: '1px solid var(--line)' }}>
          <Field label="控制台监听"><Input mono value={merged.console_listen} onChange={(e) => set('console_listen', e.target.value)} /></Field>
          <Field label="历史采集间隔（分钟）"><Input type="number" min={1} max={60} value={merged.interval_minutes} onChange={(e) => set('interval_minutes', Number(e.target.value))} /></Field>
        </div>
      </Card>
      <SaveBar dirty={dirty} busy={save.busy} onReset={() => setDraft({})} onSave={() => void save.run()} disabled={!!value?.cached || !!endpointError} />
    </div>
  );
}

export function ToggleRow({ title, sub, checked, onChange, disabled }: { title: string; sub?: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <div className="list-item">
      <div className="li-main"><span className="li-title">{title}</span>{sub && <span className="li-sub">{sub}</span>}</div>
      <Switch checked={checked} onChange={onChange} label={title} disabled={disabled} />
    </div>
  );
}

export function SaveBar({ dirty, busy, onReset, onSave, disabled }: { dirty: boolean; busy: boolean; onReset: () => void; onSave: () => void; disabled?: boolean }) {
  return (
    <AnimatePresence>
      {dirty && (
        <motion.div
          initial={{ opacity: 0, y: 24 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 16 }}
          transition={{ type: 'spring', stiffness: 420, damping: 34 }}
          style={{ position: 'sticky', bottom: 16, zIndex: 5, display: 'flex', justifyContent: 'center' }}
        >
          <div className="dock-pill" style={{ paddingRight: 6, gap: 12 }}>
            <Dot tone="warn" /> 有未保存的修改
            <Button size="sm" variant="ghost" onClick={onReset}>放弃</Button>
            <Button size="sm" variant="primary" icon={<Save />} busy={busy} disabled={disabled} onClick={onSave}>保存</Button>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

function NetworkTab({ id }: { id: string }) {
  const q = usePublicAddresses(id);
  const v = q.data?.value;
  const refresh = useCommand<void, PublicAddresses>({
    label: '重新发现公网地址',
    node: id,
    run: async () => (await call<PublicAddresses>('POST', `/nodes/${enc(id)}/public-addresses/actions`, { body: { action: 'refresh', expected_revision: v!.revision } })).data,
    onSuccess: () => void q.refetch(),
  });
  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  if (!v) return <Card pad><Skeleton height={120} /></Card>;
  return (
    <div className="stack">
      {v.cached && <Notice tone="warn">节点不可达，显示的是缓存地址。</Notice>}
      <div className="split" style={{ gridTemplateColumns: '1fr 1fr' }}>
        <FamilyCard id={id} family="ipv4" value={v.ipv4} etag={q.data?.etag} onSaved={() => void q.refetch()} />
        <FamilyCard id={id} family="ipv6" value={v.ipv6} etag={q.data?.etag} onSaved={() => void q.refetch()} />
      </div>
      <div className="row"><Button icon={<RefreshCw />} busy={refresh.busy} onClick={() => void refresh.run()}>重新发现</Button><span className="muted" style={{ fontSize: 12.5 }}>自动模式每 5 分钟发现一次，网络变化时立即重新发现。</span></div>
    </div>
  );
}

function FamilyCard({ id, family, value, etag, onSaved }: { id: string; family: 'ipv4' | 'ipv6'; value: PublicAddress; etag?: string; onSaved: () => void }) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState('');
  const save = useCommand<{ mode: 'auto' | 'manual'; addresses?: string[] }, PublicAddresses>({
    label: '保存公网地址',
    node: id,
    run: async (patch) => (await call<PublicAddresses>('PATCH', `/nodes/${enc(id)}/public-addresses`, { ifMatch: etag, body: { [family]: patch } })).data,
    onSuccess: () => { setEditing(false); onSaved(); },
    onConflict: onSaved,
  });
  const tone = value.state === 'confirmed' ? 'ok' : value.state === 'failed' || value.state === 'conflict' ? 'bad' : value.state === 'unavailable' ? 'idle' : 'info';
  const label = { pending: '发现中', confirmed: '已确认', failed: '发现失败', conflict: '来源冲突', unavailable: '无此地址族' }[value.state];
  return (
    <Card>
      <CardHead title={family === 'ipv4' ? 'IPv4' : 'IPv6'} sub={value.mode === 'auto' ? '自动发现' : '手工指定'} icon={<Globe width={18} height={18} style={{ color: 'var(--text-3)' }} />} actions={<Badge tone={tone} dot>{label}</Badge>} />
      <div className="card-body stack">
        {value.addresses.length ? <div className="row wrap">{value.addresses.map((a) => <CopyText key={a} value={a} />)}</div> : <span className="muted">没有地址</span>}
        {value.error && <span className="field-error">{value.error}</span>}
        {!!value.observations.length && (
          <div className="col" style={{ gap: 4 }}>
            {value.observations.map((o, i) => (
              <div key={i} className="row" style={{ fontSize: 12.5 }}>
                <Dot tone={o.error ? 'bad' : 'ok'} /><span className="muted" style={{ width: 70 }}>{o.source}</span>
                <span className="mono grow ellipsis">{o.address ?? o.error?.message ?? '—'}</span>
                <span className="dim">{ago(o.observed_at)}</span>
              </div>
            ))}
          </div>
        )}
        <AnimatePresence initial={false}>
          {editing ? (
            <motion.div key="edit" className="stack" initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: 'auto' }} exit={{ opacity: 0, height: 0 }} style={{ overflow: 'hidden' }}>
              <Field label="手工地址" hint="每行一个"><textarea className="textarea mono" rows={3} value={text} onChange={(e) => setText(e.target.value)} /></Field>
              <div className="row">
                <Button size="sm" variant="primary" busy={save.busy} onClick={() => void save.run({ mode: 'manual', addresses: text.split(/\s+/).filter(Boolean) })}>保存为手工</Button>
                <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>取消</Button>
              </div>
            </motion.div>
          ) : (
            <motion.div key="actions" className="row" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
              <Button size="sm" onClick={() => { setText(value.addresses.join('\n')); setEditing(true); }}>手工指定</Button>
              {value.mode === 'manual' && <Button size="sm" variant="ghost" busy={save.busy} onClick={() => void save.run({ mode: 'auto' })}>改为自动</Button>}
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </Card>
  );
}

function AccountTab({ id }: { id: string }) {
  const q = useNodeAccount(id);
  const [revealed, setRevealed] = useState<NodePassword | null>(null);
  const [draft, setDraft] = useState<{ username?: string; entrance?: string; password?: string }>({});
  useEffect(() => setDraft({}), [q.data?.etag]);
  const v = q.data?.value;
  const reveal = useCommand<void, NodePassword>({
    label: '查看节点密码',
    node: id,
    run: async () => (await call<NodePassword>('POST', `/nodes/${enc(id)}/account/actions`, { body: { action: 'reveal_password', expected_revision: v!.revision } })).data,
    onSuccess: (r) => setRevealed(r),
    success: false,
  });
  const save = useCommand<void, Operation>({
    label: '修改节点账号',
    node: id,
    run: async (_v, key) => (await call<Operation>('PATCH', `/nodes/${enc(id)}/account`, { key, ifMatch: q.data?.etag, body: draft })).data,
    onSuccess: () => setDraft({}),
    onConflict: () => void q.refetch(),
  });
  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  if (!v) return <Card pad><Skeleton height={120} /></Card>;
  const dirty = Object.values(draft).some((x) => x !== undefined && x !== '');
  return (
    <div className="stack">
      <Card>
        <CardHead title="节点管理员" sub="每个节点拥有独立账号与浏览器会话" icon={<KeyRound width={18} height={18} style={{ color: 'var(--text-3)' }} />} actions={<Button size="sm" icon={<Eye />} busy={reveal.busy} onClick={() => void reveal.run()}>查看密码</Button>} />
        <div className="card-body form-grid">
          <Field label="账号"><Input value={draft.username ?? v.username} onChange={(e) => setDraft((d) => ({ ...d, username: e.target.value }))} /></Field>
          <Field label="安全入口" hint="登录页路径，例如 /s_xxxx"><Input mono value={draft.entrance ?? v.entrance} onChange={(e) => setDraft((d) => ({ ...d, entrance: e.target.value }))} /></Field>
          <Field label="新密码" hint="留空表示不修改" className="span-2"><Input type="password" autoComplete="new-password" value={draft.password ?? ''} onChange={(e) => setDraft((d) => ({ ...d, password: e.target.value || undefined }))} /></Field>
        </div>
        {v.cached && <div className="card-body" style={{ paddingTop: 0 }}><Notice tone="warn">节点不可达，显示的是缓存值。</Notice></div>}
      </Card>
      <SaveBar dirty={dirty} busy={save.busy} onReset={() => setDraft({})} onSave={() => void save.run()} />
      <Dialog open={!!revealed} onClose={() => setRevealed(null)} title="节点登录信息" sub="请妥善保管，关闭后不再显示" width={460} footer={<Button onClick={() => setRevealed(null)}>关闭</Button>}>
        {revealed && (
          <dl className="kv">
            <dt>账号</dt><dd><CopyText value={revealed.username} /></dd>
            <dt>密码</dt><dd><CopyText value={revealed.password} /></dd>
            <dt>安全入口</dt><dd><CopyText value={revealed.entrance} /></dd>
          </dl>
        )}
      </Dialog>
    </div>
  );
}

