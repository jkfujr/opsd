import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
import { motion } from 'motion/react';
import { Check, Copy, KeyRound, Plus, Server, Terminal, Trash2, Ellipsis, ShieldCheck, Boxes } from 'lucide-react';
import { call, getVersioned, enc } from '../api/http';
import type { EnrollmentTokenCreated, EnrollmentToken, NodeStatus } from '../api/types';
import { useEnrollments, useNodes, useOverview } from '../app/queries';
import { useCommand } from '../app/commands';
import { useSession } from '../app/session';
import { confirm } from '../app/confirm';
import { invalidateResource } from '../app/query';
import { ago, copyText, initials, pct, ratio, fullTime } from '../lib/format';
import { usageTone } from '../lib/status';
import { Badge, Button, Card, CardHead, CopyText, Dialog, Dot, Empty, ErrorNotice, Field, Input, Menu, Meter, PageHead, SearchInput, Segmented, Skeleton, IconButton, Notice, Tag } from '../ui/kit';
import { toast } from '../app/toast';

type Filter = 'all' | 'online' | 'offline';

export function Nodes() {
  const nodes = useNodes();
  const overview = useOverview();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<Filter>('all');
  const adding = params.get('add') === '1';

  const readings = useMemo(() => new Map((overview.data?.nodes ?? []).map((n) => [n.node.id, n.reading])), [overview.data]);
  const list = (nodes.data?.items ?? []).filter((n) => !n.revoked_at);
  const shown = list.filter((n) => {
    if (filter === 'online' && !n.connected) return false;
    if (filter === 'offline' && n.connected) return false;
    const q = search.trim().toLowerCase();
    return !q || `${n.name} ${n.region ?? ''} ${n.address ?? ''} ${n.id}`.toLowerCase().includes(q);
  }).sort((a, b) => Number(b.local) - Number(a.local) || a.name.localeCompare(b.name, 'zh-CN'));

  return (
    <div className="page">
      <PageHead title="节点" sub="集群中的对等节点；每个节点独立保存身份、账号与本机队列。" actions={<Button variant="primary" icon={<Plus />} onClick={() => setParams({ add: '1' })}>添加节点</Button>} />
      <div className="toolbar">
        <SearchInput className="search" value={search} onChange={setSearch} placeholder="名称、地域、地址或 ID" />
        <Segmented value={filter} onChange={setFilter} options={[
          { value: 'all', label: '全部', count: list.length },
          { value: 'online', label: '在线', count: list.filter((n) => n.connected).length },
          { value: 'offline', label: '离线', count: list.filter((n) => !n.connected).length },
        ]} />
      </div>
      {nodes.isError && <ErrorNotice error={nodes.error} retry={() => void nodes.refetch()} />}
      <Card>
        {nodes.isPending ? (
          <div className="stack" style={{ padding: 20 }}>{Array.from({ length: 4 }, (_, i) => <Skeleton key={i} height={40} />)}</div>
        ) : shown.length === 0 ? (
          <Empty icon={<Server />} title="没有匹配的节点">调整筛选条件或添加新节点。</Empty>
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead><tr><th>节点</th><th>状态</th><th className="hide-sm">地址</th><th style={{ width: 140 }}>CPU</th><th style={{ width: 140 }}>内存</th><th className="actions-cell" /></tr></thead>
              <motion.tbody initial="hidden" animate="show" variants={{ hidden: {}, show: { transition: { staggerChildren: 0.03 } } }}>
                {shown.map((n) => <NodeRow key={n.id} node={n} reading={readings.get(n.id)} onOpen={() => navigate(`/nodes/${enc(n.id)}`)} />)}
              </motion.tbody>
            </table>
          </div>
        )}
      </Card>
      <Enrollments />
      <AddNodeDialog open={adding} onClose={() => { params.delete('add'); setParams(params, { replace: true }); }} />
    </div>
  );
}

