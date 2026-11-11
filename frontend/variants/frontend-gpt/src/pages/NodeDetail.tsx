import { useEffect, useRef, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router';
import type { NodeItem, TaskItem } from '../data';
import { useDemo } from '../store';
import { Btn, Cell, Choice, Confirm, Copy, DataTable, EditorForm, Empty, Field, KeyValue, Modal, Notice, PageHeading, Panel, Refresh, Row, Sheet, Status, Switch, TabSet, type Confirmation } from '../components';
import { MetricsStrip, TrendChart } from '../monitoring';
import { Icon } from '../icons';

export function NodeDetail() {
  const { id } = useParams();
  const { nodes } = useDemo();
  const node = nodes.find(n => n.id === id);
  const [params, setParams] = useSearchParams();
  const tabs = [['monitoring', '监控'], ['terminal', '终端'], ['tasks', 'Shell 任务'], ['settings', '配置']] as const;
  const tab = tabs.some(t => t[0] === params.get('tab')) ? params.get('tab')! : 'monitoring';
  const [dirty, setDirty] = useState(false), [confirm, setConfirm] = useState<Confirmation | null>(null);
  useEffect(() => { const before = (event: BeforeUnloadEvent) => { if (dirty) event.preventDefault(); }; window.addEventListener('beforeunload', before); return () => window.removeEventListener('beforeunload', before); }, [dirty]);
  if (!node) return <><PageHeading title="节点不存在"/><Panel><Empty title="未找到这个节点" action={<Link to="/nodes" className="text-link">返回节点列表</Link>}/></Panel></>;
  const changeTab = (next: string) => {
    const apply = () => { setDirty(false); setParams({ tab: next }); };
    if (dirty) setConfirm({ title: '放弃未保存的修改？', confirmLabel: '放弃修改', action: apply }); else apply();
  };
  return <><PageHeading title={node.name} back={<Link className="back-link" to="/nodes"><Icon name="arrow" size={13}/>节点</Link>} metadata={<><Status tone={node.online ? 'success' : 'muted'}>{node.online ? '在线' : '离线'}</Status><span>{node.region} · {node.os}</span><span className="meta-separator"/><Copy value={node.address}/></>}><Refresh/><Link className="text-link" to={`/firewall?node=${node.id}`}>防火墙<Icon name="chevron" size={14}/></Link></PageHeading>
    {!node.online && <Notice>节点当前离线，保留最近的系统信息</Notice>}
    <div className="page-panel"><TabSet tabs={tabs} value={tab} onChange={changeTab}>
      {tab === 'monitoring' && <div className="node-detail-grid"><div><MetricsStrip nodes={[node]}/><TrendChart nodes={[node]}/></div><section className="node-system"><h2>系统信息</h2><KeyValue items={[["主机名", node.name], ["操作系统", node.os], ["架构", 'x86_64'], ["CPU", `${node.cores} 核`], ["内存", `${node.ram} GiB`], ["磁盘", `${node.capacity} GiB`], ["运行时间", node.uptime], ["公网 IPv4", <Copy value={node.address}/>], ["公网 IPv6", node.ipv6 ? <Copy value={node.ipv6}/> : '未发现'], ["通信地址", <Copy value={`http://${node.address}:8444`}/>]]}/></section></div>}
      {tab === 'terminal' && <Terminal key={node.id} node={node}/>}
      {tab === 'tasks' && <Tasks key={node.id} node={node}/>}
      {tab === 'settings' && <NodeConfiguration key={node.id} node={node} onDirty={setDirty}/>}
    </TabSet></div><Confirm value={confirm} close={() => setConfirm(null)}/>
  </>;
}

function Terminal({ node }: { node: NodeItem }) {
  const [lines, setLines] = useState(['OPSD 演示终端', `连接到 ${node.name} · ${node.os}`, '输入 help 查看可用命令。', '']);
  const [command, setCommand] = useState('');
  const output = useRef<HTMLPreElement>(null);
  useEffect(() => { if (output.current) output.current.scrollTop = output.current.scrollHeight; }, [lines]);
  const execute = () => {
    const value = command.trim();
    if (!value) return;
    setCommand('');
    if (value === 'clear') { setLines([]); return; }
    const answers: Record<string, string> = {
      help: '可用命令：help、whoami、hostname、pwd、ls、uptime、df -h、free -h、docker ps、clear',
      whoami: 'demo', hostname: node.name, pwd: '/home/demo', ls: 'backups/    compose/    logs/    opsd/',
      uptime: `14:32:18 up ${node.uptime}, load average: 0.32, 0.28, 0.21`,
      'df -h': `Filesystem      Size  Used Avail Use% Mounted on\n/dev/vda1       ${node.capacity}G   ${Math.round(node.capacity * node.disk / 100)}G   ${Math.round(node.capacity * (1 - node.disk / 100))}G  ${Math.round(node.disk)}% /`,
      'free -h': `              total    used    free\nMem:          ${node.ram}Gi     ${(node.ram * node.memory / 100).toFixed(1)}Gi    ${(node.ram * (1 - node.memory / 100)).toFixed(1)}Gi`,
      'docker ps': 'CONTAINER ID   IMAGE                STATUS        NAMES\na73e6b128f0c   nginx:1.28-alpine    Up 32 days    gateway',
    };
    setLines(old => [...old, `demo@${node.name}:~$ ${value}`, answers[value] ?? '演示终端未提供此命令。输入 help 查看可用命令。', '']);
  };
  return <div className="terminal-area">{!node.online ? <Empty title="节点离线，无法连接终端" icon="terminal"/> : <div className="terminal-window"><div className="terminal-bar"><Icon name="terminal" size={15}/><span>{node.name}</span><Status tone="primary">演示终端</Status></div><pre className="terminal-output mono" ref={output} aria-live="polite">{lines.join('\n')}</pre><form className="terminal-prompt mono" onSubmit={event => { event.preventDefault(); execute(); }}><span>$</span><input aria-label="终端命令" value={command} onChange={e => setCommand(e.target.value)} autoComplete="off" spellCheck={false} placeholder="输入命令"/><Btn type="submit" variant="ghost" icon="arrow" aria-label="执行演示命令"/></form></div>}</div>;
}

function Tasks({ node }: { node: NodeItem }) {
  const demo = useDemo();
  const tasks = demo.scene === 'empty' ? [] : demo.tasks.filter(t => t.nodeId === node.id);
  const [createOpen, setCreateOpen] = useState(false), [logTask, setLogTask] = useState<TaskItem | null>(null);
  const running = useRef(new Map<string, string>());
  function run(task: TaskItem) {
    const op = demo.addOperation(`运行 ${task.name}`, node.id, 'running', 'Shell 任务执行中。');
    running.current.set(task.id, op);
    demo.setTasks(old => old.map(t => t.id === task.id ? { ...t, state: 'running', lastRun: '14:33' } : t));
    demo.later(() => {
      if (!running.current.has(task.id)) return;
      running.current.delete(task.id);
      demo.setTasks(old => old.map(t => t.id === task.id && t.state === 'running' ? { ...t, state: 'success' } : t));
      demo.setOperations(old => old.map(o => o.id === op && o.state === 'running' ? { ...o, state: 'success', detail: '任务完成，退出码 0。' } : o));
      demo.setToast('任务已完成');
    }, 2200);
  }
  const viewed = demo.tasks.find(t => t.id === logTask?.id);
  const names = { success: '已完成', idle: '未运行', running: '执行中', cancelled: '已取消' };
  return <><div className="page-toolbar"><span className="muted">{tasks.length} 个任务</span><span className="toolbar-spacer"/><Btn icon="plus" variant="primary" disabled={!node.online} onClick={() => setCreateOpen(true)}>新建任务</Btn></div><DataTable headings={['任务', '命令', '最近运行', '状态', '操作']}>
    {tasks.map(task => <Row key={task.id}><Cell><strong>{task.name}</strong></Cell><Cell><code>{task.command}</code></Cell><Cell>{task.lastRun}</Cell><Cell><Status tone={task.state === 'success' ? 'success' : task.state === 'running' ? 'primary' : 'muted'}>{names[task.state]}</Status></Cell><Cell><div className="inline-actions"><Btn variant="ghost" icon="log" onClick={() => setLogTask(task)}>日志</Btn>{task.state === 'running' ? <Btn variant="ghost" onClick={() => { const op = running.current.get(task.id); running.current.delete(task.id); demo.setTasks(old => old.map(t => t.id === task.id ? { ...t, state: 'cancelled' } : t)); if (op) demo.resolveOperation(op, 'cancelled'); }}>取消</Btn> : <Btn variant="ghost" icon="play" disabled={!node.online} onClick={() => run(task)}>运行</Btn>}</div></Cell></Row>)}
    </DataTable>{!tasks.length && <Empty title="暂无 Shell 任务" action={<Btn icon="plus" disabled={!node.online} onClick={() => setCreateOpen(true)}>新建任务</Btn>}/>}
    <Modal open={createOpen} onClose={() => setCreateOpen(false)} title="新建 Shell 任务" footer={<><Btn onClick={() => setCreateOpen(false)}>取消</Btn><Btn variant="primary" type="submit" form="new-task">创建任务</Btn></>}><EditorForm id="new-task" onSubmit={form => { demo.setTasks(old => [...old, { id: demo.nextId('task'), nodeId: node.id, name: String(form.get('name')).trim(), command: String(form.get('command')), schedule: '手动', lastRun: '—', state: 'idle' }]); setCreateOpen(false); demo.setToast('任务已创建'); }}><Field label="任务名称" name="name" required maxLength={80}/><div className="field"><label htmlFor="task-command">Shell 命令</label><textarea id="task-command" name="command" className="mono" defaultValue="df -h" required/></div><KeyValue items={[["执行节点", node.name], ["触发方式", '手动']]}/></EditorForm></Modal>
    <Sheet open={!!logTask} onClose={() => setLogTask(null)} title={logTask?.name ?? '任务日志'}><pre className="log-output">{viewed?.state === 'idle' ? '任务尚未运行。' : `14:33:00 [demo] ${viewed?.command}\n${viewed?.state === 'running' ? '正在执行…' : viewed?.state === 'cancelled' ? '任务已取消。' : '演示执行完成。\n退出码：0'}`}</pre></Sheet>
  </>;
}

function NodeConfiguration({ node, onDirty }: { node: NodeItem; onDirty: (value: boolean) => void }) {
  const { setNodes, setToast } = useDemo();
  const [name, setName] = useState(node.name), [region, setRegion] = useState(node.region), [address, setAddress] = useState(node.address), [ipv6, setIpv6] = useState(node.ipv6), [auto, setAuto] = useState(node.autoAddress ?? true), [couch, setCouch] = useState(node.couch ?? node.id === 'sgp-01');
  const [dirty, setDirty] = useState(false);
  const [couchBinding, setCouchBinding] = useState(node.couchBinding ?? '127.0.0.1'), [couchPort, setCouchPort] = useState(node.couchPort ?? '5984');
  const mark = () => { setDirty(true); onDirty(true); };
  return <div className="content-section"><form className="editor-form settings-form" onChange={mark} onSubmit={event => { event.preventDefault(); setNodes(old => old.map(n => n.id === node.id ? { ...n, name: name.trim(), region, address, ipv6, couch, couchBinding, couchPort, autoAddress: auto } : n)); setDirty(false); onDirty(false); setToast('节点配置已保存'); }}><div className="form-row"><Field label="节点名称" value={name} onChange={e => setName(e.target.value)} required maxLength={128}/><Field label="地域" value={region} onChange={e => setRegion(e.target.value)} required/></div><div className="form-section"><h3>公网地址</h3><label className="settings-row"><div><strong>自动发现 IPv4</strong></div><Switch checked={auto} onCheckedChange={value => { setAuto(value); mark(); }} aria-label="自动发现 IPv4"/></label><Field label="IPv4" value={address} onChange={e => setAddress(e.target.value)} required disabled={auto}/><Field label="IPv6" value={ipv6} onChange={e => setIpv6(e.target.value)} placeholder="未配置"/></div><div className="form-section"><h3>OPSD 数据</h3><label className="settings-row"><div><strong>承载自用 CouchDB</strong></div><Switch checked={couch} onCheckedChange={value => { setCouch(value); mark(); }} aria-label="承载自用 CouchDB"/></label>{couch && <div className="form-row"><Field label="绑定 IP" value={couchBinding} onChange={event => setCouchBinding(event.target.value)} required/><Field label="端口" type="number" min={1} max={65535} value={couchPort} onChange={event => setCouchPort(event.target.value)} required/></div>}</div><div className="form-actions"><Btn type="submit" variant="primary" disabled={!dirty}>保存配置</Btn>{dirty && <span className="muted">未保存</span>}</div></form></div>;
}
