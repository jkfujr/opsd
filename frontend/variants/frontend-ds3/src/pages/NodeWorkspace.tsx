import { useEffect, useState, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { bytes, level, percent, ratio, speed, stamp } from '../lib/format';
import { readingStates } from '../lib/labels';
import { Button, Failure, Meter, Select, Sheet, Skeleton, Stamp, Tabs, TextInput, Trace } from '../components/ui';

type TabId = 'reading' | 'network' | 'config' | 'account' | 'data';

export function NodeWorkspace({
  id,
  tools,
  onRename,
  onRevoke,
}: {
  id: string;
  tools?: ReactNode;
  onRename?: (node: Schema['Node']) => void;
  onRevoke?: (node: Schema['Node']) => void;
}) {
  const [tab, setTab] = useState<TabId>('reading');
  const node = useQuery({
    queryKey: ['nodes', id],
    queryFn: async ({ signal }) => value(await api.GET('/nodes/{id}', { signal, params: { path: { id } } })),
    refetchInterval: 10_000,
  });
  const overview = useQuery({
    queryKey: ['overview'],
    queryFn: async ({ signal }) => value(await api.GET('/overview', { signal })),
    refetchInterval: 3000,
    staleTime: 2500,
  });
  const data = node.data;
  const reading = overview.data?.nodes.find((item) => item.node.id === id)?.reading;

  return (
    <div className="stack gap-12" style={{ minWidth: 0 }}>
      <header className="spread wrap" style={{ minWidth: 0 }}>
        <div className="row gap-10" style={{ minWidth: 0 }}>
          <h1 style={{ fontSize: 21, fontWeight: 600, letterSpacing: '-.015em', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {node.isPending ? '…' : data?.name ?? id}
          </h1>
          <Stamp state={data?.revoked_at ? 'rejected' : data?.connected ? 'online' : 'offline'} size="lg">
            {data?.revoked_at ? '已撤销' : data?.connected ? '在线' : '离线'}
          </Stamp>
          {data?.local ? <span className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>本机</span> : null}
        </div>
        <div className="row gap-6 wrap">
          {tools}
          {onRename && data && !data.revoked_at ? (
            <Button size="sm" variant="ghost" icon="edit" onClick={() => onRename(data)}>
              改名
            </Button>
          ) : null}
          {onRevoke && data && !data.revoked_at && !data.local ? (
            <Button size="sm" variant="danger" onClick={() => onRevoke(data)}>
              撤销
            </Button>
          ) : null}
        </div>
      </header>

      {reading?.error ? <div className="notice">{reading.error}</div> : null}
      {reading?.firewall_error ? <div className="notice">{reading.firewall_error}</div> : null}
      {data?.write_blocked_reason ? <div className="notice" style={{ borderLeftColor: 'var(--bad)' }}>{data.write_blocked_reason}</div> : null}
      <Failure error={node.error} retry={() => void node.refetch()} busy={node.isFetching} />

      <Tabs
        items={[
          { id: 'reading', label: '读数' },
          { id: 'network', label: '网络' },
          { id: 'config', label: '配置' },
          { id: 'account', label: '账号' },
          { id: 'data', label: '数据' },
        ]}
        value={tab}
        onChange={(next) => setTab(next as TabId)}
        label="节点页签"
      />

      {tab === 'reading' ? <ReadingPanel id={id} reading={reading} /> : null}
      {tab === 'network' ? <NetworkPanel id={id} node={data} /> : null}
      {tab === 'config' ? <ConfigPanel id={id} /> : null}
      {tab === 'account' ? <AccountPanel id={id} /> : null}
      {tab === 'data' ? <DataPanel id={id} /> : null}
    </div>
  );
}

function ReadingPanel({ id, reading }: { id: string; reading?: Schema['MonitoringReading'] }) {
  const trend = useQuery({
    queryKey: ['monitoring', 'history', '24h', id],
    queryFn: async ({ signal }) => value(await api.GET('/monitoring/history', { signal, params: { query: { range: '24h', node_id: id } } })),
    refetchInterval: 180_000,
  });
  const metrics = reading?.metrics;
  const memory = ratio(metrics?.memory_used, metrics?.memory_total);
  const disk = ratio(metrics?.disk_used, metrics?.disk_total);
  const points = trend.data?.items ?? [];
  const label: [string, string] = [
    points[0] ? stamp(points[0].collected_at).slice(5, 16) : '',
    points[points.length - 1] ? stamp(points[points.length - 1].collected_at).slice(5, 16) : '',
  ];

  return (
    <Sheet title="运行读数" tools={reading ? <Stamp state={reading.state === 'ready' ? 'online' : reading.state}>{readingStates[reading.state]}</Stamp> : null}>
      <div className="stack gap-14">
        <div className="stack gap-10">
          <Reading label="CPU" value={metrics?.cpu_usage} text={percent(metrics?.cpu_usage)} detail={`${metrics?.cpu_cores ?? '—'} 核`} />
          <Reading label="内存" value={memory} text={percent(memory)} detail={`${bytes(metrics?.memory_used)} / ${bytes(metrics?.memory_total)}`} />
          <Reading label="磁盘" value={disk} text={percent(disk)} detail={`${bytes(metrics?.disk_used)} / ${bytes(metrics?.disk_total)}`} />
          <div className="spread" style={{ fontSize: 12 }}>
            <span className="kicker">网络</span>
            <span className="mono" style={{ fontSize: 11 }}>↓ {speed(metrics?.network_rx)} · ↑ {speed(metrics?.network_tx)}</span>
          </div>
        </div>
        {metrics?.errors.length ? (
          <ul className="stack gap-4">
            {metrics.errors.map((error) => (
              <li key={error} className="mono" style={{ fontSize: 10, color: 'var(--warn)' }}>{error}</li>
            ))}
          </ul>
        ) : null}
        <div className="chart">
          <div className="chart__head">
            <h3>24 小时 CPU / 内存</h3>
            <span className="legend">
              <span><i />CPU</span>
              <span><i style={{ background: 'var(--info)' }} />内存</span>
            </span>
          </div>
          {trend.isPending ? <Skeleton height={70} /> : (
            <Trace
              height={70}
              ceiling={100}
              xLabels={label}
              aria="节点资源趋势"
              series={[
                { values: points.map((point) => point.summary.metrics.cpu_usage), color: 'var(--ink)' },
                { values: points.map((point) => ratio(point.summary.metrics.memory_used, point.summary.metrics.memory_total)), color: 'var(--info)', dashed: true },
              ]}
            />
          )}
        </div>
      </div>
    </Sheet>
  );
}

function Reading({ label, value: reading, text, detail }: { label: string; value?: number; text: string; detail: string }) {
  return (
    <div className="stack" style={{ gap: 4 }}>
      <div className="spread">
        <span className="kicker">{label}</span>
        <span className="num" style={{ fontSize: 12.5 }}>{text}</span>
      </div>
      <Meter value={reading} levelHint={level(reading)} />
      <span className="mono" style={{ fontSize: 9.5, color: 'var(--ink-mute)' }}>{detail}</span>
    </div>
  );
}

function NetworkPanel({ id, node }: { id: string; node?: Schema['Node'] }) {
  return (
    <Sheet title="身份与网络">
      <div className="stack gap-14">
        <dl className="facts">
          <dt>节点 ID</dt>
          <dd className="mono">{node?.id ?? id}</dd>
          <dt>地域</dt>
          <dd>{node?.region || '未设地域'}</dd>
          <dt>通信地址</dt>
          <dd className="mono">{node?.address || '—'}</dd>
          <dt>控制台</dt>
          <dd>{node?.console_enabled ? '已开启' : '未开启'}</dd>
          <dt>最近连接</dt>
          <dd className="mono">{node ? stamp(node.last_seen_at) : '—'}</dd>
        </dl>
        <PublicAddresses id={id} />
      </div>
    </Sheet>
  );
}

function PublicAddresses({ id }: { id: string }) {
  const query = useQuery({
    queryKey: ['public-addresses', id],
    queryFn: async ({ signal }) => value(await api.GET('/nodes/{id}/public-addresses', { signal, params: { path: { id } } })),
    refetchInterval: 30_000,
  });
  const [v4mode, setV4Mode] = useState<'auto' | 'manual'>('auto');
  const [v4, setV4] = useState('');
  const [v6mode, setV6Mode] = useState<'auto' | 'manual'>('auto');
  const [v6, setV6] = useState('');

  useEffect(() => {
    const data = query.data;
    if (!data) return;
    setV4Mode(data.ipv4.mode);
    setV4(data.ipv4.addresses.join('\n'));
    setV6Mode(data.ipv6.mode);
    setV6(data.ipv6.addresses.join('\n'));
  }, [query.data]);

  const save = useMutation({
    mutationFn: async () =>
      value(
        await api.PATCH('/nodes/{id}/public-addresses', {
          params: { path: { id } },
          body: {
            expected_revision: query.data?.revision ?? 0,
            ipv4: { mode: v4mode, addresses: v4mode === 'manual' ? v4.split(/[\s,]+/).filter(Boolean) : [] },
            ipv6: { mode: v6mode, addresses: v6mode === 'manual' ? v6.split(/[\s,]+/).filter(Boolean) : [] },
          },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['public-addresses', id] });
      void queryClient.invalidateQueries({ queryKey: ['nodes'] });
    },
  });

  const dirty = query.data
    ? v4mode !== query.data.ipv4.mode || v6mode !== query.data.ipv6.mode ||
      (v4mode === 'manual' && v4.trim() !== query.data.ipv4.addresses.join('\n')) ||
      (v6mode === 'manual' && v6.trim() !== query.data.ipv6.addresses.join('\n'))
    : false;

  const stateLabel = (state?: string) =>
    state === 'confirmed' ? '已确认' : state === 'pending' ? '待确认' : state === 'conflict' ? '冲突' : state === 'failed' ? '失败' : '不可用';

  return (
    <div className="stack gap-10" style={{ paddingTop: 12, borderTop: '1px solid var(--rule-soft)' }}>
      <div className="spread">
        <span className="kicker">公网地址</span>
        <span className="mono" style={{ fontSize: 9.5, color: 'var(--ink-mute)' }}>修订 {query.data?.revision ?? '—'}</span>
      </div>
      {query.isPending ? <Skeleton height={50} /> : null}
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      {(['ipv4', 'ipv6'] as const).map((family) => {
        const addr = family === 'ipv4' ? query.data?.ipv4 : query.data?.ipv6;
        const mode = family === 'ipv4' ? v4mode : v6mode;
        return (
          <div key={family} className="stack gap-6">
            <div className="spread">
              <span className="mono" style={{ fontSize: 10, letterSpacing: '.1em', color: 'var(--ink-mute)' }}>{family.toUpperCase()}</span>
              <Stamp state={addr?.state === 'confirmed' ? 'confirmed' : addr?.state === 'pending' ? 'pending' : 'failed'}>{stateLabel(addr?.state)}</Stamp>
            </div>
            <div className="row gap-6">
              <Select
                aria-label={`${family} 模式`}
                value={mode}
                onChange={(event) => (family === 'ipv4' ? setV4Mode(event.target.value as 'auto' | 'manual') : setV6Mode(event.target.value as 'auto' | 'manual'))}
                options={[['auto', '自动'], ['manual', '手工']]}
                style={{ height: 28, width: 86, fontSize: 11.5 }}
              />
              {mode === 'manual' ? (
                <input
                  className="input input--mono"
                  style={{ height: 28, fontSize: 11 }}
                  value={family === 'ipv4' ? v4 : v6}
                  onChange={(event) => (family === 'ipv4' ? setV4(event.target.value) : setV6(event.target.value))}
                  placeholder={family === 'ipv4' ? '203.0.113.7' : '2001:db8::7'}
                  aria-label={`${family} 地址`}
                />
              ) : (
                <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-soft)' }}>{addr?.addresses.join('、') || '尚未确认'}</span>
              )}
            </div>
            {addr?.error ? <span style={{ fontSize: 11, color: 'var(--bad)' }}>{addr.error}</span> : null}
          </div>
        );
      })}
      <div className="row gap-8">
        <Button size="sm" variant="line" busy={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
          保存地址
        </Button>
      </div>
      <Failure error={save.error} />
    </div>
  );
}

function ConfigPanel({ id }: { id: string }) {
  const query = useQuery({
    queryKey: ['node-settings', id],
    queryFn: async ({ signal }) => value(await api.GET('/nodes/{id}/settings', { signal, params: { path: { id } } })),
    retry: false,
  });
  const [form, setForm] = useState<Schema['NodeConfig'] | null>(null);
  useEffect(() => setForm(query.data ?? null), [query.data]);
  const save = useMutation({
    mutationFn: async () =>
      value(await api.PATCH('/nodes/{id}/settings', { params: { path: { id }, header: { 'Idempotency-Key': crypto.randomUUID() } }, body: form! })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['node-settings', id] }),
  });
  const dirty = Boolean(form && query.data && JSON.stringify(form) !== JSON.stringify(query.data));

  return (
    <Sheet
      title="本机配置"
      tools={
        <Button size="sm" variant="solid" busy={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
          保存配置
        </Button>
      }
    >
      {query.isPending ? <Skeleton height={110} /> : null}
      {query.error ? <div className="empty">无法读取本机配置</div> : null}
      {form ? (
        <div className="stack gap-14">
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2,minmax(0,1fr))', gap: 12, maxWidth: 640 }}>
            <TextInput label="节点名称" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            <TextInput label="地域" value={form.region} onChange={(event) => setForm({ ...form, region: event.target.value })} placeholder="留空表示未设置" />
            <TextInput label="节点通信监听" value={form.listen} onChange={(event) => setForm({ ...form, listen: event.target.value })} className="input--mono" />
            <TextInput label="控制台监听" value={form.console_listen} onChange={(event) => setForm({ ...form, console_listen: event.target.value })} className="input--mono" />
            <Select
              label="采集间隔"
              value={String(form.interval_minutes)}
              onChange={(event) => setForm({ ...form, interval_minutes: Number(event.target.value) })}
              options={[['1', '1 分钟'], ['5', '5 分钟'], ['15', '15 分钟'], ['30', '30 分钟'], ['60', '60 分钟']]}
            />
            <Select
              label="控制台"
              value={form.console ? 'on' : 'off'}
              onChange={(event) => setForm({ ...form, console: event.target.value === 'on' })}
              options={[['on', '开启'], ['off', '关闭']]}
            />
          </div>
          <div className="spread" style={{ paddingTop: 12, borderTop: '1px solid var(--rule-soft)' }}>
            <div className="stack gap-2">
              <span className="kicker">OPSD 数据</span>
              <span className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>
                {form.storage.enabled ? `启用 · ${form.storage.address}:${form.storage.port}` : '未启用'}
              </span>
            </div>
            <Button size="sm" onClick={() => setForm({ ...form, storage: { ...form.storage, enabled: !form.storage.enabled } })}>
              {form.storage.enabled ? '停用' : '启用'}
            </Button>
          </div>
          <Failure error={save.error} />
        </div>
      ) : null}
    </Sheet>
  );
}

function AccountPanel({ id }: { id: string }) {
  const query = useQuery({
    queryKey: ['node-account', id],
    queryFn: async ({ signal }) => value(await api.GET('/nodes/{id}/account', { signal, params: { path: { id } } })),
    retry: false,
  });
  const [username, setUsername] = useState('');
  const [entrance, setEntrance] = useState('');
  const [password, setPassword] = useState('');
  useEffect(() => {
    setUsername(query.data?.username ?? '');
    setEntrance(query.data?.entrance ?? '');
  }, [query.data]);
  const save = useMutation({
    mutationFn: async () =>
      value(
        await api.PATCH('/nodes/{id}/account', {
          params: { path: { id }, header: { 'Idempotency-Key': crypto.randomUUID() } },
          body: { revision: query.data?.revision, username, entrance, password: password || undefined },
        }),
      ),
    onSuccess: () => {
      setPassword('');
      void queryClient.invalidateQueries({ queryKey: ['node-account', id] });
    },
  });
  const dirty = Boolean(query.data && (username !== query.data.username || entrance !== query.data.entrance || password));

  return (
    <Sheet
      title="本机账号"
      tools={
        <Button size="sm" variant="solid" busy={save.isPending} disabled={!dirty || !username} onClick={() => save.mutate()}>
          保存账号
        </Button>
      }
    >
      {query.isPending ? <Skeleton height={80} /> : null}
      {query.error ? <div className="empty">无法读取本机账号</div> : null}
      {query.data ? (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2,minmax(0,1fr))', gap: 12, maxWidth: 560 }}>
          <TextInput label="账号" value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="off" />
          <TextInput label="登录入口" value={entrance} onChange={(event) => setEntrance(event.target.value)} className="input--mono" placeholder="/console" />
          <TextInput label="新密码（留空不修改）" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" />
        </div>
      ) : null}
      <Failure error={save.error} />
    </Sheet>
  );
}

function DataPanel({ id }: { id: string }) {
  const query = useQuery({
    queryKey: ['storage', id],
    queryFn: async ({ signal }) => value(await api.GET('/nodes/{id}/storage', { signal, params: { path: { id } } })),
    retry: false,
  });
  return (
    <Sheet title="OPSD 数据">
      {query.isPending ? <Skeleton height={60} /> : null}
      {query.error ? <div className="empty">无法读取存储状态</div> : null}
      {query.data ? (
        <div className="stack gap-10">
          <dl className="facts">
            <dt>状态</dt>
            <dd><Stamp state={query.data.state === 'ready' ? 'ready' : query.data.state === 'pending' ? 'pending' : 'unavailable'}>{query.data.state === 'ready' ? '就绪' : query.data.state === 'pending' ? '同步中' : '不可用'}</Stamp></dd>
            <dt>承载节点</dt>
            <dd className="mono">{query.data.selected || '未指定'}</dd>
            <dt>待同步</dt>
            <dd className="mono">{query.data.pending} 项</dd>
          </dl>
          <div className="stack gap-4">
            {query.data.replications.map((item) => (
              <div key={item.id} className="spread mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>
                <span>{item.id}</span>
                <span>{item.state} · 错误 {item.error_count}</span>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </Sheet>
  );
}
