import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router';
import { AnimatePresence, motion, Reorder, useDragControls } from 'motion/react';
import { GripVertical, Ellipsis, Pencil, Plus, ShieldCheck, ShieldOff, Trash2, Download, Users, ScanSearch, RefreshCw } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { call, enc, get } from '../api/http';
import type { FirewallAdoption, ManagedFirewallChange, ManagedFirewallState, ManagedPolicy, ManagedRule, Operation, PeerTrustPolicy, RuleSpec } from '../api/types';
import { useFirewallStatus, useNodes, usePeerTrust, usePeerTrustStatus } from '../app/queries';
import { useCommand } from '../app/commands';
import { confirm } from '../app/confirm';
import { useSession } from '../app/session';
import { invalidateResource } from '../app/query';
import { ago } from '../lib/format';
import { backendLabel } from '../lib/status';
import { Badge, Button, Card, CardHead, Dialog, Dot, Empty, ErrorNotice, Field, IconButton, Input, Menu, Notice, PageHead, Segmented, Select, Skeleton, Spinner, Switch } from '../ui/kit';

const stateLabel: Record<ManagedFirewallState['state'], [string, 'ok' | 'warn' | 'bad' | 'info' | 'idle']> = {
  importing: ['导入中', 'info'], unavailable: ['不可用', 'idle'], pending: ['待应用', 'info'], applying: ['应用中', 'info'],
  awaiting_confirmation: ['待确认', 'warn'], in_sync: ['已同步', 'ok'], failed: ['应用失败', 'bad'], unknown: ['未知', 'idle'],
};

export function Firewall() {
  const [params, setParams] = useSearchParams();
  const nodes = useNodes();
  const session = useSession();
  const list = (nodes.data?.items ?? []).filter((n) => !n.revoked_at);
  const node = params.get('node') || session.local_node_id;
  const tab = params.get('tab') === 'trust' ? 'trust' : 'rules';
  const set = (k: string, v: string) => { const next = new URLSearchParams(params); next.set(k, v); setParams(next, { replace: true }); };
  return (
    <div className="page">
      <PageHead title="防火墙" sub="每个节点保存统一的入站配置；外部修改会被恢复到已确认版本，运行规则变更需在期限内确认。" actions={
        <Segmented value={tab} onChange={(v) => set('tab', v)} options={[{ value: 'rules', label: '规则', icon: <ShieldCheck width={15} height={15} /> }, { value: 'trust', label: '集群互信', icon: <Users width={15} height={15} /> }]} />
      } />
      <AnimatePresence mode="wait">
        <motion.div key={tab} className="stack" initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }} transition={{ duration: 0.2 }}>
          {tab === 'rules' ? (
            <>
              <div className="toolbar">
                <Select value={node} onChange={(e) => set('node', e.target.value)} style={{ width: 260 }} aria-label="节点">
                  {list.map((n) => <option key={n.id} value={n.id}>{n.name}{n.connected ? '' : '（离线）'}</option>)}
                </Select>
              </div>
              <NodeFirewall key={node} nodeId={node} />
            </>
          ) : <PeerTrust />}
        </motion.div>
      </AnimatePresence>
    </div>
  );
}

