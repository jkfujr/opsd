import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@appica/ui-react/dialog';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@appica/ui-react/table';
import { api, findOperation, targetNode, unwrap, unknownResult } from './api/client';
import type { components } from './api/types';
import { queryClient } from './state';
import { useOperations, invalidateOperations } from './operations';
import { BusyButton, Button, ChoiceField, CommandError, date, Failure, newRequestKey, StatusLabel, TextField } from './business-ui';
import { usePageVisible } from './read-polling';

type Cluster = components['schemas']['ManagedDatabaseCluster'];
type Member = components['schemas']['ManagedDatabaseMember'];
type Preview = components['schemas']['ProxyRoutingPreview'];
type Backend = components['schemas']['ProxyBackend'];
const address = (value?: components['schemas']['ProxyEndpoint']) => value ? `${value.host.includes(':') ? `[${value.host}]` : value.host}:${value.port}` : '—';
const states: Record<string, string> = { unmanaged: '未接管', ready: '已应用', paused: '已暂停', pending: '待应用', applying: '应用中', conflict: '需核对', error: '读取失败', offline: '节点离线' };

export function ProxyRoutingPanel({ cluster, members, proxies, names, writable, active }: { cluster: Cluster; members: Member[]; proxies: Member[]; names: Map<string, string>; writable: boolean; active: boolean }) {
  const visible = usePageVisible(), [editing, setEditing] = useState(false), operations = useOperations();
  const query = useQuery({ queryKey: ['databases', cluster.id, 'proxy-routing'], enabled: active, queryFn: async ({ signal }) => unwrap(await api.GET('/database-clusters/{id}/proxy-routing', { signal, params: { path: { id: cluster.id } } })), refetchInterval: active && visible ? 30000 : false });
  const policy = query.data?.policy ?? cluster.proxy_routing;
  return <section className="content-stack"><div className="flex flex-wrap items-center justify-between gap-3"><h3 className="font-semibold">受管路由</h3><Button variant="outline" disabled={!writable || !proxies.length || query.isError} onClick={() => setEditing(true)}>配置路由</Button></div><Failure error={query.error} retry={() => void query.refetch()}/>
    {query.isPending ? <p role="status" className="text-sm text-foreground-muted">正在读取路由状态…</p> : query.data?.proxies.map(proxy => <section key={`${proxy.node_id}/${proxy.instance_id}`} className="space-y-3"><div className="flex flex-wrap items-center gap-x-4 gap-y-2"><strong>{names.get(proxy.node_id) ?? proxy.node_id}</strong><StatusLabel state={proxy.state === 'conflict' ? 'failed' : proxy.state}>{states[proxy.state]}</StatusLabel><span className="text-sm">写入：{address(proxy.current_writer)}</span><span className="text-xs text-foreground-muted">{date(proxy.checked_at)}</span>{proxy.operation_id && <Button size="sm" variant="ghost" onClick={() => operations.show(proxy.operation_id)}>查看操作</Button>}</div>{proxy.error && <p role="alert" className="text-sm text-error-intense">{proxy.error.message}</p>}{proxy.entries.length > 0 && <div className="max-w-full overflow-x-auto"><Table className="min-w-144"><TableHeader><TableRow><TableHead>顺序</TableHead><TableHead>数据库成员</TableHead><TableHead>SQL 地址</TableHead><TableHead>排序依据</TableHead><TableHead className="text-right">延迟（ms）</TableHead></TableRow></TableHeader><TableBody>{proxy.entries.map(entry => <TableRow key={`${entry.node_id}/${entry.instance_id}`}><TableCell>{entry.rank}</TableCell><TableCell>{names.get(entry.node_id) ?? entry.node_id}</TableCell><TableCell className="font-mono text-xs">{address(entry)}</TableCell><TableCell>{entry.local ? '本机优先' : entry.latency_us === undefined ? '保留上次顺序' : '持续延迟'}</TableCell><TableCell className="text-right tabular-nums">{entry.latency_us === undefined ? '—' : (entry.latency_us / 1000).toFixed(2)}</TableCell></TableRow>)}</TableBody></Table></div>}</section>)}
    {policy && <ProxyConnectionBudget policy={policy} members={members} proxies={proxies} names={names}/>}
    {editing && <ProxyRoutingEditor key={cluster.id} cluster={cluster} policy={policy} names={names} onClose={() => setEditing(false)}/>}
  </section>;
}

