import { usePagedQuery } from '../api/pagination';
import { ContinueReading } from '../business-ui';
import { forgetRequest } from '../api/requests';
import { useId, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Button, buttonVariants } from '../business-ui';
import { Switch } from '@appica/ui-react/switch';
import { Checkbox } from '@appica/ui-react/checkbox';
import { Field, FieldLabel } from '@appica/ui-react/field';
import { Textarea } from '@appica/ui-react/textarea';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogBody, DialogFooter } from '@appica/ui-react/dialog';
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@appica/ui-react/collapsible';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from '@appica/ui-react/dropdown-menu';
import { allPages, api, condition, findOperation, unwrap, RequestError, unknownResult } from '../api/client';
import type { components } from '../api/types';
import { useSession } from '../auth';
import { queryClient } from '../state';
import { invalidateOperations, useOperations } from '../operations';
import { BusyButton, ChoiceField, Confirm, date, Failure, LoadingRows, newRequestKey, PageTitle, StatusLabel, TextField, useUnsavedChanges } from '../business-ui';
import { CommandError } from '../commands';
import { nodesQuery } from './Nodes';

type Token = components['schemas']['APIToken'];
type TokenInput = components['schemas']['APITokenInput'];
type Created = components['schemas']['APITokenCreated'];
type Grant = components['schemas']['APIGrant'];
const modules = [['monitoring', '监控'], ['nodes', '节点基础信息'], ['containers', '容器'], ['firewall', '防火墙'], ['databases', '数据库'], ['tasks', 'Shell 任务'], ['compose', 'Compose']] as const;
type Module = typeof modules[number][0];
type Level = 'none' | 'read' | 'manage';
const templates: Record<string, { name: string; levels: Partial<Record<Module, Level>> }> = {
  monitoring: { name: '监控看板', levels: { monitoring: 'read' } },
  readonly: { name: '全部只读', levels: { monitoring: 'read', nodes: 'read', containers: 'read', firewall: 'read', databases: 'read', tasks: 'read', compose: 'read' } },
  containers: { name: '容器管理', levels: { containers: 'manage' } },
  databases: { name: '数据库管理', levels: { databases: 'manage' } },
  custom: { name: '自定义', levels: {} },
};
async function tokenCondition(token: Token) {
  const current = unwrap(await api.GET('/nodes/{node_id}/api-tokens/{id}', { params: { path: { node_id: token.owner, id: token.id } } }));
  if (current.revision !== token.revision) throw new RequestError('令牌已经改变，请重新读取', 412);
  return condition(current);
}
function tokenState(token: Token) { return !token.enabled ? 'disabled' : token.expires_at && new Date(token.expires_at).getTime() <= Date.now() ? 'expired' : 'ready'; }