function NodeFirewall({ nodeId }: { nodeId: string }) {
  const q = useFirewallStatus(nodeId);
  const s = q.data;
  const [editing, setEditing] = useState<ManagedRule | 'new' | null>(null);
  const [adopting, setAdopting] = useState(false);
  const change = useCommand<{ change: ManagedFirewallChange; label: string }, Operation>({
    label: (v) => v.label,
    node: nodeId,
    run: async (v, key) => (await call<Operation>('POST', `/nodes/${enc(nodeId)}/firewall/changes`, { key, body: { expected_revision: s!.revision, change: v.change } })).data,
    onSuccess: () => { setEditing(null); invalidateResource('firewall'); },
    onConflict: () => void q.refetch(),
  });
  const install = useCommand<'ufw' | 'firewalld' | 'nftables', Operation>({
    label: '安装防火墙组件',
    node: nodeId,
    run: async (backend, key) => (await call<Operation>('POST', `/nodes/${enc(nodeId)}/firewall/actions`, { key, body: { action: 'install', backend, expected_revision: s?.revision ?? 0 } })).data,
  });

  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  if (!s) return <div className="stack"><div className="fw-status">{Array.from({ length: 4 }, (_, i) => <Card key={i} className="fw-stat"><Skeleton width="40%" /><Skeleton width="70%" height={20} /></Card>)}</div><Card pad><Skeleton height={200} /></Card></div>;
  const [label, tone] = stateLabel[s.state];
  const caps = s.capabilities;
  const canEdit = caps.edit_rules.supported;
  return (
    <div className="stack">
      <div className="fw-status">
        <Card className="fw-stat"><span className="label">后端</span><span className="value">{backendLabel[s.backend] ?? s.backend}{s.service_active !== undefined && <Badge tone={s.service_active ? 'ok' : 'warn'}>{s.service_active ? '运行中' : '未启用'}</Badge>}</span></Card>
        <Card className="fw-stat"><span className="label">应用状态</span><span className="value"><Dot tone={tone} live={s.state === 'applying' || s.state === 'awaiting_confirmation'} />{label}</span></Card>
        <Card className="fw-stat"><span className="label">配置版本</span><span className="value num">{s.revision}{s.confirmed_revision !== s.revision && <span className="muted" style={{ fontSize: 12.5, fontWeight: 500 }}>已确认 {s.confirmed_revision}</span>}</span></Card>
        <Card className="fw-stat"><span className="label">最近检查</span><span className="value" style={{ fontSize: 14 }}>{s.checked_at ? ago(s.checked_at) : '—'}<IconButton size="sm" label="刷新" onClick={() => void q.refetch()}>{q.isFetching ? <Spinner size={14} /> : <RefreshCw />}</IconButton></span></Card>
      </div>
      {s.error && <Notice tone="bad" title="应用失败">{s.error.message}</Notice>}
      {s.migration && s.migration.state !== 'in_sync' && <Notice tone={s.migration.state === 'failed' ? 'bad' : 'info'} title="互信规则转交">{s.migration.state === 'failed' ? s.migration.error?.message ?? '转交失败' : '正在把与成员地址相同的放行规则转交给集群互信。'}</Notice>}
      {(s.backend === 'none' || !caps.read.supported) && (
        <Card>
          <CardHead title="未检测到可管理的防火墙" sub={caps.read.reason ?? '安装组件后才能管理入站规则；组件安装后不会自动启用。'} />
          {caps.install.supported && <div className="card-body row wrap">
            {(['ufw', 'firewalld', 'nftables'] as const).map((b) => <Button key={b} icon={<Download />} busy={install.busy} onClick={async () => { if (await confirm({ title: `安装 ${backendLabel[b]}？`, body: '通过本机软件包管理器安装；安装完成后保持停用，由你决定何时启用。', confirmLabel: '安装' })) void install.run(b); }}>{backendLabel[b]}</Button>)}
          </div>}
        </Card>
      )}
      {caps.read.supported && (
        <>
          <Policies s={s} disabled={!caps.default_policy.supported || change.busy} onChange={(p, action) => void change.run({ label: '修改默认策略', change: { kind: 'set_default_policy', action, chain: p.chain, zone: p.zone } })} />
          <Card>
            <CardHead title="入站规则" sub={canEdit ? (caps.reorder.supported ? '拖动左侧手柄调整顺序；规则自上而下匹配' : '规则自上而下匹配') : caps.edit_rules.reason} actions={
              <>
                <Button size="sm" variant="ghost" icon={<ScanSearch />} onClick={() => setAdopting(true)}>接纳现场配置</Button>
                <Button size="sm" variant="primary" icon={<Plus />} disabled={!canEdit} onClick={() => setEditing('new')}>添加规则</Button>
              </>
            } />
            <RuleList rules={s.rules} canEdit={canEdit} canReorder={caps.reorder.supported && canEdit} busy={change.busy}
              onEdit={(r) => setEditing(r)}
              onDelete={async (r) => { if (await confirm({ title: '删除这条规则？', body: describe(r.spec), danger: true, confirmLabel: '删除' })) void change.run({ label: '删除规则', change: { kind: 'delete_rule', rule_id: r.id, chain: r.chain, zone: r.zone } }); }}
              onMove={(r, before) => void change.run({ label: '调整规则顺序', change: { kind: 'move_rule', rule_id: r.id, before_rule_id: before, chain: r.chain, zone: r.zone } })}
            />
          </Card>
        </>
      )}
      <RuleDialog open={editing !== null} rule={editing === 'new' ? undefined : editing ?? undefined} busy={change.busy} onClose={() => setEditing(null)} onSubmit={(spec) => {
        if (editing === 'new') void change.run({ label: '添加规则', change: { kind: 'add_rule', rule: spec } });
        else if (editing) void change.run({ label: '修改规则', change: { kind: 'edit_rule', rule_id: editing.id, rule: spec, chain: editing.chain, zone: editing.zone } });
      }} />
      <AdoptDialog open={adopting} onClose={() => setAdopting(false)} nodeId={nodeId} />
    </div>
  );
}

