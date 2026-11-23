import { QueryClient } from '@tanstack/react-query';
import { ApiError } from '../api/http';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: true,
      retry: (count, error) => count < 2 && (!(error instanceof ApiError) || error.status === 0 || error.status >= 500) && !(error instanceof ApiError && error.status === 503),
      retryDelay: (n) => Math.min(800 * 2 ** n, 4000),
    },
    mutations: { retry: false },
  },
});

/** 资源变更事件 → 需要失效的查询前缀。 */
export const invalidationMap: Record<string, string[][]> = {
  nodes: [['nodes'], ['overview'], ['peer-trust'], ['containers'], ['tasks'], ['databases']],
  monitoring: [['overview'], ['monitoring']],
  settings: [['nodes'], ['tokens']],
  account: [['nodes'], ['account']],
  operations: [['operations'], ['overview'], ['containers'], ['compose'], ['tasks']],
  firewall: [['firewall'], ['overview'], ['peer-trust']],
  peer_trust: [['peer-trust'], ['firewall']],
  public_addresses: [['nodes'], ['peer-trust']],
  tokens: [['tokens']],
  containers: [['containers'], ['compose']],
  databases: [['databases']],
  tasks: [['tasks']],
  compose: [['compose'], ['containers']],
  navigation: [['navigation']],
  sessions: [['sessions']],
  themes: [['themes']],
  storage: [['storage'], ['monitoring']],
};

export function invalidateResource(resource: string) {
  if (resource === 'all') return void queryClient.invalidateQueries({ type: 'active' }, { cancelRefetch: false });
  for (const key of invalidationMap[resource] ?? []) void queryClient.invalidateQueries({ queryKey: key }, { cancelRefetch: false });
}
