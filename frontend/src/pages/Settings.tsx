import { useEffect, useState, useRef } from 'react';
import { NavLink, useParams } from 'react-router';
import { AnimatePresence, motion } from 'motion/react';
import { Info, KeyRound, MonitorSmartphone, Palette, Plus, RefreshCw, Trash2, UserRound, Ellipsis, Power, LayoutTemplate, Upload } from 'lucide-react';
import { call, enc, getVersioned, isUnknown } from '../api/http';
import type { APIGrant, APIToken, APITokenCreated, AccountChanged, ThemeSelectionState, ThemeInstallation } from '../api/types';
import { useAccount, useIdentity, useSessions, useThemes, useTokens } from '../app/queries';
import { useCommand } from '../app/commands';
import { confirm } from '../app/confirm';
import { endSession, holdUnauthorized, releaseUnauthorized, setLoginAddress, useSession } from '../app/session';
import { invalidateResource } from '../app/query';
import { ago, fullTime } from '../lib/format';
import { setPrefs, usePrefs, type ThemeChoice } from '../lib/prefs';
import { Badge, Button, Card, CardHead, CopyText, Dialog, Empty, ErrorNotice, Field, IconButton, Input, Menu, Notice, PageHead, Segmented, Select, Skeleton, Switch, Tag, spring } from '../ui/kit';

const sections = [
  { id: 'account', label: '账号', icon: UserRound },
  { id: 'sessions', label: '登录会话', icon: MonitorSmartphone },
  { id: 'api', label: 'API 令牌', icon: KeyRound },
  { id: 'appearance', label: '外观', icon: Palette },
  { id: 'about', label: '版本', icon: Info },
] as const;

export function Settings() {
  const { section = 'account' } = useParams();
  return (
    <div className="page">
      <PageHead title="设置" sub="账号、会话与令牌只属于当前入口节点；外观偏好保存在本浏览器。" />
      <div className="settings">
        <nav className="settings-nav">
          {sections.map((s) => {
            const Icon = s.icon;
            return (
              <NavLink key={s.id} to={`/settings/${s.id}`} className={({ isActive }) => (isActive ? 'active' : '')}>
                {({ isActive }) => (<>{isActive && <motion.span layoutId="settings-nav" className="sn-bg" transition={spring} />}<Icon />{s.label}</>)}
              </NavLink>
            );
          })}
        </nav>
        <AnimatePresence mode="wait">
          <motion.div key={section} className="stack" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -4 }} transition={{ duration: 0.22 }}>
            {section === 'account' && <AccountSection />}
            {section === 'sessions' && <SessionsSection />}
            {section === 'api' && <TokensSection />}
            {section === 'appearance' && <AppearanceSection />}
            {section === 'about' && <AboutSection />}
          </motion.div>
        </AnimatePresence>
      </div>
    </div>
  );
}

