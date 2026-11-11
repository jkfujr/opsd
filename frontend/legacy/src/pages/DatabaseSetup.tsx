import { useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Button } from '@appica/ui-react/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogBody, DialogFooter } from '@appica/ui-react/dialog';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { allPages, api, condition, RequestError, unwrap, unknownResult } from '../api/client';
import type { components } from '../api/schema.gen';
import { useSession } from '../auth';
import { queryClient } from '../state';
import { BusyButton, ChoiceField, Failure, newRequestKey, StatusLabel, TextField, useUnsavedChanges } from '../ui';
import { nodesQuery } from './Nodes';

type Definition = components['schemas']['ManagedDatabaseCluster'];
type Input = components['schemas']['DatabaseClusterInput'];
type Instance = components['schemas']['DiscoveredDatabaseInstance'];
type InstanceInput = components['schemas']['DatabaseInstanceInput'];
type Ref = components['schemas']['DatabaseRef'];

export function DatabaseSetup({ cluster, onClose }: { cluster?: Definition; onClose: () => void }) {
  const session = useSession();
  const nodes = useQuery(nodesQuery);
  const [node, setNode] = useState(session.local_node_id);
  const [draft, setDraft] = useState<Input>(cluster ? { name: cluster.name, coordinator: cluster.coordinator, cluster_id: cluster.cluster_id, members: cluster.members, proxies: cluster.proxies, workbench_url: cluster.workbench_url } : { name: '', coordinator: session.local_node_id, cluster_id: '', members: [], proxies: [], workbench_url: '' });
  const [instances, setInstances] = useState<Instance[]>([]);
  const [connection, setConnection] = useState<Instance | 'new' | null>(null);
  const [dirty, setDirty] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const key = useRef(newRequestKey());
  useUnsavedChanges(dirty);
  const discover = useMutation({ mutationFn: async () => allPages(async (cursor) => unwrap(await api.POST('/nodes/{node_id}/database-instances/discover', { params: { path: { node_id: node }, query: { cursor, limit: 100 } } }))), onSuccess: (data) => setInstances(data.items) });
  const save = useMutation({ mutationFn: async () => {
    if (cluster) {
      const current = unwrap(await api.GET('/database-clusters/{id}', { params: { path: { id: cluster.id } } }));
      if (current.revision !== cluster.revision) throw new RequestError('数据库集群已经改变，请重新读取', 412);
      return unwrap(await api.PATCH('/database-clusters/{id}', { params: { path: { id: cluster.id }, header: condition(current) }, body: draft }));
    }
    return unwrap(await api.POST('/database-clusters', { params: { header: { 'Idempotency-Key': key.current } }, body: draft }));
  }, onSuccess: () => { setDirty(false); void queryClient.invalidateQueries({ queryKey: ['databases'] }); onClose(); }, onError: (error) => { if (unknownResult(error)) setUncertain(true); } });
  const change = (value: Partial<Input>) => { setDraft({ ...draft, ...value }); setDirty(true); };
  const add = (instance: Instance) => { const ref: Ref = { node_id: node, instance_id: instance.id, name: instance.name, address: instance.address }; const field = instance.kind === 'galera' ? 'members' : 'proxies'; if (!draft[field].some((r) => r.node_id === node && r.instance_id === instance.id)) change({ [field]: [...draft[field], ref], ...(!draft.cluster_id && instance.kind === 'galera' && instance.status?.cluster_id ? { cluster_id: instance.status.cluster_id } : {}) }); };
  const nodeName = (id: string) => nodes.data?.items.find((n) => n.id === id)?.name ?? id;
  return <Dialog open onOpenChange={(open) => { if (!open && !save.isPending) onClose(); }}><DialogContent className="w-full max-w-3xl" closeLabel="关闭数据库集群表单"><DialogHeader><DialogTitle>{cluster ? '编辑数据库集群' : '接入数据库集群'}</DialogTitle></DialogHeader><DialogBody className="space-y-5 overflow-y-auto">
    <form id="database-setup" className="space-y-5" onSubmit={(event) => { event.preventDefault(); save.mutate(); }}><fieldset disabled={save.isPending || uncertain} className="space-y-5"><div className="grid gap-4 sm:grid-cols-2"><TextField label="集群名称" value={draft.name} onChange={(e) => change({ name: e.target.value })} required maxLength={128} /><ChoiceField label="协调节点" value={draft.coordinator} options={nodes.data?.items.filter((n) => !n.revoked_at).map((n) => [n.id, n.name] as const) ?? []} disabled={Boolean(cluster)} onChange={(coordinator) => change({ coordinator })} /><TextField label="Galera 集群 ID" value={draft.cluster_id} onChange={(e) => change({ cluster_id: e.target.value })} required /><TextField label="SQL 工作台 URL" type="url" value={draft.workbench_url} onChange={(e) => change({ workbench_url: e.target.value })} /></div>
      <div className="grid items-end gap-4 sm:grid-cols-[minmax(0,1fr)_auto_auto]"><ChoiceField label="节点" value={node} options={nodes.data?.items.filter((n) => !n.revoked_at).map((n) => [n.id, n.name] as const) ?? []} disabled={discover.isPending} onChange={(id) => { setNode(id); setInstances([]); discover.reset(); }} /><BusyButton type="button" variant="outline" busy={discover.isPending} onClick={() => discover.mutate()}>发现实例</BusyButton><Button type="button" variant="outline" onClick={() => setConnection('new')}>手动添加</Button></div>
      <Failure error={discover.error ?? nodes.error} />
      {instances.length > 0 && <div className="max-w-full overflow-x-auto"><Table className="min-w-128"><TableHeader><TableRow><TableHead>实例</TableHead><TableHead>连接</TableHead><TableHead>状态</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader><TableBody>{instances.map((instance) => <TableRow key={instance.id}><TableCell><span className="font-medium">{instance.name}</span><span className="mt-1 block text-xs text-foreground-muted">{instance.kind === 'galera' ? 'MariaDB Galera' : 'ProxySQL'}</span></TableCell><TableCell className="font-mono text-xs">{instance.address}</TableCell><TableCell><StatusLabel state={instance.status?.state ?? 'unknown'}>{instance.status?.state === 'ready' ? '可用' : instance.running ? '连接失败' : '已停止'}</StatusLabel>{instance.status?.error && <div className="mt-1 max-w-56 text-xs text-error-intense">{instance.status.error.message}</div>}</TableCell><TableCell><div className="flex justify-end gap-2"><Button type="button" size="sm" variant="ghost" onClick={() => setConnection(instance)}>连接信息</Button><Button type="button" size="sm" variant="outline" disabled={[...draft.members, ...draft.proxies].some((r) => r.node_id === node && r.instance_id === instance.id)} onClick={() => add(instance)}>{instance.kind === 'galera' ? '添加成员' : '添加代理'}</Button></div></TableCell></TableRow>)}</TableBody></Table></div>}
      {(['members', 'proxies'] as const).map((field) => <section key={field} className="space-y-3"><h3 className="font-semibold">{field === 'members' ? '成员' : '代理'} · {draft[field].length}</h3><ul className="divide-y divide-border rounded-md border border-border">{draft[field].map((ref) => <li key={`${ref.node_id}/${ref.instance_id}`} className="flex items-center justify-between gap-3 px-3 py-2"><span className="min-w-0 break-words text-sm">{nodeName(ref.node_id)} · {ref.name}</span><Button type="button" size="sm" variant="ghost" onClick={() => change({ [field]: draft[field].filter((r) => r !== ref) })}>移除</Button></li>)}</ul></section>)}
    </fieldset><Failure error={save.error} />{uncertain && <p role="status" className="text-sm">请刷新数据库列表，核对本次结果。</p>}</form>
    {connection && <InstanceForm node={node} instance={connection === 'new' ? undefined : connection} onClose={() => setConnection(null)} onSaved={(item) => { setInstances((items) => [...items.filter((i) => i.id !== item.id), item]); setConnection(null); }} />}
  </DialogBody><DialogFooter><Button variant="outline" disabled={save.isPending} onClick={onClose}>关闭</Button><BusyButton type="submit" form="database-setup" busy={save.isPending} disabled={uncertain || !draft.members.length}>保存</BusyButton></DialogFooter></DialogContent></Dialog>;
}