export function APISettings() {
  const session = useSession();
  const id = session.local_node_id;
  const config = useQuery({ queryKey: ['nodes', id, 'settings'], queryFn: async ({ signal }) => unwrap(await api.GET('/nodes/{node_id}/configuration', { signal, params: { path: { node_id: id } } })) });
  const tokens = usePagedQuery({ queryKey: ['api-tokens'], queryFn: async ({ signal }) => allPages(async (cursor) => unwrap(await api.GET('/nodes/{node_id}/api-tokens', { signal, params: { path: { node_id: id }, query: { cursor, limit: 100 } } }))) });
  const [form, setForm] = useState<Token | 'new' | null>(null);
  const [created, setCreated] = useState<Created | null>(null);
  const [confirm, setConfirm] = useState<{ token: Token; kind: 'delete' | 'regenerate' } | null>(null);
  const operations = useOperations();
  const switchID = useId();
  const switchKey=useRef('');const [switchUnknown,setSwitchUnknown]=useState(false);
  const toggle = useMutation({ mutationFn: async (enabled: boolean) => {switchKey.current||=newRequestKey();return unwrap(await api.PATCH('/nodes/{node_id}/configuration', { params: { path: { node_id: id }, header: { ...condition(config.data), 'Idempotency-Key': switchKey.current } }, body: { api: enabled } }));}, onSuccess: (op) => {switchKey.current='';operations.show(op.id); invalidateOperations();},onError:(error)=>{if(unknownResult(error))setSwitchUnknown(true);else switchKey.current='';} });
  const locateSwitch=useMutation({mutationFn:()=>findOperation(id,switchKey.current),onSuccess:(op)=>{setSwitchUnknown(false);toggle.reset();switchKey.current='';void config.refetch();if(op){operations.show(op.id);invalidateOperations();}}});
  const update = useMutation({ mutationFn: async (token: Token) => unwrap(await api.PATCH('/nodes/{node_id}/api-tokens/{id}', { params: { path: { node_id: token.owner, id: token.id }, header: await tokenCondition(token) }, body: { enabled: !token.enabled } })), onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['api-tokens'] }), onError: () => void tokens.refetch() });
  const actionKey = useRef('');
  const action = useMutation({ gcTime: 0, mutationFn: async () => {
    const { token, kind } = confirm!;
    if (kind === 'delete') { unwrap(await api.DELETE('/nodes/{node_id}/api-tokens/{id}', { params: { path: { node_id: token.owner, id: token.id }, header: await tokenCondition(token) } })); return null; }
    actionKey.current ||= newRequestKey();
    return unwrap(await api.POST('/nodes/{node_id}/api-tokens/{id}/actions', { params: { path: { node_id: token.owner, id: token.id }, header: { 'Idempotency-Key': actionKey.current } }, body: { action: 'regenerate', expected_revision: token.revision } }));
  }, onSuccess: (result) => { setConfirm(null); if (result) setCreated(result); void queryClient.invalidateQueries({ queryKey: ['api-tokens'] }); }, onError: () => void tokens.refetch() });
  return <div className="page-stack"><PageTitle>API</PageTitle><div className="flex flex-wrap items-end justify-between gap-4"><div className="flex h-10 items-center gap-3"><label htmlFor={switchID} className="font-medium">API</label><Switch id={switchID} checked={Boolean(config.data?.api)} disabled={!config.data || config.isError || config.data.cached || toggle.isPending||switchUnknown} onCheckedChange={(value) => toggle.mutate(value)} /><StatusLabel state={config.data?.api ? 'ready' : 'disabled'}>{config.data?.api ? '已开启' : '已关闭'}</StatusLabel></div><div className="flex flex-wrap gap-3">{switchUnknown&&<BusyButton variant="outline" busy={locateSwitch.isPending} onClick={()=>locateSwitch.mutate()}>查询原操作</BusyButton>}<BusyButton variant="outline" busy={tokens.isFetching} onClick={()=>void tokens.refetch()}>刷新</BusyButton><Button disabled={!config.data?.api || tokens.isError} onClick={() => setForm('new')}>创建令牌</Button></div></div>
    <TextField label="API 地址" readOnly value={`${window.location.origin}/api/v2`} className="font-mono text-sm" />
    <Failure error={config.error ?? toggle.error ?? locateSwitch.error ?? update.error ?? tokens.error} retry={() => { void config.refetch(); void tokens.refetch(); }} busy={config.isFetching || tokens.isFetching} />
    <div className="max-w-full overflow-x-auto" aria-busy={tokens.isPending}><Table className="min-w-160"><TableHeader><TableRow><TableHead>令牌</TableHead><TableHead>权限</TableHead><TableHead>状态</TableHead><TableHead>到期时间</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader><TableBody>
      {tokens.isPending ? <LoadingRows columns={5} /> : tokens.data?.items.map((token) => <TableRow key={token.id}><TableCell className="max-w-56 break-words font-medium">{token.name}</TableCell><TableCell><div className="space-y-1 text-xs">{token.grants.map((grant) => <div key={grant.module}>{modules.find(([key]) => key === grant.module)?.[1]} · {grant.level === 'manage' ? '管理' : '读取'} · {grant.scope === 'all' ? '全部' : `${grant.resources.length} ${grant.scope === 'regions' ? '个地域' : grant.scope === 'clusters' ? '个集群' : '个节点'}`}</div>)}</div></TableCell><TableCell><StatusLabel state={tokenState(token)}>{({ ready: '可用', disabled: '已停用', expired: '已过期' })[tokenState(token)]}</StatusLabel></TableCell><TableCell className="whitespace-nowrap">{token.expires_at ? date(token.expires_at) : '长期有效'}</TableCell><TableCell className="text-right"><DropdownMenu><DropdownMenuTrigger className={buttonVariants({ variant: 'ghost', size: 'sm' })} disabled={update.isPending || action.isPending}>更多</DropdownMenuTrigger><DropdownMenuContent align="end"><DropdownMenuItem onClick={() => setForm(token)}>编辑权限</DropdownMenuItem><DropdownMenuItem onClick={() => update.mutate(token)}>{token.enabled ? '停用' : '启用'}</DropdownMenuItem><DropdownMenuItem onClick={() => { actionKey.current = ''; action.reset(); setConfirm({ token, kind: 'regenerate' }); }}>重新生成</DropdownMenuItem><DropdownMenuItem onClick={() => { action.reset(); setConfirm({ token, kind: 'delete' }); }}>删除</DropdownMenuItem></DropdownMenuContent></DropdownMenu></TableCell></TableRow>)}
      {tokens.data?.items.length === 0 && !tokens.isError && <TableRow><TableCell colSpan={5} className="py-8 text-center text-foreground-muted">暂无令牌</TableCell></TableRow>}
    </TableBody></Table></div>
    <ContinueReading query={tokens}/>{form && <TokenForm token={form === 'new' ? null : form} onClose={() => setForm(null)} onCreated={(value) => { setCreated(value); setForm(null); }} />}
    {created && <TokenResult value={created} onClose={() => { setCreated(null); action.reset(); }} />}
    <Confirm open={confirm !== null} title={`${confirm?.kind === 'delete' ? '删除' : '重新生成'}令牌“${confirm?.token.name ?? ''}”？`} description={confirm?.kind === 'delete' ? '删除后将拒绝该令牌的新请求。' : '旧值立即失效，客户端需要更新令牌。'} confirmLabel={action.error && unknownResult(action.error) ? '查询原请求' : confirm?.kind === 'delete' ? '删除' : '重新生成'} busy={action.isPending} error={action.error} onCancel={() => setConfirm(null)} onConfirm={() => { if (action.error && unknownResult(action.error)) { setConfirm(null); operations.show(''); } else action.mutate(); }} />
  </div>;
}