function AccountSection() {
  const session = useSession();
  const q = useAccount();
  const [form, setForm] = useState({ username: '', entrance: '', password: '', confirm: '', current: '' });
  // 服务端值变化时只更新用户未改动的字段，切回窗口不会冲掉草稿。
  const base = useRef<{ username: string; entrance: string } | null>(null);
  useEffect(() => {
    if (!q.data) return;
    const next = q.data.value, prev = base.current;
    setForm((f) => ({
      ...f,
      username: !prev || f.username === prev.username ? next.username : f.username,
      entrance: !prev || f.entrance === prev.entrance ? next.entrance : f.entrance,
    }));
    base.current = { username: next.username, entrance: next.entrance };
  }, [q.data?.etag]); // eslint-disable-line react-hooks/exhaustive-deps
  const v = q.data?.value;
  const save = useCommand<void, AccountChanged>({
    label: '保存账号',
    node: session.local_node_id,
    success: false,
    run: async (_x, key) => {
      const body: Record<string, string> = { current_password: form.current };
      if (form.username !== v?.username) body.username = form.username;
      if (form.entrance !== v?.entrance) body.entrance = form.entrance;
      if (form.password) body.password = form.password;
      holdUnauthorized();
      try {
        return (await call<AccountChanged>('PATCH', '/settings/account', { key, ifMatch: q.data?.etag, body })).data;
      } catch (reason) {
        // 明确失败或结果未知都立即解防；期间若收到过 401，release 会补处理。
        releaseUnauthorized();
        throw reason;
      }
    },
    // 先记录新入口并解防（不补处理，本次由这里完成跳转），再用应答里的入口退出。
    onSuccess: (r) => { setLoginAddress(r.login_address); releaseUnauthorized(false); endSession(r.login_address, false); },
    onConflict: () => void q.refetch(),
  });
  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  if (!v) return <Card pad><Skeleton height={200} /></Card>;
  const changed = form.username !== v.username || form.entrance !== v.entrance || !!form.password;
  const mismatch = !!form.password && form.password !== form.confirm;
  return (
    <Card>
      <CardHead title="管理员账号" sub="修改后需要使用新的凭据重新登录；其他会话会被撤销。" />
      <div className="card-body stack">
        <div className="form-grid">
          <Field label="账号"><Input value={form.username} onChange={(e) => setForm((f) => ({ ...f, username: e.target.value }))} autoComplete="username" /></Field>
          <Field label="安全入口" hint="登录页路径，未登录访问其他路径不会暴露控制台"><Input mono value={form.entrance} onChange={(e) => setForm((f) => ({ ...f, entrance: e.target.value }))} /></Field>
          <Field label="新密码" hint="留空表示不修改"><Input type="password" value={form.password} onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))} autoComplete="new-password" /></Field>
          <Field label="确认新密码" error={mismatch ? '两次输入不一致' : undefined}><Input type="password" value={form.confirm} aria-invalid={mismatch} onChange={(e) => setForm((f) => ({ ...f, confirm: e.target.value }))} autoComplete="new-password" /></Field>
        </div>
        <AnimatePresence>
          {changed && (
            <motion.div initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: 'auto' }} exit={{ opacity: 0, height: 0 }} style={{ overflow: 'hidden' }}>
              <div className="stack" style={{ paddingTop: 4 }}>
                <hr className="divider" />
                <Field label="当前密码" hint="保存前需要验证当前密码"><Input type="password" value={form.current} onChange={(e) => setForm((f) => ({ ...f, current: e.target.value }))} autoComplete="current-password" /></Field>
                <div className="row"><Button variant="primary" busy={save.busy} disabled={!form.current || mismatch} onClick={() => void save.run()}>保存并重新登录</Button><Button variant="ghost" onClick={() => setForm({ username: v.username, entrance: v.entrance, password: '', confirm: '', current: '' })}>放弃</Button></div>
              </div>
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </Card>
  );
}

