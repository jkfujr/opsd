import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router';
import { motion } from 'motion/react';
import { Database, ExternalLink, Plus, Server, ArrowRight, ArrowLeft, Check } from 'lucide-react';
import { call } from '../api/http';
import type { ManagedDatabaseMember, ManagedDatabaseStatus, Operation, Schemas } from '../api/types';
import { useDatabases, useNodes, useSharedStorage } from '../app/queries';
import { useCommand } from '../app/commands';
import { useSession } from '../app/session';
import { ago, bytes, n0 } from '../lib/format';
import { Badge, Button, Card, Dialog, Dot, Empty, ErrorNotice, Field, Input, Notice, PageHead, Skeleton, Tag, itemVariants, listVariants } from '../ui/kit';
import type { Tone } from '../lib/status';

export function memberTone(m: ManagedDatabaseMember): Tone {
  const s = m.status;
  if (s.error || s.state === 'error' || s.state === 'timeout' || s.state === 'mismatch') return 'bad';
  if (s.state === 'offline' || s.state === 'unavailable') return 'idle';
  if (s.state === 'ready' || s.read.data_at) {
    if (s.primary === false || s.ready === false || s.synced === false) return 'warn';
    return 'ok';
  }
  return 'info';
}
export function memberLabel(m: ManagedDatabaseMember) {
  const s = m.status;
  if (s.error) return s.error.message;
  if (s.state === 'ready' || s.read.data_at) {
    if (s.synced === false) return '未同步';
    if (s.primary === false) return '非主分区';
    if (s.read_only) return '只读';
    return '正常';
  }
  return ({ queued: '等待读取', reading: '读取中', stale: '数据过期', timeout: '读取超时', error: '读取失败', offline: '离线', unavailable: '不可用', mismatch: '身份不一致', unknown: '未知', ready: '正常' } as Record<string, string>)[s.state] ?? s.state;
}
export function clusterHealth(d: ManagedDatabaseStatus) {
  const known = d.members.filter((m) => m.status.read.data_at || m.status.state === 'ready');
  const ok = d.members.filter((m) => memberTone(m) === 'ok').length;
  const tone: Tone = !known.length ? 'info' : ok === d.members.length ? 'ok' : ok === 0 ? 'bad' : 'warn';
  return { ok, total: d.members.length, tone, known: known.length };
}

