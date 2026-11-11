import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { initialNodes, initialContainers, initialOperations, initialRules, initialTasks, initialDatabases, initialAccounts, type NodeItem, type ContainerItem, type OperationItem, type RuleItem, type TaskItem, type ClusterItem, type DatabaseItem, type AccountItem } from './data';

export type Scene = 'normal' | 'empty' | 'loading' | 'partial' | 'unknown';
function useDemoState() {
  const [nodes, setNodes] = useState<NodeItem[]>(() => initialNodes.map(n => ({ ...n })));
  const [containers, setContainers] = useState<ContainerItem[]>(() => initialContainers.map(c => ({ ...c })));
  const [rules, setRules] = useState<RuleItem[]>(() => initialNodes.flatMap(n => initialRules.map(r => ({ ...r, id: `${r.id}-${n.id}`, nodeId: n.id }))));
  const [tasks, setTasks] = useState<TaskItem[]>(() => initialTasks.map(t => ({ ...t })));
  const [operations, setOperations] = useState<OperationItem[]>(() => initialOperations.map(o => ({ ...o })));
  const [clusters, setClusters] = useState<ClusterItem[]>([{ id: 'primary', name: 'production-galera', coordinator: 'tyo-01' }]);
  const [databases, setDatabases] = useState<DatabaseItem[]>(() => initialDatabases.map(d => ({ ...d })));
  const [accounts, setAccounts] = useState<AccountItem[]>(() => initialAccounts.map(a => ({ ...a })));
  const [apiEnabled, setApiEnabled] = useState(true);
  const [tokens, setTokens] = useState([{ id: 'token-1', name: 'Grafana 监控', permission: '监控只读', scope: 'all', expires: 'never', enabled: true, created: '2026-11-08', revision: 1 }, { id: 'token-2', name: '运维工具', permission: '容器管理', scope: 'tyo-01', expires: '30d', enabled: true, created: '2026-11-09', revision: 1 }]);
  const [profile, setProfile] = useState({ name: 'demo-admin', entrance: '/login' });
  const [sessions, setSessions] = useState([{ id: 's1', address: '203.0.113.80', client: 'Chrome · Windows', current: true, created: '2026-11-10 09:12', expires: '2026-11-17 09:12' }, { id: 's2', address: '2001:db8:1::80', client: 'Safari · iPhone', current: false, created: '2026-11-09 20:34', expires: '2026-11-16 20:34' }]);
  const [navigationOrder, setNavigationOrder] = useState(['overview', 'nodes', 'containers', 'databases', 'storage', 'firewall', 'settings']);
  const [trustEnabled, setTrustEnabled] = useState(true);
  const [scene, setScene] = useState<Scene>('normal');
  const [toast, setToast] = useState('');
  const [operationId, setOperationId] = useState<string | null>(null);
  const [operationsOpen, setOperationsOpen] = useState(false);
  const [enrollmentOpen, setEnrollmentOpen] = useState(false);
  const sequence = useRef(1043);
  const nextId = (prefix: string) => `${prefix}-${sequence.current++}`;
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  useEffect(() => () => timers.current.forEach(clearTimeout), []);
  useEffect(() => { if (!toast) return; const timer = setTimeout(() => setToast(''), 3600); return () => clearTimeout(timer); }, [toast]);
  function later(action: () => void, delay = 800) { timers.current.push(setTimeout(action, delay)); }
  function addOperation(title: string, nodeId: string, state: OperationItem['state'] = 'success', detail = '操作已完成。', containerUpdate?: OperationItem['containerUpdate']) {
    const id = `OP-${sequence.current++}`;
    setOperations(old => [{ id, title, nodeId, state, detail, time: '14:33', containerUpdate }, ...old]);
    return id;
  }
  function showOperation(id?: string) { setOperationId(id ?? null); setOperationsOpen(true); }
  function resolveOperation(id: string, state: OperationItem['state'] = 'success') {
    const update = operations.find(o => o.id === id)?.containerUpdate;
    if (state === 'success' && update) setContainers(old => old.map(c => c.id === update.id ? { ...c, image: update.image } : c));
    setOperations(old => old.map(o => o.id === id ? { ...o, state, detail: state === 'cancelled' ? '操作已取消。' : '已核对目标状态。' } : o));
    setToast(state === 'cancelled' ? '操作已取消' : '结果已确认');
  }
  function setDemoScene(value: Scene) {
    setScene(value);
    if (value === 'unknown') setOperations(old => old.some(o => o.state === 'unknown') ? old : [{ id: `OP-${sequence.current++}`, title: '更新 worker 镜像', nodeId: 'fra-01', state: 'unknown', time: '14:32', detail: '目标已受理，返回结果中断。请查询原操作。' }, ...old]);
  }
  return { nodes, setNodes, containers, setContainers, rules, setRules, tasks, setTasks, operations, setOperations, clusters, setClusters, databases, setDatabases, accounts, setAccounts, apiEnabled, setApiEnabled, tokens, setTokens, profile, setProfile, sessions, setSessions, navigationOrder, setNavigationOrder, trustEnabled, setTrustEnabled, scene, setScene: setDemoScene, toast, setToast, operationsOpen, setOperationsOpen, operationId, setOperationId, enrollmentOpen, setEnrollmentOpen, nextId, addOperation, showOperation, resolveOperation, later };
}
type DemoState = ReturnType<typeof useDemoState>;
const DemoContext = createContext<DemoState | null>(null);
export function DemoProvider({ children }: { children: ReactNode }) { return <DemoContext value={useDemoState()}>{children}</DemoContext>; }
export function useDemo() { const state = useContext(DemoContext); if (!state) throw new Error('演示状态未初始化'); return state; }