function SessionsSection() {
  const q = useSessions();
  const session = useSession();
  const revoke = useCommand<string, unknown>({
    label: '撤销会话',
    node: session.local_node_id,
    run: async (id) => { await call('DELETE', `/auth/sessions/${enc(id)}`); },
    onSuccess: () => invalidateResource('sessions'),
  });
  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  return (
    <Card>
      <CardHead title="登录会话" sub="当前节点上的管理员浏览器会话" />
      {!q.data ? <div className="card-body"><Skeleton height={120} /></div> : (
        <div className="list">
          {q.data.items.map((s) => (
            <div key={s.id} className="list-item">
              <span className="avatar"><MonitorSmartphone /></span>
              <div className="li-main">
                <span className="li-title">{s.remote_address || '未知地址'} {s.current && <Badge tone="accent">当前</Badge>}</span>
                <span className="li-sub">登录于 {fullTime(s.created_at)} · 活动 {ago(s.last_seen_at)} · {fullTime(s.expires_at)} 过期</span>
              </div>
              {!s.current && <Button size="sm" variant="ghost" icon={<Trash2 />} busy={revoke.busy} onClick={async () => { if (await confirm({ title: '撤销这个会话？', body: '该浏览器需要重新登录。', danger: true, confirmLabel: '撤销' })) void revoke.run(s.id); }}>撤销</Button>}
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

const moduleNames: Record<APIGrant['module'], string> = { monitoring: '监控', nodes: '节点', containers: '容器', firewall: '防火墙', databases: '数据库', tasks: '任务（可执行 root Shell）', compose: 'Compose' };
const templates: { label: string; grants: APIGrant[] }[] = [
  { label: '只读监控', grants: [{ module: 'monitoring', level: 'read', scope: 'all', resources: [] }] },
  { label: '运维只读', grants: (['monitoring', 'nodes', 'containers', 'firewall', 'databases', 'compose'] as const).map((m) => ({ module: m, level: 'read' as const, scope: 'all' as const, resources: [] })) },
  { label: '容器运维', grants: [{ module: 'monitoring', level: 'read', scope: 'all', resources: [] }, { module: 'containers', level: 'manage', scope: 'all', resources: [] }, { module: 'compose', level: 'manage', scope: 'all', resources: [] }] },
];

function TokensSection() {
  const session = useSession();
  const node = session.local_node_id;
  const q = useTokens(node);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<APITokenCreated | null>(null);
  const patch = useCommand<{ t: APIToken; body: Record<string, unknown>; label: string }, APIToken>({
    label: (v) => v.label,
    node,
    run: async ({ t, body }) => { const cur = await getVersioned<APIToken>(`/nodes/${enc(node)}/api-tokens/${enc(t.id)}`); return (await call<APIToken>('PATCH', `/nodes/${enc(node)}/api-tokens/${enc(t.id)}`, { ifMatch: cur.etag, body })).data; },
    onSuccess: () => invalidateResource('tokens'),
  });
  const remove = useCommand<APIToken, unknown>({
    label: '删除令牌',
    node,
    run: async (t) => { const cur = await getVersioned<APIToken>(`/nodes/${enc(node)}/api-tokens/${enc(t.id)}`); await call('DELETE', `/nodes/${enc(node)}/api-tokens/${enc(t.id)}`, { ifMatch: cur.etag }); },
    onSuccess: () => invalidateResource('tokens'),
  });
  const regen = useCommand<APIToken, APITokenCreated>({
    label: '重新生成令牌',
    node,
    success: false,
    run: async (t, key) => (await call<APITokenCreated>('POST', `/nodes/${enc(node)}/api-tokens/${enc(t.id)}/actions`, { key, body: { action: 'regenerate', expected_revision: t.revision } })).data,
    onSuccess: (r) => { setCreated(r); invalidateResource('tokens'); },
  });
  return (
    <>
      <Card>
        <CardHead title="API 令牌" sub="Bearer 令牌访问 /api/v2 与 /metrics；完整值只在生成时显示一次。" actions={<Button size="sm" variant="primary" icon={<Plus />} onClick={() => setCreating(true)}>创建令牌</Button>} />
        {q.isError ? <div className="card-body"><ErrorNotice error={q.error} retry={() => void q.refetch()} /></div> : !q.data ? <div className="card-body"><Skeleton height={100} /></div> : q.data.items.length === 0 ? <Empty icon={<KeyRound />} title="还没有令牌">用于 Prometheus 抓取、自动化脚本等外部调用。</Empty> : (
          <div className="list">
            {q.data.items.map((t) => (
              <div key={t.id} className="list-item">
                <span className="avatar"><KeyRound /></span>
                <div className="li-main">
                  <span className="li-title">{t.name} {!t.enabled && <Badge>已停用</Badge>}{t.expires_at && Date.parse(t.expires_at) < Date.now() && <Badge tone="warn">已过期</Badge>}</span>
                  <span className="li-sub">{t.grants.map((g) => `${moduleNames[g.module].replace(/（.*）/, '')}·${g.level === 'manage' ? '管理' : '只读'}`).join('、') || '无权限'} · 创建于 {ago(t.created_at)}{t.expires_at ? ` · ${fullTime(t.expires_at)} 过期` : ''}</span>
                </div>
                <Menu trigger={(p) => <IconButton size="sm" label="更多" {...p}><Ellipsis /></IconButton>} items={[
                  { label: t.enabled ? '停用' : '启用', icon: <Power />, onSelect: () => void patch.run({ t, body: { enabled: !t.enabled }, label: t.enabled ? '停用令牌' : '启用令牌' }) },
                  { label: '重新生成', icon: <RefreshCw />, onSelect: async () => { if (await confirm({ title: '重新生成令牌？', body: '旧值会立即失效，使用它的脚本需要更新。', confirmLabel: '重新生成' })) void regen.run(t); } },
                  { separator: true, label: '' },
                  { label: '删除', icon: <Trash2 />, danger: true, onSelect: async () => { if (await confirm({ title: `删除令牌 ${t.name}？`, danger: true, confirmLabel: '删除' })) void remove.run(t); } },
                ]} />
              </div>
            ))}
          </div>
        )}
      </Card>
      <CreateTokenDialog open={creating} onClose={() => setCreating(false)} onCreated={(r) => { setCreating(false); setCreated(r); }} />
      <Dialog open={!!created} onClose={() => setCreated(null)} width={600} title="令牌已生成" sub="这是唯一一次显示完整值，请立即复制保存。" footer={<Button variant="primary" onClick={() => setCreated(null)}>我已保存</Button>}>
        {created && (
          <>
            <div className="secret-box"><span className="grow">{created.value}</span><CopyText value={created.value} display="复制" /></div>
            <pre className="code">{`curl -fsS "${window.location.origin}/api/v2/" \\\n  -H "Authorization: Bearer ${created.value}"`}</pre>
            <Field label="Prometheus 抓取配置"><pre className="code">{`- job_name: opsd\n  metrics_path: /metrics\n  static_configs:\n    - targets: ['${window.location.host}']\n  authorization:\n    type: Bearer\n    credentials: '${created.value}'`}</pre></Field>
          </>
        )}
      </Dialog>
    </>
  );
}

function CreateTokenDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (r: APITokenCreated) => void }) {
  const session = useSession();
  const [name, setName] = useState('');
  const [grants, setGrants] = useState<APIGrant[]>(templates[0]!.grants);
  const [expires, setExpires] = useState('');
  const [ips, setIps] = useState('');
  useEffect(() => { if (open) { setName(''); setGrants(templates[0]!.grants); setExpires(''); setIps(''); } }, [open]);
  const create = useCommand<void, APITokenCreated>({
    label: '创建令牌',
    node: session.local_node_id,
    success: false,
    run: async (_v, key) => (await call<APITokenCreated>('POST', `/nodes/${enc(session.local_node_id)}/api-tokens`, { key, body: { name: name.trim(), enabled: true, grants, source_ips: ips.split(/[\s,]+/).filter(Boolean), ...(expires ? { expires_at: new Date(expires).toISOString() } : {}) } })).data,
    onSuccess: onCreated,
  });
  const level = (m: APIGrant['module']) => grants.find((g) => g.module === m)?.level ?? 'none';
  const setLevel = (m: APIGrant['module'], l: 'none' | 'read' | 'manage') => setGrants((gs) => { const rest = gs.filter((g) => g.module !== m); return l === 'none' ? rest : [...rest, { module: m, level: l, scope: 'all', resources: [] }]; });
  return (
    <Dialog open={open} onClose={onClose} width={640} title="创建 API 令牌" footer={<><Button variant="ghost" onClick={onClose}>取消</Button><Button variant="primary" busy={create.busy} disabled={!name.trim()} onClick={() => void create.run()}>生成令牌</Button></>}>
      <Field label="名称"><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如 prometheus" autoFocus /></Field>
      <Field label="模板"><div className="row wrap">{templates.map((t) => <button key={t.label} type="button" className="badge outline" style={{ cursor: 'pointer', height: 28 }} onClick={() => setGrants(t.grants)}>{t.label}</button>)}</div></Field>
      <Field label="权限">
        <div className="col" style={{ gap: 6 }}>
          {(Object.keys(moduleNames) as APIGrant['module'][]).map((m) => (
            <div key={m} className="row" style={{ justifyContent: 'space-between' }}>
              <span style={{ color: m === 'tasks' ? 'var(--warn)' : undefined }}>{moduleNames[m]}</span>
              <Segmented size="sm" value={level(m)} onChange={(l) => setLevel(m, l)} options={m === 'monitoring' ? [{ value: 'none', label: '无' }, { value: 'read', label: '只读' }] : [{ value: 'none', label: '无' }, { value: 'read', label: '只读' }, { value: 'manage', label: '管理' }]} />
            </div>
          ))}
        </div>
      </Field>
      <details>
        <summary className="muted" style={{ cursor: 'pointer', fontSize: 13 }}>可选：有效期与来源 IP</summary>
        <div className="form-grid" style={{ marginTop: 12 }}>
          <Field label="过期时间"><Input type="datetime-local" value={expires} onChange={(e) => setExpires(e.target.value)} /></Field>
          <Field label="来源 IP" hint="逗号或空格分隔，留空不限制"><Input mono value={ips} onChange={(e) => setIps(e.target.value)} /></Field>
        </div>
      </details>
    </Dialog>
  );
}

const swatches = [166, 145, 200, 240, 275, 320, 20, 55, 90];
function AppearanceSection() {
  const p = usePrefs();
  return (
    <>
      <Card>
        <CardHead title="主题" />
        <div className="card-body stack">
          <div className="theme-cards" role="radiogroup" aria-label="颜色模式">
            {(['dark', 'light', 'system'] as ThemeChoice[]).map((t) => (
              <button key={t} type="button" role="radio" className="theme-card" aria-checked={p.theme === t} onClick={() => setPrefs({ theme: t })}>
                <div className="preview" style={{ background: t === 'light' ? '#f4f5f8' : t === 'dark' ? '#121318' : 'linear-gradient(90deg,#121318 50%,#f4f5f8 50%)' }}>
                  <div style={{ background: t === 'light' ? '#ffffff' : '#1a1c22', borderRight: '1px solid rgba(127,127,127,.15)' }} />
                  <div style={{ padding: 10, display: 'flex', flexDirection: 'column', gap: 6 }}>
                    <span style={{ height: 6, width: '60%', borderRadius: 4, background: 'var(--accent)' }} />
                    <span style={{ height: 6, width: '80%', borderRadius: 4, background: 'rgba(127,127,127,.3)' }} />
                    <span style={{ height: 6, width: '45%', borderRadius: 4, background: 'rgba(127,127,127,.3)' }} />
                  </div>
                </div>
                <span style={{ fontWeight: 560 }}>{{ dark: '深色', light: '浅色', system: '跟随系统' }[t]}</span>
              </button>
            ))}
          </div>
        </div>
      </Card>
      <Card>
        <CardHead title="强调色" sub="影响按钮、选中状态与图表" />
        <div className="card-body stack">
          <div className="swatches">{swatches.map((h) => <button key={h} type="button" className="swatch" aria-pressed={Math.round(p.hue) === h} aria-label={`色相 ${h}`} style={{ background: `oklch(0.8 0.15 ${h})` }} onClick={() => setPrefs({ hue: h })} />)}</div>
          <div className="hue-wrap"><div className="hue-strip" /><input className="hue" type="range" min={0} max={360} value={p.hue} onChange={(e) => setPrefs({ hue: Number(e.target.value) })} aria-label="色相" /></div>
        </div>
      </Card>
      <Card>
        <CardHead title="布局" />
        <div className="list">
          <div className="list-item"><div className="li-main"><span className="li-title">紧凑密度</span><span className="li-sub">减小表格行高与控件高度</span></div><Switch checked={p.density === 'compact'} onChange={(v) => setPrefs({ density: v ? 'compact' : 'comfortable' })} label="紧凑密度" /></div>
          <div className="list-item"><div className="li-main"><span className="li-title">固定侧栏</span><span className="li-sub">始终显示导航文字</span></div><Switch checked={p.pinned} onChange={(v) => setPrefs({ pinned: v })} label="固定侧栏" /></div>
          <div className="list-item"><div className="li-main"><span className="li-title">终端字号</span></div><Select value={String(p.terminalSize)} onChange={(e) => setPrefs({ terminalSize: Number(e.target.value) })} style={{ width: 100 }}>{Array.from({ length: 12 }, (_, i) => i + 11).map((s) => <option key={s} value={s}>{s}</option>)}</Select></div>
        </div>
      </Card>
      <ConsoleThemes />
    </>
  );
}

function ConsoleThemes() {
  const q = useThemes();
  const session = useSession();
  const [file, setFile] = useState<File | null>(null);
  const [repo, setRepo] = useState('');
  const [tag, setTag] = useState('');
  const fileInput = useRef<HTMLInputElement>(null);
  const select = useCommand<string, ThemeSelectionState>({
    label: '切换控制台主题',
    node: session.local_node_id,
    run: async (installation_id) => { const cur = await getVersioned<ThemeSelectionState>('/console/theme-selection'); return (await call<ThemeSelectionState>('PUT', '/console/theme-selection', { ifMatch: cur.etag, body: { installation_id } })).data; },
    success: '已切换主题，点击“刷新界面”生效',
    onSuccess: () => invalidateResource('themes'),
    onConflict: () => void q.refetch(),
  });
  const install = useCommand<{ file?: File; repository?: string; tag?: string }, ThemeInstallation>({
    label: '安装主题',
    node: session.local_node_id,
    run: async (v, key) => (v.file
      ? await call<ThemeInstallation>('POST', '/console/theme-installations', { key, raw: v.file, contentType: 'application/zip' })
      : await call<ThemeInstallation>('POST', '/console/theme-installations', { key, body: { repository: v.repository, ...(v.tag ? { tag: v.tag } : {}) } })).data,
    success: (r) => `已安装 ${r.manifest.name} ${r.manifest.version}`,
    onSuccess: () => { setFile(null); setRepo(''); setTag(''); if (fileInput.current) fileInput.current.value = ''; void q.refetch(); },
  });
  const remove = useCommand<ThemeInstallation, unknown>({
    label: '删除主题',
    node: session.local_node_id,
    run: async (t) => {
      const cur = await getVersioned<ThemeInstallation>(`/console/theme-installations/${enc(t.installation_id)}`);
      await call('DELETE', `/console/theme-installations/${enc(t.installation_id)}`, { ifMatch: cur.etag });
    },
    success: '已删除主题',
    onSuccess: () => void q.refetch(),
  });
  // 上传应答未知时只允许重新读取列表，不自动重传。
  const uncertain = install.error != null && isUnknown(install.error);
  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  if (!q.data) return <Card pad><Skeleton height={160} /></Card>;
  const active = q.data.active_installation_id || 'builtin';
  const effective = q.data.effective_installation_id || 'builtin';
  return (
    <Card>
      <CardHead title="控制台主题" sub="当前节点为所有访问者提供的整套前端，切换只影响此节点。主题异常时访问 /__opsd/builtin/，或在登录地址后加 ?builtin=1 回到内置界面。" icon={<LayoutTemplate width={18} height={18} style={{ color: 'var(--text-3)' }} />} actions={
        <>
          <Button size="sm" variant="ghost" icon={<RefreshCw />} onClick={() => window.location.assign('/')}>刷新界面</Button>
          <Button size="sm" disabled={active === 'builtin' || select.busy} onClick={() => void select.run('builtin')}>切回内置</Button>
        </>
      } />
      <div className="table-wrap">
        <table className="table">
          <thead><tr><th>主题</th><th>状态</th><th className="actions-cell" /></tr></thead>
          <tbody>
            <tr>
              <td><strong style={{ fontWeight: 580 }}>内置界面</strong><div className="muted" style={{ fontSize: 12 }}>随程序发布</div></td>
              <td>{effective === 'builtin' ? <Badge tone="ok" dot>正在提供</Badge> : <Badge>可用</Badge>}</td>
              <td className="actions-cell">{active === 'builtin' && <Badge tone="accent">已选择</Badge>}</td>
            </tr>
            {q.data.items.map((t) => (
              <tr key={t.installation_id}>
                <td>
                  <span className="row"><strong style={{ fontWeight: 580 }}>{t.manifest.name}</strong><Tag>{t.manifest.version}</Tag>{t.manifest.login === 'theme' && <Tag>自带登录</Tag>}</span>
                  <div className="muted ellipsis" style={{ fontSize: 12, maxWidth: 420 }}>{t.source} · 安装于 {ago(t.installed_at)}</div>
                </td>
                <td>{!t.available ? <Badge tone="bad" title={t.problem}>{t.problem ?? '不可用'}</Badge> : effective === t.installation_id ? <Badge tone="ok" dot>正在提供</Badge> : <Badge>可用</Badge>}</td>
                <td className="actions-cell">
                  <div className="row" style={{ justifyContent: 'flex-end' }}>
                    {active === t.installation_id ? <Badge tone="accent">已选择</Badge> : (
                      <>
                        <Button size="sm" disabled={!t.available || select.busy} onClick={() => void select.run(t.installation_id)}>启用</Button>
                        <Button size="sm" variant="ghost" disabled={remove.busy} onClick={async () => { if (await confirm({ title: `删除 ${t.manifest.name} ${t.manifest.version}？`, body: '主题文件会从当前节点移除，已选择的主题需先切换后才能删除。', danger: true, confirmLabel: '删除' })) void remove.run(t); }}>删除</Button>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="card-body stack" style={{ borderTop: '1px solid var(--line)' }}>
        <div className="form-grid">
          <Field label="ZIP 文件（最大 100 MiB）" hint="根目录只包含 theme.json 与 dist/">
            <input ref={fileInput} type="file" accept=".zip,application/zip" className="input" style={{ paddingTop: 6 }} onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
          </Field>
          <Field label="公开 GitHub 仓库" hint="读取 Release 中的 opsd-theme.zip；标签留空为最新正式版">
            <div className="row"><Input mono value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="https://github.com/owner/repo" /><Input mono value={tag} onChange={(e) => setTag(e.target.value)} placeholder="标签" style={{ width: 110 }} aria-label="标签" /></div>
          </Field>
        </div>
        {uncertain && (
          <Notice tone="warn" title="安装结果未知" actions={<Button size="sm" icon={<RefreshCw />} onClick={() => { install.reset(); void q.refetch(); }}>重新读取</Button>}>
            主题可能已经安装完成。请重新读取列表确认，不会自动重新上传。
          </Notice>
        )}
        <div className="row wrap">
          <Button variant="primary" icon={<Upload />} busy={install.busy} disabled={!file || uncertain} onClick={() => { if (file) void install.run({ file }); }}>上传并安装</Button>
          <Button busy={install.busy} disabled={!repo.trim() || uncertain} onClick={() => void install.run({ repository: repo.trim(), tag: tag.trim() })}>从仓库安装</Button>
        </div>
      </div>
    </Card>
  );
}

function AboutSection() {
  const q = useIdentity();
  if (q.isError) return <ErrorNotice error={q.error} retry={() => void q.refetch()} />;
  const v = q.data;
  return (
    <Card>
      <CardHead title="版本信息" />
      <div className="card-body">
        {!v ? <Skeleton height={140} /> : (
          <dl className="kv">
            <dt>节点程序</dt><dd className="mono">{v.node_version}</dd>
            <dt>API</dt><dd className="mono">{v.api_version}</dd>
            <dt>内置前端</dt><dd className="mono">{v.frontend_version}</dd>
            <dt>本界面</dt><dd className="mono">t1 · 1.0.0</dd>
            {v.commit && <><dt>提交</dt><dd><CopyText value={v.commit} display={v.commit.slice(0, 12)} /></dd></>}
            <dt>入口节点</dt><dd><CopyText value={v.node_id} /></dd>
            <dt>协议版本</dt><dd className="num">{v.protocol_version ?? '—'} · 持久化 {v.persistence_version ?? '—'} · 共享管理 {v.shared_management_version ?? '—'}</dd>
            <dt>接口文档</dt><dd><a className="strong" style={{ color: 'var(--accent)' }} href={v.docs_url} target="_blank" rel="noreferrer">{v.docs_url}</a></dd>
          </dl>
        )}
      </div>
    </Card>
  );
}
