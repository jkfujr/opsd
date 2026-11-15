import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { api, findOperation, unknownResult, unwrap } from '../api/client';
import type { components } from '../api/types';
import { BusyButton, Button, ChoiceField, Failure, LoadingRows, newRequestKey, StatusLabel } from '../business-ui';
import { useOperations, invalidateOperations } from '../operations';
import { queryClient } from '../state';
import { nodesQuery } from './Nodes';

type Cluster = components['schemas']['ManagedDatabaseCluster'];
const states: Record<string, string> = { unbound: '未绑定', pending: '准备中', ready: '已连接', error: '需处理', unavailable: '不可用' };

export function SharedStorage({ clusters }: { clusters: Cluster[] }) {
  const query = useQuery({ queryKey: ['shared-storage'], queryFn: async ({ signal }) => unwrap(await api.GET('/shared-storage', { signal })), refetchInterval: 10000 });
  const [cluster, setCluster] = useState(''), [uncertain, setUncertain] = useState(false);
  const key = useRef(newRequestKey());
  const operations = useOperations();
  const finished = (op: components['schemas']['Operation']) => { setUncertain(false); initialize.reset(); invalidateOperations(); void query.refetch(); operations.show(op.id); };
  const initialize = useMutation({ mutationFn: async () => unwrap(await api.POST('/shared-storage/initialize', { params: { header: { 'Idempotency-Key': key.current } }, body: { cluster_id: cluster, database: 'opsd_core' } })), onSuccess: finished, onError: error => setUncertain(unknownResult(error)) });
  const locate = useMutation({ mutationFn: async () => { const session = queryClient.getQueryData<components['schemas']['CurrentSession']>(['session']) ?? unwrap(await api.GET('/auth/sessions/current')); return findOperation(session.local_node_id, key.current); }, onSuccess: op => { if (op) finished(op); } });
  const recover = useMutation({ mutationFn: async (id: string) => unwrap(await api.POST('/nodes/{node_id}/shared-storage/recovery', { params: { path: { node_id: id } } })), onSuccess: () => { void query.refetch(); invalidateOperations(); } });
  const local = query.data?.local, binding = local?.binding;
  const active = operations.items.filter(op => op.subject === 'shared_initialize' || binding && op.resource_id === binding.cluster_id);
  return <section className="panel panel-padded content-stack"><div className="flex flex-wrap items-center justify-between gap-3"><h2 className="section-title">OPSD 数据</h2>{local && <StatusLabel state={local.state}>{states[local.state] ?? '状态未知'}</StatusLabel>}</div>
    <Failure error={query.error ?? initialize.error ?? locate.error ?? recover.error} retry={query.isError ? () => void query.refetch() : undefined} busy={query.isFetching}/>
    {binding ? <dl className="detail-fields"><div><dt>集群</dt><dd><Link className="hover:underline" to={`/databases/${binding.cluster_id}`}>{clusters.find(item => item.id === binding.cluster_id)?.name ?? binding.cluster_id}</Link></dd></div><div><dt>数据库</dt><dd>{binding.database}</dd></div></dl> : local?.phase === 'unbound' && <form className="flex flex-wrap items-end gap-3" onSubmit={event => { event.preventDefault(); initialize.mutate(); }}><ChoiceField resource label="Galera 集群" value={cluster} options={[[ '', '选择集群' ], ...clusters.map(item => [item.id, item.name] as [string, string])]} disabled={initialize.isPending || uncertain} onChange={value => { setCluster(value); key.current = newRequestKey(); initialize.reset(); }}/>{uncertain ? <BusyButton type="button" busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton> : <BusyButton type="submit" busy={initialize.isPending} disabled={!cluster || query.isError}>初始化 opsd_core</BusyButton>}</form>}
    {binding && local?.phase === 'active' && <ConsoleNodes/>}
    <div className="max-w-full overflow-x-auto"><Table className="min-w-144"><TableHeader><TableRow><TableHead>节点</TableHead><TableHead>本机代理</TableHead><TableHead>状态</TableHead><TableHead>策略版本</TableHead><TableHead className="text-right">待上传</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader><TableBody>{query.isPending ? <LoadingRows columns={6}/> : query.data?.nodes.map(item => <TableRow key={item.node_id}><TableCell>{item.name}</TableCell><TableCell><span className="break-all text-xs">{item.status.proxy_id || '—'}</span></TableCell><TableCell><StatusLabel state={item.status.state}>{states[item.status.state] ?? '状态未知'}</StatusLabel>{item.status.error && <p role="alert" className="mt-1 max-w-64 break-words text-xs text-error-intense">{item.status.error.message}</p>}</TableCell><TableCell className="font-mono text-xs">{item.status.policy_version?.slice(0, 12) || '—'}</TableCell><TableCell className="text-right tabular-nums">{item.status.pending ?? '—'}</TableCell><TableCell className="text-right"><BusyButton variant="ghost" size="sm" busy={recover.isPending && recover.variables === item.node_id} disabled={!item.status.binding || recover.isPending} onClick={() => recover.mutate(item.node_id)}>核对恢复</BusyButton></TableCell></TableRow>)}</TableBody></Table></div>
    {active.length > 0 && <div className="flex flex-wrap gap-2">{active.map(op => <Button key={op.id} size="sm" variant="outline" onClick={() => operations.show(op.id)}>查看操作 · {op.target_name || op.id.slice(-8)}</Button>)}</div>}
  </section>;
}

