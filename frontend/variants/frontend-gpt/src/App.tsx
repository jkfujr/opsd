import { lazy, Suspense, useEffect, useState } from 'react';
import { BrowserRouter, Link, Navigate, NavLink, Outlet, Route, Routes, useLocation, useNavigate } from 'react-router';
import { useTheme } from '@appica/ui-react/hooks/use-theme';
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle, DrawerBody } from '@appica/ui-react/drawer';
import { BrandMark, Icon, type IconName } from './icons';
import { useDemo, type Scene } from './store';
import { Btn, Choice, Copy, EditorForm, Empty, Field, IconButton, KeyValue, Modal, Notice, RouteLoading, SearchBox, Sheet, Status, type Tone } from './components';
import { initialNodes } from './data';
const Overview = lazy(() => import('./pages/Overview').then(module => ({ default: module.Overview })));
const Nodes = lazy(() => import('./pages/Overview').then(module => ({ default: module.Nodes })));
const NodeDetail = lazy(() => import('./pages/NodeDetail').then(module => ({ default: module.NodeDetail })));
const Containers = lazy(() => import('./pages/Containers').then(module => ({ default: module.Containers })));
const DatabaseDetail = lazy(() => import('./pages/Databases').then(module => ({ default: module.DatabaseDetail })));
const Databases = lazy(() => import('./pages/Databases').then(module => ({ default: module.Databases })));
const Firewall = lazy(() => import('./pages/Firewall').then(module => ({ default: module.Firewall })));
const Settings = lazy(() => import('./pages/Settings').then(module => ({ default: module.Settings })));
const Storage = lazy(() => import('./pages/Settings').then(module => ({ default: module.Storage })));

const navigation: { path: string; label: string; icon: IconName }[] = [
  { path: '/', label: '概览', icon: 'overview' }, { path: '/nodes', label: '节点', icon: 'nodes' },
  { path: '/containers', label: '容器', icon: 'containers' }, { path: '/databases', label: '数据库', icon: 'databases' },
  { path: '/storage', label: '存储', icon: 'storage' }, { path: '/firewall', label: '防火墙', icon: 'firewall' }, { path: '/settings', label: '设置', icon: 'settings' },
];
const opNames = { success: '已完成', running: '执行中', confirm: '待确认', unknown: '应答未知', cancelled: '已取消' };
const opTones: Record<string, Tone> = { success: 'success', running: 'primary', confirm: 'warning', unknown: 'warning', cancelled: 'muted' };

