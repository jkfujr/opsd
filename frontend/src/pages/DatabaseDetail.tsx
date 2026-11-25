import { useEffect, useState } from 'react';
import { useParams, useSearchParams } from 'react-router';
import { AnimatePresence, motion } from 'motion/react';
import { useQuery } from '@tanstack/react-query';
import { Database, KeyRound, Plus, Trash2, UserPlus, ExternalLink, Shield, Ellipsis, Lock } from 'lucide-react';
import { call, enc, get } from '../api/http';
import type { DatabaseBackupList, DatabaseChange, DatabaseProxySelection, ManagedDatabaseMember, ManagedDatabaseStatus, NodeStatus, Operation } from '../api/types';
import { useDatabaseStatus, useNodes } from '../app/queries';
import { useCommand } from '../app/commands';
import { confirm } from '../app/confirm';
import { ago, bytes, fullTime, n0 } from '../lib/format';
import { Badge, Button, Card, CardHead, Dialog, Dot, Empty, ErrorNotice, Field, IconButton, Input, Menu, Notice, Select, Skeleton, Tabs, Tag } from '../ui/kit';
import { clusterHealth, memberLabel, memberTone } from './Databases';

type Tab = 'health' | 'schemas' | 'users' | 'proxies' | 'backups';

export function DatabaseDetail() {
  const { id = '' } = useParams();
  const [params, setParams] = useSearchParams();
  const tab = (params.get('tab') as Tab) || 'health';
  const health = useDatabaseStatus(id, 'health');
  const d = health.data;
  return (
    <div className="page">
      {health.isError && !d && <ErrorNotice error={health.error} retry={() => void health.refetch()} />}
      <motion.div className="node-hero" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }}>
        <div className="nh-title">
          <span className="avatar"><Database /></span>
          <div className="col" style={{ gap: 4, minWidth: 0 }}>
            <div className="row"><h1 className="ellipsis">{d?.cluster.name ?? '数据库集群'}</h1>{d && (() => { const h = clusterHealth(d); return <Badge tone={h.tone} dot>{h.known ? `${h.ok}/${h.total} 正常` : '读取中'}</Badge>; })()}</div>
            <div className="row muted wrap" style={{ fontSize: 13, gap: 12 }}>
              {d && <span className="mono">Galera {d.cluster.cluster_id.slice(0, 18)}</span>}
              {d && <span>版本 {d.cluster.revision}</span>}
              {d && <span>更新于 {ago(d.checked_at)}</span>}
            </div>
          </div>
        </div>
        {d?.cluster.workbench_url && <Button icon={<ExternalLink />} onClick={() => window.open(d.cluster.workbench_url, '_blank', 'noopener')}>SQL 工作台</Button>}
      </motion.div>
      <Tabs value={tab} onChange={(t) => setParams({ tab: t }, { replace: true })} options={[
        { value: 'health', label: '成员' }, { value: 'schemas', label: '库' }, { value: 'users', label: '账号' }, { value: 'proxies', label: '代理' }, { value: 'backups', label: '备份' },
      ]} />
      <AnimatePresence mode="wait">
        <motion.div key={tab} initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }} transition={{ duration: 0.2 }}>
          {tab === 'health' && <Members d={d} />}
          {tab === 'schemas' && <Schemas id={id} base={d} />}
          {tab === 'users' && <Users id={id} base={d} />}
          {tab === 'proxies' && <Proxies id={id} />}
          {tab === 'backups' && <Backups id={id} />}
        </motion.div>
      </AnimatePresence>
    </div>
  );
}

