import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from 'react-router';
import { api, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { bytes, level, percent, ratio, speed, stamp } from '../lib/format';
import { firewallStates, readingStates } from '../lib/labels';
import { Docket, useOperations } from '../components/Operations';
import {
  Button,
  Empty,
  Failure,
  Meter,
  Select,
  Sheet,
  Skeleton,
  Stamp,
  TextInput,
  Trace,
} from '../components/ui';

type Family = Schema['PublicAddress'];

const trendRange = ['24h', '7d'] as const;

export function NodeDetail() {
  const { id = '' } = useParams();
  return <Detail key={id} id={id} />;
}

function Detail({ id }: { id: string }) {
  const navigate = useNavigate();
  const operations = useOperations();
  const node = useQuery({
    queryKey: ['nodes', id],
    queryFn: async ({ signal }) => value(await api.GET('/nodes/{id}', { signal, params: { path: { id } } })),
    refetchInterval: 10_000,
  });
  const overview = useQuery({
    queryKey: ['overview'],
    queryFn: async ({ signal }) => value(await api.GET('/overview', { signal })),
    refetchInterval: 3000,
  });
  const trend = useQuery({
    queryKey: ['monitoring', 'history', '24h', id],
    queryFn: async ({ signal }) => value(await api.GET('/monitoring/history', { signal, params: { query: { range: '24h', node_id: id } } })),
    refetchInterval: 180_000,
  });
  const nodeOperations = useQuery({
    queryKey: ['operations', 'node', id],
    queryFn: async ({ signal }) => value(await api.GET('/operations', { signal, params: { query: { node_id: id, limit: 12 } } })),
    refetchInterval: 15_000,
  });

  const reading = overview.data?.nodes.find((item) => item.node.id === id)?.reading;
  const data = node.data;
  const points = trend.data?.items ?? [];
  const label: [string, string] = [
    points[0] ? stamp(points[0].collected_at).slice(5, 16) : '',
    points[points.length - 1] ? stamp(points[points.length - 1].collected_at).slice(5, 16) : '',
  ];

  const metrics = reading?.metrics;
  const memory = ratio(metrics?.memory_used, metrics?.memory_total);
  const disk = ratio(metrics?.disk_used, metrics?.disk_total);

  return (
    <div className="page">
      <div className="pagehead">
        <div className="node-title">
          <h1>{node.isPending ? <Skeleton width={180} height={26} /> : data?.name ?? id}</h1>
          <Stamp state={data?.revoked_at ? 'rejected' : data?.connected ? 'online' : 'offline'} size="lg">
            {data?.revoked_at ? '已撤销' : data?.connected ? '在线' : '离线'}
          </Stamp>
          {data?.local ? <span className="kicker">本机</span> : null}
        </div>
        <div className="pagehead__tools">
          <Button size="sm" variant="ghost" icon="arrow" onClick={() => navigate(-1)}>
            返回
          </Button>
          <Link to={`/firewall?node=${id}`}>
            <Button size="sm" icon="firewall">
              防火墙{reading ? ` · ${firewallStates[reading.firewall]}` : ''}
            </Button>
          </Link>
          <Button size="sm" variant="solid" icon="terminal" onClick={() => operations.open()}>
            操作队列
          </Button>
        </div>
      </div>

      <Failure error={node.error} retry={() => void node.refetch()} busy={node.isFetching} />
      <Failure error={overview.error} retry={() => void overview.refetch()} busy={overview.isFetching} />

      {reading && reading.state !== 'ready' && !reading.error ? null : null}
      {reading?.error ? <div className="notice">{reading.error}</div> : null}
      {reading?.firewall_error ? <div className="notice">{reading.firewall_error}</div> : null}
      {data?.write_blocked_reason ? <div className="notice" style={{ borderLeftColor: 'var(--stamp)' }}>{data.write_blocked_reason}</div> : null}

      <div className="node-grid">
        <Sheet
          title="运行读数"
          tools={reading ? <Stamp state={reading.state === 'ready' ? 'online' : reading.state}>{readingStates[reading.state]}</Stamp> : null}
        >
          <div className="stack gap-16">
            <div className="stack gap-12">
              <Reading label="CPU" value={metrics?.cpu_usage} text={percent(metrics?.cpu_usage)} detail={`${metrics?.cpu_cores ?? '—'} 核`} />
              <Reading label="内存" value={memory} text={percent(memory)} detail={`${bytes(metrics?.memory_used)} / ${bytes(metrics?.memory_total)}`} />
              <Reading label="磁盘" value={disk} text={percent(disk)} detail={`${bytes(metrics?.disk_used)} / ${bytes(metrics?.disk_total)}`} />
              <div className="spread" style={{ fontSize: 12.5 }}>
                <span className="kicker">网络</span>
                <span className="mono" style={{ fontSize: 11.5 }}>
                  ↓ {speed(metrics?.network_rx)} · ↑ {speed(metrics?.network_tx)}
                </span>
              </div>
            </div>
            {metrics?.errors.length ? (
              <ul className="stack gap-4">
                {metrics.errors.map((error) => (
                  <li key={error} className="mono" style={{ fontSize: 10.5, color: 'var(--amber)' }}>{error}</li>
                ))}
              </ul>
            ) : null}
            <div className="chart">
              <div className="chart__head">
                <h3>24 小时 CPU / 内存</h3>
                <span className="legend">
                  <span><i />CPU</span>
                  <span><i style={{ background: 'var(--carbon)' }} />内存</span>
                </span>
              </div>
              {trend.isPending ? <Skeleton height={72} /> : (
                <Trace
                  height={72}
                  ceiling={100}
                  xLabels={label}
                  aria="节点资源趋势"
                  series={[
                    { values: points.map((point) => point.summary.metrics.cpu_usage), color: 'var(--ink)' },
                    { values: points.map((point) => ratio(point.summary.metrics.memory_used, point.summary.metrics.memory_total)), color: 'var(--carbon)', dashed: true },
                  ]}
                />
              )}
            </div>
          </div>
        </Sheet>

        <div className="stack gap-12">
          <Sheet
            title="身份与网络"

            tools={
              <Button size="sm" variant="ghost" icon="copy" onClick={() => void navigator.clipboard.writeText(data?.public_key ?? '')}>
                公钥
              </Button>
            }
          >
            <dl className="facts">
              <dt>节点 ID</dt>
              <dd className="mono">{data?.id ?? id}</dd>
              <dt>地域</dt>
              <dd>{data?.region || '未设地域'}</dd>
              <dt>通信地址</dt>
              <dd className="mono">{data?.address || '—'}</dd>
              <dt>控制台</dt>
              <dd>{data?.console_enabled ? '已开启' : '未开启'}</dd>
              <dt>最近连接</dt>
              <dd className="mono">{data ? stamp(data.last_seen_at) : '—'}</dd>
              <dt>存储地址</dt>
              <dd className="mono">{data?.storage_address || '未启用'}</dd>
              <dt>可写</dt>
              <dd>{data?.writable ? '是' : '否'}</dd>
            </dl>
            <div style={{ marginTop: 14 }}>
              <PublicAddresses id={id} />
            </div>
          </Sheet>
        </div>
      </div>

      <NodeConfigPanel id={id} />

      <div className="node-grid">
        <NodeAccountPanel id={id} />
        <NodeStorage id={id} />
      </div>

      <Sheet
        title={`操作记录 ${nodeOperations.data?.items.length ?? ''}`.trim()}
        tools={
          <Button size="sm" variant="ghost" icon="arrow" onClick={() => operations.open()}>
            全部
          </Button>
        }
      >
        {nodeOperations.isPending ? <Skeleton height={90} /> : null}
        {nodeOperations.data?.items.length === 0 ? <Empty>没有操作记录</Empty> : null}
        <div className="queue-list">
          {nodeOperations.data?.items.slice(0, 6).map((operation) => (
            <Docket
              key={operation.id}
              operation={operation}
              nodeName={data?.name}
              onOpen={() => operations.open(operation.id)}
              dense
            />
          ))}
        </div>
      </Sheet>
    </div>
  );
}

function Reading({ label, value: reading, text, detail }: { label: string; value?: number; text: string; detail: string }) {
  return (
    <div className="stack" style={{ gap: 4 }}>
      <div className="spread">
        <span className="kicker">{label}</span>
        <span className="num" style={{ fontSize: 13 }}>{text}</span>
      </div>
      <Meter value={reading} levelHint={level(reading)} />
      <span className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>{detail}</span>
    </div>
  );
}

function familyState(family?: Family) {
  if (!family) return { state: 'unknown', label: '未知' };
  return { state: family.state === 'confirmed' ? 'confirmed' : family.state === 'pending' ? 'pending' : 'failed', label: family.state === 'confirmed' ? '已确认' : family.state === 'pending' ? '待确认' : family.state === 'conflict' ? '冲突' : family.state === 'failed' ? '失败' : '不可用' };
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

  return (
    <div className="stack gap-12">
      <div className="spread">
        <span className="kicker">公网地址</span>
        <span className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>修订 {query.data?.revision ?? '—'}</span>
      </div>
      {query.isPending ? <Skeleton height={60} /> : null}
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      {(['ipv4', 'ipv6'] as const).map((family) => {
        const value_ = family === 'ipv4' ? query.data?.ipv4 : query.data?.ipv6;
        const state = familyState(value_);
        const mode = family === 'ipv4' ? v4mode : v6mode;
        return (
          <div key={family} className="stack gap-6" style={{ paddingTop: 8, borderTop: '1px dashed var(--rule)' }}>
            <div className="spread">
              <span className="mono" style={{ fontSize: 10.5, letterSpacing: '0.1em', color: 'var(--ink-mute)' }}>
                {family.toUpperCase()}
              </span>
              <Stamp state={state.state}>{state.label}</Stamp>
            </div>
            <div className="row gap-6">
              <Select
                aria-label={`${family} 模式`}
                value={mode}
                onChange={(event) => (family === 'ipv4' ? setV4Mode(event.target.value as 'auto' | 'manual') : setV6Mode(event.target.value as 'auto' | 'manual'))}
                options={[['auto', '自动'], ['manual', '手工']]}
                style={{ height: 28, width: 88, fontSize: 12 }}
              />
              {mode === 'manual' ? (
                <input
                  className="input input--mono"
                  style={{ height: 28, fontSize: 11.5 }}
                  value={family === 'ipv4' ? v4 : v6}
                  onChange={(event) => (family === 'ipv4' ? setV4(event.target.value) : setV6(event.target.value))}
                  placeholder={family === 'ipv4' ? '203.0.113.7' : '2001:db8::7'}
                  aria-label={`${family} 地址`}
                />
              ) : (
                <span className="mono" style={{ fontSize: 11, color: 'var(--ink-soft)' }}>
                  {value_?.addresses.join('、') || '尚未确认'}
                </span>
              )}
            </div>
            {value_?.error ? <span style={{ fontSize: 11, color: 'var(--stamp)' }}>{value_.error}</span> : null}
            {mode === 'auto' && value_?.observations.length ? (
              <div className="addr-list">
                {value_.observations.slice(0, 3).map((observation) => (
                  <span key={observation.address} className="addr">
                    {observation.address} <span className="muted">· {observation.source}</span>
                  </span>
                ))}
              </div>
            ) : null}
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

function NodeStorage({ id }: { id: string }) {
  const query = useQuery({
    queryKey: ['storage', id],
    queryFn: async ({ signal }) => value(await api.GET('/nodes/{id}/storage', { signal, params: { path: { id } } })),
    retry: false,
  });
  return (
    <Sheet title="OPSD 数据">
      {query.isPending ? <Skeleton height={60} /> : null}
      {query.error ? <Empty>无法读取存储状态</Empty> : null}
      {query.data ? (
        <div className="stack gap-12">
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
              <div key={item.id} className="spread mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
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

function NodeConfigPanel({ id }: { id: string }) {
  const query = useQuery({
    queryKey: ['node-settings', id],
    queryFn: async ({ signal }) => value(await api.GET('/nodes/{id}/settings', { signal, params: { path: { id } } })),
    retry: false,
  });
  const [form, setForm] = useState<Schema['NodeConfig'] | null>(null);
  useEffect(() => setForm(query.data ?? null), [query.data]);
  const save = useMutation({
    mutationFn: async () => value(await api.PATCH('/nodes/{id}/settings', { params: { path: { id }, header: { 'Idempotency-Key': crypto.randomUUID() } }, body: form! })),
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
      {query.isPending ? <Skeleton height={120} /> : null}
      {query.error ? <Empty>无法读取</Empty> : null}
      {form ? (
        <div className="stack gap-16">
          <div className="form-grid">
            <TextInput label="节点名称" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            <TextInput label="地域" value={form.region} onChange={(event) => setForm({ ...form, region: event.target.value })} placeholder="留空表示未设置" />
            <TextInput label="节点通信监听" value={form.listen} onChange={(event) => setForm({ ...form, listen: event.target.value })} className="input--mono" />
            <TextInput label="控制台监听" value={form.console_listen} onChange={(event) => setForm({ ...form, console_listen: event.target.value })} className="input--mono" />
            <Select
              label="公网地址模式"
              value={form.address_mode ?? 'auto'}
              onChange={(event) => setForm({ ...form, address_mode: event.target.value as 'auto' | 'manual' })}
              options={[['auto', '自动'], ['manual', '手工']]}
            />
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
            <Select
              label="API"
              value={form.api ? 'on' : 'off'}
              onChange={(event) => setForm({ ...form, api: event.target.value === 'on' })}
              options={[['on', '开启'], ['off', '关闭']]}
            />
          </div>
          <div className="spread" style={{ paddingTop: 12, borderTop: '1px solid var(--rule-soft)' }}>
            <div className="stack gap-2">
              <span className="kicker">OPSD 数据（CouchDB）</span>
              <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
                {form.storage.enabled ? `启用 · ${form.storage.address}:${form.storage.port}` : '未启用，使用附近节点保存共享数据'}
              </span>
            </div>
            <Button
              size="sm"
              onClick={() => setForm({ ...form, storage: { ...form.storage, enabled: !form.storage.enabled } })}
            >
              {form.storage.enabled ? '停用' : '启用'}
            </Button>
          </div>
          <Failure error={save.error} />
          <Failure error={query.error} />
        </div>
      ) : null}
    </Sheet>
  );
}

function NodeAccountPanel({ id }: { id: string }) {
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

  if (query.error) {
    return (
      <Sheet title="本机账号">
        <Empty>无法读取</Empty>
      </Sheet>
    );
  }

  return (
    <Sheet
      title="本机账号"
      tools={
        <Button size="sm" variant="solid" busy={save.isPending} disabled={!dirty || !username} onClick={() => save.mutate()}>
          保存账号
        </Button>
      }
    >
      {query.isPending ? <Skeleton height={90} /> : null}
      {query.data ? (
        <div className="stack gap-14">
          <div className="form-grid">
            <TextInput label="账号" value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="off" />
            <TextInput label="登录入口" value={entrance} onChange={(event) => setEntrance(event.target.value)} className="input--mono" placeholder="/console" />
            <TextInput
              label="新密码"
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="new-password"
              className="field--full"
            />
          </div>
          <Failure error={save.error} />
        </div>
      ) : null}
    </Sheet>
  );
}

export { trendRange };




