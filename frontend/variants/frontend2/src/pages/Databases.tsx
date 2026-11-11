import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router';
import { Button, buttonVariants } from '@appica/ui-react/button';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogBody, DialogFooter } from '@appica/ui-react/dialog';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@appica/ui-react/tabs';
import { Checkbox } from '@appica/ui-react/checkbox';
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from '@appica/ui-react/dropdown-menu';
import { api, RequestError, unwrap } from '../api/client';
import type { components } from '../api/schema.gen';
import { queryClient } from '../state';
import { useOperations, invalidateOperations } from '../operations';
import { bytes, percent } from '../monitoring';
import { BusyButton, ChoiceField, Confirm, date, Failure, LoadingRows, newRequestKey, PageTitle, StatusLabel, TextField, useUnsavedChanges } from '../ui';
import { nodesQuery } from './Nodes';
import { DatabaseSetup } from './DatabaseSetup';

type Status = components['schemas']['ManagedDatabaseStatus'];
type Member = components['schemas']['ManagedDatabaseMember'];
type User = components['schemas']['DatabaseUser'];
type Change = components['schemas']['DatabaseChange'];
type Kind = Change['kind'];

const actions: Record<Kind, string> = {
  create_database: '创建库',
  delete_database: '删除库',
  create_user: '创建账号',
  password: '修改密码',
  delete_user: '删除账号',
  grant: '授权',
  revoke: '撤权',
  sync_user: '同步代理',
};

const stateNames: Record<string, string> = {
  ready: '可用',
  partial: '部分可用',
  unavailable: '不可用',
  mismatch: '关联不一致',
  queued: '等待读取',
  reading: '读取中',
  stale: '待更新',
  timeout: '读取超时',
  error: '读取失败',
  offline: '节点离线',
};

const allPrivileges = ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP', 'INDEX', 'ALTER', 'EXECUTE', 'CREATE VIEW', 'SHOW VIEW', 'TRIGGER', 'EVENT'];
const privilegeSets: Record<string, string[]> = {
  read: ['SELECT'],
  write: ['SELECT', 'INSERT', 'UPDATE', 'DELETE'],
  manage: allPrivileges,
};

function serviceState(value: Status) {
  const count = value.members.filter((m) => m.status.state === 'ready').length;
  return count === value.members.length && count > 0 ? 'ready' : count ? 'partial' : 'unavailable';
}

function useVisible() {
  const [visible, setVisible] = useState(document.visibilityState !== 'hidden');
  useEffect(() => {
    const update = () => setVisible(document.visibilityState !== 'hidden');
    document.addEventListener('visibilitychange', update);
    return () => document.removeEventListener('visibilitychange', update);
  }, []);
  return visible;
}

