import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Button } from '@appica/ui-react/button';
import { Skeleton } from '@appica/ui-react/skeleton';
import { api, RequestError, unwrap } from '../api/client';
import type { components } from '../api/schema.gen';
import { useOperations, invalidateOperations } from '../operations';
import { BusyButton, ChoiceField, Failure, FormSection, newRequestKey, TextField, useUnsavedChanges } from '../ui';
import { nodesQuery } from './Nodes';

type Config = components['schemas']['NodeConfig'];
type Operation = components['schemas']['Operation'];
type Account = components['schemas']['NodeAccount'];
const uncertainResult = (error: unknown) => !(error instanceof RequestError && [403, 409, 422].includes(error.status));
async function findRequest(id: string, key: string) {
  const result = unwrap(await api.GET('/operations', { params: { query: { node_id: id, idempotency_key: key } } }));
  if (result.unavailable?.length) throw new Error('节点暂不可达');
  return result.items[0];
}

export function NodeSettings({ id }: { id: string }) {
  const query = useQuery({ queryKey: ['nodes', id, 'settings'], queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{id}/settings', { signal, params: { path: { id } } })) });
  return <section id="node-settings" className="panel"><h2 className="section-title">节点设置</h2><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
    {query.data ? <ConfigForm key={id} id={id} config={query.data} /> : <div className="space-y-5"><div className="grid gap-5 sm:grid-cols-2">{['名称', '地域', '节点地址', '采集间隔（分钟）', '控制台', '控制台监听地址', '数据库', '首选数据库'].map((label) => <TextField key={label} label={label} loading={query.isPending} disabled value="" readOnly />)}</div><Button disabled>保存</Button></div>}
    <div className="mt-5 space-y-5"><StorageState id={id} /><NodeAccount key={id} id={id} /></div>
  </section>;
}
function ConfigForm({ id, config }: { id: string; config: Config }) {
  const [draft, setDraft] = useState(config);
  const [dirty, setDirty] = useState(false);
  useUnsavedChanges(dirty);
  const [uncertain, setUncertain] = useState(false);
  const [accepted, setAccepted] = useState<Operation | null>(null);
  const nodes = useQuery(nodesQuery);
  const operations = useOperations();
  const requestKey = useRef('');
  const save = useMutation({ mutationFn: async () => {
    requestKey.current ||= newRequestKey();
    return unwrap(await api.PATCH('/nodes/{id}/settings', { params: { path: { id }, header: { 'Idempotency-Key': requestKey.current, 'X-CSRF-Token': '' } }, body: draft }));
  }, onSuccess: (op) => { setAccepted(op); setDirty(false); requestKey.current = ''; invalidateOperations(); operations.show(op.id); }, onError: (error) => setUncertain(uncertainResult(error)) });
  const locate = useMutation({ mutationFn: () => findRequest(id, requestKey.current), onSuccess: (op) => { setUncertain(false); requestKey.current = ''; save.reset(); if (op) { setAccepted(op); setDirty(false); operations.show(op.id); invalidateOperations(); } } });
  useEffect(() => { if (!dirty && !save.isPending) setDraft(config); }, [config, dirty, save.isPending]);
  const change = (next: Partial<Config>) => { setDraft({ ...(dirty ? draft : config), ...next }); setDirty(true); requestKey.current = ''; setAccepted(null); save.reset(); };
  const locked = config.cached || save.isPending || uncertain;
  return <>{config.cached && <p role="status" className="mb-4 text-sm text-foreground-muted">离线数据 · {config.last_synced_at ? new Date(config.last_synced_at).toLocaleString('zh-CN') : '时间未知'}</p>}<form className="space-y-5" onSubmit={(event) => { event.preventDefault(); save.mutate(); }}>
    <fieldset disabled={locked} className="space-y-6">
      <FormSection title="基本配置"><TextField label="名称" value={draft.name} onChange={(e) => change({ name: e.target.value })} required maxLength={255} /><TextField label="地域" value={draft.region} onChange={(e) => change({ region: e.target.value })} /><ChoiceField label="节点地址" value={draft.address_mode || 'manual'} options={[[ 'auto', '自动公网地址' ], [ 'manual', '手工地址' ]]} disabled={locked} onChange={(value) => change({ address_mode: value as 'auto' | 'manual' })} /><TextField label="通信监听地址" value={draft.listen} readOnly /><TextField label="通信 URL" value={draft.address} readOnly={draft.address_mode === 'auto'} onChange={(e) => change({ address: e.target.value })} required /><TextField label="采集间隔（分钟）" type="number" min={1} max={1440} value={draft.interval_minutes} onChange={(e) => change({ interval_minutes: Number(e.target.value) })} required /></FormSection>
      <FormSection title="控制台"><ChoiceField label="控制台" value={String(draft.console)} options={[[ 'true', '开启' ], [ 'false', '关闭' ]]} disabled={locked} onChange={(value) => change({ console: value === 'true' })} /><TextField label="监听地址" value={draft.console_listen} onChange={(e) => change({ console_listen: e.target.value })} required /></FormSection>
      <FormSection title="数据库"><ChoiceField label="存储角色" value={String(draft.storage.enabled)} options={[[ 'true', '启用' ], [ 'false', '停用' ]]} disabled={locked} onChange={(value) => change({ storage: { ...draft.storage, enabled: value === 'true' } })} /><ChoiceField label="首选数据库" value={draft.preferred_storage} options={[[ '', '自动选择' ], ...(nodes.data?.items.filter((n) => n.storage_address && !n.revoked_at).map((n) => [n.id, n.name] as [string, string]) ?? [])]} disabled={locked || !nodes.data} onChange={(preferred_storage) => change({ preferred_storage })} />
      {draft.storage.enabled && <><TextField label="绑定 IP" value={draft.storage.address} onChange={(e) => change({ storage: { ...draft.storage, address: e.target.value } })} required /><TextField label="端口" type="number" min={1} max={65535} value={draft.storage.port} onChange={(e) => change({ storage: { ...draft.storage, port: Number(e.target.value) } })} required /></>}</FormSection>
    </fieldset><Failure error={save.error ?? locate.error} /><div className="flex flex-wrap items-center gap-3">{uncertain ? <BusyButton type="button" busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton> : <BusyButton type="submit" busy={save.isPending} disabled={config.cached || !dirty}>保存</BusyButton>}{accepted && <Button type="button" variant="ghost" onClick={() => operations.show(accepted.id)}>查看操作</Button>}</div>
  </form></>;
}
function StorageState({ id }: { id: string }) {
  const query = useQuery({ queryKey: ['nodes', id, 'storage'], queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{id}/storage', { signal, params: { path: { id } } })), refetchInterval: 10000 });
  return <div className="min-h-16 border-t border-border pt-5"><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} /><dl className="flex flex-wrap gap-6 text-sm"><div>待上传 {query.isPending ? <Skeleton className="inline-block h-4 w-8" /> : query.data?.pending ?? '—'}</div><div>{query.isPending ? <Skeleton className="h-4 w-24" /> : !query.data ? '—' : query.data.state === 'ready' ? '已连接' : query.data.state === 'pending' ? '数据待同步' : '数据库不可用'}</div></dl>{Boolean(query.data?.replications.length) && <ul className="mt-3 space-y-2 text-xs text-foreground-muted">{query.data?.replications.map((r) => <li key={r.id} className="break-all">{r.id} · {({ running: '同步中', pending: '等待同步', completed: '已完成', error: '同步失败', failed: '同步失败', crashing: '同步失败' } as Record<string, string>)[r.state] ?? '状态未知'}{r.error_count > 0 ? ` · 失败 ${r.error_count}` : ''}</li>)}</ul>}</div>;
}
function NodeAccount({ id }: { id: string }) {
  const query = useQuery({ queryKey: ['nodes', id, 'account'], queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{id}/account', { signal, params: { path: { id } } })) });
  return <section className="space-y-4 border-t border-border pt-5"><h3 className="font-semibold">账号</h3><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />{query.data ? <AccountForm key={id} id={id} account={query.data} /> : <><Button variant="outline" disabled>查看密码</Button><div className="grid gap-4 sm:grid-cols-2">{['账号', '登录入口', '新密码'].map((label) => <TextField key={label} label={label} loading={query.isPending} disabled value="" readOnly />)}<div className="flex items-end"><Button disabled>保存账号</Button></div></div></>}</section>;
}
function AccountForm({ id, account }: { id: string; account: Account }) {
  const [draft, setDraft] = useState({ ...account, password: '' });
  const [dirty, setDirty] = useState(false);
  useUnsavedChanges(dirty);
  const [uncertain, setUncertain] = useState(false);
  const [revealed, setRevealed] = useState<components['schemas']['NodePassword'] | null>(null);
  const key = useRef('');
  const operations = useOperations();
  const reveal = useMutation({ gcTime: 0, mutationFn: async () => unwrap(await api.POST('/nodes/{id}/account/password', { params: { path: { id }, header: { 'X-CSRF-Token': '' } } })), onSuccess: setRevealed });
  const reset = useMutation({ mutationFn: async () => {
    key.current ||= newRequestKey();
    return unwrap(await api.PATCH('/nodes/{id}/account', { params: { path: { id }, header: { 'Idempotency-Key': key.current, 'X-CSRF-Token': '' } }, body: { password: draft.password, revision: draft.revision, username: draft.username, entrance: draft.entrance } }));
  }, onSuccess: (op) => { setDirty(false); setRevealed(null); key.current = ''; operations.show(op.id); invalidateOperations(); }, onError: (error) => setUncertain(uncertainResult(error)) });
  const locate = useMutation({ mutationFn: () => findRequest(id, key.current), onSuccess: (op) => { setUncertain(false); key.current = ''; reset.reset(); if (op) { setDirty(false); setRevealed(null); operations.show(op.id); invalidateOperations(); } } });
  useEffect(() => { if (!dirty && !reset.isPending) setDraft({ ...account, password: '' }); }, [account, dirty, reset.isPending]);
  const change = (next: Partial<typeof draft>) => { setDraft({ ...(dirty ? draft : { ...account, password: '' }), ...next }); setDirty(true); key.current = ''; reset.reset(); };
  return <><Failure error={reveal.error ?? reset.error ?? locate.error} />
    <div className="flex flex-wrap items-end gap-3"><BusyButton type="button" variant="outline" busy={reveal.isPending} disabled={account.cached} onClick={() => revealed ? setRevealed(null) : reveal.mutate()}>{revealed ? '隐藏密码' : '查看密码'}</BusyButton>{revealed && <TextField label="密码" value={revealed.password} readOnly autoComplete="off" />}</div>
    <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); reset.mutate(); }}><fieldset disabled={account.cached || reset.isPending || uncertain} className="grid min-w-0 gap-4 sm:grid-cols-2"><TextField label="账号" value={draft.username} onChange={(e) => change({ username: e.target.value })} required /><TextField label="登录入口" value={draft.entrance} onChange={(e) => change({ entrance: e.target.value })} required /><TextField label="新密码" value={draft.password} type="password" autoComplete="new-password" minLength={8} maxLength={1024} onChange={(e) => change({ password: e.target.value })} /></fieldset>{uncertain ? <BusyButton type="button" busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton> : <BusyButton type="submit" busy={reset.isPending} disabled={account.cached || !dirty}>保存账号</BusyButton>}</form>
  </>;
}
