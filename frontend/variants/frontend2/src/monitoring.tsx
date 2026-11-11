import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@appica/ui-react/button';
import { Progress } from '@appica/ui-react/progress';
import { Skeleton } from '@appica/ui-react/skeleton';
import { api, unwrap } from './api/client';
import type { components } from './api/schema.gen';
import { Failure } from './ui';

export type Metrics = components['schemas']['Metrics'];
export type OverviewNode = components['schemas']['OverviewNode'];
type Point = components['schemas']['MonitoringPoint'];

export const firewallNames = { enabled: '已启用', disabled: '已停用', none: '未安装', unknown: '未知' };
export const readingNames = {
  ready: '',
  partial: '采集不完整',
  error: '采集失败',
  offline: '离线',
  unsupported: '无法采集',
  queued: '等待读取',
  reading: '读取中',
  stale: '待更新',
  timeout: '读取超时',
};

export const overviewQuery = {
  queryKey: ['overview'],
  retry: false,
  queryFn: async ({ signal }: { signal: AbortSignal }) => unwrap(await api.GET('/overview', { signal })),
  refetchInterval: 3000,
};

export function bytes(value?: number) {
  if (value === undefined) return '—';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  const index = value > 0 ? Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1) : 0;
  return `${(value / 1024 ** index).toLocaleString('zh-CN', { maximumFractionDigits: index ? 1 : 0 })} ${units[index]}`;
}

export const percent = (value?: number) => (value === undefined ? '—' : `${value.toFixed(1)}%`);
export const ratio = (used?: number, total?: number) =>
  used !== undefined && total ? (used / total) * 100 : undefined;
export const speed = (value?: number) => (value === undefined ? '—' : `${bytes(value)}/s`);

export function ResourceCards({
  metrics,
  coverage,
  loading,
}: {
  metrics?: Metrics;
  coverage?: components['schemas']['MonitoringCoverage'];
  loading?: boolean;
}) {
  const memory = ratio(metrics?.memory_used, metrics?.memory_total);
  const disk = ratio(metrics?.disk_used, metrics?.disk_total);
  const items = [
    {
      label: 'CPU',
      value: percent(metrics?.cpu_usage),
      detail: metrics?.cpu_usage === undefined ? '' : `${metrics.cpu_cores} 核`,
      count: coverage?.cpu,
      progress: metrics?.cpu_usage,
    },
    {
      label: '内存',
      value: percent(memory),
      detail: `${bytes(metrics?.memory_used)} / ${bytes(metrics?.memory_total)}`,
      count: coverage?.memory,
      progress: memory,
    },
    {
      label: '磁盘',
      value: percent(disk),
      detail: `${bytes(metrics?.disk_used)} / ${bytes(metrics?.disk_total)}`,
      count: coverage?.disk,
      progress: disk,
    },
    {
      label: '网络',
      value: `↓ ${speed(metrics?.network_rx)}`,
      detail: `↑ ${speed(metrics?.network_tx)}`,
      count: coverage?.network,
    },
  ];

  return (
    <div className="grid gap-3.5 sm:grid-cols-2 xl:grid-cols-4" aria-busy={loading}>
      {items.map((item) => (
        <section key={item.label} className="metric-card flex min-w-0 flex-col">
          <div className="mb-2 flex min-h-5 items-center justify-between gap-2">
            <h2 className="text-xs font-semibold uppercase tracking-wide text-foreground-muted">{item.label}</h2>
            {item.count !== undefined && (
              <span className="tabular-nums text-xs text-foreground-muted">{item.count} 个有效节点</span>
            )}
          </div>
          <div className="min-h-8 truncate text-2xl font-semibold tabular-nums tracking-tight text-foreground-intense">
            {loading ? <Skeleton className="h-8 w-24 rounded-md" /> : item.value}
          </div>
          <div className="mt-1.5 min-h-5 text-xs tabular-nums text-foreground-muted">
            {loading ? <Skeleton className="h-4 w-36 rounded-md" /> : item.detail}
          </div>
          <div className="mt-3 h-2">
            {loading ? (
              <Skeleton className="h-full w-full rounded-full" />
            ) : (
              item.progress !== undefined && <Progress aria-label={`${item.label}使用率`} value={item.progress} />
            )}
          </div>
        </section>
      ))}
    </div>
  );
}