function Policies({ s, disabled, onChange }: { s: ManagedFirewallState; disabled: boolean; onChange: (p: ManagedPolicy, action: ManagedPolicy['action']) => void }) {
  if (!s.default_policies.length) return null;
  return (
    <Card>
      <CardHead title="默认入站策略" sub="未匹配任何规则的入站流量" />
      <div className="list">
        {s.default_policies.map((p, i) => (
          <div key={i} className="list-item">
            <div className="li-main"><span className="li-title">{p.family === 'ipv4' ? 'IPv4' : 'IPv6'}{p.zone ? ` · ${p.zone}` : ''}{p.chain ? ` · ${p.chain}` : ''}</span></div>
            <Segmented size="sm" value={p.action} onChange={async (a) => { if (a === p.action || disabled) return; if (await confirm({ title: `默认策略改为“${actionText[a]}”？`, body: a === 'allow' ? '所有未匹配的入站连接都会被放行。' : '请先确认已有放行规则覆盖当前管理连接，否则可能失去访问。修改后需在期限内确认。', danger: a !== 'allow', confirmLabel: '修改' })) onChange(p, a); }}
              options={(['allow', 'deny', 'reject'] as const).map((a) => ({ value: a, label: actionText[a] }))} />
          </div>
        ))}
      </div>
    </Card>
  );
}

const actionText: Record<string, string> = { allow: '放行', deny: '丢弃', reject: '拒绝', default: '默认' };
const describe = (r: RuleSpec) => `${actionText[r.action]} ${r.family.toUpperCase()} ${r.source || '任意来源'} → ${r.protocol === 'any' ? '任意协议' : r.protocol.toUpperCase()}${r.port ? `:${r.port}` : ''}`;

function RuleList({ rules, canEdit, canReorder, busy, onEdit, onDelete, onMove }: { rules: ManagedRule[]; canEdit: boolean; canReorder: boolean; busy: boolean; onEdit: (r: ManagedRule) => void; onDelete: (r: ManagedRule) => void; onMove: (r: ManagedRule, before?: string) => void }) {
  const [order, setOrder] = useState(rules.map((r) => r.id));
  const [dragging, setDragging] = useState<string | null>(null);
  const key = rules.map((r) => r.id).join('|');
  useEffect(() => { if (!dragging) setOrder(rules.map((r) => r.id)); }, [key]); // eslint-disable-line react-hooks/exhaustive-deps
  const byId = useMemo(() => new Map(rules.map((r) => [r.id, r])), [rules]);
  if (!rules.length) return <Empty icon={<ShieldOff />} title="还没有入站规则">没有规则时，入站流量按默认策略处理。</Empty>;
  const finish = (id: string) => {
    setDragging(null);
    const from = rules.findIndex((r) => r.id === id);
    const to = order.indexOf(id);
    if (from === to || to < 0) return;
    const rule = byId.get(id)!;
    onMove(rule, order[to + 1]);
  };
  return (
    <div className="rule-list">
      <div className="rule head"><span /><span>动作</span><span>来源</span><span className="hide-sm">协议</span><span className="hide-sm">端口</span><span className="hide-sm">备注</span><span /></div>
      <Reorder.Group as="div" axis="y" values={order} onReorder={setOrder} style={{ display: 'flex', flexDirection: 'column' }}>
        {order.map((id) => { const r = byId.get(id); return r ? <RuleRow key={id} rule={r} canEdit={canEdit} canReorder={canReorder && !busy} dragging={dragging === id} onDragStart={() => setDragging(id)} onDragEnd={() => finish(id)} onEdit={() => onEdit(r)} onDelete={() => onDelete(r)} /> : null; })}
      </Reorder.Group>
    </div>
  );
}

