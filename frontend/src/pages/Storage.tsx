import { useEffect, useState } from 'react';
import { motion } from 'motion/react';
import { CircleCheck, HardDrive, Pencil, Plus, ScanLine } from 'lucide-react';
import { call, enc, getVersioned, newKey } from '../api/http';
import type { StorageCheck, StorageProbe, StorageTarget, StorageTargetInput } from '../api/types';
import { useNodes, useStorageTargets } from '../app/queries';
import { useCommand } from '../app/commands';
import { useSession } from '../app/session';
import { invalidateResource } from '../app/query';
import { ago } from '../lib/format';
import { Badge, Button, Card, Dialog, Empty, ErrorNotice, Field, Input, Notice, PageHead, Segmented, Select, Skeleton, Switch, Textarea, itemVariants, listVariants } from '../ui/kit';

export function Storage() {
  const q = useStorageTargets();
  const [editing, setEditing] = useState<StorageTarget | 'new' | null>(null);
  const items = q.data?.items ?? [];
  return (
    <div className="page">
      <PageHead title="存储" actions={<Button variant="primary" icon={<Plus />} onClick={() => setEditing('new')}>添加存储目标</Button>} />
      {q.isError && <ErrorNotice error={q.error} retry={() => void q.refetch()} />}
      {q.isPending ? <div className="db-grid"><Card pad><Skeleton height={140} /></Card></div> : items.length === 0 ? (
        <Card><Empty icon={<HardDrive />} title="还没有存储目标" action={<Button variant="primary" icon={<Plus />} onClick={() => setEditing('new')}>添加存储目标</Button>} /></Card>
      ) : (
        <motion.div className="db-grid" variants={listVariants} initial="hidden" animate="show">
          {items.map((t) => <TargetCard key={t.id} t={t} onEdit={() => setEditing(t)} />)}
        </motion.div>
      )}
      <TargetDialog open={editing !== null} target={editing === 'new' ? undefined : editing ?? undefined} onClose={() => setEditing(null)} />
    </div>
  );
}