function Chart({
  title,
  points,
  series,
  network = false,
  loading,
  unavailable,
}: {
  title: string;
  points: Point[];
  series: ((metrics: Metrics) => number | undefined)[];
  network?: boolean;
  loading?: boolean;
  unavailable?: boolean;
}) {
  const values = points
    .flatMap((p) => series.map((read) => read(p.summary.metrics)))
    .filter((n): n is number => n !== undefined);
  const ceiling = network ? Math.max(...values, 1) * 1.1 : 100;
  const width = 600;
  const height = 110;
  const padding = 5;
  const paths = series.map((read) => {
    let connected = false;
    return points
      .map((point, index) => {
        const value = read(point.summary.metrics);
        if (value === undefined) {
          connected = false;
          return '';
        }
        const x = padding + (index / Math.max(points.length - 1, 1)) * (width - padding * 2);
        const y = padding + (1 - value / ceiling) * (height - padding * 2);
        const command = connected ? 'L' : 'M';
        connected = true;
        return `${command}${x.toFixed(2)},${y.toFixed(2)} l0.1,0`;
      })
      .join(' ');
  });
  const shortTime = (value: string) =>
    new Date(value).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });

  return (
    <div className="min-w-0 rounded-xl border border-border bg-background-subtle/35 p-3.5">
      <div className="mb-2.5 flex items-center justify-between gap-3 text-xs">
        <h3 className="font-semibold text-foreground-strong">{title}</h3>
        <span className="text-foreground-muted">{network ? '↓ 接收 / ↑ 发送' : '%'}</span>
      </div>
      {loading ? (
        <Skeleton className="h-28 w-full rounded-lg" />
      ) : values.length ? (
        <svg
          role="img"
          aria-label={`${title}历史趋势`}
          viewBox={`0 0 ${width} ${height}`}
          className="h-28 w-full overflow-visible"
          preserveAspectRatio="none"
        >
          <title>
            {title}，{network ? `最大刻度 ${speed(ceiling)}` : '0 至 100%'}
          </title>
          {[0, 0.5, 1].map((p) => (
            <line
              key={p}
              x1={padding}
              y1={padding + p * (height - padding * 2)}
              x2={width - padding}
              y2={padding + p * (height - padding * 2)}
              className="stroke-border"
              vectorEffect="non-scaling-stroke"
            />
          ))}
          {paths.map((d, i) => (
            <path
              key={i}
              d={d}
              fill="none"
              className={i ? 'stroke-foreground-muted' : 'stroke-primary'}
              strokeWidth="1.8"
              strokeDasharray={i ? '5 3' : undefined}
              strokeLinecap="round"
              vectorEffect="non-scaling-stroke"
            />
          ))}
        </svg>
      ) : (
        <div className="flex h-28 items-center justify-center rounded-lg bg-background-muted text-xs text-foreground-muted">
          {unavailable ? '—' : '暂无监控数据'}
        </div>
      )}
      <div className="mt-1.5 flex min-h-4 justify-between gap-2 tabular-nums text-xs text-foreground-muted">
        {loading ? (
          <>
            <Skeleton className="h-4 w-24 rounded-md" />
            <Skeleton className="h-4 w-24 rounded-md" />
          </>
        ) : (
          points.length > 0 && (
            <>
              <span>{shortTime(points[0].collected_at)}</span>
              <span>{network ? speed(ceiling) : '100%'}</span>
              <span>{shortTime(points[points.length - 1].collected_at)}</span>
            </>
          )
        )}
      </div>
    </div>
  );
}

export function Trends({ nodeID }: { nodeID?: string }) {
  const [range, setRange] = useState<'24h' | '7d'>('24h');
  const query = useQuery({
    queryKey: ['monitoring', 'history', range, nodeID ?? ''],
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/monitoring/history', { signal, params: { query: { range, node_id: nodeID } } })),
    refetchInterval: (q) => (q.state.data?.interval_minutes ?? 5) * 60000,
  });
  const points = query.data?.items ?? [];

  return (
    <section className="panel min-w-0">
      <div className="panel-header">
        <h2 className="section-title">资源趋势</h2>
        <div className="flex gap-1" aria-label="时间范围">
          {([
            ['24h', '24 小时'],
            ['7d', '7 天'],
          ] as const).map(([value, label]) => (
            <Button
              key={value}
              variant={range === value ? 'secondary' : 'ghost'}
              size="sm"
              aria-pressed={range === value}
              onClick={() => setRange(value)}
            >
              {label}
            </Button>
          ))}
        </div>
      </div>
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      <div className="space-y-4" aria-busy={query.isPending}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Chart
            title="CPU"
            points={points}
            series={[(m) => m.cpu_usage]}
            loading={query.isPending}
            unavailable={!query.data && query.isError}
          />
          <Chart
            title="内存"
            points={points}
            series={[(m) => ratio(m.memory_used, m.memory_total)]}
            loading={query.isPending}
            unavailable={!query.data && query.isError}
          />
        </div>
        <Chart
          title="网络"
          points={points}
          series={[(m) => m.network_rx, (m) => m.network_tx]}
          network
          loading={query.isPending}
          unavailable={!query.data && query.isError}
        />
      </div>
    </section>
  );
}
