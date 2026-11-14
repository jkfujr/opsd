import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogBody, DialogFooter } from '@appica/ui-react/dialog';
import { Checkbox } from '@appica/ui-react/checkbox';
import { Textarea } from '@appica/ui-react/textarea';
import { api, targetNode, condition, findOperation, RequestError, unwrap, unknownResult } from '../api/client';
import type { components } from '../api/types';
import { queryClient } from '../state';
import { Button, BusyButton, ChoiceField, Failure, newRequestKey, TextField, useUnsavedChanges } from '../business-ui';
import { canWriteNode, nodesQuery } from './Nodes';
import { useOperations, invalidateOperations } from '../operations';
import { bytes } from '../monitoring';
import { CommandError } from '../commands';

type Definition = components['schemas']['ManagedDatabaseCluster'];
type Input = components['schemas']['DatabaseDeploymentInput'];
type Preview = components['schemas']['DatabaseDeploymentPreview'];
type ScanItem = components['schemas']['DatabaseScanItem'];
type InstanceInput = components['schemas']['DatabaseInstanceInput'];
export type DatabaseSetupMode = Input['mode'] | 'settings';
const labels: Record<DatabaseSetupMode, string> = { create: '新建集群', import: '接入已有集群', members: '添加节点', proxy: '添加代理', settings: '集群设置' };
const instanceKey = (item: ScanItem) => `${item.instance.node_id}/${item.instance.id}`;

export function DatabaseSetup({ cluster, mode = cluster ? 'settings' : 'create', onClose }: { cluster?: Definition; mode?: DatabaseSetupMode; onClose: () => void }) {
  if (mode === 'settings' && cluster) return <DatabaseSettings cluster={cluster} onClose={onClose}/>;
  return <DatabaseDeployment cluster={cluster} mode={mode === 'settings' ? 'create' : mode} onClose={onClose}/>;
}

