import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api, value, type Schema } from '../api/client';
import { ratio } from '../lib/format';
import { Led } from './ui';

export function FleetList({ selected, onSelect }: { selected: string; onSelect: (id: string) => void }) {
  const [search, setSearch] = useState('');
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 8_000,
  });
  const overview = useQuery({
    queryKey: ['overview'],
    queryFn: async ({ signal }) => value(await api.GET('/overview', { signal })),
    refetchInterval: 3000,
    staleTime: 2500,
  });

  const readings = useMemo(() => {
    const map = new Map<string, Schema['MonitoringReading']>();
    for (const item of overview.data?.nodes ?? []) map.set(item.node.id, item.reading);
    return map;
  }, [overview.data]);

  const list = (nodes.data?.items ?? []).filter((node) => node.name.toLowerCase().includes(search.trim().toLowerCase()));

  return (
    <aside className="fleet" aria-label="节点列表">
      <input className="input" placeholder="搜索节点" aria-label="搜索节点" value={search} onChange={(event) => setSearch(event.target.value)} />
      <div className="fleet__list">
        {list.map((node) => {
          const reading = readings.get(node.id);
          const cpu = reading?.metrics?.cpu_usage;
          const mem = ratio(reading?.metrics?.memory_used, reading?.metrics?.memory_total);
          const load = Math.max(cpu ?? 0, mem ?? 0);
          return (
            <button
              key={node.id}
              type="button"
              className="fleet__node"
              aria-current={node.id === selected ? 'true' : undefined}
              onClick={() => onSelect(node.id)}
            >
              <Led state={node.connected ? (load >= 90 ? 'warn' : 'on') : 'off'} />
              <span className="name">{node.name}</span>
              <span className="cpu">{node.connected ? (load >= 90 ? `${Math.round(load)}%` : `${Math.round(cpu ?? 0)}`) : '—'}</span>
            </button>
          );
        })}
      </div>
    </aside>
  );
}