export function Databases() {
  const q = useDatabases();
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const items = (q.data?.items ?? []).filter((d) => !d.cluster.deleted);
  return (
    <div className="page">
      <PageHead title="数据库" sub="MariaDB Galera 集群与 ProxySQL 代理；变更由协调节点推进，每个成员保留独立的执行结果。" actions={
        <>
          <Button variant="primary" icon={<Plus />} onClick={() => setCreating(true)}>新建集群</Button>
        </>
      } />
      <SharedStorageBanner />
      {q.isError && <ErrorNotice error={q.error} retry={() => void q.refetch()} />}
      {q.isPending ? (
        <div className="db-grid">{Array.from({ length: 3 }, (_, i) => <Card key={i} pad><Skeleton height={140} /></Card>)}</div>
      ) : items.length === 0 ? (
        <Card><Empty icon={<Database />} title="还没有数据库集群" action={<Button variant="primary" icon={<Plus />} onClick={() => setCreating(true)}>新建集群</Button>}>新建 Galera 集群，或接入已经在运行的集群。</Empty></Card>
      ) : (
        <motion.div className="db-grid" variants={listVariants} initial="hidden" animate="show">
          {items.map((d) => <ClusterCard key={d.cluster.id} d={d} onOpen={() => navigate(`/databases/${encodeURIComponent(d.cluster.id)}`)} />)}
        </motion.div>
      )}
      <CreateClusterDialog open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}

function SharedStorageBanner() {
  const q = useSharedStorage();
  const s = q.data?.local;
  if (!s) return null;
  const ok = s.binding && s.proxy_ready;
  return (
    <Notice tone={ok ? 'ok' : s.binding ? 'warn' : 'info'} title={ok ? 'OPSD 数据已绑定' : s.binding ? 'OPSD 数据代理未就绪' : '尚未绑定 OPSD 数据库'} icon={<Server />}
>
      {s.binding ? `${s.binding.database} · 阶段 ${s.phase}${s.pending ? ` · 待上传 ${s.pending}` : ''}` : '共享策略、跨节点流程与历史需要绑定一个 Galera 集群上的 opsd_core 库。'}
    </Notice>
  );
}

function ClusterCard({ d, onOpen }: { d: ManagedDatabaseStatus; onOpen: () => void }) {
  const h = clusterHealth(d);
  const connections = d.members.reduce((a, m) => a + (m.status.connections ?? 0), 0);
  const version = d.members.find((m) => m.status.version)?.status.version;
  return (
    <motion.div variants={itemVariants}>
      <Card hoverable className="db-card" onClick={onOpen} role="link" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter') onOpen(); }}>
        <div className="row-3">
          <span className="avatar"><Database /></span>
          <div className="col grow" style={{ gap: 0 }}>
            <strong className="ellipsis" style={{ fontWeight: 620, fontSize: 15 }}>{d.cluster.name}</strong>
            <span className="muted ellipsis" style={{ fontSize: 12 }}>{version ?? 'MariaDB Galera'} · 更新于 {ago(d.checked_at)}</span>
          </div>
          <Badge tone={h.tone} dot>{h.known ? `${h.ok}/${h.total} 正常` : '读取中'}</Badge>
        </div>
        <div className="db-stats">
          <div><b>{h.total}</b><span>成员</span></div>
          <div><b>{d.proxies.length}</b><span>代理</span></div>
          <div><b>{n0(connections)}</b><span>连接</span></div>
        </div>
        <div className="db-members">
          {d.members.map((m) => <span key={`${m.node_id}/${m.instance_id}`} className="db-member" title={memberLabel(m)}><Dot tone={memberTone(m)} />{m.name}</span>)}
          {d.proxies.map((p) => <span key={`p${p.node_id}/${p.instance_id}`} className="db-member" title={memberLabel(p)}><Dot tone={memberTone(p)} />{p.name}<Tag>proxy</Tag></span>)}
        </div>
        <div className="row muted" style={{ fontSize: 12.5 }}><span className="grow">协调节点 {d.cluster.coordinator.slice(0, 10)}</span><ArrowRight width={14} height={14} /></div>
      </Card>
    </motion.div>
  );
}

// ── 新建集群：填写 → 预览 → 确认 ─────────────────────────
type Preview = Schemas['DatabaseDeploymentPreview'];
function CreateClusterDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const session = useSession();
  const nodes = useNodes();
  const [step, setStep] = useState<0 | 1>(0);
  const [name, setName] = useState('');
  const [targets, setTargets] = useState<string[]>([]);
  const [advanced, setAdvanced] = useState({ image: '', sql_port: '', directory: '' });
  const [preview, setPreview] = useState<Preview | null>(null);
  useEffect(() => { if (!open) { setStep(0); setPreview(null); } }, [open]);
  const online = (nodes.data?.items ?? []).filter((n) => !n.revoked_at && n.connected);
  const makePreview = useCommand<void, Preview>({
    label: '预览集群部署',
    node: session.local_node_id,
    success: false,
    run: async () => (await call<Preview>('POST', '/database-clusters/deployment-previews', { body: {
      mode: 'create', name: name.trim(), expected_revision: 0, imports: [],
      targets: targets.map((node_id) => ({ node_id, ...(advanced.directory ? { directory: advanced.directory } : {}) })),
      options: { ...(advanced.image ? { image: advanced.image } : {}), ...(advanced.sql_port ? { sql_port: Number(advanced.sql_port) } : {}) },
    } })).data,
    onSuccess: (p) => { setPreview(p); setStep(1); },
  });
  const commit = useCommand<void, Operation>({
    label: '创建数据库集群',
    node: preview?.coordinator ?? session.local_node_id,
    run: async (_v, key) => (await call<Operation>('POST', '/database-clusters/deployments', { key, body: { coordinator: preview!.coordinator, token: preview!.token, revision: preview!.revision } })).data,
    onSuccess: onClose,
  });
  const blocked = !!preview && (preview.problems.length > 0 || preview.targets.some((t) => t.problems.length > 0));
  return (
    <Dialog open={open} onClose={onClose} width={680} title="新建数据库集群" sub={step === 0 ? '选择承载 Galera 成员的节点；建议三个及以上奇数个节点。' : '确认部署计划；预览在有效期内可以提交。'}
      footer={step === 0
        ? <><Button variant="ghost" onClick={onClose}>取消</Button><Button variant="primary" icon={<ArrowRight />} busy={makePreview.busy} disabled={!name.trim() || targets.length === 0} onClick={() => void makePreview.run()}>预览</Button></>
        : <><Button variant="ghost" icon={<ArrowLeft />} onClick={() => setStep(0)}>返回修改</Button><Button variant="primary" icon={<Check />} busy={commit.busy} disabled={blocked} onClick={() => void commit.run()}>开始部署</Button></>}>
      {step === 0 ? (
        <motion.div className="stack" initial={{ opacity: 0, x: -10 }} animate={{ opacity: 1, x: 0 }}>
          <Field label="集群名称"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如 主业务库" autoFocus /></Field>
          <Field label={`成员节点 · 已选 ${targets.length}`}>
            <div className="grant-grid">
              {online.map((n) => {
                const on = targets.includes(n.id);
                return (
                  <button key={n.id} type="button" className="grant" style={{ cursor: 'pointer', boxShadow: on ? 'inset 0 0 0 1.5px var(--accent)' : undefined, background: on ? 'var(--accent-soft)' : undefined }} onClick={() => setTargets((t) => (on ? t.filter((x) => x !== n.id) : [...t, n.id]))}>
                    <span className="col" style={{ gap: 0, minWidth: 0, textAlign: 'left' }}><strong className="ellipsis" style={{ fontWeight: 560 }}>{n.name}</strong><span className="dim ellipsis" style={{ fontSize: 11.5 }}>{n.region || n.address || n.id.slice(0, 10)}</span></span>
                    {on && <Check width={16} height={16} style={{ color: 'var(--accent)' }} />}
                  </button>
                );
              })}
            </div>
          </Field>
          {targets.length === 2 && <Notice tone="warn">两个成员在任一节点故障时会失去多数派；建议三个节点，或另加仲裁。</Notice>}
          <details>
            <summary className="muted" style={{ cursor: 'pointer', fontSize: 13 }}>高级设置</summary>
            <div className="form-grid" style={{ marginTop: 12 }}>
              <Field label="镜像" hint="留空使用默认固定版本"><Input mono value={advanced.image} onChange={(e) => setAdvanced((a) => ({ ...a, image: e.target.value }))} /></Field>
              <Field label="SQL 端口" hint="默认 3306"><Input type="number" value={advanced.sql_port} onChange={(e) => setAdvanced((a) => ({ ...a, sql_port: e.target.value }))} /></Field>
              <Field label="数据目录" hint="须为空目录" className="span-2"><Input mono value={advanced.directory} onChange={(e) => setAdvanced((a) => ({ ...a, directory: e.target.value }))} /></Field>
            </div>
          </details>
        </motion.div>
      ) : preview && (
        <motion.div className="stack" initial={{ opacity: 0, x: 10 }} animate={{ opacity: 1, x: 0 }}>
          {preview.problems.map((p, i) => <Notice key={i} tone="bad">{p}</Notice>)}
          <Card>
            <div className="list">
              {preview.targets.map((t) => (
                <div key={t.node_id} className="list-item" style={{ alignItems: 'flex-start' }}>
                  <Dot tone={t.problems.length ? 'bad' : 'ok'} />
                  <div className="li-main">
                    <span className="li-title">{t.name} <span className="muted mono" style={{ fontSize: 12 }}>{t.address}</span></span>
                    <span className="li-sub mono">{t.image} · {t.directory} · 端口 {t.ports.join(', ')}</span>
                    <span className="li-sub">Docker {t.docker_state} · 可用 {bytes(t.free_bytes)}</span>
                    {t.problems.map((p, i) => <span key={i} className="field-error">{p}</span>)}
                  </div>
                </div>
              ))}
            </div>
          </Card>
          {preview.network.length > 0 && (
            <Card>
              <div className="card-body stack" style={{ gap: 8 }}>
                <strong style={{ fontWeight: 600 }}>将添加的防火墙规则</strong>
                {preview.network.map((n, i) => <div key={i} className="row mono" style={{ fontSize: 12.5 }}><Tag>{n.protocol}</Tag>{n.source} → {n.ports}</div>)}
              </div>
            </Card>
          )}
          <span className="muted" style={{ fontSize: 12.5 }}>预览于 {new Date(preview.expires_at).toLocaleTimeString('zh-CN', { hour12: false })} 过期。</span>
        </motion.div>
      )}
    </Dialog>
  );
}