function RuleRow({ rule, canEdit, canReorder, dragging, onDragStart, onDragEnd, onEdit, onDelete }: { rule: ManagedRule; canEdit: boolean; canReorder: boolean; dragging: boolean; onDragStart: () => void; onDragEnd: () => void; onEdit: () => void; onDelete: () => void }) {
  const controls = useDragControls();
  const r = rule.spec;
  return (
    <Reorder.Item as="div" value={rule.id} dragListener={false} dragControls={controls} onDragStart={onDragStart} onDragEnd={onDragEnd} className="rule" data-dragging={dragging} whileDrag={{ scale: 1.01 }} transition={{ type: 'spring', stiffness: 600, damping: 40 }}>
      <span className="grip" onPointerDown={(e) => { if (canReorder) controls.start(e); }} style={{ opacity: canReorder ? 1 : 0.3 }} aria-label="拖动排序"><GripVertical /></span>
      <span><span className={`action-chip ${r.action}`}>{actionText[r.action]}</span></span>
      <span className="col" style={{ gap: 0, minWidth: 0 }}>
        <span className="mono ellipsis" style={{ fontSize: 13 }}>{r.source || '任意'}</span>
        <span className="dim" style={{ fontSize: 11.5 }}>{r.family.toUpperCase()}{r.destination ? ` → ${r.destination}` : ''}{rule.zone ? ` · ${rule.zone}` : ''}</span>
      </span>
      <span className="hide-sm mono" style={{ fontSize: 12.5 }}>{r.protocol === 'any' ? '任意' : r.protocol.toUpperCase()}</span>
      <span className="hide-sm mono" style={{ fontSize: 12.5 }}>{r.port || '全部'}</span>
      <span className="hide-sm muted ellipsis" style={{ fontSize: 12.5 }}>{r.comment || ''}</span>
      <span>
        <Menu trigger={(p) => <IconButton size="sm" label="更多" {...p}><Ellipsis /></IconButton>} items={[
          { label: '编辑', icon: <Pencil />, onSelect: onEdit, disabled: !canEdit },
          { label: '删除', icon: <Trash2 />, onSelect: onDelete, danger: true, disabled: !canEdit },
        ]} />
      </span>
    </Reorder.Item>
  );
}

const emptySpec: RuleSpec = { family: 'ipv4', source: '', protocol: 'tcp', port: '', action: 'allow', comment: '' };