function Members({ d }: { d?: ManagedDatabaseStatus }) {
  const nodes = useNodes();
  if (!d) return <Card pad><Skeleton height={160} /></Card>;
  const directory = new Map(nodes.data?.items.map((node) => [node.id, node]));
  return (
    <div className="stack">
      <Card>
        <CardHead title="Galera 成员" sub={`${d.members.length} 个`} />
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>成员</th><th>节点</th><th>状态</th><th className="num-cell">集群</th><th className="num-cell">连接</th><th className="num-cell hide-sm">收 / 发队列</th><th className="num-cell hide-sm">流控</th><th className="hide-sm">采集</th></tr></thead>
            <tbody>{d.members.map((m) => <MemberRow key={`${m.node_id}/${m.instance_id}`} m={m} node={directory.get(m.node_id)} />)}</tbody>
          </table>
        </div>
      </Card>
      {d.proxies.length > 0 && (
        <Card>
          <CardHead title="ProxySQL" sub={`${d.proxies.length} 个`} />
          <div className="table-wrap">
            <table className="table">
              <thead><tr><th>代理</th><th>状态</th><th className="num-cell">连接</th><th className="hide-sm">地址</th><th className="hide-sm">采集</th></tr></thead>
              <tbody>{d.proxies.map((m) => (
                <tr key={`${m.node_id}/${m.instance_id}`}>
                  <td><strong style={{ fontWeight: 560 }}>{m.name}</strong></td>
                  <td><Badge tone={memberTone(m)} dot>{memberLabel(m)}</Badge></td>
                  <td className="num-cell">{m.status.connections !== undefined ? n0(m.status.connections) : '—'}</td>
                  <td className="hide-sm mono muted" style={{ fontSize: 12.5 }}>{m.address}</td>
                  <td className="hide-sm muted" style={{ fontSize: 12.5 }}>{m.status.read.data_at ? ago(m.status.read.data_at) : '—'}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        </Card>
      )}
    </div>
  );
}

function MemberRow({ m, node }: { m: ManagedDatabaseMember; node?: NodeStatus }) {
  const s = m.status;
  return (
    <tr>
      <td><div className="col" style={{ gap: 0 }}><strong style={{ fontWeight: 560 }}>{m.name}</strong><span className="mono muted" style={{ fontSize: 12 }}>{m.address}</span></div></td>
      <td><div className="col" style={{ gap: 0 }}><span title={m.node_id}>{node?.name || m.node_id.slice(0, 12)}</span>{node?.address && <span className="mono muted" style={{ fontSize: 12 }}>{node.address}</span>}</div></td>
      <td><div className="row wrap" style={{ gap: 4 }}><Badge tone={memberTone(m)} dot>{memberLabel(m)}</Badge>{s.read_only && <Tag>read_only</Tag>}</div></td>
      <td className="num-cell">{s.cluster_size ?? '—'}</td>
      <td className="num-cell">{s.connections !== undefined ? `${n0(s.connections)}${s.max_connections ? ` / ${n0(s.max_connections)}` : ''}` : '—'}</td>
      <td className="num-cell hide-sm">{s.receive_queue ?? '—'} / {s.send_queue ?? '—'}</td>
      <td className="num-cell hide-sm">{s.flow_control !== undefined ? `${(s.flow_control * 100).toFixed(1)}%` : '—'}</td>
      <td className="hide-sm muted" style={{ fontSize: 12.5 }}>{s.read.data_at ? ago(s.read.data_at) : '—'}</td>
    </tr>
  );
}

function useChange(id: string, cluster?: ManagedDatabaseStatus) {
  return useCommand<{ label: string; change: Omit<DatabaseChange, 'expected_revision'> }, Operation>({
    label: (v) => v.label,
    node: cluster?.cluster.coordinator ?? '',
    run: async (v, key) => (await call<Operation>('POST', `/database-clusters/${enc(id)}/changes`, { key, body: { ...v.change, expected_revision: cluster!.cluster.revision } })).data,
  });
}

function ProxyPicker({ cluster, value, onChange }: { cluster?: ManagedDatabaseStatus; value: DatabaseProxySelection[]; onChange: (v: DatabaseProxySelection[]) => void }) {
  if (!cluster?.proxies.length) return null;
  const hg = cluster.cluster.proxy_routing?.hostgroups.writer ?? 10;
  return (
    <Field label="同步到代理" hint="账号会同时写入所选 ProxySQL">
      <div className="grant-grid">
        {cluster.proxies.map((p) => {
          const on = value.some((v) => v.node_id === p.node_id && v.instance_id === p.instance_id);
          return (
            <label key={`${p.node_id}/${p.instance_id}`} className="grant" style={{ cursor: 'pointer', boxShadow: on ? 'inset 0 0 0 1.5px var(--accent)' : undefined }}>
              <span className="ellipsis">{p.name}</span>
              <input type="checkbox" checked={on} style={{ accentColor: 'var(--accent)' }} onChange={() => onChange(on ? value.filter((v) => !(v.node_id === p.node_id && v.instance_id === p.instance_id)) : [...value, { node_id: p.node_id, instance_id: p.instance_id, hostgroup: hg }])} />
            </label>
          );
        })}
      </div>
    </Field>
  );
}

function Schemas({ id, base }: { id: string; base?: ManagedDatabaseStatus }) {
  const q = useDatabaseStatus(id, 'schemas');
  const change = useChange(id, base);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [charset, setCharset] = useState<'utf8mb4' | 'utf8mb3' | 'latin1' | 'ascii'>('utf8mb4');
  const list = q.data?.members.find((m) => m.status.databases)?.status.databases ?? [];
  return (
    <Card>
      <CardHead title="数据库" sub={q.data ? `${list.length} 个` : undefined} actions={<Button size="sm" variant="primary" icon={<Plus />} disabled={!base} onClick={() => { setName(''); setCreating(true); }}>新建库</Button>} />
      {q.isError ? <div className="card-body"><ErrorNotice error={q.error} retry={() => void q.refetch()} /></div> : !q.data ? <div className="card-body"><Skeleton height={120} /></div> : list.length === 0 ? <Empty icon={<Database />} title="没有业务库" /> : (
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>名称</th><th className="hide-sm">字符集</th><th className="num-cell">大小</th><th className="actions-cell" /></tr></thead>
            <tbody>{list.map((s) => (
              <tr key={s.name}>
                <td><span className="row"><span className="mono">{s.name}</span>{s.protected && <Badge><Lock width={11} height={11} />受保护</Badge>}</span></td>
                <td className="hide-sm muted mono" style={{ fontSize: 12.5 }}>{s.charset} · {s.collation}</td>
                <td className="num-cell">{bytes(s.bytes)}</td>
                <td className="actions-cell">{!s.protected && <IconButton size="sm" danger label="删除" disabled={change.busy} onClick={async () => { if (await confirm({ title: `删除数据库 ${s.name}？`, body: '库中的全部数据将被永久删除。', danger: true, confirmLabel: '删除', typeToConfirm: s.name })) void change.run({ label: '删除数据库', change: { kind: 'delete_database', database: s.name, proxies: [] } }); }}><Trash2 /></IconButton>}</td>
              </tr>
            ))}</tbody>
          </table>
        </div>
      )}
      <Dialog open={creating} onClose={() => setCreating(false)} width={460} title="新建数据库" footer={<><Button variant="ghost" onClick={() => setCreating(false)}>取消</Button><Button variant="primary" busy={change.busy} disabled={!/^[A-Za-z0-9_$]{1,64}$/.test(name)} onClick={() => void change.run({ label: '创建数据库', change: { kind: 'create_database', database: name, charset, proxies: [] } }).then((r) => r && setCreating(false))}>创建</Button></>}>
        <Field label="名称" hint="字母、数字、下划线，最多 64 个字符"><Input mono value={name} onChange={(e) => setName(e.target.value)} autoFocus /></Field>
        <Field label="字符集"><Select value={charset} onChange={(e) => setCharset(e.target.value as typeof charset)}><option value="utf8mb4">utf8mb4</option><option value="utf8mb3">utf8mb3</option><option value="latin1">latin1</option><option value="ascii">ascii</option></Select></Field>
      </Dialog>
    </Card>
  );
}

const privilegeSets: Record<string, string[]> = {
  '只读': ['SELECT'],
  '读写': ['SELECT', 'INSERT', 'UPDATE', 'DELETE'],
  '完整': ['ALL PRIVILEGES'],
};

function Users({ id, base }: { id: string; base?: ManagedDatabaseStatus }) {
  const q = useDatabaseStatus(id, 'users');
  const schemas = useDatabaseStatus(id, 'schemas');
  const change = useChange(id, base);
  const [mode, setMode] = useState<null | { kind: 'create' } | { kind: 'password' | 'grant'; user: string; host: string }>(null);
  const [form, setForm] = useState({ user: '', host: '%', password: '', database: '', level: '读写' });
  const [proxies, setProxies] = useState<DatabaseProxySelection[]>([]);
  useEffect(() => { if (mode?.kind === 'create') { setForm({ user: '', host: '%', password: '', database: '', level: '读写' }); setProxies([]); } }, [mode]);
  const users = q.data?.members.find((m) => m.status.users)?.status.users ?? [];
  const dbs = schemas.data?.members.find((m) => m.status.databases)?.status.databases?.filter((s) => !s.protected) ?? [];
  const submit = async () => {
    if (!mode) return;
    let r: Operation | undefined;
    if (mode.kind === 'create') {
      r = await change.run({ label: '创建数据库账号', change: { kind: 'create_user', user: form.user, host: form.host, password: form.password, grants: form.database ? [{ database: form.database, privileges: privilegeSets[form.level]! }] : [], proxies } });
    } else if (mode.kind === 'password') {
      r = await change.run({ label: '修改账号密码', change: { kind: 'password', user: mode.user, host: mode.host, password: form.password, proxies } });
    } else {
      r = await change.run({ label: '授予权限', change: { kind: 'grant', user: mode.user, host: mode.host, database: form.database, privileges: privilegeSets[form.level]!, proxies: [] } });
    }
    if (r) setMode(null);
  };
  return (
    <Card>
      <CardHead title="账号" sub={q.data ? `${users.length} 个` : undefined} actions={<Button size="sm" variant="primary" icon={<UserPlus />} disabled={!base} onClick={() => setMode({ kind: 'create' })}>新建账号</Button>} />
      {q.isError ? <div className="card-body"><ErrorNotice error={q.error} retry={() => void q.refetch()} /></div> : !q.data ? <div className="card-body"><Skeleton height={120} /></div> : users.length === 0 ? <Empty title="没有账号" /> : (
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>账号</th><th>权限</th><th className="actions-cell" /></tr></thead>
            <tbody>{users.map((u) => (
              <tr key={`${u.name}@${u.host}`}>
                <td><span className="row"><span className="mono">{u.name}</span><span className="dim mono">@{u.host}</span>{u.protected && <Badge><Shield width={11} height={11} />系统</Badge>}</span></td>
                <td><div className="row wrap" style={{ gap: 4 }}>{u.grants.length ? u.grants.map((g) => <Tag key={g.database} title={g.privileges.join(', ')}>{g.database}: {g.privileges.length > 2 ? `${g.privileges.length} 项` : g.privileges.join(',')}</Tag>) : <span className="dim">—</span>}</div></td>
                <td className="actions-cell">{!u.protected && (
                  <Menu trigger={(p) => <IconButton size="sm" label="更多" {...p}><Ellipsis /></IconButton>} items={[
                    { label: '修改密码', icon: <KeyRound />, onSelect: () => { setForm((f) => ({ ...f, password: '' })); setProxies([]); setMode({ kind: 'password', user: u.name, host: u.host }); } },
                    { label: '授予库权限', icon: <Plus />, onSelect: () => { setForm((f) => ({ ...f, database: dbs[0]?.name ?? '', level: '读写' })); setMode({ kind: 'grant', user: u.name, host: u.host }); } },
                    ...u.grants.map((g) => ({ label: `撤销 ${g.database} 权限`, icon: <Trash2 />, onSelect: () => void change.run({ label: '撤销权限', change: { kind: 'revoke', user: u.name, host: u.host, database: g.database, privileges: g.privileges, proxies: [] } }) })),
                    { separator: true, label: '' },
                    { label: '删除账号', icon: <Trash2 />, danger: true, onSelect: async () => { if (await confirm({ title: `删除账号 ${u.name}@${u.host}？`, body: '已建立的连接会被断开，代理中的对应账号也会删除。', danger: true, confirmLabel: '删除', typeToConfirm: u.name })) void change.run({ label: '删除数据库账号', change: { kind: 'delete_user', user: u.name, host: u.host, proxies: (base?.proxies ?? []).map((p) => ({ node_id: p.node_id, instance_id: p.instance_id, hostgroup: base?.cluster.proxy_routing?.hostgroups.writer ?? 10 })) } }); } },
                  ]} />
                )}</td>
              </tr>
            ))}</tbody>
          </table>
        </div>
      )}
      <Dialog open={!!mode} onClose={() => setMode(null)} width={540} title={mode?.kind === 'create' ? '新建账号' : mode?.kind === 'password' ? `修改 ${mode.user} 的密码` : mode ? `为 ${mode.user} 授予权限` : ''}
        footer={<><Button variant="ghost" onClick={() => setMode(null)}>取消</Button><Button variant="primary" busy={change.busy} onClick={() => void submit()} disabled={mode?.kind === 'create' ? !form.user || form.password.length < 8 : mode?.kind === 'password' ? form.password.length < 8 : !form.database}>保存</Button></>}>
        {mode?.kind === 'create' && (
          <div className="form-grid">
            <Field label="账号"><Input mono value={form.user} onChange={(e) => setForm((f) => ({ ...f, user: e.target.value }))} autoFocus /></Field>
            <Field label="来源主机" hint="% 表示任意主机"><Input mono value={form.host} onChange={(e) => setForm((f) => ({ ...f, host: e.target.value }))} /></Field>
          </div>
        )}
        {(mode?.kind === 'create' || mode?.kind === 'password') && (
          <Field label="密码" hint="至少 8 位">
            <div className="row"><Input mono type="text" value={form.password} onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))} autoComplete="new-password" /><Button onClick={() => setForm((f) => ({ ...f, password: randomPassword() }))}>生成</Button></div>
          </Field>
        )}
        {(mode?.kind === 'create' || mode?.kind === 'grant') && (
          <div className="form-grid">
            <Field label="数据库" hint={mode?.kind === 'create' ? '可选，创建后立即授权' : undefined}>
              <Select value={form.database} onChange={(e) => setForm((f) => ({ ...f, database: e.target.value }))}>
                {mode?.kind === 'create' && <option value="">暂不授权</option>}
                {dbs.map((s) => <option key={s.name} value={s.name}>{s.name}</option>)}
              </Select>
            </Field>
            <Field label="权限"><Select value={form.level} onChange={(e) => setForm((f) => ({ ...f, level: e.target.value }))}>{Object.keys(privilegeSets).map((k) => <option key={k} value={k}>{k}（{privilegeSets[k]!.join(', ')}）</option>)}</Select></Field>
          </div>
        )}
        {(mode?.kind === 'create' || mode?.kind === 'password') && <ProxyPicker cluster={base} value={proxies} onChange={setProxies} />}
      </Dialog>
    </Card>
  );
}