function Navigation({ close }: { close?: () => void }) {
  const { nodes, containers, navigationOrder } = useDemo();
  const items = navigationOrder.flatMap(id => navigation.filter(n => n.icon === id));
  return <nav className="nav-list" aria-label="主导航">{items.map(({ path, label, icon }) => <NavLink key={path} end={path === '/'} to={path} aria-label={label} onClick={close} className="nav-link"><Icon name={icon}/><span>{label}</span>{path === '/nodes' && <span className="nav-count">{nodes.length}</span>}{path === '/containers' && <span className="nav-count">{containers.length}</span>}</NavLink>)}</nav>;
}
function AccountFooter() {
  const { profile } = useDemo();
  return <div className="sidebar-bottom"><div className="local-node"><span className="connection-dot"/>本机 · edge-tokyo-01</div><div className="sidebar-account"><span className="avatar">AD</span><Link to="/settings/account" className="account-text"><span>{profile.name}</span><small>管理员</small></Link><Link to="/login" className="row-open" aria-label="退出演示会话" title="退出演示会话"><Icon name="logout" size={16}/></Link></div></div>;
}
function Shell() {
  const { pathname } = useLocation();
  const { theme, resolvedTheme, setTheme } = useTheme();
  const [menuOpen, setMenuOpen] = useState(false), [commandOpen, setCommandOpen] = useState(false), [sceneOpen, setSceneOpen] = useState(false), [search, setSearch] = useState('');
  const demo = useDemo();
  useEffect(() => { setMenuOpen(false); setCommandOpen(false); window.scrollTo(0, 0); }, [pathname]);
  useEffect(() => { const key = (event: KeyboardEvent) => { if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); setCommandOpen(v => !v); } }; window.addEventListener('keydown', key); return () => window.removeEventListener('keydown', key); }, []);
  const pending = demo.operations.filter(o => ['confirm', 'unknown', 'running'].includes(o.state)).length;
  return <div className="shell">
    <a href="#main-content" className="skip-link">跳至主要内容</a>
    <aside className="sidebar"><div className="brand"><Link to="/" aria-label="OPSD 概览"><BrandMark/></Link><Link to="/" className="brand-word">OPSD</Link><button className="demo-badge" onClick={() => setSceneOpen(true)}>演示</button></div><Navigation/><AccountFooter/></aside>
    <Drawer side="left" open={menuOpen} onOpenChange={setMenuOpen}><DrawerContent frame={false} className="mobile-nav-content" closeLabel="关闭菜单" aria-describedby={undefined}><DrawerHeader className="g-modal-header"><DrawerTitle><span className="brand-word">OPSD</span><button className="demo-badge" onClick={() => { setMenuOpen(false); setSceneOpen(true); }}>演示</button></DrawerTitle></DrawerHeader><DrawerBody><Navigation close={() => setMenuOpen(false)}/><AccountFooter/></DrawerBody></DrawerContent></Drawer>
    <div className="shell-body"><header className="topbar"><div className="topbar-left"><IconButton icon="menu" label="打开菜单" className="mobile-menu-trigger" onClick={() => setMenuOpen(true)}/><button className="quick-search" onClick={() => setCommandOpen(true)}><Icon name="search" size={15}/>查找页面或节点<kbd>{/Mac|iPhone|iPad/.test(navigator.platform) ? '⌘ K' : 'Ctrl K'}</kbd></button><IconButton icon="search" label="快速查找" className="mobile-search-trigger" onClick={() => setCommandOpen(true)}/></div><div className="topbar-actions"><IconButton label={resolvedTheme === 'dark' ? '切换浅色主题' : '切换深色主题'} icon={resolvedTheme === 'dark' ? 'sun' : 'moon'} onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}/><span className="topbar-divider"/><button className="operation-trigger" onClick={() => demo.showOperation()} aria-label={'操作记录，' + pending + ' 个待处理'}><Icon name="activity" size={17}/><span className="operation-label">操作记录</span>{pending > 0 && <span className="operation-count">{pending}</span>}</button><button className="demo-badge mobile-demo" onClick={() => setSceneOpen(true)}>演示</button></div></header>
      <main id="main-content" tabIndex={-1} className="workspace"><Suspense fallback={<RouteLoading/>}><Outlet/></Suspense></main>
    </div>
    <Modal open={commandOpen} onClose={() => setCommandOpen(false)} title="快速查找"><SearchBox value={search} onChange={setSearch} placeholder="搜索页面或节点…"/><div className="command-results">{navigation.filter(n => n.label.includes(search)).map(n => <Link key={n.path} to={n.path} onClick={() => setCommandOpen(false)}><Icon name={n.icon}/>{n.label}<Icon name="arrow" size={15}/></Link>)}{demo.nodes.filter(n => search && `${n.name} ${n.region}`.includes(search)).map(n => <Link key={n.id} to={`/nodes/${n.id}`} onClick={() => setCommandOpen(false)}><Icon name="nodes"/>{n.name}<Icon name="arrow" size={15}/></Link>)}</div></Modal>
    <Modal open={sceneOpen} onClose={() => setSceneOpen(false)} title="演示状态"><div className="scene-options">{([
      ['normal', '标准状态', '在线与离线节点、磁盘告警和待确认操作'], ['empty', '空数据', '无节点、容器和集群时的页面'], ['loading', '加载中', '保留页面结构，显示数据骨架'], ['partial', '部分失败', '保留最近读数，显示重试入口'], ['unknown', '操作应答未知', '查询原操作并核对结果'],
    ] as [Scene, string, string][]).map(([value, label, detail]) => <button key={value} aria-pressed={demo.scene === value} onClick={() => { demo.setScene(value); setSceneOpen(false); }}><div>{label}<small>{detail}</small></div>{demo.scene === value && <Icon name="check" size={17}/>}</button>)}</div><p className="demo-inline">业务演示数据刷新后恢复；外观偏好保留。</p></Modal>
    <OperationPanel/><Enrollment/>
    {demo.toast && <div className="toast" role="status"><Icon name="check" size={16}/>{demo.toast}</div>}
    <span className="sr-only">当前主题：{theme === 'system' ? '跟随系统' : resolvedTheme === 'dark' ? '深色' : '浅色'}</span>
  </div>;
}