function DatabaseDeployment({ cluster, mode, onClose }: { cluster?: Definition; mode: Input['mode']; onClose: () => void }) {
  const nodes = useQuery(nodesQuery), operations = useOperations();
  const [name, setName] = useState(cluster?.name ?? '');
  const [selected, setSelected] = useState<string[]>([]), [search, setSearch] = useState('');
  const [targets, setTargets] = useState<Record<string, { address: string; directory: string }>>({});
  const [options, setOptions] = useState<Input['options']>({});
  const [groupID, setGroupID] = useState(''), [proxies, setProxies] = useState<string[]>([]);
  const [credentials, setCredentials] = useState<Record<string, { username: string; password: string }>>({});
  const [review, setReview] = useState<Preview>(), [uncertain, setUncertain] = useState(false);
  const key = useRef(newRequestKey());
  const title = labels[mode];
  const names = new Map(nodes.data?.items.map(node => [node.id, node.name]) ?? []);
  const scans = useRef<ScanItem[]>([]);
  const instanceInput = (item: ScanItem): InstanceInput => ({ id: item.instance.id, node_id: item.instance.node_id, name: item.instance.name, kind: item.instance.kind, address: item.instance.address, socket: item.instance.socket, username: credentials[instanceKey(item)]?.username || item.instance.username, password: credentials[instanceKey(item)]?.password || undefined, protected_users: item.instance.protected_users ?? [] });
  const scan = useMutation({ gcTime: 0, mutationFn: async () => unwrap(await api.POST('/database-clusters/discovery', { body: { nodes: [], credentials: scans.current.filter(item => credentials[instanceKey(item)]?.password).map(instanceInput) } })), onSuccess: data => {
    scans.current = [...data.groups.flatMap(group => [...group.members, ...group.proxies]), ...data.unresolved];
    const group = data.groups.find(value => value.cluster_id === groupID) ?? data.groups[0];
    setGroupID(group?.cluster_id ?? ''); setProxies([]);
    if (!name && group) setName(group.name || 'MariaDB');
  } });
  useEffect(() => { if (mode === 'import') scan.mutate(); }, []);
  const group = scan.data?.groups.find(value => value.cluster_id === groupID);
  const preview = useMutation({ gcTime: 0, mutationFn: async () => {
    const input: Input = { mode, name, cluster_id: cluster?.id, expected_revision: cluster?.revision ?? 0, options, targets: selected.map(node_id => ({ node_id, ...targets[node_id] })), imports: mode === 'import' && group ? [...group.members, ...group.proxies.filter(item => proxies.includes(instanceKey(item)))].map(instanceInput) : [], galera_id: group?.cluster_id };
    return unwrap(await api.POST('/database-clusters/deployment-previews', { body: input }));
  }, onSuccess: value => { setReview(value); key.current = newRequestKey(); } });
  const completed = (op: components['schemas']['Operation']) => { invalidateOperations(); void queryClient.invalidateQueries({ queryKey: ['databases'] }); onClose(); operations.show(op.id); };
  const save = useMutation({ gcTime: 0, mutationFn: async () => {
    if (!review) throw new Error('请先预览变更');
    const body = { coordinator: review.coordinator, token: review.token, revision: review.revision };
    const header = { 'Idempotency-Key': key.current };
    if (mode === 'members') return unwrap(await api.POST('/database-clusters/{id}/members/actions', { ...targetNode(review.coordinator), params: { path: { id: cluster!.id }, header }, body }));
    if (mode === 'proxy') return unwrap(await api.POST('/database-clusters/{id}/proxies/actions', { ...targetNode(review.coordinator), params: { path: { id: cluster!.id }, header }, body }));
    return unwrap(await api.POST('/database-clusters/deployments', { ...targetNode(review.coordinator), params: { header }, body }));
  }, onSuccess: completed, onError: error => { if (unknownResult(error)) setUncertain(true); } });
  const locate = useMutation({ mutationFn: () => findOperation(review!.coordinator, key.current), onSuccess: op => { if (op) completed(op); else { setUncertain(false); save.reset(); } } });
  const busy = preview.isPending || save.isPending;
  const available = nodes.data?.items.filter(node => !node.revoked_at && (mode !== 'members' || !cluster?.members.some(member => member.node_id === node.id))) ?? [];
  const toggle = (id: string, checked: boolean) => setSelected(value => checked ? [...value, id] : value.filter(node => node !== id));
  const updateOptions = (value: Partial<Input['options']>) => setOptions(old => ({ ...old, ...value }));
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose(); }}><DialogContent frame={false} className="ui-modal wide w-full max-w-3xl" closeLabel={`关闭${title}`}><DialogHeader><DialogTitle>{review ? `确认${title}` : title}</DialogTitle></DialogHeader><DialogBody className="space-y-5 overflow-y-auto">
    <Failure error={nodes.error}/>
    {review ? <div className="space-y-5">
      <dl className="detail-grid text-sm"><div><dt>集群</dt><dd>{name}</dd></div><div><dt>节点</dt><dd>{review.targets.length}</dd></div><div><dt>防火墙变更</dt><dd>{review.network.length} 条</dd></div></dl>
      <ul className="divide-y divide-border">{review.targets.map((target, index) => <li key={`${target.node_id}/${index}`} className="space-y-3 py-4"><h3 className="break-words font-semibold">{names.get(target.node_id) ?? target.name}</h3><dl className="detail-grid text-sm"><div><dt>版本</dt><dd className="break-all">{target.image || '—'}</dd></div><div><dt>地址</dt><dd className="break-all">{target.address}</dd></div>{target.directory && <div><dt>目录</dt><dd className="break-all">{target.directory}</dd></div>}{target.ports.length > 0 && <div><dt>端口</dt><dd>{target.ports.join(' / ')}</dd></div>}{target.docker_state && <><div><dt>Docker</dt><dd>{target.docker_state === 'not_installed' ? '安装' : target.docker_state === 'ready' ? '已安装' : '待恢复'}</dd></div><div><dt>可用空间</dt><dd>{bytes(target.free_bytes)}</dd></div></>}</dl></li>)}</ul>
      {review.network.length > 0 && <section className="space-y-2"><h3 className="font-semibold">防火墙变更</h3><ul className="space-y-2 text-sm">{review.network.map((rule, index) => <li key={index} className="break-all">{names.get(rule.node_id) ?? rule.node_id} · {rule.source} → {rule.protocol.toUpperCase()} {rule.ports}</li>)}</ul></section>}
      {review.problems.length > 0 && <ul role="alert" className="space-y-2 text-sm text-error-intense">{review.problems.map((problem, index) => <li key={index}>{problem}</li>)}</ul>}
    </div> : <form id="database-deployment" className="space-y-5" onSubmit={event => { event.preventDefault(); preview.mutate(); }}><fieldset disabled={busy} className="space-y-5">
      {(mode === 'create' || mode === 'import') && <TextField label="集群名称" required maxLength={128} value={name} onChange={event => setName(event.target.value)}/>}
      {mode === 'import' ? <>
        <BusyButton type="button" variant="outline" busy={scan.isPending} onClick={() => scan.mutate()}>扫描在线节点</BusyButton><Failure error={scan.error}/>
        {scan.isPending && <p role="status" className="text-sm text-foreground-muted">正在扫描数据库…</p>}
        {scan.data?.problems.map((problem, index) => <p className="text-sm text-error-intense" key={index}>{problem}</p>)}
        {!!scan.data?.groups.length && <ChoiceField label="发现的集群" value={groupID} options={scan.data.groups.map(value => [value.cluster_id, `${value.name || 'MariaDB'} · ${value.members.length} 个成员${value.registered_id ? ' · 已接入' : ''}`] as const)} onChange={value => { setGroupID(value); setProxies([]); const found = scan.data!.groups.find(item => item.cluster_id === value); if (found) setName(found.name || 'MariaDB'); }}/>}
        {group && <section className="space-y-3"><h3 className="font-semibold">成员</h3><ul className="divide-y divide-border">{group.members.map(item => <li key={instanceKey(item)} className="flex flex-wrap justify-between gap-2 py-3 text-sm"><span>{names.get(item.instance.node_id) ?? item.instance.name}</span><span className="break-all text-foreground-muted">{item.status.member_address}</span></li>)}</ul>{group.proxies.length > 0 && <><h3 className="font-semibold">关联代理</h3>{group.proxies.map(item => <label key={instanceKey(item)} className="flex min-h-11 cursor-pointer items-center gap-3"><Checkbox checked={proxies.includes(instanceKey(item))} onCheckedChange={checked => setProxies(value => checked ? [...value, instanceKey(item)] : value.filter(id => id !== instanceKey(item)))}/><span className="min-w-0 break-words text-sm">{names.get(item.instance.node_id) ?? item.instance.node_id} · {item.instance.name}</span></label>)}</>}</section>}
        {scan.data && !scan.isPending && !scan.data.groups.length && !scan.data.unresolved.length && <p className="py-4 text-sm text-foreground-muted">未发现数据库实例</p>}
        {!!scan.data?.unresolved.length && <section className="space-y-4"><h3 className="font-semibold">待处理实例</h3>{scan.data.unresolved.map(item => <div key={instanceKey(item)} className="space-y-3 border-t border-border pt-4"><p className="break-words text-sm font-medium">{names.get(item.instance.node_id) ?? item.instance.node_id} · {item.instance.name}</p><p className="text-sm text-error-intense">{item.status.error?.message || (item.instance.kind === 'proxysql' ? '未匹配到数据库后端' : '未取得 Galera 集群身份')}</p><div className="grid gap-4 sm:grid-cols-2"><TextField label="管理账号" value={credentials[instanceKey(item)]?.username ?? item.instance.username} onChange={event => setCredentials(value => ({ ...value, [instanceKey(item)]: { username: event.target.value, password: value[instanceKey(item)]?.password ?? '' } }))}/><TextField label="管理密码" type="password" autoComplete="new-password" value={credentials[instanceKey(item)]?.password ?? ''} onChange={event => setCredentials(value => ({ ...value, [instanceKey(item)]: { username: value[instanceKey(item)]?.username ?? item.instance.username, password: event.target.value } }))}/></div></div>)}<BusyButton type="button" variant="outline" busy={scan.isPending} onClick={() => scan.mutate()}>重新读取</BusyButton></section>}
      </> : <>
        <section className="space-y-3"><h3 className="font-semibold">{mode === 'proxy' ? '代理节点' : '数据库节点'}</h3><TextField label="搜索节点" value={search} onChange={event => setSearch(event.target.value)}/><div className="max-h-64 overflow-y-auto divide-y divide-border">{nodes.isPending && <p role="status" className="py-4 text-sm text-foreground-muted">正在读取节点…</p>}{available.filter(node => `${node.name} ${node.address} ${node.region}`.toLowerCase().includes(search.toLowerCase())).map(node => <label key={node.id} className="flex min-h-11 cursor-pointer items-center gap-3 py-3"><Checkbox disabled={!canWriteNode(nodes, node.id)} checked={selected.includes(node.id)} onCheckedChange={checked => toggle(node.id, checked === true)}/><span className="min-w-0 flex-1 break-words text-sm">{node.name}<span className="mt-1 block text-xs text-foreground-muted">{node.region || node.address}</span></span>{!canWriteNode(nodes, node.id) && <span className="text-xs text-foreground-muted">不可用</span>}</label>)}{!nodes.isPending && !available.length && <p className="py-4 text-sm text-foreground-muted">暂无可选节点</p>}</div></section>
        <details className="space-y-4"><summary className="cursor-pointer text-sm font-medium">高级设置</summary><div className="grid gap-4 sm:grid-cols-2">{mode !== 'members' && <><TextField label="固定版本镜像" value={options.image ?? ''} onChange={event => updateOptions({ image: event.target.value })}/><TextField label="SQL 端口" type="number" min={1} max={65535} value={options.sql_port ?? ''} onChange={event => updateOptions({ sql_port: Number(event.target.value) || undefined })}/>{mode === 'proxy' ? <TextField label="管理端口" type="number" min={1} max={65535} value={options.admin_port ?? ''} onChange={event => updateOptions({ admin_port: Number(event.target.value) || undefined })}/> : ([['replication_port', '复制端口'], ['ist_port', '增量同步端口'], ['sst_port', '全量同步端口']] as const).map(([field, label]) => <TextField key={field} label={label} type="number" min={1} max={65535} value={options[field] ?? ''} onChange={event => updateOptions({ [field]: Number(event.target.value) || undefined })}/>)}</>}{mode === 'members' && <><TextField label="同步账号" value={options.sst_user ?? ''} onChange={event => updateOptions({ sst_user: event.target.value })}/><TextField label="同步密码" type="password" autoComplete="new-password" value={options.sst_password ?? ''} onChange={event => updateOptions({ sst_password: event.target.value })}/></>}</div>{selected.map(id => <section key={id} className="space-y-3 border-t border-border pt-4"><h3 className="break-words text-sm font-medium">{names.get(id) ?? id}</h3><div className="grid gap-4 sm:grid-cols-2"><TextField label="成员 IP" value={targets[id]?.address ?? ''} onChange={event => setTargets(value => ({ ...value, [id]: { directory: value[id]?.directory ?? '', address: event.target.value } }))}/><TextField label="数据库目录" value={targets[id]?.directory ?? ''} onChange={event => setTargets(value => ({ ...value, [id]: { address: value[id]?.address ?? '', directory: event.target.value } }))}/></div></section>)}{mode === 'members' && <div className="space-y-4">{[['ca.pem', '同步 CA 证书'], ['cert.pem', '同步证书'], ['key.pem', '同步私钥']].map(([file, label]) => <label key={file} className="block space-y-2 text-sm font-medium">{label}<Textarea aria-label={label} rows={3} value={options.tls?.[file] ?? ''} onChange={event => updateOptions({ tls: { ...options.tls, [file]: event.target.value } })}/></label>)}</div>}</details>
      </>}
    </fieldset></form>}
    <CommandError error={preview.error ?? save.error ?? locate.error}/>
  </DialogBody><DialogFooter><Button variant="outline" disabled={busy} onClick={onClose}>关闭</Button>{uncertain ? <BusyButton busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton> : review ? <><Button variant="outline" disabled={busy} onClick={() => { setReview(undefined); save.reset(); }}>返回修改</Button><BusyButton busy={save.isPending} disabled={review.problems.length > 0 || !review.token} onClick={() => save.mutate()}>确认{title}</BusyButton></> : <BusyButton type="submit" form="database-deployment" busy={preview.isPending} disabled={scan.isPending || !name.trim() || (mode === 'import' ? !group : !selected.length)}>预览变更</BusyButton>}</DialogFooter></DialogContent></Dialog>;
}