function TokenForm({ token, onClose, onCreated }: { token: Token | null; onClose: () => void; onCreated: (result: Created) => void }) {
  const owner = useSession().local_node_id;
  const base = useRef(token);
  const read = useMutation({ mutationFn: async () => unwrap(await api.GET('/nodes/{node_id}/api-tokens/{id}', { params: { path: { node_id: token!.owner, id: token!.id } } })), onSuccess: value => { base.current = value; save.reset(); } });
  const key = useRef(newRequestKey());
  const [receipt, setReceipt] = useState('');
  const nodes = useQuery(nodesQuery);
  const databases = usePagedQuery({ queryKey: ['databases', 'list'], queryFn: async ({ signal }) => allPages(async (cursor) => unwrap(await api.GET('/database-clusters', { signal, params: { query: { cursor, limit: 100 } } }))) });
  const [template, setTemplate] = useState(token ? 'custom' : 'monitoring');
  const [name, setName] = useState(token?.name ?? '监控看板');
  const [permissions, setPermissions] = useState<Record<Module, { level: Level; scope: Grant['scope']; resources: string[] }>>(() => Object.fromEntries(modules.map(([module]) => { const grant = token?.grants.find((g) => g.module === module); return [module, { level: grant?.level ?? (!token && module === 'monitoring' ? 'read' : 'none'), scope: grant?.scope ?? 'all', resources: grant?.resources ?? [] }]; })) as Record<Module, { level: Level; scope: Grant['scope']; resources: string[] }>);
  const [expires, setExpires] = useState(token?.expires_at ? new Date(new Date(token.expires_at).getTime() - new Date(token.expires_at).getTimezoneOffset() * 60000).toISOString().slice(0, 16) : '');
  const [ips, setIPs] = useState(token?.source_ips.join('\n') ?? '');
  const [dirty, setDirty] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  useUnsavedChanges(dirty);
  const sourceID = useId();
  const save = useMutation({ gcTime: 0, mutationFn: async () => {
    const grants: Grant[] = modules.flatMap(([module]) => { const p = permissions[module]; return p.level === 'none' ? [] : [{ module, level: p.level, scope: p.scope, resources: p.scope === 'all' ? [] : p.resources }]; });
    if (!grants.length || grants.some((g) => g.scope !== 'all' && !g.resources.length)) throw new RequestError('请选择权限和资源', 422);
    const body: TokenInput = { name: name.trim(), enabled: token?.enabled ?? true, grants, expires_at: expires ? new Date(expires).toISOString() : undefined, source_ips: ips.split(/\s+/).filter(Boolean) };
    if (token) { unwrap(await api.PATCH('/nodes/{node_id}/api-tokens/{id}', { params: { path: { node_id: token.owner, id: token.id }, header: await tokenCondition(base.current!) }, body: { ...body, expires_at: body.expires_at ?? null } })); return null; }
    return unwrap(await api.POST('/nodes/{node_id}/api-tokens', { params: { path: { node_id: owner }, header: { 'Idempotency-Key': key.current } }, body }));
  }, onSuccess: (result) => { setDirty(false); void queryClient.invalidateQueries({ queryKey: ['api-tokens'] }); if (result) onCreated(result); else onClose(); }, onError: (error) => { if (unknownResult(error)) setUncertain(true); } });
  const selectTemplate = (key: string) => {
    setTemplate(key); setName(templates[key].name);
    setPermissions((previous) => {
      const next = { ...previous };
      for (const [module] of modules) next[module] = { level: templates[key].levels[module] ?? 'none', scope: 'all', resources: [] };
      return next;
    });
    setDirty(true);
  };
  const change = (module: Module, value: Partial<typeof permissions[Module]>) => { setPermissions({ ...permissions, [module]: { ...permissions[module], ...value } }); setDirty(true); };
  const locate = useMutation({ mutationFn: async () => unwrap(await api.GET('/requests/{request_key}', { params: { path: { request_key: key.current }, query: { node_id: owner } } })), onSuccess: (value) => { if (value.state !== 'completed') { setReceipt('原请求尚未完成，请继续查询。'); return; } forgetRequest(key.current); setReceipt(value.secret_unavailable ? '令牌已创建，秘密值无法重新读取。请关闭表单后显式重新生成。' : '请求已完成，请关闭表单后读取令牌。'); void queryClient.invalidateQueries({ queryKey: ['api-tokens'] }); } });
  return <Dialog open onOpenChange={(open) => { if (!open && !save.isPending) onClose(); }}><DialogContent frame={false} closeLabel="关闭令牌表单" className="ui-modal wide w-full max-w-2xl"><DialogHeader><DialogTitle>{token ? '编辑令牌' : '创建令牌'}</DialogTitle></DialogHeader><DialogBody className="space-y-5 overflow-y-auto">
    <form id="api-token-form" className="space-y-5" onSubmit={(event) => { event.preventDefault(); save.mutate(); }}><fieldset disabled={save.isPending || uncertain} className="space-y-5"><div className="grid gap-4 sm:grid-cols-2"><TextField label="名称" value={name} onChange={(e) => { setName(e.target.value); setDirty(true); }} maxLength={128} required /><ChoiceField label="用途模板" value={template} options={Object.entries(templates).map(([key, value]) => [key, value.name])} onChange={selectTemplate} /></div>
      <div className="space-y-4">{modules.map(([module, label]) => { const p = permissions[module]; const choices: [string, string][] = p.scope === 'nodes' ? nodes.data?.items.filter((n) => !n.revoked_at).map((n) => [n.id, n.name]) ?? [] : p.scope === 'regions' ? [...new Set(nodes.data?.items.filter((n) => !n.revoked_at).map((n) => n.region).filter((v): v is string => Boolean(v)))].map((r) => [r, r]) : databases.data?.items.map((c) => [c.cluster.id, c.cluster.name]) ?? [];
        return <section key={module} className="space-y-3 rounded-md border border-border p-4"><div className="grid gap-4 sm:grid-cols-2"><ChoiceField label={label} value={p.level} options={module === 'monitoring' ? [['none', '未授权'], ['read', '读取']] : [['none', '未授权'], ['read', '读取'], ['manage', module === 'tasks' ? '管理（任意 root Shell）' : '管理']]} onChange={(level) => change(module, { level: level as Level })} />{p.level !== 'none' && <ChoiceField label={`${label}范围`} value={p.scope} options={module === 'databases' ? [['all', '全部集群'], ['clusters', '指定集群']] : [['all', '全部节点'], ['nodes', '指定节点'], ['regions', '指定地域']]} onChange={(scope) => change(module, { scope: scope as Grant['scope'], resources: [] })} />}</div>{p.level !== 'none' && p.scope !== 'all' && <div className="grid max-h-44 gap-2 overflow-y-auto sm:grid-cols-2">{choices.length ? choices.map(([value, title]) => <label key={value} className="flex min-w-0 items-center gap-2 text-sm"><Checkbox checked={p.resources.includes(value)} onCheckedChange={(checked) => change(module, { resources: checked ? [...p.resources, value] : p.resources.filter((v) => v !== value) })} /><span className="min-w-0 break-words">{title}</span></label>) : <p className="text-sm text-foreground-muted">暂无资源</p>}</div>}</section>;
      })}</div>
      <Collapsible><CollapsibleTrigger className={buttonVariants({ variant: 'ghost', size: 'sm' })}>可选设置</CollapsibleTrigger><CollapsibleContent className="space-y-4 pt-4"><TextField label="到期时间" type="datetime-local" value={expires} onChange={(e) => { setExpires(e.target.value); setDirty(true); }} /><Field className="space-y-2"><FieldLabel htmlFor={sourceID}>来源 IP／CIDR（每行一个）</FieldLabel><Textarea id={sourceID} rows={3} value={ips} onChange={(e) => { setIPs(e.target.value); setDirty(true); }} /></Field></CollapsibleContent></Collapsible>
    </fieldset><ContinueReading query={databases}/><Failure error={locate.error ?? nodes.error ?? databases.error ?? read.error}/>{uncertain ? <Failure error={save.error}/> : <CommandError error={save.error} read={token ? () => read.mutate() : undefined}/>}{receipt && <p role="status" className="text-sm">{receipt}</p>}{uncertain && !token && <BusyButton type="button" variant="outline" busy={locate.isPending} onClick={() => locate.mutate()}>查询原请求</BusyButton>}{uncertain && <p role="status" className="text-sm">请刷新令牌列表，核对本次结果。</p>}</form>
  </DialogBody><DialogFooter><Button variant="outline" disabled={save.isPending} onClick={onClose}>关闭</Button><BusyButton form="api-token-form" type="submit" busy={save.isPending} disabled={uncertain}>{token ? '保存' : '生成令牌'}</BusyButton></DialogFooter></DialogContent></Dialog>;
}