function OperationPanel() {
  const demo = useDemo();
  const [checking, setChecking] = useState(false);
  const op = demo.operations.find(item => item.id === demo.operationId);
  const node = demo.nodes.find(item => item.id === op?.nodeId);
  const steps = op ? [
    { title: '请求已受理', detail: '', tone: 'done' },
    ...(op.state === 'confirm' ? [{ title: '配置已写入', detail: op.detail, tone: 'done' }, { title: '等待确认', detail: '确认期限 120 秒', tone: 'warning' }] :
      op.state === 'unknown' ? [{ title: '结果待核对', detail: op.detail, tone: 'warning' }] :
      [{ title: opNames[op.state], detail: op.state === 'running' ? '正在处理目标操作' : op.detail, tone: op.state === 'success' ? 'done' : op.state === 'running' ? 'warning' : 'muted' }]),
  ] : [];
  return <Sheet open={demo.operationsOpen} onClose={() => demo.setOperationsOpen(false)} title={op?.title ?? '操作记录'} footer={op ? <><Btn icon="arrow" onClick={() => demo.setOperationId(null)}>全部操作</Btn>{op.state === 'confirm' && <Btn variant="primary" onClick={() => demo.resolveOperation(op.id)}>确认配置</Btn>}{op.state === 'unknown' && <Btn icon="refresh" variant="primary" disabled={checking} onClick={() => { setChecking(true); demo.later(() => { demo.resolveOperation(op.id); setChecking(false); }); }}>{checking ? '查询中' : '查询原操作'}</Btn>}{['running', 'confirm'].includes(op.state) && <Btn onClick={() => demo.resolveOperation(op.id, 'cancelled')}>取消操作</Btn>}</> : undefined}>
    {op ? <><div className="operation-summary"><Status tone={opTones[op.state]}>{opNames[op.state]}</Status></div><KeyValue items={[["目标节点", node?.name ?? '未知节点'], ["操作编号", <Copy value={op.id}/>], ["受理时间", '2026-11-10 ' + op.time + ':00']]}/><ol className="timeline">{steps.map((step, index) => <li key={index}><span className={'timeline-icon step-' + step.tone}><Icon name={step.tone === 'done' ? 'check' : step.tone === 'muted' ? 'close' : 'clock'} size={16}/></span><div><strong>{step.title}</strong>{step.detail && <p>{step.detail}</p>}</div></li>)}</ol></> :
      demo.operations.length ? <div className="operation-list">{demo.operations.map(item => <button key={item.id} onClick={() => demo.setOperationId(item.id)}><Icon name={item.state === 'success' ? 'check' : 'clock'} size={18}/><div><strong>{item.title}</strong><small>{demo.nodes.find(n => n.id === item.nodeId)?.name} · {item.id} · {item.time}</small></div><Status tone={opTones[item.state]}>{opNames[item.state]}</Status></button>)}</div> : <Empty title="暂无操作记录" icon="activity"/>}
  </Sheet>;
}

