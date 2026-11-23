import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { enc, get, getVersioned, readAll } from '../api/http';
import type {
  ContainerList, ComposeProjectList, FirewallConfiguration, ManagedDatabaseList, ManagedDatabaseStatus, ManagedFirewallState, MonitoringArchiveStatusList,
  NodeAccount, NodeConfig, NodeList, NodeStatus, Overview, PeerTrustPolicy, PeerTrustStatus, PublicAddresses, ScheduledTaskList, SessionList, Account,
  APIIdentity, APITokenList, EnrollmentTokenList, StorageTargetList, OperationList, NodeInfo, SharedStorageOverview, ThemeList,
} from '../api/types';
import { createStore, useStore } from '../lib/store';
import { useVisible } from '../lib/hooks';

// ── 实时历史：每次概览读取都记入环形缓冲，用于火花线与瞬时趋势 ─────────
const SIZE = 48;
export interface LivePoint { t: number; cpu?: number; mem?: number; disk?: number; rx?: number; tx?: number }
export const live = createStore<Record<string, LivePoint[]>>({});

function record(overview: Overview) {
  const t = Date.parse(overview.updated_at) || Date.now();
  live.set((prev) => {
    const next = { ...prev };
    const push = (key: string, p: LivePoint) => {
      const list = prev[key] ?? [];
      if (list.length && list[list.length - 1]!.t >= p.t) return;
      next[key] = [...list.slice(-(SIZE - 1)), p];
    };
    const m = overview.summary.metrics;
    push('__cluster', point(t, m));
    for (const n of overview.nodes) if (n.reading.metrics) push(n.node.id, point(Date.parse(n.reading.metrics.collected_at) || t, n.reading.metrics));
    return next;
  });
}
function point(t: number, m: Overview['summary']['metrics']): LivePoint {
  return {
    t,
    cpu: m.cpu_usage,
    mem: m.memory_used !== undefined && m.memory_total ? (m.memory_used / m.memory_total) * 100 : undefined,
    disk: m.disk_used !== undefined && m.disk_total ? (m.disk_used / m.disk_total) * 100 : undefined,
    rx: m.network_rx,
    tx: m.network_tx,
  };
}
export const useLive = (key: string) => useStore(live, (s) => s[key]);

function usePoll(ms: number) {
  const visible = useVisible();
  return visible ? ms : false;
}

export function useOverview() {
  const interval = usePoll(3000);
  return useQuery({
    queryKey: ['overview'],
    queryFn: async ({ signal }) => { const v = await get<Overview>('/overview', { signal }); record(v); return v; },
    refetchInterval: interval,
    staleTime: 2000,
    placeholderData: keepPreviousData,
  });
}

export function useNodes() {
  return useQuery({
    queryKey: ['nodes'],
    queryFn: ({ signal }) => readAll<NodeList>((cursor) => get('/nodes', { signal, query: { limit: 100, cursor } })),
    staleTime: 10_000,
    refetchInterval: usePoll(15_000),
  });
}
export const useNodeStatus = (id: string) => useQuery({ queryKey: ['nodes', id, 'status'], queryFn: ({ signal }) => get<NodeStatus>(`/nodes/${enc(id)}/status`, { signal }), enabled: !!id });
export const useNode = (id: string) => useQuery({ queryKey: ['nodes', id, 'info'], queryFn: ({ signal }) => getVersioned<NodeInfo>(`/nodes/${enc(id)}`, { signal }), enabled: !!id });
export const useNodeConfig = (id: string) => useQuery({ queryKey: ['nodes', id, 'config'], queryFn: ({ signal }) => getVersioned<NodeConfig>(`/nodes/${enc(id)}/configuration`, { signal }), enabled: !!id, retry: 0 });
export const useNodeAccount = (id: string) => useQuery({ queryKey: ['nodes', id, 'account'], queryFn: ({ signal }) => getVersioned<NodeAccount>(`/nodes/${enc(id)}/account`, { signal }), enabled: !!id, retry: 0 });
export const usePublicAddresses = (id: string) => useQuery({ queryKey: ['nodes', id, 'public-addresses'], queryFn: ({ signal }) => getVersioned<PublicAddresses>(`/nodes/${enc(id)}/public-addresses`, { signal }), enabled: !!id, retry: 0 });

export function useContainers(nodeId?: string) {
  return useQuery({
    queryKey: ['containers', nodeId ?? 'all'],
    queryFn: ({ signal }) => readAll<ContainerList>((cursor) => get('/containers', { signal, query: { node_id: nodeId, limit: 100, cursor } })),
    refetchInterval: usePoll(3000),
    placeholderData: keepPreviousData,
  });
}
export function useCompose(nodeId?: string) {
  return useQuery({
    queryKey: ['compose', nodeId ?? 'all'],
    queryFn: ({ signal }) => readAll<ComposeProjectList>((cursor) => get('/compose-projects', { signal, query: { node_id: nodeId, limit: 100, cursor } })),
    refetchInterval: usePoll(5000),
    placeholderData: keepPreviousData,
  });
}