function ProxyConnectionBudget({ policy, members, proxies, names }: { policy: components['schemas']['ProxyRoutingPolicy']; members: Member[]; proxies: Member[]; names: Map<string, string> }) {
  return <section className="space-y-3"><h3 className="font-medium">连接预算</h3><div className="max-w-full overflow-x-auto"><Table className="min-w-144"><TableHeader><TableRow><TableHead>数据库成员</TableHead><TableHead className="text-right">代理上限合计</TableHead><TableHead className="text-right">数据库上限</TableHead><TableHead className="text-right">当前连接</TableHead></TableRow></TableHeader><TableBody>{policy.backends.map(backend => {
    const member = members.find(value => value.node_id === backend.node_id && value.instance_id === backend.instance_id);
    const limits = proxies.map(proxy => {
      const servers = proxy.status.servers?.filter(server => server.host === backend.host && server.port === backend.port && Object.values(policy.hostgroups).includes(server.hostgroup));
      return proxy.status.read.data_at && servers?.length && servers.every(server => server.max_connections !== undefined) ? Math.max(...servers.map(server => server.max_connections!)) : undefined;
    });
    const total = limits.length && limits.every(limit => limit !== undefined) ? limits.reduce<number>((sum, limit) => sum + limit!, 0) : undefined;
    const max = member?.status.read.data_at ? member.status.max_connections : undefined;
    return <TableRow key={`${backend.node_id}/${backend.instance_id}`}><TableCell>{names.get(backend.node_id) ?? backend.node_id}</TableCell><TableCell className="text-right tabular-nums">{total ?? '—'}{total !== undefined && max !== undefined && total > max && <span className="ml-2 text-xs text-warning-intense">配置超额</span>}</TableCell><TableCell className="text-right tabular-nums">{max ?? '—'}</TableCell><TableCell className="text-right tabular-nums">{member?.status.read.data_at ? member.status.connections ?? '—' : '—'}</TableCell></TableRow>;
  })}</TableBody></Table></div></section>;
}

