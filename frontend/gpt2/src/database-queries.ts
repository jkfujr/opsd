import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { components } from './api/types';
import { allPages, api, unwrap } from './api/client';
import { usePagedQuery } from './api/pagination';
import { queryClient } from './state';
import { databaseReading, mergeDatabaseHealth } from './database-state';

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

function usePolling(scope: string) {
  const [visible, setVisible] = useState(document.visibilityState !== 'hidden');
  const window = useRef({ scope, until: Date.now() + 5000 });
  if (window.current.scope !== scope) window.current = { scope, until: Date.now() + 5000 };
  useEffect(() => { const update = () => setVisible(document.visibilityState !== 'hidden'); document.addEventListener('visibilitychange', update); return () => document.removeEventListener('visibilitychange', update); }, []);
  return { begin: () => { window.current.until = Date.now() + 5000; }, interval: (reading: boolean, error: boolean) => !visible ? false : !error && reading && Date.now() < window.current.until ? 500 : 10000 };
}

export function useDatabaseList() {
  const polling = usePolling('list');
  const query = usePagedQuery({ queryKey: ['databases', 'list'], queryFn: async ({ signal }) => {
    const list = await allPages(cursor => api.GET('/database-clusters', { signal, params: { query: { cursor, limit: 100 } } }).then(unwrap));
    return { ...list, items: list.items.map(syncHealth) };
  }, refetchInterval: q => polling.interval(q.state.data?.items.some(item => databaseReading(item)) ?? false, !!q.state.error) });
  return { ...query, refetch: () => { polling.begin(); return query.refetch(); } };
}

export function useDatabaseQuery(id: string, view: View, enabled = true) {
  const polling = usePolling(`${id}/${view}/${enabled}`);
  const query = useQuery({ queryKey: ['databases', 'detail', id, view], enabled, queryFn: async ({ signal }) => {
    const value = unwrap(await api.GET('/database-clusters/{id}/status', { signal, params: { path: { id }, query: { view } } }));
    return view === 'health' ? syncHealth(value) : value;
  }, refetchInterval: q => enabled ? polling.interval(!!q.state.data && databaseReading(q.state.data, view), !!q.state.error) : false });
  return { ...query, refetch: () => { polling.begin(); return query.refetch(); } };
}
