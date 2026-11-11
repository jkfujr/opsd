import { useEffect, useId, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Button, buttonVariants } from '@appica/ui-react/button';
import { Switch } from '@appica/ui-react/switch';
import { Skeleton } from '@appica/ui-react/skeleton';
import { Field, FieldLabel } from '@appica/ui-react/field';
import { Textarea } from '@appica/ui-react/textarea';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle, DrawerBody } from '@appica/ui-react/drawer';
import { api, condition, unwrap, unknownResult } from './api/client';
import type { components } from './api/schema.gen';
import { queryClient } from './state';
import { BusyButton, ChoiceField, Confirm, date, Failure, LoadingRows, PageTitle, PageToolbar, StatusLabel, useUnsavedChanges } from './ui';
import { nodesQuery } from './pages/Nodes';
import { CopyAddress } from './CopyAddress';
import { useOperations } from './operations';

type Public = components['schemas']['PublicAddresses'];
type Trust = components['schemas']['PeerTrustNodeStatus'];
type Policy = components['schemas']['PeerTrustPolicy'];

export const networkStates: Record<string, string> = {
  pending: '待应用', in_sync: '已生效', disabled: '已关闭', offline: '离线', failed: '失败', inactive: '未启用', unsupported: '未支持',
  removing: '待撤除', saved: '已保存', confirmed: '已确认', conflict: '地址冲突', partial: '部分生效', empty: '未配置', unavailable: '无公网 IPv6',
};

function combinedTrustState(states: string[], fallback: string) {
	const present=states.filter((state)=>state!=='unavailable');
	if (!present.length && states.length) return 'unavailable';
	states=present;
  if (!states.length) return fallback;
  if (states.includes('in_sync') && states.some((state) => state !== 'in_sync')) return 'partial';
  return ['failed', 'unsupported', 'conflict', 'removing', 'pending', 'saved', 'inactive'].find((state) => states.includes(state)) ?? states[0];
}

function trustState(data: Trust, family?: 'ipv4' | 'ipv6') {
  if (data.state === 'offline' || data.state === 'disabled') return data.state;
  const rules = data.rules.filter((rule) => !family || rule.family === family);
  const results = data.results.filter((result) => !family || result.family === family);
  const fallback = results.find((result) => result.error)?.state ?? (data.enabled && !rules.length ? 'empty' : data.state);
  return combinedTrustState(rules.map((rule) => rule.state), fallback);
}

