import { useEffect, useId, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Button, buttonVariants } from '@appica/ui-react/button';
import { Switch } from '@appica/ui-react/switch';
import { Skeleton } from '@appica/ui-react/skeleton';
import { Field, FieldLabel } from '@appica/ui-react/field';
import { Textarea } from '@appica/ui-react/textarea';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle, DrawerBody } from '@appica/ui-react/drawer';
import { api, RequestError, unwrap } from './api/client';
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
  removing: '待撤除', saved: '已保存', confirmed: '已确认', conflict: '地址冲突',
};
export const trustPolicyQuery = { queryKey: ['peer-trust', 'policy'], queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/peer-trust', { signal })) };
export const trustRulesQuery = (id: string) => ({ queryKey: ['peer-trust', id], retry: false, queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/nodes/{id}/firewall/peer-trust', { signal, params: { path: { id } } })), refetchInterval: 5000 });
const uncertainWrite = (error: unknown) => !(error instanceof RequestError && [403, 404, 409, 422].includes(error.status));
function invalidateNetwork() { void queryClient.invalidateQueries({ queryKey: ['peer-trust'] }); void queryClient.invalidateQueries({ queryKey: ['public-addresses'] }); }

export function FirewallSettings() {
  const [search] = useSearchParams();
  const policy = useQuery(trustPolicyQuery);
  const status = useQuery({ queryKey: ['peer-trust', 'status'], queryFn: async ({ signal }) => unwrap(await api.GET('/peer-trust/status', { signal })), refetchInterval: 5000 });
  const nodes = useQuery(nodesQuery);
  const [change, setChange] = useState<{ enabled: boolean; previous: Policy } | null>(null);
  const [uncertain, setUncertain] = useState(false);
  const save = useMutation({ mutationFn: async () => unwrap(await api.PATCH('/peer-trust', { params: { header: { 'X-CSRF-Token': '' } }, body: { enabled: change!.enabled, expected_version: change!.previous.version } })), onSuccess: (value) => { queryClient.setQueryData(['peer-trust', 'policy'], value); setChange(null); invalidateNetwork(); }, onError: (error) => { const unknown = uncertainWrite(error); setUncertain(unknown); if (unknown) setChange(null); } });
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
    <div className="mt-6 overflow-x-auto" aria-busy={status.isPending}><Table className="min-w-128" hoverableRows><TableHeader><TableRow><TableHead>节点</TableHead><TableHead>互信状态</TableHead><TableHead>检查时间</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader><TableBody>
      {status.isPending ? <LoadingRows columns={4} /> : status.data?.nodes.map((n) => <TableRow key={n.node_id}><TableCell className="max-w-56 break-words">{names.get(n.node_id) ?? n.node_id}</TableCell><TableCell><StatusLabel state={n.state}>{networkStates[n.state] ?? n.state}</StatusLabel>{n.results.find((r) => r.error)?.error && <p className="mt-1 max-w-80 break-words text-xs text-error-intense">{n.results.find((r) => r.error)?.error?.message}</p>}</TableCell><TableCell className="whitespace-nowrap text-xs text-foreground-muted">{date(n.checked_at)}</TableCell><TableCell className="text-right"><Link to={`/firewall?node=${n.node_id}&type=trust`} className={buttonVariants({ variant: 'ghost', size: 'sm' })}>查看规则</Link></TableCell></TableRow>)}
      {status.data?.nodes.length === 0 && !status.isError && <TableRow><TableCell colSpan={4} className="py-8 text-center text-foreground-muted">暂无节点</TableCell></TableRow>}
    </TableBody></Table></div>
    <Confirm open={change !== null} title={change?.enabled ? '开启集群互信？' : '关闭集群互信？'} description={change?.enabled ? `为 ${count} 个节点放行全部成员公网 IP 的主机入站流量，包含本机。已有相同的全协议 IP 放行规则将转交互信管理。共享公网 IP 的其他来源也在放行范围内。` : `撤除 ${count} 个节点的 OPSD 互信规则，包含已从普通 IP 规则转交的条目。离线节点恢复连接后继续撤除。`} confirmLabel={change?.enabled ? '开启互信' : '关闭互信'} dangerous={false} busy={save.isPending} error={save.error} onCancel={() => { if (!uncertain) setChange(null); }} onConfirm={() => { if (!uncertain) save.mutate(); }} />
  </section>;
}

export function TrustRulesTable({ data, family, loading, error }: { data?: Trust; family: string; loading: boolean; error: unknown }) {
  const [detail, setDetail] = useState<string | null>(null);
  const operations = useOperations();
  const rows = data?.rules.filter((r) => !family || r.family === family) ?? [];
  const failures = data?.results.filter((r) => r.error && (!family || r.family === family)) ?? [];
  return <>{data?.migration && data.migration.state !== 'in_sync' && <div className="mb-3 flex flex-wrap items-center gap-3"><StatusLabel state={data.migration.state}>{data.migration.state === 'failed' ? '迁移失败' : '迁移中'}</StatusLabel>{data.migration.error && <span role="alert" className="text-sm text-error-intense">{data.migration.error.message}</span>}<Button variant="ghost" size="sm" onClick={() => operations.show(data.migration!.operation_id)}>查看操作</Button></div>}{failures.map((r) => <p key={`${r.family}/${r.scope}`} role="alert" className="mb-3 text-sm text-error-intense">{r.family === 'ipv4' ? 'IPv4' : 'IPv6'} · {r.scope === 'permanent' ? '永久配置' : '运行配置'} · {r.error?.message}</p>)}<div className="max-w-full overflow-x-auto" aria-busy={loading}><Table className="min-w-200" hoverableRows><TableHeader><TableRow><TableHead>节点</TableHead><TableHead>来源 IP / CIDR</TableHead><TableHead>地址族</TableHead><TableHead>动作</TableHead><TableHead>状态</TableHead><TableHead>检查时间</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader><TableBody>
    {loading ? <LoadingRows columns={7} /> : rows.map((r, index) => <TableRow key={`${r.family}/${r.address ?? r.members[0]?.id}/${index}`}><TableCell className="max-w-56 break-words">{r.members.map((m) => <Link key={m.id} to={`/nodes/${m.id}`} className="block text-primary hover:underline">{m.name || m.id}</Link>)}{r.members.length === 0 && '已移除节点'}</TableCell><TableCell className="max-w-56 break-all font-mono text-xs">{r.address ? <CopyAddress value={`${r.address}/${r.family === 'ipv4' ? 32 : 128}`} /> : '—'}</TableCell><TableCell>{r.family === 'ipv4' ? 'IPv4' : 'IPv6'}</TableCell><TableCell>允许</TableCell><TableCell><StatusLabel state={r.state}>{r.address ? networkStates[r.state] ?? r.state : r.state === 'pending' ? '待发现' : networkStates[r.state] ?? r.state}</StatusLabel>{r.error && <p className="mt-1 max-w-64 break-words text-xs text-error-intense">{r.error}</p>}</TableCell><TableCell className="whitespace-nowrap text-xs text-foreground-muted">{date(data?.checked_at)}</TableCell><TableCell className="text-right"><Button variant="ghost" size="sm" onClick={() => setDetail(`${r.family}/${index}`)}>查看</Button></TableCell></TableRow>)}
    {!loading && !error && !failures.length && rows.length === 0 && <TableRow><TableCell colSpan={7} className="py-8 text-center text-foreground-muted">暂无互信规则</TableCell></TableRow>}
  </TableBody></Table></div><Drawer side="right" open={detail !== null} onOpenChange={(open) => { if (!open) setDetail(null); }}><DrawerContent frame={false} closeLabel="关闭应用状态" className="h-full w-full max-w-lg"><DrawerHeader><DrawerTitle>应用状态</DrawerTitle></DrawerHeader><DrawerBody className="space-y-4 overflow-y-auto">{data?.results.filter((r) => r.family === detail?.split('/')[0]).map((r) => <div key={`${r.family}/${r.scope}`} className="flex flex-wrap items-center gap-3 text-sm"><span>{r.scope === 'permanent' ? '永久配置' : '运行配置'}</span><StatusLabel state={r.state}>{networkStates[r.state] ?? r.state}</StatusLabel>{r.error && <span className="text-error-intense">{r.error.message}</span>}</div>)}</DrawerBody></DrawerContent></Drawer></>;
}

const publicQuery = (id: string) => ({ queryKey: ['public-addresses', id], retry: false, queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/nodes/{id}/public-addresses', { signal, params: { path: { id } } })) });
export function NodePublicAddresses({ id }: { id: string }) {
  const query = useQuery(publicQuery(id));
  return <section id="public-addresses" className="panel"><h2 className="section-title">公网地址</h2><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />{query.data ? <PublicForm key={id} id={id} value={query.data} /> : <div className="form-grid">{['IPv4', 'IPv6'].map((family) => <div key={family}><h3 className="mb-4 font-medium">{family}</h3>{query.isPending && <Skeleton className="h-24 w-full" />}</div>)}</div>}</section>;
}
function PublicForm({ id, value }: { id: string; value: Public }) {
  const nodes = useQuery(nodesQuery);
  const [draft, setDraft] = useState(value);
  const [text, setText] = useState({ ipv4: value.ipv4.addresses.join('\n'), ipv6: value.ipv6.addresses.join('\n') });
  const [dirty, setDirty] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [verifyError, setVerifyError] = useState<unknown>(null);
  const refresh = useMutation({ mutationFn: async () => unwrap(await api.POST('/nodes/{id}/public-addresses/refresh', { params: { path: { id }, header: { 'X-CSRF-Token': '' } } })), onSuccess: (v) => { queryClient.setQueryData(['public-addresses', id], v); invalidateNetwork(); } });
  const body = () => ({ expected_revision: draft.revision, ...Object.fromEntries((['ipv4', 'ipv6'] as const).map((family) => [family, { mode: draft[family].mode, addresses: draft[family].mode === 'manual' ? text[family].split(/[\s,]+/).filter(Boolean) : [] }])) }) as components['schemas']['PublicAddressPatch'];
  const save = useMutation({ mutationFn: async () => unwrap(await api.PATCH('/nodes/{id}/public-addresses', { params: { path: { id }, header: { 'X-CSRF-Token': '' } }, body: body() })), onSuccess: (v) => { setDirty(false); setConfirm(false); queryClient.setQueryData(['public-addresses', id], v); invalidateNetwork(); }, onError: (error) => { const unknown = uncertainWrite(error); setUncertain(unknown); if (unknown) setConfirm(false); } });
  useEffect(() => { if (!dirty && !save.isPending) { setDraft(value); setText({ ipv4: value.ipv4.addresses.join('\n'), ipv6: value.ipv6.addresses.join('\n') }); } }, [value, dirty, save.isPending]);
  useUnsavedChanges(dirty);
  const offline = nodes.data?.items.find((n) => n.id === id)?.connected === false;
  const locked = offline || value.cached || save.isPending || uncertain;
  const verify = async () => { setVerifyError(null); try { const v = await queryClient.fetchQuery(publicQuery(id)); if (v.cached) throw new Error('节点离线，恢复连接后核对'); queryClient.setQueryData(['public-addresses', id], v); setUncertain(false); setConfirm(false); setDirty(false); save.reset(); } catch (error) { setVerifyError(error); } };
  const firstID = useId();
  return <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); setConfirm(true); }}>
    <fieldset disabled={locked} className="form-grid">{(['ipv4', 'ipv6'] as const).map((family) => <div key={family} className="min-w-0 space-y-4"><h3 className="font-medium">{family === 'ipv4' ? 'IPv4' : 'IPv6'}</h3><ChoiceField label="发现模式" value={draft[family].mode} options={[[ 'auto', '自动' ], ['manual', '手工' ]]} disabled={locked} onChange={(mode) => { setDraft({ ...draft, [family]: { ...draft[family], mode: mode as 'auto' | 'manual' } }); setDirty(true); }} />{draft[family].mode === 'manual' && <Field className="space-y-2"><FieldLabel htmlFor={`${firstID}-${family}`}>公网 IP（每行一个）</FieldLabel><Textarea id={`${firstID}-${family}`} value={text[family]} rows={3} onChange={(e) => { setText({ ...text, [family]: e.target.value }); setDirty(true); }} /></Field>}<div className="flex flex-wrap items-center gap-2"><StatusLabel state={value[family].state}>{value[family].state === 'pending' ? '待发现' : networkStates[value[family].state]}</StatusLabel><span className="text-xs text-foreground-muted">{date(value[family].updated_at)}</span></div><div className="space-y-1 break-all font-mono text-xs">{value[family].addresses.length ? value[family].addresses.map((ip) => <p key={ip}>{ip}</p>) : '—'}</div>{value[family].error && <p className="text-sm text-error-intense">{value[family].error}</p>}{value[family].observations.length > 0 && <dl className="space-y-2 text-xs">{value[family].observations.map((o) => <div key={o.source} className="flex flex-wrap gap-x-3 gap-y-1"><dt className="text-foreground-muted">{o.source === 'peer' ? '成员观测' : '公共回显'}</dt><dd className="break-all">{o.address || o.error?.message || '—'}</dd></div>)}</dl>}</div>)}</fieldset>
    {value.cached && <StatusLabel state="offline">已保存数据</StatusLabel>}
    <Failure error={verifyError ?? save.error ?? refresh.error} />
    <div className="flex flex-wrap gap-2">{uncertain ? <BusyButton type="button" variant="outline" busy={queryClient.isFetching({ queryKey: ['public-addresses', id] }) > 0} onClick={() => void verify()}>核对设置</BusyButton> : <BusyButton type="submit" busy={save.isPending} disabled={!dirty || locked}>保存</BusyButton>}<BusyButton type="button" variant="outline" busy={refresh.isPending} disabled={dirty || locked} onClick={() => refresh.mutate()}>重新发现</BusyButton></div>
    <Confirm open={confirm} title="保存公网地址？" description={`将更新该节点的 IPv4、IPv6 地址设置，并更新已开启互信的节点规则。IPv4：${draft.ipv4.mode === 'auto' ? '自动发现' : text.ipv4.trim() || '清空'}；IPv6：${draft.ipv6.mode === 'auto' ? '自动发现' : text.ipv6.trim() || '清空'}。`} confirmLabel="保存" dangerous={false} busy={save.isPending} error={save.error} onCancel={() => { if (!uncertain) setConfirm(false); }} onConfirm={() => { if (!uncertain) save.mutate(); }} />
  </form>;
}