function DatabaseSettings({ cluster, onClose }: { cluster: Definition; onClose: () => void }) {
  const base = useRef(cluster);
  const [name, setName] = useState(cluster.name), [url, setURL] = useState(cluster.workbench_url);
  const [uncertain, setUncertain] = useState(false);
  useUnsavedChanges(name !== cluster.name || url !== cluster.workbench_url);
  const read = useMutation({ mutationFn: async () => unwrap(await api.GET('/database-clusters/{id}', { params: { path: { id: cluster.id } } })), onSuccess: value => { base.current = value; setName(value.name); setURL(value.workbench_url); setUncertain(false); save.reset(); } });
  const save = useMutation({ mutationFn: async () => {
    const current = unwrap(await api.GET('/database-clusters/{id}', { params: { path: { id: cluster.id } } }));
    if (current.revision !== base.current.revision) throw new RequestError('集群设置已经改变，请重新读取', 412);
    return unwrap(await api.PATCH('/database-clusters/{id}', { ...targetNode(cluster.coordinator), params: { path: { id: cluster.id }, header: condition(current) }, body: { name, workbench_url: url } }));
  }, onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['databases'] }); onClose(); }, onError: error => { if (unknownResult(error)) setUncertain(true); } });
  return <Dialog open onOpenChange={open => { if (!open && !save.isPending) onClose(); }}><DialogContent frame={false} className="ui-modal w-full max-w-lg" closeLabel="关闭集群设置"><DialogHeader><DialogTitle>集群设置</DialogTitle></DialogHeader><DialogBody><form id="database-settings" className="space-y-5" onSubmit={event => { event.preventDefault(); save.mutate(); }}><fieldset className="space-y-5" disabled={save.isPending || uncertain}><TextField label="集群名称" required maxLength={128} value={name} onChange={event => setName(event.target.value)}/><TextField label="SQL 工作台 URL" type="url" value={url} onChange={event => setURL(event.target.value)}/></fieldset><CommandError error={save.error} read={() => read.mutate()}/><Failure error={read.error}/></form></DialogBody><DialogFooter><Button variant="outline" disabled={save.isPending} onClick={onClose}>关闭</Button>{uncertain ? <BusyButton busy={read.isPending} onClick={() => read.mutate()}>读取当前设置</BusyButton> : <BusyButton form="database-settings" type="submit" busy={save.isPending}>保存</BusyButton>}</DialogFooter></DialogContent></Dialog>;
}