function ProxyRoutingEditor({ cluster, policy, names, onClose }: { cluster: Cluster; policy?: components['schemas']['ProxyRoutingPolicy']; names: Map<string, string>; onClose: () => void }) {
  const [automatic, setAutomatic] = useState(policy?.automatic ?? true), [backends, setBackends] = useState<Backend[] | undefined>(), [value, setValue] = useState<Preview>(), [dirty, setDirty] = useState(false), [uncertain, setUncertain] = useState(false);
  const key = useRef(''), operations = useOperations();
  const preview = useMutation({ mutationFn: async () => unwrap(await api.POST('/database-clusters/{id}/proxy-routing/previews', { params: { path: { id: cluster.id } }, body: { expected_revision: cluster.revision, enabled: true, automatic, ...(backends ? { backends } : {}) } })), onSuccess: result => { setValue(result); setBackends(result.policy.backends); setDirty(false); key.current = newRequestKey(); } });
  useEffect(() => { preview.mutate(); }, []);
  const completed = (op: components['schemas']['Operation']) => { invalidateOperations(); void queryClient.invalidateQueries({ queryKey: ['databases'] }); operations.show(op.id); onClose(); };
  const save = useMutation({ mutationFn: async () => unwrap(await api.POST('/database-clusters/{id}/proxy-routing/actions', { ...targetNode(cluster.coordinator), params: { path: { id: cluster.id }, header: { 'Idempotency-Key': key.current } }, body: value! })), onSuccess: completed, onError: error => { if (unknownResult(error)) setUncertain(true); } });
  const locate = useMutation({ mutationFn: () => findOperation(cluster.coordinator, key.current), onSuccess: op => { if (op) completed(op); else { setUncertain(false); save.reset(); } } });
  const locked = save.isPending || uncertain;
  const changeBackend = (index: number, change: Partial<Backend>) => { setBackends(backends?.map((backend, i) => i === index ? { ...backend, ...change } : backend)); setDirty(true); save.reset(); };
  return <Dialog open onOpenChange={open => { if (!open && !locked) onClose(); }}><DialogContent frame={false} className="ui-modal wide w-full max-w-3xl" closeLabel="关闭代理路由配置"><DialogHeader><DialogTitle>代理路由 · {cluster.name}</DialogTitle></DialogHeader><DialogBody className="space-y-5 overflow-y-auto"><fieldset disabled={locked || preview.isPending} className="space-y-5"><ChoiceField label="自动排序" value={automatic ? 'auto' : 'paused'} options={[[ 'auto', '本机优先、远端按延迟' ], ['paused', '暂停自动排序']]} onChange={next => { setAutomatic(next === 'auto'); setDirty(true); }}/>{backends?.map((backend, index) => <section key={`${backend.node_id}/${backend.instance_id}`} className="space-y-3"><h3 className="font-medium">{names.get(backend.node_id) ?? backend.node_id}</h3><div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_8rem]"><TextField label="SQL 主机地址" value={backend.host} onChange={event => changeBackend(index, { host: event.target.value })} required/><TextField label="端口" type="number" min={1} max={65535} value={backend.port} onChange={event => changeBackend(index, { port: Number(event.target.value) })} required/></div></section>)}</fieldset>
    <Failure error={preview.error ?? locate.error}/><CommandError error={save.error} read={() => { save.reset(); preview.mutate(); }}/>{preview.isPending && <p role="status" className="text-sm text-foreground-muted">正在读取配置并计算差异…</p>}
    {value && !dirty && value.targets.map(target => <section key={`${target.node_id}/${target.instance_id}`} className="space-y-3"><h3 className="font-medium">{names.get(target.node_id) ?? target.node_id}</h3><div className="max-w-full overflow-x-auto"><Table className="min-w-112"><TableHeader><TableRow><TableHead>变更</TableHead><TableHead>SQL 后端</TableHead><TableHead className="text-right">原权重</TableHead><TableHead className="text-right">新权重</TableHead></TableRow></TableHeader><TableBody>{target.changes.map((change, index) => <TableRow key={index}><TableCell>{({ add: '加入路由', remove: '移除路由', weight: '调整权重', hostgroups: '更新主机组' })[change.kind]}</TableCell><TableCell className="font-mono text-xs">{change.kind === 'hostgroups' ? Object.values(value.policy.hostgroups).join(' / ') : address(change)}</TableCell><TableCell className="text-right">{change.kind === 'weight' ? change.before_weight : '—'}</TableCell><TableCell className="text-right">{change.kind === 'weight' || change.kind === 'add' ? change.after_weight : '—'}</TableCell></TableRow>)}{!target.changes.length && <TableRow><TableCell colSpan={4} className="text-center text-foreground-muted">配置一致</TableCell></TableRow>}</TableBody></Table></div></section>)}
  </DialogBody><DialogFooter><Button variant="outline" disabled={locked} onClick={onClose}>取消</Button>{uncertain ? <BusyButton busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton> : <><BusyButton variant="outline" busy={preview.isPending} disabled={locked} onClick={() => preview.mutate()}>重新预览</BusyButton><BusyButton busy={save.isPending} disabled={!value || dirty || preview.isPending || preview.isError} onClick={() => save.mutate()}>应用配置</BusyButton></>}</DialogFooter></DialogContent></Dialog>;
}