export const useFirewallStatus = (nodeId: string) => useQuery({
  queryKey: ['firewall', nodeId, 'status'],
  queryFn: ({ signal }) => get<ManagedFirewallState>(`/nodes/${enc(nodeId)}/firewall/status`, { signal }),
  enabled: !!nodeId,
  refetchInterval: usePoll(10_000),
});
export const useFirewallConfig = (nodeId: string) => useQuery({
  queryKey: ['firewall', nodeId, 'config'],
  queryFn: ({ signal }) => getVersioned<FirewallConfiguration>(`/nodes/${enc(nodeId)}/firewall/configuration`, { signal }),
  enabled: !!nodeId,
});
export const usePeerTrust = () => useQuery({ queryKey: ['peer-trust', 'policy'], queryFn: ({ signal }) => getVersioned<PeerTrustPolicy>('/peer-trust', { signal }) });
export const usePeerTrustStatus = () => useQuery({ queryKey: ['peer-trust', 'status'], queryFn: ({ signal }) => get<PeerTrustStatus>('/peer-trust/status', { signal }), refetchInterval: usePoll(15_000) });

export function useDatabases() {
  return useQuery({
    queryKey: ['databases', 'list'],
    queryFn: ({ signal }) => readAll<ManagedDatabaseList>((cursor) => get('/database-clusters', { signal, query: { limit: 100, cursor } })),
    refetchInterval: usePoll(10_000),
    placeholderData: keepPreviousData,
  });
}
export function useDatabaseStatus(id: string, view: 'health' | 'schemas' | 'users' | 'proxies') {
  return useQuery({
    queryKey: ['databases', id, view],
    queryFn: ({ signal }) => get<ManagedDatabaseStatus>(`/database-clusters/${enc(id)}/status`, { signal, query: { view } }),
    enabled: !!id,
    refetchInterval: usePoll(view === 'health' ? 10_000 : 30_000),
    placeholderData: keepPreviousData,
  });
}
export const useSharedStorage = () => useQuery({ queryKey: ['databases', 'shared'], queryFn: ({ signal }) => get<SharedStorageOverview>('/shared-storage', { signal }), retry: 0 });

export function useTasks(nodeId?: string) {
  return useQuery({
    queryKey: ['tasks', nodeId ?? 'all'],
    queryFn: ({ signal }) => readAll<ScheduledTaskList>((cursor) => get('/tasks', { signal, query: { node_id: nodeId, limit: 100, cursor } })),
    refetchInterval: usePoll(5000),
    placeholderData: keepPreviousData,
  });
}

export function useOperationsHistory(filters: { node_id?: string; module?: string; state?: string }) {
  return useQuery({
    queryKey: ['operations', 'history', filters],
    queryFn: ({ signal }) => get<OperationList>('/operations', { signal, query: { ...filters, limit: 50 } }),
    placeholderData: keepPreviousData,
  });
}

export const useMonitoringStatus = () => useQuery({ queryKey: ['monitoring', 'status'], queryFn: ({ signal }) => get<MonitoringArchiveStatusList>('/monitoring/status', { signal }), refetchInterval: usePoll(15_000), retry: 0 });

export const useSessions = () => useQuery({ queryKey: ['sessions'], queryFn: ({ signal }) => readAll<SessionList>((cursor) => get('/auth/sessions', { signal, query: { limit: 100, cursor } })) });
export const useAccount = () => useQuery({ queryKey: ['account'], queryFn: ({ signal }) => getVersioned<Account>('/settings/account', { signal }) });
export const useIdentity = () => useQuery({ queryKey: ['identity'], queryFn: ({ signal }) => get<APIIdentity>('/', { signal }), staleTime: 5 * 60_000 });
export const useTokens = (nodeId: string) => useQuery({ queryKey: ['tokens', nodeId], queryFn: ({ signal }) => readAll<APITokenList>((cursor) => get(`/nodes/${enc(nodeId)}/api-tokens`, { signal, query: { limit: 100, cursor } })), enabled: !!nodeId });
export const useEnrollments = () => useQuery({ queryKey: ['nodes', 'enrollments'], queryFn: ({ signal }) => readAll<EnrollmentTokenList>((cursor) => get('/enrollment-tokens', { signal, query: { limit: 100, cursor } })) });
export const useStorageTargets = () => useQuery({ queryKey: ['storage', 'targets'], queryFn: ({ signal }) => get<StorageTargetList>('/storage/targets', { signal }), retry: 0 });
export const useThemes = () => useQuery({ queryKey: ['themes'], queryFn: ({ signal }) => get<ThemeList>('/console/theme-installations', { signal, query: { limit: 100 } }), retry: 0 });
