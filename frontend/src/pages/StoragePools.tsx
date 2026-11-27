import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus, Server, Trash2 } from 'lucide-react';
import { call, enc, get, isUnknown } from '../api/http';
import type { Operation, Schemas } from '../api/types';
import { useCommand } from '../app/commands';
import { invalidateResource } from '../app/query';
import { useNodes } from '../app/queries';
import { useSession } from '../app/session';
import { bytes, copyText } from '../lib/format';
import { Badge, Button, Card, Dialog, Empty, ErrorNotice, Field, Input, Notice, Select, Skeleton } from '../ui/kit';

type Pool = Schemas['StoragePool'];
type Member = Schemas['StoragePoolMemberInput'];
type Plan = Schemas['StoragePoolPlan'];
type Preview = Schemas['StoragePoolPreviewInput'];
type Create = Schemas['StoragePoolCreateInput'];

const statusText: Record<Pool['state'] | Schemas['StoragePoolMember']['state'], string> = {
  creating: '预分配中', deploying: '部署中', ready: '就绪', degraded: '降级',
  read_only: '只读', unavailable: '不可用', attention_required: '需处理', pending: '待部署',
};

function PoolBadge({ state }: { state: keyof typeof statusText }) {
  return <Badge dot tone={state === 'ready' ? 'ok' : state === 'unavailable' ? 'bad' : state === 'creating' || state === 'deploying' ? 'info' : 'warn'}>{statusText[state]}</Badge>;
}

export function StoragePools() {
  const [cursor, setCursor] = useState<string>();
  const [creating, setCreating] = useState(false);
  const [viewing, setViewing] = useState<string>();
  const list = useQuery({ queryKey: ['storage', 'pools', cursor], queryFn: ({ signal }) => get<Schemas['StoragePoolList']>('/storage/pools', { signal, query: { cursor } }), retry: 0 });
  return (
    <section className="stack" aria-labelledby="storage-pools">
      <div className="row wrap">
        <h2 id="storage-pools" className="grow">集群 S3 池</h2>
        <Button icon={<Plus />} onClick={() => setCreating(true)}>创建存储池</Button>
      </div>
      {list.isError && <ErrorNotice error={list.error} retry={() => void list.refetch()} />}
      {!!list.data?.unavailable.length && <Notice tone="warn">{list.data.unavailable.length} 个节点的存储池列表不可用</Notice>}
      {list.isPending ? <Card pad><Skeleton height={84} /></Card> : list.data?.items.length ? (
        <div className="db-grid">
          {list.data.items.map((p) => <Card className="db-card" key={p.id}>
            <div className="row-3">
              <span className="avatar"><Server /></span>
              <strong className="ellipsis">{p.name}</strong>
              <PoolBadge state={p.state} />
            </div>
            <div className="row"><Button size="sm" onClick={() => setViewing(p.id)}>查看详情</Button></div>
          </Card>)}
        </div>
      ) : list.data && <Card><Empty icon={<Server />} title="还没有集群 S3 池" /></Card>}
      {(cursor || list.data?.next_cursor) && <div className="row wrap">
        {cursor && <Button size="sm" onClick={() => setCursor(undefined)}>首页</Button>}
        {list.data?.next_cursor && <Button size="sm" onClick={() => setCursor(list.data?.next_cursor)}>下一页</Button>}
      </div>}
      {creating && <CreatePool onClose={() => setCreating(false)} />}
      {viewing && <PoolDetails id={viewing} onClose={() => setViewing(undefined)} />}
    </section>
  );
}

