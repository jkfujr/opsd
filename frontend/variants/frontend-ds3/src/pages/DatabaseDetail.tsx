import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router';
import { api, idempotency, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { bytes, stamp } from '../lib/format';
import { databaseStates } from '../lib/labels';
import {
  Button,
  Confirm,
  Dialog,
  Empty,
  Failure,
  Led,
  Notice,
  PageHead,
  Select,
  Sheet,
  Skeleton,
  Stamp,
  Tabs,
  TextInput,
} from '../components/ui';

type Change = Schema['DatabaseChange'];

const tabs = [
  { id: 'overview', label: '概览' },
  { id: 'members', label: '成员' },
  { id: 'schemas', label: '库' },
  { id: 'users', label: '账号' },
  { id: 'proxies', label: '代理' },
  { id: 'backups', label: '备份' },
] as const;

type TabId = (typeof tabs)[number]['id'];

export function DatabaseDetail() {
  const { id = '' } = useParams();
  return <Detail key={id} id={id} />;
}

function Detail({ id }: { id: string }) {
  const [tab, setTab] = useState<TabId>('overview');
  const query = useQuery({
    queryKey: ['databases', 'cluster', id],
    queryFn: async ({ signal }) => value(await api.GET('/databases/clusters/{id}', { signal, params: { path: { id } } })),
    refetchInterval: 15_000,
  });
  const data = query.data;
  const cluster = data?.cluster;
  const primary = data?.members[0]?.status;
  const proxies = useQuery({
    queryKey: ['databases', 'backups', id],
    enabled: tab === 'backups',
    queryFn: async ({ signal }) => value(await api.GET('/databases/clusters/{id}/backups', { signal, params: { path: { id } } })),
  });

  const [creating, setCreating] = useState<'database' | 'user' | null>(null);
  const [passwordFor, setPasswordFor] = useState<Schema['DatabaseUser'] | null>(null);
  const [removing, setRemoving] = useState<{ kind: 'database' | 'user'; name: string; host?: string } | null>(null);

  const change = useMutation({
    mutationFn: async (body: Omit<Change, 'expected_revision' | 'proxies'>) =>
      value(
        await api.POST('/databases/clusters/{id}/changes', {
          params: { path: { id }, header: idempotency() },
          body: {
            ...body,
            expected_revision: cluster?.revision ?? 0,
            proxies: (cluster?.proxies ?? []).map((proxy) => ({ node_id: proxy.node_id, instance_id: proxy.instance_id, hostgroup: 0 })),
          },
        }),
      ),
    onSuccess: () => {
      setCreating(null);
      setPasswordFor(null);
      setRemoving(null);
      void queryClient.invalidateQueries({ queryKey: ['databases'] });
      void queryClient.invalidateQueries({ queryKey: ['operations'] });
    },
  });

  return (
    <div className="page">
      <PageHead
        title={cluster?.name ?? '数据库集群'}
        tools={
          <>
            <Link to="/databases">
              <Button size="sm" icon="arrow">返回列表</Button>
            </Link>
            <Button size="sm" icon="refresh" busy={query.isFetching} onClick={() => void query.refetch()}>
              重新读取
            </Button>
          </>
        }
      />

      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      {query.isPending ? <Skeleton height={220} /> : null}

      {data ? (
        <>
          <Sheet flush>
            <Tabs items={tabs.map((item) => ({ id: item.id, label: item.label }))} value={tab} onChange={(next) => setTab(next as TabId)} label="集群详情" />

            <div className="sheet__body">
              {tab === 'overview' ? (
                <div className="stack gap-16">
                  <section className="metrics" style={{ gridTemplateColumns: 'repeat(4, minmax(0, 1fr))' }}>
                    <article className="metric" style={{ padding: '11px 14px' }}>
                      <span className="kicker">集群连接</span>
                      <div className="metric__value" style={{ fontSize: 22 }}>{data.members.reduce((sum, member) => sum + (member.status.connections ?? 0), 0)}</div>
                      <span className="metric__cover" style={{ marginLeft: 0 }}>{data.members.length} 个成员</span>
                    </article>
                    <article className="metric" style={{ padding: '11px 14px' }}>
                      <span className="kicker">代理连接</span>
                      <div className="metric__value" style={{ fontSize: 22 }}>{data.proxies.reduce((sum, proxy) => sum + (proxy.status.connections ?? 0), 0)}</div>
                      <span className="metric__cover" style={{ marginLeft: 0 }}>{data.proxies.length} 个代理</span>
                    </article>
                    <article className="metric" style={{ padding: '11px 14px' }}>
                      <span className="kicker">接收队列</span>
                      <div className="metric__value" style={{ fontSize: 22 }}>{primary?.receive_queue ?? 0}</div>
                      <span className="metric__cover" style={{ marginLeft: 0 }}>流控 {primary?.flow_control ?? 0}</span>
                    </article>
                    <article className="metric" style={{ padding: '11px 14px' }}>
                      <span className="kicker">业务库</span>
                      <div className="metric__value" style={{ fontSize: 22 }}>{primary?.databases?.filter((schema) => !schema.protected).length ?? 0}</div>
                      <span className="metric__cover" style={{ marginLeft: 0 }}>账号 {primary?.users?.length ?? 0}</span>
                    </article>
                  </section>
                  <div className="node-grid">
                    <div className="stack gap-12">
                      <dl className="facts">
                        <dt>集群</dt>
                        <dd className="mono">{data.cluster.cluster_id}</dd>
                        <dt>修订</dt>
                        <dd className="mono">{data.cluster.revision}</dd>
                        <dt>协调节点</dt>
                        <dd className="mono">{data.members.find((member) => member.node_id === data.cluster.coordinator)?.name ?? data.cluster.coordinator}</dd>
                        <dt>成员</dt>
                        <dd>{data.members.length} 个 · 代理 {data.proxies.length} 个</dd>
                        <dt>版本</dt>
                        <dd className="mono">{primary?.version ?? '—'}</dd>
                        <dt>集群成员</dt>
                        <dd className="mono">{primary?.configured_members || '—'}</dd>
                        <dt>实际成员</dt>
                        <dd className="mono">{primary?.actual_members || '—'}</dd>
                        <dt>工作台</dt>
                        <dd className="mono">{data.cluster.workbench_url || '未登记'}</dd>
                      </dl>
                      {primary?.pending_changes ? <Notice>存在待应用的业务变更。</Notice> : null}
                    </div>
                    <div className="stack gap-10">
                      <span className="kicker">成员状态</span>
                      {data.members.map((member) => (
                        <div key={`${member.node_id}-${member.instance_id}`} className="spread" style={{ paddingBottom: 8, borderBottom: '1px solid var(--rule-soft)' }}>
                          <span className="row gap-8">
                            <Led state={member.status.state === 'ready' ? 'on' : member.status.state === 'stale' ? 'warn' : 'off'} />
                            <b style={{ fontWeight: 500, fontSize: 12.5 }}>{member.node_id}</b>
                            <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{member.instance_id}</span>
                          </span>
                          <span className="row gap-8">
                            {member.status.primary ? <Stamp state="confirmed">主</Stamp> : null}
                            <Stamp state={member.status.state === 'ready' ? 'ready' : member.status.state}>{databaseStates[member.status.state]}</Stamp>
                          </span>
                        </div>
                      ))}
                      {data.proxies.map((proxy) => (
                        <div key={`${proxy.node_id}-${proxy.instance_id}`} className="spread">
                          <span className="row gap-8">
                            <Led state={proxy.status.state === 'ready' ? 'on' : 'off'} />
                            <b style={{ fontWeight: 500, fontSize: 12.5 }}>{proxy.node_id}</b>
                            <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>ProxySQL</span>
                          </span>
                          <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{proxy.status.connections ?? 0} 连接</span>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              ) : null}

              {tab === 'members' ? (
                <div className="table-wrap">
                  <table className="table">
                    <thead>
                      <tr>
                        <th scope="col">节点</th>
                        <th scope="col">实例</th>
                        <th scope="col">地址</th>
                        <th scope="col">状态</th>
                        <th scope="col" className="right">连接</th>
                        <th scope="col" className="right">接收队列</th>
                        <th scope="col">同步</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.members.map((member) => (
                        <tr key={`${member.node_id}-${member.instance_id}`}>
                          <td><span className="row gap-6"><Led state={member.status.state === 'ready' ? 'on' : 'warn'} />{member.node_id}</span></td>
                          <td className="mono" style={{ fontSize: 11 }}>{member.instance_id}</td>
                          <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{member.address}</td>
                          <td><Stamp state={member.status.state === 'ready' ? 'ready' : member.status.state}>{databaseStates[member.status.state]}</Stamp></td>
                          <td className="right mono" style={{ fontSize: 11 }}>{member.status.connections ?? '—'}</td>
                          <td className="right mono" style={{ fontSize: 11 }}>{member.status.receive_queue ?? '—'}</td>
                          <td className="mono" style={{ fontSize: 10.5, color: member.status.synced ? 'var(--seal)' : 'var(--amber)' }}>
                            {member.status.synced ? '已同步' : '未同步'}{member.status.read_only ? ' · 只读' : ''}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : null}

              {tab === 'schemas' ? (
                <div className="stack gap-12">
                  <div className="spread">
                    <span className="kicker">业务库</span>
                    <Button size="sm" icon="plus" onClick={() => setCreating('database')}>新建库</Button>
                  </div>
                  <div className="table-wrap">
                    <table className="table">
                      <thead>
                        <tr>
                          <th scope="col">库名</th>
                          <th scope="col">字符集</th>
                          <th scope="col">排序规则</th>
                          <th scope="col" className="right">大小</th>
                          <th scope="col" className="right">操作</th>
                        </tr>
                      </thead>
                      <tbody>
                        {(primary?.databases ?? []).map((schema) => (
                          <tr key={schema.name}>
                            <td className="mono" style={{ fontSize: 11.5 }}>{schema.name}</td>
                            <td className="mono" style={{ fontSize: 10.5 }}>{schema.charset}</td>
                            <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{schema.collation}</td>
                            <td className="right mono" style={{ fontSize: 11 }}>{bytes(schema.bytes)}</td>
                            <td className="right">
                              {schema.protected ? <span className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>只读</span> : (
                                <Button size="sm" variant="ghost" onClick={() => setRemoving({ kind: 'database', name: schema.name })}>删除</Button>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  {!primary?.databases?.length ? <Empty>没有业务库</Empty> : null}
                </div>
              ) : null}

              {tab === 'users' ? (
                <div className="stack gap-12">
                  <div className="spread">
                    <span className="kicker">账号</span>
                    <Button size="sm" icon="plus" onClick={() => setCreating('user')}>新建账号</Button>
                  </div>
                  <div className="table-wrap">
                    <table className="table">
                      <thead>
                        <tr>
                          <th scope="col">账号</th>
                          <th scope="col">来源</th>
                          <th scope="col">权限</th>
                          <th scope="col" className="right">操作</th>
                        </tr>
                      </thead>
                      <tbody>
                        {(primary?.users ?? []).map((user) => (
                          <tr key={`${user.name}@${user.host}`}>
                            <td className="mono" style={{ fontSize: 11.5 }}>{user.name}</td>
                            <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{user.host}</td>
                            <td style={{ fontSize: 11, color: 'var(--ink-soft)' }}>
                              {user.grants.length ? user.grants.map((grant) => `${grant.database}:${grant.privileges.join('/')}`).join(' · ') : '—'}
                            </td>
                            <td className="right">
                              {user.protected ? <span className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>只读</span> : (
                                <span className="row gap-2" style={{ justifyContent: 'flex-end' }}>
                                  <Button size="sm" variant="ghost" onClick={() => setPasswordFor(user)}>改密码</Button>
                                  <Button size="sm" variant="ghost" onClick={() => setRemoving({ kind: 'user', name: user.name, host: user.host })}>删除</Button>
                                </span>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  {!primary?.users?.length ? <Empty>没有可管理账号</Empty> : null}
                </div>
              ) : null}

              {tab === 'proxies' ? (
                <div className="stack gap-16">
                  {data.proxies.map((proxy) => {
                    const status = proxy.status;
                    return (
                      <div key={`${proxy.node_id}-${proxy.instance_id}`} className="stack gap-10">
                        <div className="spread">
                          <span className="row gap-8">
                            <Led state={status.state === 'ready' ? 'on' : 'off'} />
                            <b style={{ fontWeight: 500 }}>{proxy.node_id}</b>
                            <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{proxy.instance_id} · {status.version ?? '—'}</span>
                          </span>
                          <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{status.connections ?? 0} 连接</span>
                        </div>
                        <table className="table tiny-table">
                          <thead>
                            <tr>
                              <th scope="col">主机组</th>
                              <th scope="col">后端</th>
                              <th scope="col">状态</th>
                              <th scope="col" className="right">已用</th>
                              <th scope="col" className="right">空闲</th>
                              <th scope="col" className="right">错误</th>
                            </tr>
                          </thead>
                          <tbody>
                            {(status.pools ?? []).map((pool) => (
                              <tr key={`${pool.hostgroup}-${pool.host}-${pool.port}`}>
                                <td className="mono">{pool.hostgroup}</td>
                                <td className="mono" style={{ fontSize: 10.5 }}>{pool.host}:{pool.port}</td>
                                <td className="mono" style={{ fontSize: 10.5, color: 'var(--seal)' }}>ONLINE</td>
                                <td className="right mono" style={{ fontSize: 11 }}>{pool.used}</td>
                                <td className="right mono" style={{ fontSize: 11 }}>{pool.free}</td>
                                <td className="right mono" style={{ fontSize: 11 }}>{pool.errors}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    );
                  })}
                  {data.proxies.length === 0 ? <Empty hint="接入时勾选 ProxySQL 实例">没有代理</Empty> : null}
                </div>
              ) : null}

              {tab === 'backups' ? (
                <div className="stack gap-14">
                  <Failure error={proxies.error} retry={() => void proxies.refetch()} busy={proxies.isFetching} />
                  {proxies.isPending ? <Skeleton height={100} /> : null}
                  {(proxies.data?.items ?? []).map((node) => (
                    <div key={node.node_id} className="stack gap-8">
                      <div className="spread">
                        <span className="mono" style={{ fontSize: 11 }}>{node.node_id} · {node.instance_id}</span>
                        {node.error ? <Stamp state="failed">{node.error}</Stamp> : null}
                      </div>
                      <table className="table tiny-table">
                        <thead>
                          <tr>
                            <th scope="col">计划</th>
                            <th scope="col">时间表</th>
                            <th scope="col">状态</th>
                            <th scope="col">上次运行</th>
                            <th scope="col">产物</th>
                          </tr>
                        </thead>
                        <tbody>
                          {node.items.map((backup) => (
                            <tr key={backup.name}>
                              <td className="mono" style={{ fontSize: 11 }}>{backup.name}</td>
                              <td style={{ fontSize: 11 }}>{backup.schedule}</td>
                              <td><Stamp state={backup.state === 'ready' ? 'ready' : 'failed'}>{backup.state === 'ready' ? '正常' : backup.state}</Stamp></td>
                              <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{backup.last_success ? stamp(backup.last_success) : '—'}</td>
                              <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{backup.artifacts.length} 个</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                      {node.items.length === 0 ? <Empty>没有备份记录</Empty> : null}
                    </div>
                  ))}
                </div>
              ) : null}
            </div>
          </Sheet>

          <Failure error={change.error} />

          <DatabaseCreateDialog
            kind={creating}
            databases={primary?.databases?.map((schema) => schema.name) ?? []}
            busy={change.isPending}
            error={change.error}
            onClose={() => setCreating(null)}
            onCreate={(body) => change.mutate(body)}
          />

          <PasswordDialog
            user={passwordFor}
            busy={change.isPending}
            error={change.error}
            onClose={() => setPasswordFor(null)}
            onSave={(password) => change.mutate({ kind: 'password', user: passwordFor!.name, host: passwordFor!.host, password })}
          />

          <Confirm
            open={Boolean(removing)}
            title={removing?.kind === 'database' ? `删除 ${removing?.name} 库？` : `删除账号 ${removing?.name ?? ''}？`}
            description={
              removing?.kind === 'database'
                ? '删除后库和其中的表都会消失，无法恢复。'
                : '删除后先停用所选代理，已发放的凭据立即失效。'
            }
            confirmLabel={removing?.kind === 'database' ? '删除库' : '删除账号'}
            dangerous
            busy={change.isPending}
            error={change.error}
            onCancel={() => setRemoving(null)}
            onConfirm={() =>
              removing?.kind === 'database'
                ? change.mutate({ kind: 'delete_database', database: removing.name })
                : change.mutate({ kind: 'delete_user', user: removing?.name, host: removing?.host })
            }
          />
        </>
      ) : null}
    </div>
  );
}

function DatabaseCreateDialog({
  kind,
  databases,
  busy,
  error,
  onClose,
  onCreate,
}: {
  kind: 'database' | 'user' | null;
  databases: string[];
  busy: boolean;
  error: unknown;
  onClose: () => void;
  onCreate: (body: Omit<Change, 'expected_revision' | 'proxies'>) => void;
}) {
  const [name, setName] = useState('');
  const [charset, setCharset] = useState<Schema['DatabaseChange']['charset']>('utf8mb4');
  const [host, setHost] = useState('%');
  const [password, setPassword] = useState('');
  const [database, setDatabase] = useState('');
  const [privileges, setPrivileges] = useState('SELECT,INSERT,UPDATE,DELETE');

  return (
    <Dialog
      open={Boolean(kind)}
      title={kind === 'database' ? '新建库' : '新建账号'}
      onClose={onClose}
      footer={
        <>
          <span className="grow" />
          <Button onClick={onClose} disabled={busy}>取消</Button>
          <Button
            variant="solid"
            busy={busy}
            disabled={!name}
            onClick={() =>
              kind === 'database'
                ? onCreate({ kind: 'create_database', database: name, charset })
                : onCreate({
                    kind: 'create_user',
                    user: name,
                    host,
                    password,
                    grants: database ? [{ database, privileges: privileges.split(',').map((item) => item.trim()).filter(Boolean) }] : [],
                  })
            }
          >
            提交变更
          </Button>
        </>
      }
    >
      <div className="form-grid">
        <TextInput
          label={kind === 'database' ? '库名' : '账号'}
          value={name}
          onChange={(event) => setName(event.target.value)}
          className="input--mono"
          autoComplete="off"
        />
        {kind === 'database' ? (
          <Select
            label="字符集"
            value={charset}
            onChange={(event) => setCharset(event.target.value as Schema['DatabaseChange']['charset'])}
            options={[['utf8mb4', 'utf8mb4'], ['utf8mb3', 'utf8mb3'], ['latin1', 'latin1'], ['ascii', 'ascii']]}
          />
        ) : (
          <>
            <TextInput label="来源" value={host} onChange={(event) => setHost(event.target.value)} className="input--mono" />
            <TextInput label="初始密码" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" />
            <Select
              label="授权库"
              value={database}
              onChange={(event) => setDatabase(event.target.value)}
              options={[['', '暂不授权'], ...databases.map((item) => [item, item] as const)]}
            />
            <TextInput label="权限" value={privileges} onChange={(event) => setPrivileges(event.target.value)} className="input--mono" hint="逗号分隔" />
          </>
        )}
      </div>
      <Failure error={error} />
    </Dialog>
  );
}

function PasswordDialog({
  user,
  busy,
  error,
  onClose,
  onSave,
}: {
  user: Schema['DatabaseUser'] | null;
  busy: boolean;
  error: unknown;
  onClose: () => void;
  onSave: (password: string) => void;
}) {
  const [password, setPassword] = useState('');
  return (
    <Dialog
      open={Boolean(user)}
      title={`修改 ${user?.name ?? ''} 的密码`}

      width="narrow"
      onClose={onClose}
      footer={
        <>
          <span className="grow" />
          <Button onClick={onClose} disabled={busy}>取消</Button>
          <Button variant="solid" busy={busy} disabled={password.length < 8} onClick={() => onSave(password)}>
            修改密码
          </Button>
        </>
      }
    >
      <TextInput label="新密码" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" hint="至少 8 位" />
      <Failure error={error} />
    </Dialog>
  );
}