function trustSources(rules: Trust['rules']) {
  const sources = new Map<string, { id: string; name: string; rules: Trust['rules'] }>();
  for (const rule of rules) {
    for (const member of rule.members.length ? rule.members : [{ id: '', name: '已移除节点' }]) {
      let source = sources.get(member.id);
      if (!source) { source = { ...member, rules: [] }; sources.set(member.id, source); }
      source.rules.push(rule);
    }
  }
  return [...sources.values()];
}
export const trustPolicyQuery = { queryKey: ['peer-trust', 'policy'], queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/peer-trust', { signal })) };
export const trustRulesQuery = (id: string) => ({ queryKey: ['peer-trust', id], retry: false, queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/nodes/{node_id}/firewall/peer-trust', { signal, params: { path: { node_id: id } } })), refetchInterval: 5000 });
const uncertainWrite = (error: unknown) => unknownResult(error);
function invalidateNetwork() { void queryClient.invalidateQueries({ queryKey: ['peer-trust'] }); void queryClient.invalidateQueries({ queryKey: ['public-addresses'] }); }

export function FirewallSettings() {
  const [search] = useSearchParams();
  const policy = useQuery(trustPolicyQuery);
  const status = useQuery({ queryKey: ['peer-trust', 'status'], queryFn: async ({ signal }) => unwrap(await api.GET('/peer-trust/status', { signal })), refetchInterval: 5000 });
  const nodes = useQuery(nodesQuery);
  const [change, setChange] = useState<{ enabled: boolean; previous: Policy } | null>(null);
  const [uncertain, setUncertain] = useState(false);
  const save = useMutation({ mutationFn: async () => unwrap(await api.PATCH('/peer-trust', { params: { header: condition(change!.previous) }, body: { enabled: change!.enabled } })), onSuccess: (value) => { queryClient.setQueryData(['peer-trust', 'policy'], value); setChange(null); invalidateNetwork(); }, onError: (error) => { const unknown = uncertainWrite(error); setUncertain(unknown); if (unknown) setChange(null); } });
  const verify = async () => { const result = await policy.refetch(); if (result.data) { setUncertain(false); setChange(null); save.reset(); invalidateNetwork(); } };
  const names = new Map(nodes.data?.items.map((n) => [n.id, n.name]));
  const count = nodes.data?.items.filter((n) => !n.revoked_at).length ?? 0;
  return <section className="panel">
    <PageTitle>防火墙设置</PageTitle>
    <PageToolbar><Link to={`/firewall${search.size ? `?${search}` : ''}`} className={buttonVariants({ variant: 'outline' })}>返回规则</Link></PageToolbar>
    <Failure error={policy.error} retry={() => void policy.refetch()} busy={policy.isFetching} />
    <div className="flex max-w-lg items-center justify-between gap-4 border-y border-border py-4"><label htmlFor="cluster-peer-trust" className="font-medium">集群互信</label>{policy.isPending ? <Skeleton className="h-5 w-10" /> : <Switch id="cluster-peer-trust" checked={policy.data?.enabled ?? false} disabled={!policy.data || policy.isError || save.isPending || uncertain} onCheckedChange={(enabled) => { save.reset(); setChange({ enabled, previous: policy.data! }); }} />}</div>
    {uncertain && <div className="mt-4"><BusyButton variant="outline" busy={policy.isFetching} onClick={() => void verify()}>核对设置</BusyButton></div>}
    {change === null && <Failure error={save.error} />}
    <Failure error={status.error} retry={() => void status.refetch()} busy={status.isFetching} />
    <div className="mt-6 overflow-x-auto" aria-busy={status.isPending}><Table className="min-w-160" hoverableRows><TableHeader><TableRow><TableHead>目标节点</TableHead><TableHead>IPv4</TableHead><TableHead>IPv6</TableHead><TableHead>互信状态</TableHead><TableHead>检查时间</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader><TableBody>
      {status.isPending ? <LoadingRows columns={6} /> : status.data?.nodes.map((n) => <TableRow key={n.node_id}><TableCell className="max-w-56 break-words">{names.get(n.node_id) ?? n.node_id}</TableCell>{(['ipv4', 'ipv6'] as const).map((family) => <TableCell key={family}><StatusLabel state={trustState(n, family)}>{networkStates[trustState(n, family)] ?? trustState(n, family)}</StatusLabel></TableCell>)}<TableCell><StatusLabel state={trustState(n)}>{networkStates[trustState(n)] ?? trustState(n)}</StatusLabel></TableCell><TableCell className="whitespace-nowrap text-xs text-foreground-muted">{date(n.checked_at)}</TableCell><TableCell className="text-right"><Link to={`/firewall?node=${n.node_id}&type=trust`} className={buttonVariants({ variant: 'ghost', size: 'sm' })}>查看规则</Link></TableCell></TableRow>)}
      {status.data?.nodes.length === 0 && !status.isError && <TableRow><TableCell colSpan={6} className="py-8 text-center text-foreground-muted">暂无节点</TableCell></TableRow>}
    </TableBody></Table></div>
    <Confirm open={change !== null} title={change?.enabled ? '开启集群互信？' : '关闭集群互信？'} description={change?.enabled ? `为 ${count} 个节点优先放行全部成员公网 IP 的所有主机入站端口，包含本机和数据库端口。已有相同的全协议 IP 放行规则将转交互信管理。共享公网 IP 的其他来源也在放行范围内。` : `撤除 ${count} 个节点的 OPSD 互信规则，包含已从普通 IP 规则转交的条目。离线节点恢复连接后继续撤除。`} confirmLabel={change?.enabled ? '开启互信' : '关闭互信'} dangerous={false} busy={save.isPending} error={save.error} onCancel={() => { if (!uncertain) setChange(null); }} onConfirm={() => { if (!uncertain) save.mutate(); }} />
  </section>;
}

export function TrustRulesTable({ data, family, loading, error }: { data?: Trust; family: string; loading: boolean; error: unknown }) {
  const [detail, setDetail] = useState<string | null>(null);
  const operations = useOperations();
  const rows = data?.rules.filter((r) => !family || r.family === family) ?? [];
  const sources = trustSources(rows);
  const selected = sources.find((source) => source.id === detail);
  const detailRules = selected?.rules ?? rows;
  const families = (['ipv4', 'ipv6'] as const).filter((value) => !family || value === family);
  const state = data ? trustState({ ...data, rules: rows, results: data.results.filter((result) => !family || result.family === family) }) : 'pending';
  return <>
    {data && <div className="mb-3 flex flex-wrap items-center gap-3 text-sm"><StatusLabel state={state}>{networkStates[state] ?? state}</StatusLabel><span className="tabular-nums text-xs text-foreground-muted">{date(data.checked_at)}</span><Button variant="ghost" size="sm" onClick={() => setDetail('*')}>查看详情</Button></div>}
    {data?.migration && data.migration.state !== 'in_sync' && <div className="mb-3 flex flex-wrap items-center gap-3"><StatusLabel state={data.migration.state}>{data.migration.state === 'failed' ? '迁移失败' : '迁移中'}</StatusLabel>{data.migration.error && <span role="alert" className="text-sm text-error-intense">{data.migration.error.message}</span>}<Button variant="ghost" size="sm" onClick={() => operations.show(data.migration!.operation_id)}>查看操作</Button></div>}
    <div className="max-w-full overflow-x-auto" aria-busy={loading}><Table className="min-w-160" hoverableRows><TableHeader><TableRow><TableHead>来源节点</TableHead>{families.map((value) => <TableHead key={value}>{value === 'ipv4' ? 'IPv4' : 'IPv6'}</TableHead>)}<TableHead className="text-right">操作</TableHead></TableRow></TableHeader><TableBody>
      {loading ? <LoadingRows columns={families.length + 2} /> : sources.map((source) => <TableRow key={source.id || 'removed'}><TableCell className="max-w-56 break-words">{source.id ? <Link to={`/nodes/${source.id}`} className="font-medium text-primary hover:underline">{source.name || source.id}</Link> : source.name}{combinedTrustState(source.rules.map((rule) => rule.state), '') === 'partial' && <span className="mt-1 block"><StatusLabel state="partial">部分生效</StatusLabel></span>}</TableCell>{families.map((value) => <TableCell key={value}><div className="space-y-2">{source.rules.filter((rule) => rule.family === value).map((rule, index) => <div key={rule.address || index} className="flex flex-wrap items-center gap-2"><span className="max-w-64 break-all font-mono text-xs">{rule.address ? <CopyAddress value={`${rule.address}/${value === 'ipv4' ? 32 : 128}`} /> : '—'}</span><StatusLabel state={rule.state}>{!rule.address && rule.state === 'pending' ? '待发现' : networkStates[rule.state] ?? rule.state}</StatusLabel></div>)}{!source.rules.some((rule) => rule.family === value) && '—'}</div></TableCell>)}<TableCell className="text-right"><Button variant="ghost" size="sm" onClick={() => setDetail(source.id)}>查看</Button></TableCell></TableRow>)}
      {!loading && !error && sources.length === 0 && <TableRow><TableCell colSpan={families.length + 2} className="py-8 text-center text-foreground-muted">暂无互信规则</TableCell></TableRow>}
    </TableBody></Table></div>
    <Drawer side="right" open={detail !== null} onOpenChange={(open) => { if (!open) setDetail(null); }}><DrawerContent frame={false} closeLabel="关闭互信详情" className="h-full w-full max-w-lg"><DrawerHeader><DrawerTitle>互信详情</DrawerTitle></DrawerHeader><DrawerBody className="space-y-5 overflow-y-auto">
      {selected && <p className="break-words font-medium">{selected.name || selected.id}</p>}
      {families.map((value) => <section key={value} className="space-y-3"><h3 className="font-medium">{value === 'ipv4' ? 'IPv4' : 'IPv6'}</h3>{detailRules.filter((rule) => rule.family === value).map((rule, index) => <div key={rule.address || index} className="space-y-1 text-sm">{rule.address && <CopyAddress value={`${rule.address}/${value === 'ipv4' ? 32 : 128}`} />}{rule.error && !data?.results.some((result) => result.family === value && result.error?.message === rule.error) && <p className="break-words text-error-intense">{rule.error}</p>}</div>)}{data?.results.filter((result) => result.family === value).map((result) => <div key={result.scope} className="space-y-1 text-sm"><div className="flex flex-wrap items-center gap-3"><span>{result.scope === 'permanent' ? '永久配置' : '运行配置'}</span><StatusLabel state={result.state}>{networkStates[result.state] ?? result.state}</StatusLabel></div>{result.error && <p className="break-words text-error-intense">{result.error.message}</p>}</div>)}</section>)}
    </DrawerBody></DrawerContent></Drawer>
  </>;
}

const publicQuery = (id: string) => ({ queryKey: ['public-addresses', id], retry: false, queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/nodes/{node_id}/public-addresses', { signal, params: { path: { node_id: id } } })) });
export function NodePublicAddresses({ id }: { id: string }) {
  const query = useQuery(publicQuery(id));
  return <section id="public-addresses" className="panel"><h2 className="section-title">公网地址</h2><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />{query.data ? <PublicForm key={id} id={id} value={query.data} /> : <div className="form-grid">{['IPv4', 'IPv6'].map((family) => <div key={family}><h3 className="mb-4 font-medium">{family}</h3>{query.isPending && <Skeleton className="h-24 w-full" />}</div>)}</div>}</section>;
}
function PublicForm({ id, value }: { id: string; value: Public }) {
  const base = useRef(value);
  const nodes = useQuery(nodesQuery);
  const [draft, setDraft] = useState(value);
  const [text, setText] = useState({ ipv4: value.ipv4.addresses.join('\n'), ipv6: value.ipv6.addresses.join('\n') });
  const [dirty, setDirty] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [verifyError, setVerifyError] = useState<unknown>(null);
  const refresh = useMutation({ mutationFn: async () => unwrap(await api.POST('/nodes/{node_id}/public-addresses/actions', { params: { path: { node_id: id } }, body: { action: 'refresh', expected_revision: value.revision } })), onSuccess: (v) => { queryClient.setQueryData(['public-addresses', id], v); invalidateNetwork(); } });
  const body = () => ({ ...Object.fromEntries((['ipv4', 'ipv6'] as const).map((family) => [family, { mode: draft[family].mode, addresses: draft[family].mode === 'manual' ? text[family].split(/[\s,]+/).filter(Boolean) : [] }])) }) as components['schemas']['PublicAddressesPatch'];
  const save = useMutation({ mutationFn: async () => unwrap(await api.PATCH('/nodes/{node_id}/public-addresses', { params: { path: { node_id: id }, header: condition(base.current) }, body: body() })), onSuccess: (v) => { setDirty(false); setConfirm(false); queryClient.setQueryData(['public-addresses', id], v); invalidateNetwork(); }, onError: (error) => { const unknown = uncertainWrite(error); setUncertain(unknown); if (unknown) setConfirm(false); } });
  useEffect(() => { if (!dirty && !save.isPending) { base.current = value; setDraft(value); setText({ ipv4: value.ipv4.addresses.join('\n'), ipv6: value.ipv6.addresses.join('\n') }); } }, [value, dirty, save.isPending]);
  useUnsavedChanges(dirty);
  const offline = nodes.data?.items.find((n) => n.id === id)?.connected === false;
  const locked = offline || value.cached || save.isPending || uncertain;
  const verify = async () => { setVerifyError(null); try { const v = await queryClient.fetchQuery(publicQuery(id)); if (v.cached) throw new Error('节点离线，恢复连接后核对'); queryClient.setQueryData(['public-addresses', id], v); setUncertain(false); setConfirm(false); setDirty(false); save.reset(); } catch (error) { setVerifyError(error); } };
  const firstID = useId();
  return <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); setConfirm(true); }}>
    <fieldset disabled={locked} className="form-grid">{(['ipv4', 'ipv6'] as const).map((family) => <div key={family} className="min-w-0 space-y-4"><h3 className="font-medium">{family === 'ipv4' ? 'IPv4' : 'IPv6'}</h3><ChoiceField label="发现模式" value={draft[family].mode} options={[[ 'auto', '自动' ], ['manual', '手工' ]]} disabled={locked} onChange={(mode) => { setDraft({ ...draft, [family]: { ...draft[family], mode: mode as 'auto' | 'manual' } }); setDirty(true); }} />{draft[family].mode === 'manual' && <Field className="space-y-2"><FieldLabel htmlFor={`${firstID}-${family}`}>公网 IP（每行一个）</FieldLabel><Textarea id={`${firstID}-${family}`} value={text[family]} rows={3} onChange={(e) => { setText({ ...text, [family]: e.target.value }); setDirty(true); }} /></Field>}<div className="flex flex-wrap items-center gap-2"><StatusLabel state={value[family].state}>{value[family].state === 'pending' ? '待发现' : networkStates[value[family].state]}</StatusLabel><span className="text-xs text-foreground-muted">{date(value[family].updated_at)}</span></div><div className="space-y-1 break-all font-mono text-xs">{value[family].addresses.length ? value[family].addresses.map((ip) => <p key={ip}>{ip}</p>) : '—'}</div>{value[family].error && <p className="text-sm text-error-intense">{value[family].error}</p>}{value[family].observations.length > 0 && <dl className="space-y-2 text-xs">{value[family].observations.map((o) => <div key={o.source} className="flex flex-wrap gap-x-3 gap-y-1"><dt className="text-foreground-muted">{({ peer: '成员观测', ipify: '公共回显', local: '本机检测' })[o.source]}</dt><dd className="break-all">{o.address || o.error?.message || '—'}</dd></div>)}</dl>}</div>)}</fieldset>
    {value.cached && <StatusLabel state="offline">已保存数据</StatusLabel>}
    <Failure error={verifyError ?? save.error ?? refresh.error} />
    <div className="flex flex-wrap gap-2">{uncertain ? <BusyButton type="button" variant="outline" busy={queryClient.isFetching({ queryKey: ['public-addresses', id] }) > 0} onClick={() => void verify()}>核对设置</BusyButton> : <BusyButton type="submit" busy={save.isPending} disabled={!dirty || locked}>保存</BusyButton>}<BusyButton type="button" variant="outline" busy={refresh.isPending} disabled={dirty || locked} onClick={() => refresh.mutate()}>重新发现</BusyButton></div>
    <Confirm open={confirm} title="保存公网地址？" description={`将更新该节点的 IPv4、IPv6 地址设置，并更新已开启互信的节点规则。IPv4：${draft.ipv4.mode === 'auto' ? '自动发现' : text.ipv4.trim() || '清空'}；IPv6：${draft.ipv6.mode === 'auto' ? '自动发现' : text.ipv6.trim() || '清空'}。`} confirmLabel="保存" dangerous={false} busy={save.isPending} error={save.error} onCancel={() => { if (!uncertain) setConfirm(false); }} onConfirm={() => { if (!uncertain) save.mutate(); }} />
  </form>;
}