function PoolDetails({ id, onClose }: { id: string; onClose: () => void }) {
  const query = useQuery({ queryKey: ['storage', 'pools', 'detail', id], queryFn: ({ signal }) => get<Pool>(`/storage/pools/${enc(id)}`, { signal }), refetchInterval: 5_000, retry: 0 });
  const names = useNodes().data?.items ?? [];
  return <Dialog open onClose={onClose} title={query.data?.name ?? '存储池详情'} width={640}
    footer={<><Button variant="ghost" onClick={onClose}>关闭</Button>{query.data?.ca_pem && <Button onClick={() => void copyText(query.data!.ca_pem)}>复制池 CA</Button>}</>}>
    {query.isPending && <Skeleton height={88} />}
    {query.isError && <ErrorNotice error={query.error} retry={() => void query.refetch()} />}
    {query.data && <div className="stack">
      <div className="row wrap"><PoolBadge state={query.data.state} /><span className="muted">跨故障域双副本</span></div>
      {query.data.nodes.map((m) => <Card className="db-card" key={m.node_id}>
        <div className="row wrap"><strong className="grow">{names.find((n) => n.id === m.node_id)?.name ?? m.private_ip}</strong><PoolBadge state={m.state} /></div>
        <span className="mono" style={{ overflowWrap: 'anywhere' }}>{m.endpoint}</span>
        <dl className="kv"><dt>故障域</dt><dd>{m.failure_domain}</dd><dt>角色</dt><dd>{m.roles.join(' / ')}</dd>
          <dt>数据卷</dt><dd>{bytes(m.data_used_bytes)} / {bytes(m.data_bytes)}</dd>
          <dt>控制卷</dt><dd>{bytes(m.control_used_bytes)} / {bytes(m.control_bytes)}</dd></dl>
        {m.error && <p className="field-error">{m.error}</p>}
      </Card>)}
    </div>}
  </Dialog>;
}

const minimumPoolCapacity = 7;
const emptyMember = (): Member => ({ node_id: '', private_ip: '', capacity_gib: 12, storage_root_id: '', failure_domain: '' });

function CreatePool({ onClose }: { onClose: () => void }) {
  const names = (useNodes().data?.items ?? []).filter((n) => !n.revoked_at);
  const session = useSession();
  const [form, setForm] = useState<Preview>({ name: '', nodes: [emptyMember(), emptyMember(), emptyMember()], port_offset: 0, ec_enabled: false });
  const [credentials, setCredentials] = useState<Create['credentials']>({ access_key: '', secret_key: '' });
  const [plan, setPlan] = useState<Plan>();
  const preview = useCommand<Preview, Plan>({
    label: '预览存储池', node: session.local_node_id, success: false,
    run: async (input) => (await call<Plan>('POST', '/storage/pools/preview', { body: input })).data,
    onSuccess: setPlan,
  });
  const create = useCommand<Create, Operation>({
    label: '创建存储池', node: plan?.owner_node_id ?? session.local_node_id,
    run: async (body, key) => (await call<Operation>('POST', '/storage/pools', { body, key })).data,
    onSuccess: () => { invalidateResource('storage'); onClose(); },
  });
  const valid = form.name.trim() && Number.isInteger(form.port_offset) && form.port_offset >= 0 && form.port_offset <= 16000
    && new Set(form.nodes.map((m) => m.node_id)).size === form.nodes.length
    && form.nodes.every((m) => m.node_id && m.private_ip.trim() && Number.isInteger(m.capacity_gib) && m.capacity_gib >= minimumPoolCapacity);
  const canCreate = credentials.access_key.length >= 3 && credentials.secret_key.length >= 8 && !(create.error && isUnknown(create.error));
  return <Dialog open onClose={onClose} title="创建集群 S3 池" width={700} footer={<>
    <Button variant="ghost" onClick={plan ? () => { setPlan(undefined); create.reset(); } : onClose}>{plan ? '返回配置' : '取消'}</Button>
    {plan
      ? <Button variant="primary" busy={create.busy} disabled={!canCreate} onClick={() => void create.run({ plan_id: plan.plan_id, credentials })}>确认创建</Button>
      : <Button variant="primary" busy={preview.busy} disabled={!valid} onClick={() => void preview.run(form)}>预览配额</Button>}
  </>}>
    {plan ? <div className="stack">
      {plan.nodes.map((m) => <Card className="db-card" key={m.node_id}>
        <strong>{names.find((n) => n.id === m.node_id)?.name ?? m.private_ip}</strong>
        <dl className="kv">
          <dt>角色</dt><dd>{m.roles.join(' / ')}</dd>
          <dt>控制卷 A</dt><dd>{bytes(m.control_bytes)}</dd><dt>数据卷 B</dt><dd>{bytes(m.data_bytes)}</dd>
        </dl>
      </Card>)}
      <div className="form-grid">
        <Field label="Access Key"><Input mono value={credentials.access_key} maxLength={128} onChange={(e) => setCredentials((c) => ({ ...c, access_key: e.target.value }))} autoComplete="off" /></Field>
        <Field label="Secret Key"><Input mono type="password" maxLength={128} value={credentials.secret_key} onChange={(e) => setCredentials((c) => ({ ...c, secret_key: e.target.value }))} autoComplete="new-password" /></Field>
      </div>
      {!!create.error && <ErrorNotice title="创建请求" error={create.error} />}
    </div> : <div className="stack">
      <div className="form-grid">
        <Field label="池名称"><Input autoFocus maxLength={64} value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} /></Field>
        <Field label="端口偏移"><Input type="number" min={0} max={16000} value={form.port_offset} onChange={(e) => setForm((f) => ({ ...f, port_offset: Number(e.target.value) }))} /></Field>
      </div>
      {form.nodes.map((m, i) => <MemberFields key={i} value={m}
        nodes={names.filter((n) => n.id === m.node_id || !form.nodes.some((x) => x.node_id === n.id))}
        onChange={(next) => setForm((f) => ({ ...f, nodes: f.nodes.map((x, at) => at === i ? next : x) }))}
        onRemove={form.nodes.length > 3 ? () => setForm((f) => ({ ...f, nodes: f.nodes.filter((_, at) => at !== i) })) : undefined} />)}
      <div className="row"><Button size="sm" icon={<Plus />} disabled={form.nodes.length >= names.length} onClick={() => setForm((f) => ({ ...f, nodes: [...f.nodes, emptyMember()] }))}>添加成员</Button></div>
      {!!preview.error && <ErrorNotice title="预览失败" error={preview.error} />}
    </div>}
  </Dialog>;
}