function InstanceForm({ node, instance, onClose, onSaved }: { node: string; instance?: Instance; onClose: () => void; onSaved: (item: Instance) => void }) {
	const manualID=useRef(newRequestKey());
  const [draft, setDraft] = useState<InstanceInput>({ name: instance?.name ?? '', kind: instance?.kind ?? 'galera', address: instance?.address ?? '127.0.0.1:3306', socket: instance?.socket ?? '', username: instance?.username ?? 'root', password: '', protected_users: instance?.protected_users ?? [], backup_root: instance?.backup_root ?? '' });
  const [dirty, setDirty] = useState(false); useUnsavedChanges(dirty);
  const save = useMutation({ gcTime: 0, mutationFn: async () => {
    const path = { node_id: node, instance_id: instance?.id ?? manualID.current };
    let header: { 'If-Match'?: string; 'If-None-Match'?: string } = { 'If-None-Match': '*' };
    if (instance) {
      const current = unwrap(await api.GET('/nodes/{node_id}/database-instances/{instance_id}', { params: { path } }));
      if (current.revision !== instance.revision) throw new RequestError('连接配置已经改变，请重新读取', 412);
      header = condition(current);
    }
    return unwrap(await api.PUT('/nodes/{node_id}/database-instances/{instance_id}', { params: { path, header }, body: { ...draft, password: draft.password || undefined } }));
  }, onSuccess: (item) => { setDirty(false); onSaved(item); } });
  const change = (value: Partial<InstanceInput>) => { setDraft({ ...draft, ...value }); setDirty(true); };
  return <Dialog open onOpenChange={(open) => { if (!open && !save.isPending) onClose(); }}><DialogContent className="w-full max-w-lg" closeLabel="关闭连接信息"><DialogHeader><DialogTitle>连接信息</DialogTitle></DialogHeader><DialogBody className="overflow-y-auto"><form id="database-instance" className="space-y-4" onSubmit={(event) => { event.preventDefault(); save.mutate(); }}><fieldset disabled={save.isPending} className="space-y-4"><TextField label="实例名称" required value={draft.name} onChange={(e) => change({ name: e.target.value })} /><ChoiceField label="类型" value={draft.kind} disabled={Boolean(instance)} options={[['galera', 'MariaDB Galera'], ['proxysql', 'ProxySQL']]} onChange={(kind) => change({ kind: kind as InstanceInput['kind'], address: kind === 'galera' ? '127.0.0.1:3306' : '127.0.0.1:6032', username: kind === 'galera' ? 'root' : 'admin' })} /><TextField label="本机地址" required value={draft.address} onChange={(e) => change({ address: e.target.value })} /><TextField label="Unix Socket" value={draft.socket} onChange={(e) => change({ socket: e.target.value })} /><TextField label="账号" required value={draft.username} onChange={(e) => change({ username: e.target.value })} /><TextField label="密码" type="password" autoComplete="new-password" value={draft.password} onChange={(e) => change({ password: e.target.value })} /><TextField label="受保护账号（逗号分隔）" value={draft.protected_users.join(',')} onChange={(e) => change({ protected_users: e.target.value.split(',').map((v) => v.trim()).filter(Boolean) })} />{draft.kind === 'galera' && <TextField label="备份目录" value={draft.backup_root} onChange={(e) => change({ backup_root: e.target.value })} />}</fieldset><Failure error={save.error} /></form></DialogBody><DialogFooter><Button variant="outline" disabled={save.isPending} onClick={onClose}>关闭</Button><BusyButton type="submit" form="database-instance" busy={save.isPending}>保存连接</BusyButton></DialogFooter></DialogContent></Dialog>;
}
