import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Button, buttonVariants } from '../business-ui';
import { Skeleton } from '@appica/ui-react/skeleton';
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle, DrawerBody, DrawerFooter } from '@appica/ui-react/drawer';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@appica/ui-react/tabs';
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from '@appica/ui-react/dropdown-menu';
import { api, findOperation, RequestError, unwrap, unknownResult } from '../api/client';
import type { components } from '../api/types';
import { useSession } from '../auth';
import { useOperations, invalidateOperations } from '../operations';
import { queryClient } from '../state';
import { BusyButton, ChoiceField, Confirm, date, Failure, LoadingRows, newRequestKey, PageTitle, PageToolbar, StatusLabel, TextField, useUnsavedChanges } from '../business-ui';
import { nodesQuery } from './Nodes';
import { TrustRulesTable, trustPolicyQuery, trustRulesQuery } from '../network';
import { CopyAddress } from '../CopyAddress';
import { CommandError } from '../commands';

type Rule = components['schemas']['ManagedRule'];
type State = components['schemas']['ManagedFirewallState'];
type Change = components['schemas']['ManagedFirewallChange'];
type Family = components['schemas']['AddressFamily'];
type Backend = components['schemas']['FirewallInstallRequest']['backend'];
const actionNames = { allow: '允许', deny: '丢弃', reject: '拒绝', default: '原生默认' };
const kindNames = { add_rule: '添加规则', edit_rule: '编辑规则', delete_rule: '删除规则', move_rule: '调整顺序', set_default_policy: '默认入站策略' };
const stateNames: Record<State['state'], string> = { importing: '导入中', unavailable: '未安装', pending: '待应用', applying: '应用中', awaiting_confirmation: '待确认', in_sync: '已生效', failed: '失败', unknown: '待处理' };
type EditKind = keyof typeof kindNames;
function ruleText(rule: Rule) { const r = rule.spec; return `${r.family === 'ipv4' ? 'IPv4' : 'IPv6'} ${r.source || '任意来源'} → ${r.destination || '任意目标'} ${r.protocol.toUpperCase()}${r.port ? `/${r.port}` : ''} ${actionNames[r.action]}`; }
function address(value?: string, fallback = '任意来源') { return !value || value === '0.0.0.0/0' || value === '::/0' ? fallback : value; }
async function findRequest(nodeID: string, key: string) { return findOperation(nodeID, key); }
function uncertainResult(error: unknown) { return unknownResult(error); }