function TargetCard({ t, onEdit }: { t: StorageTarget; onEdit: () => void }) {
  const [probePrefix, setProbePrefix] = useState(t.connection_check?.prefix ?? '');
  const [prefix, setPrefix] = useState(t.check?.prefix ?? 'opsd/');
  const path = `/storage/targets/${enc(t.id)}`;
  const probe = useCommand<void, StorageProbe>({
    label: '探测 S3 连接',
    node: t.node_id,
    success: (r) => r.state === 'ready' ? 'S3 连接正常' : false,
    run: async (_v, key) => {
      const cur = await getVersioned<StorageTarget>(path);
      return (await call<StorageProbe>('POST', `${path}/probe`, { key, ifMatch: cur.etag, body: { prefix: probePrefix } })).data;
    },
    onSuccess: () => invalidateResource('storage'),
  });
  const check = useCommand<void, StorageCheck>({
    label: '检查监控归档',
    node: t.node_id,
    success: (r) => r.state === 'ready' ? '归档检查通过' : false,
    run: async (_v, key) => {
      const cur = await getVersioned<StorageTarget>(path);
      return (await call<StorageCheck>('POST', `${path}/check`, { key, ifMatch: cur.etag, body: { prefix } })).data;
    },
    onSuccess: () => invalidateResource('storage'),
  });
  const c = t.check, p = t.connection_check;
  const ok = c?.state === 'ready';
  return (
    <motion.div variants={itemVariants}>
      <Card className="db-card" style={{ cursor: 'default' }}>
        <div className="row-3">
          <span className="avatar"><HardDrive /></span>
          <div className="col grow" style={{ gap: 0 }}>
            <strong className="ellipsis" style={{ fontWeight: 620 }}>{t.name}</strong>
            <span className="muted mono ellipsis" style={{ fontSize: 12 }}>{t.bucket} @ {t.endpoint}</span>
          </div>
          {t.enabled ? <Badge tone="ok" dot>启用</Badge> : <Badge dot>停用</Badge>}
        </div>
        <dl className="kv" style={{ fontSize: 13 }}>
          <dt>地域</dt><dd>{t.region || '—'}</dd>
          <dt>寻址</dt><dd>{t.access_style === 'path' ? '路径' : '虚拟主机'}{t.dedicated_bucket ? ' · 专用桶' : ''}</dd>
          <dt>凭据</dt><dd>{t.credentials_available ? <Badge tone="ok">已保存</Badge> : <Badge tone="warn">缺失</Badge>}</dd>
          <dt>连接探测</dt><dd>{p ? <span className="row wrap" style={{ gap: 6 }}><Badge tone={p.state === 'ready' ? 'ok' : 'bad'}>{p.state === 'ready' ? '通过' : '失败'}</Badge><span className="muted">{ago(p.checked_at)}</span>{p.error && <span className="field-error">{p.error}</span>}</span> : <span className="dim">未探测</span>}</dd>
          <dt>归档检查</dt><dd>{c ? <span className="row wrap" style={{ gap: 6 }}><Badge tone={ok ? 'ok' : c.error ? 'bad' : 'warn'}>{ok ? '通过' : c.error ? '失败' : '需重新检查'}</Badge><span className="muted">{ago(c.checked_at)}</span>{c.lifecycle_checked && <Badge outline>生命周期</Badge>}{c.prefix_restricted && <Badge outline>前缀受限</Badge>}{c.error && <span className="field-error">{c.error}</span>}</span> : <span className="dim">未检查</span>}</dd>
        </dl>
        <div className="row">
          <Input mono value={probePrefix} onChange={(e) => setProbePrefix(e.target.value)} style={{ flex: 1, minWidth: 0 }} placeholder="列举前缀（可空）" aria-label="列举前缀" />
          <Button size="sm" icon={<ScanLine />} busy={probe.busy} disabled={!t.enabled} onClick={() => void probe.run()}>探测</Button>
          <Button size="sm" variant="ghost" icon={<Pencil />} onClick={onEdit}>编辑</Button>
        </div>
        <div className="row">
          <Input mono value={prefix} onChange={(e) => setPrefix(e.target.value)} style={{ flex: 1, minWidth: 0 }} placeholder="归档前缀" aria-label="归档前缀" />
          <Button size="sm" icon={ok ? <CircleCheck /> : <ScanLine />} busy={check.busy} disabled={!t.enabled || !prefix} onClick={() => void check.run()}>归档检查</Button>
        </div>
      </Card>
    </motion.div>
  );
}