function RuleDialog({ open, rule, busy, onClose, onSubmit }: { open: boolean; rule?: ManagedRule; busy: boolean; onClose: () => void; onSubmit: (spec: RuleSpec) => void }) {
  const [spec, setSpec] = useState<RuleSpec>(emptySpec);
  useEffect(() => { if (open) setSpec(rule ? { ...emptySpec, ...rule.spec } : emptySpec); }, [open, rule]);
  const set = <K extends keyof RuleSpec>(k: K, v: RuleSpec[K]) => setSpec((s) => ({ ...s, [k]: v }));
  const portless = spec.protocol === 'any' || spec.protocol === 'icmp' || spec.protocol === 'icmpv6';
  const submit = () => {
    const clean: RuleSpec = { family: spec.family, source: spec.source.trim(), protocol: spec.protocol, action: spec.action };
    if (spec.destination?.trim()) clean.destination = spec.destination.trim();
    if (!portless && spec.port?.trim()) clean.port = spec.port.trim();
    if (spec.comment?.trim()) clean.comment = spec.comment.trim();
    onSubmit(clean);
  };
  return (
    <Dialog open={open} onClose={onClose} width={600} title={rule ? '编辑规则' : '添加入站规则'} sub="保存后进入目标节点队列；涉及运行规则时需在期限内确认。"
      footer={<><Button variant="ghost" onClick={onClose}>取消</Button><Button variant="primary" busy={busy} onClick={submit}>{rule ? '保存' : '添加'}</Button></>}>
      <Field label="动作">
        <Segmented value={spec.action} onChange={(v) => set('action', v)} options={[{ value: 'allow', label: '放行' }, { value: 'deny', label: '丢弃' }, { value: 'reject', label: '拒绝' }]} />
      </Field>
      <div className="form-grid">
        <Field label="地址族"><Segmented value={spec.family} onChange={(v) => set('family', v)} options={[{ value: 'ipv4', label: 'IPv4' }, { value: 'ipv6', label: 'IPv6' }]} /></Field>
        <Field label="协议">
          <Select value={spec.protocol} onChange={(e) => set('protocol', e.target.value as RuleSpec['protocol'])}>
            <option value="tcp">TCP</option><option value="udp">UDP</option><option value="any">任意</option><option value="icmp">ICMP</option><option value="icmpv6">ICMPv6</option>
          </Select>
        </Field>
        <Field label="来源" hint="单个地址或 CIDR，留空表示任意来源"><Input mono value={spec.source} onChange={(e) => set('source', e.target.value)} placeholder={spec.family === 'ipv4' ? '203.0.113.0/24' : '2001:db8::/32'} /></Field>
        <Field label="端口" hint={portless ? '当前协议不使用端口' : '单个端口、范围 8000:8100 或列表 80,443'}><Input mono value={portless ? '' : spec.port ?? ''} disabled={portless} onChange={(e) => set('port', e.target.value)} placeholder="443" /></Field>
        <Field label="目标地址" hint="可选，限定本机目标地址"><Input mono value={spec.destination ?? ''} onChange={(e) => set('destination', e.target.value)} /></Field>
        <Field label="备注"><Input value={spec.comment ?? ''} onChange={(e) => set('comment', e.target.value)} maxLength={120} /></Field>
      </div>
      <Notice tone="info">{describe({ ...spec, port: portless ? undefined : spec.port })}</Notice>
    </Dialog>
  );
}

function AdoptDialog({ open, onClose, nodeId }: { open: boolean; onClose: () => void; nodeId: string }) {
  const preview = useQuery({ queryKey: ['firewall', nodeId, 'adoption'], queryFn: ({ signal }) => get<FirewallAdoption>(`/nodes/${enc(nodeId)}/firewall/adoption`, { signal }), enabled: open, staleTime: 0, gcTime: 0 });
  const adopt = useCommand<void, Operation>({
    label: '接纳当前防火墙配置',
    node: nodeId,
    run: async (_v, key) => (await call<Operation>('POST', `/nodes/${enc(nodeId)}/firewall/actions`, { key, body: { action: 'adopt_current', expected_revision: preview.data!.expected_revision, fingerprint: preview.data!.fingerprint } })).data,
    onSuccess: onClose,
    onConflict: () => void preview.refetch(),
  });
  const d = preview.data;
  return (
    <Dialog open={open} onClose={onClose} width={720} title="接纳现场配置" sub="读取节点当前的原生防火墙配置，将其保存为 OPSD 的已确认版本；现场规则保持不变。"
      footer={<><Button variant="ghost" onClick={onClose}>取消</Button><Button variant="primary" busy={adopt.busy} disabled={!d} onClick={() => void adopt.run()}>接纳此配置</Button></>}>
      {preview.isError && <ErrorNotice error={preview.error} retry={() => void preview.refetch()} />}
      {!d ? <Skeleton height={160} /> : (
        <>
          <div className="row muted" style={{ fontSize: 12.5 }}><span>{backendLabel[d.configuration.backend]}</span><span>·</span><span>{d.configuration.rules.length} 条规则</span><span>·</span><span>读取于 {ago(d.checked_at)}</span></div>
          <Card>
            <div className="rule-list">
              {d.configuration.rules.map((r) => (
                <div key={r.id} className="rule" style={{ gridTemplateColumns: '64px 1.4fr 0.8fr 0.8fr 1fr' }}>
                  <span><span className={`action-chip ${r.spec.action}`}>{actionText[r.spec.action]}</span></span>
                  <span className="mono" style={{ fontSize: 13 }}>{r.spec.source || '任意'}</span>
                  <span className="mono" style={{ fontSize: 12.5 }}>{r.spec.protocol}</span>
                  <span className="mono" style={{ fontSize: 12.5 }}>{r.spec.port || '全部'}</span>
                  <span className="muted ellipsis" style={{ fontSize: 12.5 }}>{r.spec.comment}</span>
                </div>
              ))}
              {!d.configuration.rules.length && <div className="empty" style={{ padding: 24 }}><p>现场没有入站规则</p></div>}
            </div>
          </Card>
        </>
      )}
    </Dialog>
  );
}