export function Firewall() {
  const session = useSession();
  const [search] = useSearchParams();
  const nodeID = search.get('node') || session.local_node_id;
  return <FirewallContent key={nodeID} nodeID={nodeID} />;
}
function FirewallContent({ nodeID }: { nodeID: string }) {
  const [search, setSearch] = useSearchParams();
  const family = search.get('family') || '';
  const category = search.get('type') === 'trust' ? 'trust' : search.get('type') === 'ip' ? 'ip' : 'port';
  const policy = useQuery(trustPolicyQuery);
  const trust = useQuery(trustRulesQuery(nodeID));
  const showTrust = Boolean(policy.data?.enabled || trust.data?.enabled || trust.data?.rules.length || trust.data?.state === 'failed' || trust.data?.migration?.state === 'failed' || trust.data?.migration?.state === 'pending' || trust.data && trust.data.target_revision > trust.data.applied_revision);
  useEffect(() => { if (category === 'trust' && policy.data && trust.data && !showTrust) setSearch((current) => { const next = new URLSearchParams(current); next.set('type', 'ip'); return next; }, { replace: true }); }, [category, showTrust, policy.data, trust.data, setSearch]);
  const nodes = useQuery(nodesQuery);
  const node = nodes.data?.items.find((n) => n.id === nodeID);
  const [editor, setEditor] = useState<{ kind: EditKind; rule?: Rule } | null>(null);
  const [dirty, setDirty] = useState(false);
  const [detail, setDetail] = useState<Rule | null>(null);
  const [discard, setDiscard] = useState(false);
  useUnsavedChanges(dirty);
  const update = (key: string, value: string) => setSearch((current) => { const next = new URLSearchParams(current); next.delete('scope'); if (value) next.set(key, value); else next.delete(key); return next; });
  const state = useQuery({ queryKey: ['firewall', nodeID], enabled: Boolean(nodeID), staleTime: 30000, retry: false, queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{node_id}/firewall/status', { signal, params: { path: { node_id: nodeID } } })) });
  const [refreshing, setRefreshing] = useState(false);
  const refresh = async () => { setRefreshing(true); try { await Promise.all([state.refetch(), trust.refetch()]); } finally { setRefreshing(false); } };
  const read = state.data;
  const busy = Boolean(read && ['pending', 'applying', 'awaiting_confirmation', 'unknown'].includes(read.state));
  const disabled = state.isError || node?.writable === false || node?.connected === false || Boolean(node?.revoked_at) || busy;
  const rules = read?.rules.filter((r) => !family || r.spec.family === family) ?? [];
  const showComments = (type: 'port' | 'ip') => rules.some((r) => Boolean(r.spec.port) === (type === 'port') && Boolean(r.spec.comment));
  const close = () => { setEditor(null); setDirty(false); setDiscard(false); };
  const retryKey = useRef('');
  const [retryUncertain, setRetryUncertain] = useState(false);
  const operations = useOperations();
  const accepted = (result: components['schemas']['Operation']) => { void queryClient.invalidateQueries({ queryKey: ['firewall', nodeID] }); void queryClient.invalidateQueries({ queryKey: ['peer-trust', nodeID] }); retryKey.current = ''; setRetryUncertain(false); invalidateOperations(); operations.show(result.id); };
  const retry = useMutation({ mutationFn: async () => {
    if (!retryKey.current) retryKey.current = newRequestKey();
    return unwrap(await api.POST('/nodes/{node_id}/firewall/actions', { params: { path: { node_id: nodeID }, header: { 'X-CSRF-Token': '', 'Idempotency-Key': retryKey.current } }, body: { action: 'sync', expected_revision: read!.revision } }));
  }, onSuccess: accepted, onError: (error) => setRetryUncertain(uncertainResult(error)) });
  const locateRetry = useMutation({ mutationFn: () => findRequest(nodeID, retryKey.current), onSuccess: (op) => { if (op) operations.show(op.id); setRetryUncertain(false); retryKey.current = ''; void state.refetch(); } });
  return <section className="panel">
    <PageTitle>防火墙</PageTitle>
    <PageToolbar className="firewall-filters">
      <ChoiceField label="目标节点" value={nodeID} options={nodes.data?.items.filter((n) => !n.revoked_at).map((n) => [n.id, n.name] as const) ?? [[nodeID, '本机']]} onChange={(value) => update('node', value)} />
      <ChoiceField label="地址族" value={family} options={[[ '', 'IPv4 与 IPv6' ], ['ipv4', 'IPv4'], ['ipv6', 'IPv6']]} onChange={(value) => update('family', value)} />
    </PageToolbar>
    <div className="firewall-status-row">
      <div className="firewall-status min-w-0 flex flex-wrap items-center gap-x-3 gap-y-2">
        {!read ? state.isPending ? <Skeleton className="h-5 w-48" /> : '—' : <><span className="font-medium text-foreground-intense">{read.backend === 'none' ? '未安装防火墙' : read.backend === 'unknown' ? '防火墙' : read.backend === 'ufw' ? 'UFW' : read.backend}</span>{read.service_active === false && <StatusLabel state="offline">已停用</StatusLabel>}<StatusLabel state={read.state}>{read.state === 'in_sync' && read.service_active === false ? '已保存' : stateNames[read.state]}</StatusLabel>{read.checked_at && <span className="text-xs tabular-nums text-foreground-muted">{date(read.checked_at)}</span>}</>}
      </div>
      <div className="toolbar-actions"><BusyButton variant="outline" busy={refreshing} disabled={state.isFetching || trust.isFetching} onClick={() => void refresh()}>刷新</BusyButton><Link to={`/firewall/settings?${search}`} className={buttonVariants({ variant: 'outline' })}>设置</Link></div>
    </div>
    <Failure error={state.error} retry={() => void refresh()} busy={refreshing} /><Failure error={nodes.error} retry={() => void nodes.refetch()} busy={nodes.isFetching} />
    {node?.revoked_at && <p role="alert" className="mb-3"><StatusLabel state="offline">节点已撤销</StatusLabel></p>}
    {node?.connected === false && <p role="status" className="mb-3"><StatusLabel state="offline">节点离线</StatusLabel></p>}
    {read?.error && <p role="alert" className="mb-3 text-sm text-error-intense">{read.error.message}</p>}
    <Failure error={retry.error ?? locateRetry.error} />
    {read && <InstallFirewall key={nodeID} nodeID={nodeID} revision={read.revision} available={read.backend === 'none' && read.capabilities.install.supported} disabled={node?.writable === false || state.isError} />}
    <Tabs variant="line" className="gap-4" value={category} onValueChange={(value) => update('type', String(value))}>
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-3">
        <TabsList aria-label="规则类别" className="max-w-full overflow-x-auto"><TabsTrigger value="port">端口规则 {read && rules.filter((r) => r.spec.port).length}</TabsTrigger><TabsTrigger value="ip">IP 规则 {read && rules.filter((r) => !r.spec.port).length}</TabsTrigger>{showTrust && <TabsTrigger value="trust">互信规则 {trust.data?.rules.filter((r) => r.address && (!family || r.family === family)).length}</TabsTrigger>}</TabsList>
        <div className="flex flex-wrap gap-2">{category !== 'trust' && <><Button disabled={!read?.capabilities.edit_rules.supported || disabled || retryUncertain} onClick={() => { setEditor({ kind: 'add_rule' }); setDirty(false); }}>添加规则</Button><Button variant="outline" disabled={!read?.capabilities.default_policy.supported || disabled || retryUncertain} onClick={() => { setEditor({ kind: 'set_default_policy' }); setDirty(false); }}>默认入站策略</Button></>}{read?.operation_id && !(category === 'trust' && trust.data?.migration && trust.data.migration.state !== 'in_sync') && <Button variant="ghost" onClick={() => operations.show(read.operation_id!)}>查看操作</Button>}{retryUncertain ? <Button variant="outline" disabled={locateRetry.isPending} onClick={() => locateRetry.mutate()}>查询原操作</Button> : read && (read.state === 'failed' || read.migration?.state === 'failed' || trust.data?.migration?.state === 'failed') && read.revision > 0 && <BusyButton variant="outline" busy={retry.isPending} disabled={disabled} onClick={() => retry.mutate()}>重试</BusyButton>}</div>
      </div>
      {(['port', 'ip'] as const).map((type) => <TabsContent key={type} value={type}>
        <div className="max-w-full overflow-x-auto" aria-busy={state.isPending || read?.state === 'importing'}><Table hoverableRows className={type === 'port' ? 'min-w-180' : 'min-w-160'}>
          <TableHeader><TableRow>{type === 'port' && <TableHead>端口</TableHead>}<TableHead>来源</TableHead><TableHead>目标</TableHead><TableHead>协议</TableHead><TableHead>动作</TableHead>{showComments(type) && <TableHead>备注</TableHead>}<TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
          <TableBody>{state.isPending || read?.state === 'importing' ? <LoadingRows columns={(type === 'port' ? 6 : 5) + Number(showComments(type))} /> : rules.filter((r) => type === 'port' ? Boolean(r.spec.port) : !r.spec.port).map((rule) => <TableRow key={rule.id}>
            {type === 'port' && <TableCell className="whitespace-nowrap font-mono text-xs">{rule.spec.port}</TableCell>}<TableCell className="max-w-48 break-all">{address(rule.spec.source) === '任意来源' ? <span className="font-mono text-xs">任意来源</span> : <CopyAddress value={rule.spec.source} />}<span className="block text-xs text-foreground-muted">{rule.spec.family === 'ipv4' ? 'IPv4' : 'IPv6'}</span></TableCell><TableCell className="max-w-48 break-all font-mono text-xs">{address(rule.spec.destination, '任意目标') === '任意目标' ? '任意目标' : <CopyAddress value={rule.spec.destination!} />}</TableCell><TableCell>{rule.spec.protocol === 'any' ? 'ANY' : rule.spec.protocol.toUpperCase()}</TableCell><TableCell>{actionNames[rule.spec.action]}</TableCell>{showComments(type) && <TableCell className="max-w-48 break-words text-sm text-foreground-muted">{rule.spec.comment || '—'}</TableCell>}<TableCell><div className="flex justify-end gap-1"><Button variant="ghost" size="sm" onClick={() => setDetail(rule)}>查看</Button><DropdownMenu><DropdownMenuTrigger className={buttonVariants({ variant: 'ghost', size: 'sm' })} disabled={disabled || retryUncertain || !read?.capabilities.edit_rules.supported}>更多</DropdownMenuTrigger><DropdownMenuContent align="end">{(['edit_rule', 'move_rule', 'delete_rule'] as const).map((kind) => <DropdownMenuItem key={kind} className={kind === 'delete_rule' ? 'text-error-intense' : undefined} onClick={() => { setEditor({ kind, rule }); setDirty(false); }}>{kind === 'edit_rule' ? '编辑' : kind === 'delete_rule' ? '删除' : '排序'}</DropdownMenuItem>)}</DropdownMenuContent></DropdownMenu></div></TableCell>
          </TableRow>)}{!state.isPending && read?.state !== 'importing' && !state.isError && !rules.some((r) => type === 'port' ? Boolean(r.spec.port) : !r.spec.port) && <TableRow><TableCell colSpan={(type === 'port' ? 6 : 5) + Number(showComments(type))} className="py-8 text-center text-foreground-muted">暂无规则</TableCell></TableRow>}</TableBody>
        </Table></div>
      </TabsContent>)}
      {showTrust && <TabsContent value="trust"><Failure error={trust.error} retry={() => void trust.refetch()} busy={trust.isFetching} /><TrustRulesTable data={trust.data} family={family} loading={trust.isPending} error={trust.error} /></TabsContent>}
    </Tabs>
    <Drawer side="right" open={editor !== null} onOpenChange={(next) => { if (!next) { if (dirty) setDiscard(true); else close(); } }}><DrawerContent frame={false} closeLabel="关闭编辑" className="ui-sheet h-full w-full max-w-xl"><DrawerHeader><DrawerTitle>{editor ? kindNames[editor.kind] : '编辑规则'}</DrawerTitle></DrawerHeader>{editor && read && <RuleEditor key={`${nodeID}/${editor.kind}/${editor.rule?.id ?? ''}`} nodeID={nodeID} state={read} initialFamily={family === 'ipv6' ? 'ipv6' : 'ipv4'} kind={editor.kind} rule={editor.rule} disabled={disabled} onDirty={() => setDirty(true)} onClose={close} onCancel={() => { if (dirty) setDiscard(true); else close(); }} />}</DrawerContent></Drawer>
    <Drawer side="right" open={detail !== null} onOpenChange={(open) => { if (!open) setDetail(null); }}><DrawerContent frame={false} closeLabel="关闭规则详情" className="ui-sheet h-full w-full max-w-xl"><DrawerHeader><DrawerTitle>规则详情</DrawerTitle></DrawerHeader><DrawerBody>{detail && <dl className="space-y-3 text-sm">{[['来源', address(detail.spec.source)], ['目标', address(detail.spec.destination, '任意目标')], ['协议', detail.spec.protocol.toUpperCase()], ['端口', detail.spec.port], ['动作', actionNames[detail.spec.action]], ['区域', detail.zone], ['入站链', detail.chain], ['优先级', detail.priority], ['备注', detail.spec.comment]].filter(([, value]) => value !== undefined && value !== '').map(([label, value]) => <div key={String(label)} className="grid grid-cols-[5rem_minmax(0,1fr)] gap-3"><dt className="text-foreground-muted">{label}</dt><dd className="break-all">{value}</dd></div>)}</dl>}</DrawerBody></DrawerContent></Drawer>
    <Confirm open={discard} title="丢弃未提交的修改？" description="已保存的配置仍会继续应用。" busy={false} error={null} confirmLabel="丢弃修改" onCancel={() => setDiscard(false)} onConfirm={close} />
  </section>;
}

function RuleEditor({ nodeID, state: initialState, initialFamily, kind, rule, disabled, onDirty, onClose, onCancel }: { nodeID: string; state: State; initialFamily: Family; kind: EditKind; rule?: Rule; disabled: boolean; onDirty: () => void; onClose: () => void; onCancel: () => void }) {
  const operations = useOperations();
  const [state] = useState(initialState);
  const revision = useRef(state.revision);
  const [needsRead, setNeedsRead] = useState(false);
  const requestKey = useRef('');
  const [family, setFamily] = useState<Family>(rule?.spec.family ?? initialFamily);
  const [source, setSource] = useState(rule?.spec.source ?? '');
  const [destination, setDestination] = useState(rule?.spec.destination ?? '');
  const [protocol, setProtocol] = useState(rule?.spec.protocol ?? 'tcp');
  const [port, setPort] = useState(rule?.spec.port ?? '');
  const [action, setAction] = useState<components['schemas']['RuleSpec']['action']>(rule?.spec.action ?? 'allow');
  const [comment, setComment] = useState(rule?.spec.comment ?? '');
  const zones = [...new Set(state.default_policies.map((p) => p.zone).filter((z): z is string => Boolean(z)))];
  const chains = [...new Set(state.default_policies.map((p) => p.chain).filter((c): c is string => Boolean(c)))];
  const [zone, setZone] = useState(rule?.zone ?? zones[0] ?? '');
  const [chain, setChain] = useState(rule?.chain ?? chains[0] ?? '');
  const [priority, setPriority] = useState(String(rule?.priority ?? 0));
  const [before, setBefore] = useState('');
  const [policy, setPolicy] = useState('deny');
  const [uncertain, setUncertain] = useState(false);
  const [preview, setPreview] = useState<Change | null>(null);
  const changesRule = kind === 'add_rule' || kind === 'edit_rule';
  const changed = () => { onDirty(); setPreview(null); requestKey.current = ''; };
  const set = <T,>(write: (value: T) => void) => (value: T) => { write(value); changed(); };
  const build = (): Change => {
    const location = { zone: state.backend === 'firewalld' ? zone : undefined, chain: state.backend === 'nftables' ? chain : undefined };
    if (kind === 'set_default_policy') return { kind, action: policy as Change['action'], ...location };
    if (kind === 'delete_rule') return { kind, rule_id: rule!.id };
    if (kind === 'move_rule') return { kind, rule_id: rule!.id, before_rule_id: before || undefined, priority: state.backend === 'firewalld' ? Number(priority) : undefined };
    return { kind, rule_id: kind === 'edit_rule' ? rule!.id : undefined, rule: { family, source: source.trim() || (family === 'ipv4' ? '0.0.0.0/0' : '::/0'), destination: destination.trim() || undefined, protocol, port: port.trim() || undefined, action, comment: comment.trim() || undefined }, priority: state.backend === 'firewalld' ? Number(priority) : undefined, ...location };
  };
  const accepted = (result: components['schemas']['Operation']) => { void queryClient.invalidateQueries({ queryKey: ['firewall', nodeID] }); invalidateOperations(); onClose(); operations.show(result.id); };
  const save = useMutation({ mutationFn: async () => {
    if (!requestKey.current) requestKey.current = newRequestKey();
    return unwrap(await api.POST('/nodes/{node_id}/firewall/changes', { params: { path: { node_id: nodeID }, header: { 'X-CSRF-Token': '', 'Idempotency-Key': requestKey.current } }, body: { expected_revision: revision.current, change: preview! } }));
  }, onSuccess: accepted, onError: (error) => { setUncertain(uncertainResult(error)); setNeedsRead(error instanceof RequestError && [409, 412, 428].includes(error.status)); if (!uncertainResult(error)) requestKey.current = ''; } });
  const reread = useMutation({ mutationFn: async () => unwrap(await api.GET('/nodes/{node_id}/firewall/configuration', { params: { path: { node_id: nodeID } } })), onSuccess: current => { revision.current = current.revision; requestKey.current = ''; setNeedsRead(false); save.reset(); } });
  const locate = useMutation({ mutationFn: () => findRequest(nodeID, requestKey.current), onSuccess: (op) => { if (op) { onClose(); operations.show(op.id); void queryClient.invalidateQueries({ queryKey: ['firewall', nodeID] }); } else { setUncertain(false); save.reset(); } } });
  const inFlight = save.isPending || locate.isPending;
  return <>
    <DrawerBody className="min-h-0 space-y-5 overflow-y-auto"><form id="firewall-editor" className="space-y-5" onSubmit={(event) => { event.preventDefault(); onDirty(); setPreview(build()); }}>
      {state.backend === 'firewalld' && <ChoiceField label="区域" value={zone} options={zones.map((z) => [z, z] as const)} disabled={Boolean(rule) || inFlight || uncertain} onChange={set(setZone)} />}
      {state.backend === 'nftables' && <ChoiceField label="入站链" value={chain} options={chains.map((c) => [c, c] as const)} disabled={Boolean(rule) || inFlight || uncertain} onChange={set(setChain)} />}
      {changesRule && <>
        <ChoiceField label="地址族" value={family} options={[[ 'ipv4', 'IPv4' ], ['ipv6', 'IPv6']]} disabled={Boolean(rule) || inFlight || uncertain} onChange={(v) => set(setFamily)(v as Family)} />
        <TextField label="来源 IP / CIDR" value={source} disabled={inFlight || uncertain} onChange={(e) => set(setSource)(e.target.value)} placeholder={family === 'ipv4' ? '0.0.0.0/0' : '::/0'} />
        <TextField label="目标 IP / CIDR" value={destination} disabled={inFlight || uncertain} onChange={(e) => set(setDestination)(e.target.value)} placeholder={family === 'ipv4' ? '0.0.0.0/0' : '::/0'} />
        <div className="grid grid-cols-2 gap-4"><ChoiceField label="协议" value={protocol} options={[[ 'tcp', 'TCP' ], ['udp', 'UDP' ], ['any', '任意协议' ], ...(state.backend === 'ufw' ? [] : [[family === 'ipv4' ? 'icmp' : 'icmpv6', family === 'ipv4' ? 'ICMP' : 'ICMPv6'] as const])]} onChange={(v) => { set(setProtocol)(v as typeof protocol); if (!['tcp', 'udp'].includes(v)) setPort(''); }} disabled={inFlight || uncertain} /><TextField label="端口 / 范围" value={port} disabled={inFlight || uncertain || !['tcp', 'udp'].includes(protocol)} onChange={(e) => set(setPort)(e.target.value)} placeholder="22 或 8000-8100" /></div>
        <ChoiceField label="动作" value={action} options={Object.entries(actionNames).filter(([key]) => key !== 'default')} disabled={inFlight || uncertain} onChange={(v) => set(setAction)(v as typeof action)} />
        {state.backend !== 'firewalld' && <TextField label="备注" value={comment} maxLength={255} disabled={inFlight || uncertain} onChange={(e) => set(setComment)(e.target.value)} />}
      </>}
      {(kind === 'move_rule' || (changesRule && state.backend === 'firewalld')) && (state.backend === 'firewalld' ? <TextField label="优先级" type="number" min={-32768} max={32767} value={priority} disabled={inFlight || uncertain} onChange={(e) => set(setPriority)(e.target.value)} /> : <ChoiceField label="插入位置" value={before} options={[[ '', '链末尾' ], ...state.rules.filter((r) => r.id !== rule?.id && r.spec.family === family && r.chain === rule?.chain && r.zone === rule?.zone).map((r) => [r.id, ruleText(r)] as const)]} disabled={inFlight || uncertain} onChange={set(setBefore)} />)}
      {kind === 'set_default_policy' && <ChoiceField label="策略" value={policy} options={Object.entries(actionNames).filter(([key]) => state.backend === 'firewalld' || (key !== 'default' && (state.backend !== 'nftables' || key !== 'reject')))} disabled={inFlight || uncertain} onChange={set(setPolicy)} />}
      {kind === 'delete_rule' && rule && <p className="break-words text-sm">{ruleText(rule)}</p>}
      {uncertain ? <Failure error={save.error ?? locate.error}/> : <><Failure error={reread.error}/><CommandError error={save.error ?? locate.error} read={() => reread.mutate()}/></>}
    </form>{preview && <section className="space-y-3 border-t border-border pt-5"><h3 className="font-semibold">变更预览</h3><p className="break-words text-sm">{kindNames[kind]}：{preview.rule ? ruleText({ id: '', spec: preview.rule }) : kind === 'set_default_policy' ? actionNames[preview.action!] : rule ? ruleText(rule) : ''}</p>{kind === 'move_rule' && <p className="text-sm">{state.backend === 'firewalld' ? `优先级 ${priority}` : before ? `移至 ${ruleText(state.rules.find((r) => r.id === before)!)} 之前` : '移至链末尾'}</p>}</section>}</DrawerBody>
    <DrawerFooter><Button variant="outline" disabled={inFlight} onClick={onCancel}>取消</Button>{uncertain ? <BusyButton busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton> : preview ? <BusyButton busy={save.isPending} disabled={disabled || inFlight || needsRead} onClick={() => save.mutate()}>保存</BusyButton> : <Button type="submit" form="firewall-editor" disabled={disabled || inFlight}>预览</Button>}</DrawerFooter>
  </>;
}

function InstallFirewall({ nodeID, revision, available, disabled }: { nodeID: string; revision: number; available: boolean; disabled: boolean }) {
  const [backend, setBackend] = useState<Backend>('ufw');
  const [confirm, setConfirm] = useState(false);
  const [key, setKey] = useState('');
  const [uncertain, setUncertain] = useState(false);
  const operations = useOperations();
  const accepted = (id: string) => { setConfirm(false); setUncertain(false); invalidateOperations(); operations.show(id); };
  const install = useMutation({ mutationFn: async () => unwrap(await api.POST('/nodes/{node_id}/firewall/actions', { params: { path: { node_id: nodeID }, header: { 'X-CSRF-Token': '', 'Idempotency-Key': key } }, body: { action: 'install', backend, expected_revision: revision } })), onSuccess: (op) => accepted(op.id), onError: (error) => setUncertain(unknownResult(error)) });
  const locate = useMutation({ mutationFn: () => findOperation(nodeID, key), onSuccess: (op) => accepted(op.id) });
  if (!available && !uncertain && !install.isPending) return null;
  return <div className="flex flex-wrap items-end gap-4 py-4"><ChoiceField label="管理器" value={backend} options={[[ 'ufw', 'UFW' ], ['firewalld', 'firewalld' ], ['nftables', 'nftables' ]]} onChange={(v) => { setBackend(v as Backend); setKey(''); }} disabled={disabled || install.isPending || uncertain} /><BusyButton busy={install.isPending} disabled={disabled} onClick={() => { if (!key) setKey(newRequestKey()); setConfirm(true); }}>{uncertain ? '查询原操作' : '安装'}</BusyButton><Failure error={install.error} />
    <Confirm open={confirm} title={uncertain ? '查询本次安装结果' : `安装 ${backend}？`} description={`目标节点：${nodeID}。安装完成后保持停用，已有规则与默认策略不变。`} busy={install.isPending || locate.isPending} error={locate.error ?? install.error} confirmLabel={uncertain ? '查询原操作' : '安装'} onCancel={() => setConfirm(false)} onConfirm={() => uncertain ? locate.mutate() : install.mutate()} />
  </div>;
}