function randomPassword() {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_';
  const bytes_ = crypto.getRandomValues(new Uint8Array(24));
  return Array.from(bytes_, (b) => chars[b % chars.length]).join('');
}

function Proxies({ id }: { id: string }) {
  const q = useDatabaseStatus(id, 'proxies');
  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  if (!q.data) return <Card pad><Skeleton height={160} /></Card>;
  if (!q.data.proxies.length) return <Card><Empty title="没有登记代理">集群还没有登记 ProxySQL。</Empty></Card>;
  return (
    <div className="stack">
      {q.data.proxies.map((p) => (
        <Card key={`${p.node_id}/${p.instance_id}`}>
          <CardHead title={p.name} sub={p.address} actions={<Badge tone={memberTone(p)} dot>{memberLabel(p)}</Badge>} />
          {p.status.pending_changes && <div className="card-body" style={{ paddingBottom: 0 }}><Notice tone="warn">代理存在未应用到运行层的差异。</Notice></div>}
          <div className="table-wrap">
            <table className="table">
              <thead><tr><th>主机组</th><th>后端</th><th>状态</th><th className="num-cell">使用 / 空闲</th><th className="num-cell hide-sm">错误</th><th className="num-cell hide-sm">延迟</th></tr></thead>
              <tbody>{(p.status.pools ?? []).map((pool, i) => (
                <tr key={i}>
                  <td className="num">{pool.hostgroup}</td>
                  <td className="mono" style={{ fontSize: 12.5 }}>{pool.host}:{pool.port}</td>
                  <td><Badge tone={pool.state === 'ONLINE' ? 'ok' : pool.state ? 'warn' : 'idle'}>{pool.state ?? '—'}</Badge></td>
                  <td className="num-cell">{n0(pool.used)} / {n0(pool.free)}</td>
                  <td className="num-cell hide-sm">{n0(pool.errors)}</td>
                  <td className="num-cell hide-sm">{pool.latency_us !== undefined ? `${(pool.latency_us / 1000).toFixed(1)} ms` : '—'}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
          {!!p.status.proxy_users?.length && <div className="card-body row wrap" style={{ gap: 6 }}>{p.status.proxy_users.map((u) => <Tag key={u.name}>{u.name} → {u.hostgroup}{u.active ? '' : '（停用）'}</Tag>)}</div>}
        </Card>
      ))}
    </div>
  );
}

function Backups({ id }: { id: string }) {
  const q = useQuery({ queryKey: ['databases', id, 'backups'], queryFn: ({ signal }) => get<DatabaseBackupList>(`/database-clusters/${enc(id)}/backups`, { signal }) });
  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  if (!q.data) return <Card pad><Skeleton height={120} /></Card>;
  const all = q.data.items.flatMap((n) => n.items.map((b) => ({ ...b, node: n.node_id, nodeError: n.error })));
  if (!all.length) return <Card><Empty title="没有备份计划">备份计划与产物由节点上的既有工具维护，OPSD 只读展示。</Empty></Card>;
  return (
    <Card>
      <div className="table-wrap">
        <table className="table">
          <thead><tr><th>计划</th><th>调度</th><th>状态</th><th>最近成功</th><th className="num-cell">产物</th></tr></thead>
          <tbody>{all.map((b, i) => (
            <tr key={i}>
              <td><strong style={{ fontWeight: 560 }}>{b.name}</strong>{b.error && <div className="field-error">{b.error}</div>}</td>
              <td className="mono" style={{ fontSize: 12.5 }}>{b.schedule}</td>
              <td><Badge tone={b.state === 'ok' || b.state === 'success' ? 'ok' : b.error ? 'bad' : 'idle'}>{b.state}</Badge></td>
              <td>{b.last_success ? <span title={fullTime(b.last_success)}>{ago(b.last_success)}</span> : '—'}</td>
              <td className="num-cell">{b.artifacts.length}</td>
            </tr>
          ))}</tbody>
        </table>
      </div>
    </Card>
  );
}