function NodeRow({ node, reading, onOpen }: { node: NodeStatus; reading?: { metrics?: { cpu_usage?: number; memory_used?: number; memory_total?: number } }; onOpen: () => void }) {
  const navigate = useNavigate();
  const m = reading?.metrics;
  const mem = ratio(m?.memory_used, m?.memory_total);
  return (
    <motion.tr className="clickable" onClick={onOpen} variants={{ hidden: { opacity: 0, y: 6 }, show: { opacity: 1, y: 0 } }}>
      <td>
        <div className="row-3">
          <span className="avatar">{initials(node.name)}</span>
          <div className="col" style={{ gap: 0 }}>
            <span className="row"><strong style={{ fontWeight: 580 }}>{node.name}</strong>{node.local && <Badge outline>本机</Badge>}{node.console_enabled && <Tag>控制台</Tag>}</span>
            <span className="muted" style={{ fontSize: 12 }}>{node.region || '未设置地域'}</span>
          </div>
        </div>
      </td>
      <td>
        {node.connected ? <Badge tone={node.writable ? 'ok' : 'warn'} dot title={node.write_blocked_reason}>{node.writable ? '在线' : '只读'}</Badge> : <Badge tone="idle" dot>离线 {node.last_seen_at ? `· ${ago(node.last_seen_at)}` : ''}</Badge>}
      </td>
      <td className="hide-sm"><span className="mono muted" style={{ fontSize: 12.5 }}>{node.address || '—'}</span></td>
      <td><div className="col" style={{ gap: 4 }}><span className="num" style={{ fontSize: 12.5 }}>{pct(m?.cpu_usage)}</span><Meter value={m?.cpu_usage} tone={usageTone(m?.cpu_usage)} thin /></div></td>
      <td><div className="col" style={{ gap: 4 }}><span className="num" style={{ fontSize: 12.5 }}>{pct(mem)}</span><Meter value={mem} tone={usageTone(mem)} thin /></div></td>
      <td className="actions-cell" onClick={(e) => e.stopPropagation()}>
        <Menu trigger={(p) => <IconButton label="更多" size="sm" {...p}><Ellipsis /></IconButton>} items={[
          { label: '打开终端', icon: <Terminal />, onSelect: () => navigate(`/tools/terminal?node=${enc(node.id)}`), disabled: !node.connected },
          { label: '防火墙', icon: <ShieldCheck />, onSelect: () => navigate(`/firewall?node=${enc(node.id)}`) },
          { label: '容器', icon: <Boxes />, onSelect: () => navigate(`/containers?node=${enc(node.id)}`) },
        ]} />
      </td>
    </motion.tr>
  );
}

function Enrollments() {
  const list = useEnrollments();
  const items = (list.data?.items ?? []).filter((t) => !t.consumed_at && !t.revoked_at && Date.parse(t.expires_at) > Date.now());
  if (!items.length) return null;
  return (
    <Card>
      <CardHead title="有效接入码" sub="24 小时内有效，使用一次后失效" icon={<KeyRound width={18} height={18} style={{ color: 'var(--text-3)' }} />} />
      <div className="list">
        {items.map((t) => <EnrollmentRow key={t.id} token={t} />)}
      </div>
    </Card>
  );
}

function EnrollmentRow({ token }: { token: EnrollmentToken }) {
  const session = useSession();
  const revoke = useCommand<void, unknown>({
    label: '撤销接入码',
    node: session.local_node_id,
    run: async () => {
      const current = await getVersioned<EnrollmentToken>(`/enrollment-tokens/${enc(token.id)}`);
      await call('DELETE', `/enrollment-tokens/${enc(token.id)}`, { ifMatch: current.etag });
    },
    onSuccess: () => invalidateResource('nodes'),
  });
  return (
    <div className="list-item">
      <div className="li-main">
        <span className="li-title mono" style={{ fontSize: 13 }}>{token.id}</span>
        <span className="li-sub">创建于 {ago(token.created_at)} · {fullTime(token.expires_at)} 过期</span>
      </div>
      <Button size="sm" variant="ghost" icon={<Trash2 />} busy={revoke.busy} onClick={async () => { if (await confirm({ title: '撤销这个接入码？', body: '尚未使用此接入码的安装命令将无法加入集群。', danger: true, confirmLabel: '撤销' })) void revoke.run(); }}>撤销</Button>
    </div>
  );
}