// ── 集群互信 ─────────────────────────────────────────
function PeerTrust() {
  const policy = usePeerTrust();
  const status = usePeerTrustStatus();
  const nodes = useNodes();
  const session = useSession();
  const names = new Map((nodes.data?.items ?? []).map((n) => [n.id, n.name]));
  const toggle = useCommand<boolean, PeerTrustPolicy>({
    label: (v) => (v ? '开启集群互信' : '关闭集群互信'),
    node: session.local_node_id,
    run: async (enabled) => (await call<PeerTrustPolicy>('PATCH', '/peer-trust', { ifMatch: policy.data?.etag, body: { enabled } })).data,
    onSuccess: () => { void policy.refetch(); void status.refetch(); },
    onConflict: () => void policy.refetch(),
  });
  const enabled = policy.data?.value.enabled ?? false;
  return (
    <div className="stack">
      {policy.isError && <ErrorNotice error={policy.error} retry={() => void policy.refetch()} />}
      <Card>
        <div className="card-body row-3">
          <span className="avatar" style={{ width: 44, height: 44, borderRadius: 13 }}><Users /></span>
          <div className="col grow" style={{ gap: 2 }}>
            <h2>集群互信</h2>
            <span className="muted" style={{ fontSize: 13 }}>开启后，所有有效成员的公网地址在各节点上直接放行全部主机入站端口（包括数据库端口）。</span>
          </div>
          <Switch checked={enabled} disabled={!policy.data || toggle.busy} label="集群互信" onChange={async (v) => {
            if (await confirm({ title: v ? '开启集群互信？' : '关闭集群互信？', body: v ? '成员公网 IP（含共享该 IP 的其他来源）将优先放行全部主机入站端口。' : '各节点将撤除 OPSD 互信规则，成员间访问改由普通规则控制。', danger: !v, confirmLabel: v ? '开启' : '关闭' })) void toggle.run(v);
          }} />
        </div>
      </Card>
      <Card>
        <CardHead title="逐节点状态" sub={status.data ? `策略版本 ${status.data.policy.version.counter}` : undefined} />
        {status.isError ? <div className="card-body"><ErrorNotice error={status.error} retry={() => void status.refetch()} /></div> : !status.data ? <div className="card-body"><Skeleton height={120} /></div> : (
          <div>
            {status.data.nodes.map((n) => {
              const tone = n.state === 'in_sync' ? 'ok' : n.state === 'failed' ? 'bad' : n.state === 'offline' || n.state === 'disabled' ? 'idle' : 'info';
              const fam = (f: 'ipv4' | 'ipv6') => n.rules.filter((r) => r.family === f);
              return (
                <div key={n.node_id} className="trust-row">
                  <div className="row"><Dot tone={tone} live={n.state === 'pending'} /><strong className="ellipsis" style={{ fontWeight: 580 }}>{names.get(n.node_id) ?? n.node_id.slice(0, 12)}</strong></div>
                  {(['ipv4', 'ipv6'] as const).map((f) => (
                    <div key={f} className="col" style={{ gap: 2, minWidth: 0 }}>
                      <span className="dim" style={{ fontSize: 11.5 }}>{f.toUpperCase()} · {fam(f).length} 个来源</span>
                      <span className="mono ellipsis" style={{ fontSize: 12 }} title={fam(f).map((r) => r.address).join(', ')}>{fam(f).slice(0, 3).map((r) => r.address).filter(Boolean).join(', ') || '—'}</span>
                    </div>
                  ))}
                  <Badge tone={tone}>{{ pending: '同步中', in_sync: '已生效', failed: '失败', disabled: '未开启', offline: '离线' }[n.state]}</Badge>
                </div>
              );
            })}
            {!status.data.nodes.length && <Empty title="没有节点状态" />}
          </div>
        )}
      </Card>
    </div>
  );
}
