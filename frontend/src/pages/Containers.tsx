import { useEffect, useId, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { Button, buttonVariants } from '@appica/ui-react/button';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { Drawer, DrawerContent, DrawerHeader, DrawerBody, DrawerTitle } from '@appica/ui-react/drawer';
import { Dialog, DialogContent, DialogHeader, DialogBody, DialogTitle } from '@appica/ui-react/dialog';
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from '@appica/ui-react/dropdown-menu';
import { Skeleton } from '@appica/ui-react/skeleton';
import { Switch } from '@appica/ui-react/switch';
import { api, RequestError, unwrap } from '../api/client';
import type { components } from '../api/schema.gen';
import { queryClient } from '../state';
import { useOperations, invalidateOperations } from '../operations';
import { BusyButton, ChoiceField, Confirm, date, Failure, LoadingRows, newRequestKey, PageTitle, StatusLabel, TextField } from '../ui';
import { nodesQuery } from './Nodes';

type Container = components['schemas']['ContainerListItem'];
type Detail = components['schemas']['ContainerDetail'];
type Action = components['schemas']['ContainerActionRequest']['action'];
const actions: Record<Action, string> = { start: '启动', stop: '停止', restart: '重启' };
const states: Record<string, string> = { running: '运行中', exited: '已停止', created: '未启动', paused: '已暂停', restarting: '重启中', removing: '移除中', dead: '异常' };
const health: Record<string, string> = { healthy: '健康', unhealthy: '不健康', starting: '检查中' };
const runtimeStates = { ready: '可用', not_installed: '未安装 Docker', unavailable: 'Docker 不可用', unsupported: '暂不支持', offline: '节点离线' };

function useVisible() {
  const [visible, setVisible] = useState(document.visibilityState !== 'hidden');
  useEffect(() => { const update = () => setVisible(document.visibilityState !== 'hidden'); document.addEventListener('visibilitychange', update); return () => document.removeEventListener('visibilitychange', update); }, []);
  return visible;
}
function canChange(container: Container, action: Action) {
  if (container.read_only_reason) return false;
  return action === 'start' ? ['created', 'exited'].includes(container.state) : container.state === 'running';
}
function ContainerStatus({ value }: { value: Container | Detail }) {
  return <div className="space-y-1"><StatusLabel state={value.state === 'running' ? 'ready' : value.state === 'dead' ? 'failed' : ['created', 'exited', 'paused'].includes(value.state) ? 'offline' : 'applying'}>{states[value.state] ?? value.state}</StatusLabel>{health[value.health] && <span className={`block text-xs ${value.health === 'unhealthy' ? 'text-error-intense' : 'text-foreground-muted'}`}>{health[value.health]}</span>}</div>;
}
function Ports({ value }: { value: Container['ports'] }) {
  return <>{value.length ? value.map((port, index) => <div key={index} className="break-all font-mono text-xs">{port.host_port ? `${port.host_ip.includes(':') ? `[${port.host_ip}]` : port.host_ip || '*'}:${port.host_port} → ` : ''}{port.container_port}/{port.protocol}</div>) : '—'}</>;
}
function ContainerActions({ container, disabled, onAction }: { container: Container; disabled: boolean; onAction: (container: Container, action: Action) => void }) {
  return container.read_only_reason ? <span className="text-xs text-foreground-muted">{container.read_only_reason}</span> : <DropdownMenu><DropdownMenuTrigger className={buttonVariants({ variant: 'ghost', size: 'sm' })} disabled={disabled}>更多</DropdownMenuTrigger><DropdownMenuContent align="end">{(Object.keys(actions) as Action[]).map((action) => <DropdownMenuItem key={action} disabled={!canChange(container, action)} onClick={() => onAction(container, action)}>{actions[action]}</DropdownMenuItem>)}</DropdownMenuContent></DropdownMenu>;
}

export function Containers() {
  const [params, setParams] = useSearchParams();
  const node = params.get('node') ?? '', state = params.get('state') ?? '', search = params.get('search') ?? '';
  const visible = useVisible();
  const nodes = useQuery(nodesQuery);
  const query = useQuery({ queryKey: ['containers', 'list', node, state, search], enabled: visible, queryFn: async ({ signal }) => unwrap(await api.GET('/containers', { signal, params: { query: { node_id: node || undefined, state: state || undefined, search: search || undefined } } })), refetchInterval: visible ? 5000 : false });
  const [detail, setDetail] = useState<Container | null>(null);
  const [logs, setLogs] = useState<Container | null>(null);
  const [confirm, setConfirm] = useState<{ container: Container; action: Action } | null>(null);
  type Request = { container: Container; action: Action; key: string };
  const [uncertain, setUncertain] = useState<Request | null>(null);
  const [message, setMessage] = useState('');
  const operations = useOperations();
  const updated = () => { invalidateOperations(); void queryClient.invalidateQueries({ queryKey: ['containers'] }); };
  const change = useMutation({ retry: false, mutationFn: async ({ container, action, key }: Request) => unwrap(await api.POST('/nodes/{id}/containers/{container_id}/actions', { params: { path: { id: container.node_id, container_id: container.container_id }, header: { 'X-CSRF-Token': '', 'Idempotency-Key': key } }, body: { engine_id: container.engine_id, action } })), onSuccess: (operation) => { setConfirm(null); setUncertain(null); updated(); operations.show(operation.id); }, onError: (error, request) => { setConfirm(null); if (!(error instanceof RequestError && [403, 409, 422].includes(error.status))) setUncertain(request); updated(); } });
  const locate = useMutation({ mutationFn: async () => {
    const result = unwrap(await api.GET('/operations', { params: { query: { node_id: uncertain!.container.node_id, idempotency_key: uncertain!.key } } }));
    if (result.unavailable?.length) throw new Error('节点暂不可达，请稍后查询原操作');
    return result.items[0];
  }, onSuccess: (operation) => { setUncertain(null); change.reset(); updated(); if (operation) operations.show(operation.id); else setMessage('目标节点未找到原请求'); } });
  const run = (container: Container, action: Action) => {
    if (uncertain || change.isPending) return;
    change.reset(); setMessage('');
    if (action === 'start') change.mutate({ container, action, key: newRequestKey() });
    else setConfirm({ container, action });
  };
  const update = (key: string, value: string) => { const next = new URLSearchParams(params); if (value) next.set(key, value); else next.delete(key); setParams(next, { replace: true }); };
  const unavailable = query.data?.nodes.filter((item) => item.state !== 'ready') ?? [];
  const runtimeReady = (id: string) => query.data?.nodes.some((item) => item.node_id === id && item.state === 'ready') ?? false;
  const disabled = Boolean(uncertain) || change.isPending || query.isError;
  return <section className="panel"><PageTitle>容器</PageTitle>
    <div className="mb-4 grid gap-4 sm:grid-cols-2 xl:grid-cols-[minmax(0,24rem)_minmax(0,12rem)_minmax(0,20rem)_auto]">
      <ChoiceField label="节点" value={node} options={[[ '', '全部节点' ], ...(nodes.data?.items.filter((item) => !item.revoked_at).map((item) => [item.id, item.name] as const) ?? [])]} onChange={(value) => update('node', value)} />
      <ChoiceField label="容器状态" value={state} options={[[ '', '全部状态' ], ['running', '运行中'], ['stopped', '已停止'], ['paused', '已暂停'], ['restarting', '重启中'], ['dead', '异常']]} onChange={(value) => update('state', value)} />
      <TextField label="名称" value={search} onChange={(event) => update('search', event.target.value)} />
      <div className="flex items-end"><BusyButton variant="outline" busy={query.isFetching} onClick={() => void query.refetch()}>刷新</BusyButton></div>
    </div>
    <Failure error={query.error ?? nodes.error} retry={() => { void query.refetch(); void nodes.refetch(); }} busy={query.isFetching} /><Failure error={change.error ?? locate.error} />
    {uncertain && <div className="mb-4 flex flex-wrap items-center gap-3"><span className="text-sm">{uncertain.container.node_name} · {actions[uncertain.action]} {uncertain.container.name} · 受理结果未知</span><BusyButton size="sm" variant="outline" busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton></div>}
    {message && <p role="status" className="mb-3 text-sm">{message}</p>}
    {unavailable.length > 0 && <ul className="mb-4 space-y-2 text-sm">{unavailable.map((item) => <li key={item.node_id} className="flex flex-wrap items-center gap-x-3 gap-y-1"><Link to={`/nodes/${item.node_id}`} className="font-medium hover:underline">{item.node_name}</Link><StatusLabel state={item.state === 'offline' ? 'offline' : 'unknown'}>{runtimeStates[item.state]}</StatusLabel>{item.error && <span className="text-foreground-muted">{item.error.message}</span>}</li>)}</ul>}
    <div className="max-w-full overflow-x-auto" aria-busy={query.isPending}><Table className="min-w-200" hoverableRows><TableHeader><TableRow><TableHead>节点</TableHead><TableHead>容器</TableHead><TableHead>镜像</TableHead><TableHead>状态</TableHead><TableHead>端口</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader><TableBody>
      {query.isPending ? <LoadingRows columns={6} /> : query.data?.items.map((container) => <TableRow key={`${container.node_id}/${container.engine_id}/${container.container_id}`}>
        <TableCell className="max-w-48 break-all"><Link to={`/nodes/${container.node_id}`} className="hover:underline">{container.node_name}</Link></TableCell>
        <TableCell className="max-w-56 break-all"><span className="font-medium text-foreground-intense">{container.name}</span>{container.project && <span className="mt-1 block text-xs text-foreground-muted">{container.project}{container.service ? ` / ${container.service}` : ''}</span>}</TableCell>
        <TableCell className="max-w-64 break-all font-mono text-xs">{container.image}</TableCell><TableCell><ContainerStatus value={container} /></TableCell><TableCell><Ports value={container.ports} /></TableCell>
        <TableCell><div className="flex items-center justify-end gap-1"><Button variant="ghost" size="sm" onClick={() => setDetail(container)}>查看</Button><Button variant="ghost" size="sm" onClick={() => setLogs(container)}>日志</Button><ContainerActions container={container} disabled={disabled} onAction={run} /></div></TableCell>
      </TableRow>)}
      {!query.isPending && !query.isError && query.data?.items.length === 0 && <TableRow><TableCell colSpan={6} className="py-8 text-center text-foreground-muted">{unavailable.length === query.data.nodes.length && unavailable.length > 0 ? '容器数据暂不可用' : '暂无容器'}</TableCell></TableRow>}
    </TableBody></Table></div>
    <Confirm open={confirm !== null} title={`${confirm ? actions[confirm.action] : ''}容器“${confirm?.container.name ?? ''}”？`} description={confirm ? `${confirm.container.node_name} · ${confirm.action === 'stop' ? '将停止容器中的服务。' : '将中断并重新启动容器中的服务。'}` : ''} confirmLabel={confirm ? actions[confirm.action] : '确认'} busy={change.isPending} error={null} onCancel={() => { if (!change.isPending) setConfirm(null); }} onConfirm={() => { if (confirm) change.mutate({ ...confirm, key: newRequestKey() }); }} />
    {detail && <ContainerDrawer key={`${detail.node_id}/${detail.container_id}`} container={detail} disabled={disabled || !runtimeReady(detail.node_id)} onClose={() => setDetail(null)} onLogs={() => { setLogs(detail); setDetail(null); }} onAction={run} />}
    {logs && <ContainerLogs key={`${logs.node_id}/${logs.container_id}`} container={logs} onClose={() => setLogs(null)} />}
  </section>;
}

function ContainerDrawer({ container, disabled, onClose, onLogs, onAction }: { container: Container; disabled: boolean; onClose: () => void; onLogs: () => void; onAction: (container: Container, action: Action) => void }) {
  const query = useQuery({ queryKey: ['containers', 'detail', container.node_id, container.engine_id, container.container_id], queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{id}/containers/{container_id}', { signal, params: { path: { id: container.node_id, container_id: container.container_id }, query: { engine_id: container.engine_id } } })) });
  const value = query.data;
  const fields = value ? [['节点', container.node_name], ['容器', value.name], ['容器 ID', value.container_id], ['镜像', value.image], ['创建时间', date(value.created_at)], ['启动时间', date(value.started_at)], ['停止时间', date(value.finished_at)], ['重启策略', value.restart_policy || '—'], ['重启次数', String(value.restart_count)]] : [];
  return <Drawer side="right" open onOpenChange={(open) => { if (!open) onClose(); }}><DrawerContent frame={false} closeLabel="关闭容器详情" className="h-full w-full max-w-2xl"><DrawerHeader><DrawerTitle>容器详情</DrawerTitle></DrawerHeader><DrawerBody className="min-h-0 space-y-5 overflow-y-auto">
    <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />{query.isPending ? <div className="space-y-4" aria-busy="true">{Array.from({ length: 6 }, (_, index) => <Skeleton key={index} className="h-5 w-full" />)}</div> : value && <>
      <div className="flex flex-wrap items-center gap-3"><ContainerStatus value={value} /><Button variant="outline" size="sm" onClick={onLogs}>日志</Button><ContainerActions container={{ ...container, ...value }} disabled={disabled || query.isError} onAction={onAction} /></div>
      <dl className="grid gap-4 sm:grid-cols-2">{fields.map(([label, text]) => <div key={label}><dt className="mb-1 text-xs text-foreground-muted">{label}</dt><dd className="break-all text-sm">{text}</dd></div>)}</dl>
      <section><h3 className="mb-2 font-medium">端口</h3><Ports value={value.ports} /></section>
      <section><h3 className="mb-2 font-medium">挂载</h3>{value.mounts.length ? <ul className="space-y-2 text-sm">{value.mounts.map((mount, index) => <li key={index} className="break-all"><span className="font-mono text-xs">{mount.source} → {mount.destination}</span><span className="ml-2 text-xs text-foreground-muted">{mount.type} · {mount.read_only ? '只读' : '读写'}</span></li>)}</ul> : <p className="text-sm text-foreground-muted">无挂载</p>}</section>
      <section><h3 className="mb-2 font-medium">网络</h3>{value.networks.length ? <ul className="space-y-2 text-sm">{value.networks.map((network) => <li key={network.name} className="break-all">{network.name}<span className="ml-2 font-mono text-xs text-foreground-muted">{[network.ipv4, network.ipv6].filter(Boolean).join(' · ') || '—'}</span></li>)}</ul> : <p className="text-sm text-foreground-muted">无网络</p>}</section>
    </>}
  </DrawerBody></DrawerContent></Drawer>;
}
function ContainerLogs({ container, onClose }: { container: Container; onClose: () => void }) {
  const [tail, setTail] = useState('200');
  const [automatic, setAutomatic] = useState(true);
  const visible = useVisible();
  const automaticID = useId();
  const query = useQuery({ queryKey: ['containers', 'logs', container.node_id, container.engine_id, container.container_id, tail], enabled: visible, queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{id}/containers/{container_id}/logs', { signal, params: { path: { id: container.node_id, container_id: container.container_id }, query: { engine_id: container.engine_id, tail: Number(tail) } } })), refetchInterval: visible && automatic ? 2000 : false, refetchOnWindowFocus: automatic });
  return <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}><DialogContent className="w-full max-w-4xl" closeLabel="关闭容器日志"><DialogHeader><DialogTitle>容器日志</DialogTitle></DialogHeader><DialogBody className="min-w-0 space-y-4 overflow-y-auto">
    <p className="break-all text-sm">{container.node_name} · {container.name}</p><div className="flex flex-wrap items-end gap-4"><div className="w-32"><ChoiceField label="行数" value={tail} options={['100', '200', '500', '1000'].map((value) => [value, value] as const)} onChange={setTail} /></div><div className="flex h-10 items-center gap-2"><label htmlFor={automaticID} className="text-sm">自动刷新</label><Switch id={automaticID} checked={automatic} onCheckedChange={setAutomatic} /></div><BusyButton variant="outline" busy={query.isFetching} onClick={() => void query.refetch()}>刷新</BusyButton></div>
    <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />{query.data?.truncated && <p role="status" className="text-sm text-foreground-muted">日志已截断</p>}
    {query.isPending ? <Skeleton className="h-64 w-full" /> : <pre aria-label="容器日志" className="max-h-128 min-h-48 overflow-auto whitespace-pre-wrap break-all rounded-md bg-background-muted p-4 font-mono text-xs">{query.data?.text || (query.isError ? '' : '暂无日志')}</pre>}
  </DialogBody></DialogContent></Dialog>;
}