function Enrollment() {
  const { enrollmentOpen, setEnrollmentOpen, nodes, setNodes, setToast } = useDemo();
  const [name, setName] = useState(''), [region, setRegion] = useState('东京'), [command, setCommand] = useState('');
  const close = () => { setEnrollmentOpen(false); setCommand(''); setName(''); };
  const add = () => { const id = `demo-${nodes.length + 1}`; setNodes(old => [...old, { ...initialNodes[0], id, code: 'NEW', name: name.trim() || `new-node-${nodes.length + 1}`, region, country: region, address: `192.0.2.${100 + nodes.length}`, ipv6: '', cpu: 4.2, memory: 12.5, disk: 8.1, uptime: '1 分钟' }]); setToast('节点已加入演示列表'); close(); };
  return <Modal open={enrollmentOpen} onClose={close} title="添加节点" footer={command ? <><Btn onClick={() => setCommand('')}>上一步</Btn><Btn variant="primary" onClick={add}>模拟接入</Btn></> : <><Btn onClick={close}>取消</Btn><Btn variant="primary" type="submit" form="enroll-form">生成演示命令</Btn></>}>
    {command ? <><Status tone="primary">演示命令</Status><div className="token-reveal mono" style={{ marginTop: '1rem' }}>{command}<Copy value={command}/></div></> : <EditorForm id="enroll-form" onSubmit={() => setCommand(`opsd install --name '${name.trim() || 'new-node'}' --region '${region}' --enrollment-token demo_join_001`)}><Field label="节点名称" value={name} onChange={e => setName(e.target.value)} placeholder="例如 edge-tokyo-02" maxLength={128}/><div className="field"><label>地域</label><Choice label="接入地域" value={region} onChange={setRegion} options={[["东京", "东京"], ["新加坡", "新加坡"], ["法兰克福", "法兰克福"], ["洛杉矶", "洛杉矶"], ["香港", "香港"]]}/></div><KeyValue items={[["接入地址", <Copy value="http://203.0.113.21:8444"/>], ["有效期", '24 小时']]}/></EditorForm>}
  </Modal>;
}

function Login() {
  const navigate = useNavigate();
  const { state: loginState } = useLocation();
  const { profile } = useDemo();
  const { resolvedTheme, setTheme } = useTheme();
  const [error, setError] = useState('');
  return <div className="login-page"><div className="login-theme"><IconButton label="切换主题" icon={resolvedTheme === 'dark' ? 'sun' : 'moon'} onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}/></div><div className="login-main"><div className="login-brand"><BrandMark/><span className="brand-word">OPSD</span><span className="demo-badge">演示</span></div><form className="login-form" onSubmit={event => { event.preventDefault(); const form = new FormData(event.currentTarget); if (String(form.get('username')).trim().length < 2) { setError('账号至少需要 2 个字符'); return; } navigate('/'); }}><h2>登录</h2>{loginState?.message && <Notice tone="success">{loginState.message}</Notice>}<Field label="账号" name="username" autoComplete="username" defaultValue={profile.name} required error={error || undefined} onChange={() => setError('')}/><Field label="密码" name="password" type="password" autoComplete="current-password" defaultValue="opsd-demo" required minLength={6}/><Btn variant="primary" icon="arrow" type="submit">进入演示</Btn></form></div></div>;
}

export function App() {
  return <BrowserRouter><Routes><Route path="/login" element={<Login/>}/><Route element={<Shell/>}><Route index element={<Overview/>}/><Route path="nodes" element={<Nodes/>}/><Route path="nodes/:id" element={<NodeDetail/>}/><Route path="containers" element={<Containers/>}/><Route path="databases" element={<Databases/>}/><Route path="databases/:id" element={<DatabaseDetail/>}/><Route path="storage" element={<Storage/>}/><Route path="firewall" element={<Firewall/>}/><Route path="settings/*" element={<Settings/>}/><Route path="*" element={<Navigate to="/" replace/>}/></Route></Routes></BrowserRouter>;
}
