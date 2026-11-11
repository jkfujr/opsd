import { api, unwrap } from './api/client';
import type { components } from './api/types';
export type Metrics = components['schemas']['Metrics'];
export type OverviewNode = components['schemas']['OverviewNode'];
export const overviewQuery = { queryKey: ['overview'], retry: false, queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/overview', { signal })), refetchInterval: 3000 };
export const readingNames = { ready: '', partial: '采集不完整', error: '采集失败', offline: '离线', unsupported: '无法采集', queued: '等待读取', reading: '读取中', stale: '待更新', timeout: '读取超时' };
export function bytes(value?: number) { if (value === undefined || !Number.isFinite(value)) return '—'; const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']; const index = value > 0 ? Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1) : 0; return `${(value / 1024 ** index).toLocaleString('zh-CN', { maximumFractionDigits: index ? 1 : 0 })} ${units[index]}`; }
export const percent = (value?: number) => value === undefined ? '—' : `${value.toFixed(1)}%`;
export const ratio = (used?: number, total?: number) => used !== undefined && total ? used / total * 100 : undefined;
export const speed = (value?: number) => value === undefined ? '—' : `${bytes(value)}/s`;
export function nodeMetrics(item?: OverviewNode) { return item && ['ready', 'partial'].includes(item.reading.state) ? item.reading.metrics : undefined; }
export function aggregate(nodes: OverviewNode[]): Metrics {
  const metrics = nodes.map(nodeMetrics).filter((m): m is Metrics => !!m);
  const cpus = metrics.filter(m => m.cpu_usage !== undefined && m.cpu_cores > 0), cores = cpus.reduce((n, m) => n + m.cpu_cores, 0);
  const result: Metrics = { collected_at: metrics[0]?.collected_at ?? '', cpu_cores: cores, errors: [] };
  if (cores) result.cpu_usage = cpus.reduce((n, m) => n + m.cpu_usage! * m.cpu_cores, 0) / cores;
  for (const [used, total] of [['memory_used', 'memory_total'], ['disk_used', 'disk_total'], ['network_rx', 'network_tx']] as const) {
    const valid = metrics.filter(m => m[used] !== undefined && m[total] !== undefined);
    if (valid.length) { result[used] = valid.reduce((n, m) => n + m[used]!, 0); result[total] = valid.reduce((n, m) => n + m[total]!, 0); }
  }
  return result;
}