function CopyValue({ label, value, rows = 3 }: { label: string; value: string; rows?: number }) {
  const id = useId(); const ref = useRef<HTMLTextAreaElement>(null); const [copied, setCopied] = useState(false); const [error, setError] = useState<Error | null>(null);
  const copy = async () => { try { try { if (!navigator.clipboard) throw new Error(); await navigator.clipboard.writeText(value); } catch { ref.current?.select(); if (!document.execCommand('copy')) throw new Error(); } setCopied(true); setError(null); } catch { setError(new Error('复制失败，请手动选择并复制')); } };
  return <Field className="space-y-2"><div className="flex items-center justify-between gap-3"><FieldLabel htmlFor={id}>{label}</FieldLabel><Button size="sm" variant="outline" onClick={() => void copy()}>{copied ? '已复制' : '复制'}</Button></div><Textarea ref={ref} id={id} value={value} readOnly rows={rows} className="font-mono text-xs" /><Failure error={error} /></Field>;
}
function TokenResult({ value, onClose }: { value: Created; onClose: () => void }) {
  const address = `${window.location.origin}/api/v2`;
  const command = `curl -fsS '${address}/' -H 'Authorization: Bearer ${value.value}'`;
  const prometheus = `scrape_configs:\n  - job_name: opsd\n    scrape_interval: 15s\n    scheme: ${window.location.protocol.slice(0, -1)}\n    metrics_path: /metrics\n    static_configs:\n      - targets: ['${window.location.host}']\n    authorization:\n      type: Bearer\n      credentials: '${value.value}'`;
  return <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}><DialogContent frame={false} closeLabel="关闭接入配置" className="ui-modal wide w-full max-w-2xl"><DialogHeader><DialogTitle>接入配置</DialogTitle></DialogHeader><DialogBody className="space-y-5 overflow-y-auto"><CopyValue label="令牌（仅显示一次）" value={value.value} rows={3} /><CopyValue label="API 地址" value={address} rows={1} /><CopyValue label="验证命令" value={command} />{value.token.grants.some((g) => ['monitoring', 'containers', 'databases'].includes(g.module)) && <CopyValue label="Prometheus 配置" value={prometheus} rows={10} />}</DialogBody><DialogFooter><Button onClick={onClose}>完成</Button></DialogFooter></DialogContent></Dialog>;
}