function MemberFields({ value, nodes, onChange, onRemove }: { value: Member; nodes: Schemas['NodeList']['items']; onChange: (value: Member) => void; onRemove?: () => void }) {
  const roots = useQuery({
    queryKey: ['storage', 'root', value.node_id],
    queryFn: ({ signal }) => get<Schemas['StorageRootView']>(`/nodes/${enc(value.node_id)}/storage-root`, { signal }),
    enabled: !!value.node_id, retry: 0,
  });
  return <Card className="db-card"><div className="form-grid">
    <Field label="成员节点"><Select value={value.node_id} onChange={(e) => onChange({ ...value, node_id: e.target.value, storage_root_id: '' })}>
      <option value="">选择节点</option>{nodes.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}</Select></Field>
    <Field label="私网 IP"><Input mono value={value.private_ip} onChange={(e) => onChange({ ...value, private_ip: e.target.value.trim() })} /></Field>
    <Field label="总容量 (GiB)"><Input type="number" min={minimumPoolCapacity} value={value.capacity_gib || ''} onChange={(e) => onChange({ ...value, capacity_gib: Number(e.target.value) })} /></Field>
    <Field label="数据根"><Select value={value.storage_root_id} onChange={(e) => onChange({ ...value, storage_root_id: e.target.value })}>
      <option value="">默认</option>{roots.data?.roots.filter((r) => r.root_id !== 'default').map((r) => <option key={r.root_id} value={r.root_id} disabled={r.state !== 'ready'}>{r.base_path}</option>)}</Select></Field>
    <Field label="故障域（可空）"><Input value={value.failure_domain} onChange={(e) => onChange({ ...value, failure_domain: e.target.value })} /></Field>
    {onRemove && <div className="row"><Button size="sm" variant="ghost" icon={<Trash2 />} onClick={onRemove}>移除成员</Button></div>}
  </div></Card>;
}