const emptyCred = { access_key: '', secret_key: '' };
function TargetDialog({ open, target, onClose }: { open: boolean; target?: StorageTarget; onClose: () => void }) {
  const session = useSession();
  const nodes = useNodes();
  const [id, setId] = useState('');
  const [form, setForm] = useState<StorageTargetInput>({ endpoint: '', bucket: '', region: '', access_style: 'path', dedicated_bucket: true, node_id: session.local_node_id, name: '', enabled: true });
  const [credMode, setCredMode] = useState<'same' | 'split'>('same');
  const [cred, setCred] = useState({ receive: { ...emptyCred }, store: { ...emptyCred }, compact: { ...emptyCred } });
  const [etag, setEtag] = useState<string | undefined>();
  useEffect(() => {
    if (!open) return;
    setCred({ receive: { ...emptyCred }, store: { ...emptyCred }, compact: { ...emptyCred } });
    if (target) {
      const { endpoint, bucket, region, access_style, dedicated_bucket, node_id, name, enabled, ca_pem } = target;
      setForm({ endpoint, bucket, region, access_style, dedicated_bucket, node_id, name, enabled, ca_pem });
      setId(target.id);
      void getVersioned<StorageTarget>(`/storage/targets/${enc(target.id)}`).then((r) => setEtag(r.etag)).catch(() => setEtag(undefined));
    } else {
      setForm({ endpoint: '', bucket: '', region: '', access_style: 'path', dedicated_bucket: true, node_id: session.local_node_id, name: '', enabled: true });
      setId(`s3-${newKey('x').slice(-8).toLowerCase().replace(/[^a-z0-9]/g, '0')}`);
      setEtag(undefined);
    }
  }, [open, target, session.local_node_id]);
  const filled = (c: { access_key: string; secret_key: string }) => !!c.access_key && !!c.secret_key;
  const credentials = credMode === 'same' ? (filled(cred.receive) ? { receive: cred.receive, store: cred.receive, compact: cred.receive } : undefined) : (filled(cred.receive) && filled(cred.store) && filled(cred.compact) ? cred : undefined);
  const save = useCommand<void, StorageTarget>({
    label: target ? '保存存储目标' : '添加存储目标',
    node: form.node_id,
    run: async (_v, key) => {
      const { ca_pem = '', ...fields } = form;
      const body = { ...fields, ...(ca_pem !== (target?.ca_pem ?? '') ? { ca_pem } : {}), ...(credentials ? { credentials } : {}) };
      return (await call<StorageTarget>('PUT', `/storage/targets/${enc(id)}`, { key, body, ...(target ? { ifMatch: etag } : { ifNoneMatch: '*' }) })).data;
    },
    onSuccess: () => { invalidateResource('storage'); onClose(); },
  });
  const set = <K extends keyof StorageTargetInput>(k: K, v: StorageTargetInput[K]) => setForm((f) => ({ ...f, [k]: v }));
  const valid = form.name && form.endpoint && form.bucket && (target ? true : !!credentials);
  const credFields = (k: 'receive' | 'store' | 'compact', label: string) => (
    <div className="form-grid" key={k}>
      <Field label={`${label} Access Key`}><Input mono value={cred[k].access_key} onChange={(e) => setCred((c) => ({ ...c, [k]: { ...c[k], access_key: e.target.value } }))} autoComplete="off" /></Field>
      <Field label={`${label} Secret Key`}><Input mono type="password" value={cred[k].secret_key} onChange={(e) => setCred((c) => ({ ...c, [k]: { ...c[k], secret_key: e.target.value } }))} autoComplete="new-password" /></Field>
    </div>
  );
  return (
    <Dialog open={open} onClose={onClose} width={640} title={target ? `编辑 ${target.name}` : '添加存储目标'}
      footer={<><Button variant="ghost" onClick={onClose}>取消</Button><Button variant="primary" busy={save.busy} disabled={!valid} onClick={() => void save.run()}>保存</Button></>}>
      <div className="form-grid">
        <Field label="名称"><Input value={form.name} onChange={(e) => set('name', e.target.value)} autoFocus /></Field>
        <Field label="所属节点"><Select value={form.node_id} disabled={!!target} onChange={(e) => set('node_id', e.target.value)}>{(nodes.data?.items ?? []).filter((n) => !n.revoked_at).map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}</Select></Field>
        <Field label="Endpoint" className="span-2"><Input mono value={form.endpoint} onChange={(e) => set('endpoint', e.target.value)} placeholder="https://s3.example.com" /></Field>
        <Field label="S3 CA（PEM）" className="span-2"><Textarea mono rows={3} maxLength={65536} value={form.ca_pem ?? ''} placeholder="留空使用系统信任" onChange={(e) => set('ca_pem', e.target.value)} /></Field>
        <Field label="Bucket"><Input mono value={form.bucket} onChange={(e) => set('bucket', e.target.value)} /></Field>
        <Field label="地域"><Input mono value={form.region} onChange={(e) => set('region', e.target.value)} placeholder="us-east-1" /></Field>
        <Field label="寻址方式"><Segmented value={form.access_style} onChange={(v) => set('access_style', v)} options={[{ value: 'path', label: '路径' }, { value: 'virtual_host', label: '虚拟主机' }]} /></Field>
        <Field label="选项"><div className="row" style={{ height: 36, gap: 16 }}><span className="row"><Switch checked={form.dedicated_bucket} onChange={(v) => set('dedicated_bucket', v)} label="专用桶" />专用桶</span><span className="row"><Switch checked={form.enabled} onChange={(v) => set('enabled', v)} label="启用" />启用</span></div></Field>
      </div>
      <div className="stack" style={{ gap: 12 }}>
        <div className="row"><strong style={{ fontWeight: 600 }} className="grow">凭据</strong><Segmented size="sm" value={credMode} onChange={setCredMode} options={[{ value: 'same', label: '统一凭据' }, { value: 'split', label: '按组件' }]} /></div>
        {target && <Notice tone="info">留空表示保留已保存的凭据。</Notice>}
        {credMode === 'same' ? credFields('receive', '') : <>{credFields('receive', 'Receive')}{credFields('store', 'Store')}{credFields('compact', 'Compactor')}</>}
      </div>
    </Dialog>
  );
}
