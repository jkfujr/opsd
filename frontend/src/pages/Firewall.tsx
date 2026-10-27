import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useBeforeUnload, useBlocker, useSearchParams } from 'react-router';
import { Button } from '@appica/ui-react/button';
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle, DrawerBody, DrawerFooter } from '@appica/ui-react/drawer';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { api, RequestError, unwrap } from '../api/client';
import type { components } from '../api/schema.gen';
import { useOperations, invalidateOperations } from '../operations';
import { ChoiceField, Confirm, date, Failure, PageTitle, TextField } from '../ui';
import { nodesQuery } from './Nodes';

type Rule = components['schemas']['FirewallRule'];
type State = components['schemas']['FirewallState'];
type Change = components['schemas']['FirewallChange'];
type Plan = components['schemas']['ChangePlan'];
type Family = components['schemas']['AddressFamily'];
type Scope = components['schemas']['ConfigScope'];
type Backend = components['schemas']['FirewallInstallRequest']['backend'];
const scopeNames = { runtime: '运行配置', permanent: '永久配置', both: '运行与永久' };
const actionNames = { allow: '允许', deny: '丢弃', reject: '拒绝', default: '原生默认' };
const kindNames = { add_rule: '添加规则', edit_rule: '编辑规则', delete_rule: '删除规则', move_rule: '调整顺序', set_default_policy: '默认入站策略', set_enabled: '启停防火墙' };
type EditKind = keyof typeof kindNames;
function ruleText(rule: Rule) { const r = rule.spec; return r ? `${r.family === 'ipv4' ? 'IPv4' : 'IPv6'} ${r.source || '任意来源'} → ${r.destination || '任意目标'} ${r.protocol.toUpperCase()}${r.port ? `/${r.port}` : ''} ${actionNames[r.action]}` : rule.native_expression; }
function newKey() { return Array.from(crypto.getRandomValues(new Uint8Array(16)), (v) => v.toString(16).padStart(2, '0')).join(''); }

