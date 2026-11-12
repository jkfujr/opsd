import { useQuery } from '@tanstack/react-query';
import type { components } from './api/types';
import { allPages, api, unwrap } from './api/client';
import { usePagedQuery } from './api/pagination';
import { queryClient } from './state';
import { databaseReading, mergeDatabaseHealth } from './database-state';
import { useReadPolling } from './read-polling';

type Value = components['schemas']['ManagedDatabaseStatus'];
type List = components['schemas']['ManagedDatabaseList'];
type View = 'health' | 'schemas' | 'users' | 'proxies';

function syncHealth(incoming: Value) {
  const key = ['databases', 'detail', incoming.cluster.id, 'health'];
  let value = mergeDatabaseHealth(queryClient.getQueryData<Value>(key), incoming);
  queryClient.setQueriesData<List>({ queryKey: ['databases', 'list'] }, list => list && { ...list, items: list.items.map(item => {
    if (item.cluster.id !== incoming.cluster.id) return item;
    value = mergeDatabaseHealth(item, value);
    return value;
  }) });
  if (queryClient.getQueryData(key)) queryClient.setQueryData(key, value);
  return value;
}

export function useDatabaseList() {
  const polling = useReadPolling('databases/list');
  const query = usePagedQuery({ queryKey: ['databases', 'list'], queryFn: async ({ signal }) => {
    const list = await allPages(cursor => api.GET('/database-clusters', { signal, params: { query: { cursor, limit: 100 } } }).then(unwrap));
    list.items = list.items.map(syncHealth);
    return list;
  }, refetchInterval: q => polling.interval(q.state.data?.items.some(item => databaseReading(item)) ?? false, !!q.state.error) });
  return { ...query, refetch: () => { polling.begin(); return query.refetch(); } };
}

export function useDatabaseQuery(id: string, view: View, enabled = true) {
  const polling = useReadPolling(`databases/${id}/${view}/${enabled}`);
  const query = useQuery({ queryKey: ['databases', 'detail', id, view], enabled, queryFn: async ({ signal }) => {
    const value = unwrap(await api.GET('/database-clusters/{id}/status', { signal, params: { path: { id }, query: { view } } }));
    return view === 'health' ? syncHealth(value) : value;
  }, refetchInterval: q => enabled ? polling.interval(!!q.state.data && databaseReading(q.state.data, view), !!q.state.error) : false });
  return { ...query, refetch: () => { polling.begin(); return query.refetch(); } };
}
