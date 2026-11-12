import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Button } from '../business-ui';
import { Skeleton } from '@appica/ui-react/skeleton';
import { api, condition, findOperation, unwrap, unknownResult } from '../api/client';
import type { components } from '../api/types';
import { useOperations, invalidateOperations } from '../operations';
import { BusyButton, ChoiceField, Failure, FormSection, newRequestKey, TextField, useUnsavedChanges } from '../business-ui';
import { queryClient } from '../state';
import { canWriteNode, nodesQuery } from './Nodes';
import { CommandError } from '../commands';

type Config = components['schemas']['NodeConfig'];
type Operation = components['schemas']['Operation'];
type Account = components['schemas']['NodeAccount'];
const uncertainResult = (error: unknown) => unknownResult(error);
async function findRequest(id: string, key: string) {
  return findOperation(id, key);
}

export function NodeSettings({ id }: { id: string }) {
  const query = useQuery({ queryKey: ['nodes', id, 'settings'], queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{node_id}/configuration', { signal, params: { path: { node_id: id } } })) });
  return <section id="node-settings" className="panel panel-padded content-stack"><h2 className="section-title">节点设置</h2><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
    {query.data ? <ConfigForm key={id} id={id} config={query.data} readOnly={query.isError} /> : <div className="space-y-5"><div className="grid gap-5 sm:grid-cols-2">{['名称', '地域', '节点地址', '采集间隔（分钟）', '控制台', '控制台监听地址', 'OPSD 数据', '首选节点'].map((label) => <TextField key={label} label={label} loading={query.isPending} disabled value="" readOnly />)}</div><Button disabled>保存</Button></div>}
    <div className="mt-5 space-y-5"><StorageState id={id} /><NodeAccount key={id} id={id} /></div>
  </section>;
}
function ConfigForm({ id, config, readOnly }: { id: string; config: Config; readOnly: boolean }) {
  const base = useRef(config);
  const [draft, setDraft] = useState(config);
  const [dirty, setDirty] = useState(false);
  useUnsavedChanges(dirty);
  const [uncertain, setUncertain] = useState(false);
  const [accepted, setAccepted] = useState<Operation | null>(null);
  const [readError, setReadError] = useState<unknown>(null);
  const nodes = useQuery(nodesQuery);
  const operations = useOperations();
  const requestKey = useRef('');
  const save = useMutation({ mutationFn: async () => {
    requestKey.current ||= newRequestKey();
    const { revision: _revision, cached: _cached, last_synced_at: _time, network_error: _network, ...body } = draft;
    return unwrap(await api.PATCH('/nodes/{node_id}/configuration', { params: { path: { node_id: id }, header: { ...condition(base.current), 'Idempotency-Key': requestKey.current } }, body }));
  }, onSuccess: (op) => { setAccepted(op); setDirty(false); requestKey.current = ''; invalidateOperations(); operations.show(op.id); }, onError: (error) => setUncertain(uncertainResult(error)) });
  const locate = useMutation({ mutationFn: () => findRequest(id, requestKey.current), onSuccess: (op) => { setUncertain(false); requestKey.current = ''; save.reset(); if (op) { setAccepted(op); setDirty(false); operations.show(op.id); invalidateOperations(); } } });
  useEffect(() => { if (!dirty && !save.isPending) { setDraft(config); base.current = config; } }, [config, dirty, save.isPending]);
  const change = (next: Partial<Config>) => { setDraft({ ...(dirty ? draft : config), ...next }); setDirty(true); requestKey.current = ''; setAccepted(null); save.reset(); };
  const locked = readOnly || !canWriteNode(nodes, id) || config.cached || save.isPending || uncertain;
  return <>{config.network_error && <p role="alert" className="mb-4 text-sm text-error-intense">{config.network_error.message}</p>}{config.cached && <p role="status" className="mb-4 text-sm text-foreground-muted">离线数据 · {config.last_synced_at ? new Date(config.last_synced_at).toLocaleString('zh-CN') : '时间未知'}</p>}<form className="space-y-5" onSubmit={(event) => { event.preventDefault(); save.mutate(); }}>
    <fieldset disabled={locked} className="space-y-6">
      <FormSection title="基本配置"><TextField label="名称" value={draft.name} onChange={(e) => change({ name: e.target.value })} required maxLength={255} /><TextField label="地域" value={draft.region} onChange={(e) => change({ region: e.target.value })} /><ChoiceField resource label="节点地址" value={draft.address_mode || 'manual'} options={[[ 'auto', '自动公网地址' ], [ 'manual', '手工地址' ]]} disabled={locked} onChange={(value) => change({ address_mode: value as 'auto' | 'manual' })} /><TextField label="通信监听地址" value={draft.listen} readOnly /><TextField label="通信 URL" value={draft.address} readOnly={draft.address_mode === 'auto'} onChange={(e) => change({ address: e.target.value })} required /><TextField label="采集间隔（分钟）" type="number" min={1} max={1440} value={draft.interval_minutes} onChange={(e) => change({ interval_minutes: Number(e.target.value) })} required /></FormSection>
      <FormSection title="控制台"><ChoiceField label="控制台" value={String(draft.console)} options={[[ 'true', '开启' ], [ 'false', '关闭' ]]} disabled={locked} onChange={(value) => change({ console: value === 'true' })} /><TextField label="监听地址" value={draft.console_listen} onChange={(e) => change({ console_listen: e.target.value })} required /></FormSection>
      <FormSection title="OPSD 数据"><ChoiceField label="CouchDB" value={String(draft.storage.enabled)} options={[[ 'true', '启用' ], [ 'false', '停用' ]]} disabled={locked} onChange={(value) => change({ storage: { ...draft.storage, enabled: value === 'true' } })} /><ChoiceField resource label="首选节点" value={draft.preferred_storage} options={[[ '', '自动选择' ], ...(nodes.data?.items.filter((n) => n.storage_address && !n.revoked_at).map((n) => [n.id, n.name] as [string, string]) ?? [])]} disabled={locked || !nodes.data} onChange={(preferred_storage) => change({ preferred_storage })} />
      {draft.storage.enabled && <><TextField label="绑定 IP" value={draft.storage.address} onChange={(e) => change({ storage: { ...draft.storage, address: e.target.value } })} required /><TextField label="端口" type="number" min={1} max={65535} value={draft.storage.port} onChange={(e) => change({ storage: { ...draft.storage, port: Number(e.target.value) } })} required /></>}</FormSection>
    </fieldset><CommandError error={readError ?? save.error ?? locate.error} read={() => { void api.GET('/nodes/{node_id}/configuration', { params: { path: { node_id: id } } }).then(unwrap).then(value => { base.current = value; requestKey.current = ''; save.reset(); queryClient.setQueryData(['nodes', id, 'settings'], value); setReadError(null); }).catch(setReadError); }} /><div className="flex flex-wrap items-center gap-3">{uncertain ? <BusyButton type="button" busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton> : <BusyButton type="submit" busy={save.isPending} disabled={locked || !dirty}>保存</BusyButton>}{accepted && <Button type="button" variant="ghost" onClick={() => operations.show(accepted.id)}>查看操作</Button>}</div>
  </form></>;
}
function StorageState({ id }: { id: string }) {
  const query = useQuery({ queryKey: ['nodes', id, 'storage'], queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{node_id}/storage', { signal, params: { path: { node_id: id } } })), refetchInterval: 10000 });
  return <div className="min-h-16 border-t border-border pt-5"><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} /><dl className="flex flex-wrap gap-6 text-sm"><div>待上传 {query.isPending ? <Skeleton className="inline-block h-4 w-8" /> : query.data?.pending ?? '—'}</div><div>{query.isPending ? <Skeleton className="h-4 w-24" /> : !query.data ? '—' : query.data.state === 'ready' ? '已连接' : query.data.state === 'pending' ? '数据待同步' : 'OPSD 数据不可用'}</div></dl>{Boolean(query.data?.replications.length) && <ul className="mt-3 space-y-2 text-xs text-foreground-muted">{query.data?.replications.map((r) => <li key={r.id} className="break-all">{r.id} · {({ running: '同步中', pending: '等待同步', completed: '已完成', error: '同步失败', failed: '同步失败', crashing: '同步失败' } as Record<string, string>)[r.state] ?? '状态未知'}{r.error_count > 0 ? ` · 失败 ${r.error_count}` : ''}</li>)}</ul>}</div>;
}
function NodeAccount({ id }: { id: string }) {
  const nodes = useQuery(nodesQuery);
  const query = useQuery({ queryKey: ['nodes', id, 'account'], queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{node_id}/account', { signal, params: { path: { node_id: id } } })) });
  return <section className="space-y-4 border-t border-border pt-5"><h3 className="font-semibold">账号</h3><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />{query.data ? <AccountForm key={id} id={id} account={query.data} readOnly={query.isError || !canWriteNode(nodes, id)} /> : <><Button variant="outline" disabled>查看密码</Button><div className="grid gap-4 sm:grid-cols-2">{['账号', '登录入口', '新密码'].map((label) => <TextField key={label} label={label} loading={query.isPending} disabled value="" readOnly />)}<div className="flex items-end"><Button disabled>保存账号</Button></div></div></>}</section>;
}
function AccountForm({ id, account, readOnly }: { id: string; account: Account; readOnly: boolean }) {
  const base = useRef(account);
  const [draft, setDraft] = useState({ ...account, password: '' });
  const [dirty, setDirty] = useState(false);
  useUnsavedChanges(dirty);
  const [uncertain, setUncertain] = useState(false);
  const [revealed, setRevealed] = useState<components['schemas']['NodePassword'] | null>(null);
  const read = useMutation({ mutationFn: async () => unwrap(await api.GET('/nodes/{node_id}/account', { params: { path: { node_id: id } } })), onSuccess: value => { base.current = value; key.current = ''; reset.reset(); queryClient.setQueryData(['nodes', id, 'account'], value); } });
  const key = useRef('');
  const operations = useOperations();
  const reveal = useMutation({ gcTime: 0, mutationFn: async () => unwrap(await api.POST('/nodes/{node_id}/account/actions', { params: { path: { node_id: id } }, body: { action: 'reveal_password', expected_revision: account.revision } })), onSuccess: setRevealed });
  const reset = useMutation({ mutationFn: async () => {
    key.current ||= newRequestKey();
    return unwrap(await api.PATCH('/nodes/{node_id}/account', { params: { path: { node_id: id }, header: { ...condition(base.current), 'Idempotency-Key': key.current } }, body: { ...(draft.password ? { password: draft.password } : {}), username: draft.username, entrance: draft.entrance } }));
  }, onSuccess: (op) => { setDirty(false); setRevealed(null); key.current = ''; operations.show(op.id); invalidateOperations(); }, onError: (error) => setUncertain(uncertainResult(error)) });
  const locate = useMutation({ mutationFn: () => findRequest(id, key.current), onSuccess: (op) => { setUncertain(false); key.current = ''; reset.reset(); if (op) { setDirty(false); setRevealed(null); operations.show(op.id); invalidateOperations(); } } });
  useEffect(() => { if (!dirty && !reset.isPending) { setDraft({ ...account, password: '' }); base.current = account; } }, [account, dirty, reset.isPending]);
  const change = (next: Partial<typeof draft>) => { setDraft({ ...(dirty ? draft : { ...account, password: '' }), ...next }); setDirty(true); key.current = ''; reset.reset(); };
  return <><Failure error={reveal.error ?? read.error}/><CommandError error={reset.error ?? locate.error} read={() => read.mutate()}/>
    <div className="flex flex-wrap items-end gap-3"><BusyButton type="button" variant="outline" busy={reveal.isPending} disabled={readOnly || account.cached} onClick={() => revealed ? setRevealed(null) : reveal.mutate()}>{revealed ? '隐藏密码' : '查看密码'}</BusyButton>{revealed && <TextField label="密码" value={revealed.password} readOnly autoComplete="off" />}</div>
    <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); reset.mutate(); }}><fieldset disabled={readOnly || account.cached || reset.isPending || uncertain} className="grid min-w-0 gap-4 sm:grid-cols-2"><TextField label="账号" value={draft.username} onChange={(e) => change({ username: e.target.value })} required /><TextField label="登录入口" value={draft.entrance} onChange={(e) => change({ entrance: e.target.value })} required /><TextField label="新密码" value={draft.password} type="password" autoComplete="new-password" minLength={8} maxLength={1024} onChange={(e) => change({ password: e.target.value })} /></fieldset>{uncertain ? <BusyButton type="button" busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton> : <BusyButton type="submit" busy={reset.isPending} disabled={readOnly || account.cached || !dirty}>保存账号</BusyButton>}</form>
  </>;
}