export function Firewall() {
  const [search, setSearch] = useSearchParams();
  const nodes = useQuery(nodesQuery);
  const nodeID = search.get('node') ?? '';
  const family = (search.get('family') ?? '') as Family | '';
  const scope = (search.get('scope') ?? 'both') as Scope;
  const node = nodes.data?.items.find((n) => n.id === nodeID);
  const [editor, setEditor] = useState<{ kind: EditKind; rule?: Rule } | null>(null);
  const [dirty, setDirty] = useState(false);
  const [discard, setDiscard] = useState(false);
  const blocker = useBlocker(({ currentLocation, nextLocation }) => dirty && (currentLocation.pathname !== nextLocation.pathname || currentLocation.search !== nextLocation.search));
  useBeforeUnload((event) => { if (dirty) { event.preventDefault(); event.returnValue = ''; } });
  useEffect(() => {
    if (!nodeID && nodes.data) { const local = nodes.data.items.find((n) => n.local && !n.revoked_at); if (local) setSearch((current) => { const next = new URLSearchParams(current); next.set('node', local.id); return next; }, { replace: true }); }
  }, [nodeID, nodes.data, setSearch]);
  const update = (key: string, value: string) => setSearch((current) => { const next = new URLSearchParams(current); if (value) next.set(key, value); else next.delete(key); return next; });
  const state = useQuery({ queryKey: ['firewall', nodeID, family, scope], enabled: Boolean(node?.connected && !node.revoked_at), retry: false, queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{id}/firewall', { signal, params: { path: { id: nodeID }, query: { family: family || undefined, scope } } })) });
  const stale = !node?.connected || Boolean(node.revoked_at) || state.isError;
  const read = state.data;
  const close = () => { setEditor(null); setDirty(false); setDiscard(false); };
  const open = (kind: EditKind, rule?: Rule) => { setEditor({ kind, rule }); setDirty(false); };
  const disabled = stale || !read?.complete || !node?.writable;
  return <>
    <PageTitle action={<Button variant="outline" disabled={!node?.connected || state.isFetching} onClick={() => void state.refetch()}>刷新</Button>}>防火墙</PageTitle>
    <div className="mb-7 grid grid-cols-1 gap-4 sm:grid-cols-3">
      <ChoiceField label="节点" value={nodeID} options={nodes.data?.items.filter((n) => !n.revoked_at).map((n) => [n.id, n.name] as const) ?? []} onChange={(value) => update('node', value)} />
      <ChoiceField label="地址族" value={family} options={[[ '', 'IPv4 与 IPv6' ], ['ipv4', 'IPv4'], ['ipv6', 'IPv6']]} onChange={(value) => update('family', value)} />
      <ChoiceField label="配置" value={scope} options={[[ 'both', '运行与永久' ], ['runtime', '运行配置'], ['permanent', '永久配置']]} onChange={(value) => update('scope', value)} />
    </div>
    <Failure error={nodes.error ?? state.error} />
    {nodeID && !node && nodes.data && <p role="alert">节点不存在</p>}
    {node?.revoked_at && <p role="alert">节点已撤销</p>}
    {node && !node.connected && <p role="status" className="mb-4 text-sm text-foreground-muted">节点离线{read ? `，显示最近读数：${date(read.collected_at)}` : ''}</p>}
    {node?.write_blocked_reason && <p role="status" className="mb-4 text-sm text-foreground-muted">{node.write_blocked_reason}</p>}
    {state.isPending && node?.connected && <p className="text-foreground-muted">读取中…</p>}
    {read && <>
      <div className="mb-5 flex flex-wrap items-center justify-between gap-4 border-y border-border py-4">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2"><span className="font-medium text-foreground-intense">{read.backend === 'none' ? '未安装防火墙' : read.backend}</span>{read.service_active !== undefined && <span className="text-sm">{read.service_active ? '已启用' : '已停用'}</span>}<span className="text-xs text-foreground-muted">{stale ? '最近读数' : '采集于'} {date(read.collected_at)}</span></div>
        <div className="flex flex-wrap gap-2">{read.capabilities.edit_rules.supported && <Button disabled={disabled} onClick={() => open('add_rule')}>添加规则</Button>}{read.capabilities.default_policy.supported && <Button variant="outline" disabled={disabled} onClick={() => open('set_default_policy')}>默认策略</Button>}{read.capabilities.enable.supported && <Button variant="outline" disabled={disabled} onClick={() => open('set_enabled')}>{read.service_active ? '停用' : '启用'}</Button>}</div>
      </div>
      {!read.complete && <p role="alert" className="mb-4 text-sm">读取不完整，编辑已停用。</p>}
      {read.backend !== 'none' && read.complete && !read.capabilities.edit_rules.supported && read.capabilities.edit_rules.reason && <p role="status" className="mb-4 text-sm text-foreground-muted">{read.capabilities.edit_rules.reason}</p>}
      {read.errors.map((error) => <p key={error.scope} role="alert" className="mb-2 text-sm">{error.scope}：{error.message}</p>)}
      <InstallFirewall key={nodeID} nodeID={nodeID} available={read.backend === 'none' && read.capabilities.install.supported} disabled={!node?.writable || stale} />
      {read.backend !== 'none' && <div className="max-w-full overflow-x-auto"><Table hoverableRows className="min-w-180">
        <TableHeader><TableRow><TableHead>规则</TableHead><TableHead>配置</TableHead><TableHead>原生位置</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
        <TableBody>{read.rules.map((rule) => <TableRow key={rule.id}><TableCell><div className="max-w-lg whitespace-normal break-words font-medium text-foreground-intense">{ruleText(rule)}</div>{rule.spec?.comment && <p className="mt-1 text-xs text-foreground-muted">{rule.spec.comment}</p>}{!rule.editable && <p className="mt-1 text-xs text-foreground-muted">{rule.readonly_reason || '只读'}</p>}</TableCell><TableCell className="whitespace-nowrap">{scopeNames[rule.scope]}</TableCell><TableCell className="max-w-60 break-words text-sm">{rule.native.zone || rule.native.chain}{rule.native.priority !== undefined && <span className="block text-xs text-foreground-muted">优先级 {rule.native.priority}</span>}</TableCell><TableCell><div className="flex justify-end gap-1"><Button variant="ghost" size="sm" disabled={disabled || !rule.editable} onClick={() => open('edit_rule', rule)}>编辑</Button><Button variant="ghost" size="sm" disabled={disabled || !rule.editable} onClick={() => open('delete_rule', rule)}>删除</Button>{read.capabilities.reorder.supported && <Button variant="ghost" size="sm" disabled={disabled || !rule.editable} onClick={() => open('move_rule', rule)}>排序</Button>}</div></TableCell></TableRow>)}
          {!read.rules.length && <TableRow><TableCell colSpan={4} className="py-10 text-center text-foreground-muted">当前范围没有规则</TableCell></TableRow>}
        </TableBody>
      </Table></div>}
    </>}
    <Drawer side="right" open={editor !== null} onOpenChange={(next) => { if (!next) { if (dirty) setDiscard(true); else close(); } }}><DrawerContent frame={false} closeLabel="关闭编辑" className="h-full w-full max-w-xl"><DrawerHeader><DrawerTitle>{editor ? kindNames[editor.kind] : '编辑规则'}</DrawerTitle></DrawerHeader>{editor && read && <RuleEditor key={`${nodeID}/${editor.kind}/${editor.rule?.id ?? ''}`} nodeID={nodeID} state={read} initialFamily={family || 'ipv4'} kind={editor.kind} rule={editor.rule} disabled={Boolean(disabled)} onDirty={() => setDirty(true)} onClose={close} onCancel={() => { if (dirty) setDiscard(true); else close(); }} />}</DrawerContent></Drawer>
    <Confirm open={discard || blocker.state === 'blocked'} title="丢弃未提交的修改？" description="已受理的操作仍会继续执行。" busy={false} error={null} confirmLabel="丢弃修改" onCancel={() => { setDiscard(false); if (blocker.state === 'blocked') blocker.reset(); }} onConfirm={() => { close(); if (blocker.state === 'blocked') blocker.proceed(); }} />
  </>;
}

function RuleEditor({ nodeID, state, initialFamily, kind, rule, disabled, onDirty, onClose, onCancel }: { nodeID: string; state: State; initialFamily: Family; kind: EditKind; rule?: Rule; disabled: boolean; onDirty: () => void; onClose: () => void; onCancel: () => void }) {
  const operations = useOperations();
  const scopes: readonly (readonly [Scope, string])[] = state.service_active ? state.backend === 'ufw' ? [['both', scopeNames.both]] : [['both', scopeNames.both], ['runtime', scopeNames.runtime], ['permanent', scopeNames.permanent]] : [['permanent', scopeNames.permanent]];
  const [scope, setScope] = useState<Scope>(scopes[0][0]);
  const [family, setFamily] = useState<Family>(rule?.spec?.family ?? initialFamily);
  const [source, setSource] = useState(rule?.spec?.source ?? '');
  const [destination, setDestination] = useState(rule?.spec?.destination ?? '');
  const [protocol, setProtocol] = useState(rule?.spec?.protocol ?? 'tcp');
  const [port, setPort] = useState(rule?.spec?.port ?? '');
  const [action, setAction] = useState<components['schemas']['RuleSpec']['action']>(rule?.spec?.action ?? 'allow');
  const [comment, setComment] = useState(rule?.spec?.comment ?? '');
  const zones = [...new Set(state.default_policies.map((p) => p.zone).filter((z): z is string => Boolean(z)))];
  const chains = [...new Set([...state.rules.map((r) => r.native.chain), ...state.default_policies.map((p) => p.chain)].filter((c): c is string => Boolean(c)))];
  const [zone, setZone] = useState(rule?.native.zone ?? zones[0] ?? '');
  const [chain, setChain] = useState(rule?.native.chain ?? chains[0] ?? '');
  const [priority, setPriority] = useState(String(rule?.native.priority ?? 0));
  const [before, setBefore] = useState('');
  const [policy, setPolicy] = useState('deny');
  const [reload, setReload] = useState(true);
  const [plan, setPlan] = useState<Plan | null>(null);
  const [uncertain, setUncertain] = useState(false);
  const changesRule = kind === 'add_rule' || kind === 'edit_rule';
  const changed = () => { onDirty(); setPlan(null); setUncertain(false); };
  const set = <T,>(write: (value: T) => void) => (value: T) => { write(value); changed(); };
  const build = (): Change => {
    const location = { zone: state.backend === 'firewalld' ? zone || undefined : undefined, chain: state.backend === 'nftables' ? chain || undefined : undefined };
    const nativePriority = state.backend === 'firewalld' ? { priority: Number(priority) } : {};
    if (kind === 'set_enabled') return { kind, enabled: !state.service_active };
    if (kind === 'set_default_policy') return { kind, scope, family: chain.startsWith('ip6/') ? 'ipv6' : chain.startsWith('ip/') ? 'ipv4' : family, action: policy as components['schemas']['SetDefaultPolicy']['action'], reload, ...location };
    if (kind === 'delete_rule') return { kind, scope, rule_id: rule!.id, ...location };
    if (kind === 'move_rule') return { kind, scope, rule_id: rule!.id, before_rule_id: before || undefined, ...location, ...nativePriority };
    const spec: components['schemas']['RuleSpec'] = { family, source: source.trim() || (family === 'ipv4' ? '0.0.0.0/0' : '::/0'), destination: destination.trim() || undefined, protocol, port: port.trim() || undefined, action, comment: comment.trim() || undefined };
    return kind === 'edit_rule' ? { kind, scope, rule_id: rule!.id, rule: spec, ...location, ...nativePriority } : { kind, scope, rule: spec, before_rule_id: before || undefined, ...location, ...nativePriority };
  };
  const preview = useMutation({ mutationFn: async () => unwrap(await api.POST('/nodes/{id}/firewall/plans', { params: { path: { id: nodeID }, header: { 'X-CSRF-Token': '' } }, body: build() })), onSuccess: (result) => { apply.reset(); locate.reset(); setPlan(result); setUncertain(false); } });
  const accepted = (id: string) => { invalidateOperations(); onClose(); operations.show(id); };
  const apply = useMutation({ mutationFn: async () => unwrap(await api.POST('/firewall/plans/{id}/apply', { params: { path: { id: plan!.id }, header: { 'X-CSRF-Token': '' } } })), onSuccess: (op) => accepted(op.id), onError: (error) => { if (error instanceof RequestError && [409, 422].includes(error.status)) { setPlan(null); setUncertain(false); } else setUncertain(true); invalidateOperations(); } });
  const locate = useMutation({ mutationFn: async () => unwrap(await api.GET('/firewall/plans/{id}', { params: { path: { id: plan!.id } } })), onSuccess: (result) => { if (result.operation_id) accepted(result.operation_id); else { setPlan(result); setUncertain(false); apply.reset(); } } });
  const inFlight = preview.isPending || apply.isPending || locate.isPending;
  const allowedScopes = kind === 'set_default_policy' && state.backend === 'firewalld' ? scopes.filter(([s]) => s !== 'runtime') : scopes;
  return <>
    <DrawerBody className="min-h-0 space-y-5 overflow-y-auto">
      <form id="firewall-editor" className="space-y-5" onSubmit={(event) => { event.preventDefault(); onDirty(); preview.mutate(); }}>
        {kind !== 'set_enabled' && <ChoiceField label="修改范围" value={scope} options={allowedScopes} disabled={inFlight || uncertain} onChange={(v) => set(setScope)(v as Scope)} />}
        {state.backend === 'firewalld' && kind !== 'set_enabled' && <ChoiceField label="区域" value={zone} options={zones.map((z) => [z, z] as const)} disabled={Boolean(rule) || inFlight || uncertain} onChange={set(setZone)} />}
        {state.backend === 'nftables' && kind !== 'set_enabled' && <ChoiceField label="入站链" value={chain} options={chains.map((c) => [c, c] as const)} disabled={Boolean(rule) || inFlight || uncertain} onChange={set(setChain)} />}
        {changesRule && <>
          <ChoiceField label="地址族" value={family} options={[[ 'ipv4', 'IPv4' ], ['ipv6', 'IPv6']]} disabled={Boolean(rule) || inFlight || uncertain} onChange={(v) => set(setFamily)(v as Family)} />
          <TextField label="来源 IP / CIDR" value={source} disabled={inFlight || uncertain} onChange={(e) => set(setSource)(e.target.value)} placeholder={family === 'ipv4' ? '0.0.0.0/0' : '::/0'} />
          <TextField label="目标 IP / CIDR" value={destination} disabled={inFlight || uncertain} onChange={(e) => set(setDestination)(e.target.value)} placeholder={family === 'ipv4' ? '0.0.0.0/0' : '::/0'} />
          <div className="grid grid-cols-2 gap-4"><ChoiceField label="协议" value={protocol} options={[[ 'tcp', 'TCP' ], ['udp', 'UDP' ], ['any', '任意协议' ], ...(state.backend === 'ufw' ? [] : [[family === 'ipv4' ? 'icmp' : 'icmpv6', family === 'ipv4' ? 'ICMP' : 'ICMPv6'] as const])]} onChange={(v) => { set(setProtocol)(v as typeof protocol); if (!['tcp', 'udp'].includes(v)) setPort(''); }} disabled={inFlight || uncertain} /><TextField label="端口 / 范围" value={port} disabled={inFlight || uncertain || !['tcp', 'udp'].includes(protocol)} onChange={(e) => set(setPort)(e.target.value)} placeholder="22 或 8000-8100" /></div>
          <ChoiceField label="动作" value={action} options={Object.entries(actionNames).filter(([key]) => key !== 'default')} disabled={inFlight || uncertain} onChange={(v) => set(setAction)(v as typeof action)} />
          {state.backend !== 'firewalld' && <TextField label="备注" value={comment} maxLength={255} disabled={inFlight || uncertain} onChange={(e) => set(setComment)(e.target.value)} />}
        </>}
        {(kind === 'move_rule' || (changesRule && state.backend === 'firewalld')) && (state.backend === 'firewalld' ? <TextField label="优先级" type="number" min={-32768} max={32767} value={priority} disabled={inFlight || uncertain} onChange={(e) => set(setPriority)(e.target.value)} /> : <ChoiceField label="插入位置" value={before} options={[[ '', '链末尾' ], ...state.rules.filter((r) => r.id !== rule?.id && r.scope === (rule?.scope ?? 'permanent') && r.spec?.family === family && r.native.chain === chain).map((r) => [r.id, ruleText(r)] as const)]} disabled={inFlight || uncertain} onChange={set(setBefore)} />)}
        {kind === 'set_default_policy' && <>
          <ChoiceField label="策略" value={policy} options={Object.entries(actionNames).filter(([key]) => state.backend === 'firewalld' || (key !== 'default' && (state.backend !== 'nftables' || key !== 'reject')))} disabled={inFlight || uncertain} onChange={set(setPolicy)} />
          {state.backend === 'firewalld' && scope === 'both' && <ChoiceField label="生效方式" value={reload ? 'yes' : 'no'} options={[[ 'yes', '有条件重载' ], ['no', '不重载' ]]} onChange={(v) => set(setReload)(v === 'yes')} disabled={inFlight || uncertain} />}
        </>}
        {kind === 'delete_rule' && rule && <p className="break-words text-sm">{ruleText(rule)}</p>}
        {kind === 'set_enabled' && <p className="text-sm">{state.service_active ? '停用当前管理器' : '启用当前管理器'}</p>}
        <Failure error={preview.error ?? apply.error ?? locate.error} />
      </form>
      {plan && <section className="space-y-4 border-t border-border pt-5"><h3 className="font-semibold">变更预览</h3>
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm"><dt className="text-foreground-muted">动作</dt><dd>{kindNames[plan.change.kind as EditKind]}</dd><dt className="text-foreground-muted">范围</dt><dd>{plan.affected_scopes.map((s) => s.replace('permanent', '永久配置').replace('runtime', '运行配置').replace('both', '运行与永久')).join('，')}</dd><dt className="text-foreground-muted">确认</dt><dd>{plan.requires_confirmation ? '首次写入后 120 秒内确认' : '无需再次确认'}</dd></dl>
        {plan.baseline.rules.length > 0 && <div><h4 className="mb-2 text-sm text-foreground-muted">当前受影响规则</h4><ul className="max-h-48 space-y-2 overflow-y-auto text-xs">{plan.baseline.rules.map((r) => <li key={r.id} className="break-words">{scopeNames[r.scope]}：{ruleText(r)}</li>)}</ul></div>}
        {plan.baseline.default_policies.length > 0 && <ul className="space-y-2 text-sm">{plan.baseline.default_policies.map((p, i) => <li key={i}>{p.family === 'ipv4' ? 'IPv4' : 'IPv6'} {scopeNames[p.scope]}：{actionNames[p.action as keyof typeof actionNames] ?? p.action}{plan.change.kind === 'set_default_policy' && <> → {actionNames[policy as keyof typeof actionNames]}</>}</li>)}</ul>}
        {plan.reasons.map((reason, i) => <p key={i} role="alert" className="text-sm">{reason}</p>)}
      </section>}
    </DrawerBody>
    <DrawerFooter><Button variant="outline" disabled={inFlight} onClick={onCancel}>取消</Button>{uncertain ? <Button disabled={locate.isPending} onClick={() => locate.mutate()}>查询原操作</Button> : plan?.executable ? <Button disabled={disabled || inFlight} onClick={() => apply.mutate()}>应用</Button> : <Button type="submit" form="firewall-editor" disabled={disabled || inFlight}>预览</Button>}</DrawerFooter>
  </>;
}

function InstallFirewall({ nodeID, available, disabled }: { nodeID: string; available: boolean; disabled: boolean }) {
  const [backend, setBackend] = useState<Backend>('ufw');
  const [confirm, setConfirm] = useState(false);
  const [key, setKey] = useState('');
  const [uncertain, setUncertain] = useState(false);
  const operations = useOperations();
  const accepted = (id: string) => { setConfirm(false); setUncertain(false); invalidateOperations(); operations.show(id); };
  const install = useMutation({ mutationFn: async () => unwrap(await api.POST('/nodes/{id}/firewall/install', { params: { path: { id: nodeID }, header: { 'X-CSRF-Token': '', 'Idempotency-Key': key } }, body: { backend } })), onSuccess: (op) => accepted(op.id), onError: () => setUncertain(true) });
  const locate = useMutation({ mutationFn: async () => unwrap(await api.GET('/operations', { params: { query: { node_id: nodeID, idempotency_key: key } } })), onSuccess: (result) => { const op = result.items[0]; if (op) accepted(op.id); else { setUncertain(false); install.reset(); } } });
  if (!available && !uncertain && !install.isPending) return null;
  return <div className="flex flex-wrap items-end gap-4 py-4"><ChoiceField label="管理器" value={backend} options={[[ 'ufw', 'UFW' ], ['firewalld', 'firewalld' ], ['nftables', 'nftables' ]]} onChange={(v) => { setBackend(v as Backend); setKey(''); }} disabled={disabled || install.isPending || uncertain} /><Button disabled={disabled || install.isPending} onClick={() => { if (!key) setKey(newKey()); setConfirm(true); }}>{uncertain ? '查询原操作' : '安装'}</Button><Failure error={install.error} />
    <Confirm open={confirm} title={uncertain ? '查询本次安装结果' : `安装 ${backend}？`} description="安装完成后保持停用，已有规则与默认策略不变。" busy={install.isPending || locate.isPending} error={locate.error ?? install.error} confirmLabel={uncertain ? '查询原操作' : '安装'} onCancel={() => setConfirm(false)} onConfirm={() => uncertain ? locate.mutate() : install.mutate()} />
  </div>;
}