export function Databases() {
  const visible = useVisible();
  const query = useQuery({
    queryKey: ['databases', 'list'],
    queryFn: async ({ signal }) => unwrap(await api.GET('/databases/clusters', { signal })),
    refetchInterval: (q) => !visible ? false : q.state.data?.items.some((c) => c.members.some((m) => ['queued', 'reading'].includes(m.status.state))) ? 500 : 10000,
  });
  const [setup, setSetup] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const refresh = async () => {
    setRefreshing(true);
    try { await query.refetch(); } finally { setRefreshing(false); }
  };

  return (
    <div className="space-y-4">
      <PageTitle>数据库</PageTitle>
      <section className="panel space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-base font-semibold text-foreground-intense">集群列表</h2>
          <div className="flex flex-wrap items-center gap-2.5">
            <BusyButton variant="outline" size="sm" busy={refreshing} disabled={query.isFetching} onClick={() => void refresh()}>刷新</BusyButton>
            <Button size="sm" onClick={() => setSetup(true)}>接入集群</Button>
          </div>
        </div>
        <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
        <div className="max-w-full overflow-x-auto" aria-busy={query.isPending}>
          <Table hoverableRows className="min-w-144">
            <TableHeader>
              <TableRow>
                <TableHead>集群</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>成员</TableHead>
                <TableHead>代理</TableHead>
                <TableHead className="text-right">连接数</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.isPending ? (
                <LoadingRows columns={6} />
              ) : (
                query.data?.items.map((item) => {
                  const state = serviceState(item);
                  return (
                    <TableRow key={item.cluster.id}>
                      <TableCell>
                        <Link to={`/databases/${item.cluster.id}`} className="block max-w-64 break-words font-medium text-foreground-intense hover:text-primary hover:underline">
                          {item.cluster.name}
                        </Link>
                      </TableCell>
                      <TableCell><StatusLabel state={state}>{stateNames[state]}</StatusLabel></TableCell>
                      <TableCell className="tabular-nums">{item.members.filter((m) => m.status.state === 'ready').length} / {item.members.length}</TableCell>
                      <TableCell className="tabular-nums">{item.proxies.filter((p) => p.status.state === 'ready').length} / {item.proxies.length}</TableCell>
                      <TableCell className="text-right tabular-nums">
                        {item.members.filter((m) => m.status.version).length ? item.members.reduce((n, m) => n + (m.status.connections ?? 0), 0) : '—'}
                      </TableCell>
                      <TableCell className="text-right">
                        <Link to={`/databases/${item.cluster.id}`} className={buttonVariants({ variant: 'ghost', size: 'sm' })}>查看</Link>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
              {query.data?.items.length === 0 && !query.isError && (
                <TableRow>
                  <TableCell colSpan={6} className="py-10 text-center text-foreground-muted">暂无数据库集群</TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      </section>
      {setup && <DatabaseSetup onClose={() => setSetup(false)} />}
    </div>
  );
}

export function DatabaseDetail() {
  const { id = '' } = useParams();
  const visible = useVisible();
  const nodes = useQuery(nodesQuery);
  const [tab, setTab] = useState('overview');
  const [setup, setSetup] = useState(false);
  const [edit, setEdit] = useState<Partial<Change> & { kind: Kind } | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const view = (['schemas', 'users', 'proxies'].includes(tab) ? tab : 'health') as 'health' | 'schemas' | 'users' | 'proxies';
  const query = useQuery({
    queryKey: ['databases', 'detail', id, view],
    queryFn: async ({ signal }) => unwrap(await api.GET('/databases/clusters/{id}', { signal, params: { path: { id }, query: { view } } })),
    refetchInterval: (q) => !visible ? false : q.state.data?.members.some((m) => ['queued', 'reading'].includes(m.status.state)) ? 500 : 10000,
  });
  const names = new Map(nodes.data?.items.map((node) => [node.id, node.name]) ?? []);
  const value = query.data;
  const definition = value?.cluster;
  const source = value?.members.find((m) => m.node_id === definition?.coordinator && m.status.version) ?? value?.members.find((m) => m.status.state === 'ready');
  const dataLoading = query.isPending || (!source && value?.members.some((m) => ['queued', 'reading'].includes(m.status.state)));
  const writable = value?.members.some((m) => m.node_id === definition?.coordinator && m.status.state === 'ready' && !m.status.read_only);
  const refresh = async () => {
    setRefreshing(true);
    try { await query.refetch(); } finally { setRefreshing(false); }
  };

  return (
    <div className="space-y-4">
      <PageTitle>数据库详情</PageTitle>
      <section className="panel space-y-5">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex min-w-0 flex-wrap items-center gap-3">
            <Link to="/databases" className="text-sm text-foreground-muted hover:text-foreground">数据库</Link>
            <span className="text-foreground-subtle">/</span>
            <h2 className="break-all text-lg font-semibold text-foreground-intense">{definition?.name ?? '数据库'}</h2>
            {value && <StatusLabel state={serviceState(value)}>{stateNames[serviceState(value)]}</StatusLabel>}
          </div>
          <div className="flex flex-wrap gap-2.5">
            <BusyButton variant="outline" size="sm" busy={refreshing} disabled={query.isFetching} onClick={() => void refresh()}>刷新</BusyButton>
            <Button variant="outline" size="sm" disabled={!definition} onClick={() => setSetup(true)}>编辑集群</Button>
            {definition?.workbench_url && (
              <a href={definition.workbench_url} target="_blank" rel="noreferrer" className={buttonVariants({ variant: 'outline', size: 'sm' })}>
                SQL 工作台
              </a>
            )}
          </div>
        </div>
        <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
        <Tabs variant="line" className="gap-4" value={tab} onValueChange={(v) => setTab(String(v))}>
          <TabsList aria-label="数据库页面" className="max-w-full justify-start overflow-x-auto">
            {[['overview', '概览'], ['members', '成员'], ['schemas', '库'], ['users', '账号'], ['proxies', '代理'], ['backups', '备份']].map(([key, label]) => (
              <TabsTrigger key={key} value={key}>{label}</TabsTrigger>
            ))}
          </TabsList>

          <TabsContent value="overview" className="space-y-4">
            <div className="grid gap-3.5 sm:grid-cols-2 xl:grid-cols-4">
              {[
                ['成员', value ? `${value.members.filter((m) => m.status.state === 'ready').length} / ${value.members.length}` : '—'],
                ['连接数', value?.members.some((m) => m.status.version) ? value.members.reduce((n, m) => n + (m.status.connections ?? 0), 0) : '—'],
                ['复制队列', value?.members.some((m) => m.status.version) ? value.members.reduce((n, m) => n + (m.status.receive_queue ?? 0) + (m.status.send_queue ?? 0), 0) : '—'],
                ['流控', value?.members.some((m) => m.status.version) ? percent(Math.max(...value.members.map((m) => m.status.flow_control ?? 0)) * 100) : '—'],
              ].map(([label, count]) => (
                <div key={String(label)} className="surface min-w-0 p-4">
                  <div className="text-xs font-medium text-foreground-muted">{label}</div>
                  <div className="mt-2 text-2xl font-semibold tabular-nums text-foreground-intense">{count}</div>
                </div>
              ))}
            </div>
            <dl className="grid gap-4 rounded-lg border border-border bg-surface-subtle p-4 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-xs text-foreground-muted">协调节点</dt>
                <dd className="mt-1 break-all font-medium text-foreground-intense">
                  {definition ? names.get(definition.coordinator) ?? definition.coordinator : '—'}
                </dd>
              </div>
              <div>
                <dt className="text-xs text-foreground-muted">检查时间</dt>
                <dd className="mt-1 tabular-nums text-foreground-intense">{date(value?.checked_at)}</dd>
              </div>
            </dl>
            {value?.members.filter((m) => m.status.state !== 'ready').map((member) => (
              <p key={member.instance_id} className={`break-words text-sm ${member.status.error ? 'text-error-intense' : 'text-foreground-muted'}`}>
                {names.get(member.node_id) ?? member.node_id} · {member.status.error?.message ?? stateNames[member.status.state]}
              </p>
            ))}
          </TabsContent>

          <TabsContent value="members">
            <MemberTable members={value?.members} names={names} loading={query.isPending} />
          </TabsContent>

          <TabsContent value="schemas">
            <div className="mb-4 flex justify-end">
              <Button size="sm" disabled={!writable} onClick={() => setEdit({ kind: 'create_database', charset: 'utf8mb4' })}>创建库</Button>
            </div>
            <div className="max-w-full overflow-x-auto">
              <Table hoverableRows className="min-w-128">
                <TableHeader>
                  <TableRow>
                    <TableHead>库</TableHead>
                    <TableHead>字符集</TableHead>
                    <TableHead>排序规则</TableHead>
                    <TableHead className="text-right">容量</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {dataLoading ? (
                    <LoadingRows columns={5} />
                  ) : (
                    source?.status.databases?.map((schema) => (
                      <TableRow key={schema.name}>
                        <TableCell className="font-medium text-foreground-intense">{schema.name}</TableCell>
                        <TableCell>{schema.charset}</TableCell>
                        <TableCell>{schema.collation}</TableCell>
                        <TableCell className="text-right tabular-nums">{bytes(schema.bytes)}</TableCell>
                        <TableCell className="text-right">
                          {schema.protected ? (
                            <span className="text-xs text-foreground-muted">系统库</span>
                          ) : (
                            <Button variant="ghost" size="sm" disabled={!writable} onClick={() => setEdit({ kind: 'delete_database', database: schema.name })}>删除</Button>
                          )}
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                  {!source && !dataLoading && (
                    <TableRow>
                      <TableCell colSpan={5} className="py-8 text-center text-foreground-muted">库列表不可用</TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>
          </TabsContent>

          <TabsContent value="users">
            <div className="mb-4 flex justify-end">
              <Button size="sm" disabled={!writable} onClick={() => setEdit({ kind: 'create_user', host: '%' })}>创建账号</Button>
            </div>
            <div className="max-w-full overflow-x-auto">
              <Table hoverableRows className="min-w-144">
                <TableHeader>
                  <TableRow>
                    <TableHead>账号</TableHead>
                    <TableHead>来源</TableHead>
                    <TableHead>库级权限</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {dataLoading ? (
                    <LoadingRows columns={4} />
                  ) : (
                    source?.status.users?.map((user) => (
                      <TableRow key={`${user.name}@${user.host}`}>
                        <TableCell className="font-medium text-foreground-intense">{user.name}</TableCell>
                        <TableCell className="font-mono text-xs">{user.host}</TableCell>
                        <TableCell className="max-w-80 break-words text-xs">
                          {user.grants.length ? user.grants.map((g) => <div key={g.database}>{g.database} · {g.privileges.join(', ')}</div>) : '—'}
                        </TableCell>
                        <TableCell className="text-right">
                          {user.protected ? (
                            <span className="text-xs text-foreground-muted">受保护</span>
                          ) : (
                            <UserActions user={user} disabled={!writable} onAction={(kind) => setEdit({ kind, user: user.name, host: user.host })} />
                          )}
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                  {!source && !dataLoading && (
                    <TableRow>
                      <TableCell colSpan={4} className="py-8 text-center text-foreground-muted">账号列表不可用</TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>
          </TabsContent>

          <TabsContent value="proxies">
            <div className="space-y-4">
              {value?.proxies.map((proxy) => (
                <section key={`${proxy.node_id}/${proxy.instance_id}`} className="surface space-y-3 p-4">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <h3 className="font-semibold text-foreground-intense">{names.get(proxy.node_id) ?? proxy.node_id} · {proxy.name}</h3>
                    <StatusLabel state={proxy.status.state}>{stateNames[proxy.status.state]}</StatusLabel>
                  </div>
                  <div className="break-all font-mono text-xs text-foreground-muted">{proxy.address}</div>
                  {proxy.status.error && <p className="text-sm text-error-intense">{proxy.status.error.message}</p>}
                  {proxy.status.pending_changes && <StatusLabel state="pending">存在待应用配置</StatusLabel>}
                  <div className="max-w-full overflow-x-auto">
                    <Table className="min-w-112">
                      <TableHeader>
                        <TableRow>
                          <TableHead>主机组</TableHead>
                          <TableHead>地址</TableHead>
                          <TableHead>状态</TableHead>
                          <TableHead className="text-right">使用／空闲</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {proxy.status.servers?.map((server) => {
                          const pool = proxy.status.pools?.find((p) => p.hostgroup === server.hostgroup && p.host === server.host && p.port === server.port);
                          return (
                            <TableRow key={`${server.hostgroup}/${server.host}/${server.port}`}>
                              <TableCell className="tabular-nums">{server.hostgroup}</TableCell>
                              <TableCell className="font-mono text-xs">{server.host}:{server.port}</TableCell>
                              <TableCell>{server.state}</TableCell>
                              <TableCell className="text-right tabular-nums">{pool ? `${pool.used} / ${pool.free}` : '—'}</TableCell>
                            </TableRow>
                          );
                        })}
                      </TableBody>
                    </Table>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {proxy.status.proxy_users?.map((user, index) => (
                      <StatusLabel key={`${user.name}/${index}`} state={user.state === 'in_sync' ? 'ready' : 'pending'}>
                        {user.name} · {user.active ? '启用' : '停用'} · {user.state === 'in_sync' ? '已同步' : '待同步'}
                      </StatusLabel>
                    ))}
                  </div>
                </section>
              ))}
              {value?.proxies.length === 0 && <p className="py-8 text-center text-sm text-foreground-muted">暂无代理</p>}
            </div>
          </TabsContent>

          <TabsContent value="backups">
            {tab === 'backups' && <DatabaseBackups id={id} names={names} />}
          </TabsContent>
        </Tabs>
      </section>
      {setup && definition && <DatabaseSetup cluster={definition} onClose={() => setSetup(false)} />}
      {edit && value && <DatabaseChangeForm value={value} initial={edit} onClose={() => setEdit(null)} />}
    </div>
  );
}

function MemberTable({ members, names, loading }: { members?: Member[]; names: Map<string, string>; loading: boolean }) {
  return (
    <div className="max-w-full overflow-x-auto">
      <Table hoverableRows className="min-w-176">
        <TableHeader>
          <TableRow>
            <TableHead>成员</TableHead>
            <TableHead>配置地址</TableHead>
            <TableHead>实际成员</TableHead>
            <TableHead>版本</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>检查时间</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <LoadingRows columns={6} />
          ) : (
            members?.map((member) => (
              <TableRow key={`${member.node_id}/${member.instance_id}`}>
                <TableCell className="font-medium text-foreground-intense">{names.get(member.node_id) ?? member.node_id}</TableCell>
                <TableCell>
                  <div className="max-w-64 break-all font-mono text-xs">
                    {member.address}
                    {member.status.configured_members && <div className="mt-1 text-foreground-muted">{member.status.configured_members}</div>}
                  </div>
                </TableCell>
                <TableCell className="max-w-64 break-all font-mono text-xs">{member.status.actual_members ?? '—'}</TableCell>
                <TableCell className="whitespace-nowrap">{member.status.version ?? '—'}</TableCell>
                <TableCell>
                  <StatusLabel state={member.status.state}>{stateNames[member.status.state]}</StatusLabel>
                  {member.status.error && <div className="mt-1 max-w-56 text-xs text-error-intense">{member.status.error.message}</div>}
                </TableCell>
                <TableCell className="whitespace-nowrap text-xs tabular-nums">{date(member.status.read?.data_at)}</TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}

function UserActions({ user, disabled, onAction }: { user: User; disabled: boolean; onAction: (kind: Kind) => void }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger className={buttonVariants({ variant: 'ghost', size: 'sm' })} disabled={disabled} aria-label={`${user.name} 的操作`}>更多</DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {(['password', 'grant', 'revoke', 'sync_user', 'delete_user'] as const).map((kind) => (
          <DropdownMenuItem key={kind} className={kind === 'delete_user' ? 'text-error-intense' : undefined} onClick={() => onAction(kind)}>
            {actions[kind]}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function DatabaseBackups({ id, names }: { id: string; names: Map<string, string> }) {
  const query = useQuery({
    queryKey: ['databases', id, 'backups'],
    queryFn: async ({ signal }) => unwrap(await api.GET('/databases/clusters/{id}/backups', { signal, params: { path: { id } } })),
  });
  const states: Record<string, string> = {
    succeeded: '最近执行成功',
    failed: '最近执行失败',
    not_configured: '未配置',
    unknown: '未知',
  };

  return (
    <div className="space-y-4">
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      {query.data?.items.map((node) => (
        <section key={`${node.node_id}/${node.instance_id}`} className="surface space-y-3 p-4">
          <h3 className="font-semibold text-foreground-intense">{names.get(node.node_id) ?? node.node_id}</h3>
          {node.error && <p className="text-sm text-error-intense">{node.error}</p>}
          <div className="max-w-full overflow-x-auto">
            <Table className="min-w-176">
              <TableHeader>
                <TableRow>
                  <TableHead>计划</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>最近执行</TableHead>
                  <TableHead>最后成功</TableHead>
                  <TableHead>产物</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {node.items.map((backup) => (
                  <TableRow key={backup.name}>
                    <TableCell>
                      <div className="font-medium text-foreground-intense">{({ hourly: '每小时', daily: '每日', weekly: '每周' } as Record<string, string>)[backup.name] ?? backup.name}</div>
                      <div className="mt-0.5 max-w-56 break-all text-xs text-foreground-muted">{backup.schedule}</div>
                    </TableCell>
                    <TableCell>
                      <StatusLabel state={backup.state}>{states[backup.state] ?? backup.state}</StatusLabel>
                      {backup.error && <p className="mt-1 max-w-56 break-words text-xs text-error-intense">{backup.error}</p>}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-xs tabular-nums">{date(backup.last_run)}</TableCell>
                    <TableCell className="whitespace-nowrap text-xs tabular-nums">{date(backup.last_success)}</TableCell>
                    <TableCell className="max-w-80 break-all font-mono text-xs">
                      {backup.artifacts.map((artifact) => <div key={artifact}>{artifact}</div>)}
                    </TableCell>
                  </TableRow>
                ))}
                {!node.items.length && (
                  <TableRow>
                    <TableCell colSpan={5} className="py-6 text-center text-foreground-muted">暂无备份计划</TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
        </section>
      ))}
      {query.isPending && <p role="status" className="py-8 text-center text-sm text-foreground-muted">读取备份状态中</p>}
    </div>
  );
}

function DatabaseChangeForm({ value, initial, onClose }: { value: Status; initial: Partial<Change> & { kind: Kind }; onClose: () => void }) {
  const kind = initial.kind;
  const [draft, setDraft] = useState<Change>({ expected_revision: value.cluster.revision, proxies: [], ...initial });
  const [grantDatabase, setGrantDatabase] = useState('');
  const [grantLevel, setGrantLevel] = useState('read');
  const [selected, setSelected] = useState<components['schemas']['DatabaseProxySelection'][]>(
    kind === 'password' || kind === 'delete_user'
      ? value.proxies
          .filter((p) => p.status.proxy_users?.some((u) => u.name === initial.user))
          .map((p) => ({
            node_id: p.node_id,
            instance_id: p.instance_id,
            hostgroup: p.status.proxy_users?.find((u) => u.name === initial.user)?.hostgroup ?? 0,
          }))
      : [],
  );
  const [dirty, setDirty] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const key = useRef(newRequestKey());
  const operations = useOperations();
  useUnsavedChanges(dirty);

  const changed = (next: Partial<Change>) => {
    setDraft({ ...draft, ...next });
    setDirty(true);
  };
  const params = { path: { id: value.cluster.id }, header: { 'Idempotency-Key': key.current } };
  const completed = (op: components['schemas']['Operation']) => {
    setDirty(false);
    invalidateOperations();
    void queryClient.invalidateQueries({ queryKey: ['databases'] });
    onClose();
    operations.show(op.id);
  };
  const save = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      const body: Change = {
        ...draft,
        proxies: selected,
        ...(kind === 'grant' || kind === 'revoke' ? { database: grantDatabase, privileges: privilegeSets[grantLevel] } : {}),
        ...(kind === 'create_user' && grantDatabase ? { grants: [{ database: grantDatabase, privileges: privilegeSets[grantLevel] }] } : {}),
      };
      return unwrap(await api.POST('/databases/clusters/{id}/changes', { params, body }));
    },
    onSuccess: completed,
    onError: (error) => {
      setConfirm(false);
      if (!(error instanceof RequestError && [403, 409, 422].includes(error.status))) setUncertain(true);
    },
  });
  const locate = useMutation({
    mutationFn: async () => {
      const result = unwrap(await api.GET('/operations', { params: { query: { node_id: value.cluster.coordinator, idempotency_key: key.current } } }));
      if (result.unavailable?.length) throw new Error('协调节点暂不可达');
      return result.items[0];
    },
    onSuccess: (op) => {
      if (op) completed(op);
      else {
        setUncertain(false);
        save.reset();
      }
    },
  });
  const schemasQuery = useQuery({
    queryKey: ['databases', 'detail', value.cluster.id, 'schemas'],
    queryFn: async ({ signal }) => unwrap(await api.GET('/databases/clusters/{id}', { signal, params: { path: { id: value.cluster.id }, query: { view: 'schemas' } } })),
    refetchInterval: (q) => q.state.data?.members.some((m) => ['queued', 'reading'].includes(m.status.state)) ? 500 : false,
  });
  const proxiesQuery = useQuery({
    queryKey: ['databases', 'detail', value.cluster.id, 'proxies'],
    queryFn: async ({ signal }) => unwrap(await api.GET('/databases/clusters/{id}', { signal, params: { path: { id: value.cluster.id }, query: { view: 'proxies' } } })),
    refetchInterval: (q) => q.state.data?.proxies.some((m) => ['queued', 'reading'].includes(m.status.state)) ? 500 : false,
  });
  const schemaMembers = schemasQuery.data?.members ?? value.members;
  const source = schemaMembers.find((m) => m.node_id === value.cluster.coordinator) ?? schemaMembers[0];
  const proxies = proxiesQuery.data?.proxies ?? value.proxies;
  const databases = source?.status.databases?.filter((d) => !d.protected) ?? [];
  const userAction = !['create_database', 'delete_database'].includes(kind);
  const needsPassword = kind === 'create_user' || kind === 'password';
  const needsGrant = kind === 'create_user' || kind === 'grant' || kind === 'revoke';
  const needsProxy = ['create_user', 'password', 'delete_user', 'sync_user'].includes(kind);
  const dangerous = kind === 'delete_database' || kind === 'delete_user';

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !save.isPending) onClose(); }}>
      <DialogContent className="w-full max-w-xl" closeLabel="关闭数据库操作">
        <DialogHeader><DialogTitle>{actions[kind]}</DialogTitle></DialogHeader>
        <DialogBody className="space-y-4 overflow-y-auto">
          <form
            id="database-change"
            className="space-y-5"
            onSubmit={(event) => {
              event.preventDefault();
              if (dangerous) setConfirm(true);
              else save.mutate();
            }}
          >
            <fieldset disabled={save.isPending || uncertain || schemasQuery.isPending || proxiesQuery.isPending} className="space-y-4">
              {userAction ? (
                <div className="grid gap-4 sm:grid-cols-2">
                  <TextField label="账号" required readOnly={kind !== 'create_user'} value={draft.user ?? ''} onChange={(e) => changed({ user: e.target.value })} />
                  <TextField label="来源" required readOnly={kind !== 'create_user'} value={draft.host ?? '%'} onChange={(e) => changed({ host: e.target.value })} />
                </div>
              ) : (
                <TextField label="库名" required readOnly={kind === 'delete_database'} value={draft.database ?? ''} onChange={(e) => changed({ database: e.target.value })} />
              )}
              {kind === 'create_database' && (
                <ChoiceField
                  label="字符集"
                  value={draft.charset ?? 'utf8mb4'}
                  options={['utf8mb4', 'utf8mb3', 'latin1', 'ascii'].map((v) => [v, v] as const)}
                  onChange={(charset) => changed({ charset: charset as Change['charset'] })}
                />
              )}
              {needsPassword && (
                <TextField label="密码" required type="password" autoComplete="new-password" minLength={8} maxLength={1024} value={draft.password ?? ''} onChange={(e) => changed({ password: e.target.value })} />
              )}
              {needsGrant && (
                <div className="grid gap-4 sm:grid-cols-2">
                  <ChoiceField
                    label="业务库"
                    value={grantDatabase}
                    options={[...(kind === 'create_user' ? [['', '暂不授权'] as const] : []), ...databases.map((d) => [d.name, d.name] as const)]}
                    onChange={(name) => { setGrantDatabase(name); setDirty(true); }}
                  />
                  <ChoiceField
                    label="库级权限"
                    value={grantLevel}
                    options={Object.entries({ read: '读取', write: '读写', manage: '管理' })}
                    onChange={(level) => { setGrantLevel(level); setDirty(true); }}
                  />
                </div>
              )}
              {needsProxy && (
                <section className="space-y-3">
                  <h3 className="text-sm font-medium text-foreground-intense">ProxySQL</h3>
                  {proxies.map((proxy) => {
                    const chosen = selected.find((p) => p.node_id === proxy.node_id && p.instance_id === proxy.instance_id);
                    return (
                      <div key={`${proxy.node_id}/${proxy.instance_id}`} className="flex flex-wrap items-end gap-3 rounded-lg border border-border bg-surface-subtle p-3">
                        <label className="flex h-10 min-w-0 basis-full items-center gap-2 text-sm sm:basis-auto sm:flex-1">
                          <Checkbox
                            checked={Boolean(chosen)}
                            onCheckedChange={(checked) => {
                              setSelected(
                                checked
                                  ? [
                                      ...selected,
                                      {
                                        node_id: proxy.node_id,
                                        instance_id: proxy.instance_id,
                                        hostgroup: proxy.status.servers?.length ? Math.min(...proxy.status.servers.map((s) => s.hostgroup)) : 0,
                                      },
                                    ]
                                  : selected.filter((p) => p !== chosen),
                              );
                              setDirty(true);
                            }}
                          />
                          <span className="min-w-0 break-words">{proxy.name}</span>
                        </label>
                        {chosen && kind !== 'delete_user' && (
                          <div className="w-28">
                            <TextField
                              label="主机组"
                              type="number"
                              min={0}
                              required
                              value={chosen.hostgroup}
                              onChange={(e) => {
                                setSelected(selected.map((p) => (p === chosen ? { ...p, hostgroup: Number(e.target.value) } : p)));
                                setDirty(true);
                              }}
                            />
                          </div>
                        )}
                      </div>
                    );
                  })}
                  {proxies.length === 0 && <p className="text-sm text-foreground-muted">暂无代理</p>}
                </section>
              )}
            </fieldset>
            <Failure error={save.error ?? locate.error ?? schemasQuery.error ?? proxiesQuery.error} />
          </form>
        </DialogBody>
        <DialogFooter>
          <Button variant="outline" disabled={save.isPending} onClick={onClose}>关闭</Button>
          {uncertain ? (
            <BusyButton busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton>
          ) : (
            <BusyButton
              type="submit"
              form="database-change"
              variant={dangerous ? 'destructive' : 'primary'}
              busy={save.isPending}
              disabled={((kind === 'grant' || kind === 'revoke') && !grantDatabase) || (kind === 'sync_user' && !selected.length)}
            >
              {actions[kind]}
            </BusyButton>
          )}
        </DialogFooter>
      </DialogContent>
      <Confirm
        open={confirm}
        title={`${actions[kind]}？`}
        description={kind === 'delete_database' ? `删除 ${draft.database} 及其中全部表和数据。` : `删除 ${draft.user}@${draft.host}，并停用所选代理入口。`}
        confirmLabel={actions[kind]}
        busy={save.isPending}
        error={save.error}
        onCancel={() => setConfirm(false)}
        onConfirm={() => save.mutate()}
      />
    </Dialog>
  );
}