function ConsoleNodes() {
  const query = useQuery({ queryKey: ['shared-storage', 'consoles'], queryFn: async ({ signal }) => unwrap(await api.GET('/shared-storage/consoles', { signal })), refetchInterval: 10000 });
  const nodes = useQuery(nodesQuery);
  const [draft, setDraft] = useState<components['schemas']['SharedConsoles']>({ revision: 0, primary: '', backup: '' });
  const [dirty, setDirty] = useState(false), [uncertain, setUncertain] = useState(false);
  const key = useRef(newRequestKey());
  useEffect(() => { if (!dirty && query.data) setDraft(query.data); }, [query.data, dirty]);
  const save = useMutation({ mutationFn: async () => unwrap(await api.PUT('/shared-storage/consoles', { params: { header: { 'Idempotency-Key': key.current } }, body: { revision: draft.revision, primary: draft.primary, backup: draft.backup } })), onSuccess: value => { queryClient.setQueryData(['shared-storage', 'consoles'], value); setDirty(false); setUncertain(false); key.current = newRequestKey(); }, onError: error => setUncertain(unknownResult(error)) });
  const read = useMutation({ mutationFn: async () => unwrap(await api.GET('/shared-storage/consoles')), onSuccess: value => { queryClient.setQueryData(['shared-storage', 'consoles'], value); if (value.revision > draft.revision && value.primary === draft.primary && value.backup === draft.backup) { setUncertain(false); setDirty(false); save.reset(); } } });
  const change = (value: Partial<typeof draft>) => { setDraft(previous => ({ ...previous, ...value })); setDirty(true); key.current = newRequestKey(); save.reset(); };
  const options: [string, string][] = [[ '', '未配置' ], ...(nodes.data?.items.filter(node => node.coordinator_candidate && !node.revoked_at).map(node => [node.id, node.name] as [string, string]) ?? [])];
  const locked = !query.data || query.isError || query.data.cached || save.isPending || uncertain;
  return <form className="content-stack" onSubmit={event => { event.preventDefault(); save.mutate(); }}><div className="grid gap-4 sm:grid-cols-2"><ChoiceField resource label="主用管理节点" value={draft.primary} options={options} disabled={locked} onChange={primary => change({ primary })}/><ChoiceField resource label="备用管理节点" value={draft.backup} options={options} disabled={locked} onChange={backup => change({ backup })}/></div><Failure error={query.error ?? save.error ?? read.error}/><div>{uncertain ? <BusyButton type="button" busy={read.isPending} onClick={() => read.mutate()}>查询管理节点</BusyButton> : <BusyButton type="submit" variant="outline" busy={save.isPending} disabled={locked || !dirty || !draft.primary || draft.primary === draft.backup}>保存管理节点</BusyButton>}</div></form>;
}

export function DatabaseCoordinator({ cluster, nodes, onClose }: { cluster: Cluster; nodes: components['schemas']['NodeStatus'][]; onClose: () => void }) {
  const [target, setTarget] = useState(cluster.coordinator), [uncertain, setUncertain] = useState(false);
  const key = useRef(newRequestKey());
  const done = () => { void queryClient.invalidateQueries({ queryKey: ['databases'] }); onClose(); };
  const save = useMutation({ mutationFn: async () => unwrap(await api.POST('/database-clusters/{id}/coordinator', { params: { path: { id: cluster.id }, header: { 'Idempotency-Key': key.current } }, body: { coordinator: target, expected_revision: cluster.revision } })), onSuccess: done, onError: error => setUncertain(unknownResult(error)) });
  const read = useMutation({ mutationFn: async () => unwrap(await api.GET('/database-clusters/{id}', { params: { path: { id: cluster.id } } })), onSuccess: current => { if (current.coordinator === target && current.revision > cluster.revision) done(); } });
  return <form id="database-coordinator" className="content-stack" onSubmit={event => { event.preventDefault(); save.mutate(); }}><ChoiceField resource label="协调节点" value={target} options={nodes.filter(node => node.coordinator_candidate && node.connected && !node.revoked_at || node.id === cluster.coordinator).map(node => [node.id, node.name] as [string, string])} disabled={save.isPending || uncertain} onChange={value => { setTarget(value); key.current = newRequestKey(); save.reset(); }}/><Failure error={save.error ?? read.error}/><div>{uncertain ? <BusyButton type="button" busy={read.isPending} onClick={() => read.mutate()}>读取协调归属</BusyButton> : <BusyButton type="submit" busy={save.isPending} disabled={target === cluster.coordinator}>转交</BusyButton>}</div></form>;
}