export function AddNodeDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const session = useSession();
  const [name, setName] = useState('');
  const [region, setRegion] = useState('');
  const [created, setCreated] = useState<EnrollmentTokenCreated | null>(null);
  const [tool, setTool] = useState<'curl' | 'wget'>('curl');
  const [copied, setCopied] = useState(false);
  useEffect(() => { if (!open) { setCreated(null); setCopied(false); } }, [open]);
  const create = useCommand<void, EnrollmentTokenCreated>({
    label: '生成接入码',
    node: session.local_node_id,
    run: async (_v, key) => (await call<EnrollmentTokenCreated>('POST', '/enrollment-tokens', { key })).data,
    onSuccess: (r) => { setCreated(r); invalidateResource('nodes'); },
    success: false,
  });
  const command = created ? buildCommand(created, tool, name, region) : '';
  return (
    <Dialog open={open} onClose={onClose} width={640} title="添加节点" sub="在 Debian / Ubuntu 的 systemd 主机上以 root 执行接入命令。"
      footer={created ? <><span className="left muted" style={{ fontSize: 12.5 }}>接入码 {fullTime(created.record.expires_at)} 过期</span><Button onClick={onClose}>完成</Button></> : <><Button variant="ghost" onClick={onClose}>取消</Button><Button variant="primary" busy={create.busy} onClick={() => void create.run()}>生成接入命令</Button></>}
    >
      <div className="enroll-steps">
        <div className="enroll-step">
          <span className="n">1</span>
          <div className="form-grid">
            <Field label="节点名称" hint="可选，稍后也可修改"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如 东京-01" maxLength={64} /></Field>
            <Field label="地域" hint="可选"><Input value={region} onChange={(e) => setRegion(e.target.value)} placeholder="例如 ap-northeast" maxLength={64} /></Field>
          </div>
        </div>
        <div className="enroll-step">
          <span className="n">2</span>
          <div className="stack" style={{ gap: 10 }}>
            {!created ? (
              <Notice tone="info">接入码一次性使用，24 小时内有效；完整值只在生成时显示一次。</Notice>
            ) : (
              <motion.div className="stack" style={{ gap: 10 }} initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }}>
                <div className="row">
                  <Segmented size="sm" value={tool} onChange={setTool} options={[{ value: 'curl', label: 'curl' }, { value: 'wget', label: 'wget' }]} />
                  <span className="spacer" />
                  <Button size="sm" variant="primary" icon={copied ? <Check /> : <Copy />} onClick={async () => { if (await copyText(command)) { setCopied(true); toast('接入命令已复制', 'ok'); } }}>{copied ? '已复制' : '复制命令'}</Button>
                </div>
                <pre className="code" style={{ maxHeight: 220 }}>{command}</pre>
                <div className="kv" style={{ fontSize: 12.5 }}>
                  <dt>种子节点</dt><dd><CopyText value={created.peer_address} /></dd>
                  <dt>版本</dt><dd className="mono">{created.version}</dd>
                </div>
              </motion.div>
            )}
          </div>
        </div>
        <div className="enroll-step">
          <span className="n">3</span>
          <div className="muted" style={{ fontSize: 13, paddingTop: 3 }}>安装完成后会输出新节点的账号、密码与登录入口；节点上线后会自动出现在列表中。</div>
        </div>
      </div>
    </Dialog>
  );
}

const q = (v: string) => `'${v.replace(/'/g, `'\\''`)}'`;
function buildCommand(c: EnrollmentTokenCreated, tool: 'curl' | 'wget', name: string, region: string) {
  const args = [`-e ${q(c.peer_address)}`, `-k ${q(c.peer_public_key)}`, `-t ${q(c.token)}`];
  if (name.trim()) args.push(`-n ${q(name.trim())}`);
  if (region.trim()) args.push(`-r ${q(region.trim())}`);
  const fetcher = tool === 'curl' ? `curl -fsSL ${q(c.installer_url)}` : `wget -qO- ${q(c.installer_url)}`;
  return `${fetcher} | sudo sh -s -- \\\n  ${args.join(' \\\n  ')}`;
}
