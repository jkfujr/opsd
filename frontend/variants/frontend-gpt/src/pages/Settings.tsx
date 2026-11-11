import { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { useTheme } from '@appica/ui-react/hooks/use-theme';
import { version } from '../../../../../package.json';
import { useDemo } from '../store';
import { Btn, Cell, Choice, Confirm, Copy, DataTable, EditorForm, Empty, Field, IconButton, KeyValue, Modal, Notice, PageHeading, Panel, Row, Status, Switch, TabSet, type Confirmation } from '../components';
import { Icon } from '../icons';
import { palettes, usePalette } from '../palettes';

const settingsTabs = [['account', '账号'], ['sessions', '会话'], ['api', 'API'], ['appearance', '外观'], ['version', '版本']] as const;
export function Settings() {
  const { pathname } = useLocation(), navigate = useNavigate();
  const value = pathname.split('/')[2] ?? 'account';
  const tab = settingsTabs.some(t => t[0] === value) ? value : 'account';
  return <><PageHeading title="设置"/><div className="page-panel"><TabSet value={tab} onChange={value => navigate(`/settings/${value}`)} tabs={settingsTabs}>{tab === 'account' && <Account/>}{tab === 'sessions' && <Sessions/>}{tab === 'api' && <APISettings/>}{tab === 'appearance' && <Appearance/>}{tab === 'version' && <Version/>}</TabSet></div></>;
}
function Account() {
  const { profile, setProfile } = useDemo();
  const navigate = useNavigate();
  const [error, setError] = useState('');
  return <div className="content-section"><EditorForm onSubmit={form => {
    const password = String(form.get('password')), repeat = String(form.get('repeat'));
    if (password !== repeat) { setError('两次输入的新密码不一致'); return; }
    const entrance = String(form.get('entrance'));
    if (!/^\/[a-zA-Z0-9/_-]+$/.test(entrance)) { setError('入口需以 / 开头，仅含字母、数字、下划线和短横线'); return; }
    setProfile({ name: String(form.get('username')).trim(), entrance });
    navigate('/login', { state: { message: '账号已保存，请重新进入工作空间' } });
  }}><div className="settings-form editor-form"><h2>管理员账号</h2><div className="form-row"><Field label="账号" name="username" defaultValue={profile.name} required minLength={2} maxLength={128} autoComplete="username"/><Field label="安全入口" name="entrance" defaultValue={profile.entrance} required error={error.startsWith('入口') ? error : undefined} onChange={() => setError('')}/></div><div className="form-section"><h3>修改密码</h3><Field label="当前密码" type="password" name="current" required autoComplete="current-password"/><div className="form-row"><Field label="新密码" type="password" name="password" minLength={8} autoComplete="new-password"/><Field label="确认新密码" type="password" name="repeat" minLength={8} autoComplete="new-password" error={error.startsWith('两次') ? error : undefined} onChange={() => setError('')}/></div></div><div className="form-actions"><Btn variant="primary" type="submit">保存并重新登录</Btn></div></div></EditorForm></div>;
}
function Sessions() {
  const { sessions, setSessions, setToast } = useDemo();
  const navigate = useNavigate();
  const [confirm, setConfirm] = useState<Confirmation | null>(null);
  return <><div className="page-toolbar"><span className="muted">{sessions.length} 个会话</span></div><DataTable headings={['客户端', '来源地址', '登录时间', '到期时间', '操作']}>{sessions.map(session => <Row key={session.id}><Cell><strong>{session.client}</strong>{session.current && <span className="subtle-text">当前会话</span>}</Cell><Cell><Copy value={session.address}/></Cell><Cell>{session.created}</Cell><Cell>{session.expires}</Cell><Cell><Btn variant="ghost" onClick={() => setConfirm({ title: session.current ? '退出当前会话？' : '退出这个会话？', confirmLabel: '退出会话', action: () => { setSessions(old => old.filter(s => s.id !== session.id)); setToast('会话已退出'); if (session.current) navigate('/login'); } })}>退出会话</Btn></Cell></Row>)}</DataTable>{!sessions.length && <Empty title="暂无会话" icon="monitor"/>}<Confirm value={confirm} close={() => setConfirm(null)}/></>;
}

type TokenItem = ReturnType<typeof useDemo>['tokens'][number];
function APISettings() {
  const demo = useDemo();
  const [open, setOpen] = useState(false), [editing, setEditing] = useState<TokenItem | null>(null), [permission, setPermission] = useState('监控只读'), [scope, setScope] = useState('all'), [expires, setExpires] = useState('never');
  const [reveal, setReveal] = useState(''), [confirm, setConfirm] = useState<Confirmation | null>(null);
  function regenerate(token: TokenItem) {
    demo.setTokens(old => old.map(t => t.id === token.id ? { ...t, revision: t.revision + 1 } : t));
    setReveal(`op_demo_${token.id}_revision${token.revision + 1}_example_only`);
  }
  return <><div className="content-section"><label className="settings-row" style={{ paddingTop: 0 }}><div><h3>API</h3><p>Bearer 令牌 · 203.0.113.21:65533</p></div><Switch checked={demo.apiEnabled} onCheckedChange={value => { demo.setApiEnabled(value); demo.setToast(`API 已${value ? '开启' : '关闭'}`); }} aria-label="启用 API"/></label></div><div className="page-toolbar"><h2>访问令牌</h2><span className="toolbar-spacer"/><Btn icon="plus" variant="primary" disabled={!demo.apiEnabled} onClick={() => { setEditing(null); setPermission('监控只读'); setScope('all'); setExpires('never'); setOpen(true); }}>创建令牌</Btn></div>
    <DataTable headings={['名称', '权限', '创建时间', '状态', '操作']}>{demo.tokens.map(token => <Row key={token.id}><Cell><strong>{token.name}</strong><span className="subtle-text">op_demo_••••••••</span></Cell><Cell>{token.permission}<span className="subtle-text">{token.scope === 'all' ? '全部节点' : demo.nodes.find(n => n.id === token.scope)?.name} · {token.expires === 'never' ? '长期有效' : token.expires.replace('d', ' 天')}</span></Cell><Cell>{token.created}</Cell><Cell><Status tone={token.enabled ? 'success' : 'muted'}>{token.enabled ? '已启用' : '已停用'}</Status></Cell><Cell><div className="inline-actions"><Btn variant="ghost" onClick={() => { setEditing(token); setPermission(token.permission); setScope(token.scope); setExpires(token.expires); setOpen(true); }}>编辑</Btn><IconButton icon={token.enabled ? 'stop' : 'play'} label={`${token.enabled ? '停用' : '启用'} ${token.name}`} onClick={() => { demo.setTokens(old => old.map(t => t.id === token.id ? { ...t, enabled: !t.enabled } : t)); demo.setToast('演示令牌状态已更新'); }}/><IconButton icon="refresh" label={`重新生成 ${token.name}`} onClick={() => setConfirm({ title: '重新生成令牌？', description: '旧令牌将失效，新值仅展示一次。', confirmLabel: '重新生成令牌', action: () => regenerate(token) })}/><IconButton icon="trash" label={`删除 ${token.name}`} onClick={() => setConfirm({ title: `删除令牌 ${token.name}？`, confirmLabel: '删除令牌', danger: true, action: () => { demo.setTokens(old => old.filter(t => t.id !== token.id)); demo.setToast('令牌已删除'); } })}/></div></Cell></Row>)}</DataTable>{!demo.tokens.length && <Empty title="暂无访问令牌" icon="key"/>}
    <Modal open={open} onClose={() => setOpen(false)} title={editing ? '编辑令牌' : '创建访问令牌'} footer={<><Btn onClick={() => setOpen(false)}>取消</Btn><Btn variant="primary" type="submit" form="token-editor">{editing ? '保存' : '创建令牌'}</Btn></>}><EditorForm key={editing?.id ?? 'new'} id="token-editor" onSubmit={form => { const name = String(form.get('name')); if (editing) { demo.setTokens(old => old.map(t => t.id === editing.id ? { ...t, name, permission, scope, expires } : t)); demo.setToast('令牌已更新'); } else { const id = demo.nextId('token'); demo.setTokens(old => [...old, { id, name, permission, scope, expires, enabled: true, created: '2026-11-10', revision: 1 }]); setReveal(`op_demo_${id}_revision1_example_only`); } setOpen(false); }}><Field label="名称" name="name" defaultValue={editing?.name} required maxLength={80}/><div className="field"><label>权限模板</label><Choice label="权限模板" value={permission} onChange={setPermission} options={[["监控只读", "监控只读"], ["全部只读", "全部只读"], ["容器管理", "容器管理"], ["数据库管理", "数据库管理"]]}/></div><div className="field"><label>节点范围</label><Choice label="令牌节点范围" value={scope} onChange={setScope} options={[["all", "全部节点"], ...demo.nodes.map(n => [n.id, n.name] as const)]}/></div><div className="field"><label>有效期</label><Choice label="有效期" value={expires} onChange={setExpires} options={[["never", "长期有效"], ["7d", "7 天"], ["30d", "30 天"], ["90d", "90 天"]]}/></div></EditorForm></Modal>
    <Modal open={!!reveal} onClose={() => setReveal('')} title="演示令牌已生成" footer={<Btn variant="primary" onClick={() => setReveal('')}>完成</Btn>}><p className="muted">完整令牌仅展示一次</p><div className="token-reveal mono" style={{ marginTop: '1rem' }}>{reveal}<Copy value={reveal}/></div><KeyValue items={[["API 地址", <Copy value="https://opsd.example/api/v1"/>], ["指标地址", <Copy value="https://opsd.example/metrics"/>]]}/><pre className="log-output token-example">{`curl -H 'Authorization: Bearer ${reveal}' \\\n  https://opsd.example/api/v1/monitoring/current`}</pre></Modal><Confirm value={confirm} close={() => setConfirm(null)}/>
  </>;
}

const menuLabels: Record<string, string> = { overview: '概览', nodes: '节点', containers: '容器', databases: '数据库', storage: '存储', firewall: '防火墙', settings: '设置' };
function Appearance() {
  const { theme, setTheme } = useTheme();
  const { palette, setPalette } = usePalette();
  const demo = useDemo();
  const [order, setOrder] = useState(demo.navigationOrder);
  const dirty = order.join() !== demo.navigationOrder.join();
  const move = (index: number, delta: number) => { setOrder(old => { const next = [...old]; [next[index], next[index + delta]] = [next[index + delta], next[index]]; return next; }); };
  return <div className="content-section"><div className="settings-form"><h2>主题</h2><section className="appearance-section"><h3>颜色模式</h3><div className="theme-options" aria-label="颜色模式">{[['dark', '深色', 'moon'], ['light', '浅色', 'sun'], ['system', '跟随系统', 'monitor']].map(([id, label, icon]) => <button className="theme-option" key={id} aria-pressed={theme === id} onClick={() => setTheme(id)}><span className={`theme-preview ${id === 'system' ? '' : id}`}><i/><span><i/><i/></span></span><Icon name={icon as 'moon' | 'sun' | 'monitor'} size={14}/> {label}</button>)}</div></section><section className="appearance-section"><h3>配色</h3><div className="palette-options" aria-label="配色">{palettes.map(item => <button key={item.id} className="palette-option" aria-pressed={palette === item.id} onClick={() => setPalette(item.id)}><span className="palette-swatch" data-palette={item.id} aria-hidden="true"><i/><span><b/><b/><b/></span></span><span className="palette-option-label">{item.name}{palette === item.id && <Icon name="check" size={15}/>}</span></button>)}</div></section><div className="form-section"><h2>菜单顺序</h2><div className="menu-order">{order.map((id, index) => <div className="menu-order-row" key={id}><span>{menuLabels[id]}</span><IconButton icon="arrowUp" label={`上移${menuLabels[id]}`} disabled={!index} onClick={() => move(index, -1)}/><IconButton icon="arrowDown" label={`下移${menuLabels[id]}`} disabled={index === order.length - 1} onClick={() => move(index, 1)}/></div>)}</div><div className="form-actions"><Btn variant="primary" disabled={!dirty} onClick={() => { demo.setNavigationOrder(order); demo.setToast('演示菜单顺序已保存'); }}>保存顺序</Btn><Btn onClick={() => setOrder(Object.keys(menuLabels))}>恢复默认</Btn>{dirty && <span className="muted">未保存</span>}</div></div></div></div>;
}
function Version() {
  return <div className="content-section"><div className="settings-form"><h2>版本</h2><KeyValue items={[["产品", 'OPSD'], ["程序版本", version], ["界面", 'Monitor · 视觉原型'], ["后端连接", <Status tone="muted">未接入</Status>], ["运行方式", '本地独立前端'], ["数据", '固定演示数据']]}/></div></div>;
}
export function Storage() {
  return <><PageHeading title="存储" metadata={<Status tone="muted">未接入</Status>}/><Panel><div className="storage-empty"><Empty title="Silo 存储尚未接入" icon="storage" action={<Link className="text-link" to="/nodes/sgp-01?tab=settings">查看节点中的 OPSD 数据<Icon name="arrow" size={14}/></Link>}/></div></Panel></>;
}
