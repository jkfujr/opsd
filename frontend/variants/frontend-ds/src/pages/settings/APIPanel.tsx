import { useMemo, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, value, type Schema } from '../../api/client';
import { queryClient } from '../../state';
import { stamp } from '../../lib/format';
import {
  Button,
  Confirm,
  CopyLine,
  Dialog,
  Empty,
  Failure,
  Notice,
  Select,
  Sheet,
  Skeleton,
  Stamp,
  TextInput,
  useSession,
} from '../../components/ui';

type Grant = Schema['APIGrant'];
type Token = Schema['APIToken'];

const modules: { id: Grant['module']; label: string }[] = [
  { id: 'monitoring', label: '监控' },
  { id: 'nodes', label: '节点' },
  { id: 'containers', label: '容器' },
  { id: 'firewall', label: '防火墙' },
  { id: 'databases', label: '数据库' },
];

const templates: { id: string; label: string; grants: Grant[] }[] = [
  { id: 'board', label: '监控看板', grants: [{ module: 'monitoring', level: 'read', scope: 'all', resources: [] }] },
  {
    id: 'ro',
    label: '全部只读',
    grants: modules.map((module) => ({ module: module.id, level: 'read', scope: 'all', resources: [] })),
  },
  {
    id: 'containers',
    label: '容器管理',
    grants: [
      { module: 'containers', level: 'manage', scope: 'all', resources: [] },
      { module: 'nodes', level: 'read', scope: 'all', resources: [] },
    ],
  },
  {
    id: 'databases',
    label: '数据库管理',
    grants: [
      { module: 'databases', level: 'manage', scope: 'all', resources: [] },
      { module: 'monitoring', level: 'read', scope: 'all', resources: [] },
    ],
  },
];

export function APIPanel() {
  const session = useSession();
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<Schema['APITokenCreated'] | null>(null);
  const [deleting, setDeleting] = useState<Token | null>(null);
  const [regenerating, setRegenerating] = useState<Token | null>(null);

  const query = useQuery({
    queryKey: ['api-tokens', session.local_node_id],
    queryFn: async ({ signal }) => value(await api.GET('/api-tokens', { signal, params: { query: { node_id: session.local_node_id } } })),
  });
  const patch = useMutation({
    mutationFn: async ({ token, enabled }: { token: Token; enabled: boolean }) =>
      value(
        await api.PATCH('/api-tokens/{id}', {
          params: { path: { id: token.id }, query: { node_id: session.local_node_id } },
          body: { name: token.name, enabled, revision: token.revision, grants: token.grants, expires_at: token.expires_at, source_ips: token.source_ips },
        }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['api-tokens', session.local_node_id] }),
  });
  const remove = useMutation({
    mutationFn: async (token: Token) =>
      value(await api.DELETE('/api-tokens/{id}', { params: { path: { id: token.id }, query: { node_id: session.local_node_id, revision: token.revision } } })),
    onSuccess: () => {
      setDeleting(null);
      void queryClient.invalidateQueries({ queryKey: ['api-tokens', session.local_node_id] });
    },
  });
  const regenerate = useMutation({
    mutationFn: async (token: Token) =>
      value(await api.POST('/api-tokens/{id}/regenerate', { params: { path: { id: token.id }, query: { node_id: session.local_node_id } }, body: { revision: token.revision } })),
    onSuccess: (data) => {
      setRegenerating(null);
      setCreated(data);
      void queryClient.invalidateQueries({ queryKey: ['api-tokens', session.local_node_id] });
    },
  });

  const items = query.data?.items ?? [];

  return (
    <Sheet
      title="API"

      tools={
        <>
          <Button size="sm" icon="refresh" busy={query.isFetching} onClick={() => void query.refetch()}>刷新</Button>
          <Button size="sm" variant="solid" icon="plus" onClick={() => setCreating(true)}>生成令牌</Button>
        </>
      }
    >
      <div className="stack gap-16">
        <Notice tone="info">API 开关在「节点详情 → 本机配置」。</Notice>
        <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
        {query.isPending ? <Skeleton height={120} /> : null}
        {!query.isPending && items.length === 0 ? <Empty>还没有 API 令牌</Empty> : null}
        {items.length ? (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">名称</th>
                  <th scope="col">权限</th>
                  <th scope="col">来源</th>
                  <th scope="col">创建</th>
                  <th scope="col">状态</th>
                  <th scope="col" className="right">操作</th>
                </tr>
              </thead>
              <tbody>
                {items.map((token) => (
                  <tr key={token.id}>
                    <td>
                      <div className="table__name">{token.name}</div>
                      <div className="cell-sub">{token.id}</div>
                    </td>
                    <td style={{ fontSize: 11, color: 'var(--ink-soft)', maxWidth: 300, whiteSpace: 'normal' }}>
                      {token.grants.map((grant) => `${modules.find((module) => module.id === grant.module)?.label ?? grant.module}·${grant.level === 'read' ? '只读' : '管理'}`).join(' / ')}
                    </td>
                    <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{token.source_ips.length ? token.source_ips.join('、') : '任意'}</td>
                    <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{stamp(token.created_at)}</td>
                    <td><Stamp state={token.enabled ? 'enabled' : 'disabled'}>{token.enabled ? '启用' : '停用'}</Stamp></td>
                    <td className="right">
                      <span className="row gap-2" style={{ justifyContent: 'flex-end' }}>
                        <Button size="sm" variant="ghost" disabled={patch.isPending} onClick={() => patch.mutate({ token, enabled: !token.enabled })}>
                          {token.enabled ? '停用' : '启用'}
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => setRegenerating(token)}>重新生成</Button>
                        <Button size="sm" variant="ghost" onClick={() => setDeleting(token)}>删除</Button>
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        <Failure error={patch.error} />
        <Failure error={regenerate.error} />
      </div>

      <CreateToken
        open={creating}
        nodeId={session.local_node_id}
        onClose={() => setCreating(false)}
        onCreated={(data) => {
          setCreating(false);
          setCreated(data);
          void queryClient.invalidateQueries({ queryKey: ['api-tokens', session.local_node_id] });
        }}
      />

      <CreatedToken data={created} onClose={() => setCreated(null)} />

      <Confirm
        open={Boolean(deleting)}
        title={`删除令牌 ${deleting?.name ?? ''}？`}
        description="使用该令牌的采集器会立即失败。"
        confirmLabel="删除令牌"
        dangerous
        busy={remove.isPending}
        error={remove.error}
        onCancel={() => setDeleting(null)}
        onConfirm={() => deleting && remove.mutate(deleting)}
      />
      <Confirm
        open={Boolean(regenerating)}
        title={`重新生成 ${regenerating?.name ?? ''}？`}
        description="旧令牌立即失效，新令牌只显示一次。"
        confirmLabel="重新生成"
        busy={regenerate.isPending}
        error={regenerate.error}
        onCancel={() => setRegenerating(null)}
        onConfirm={() => regenerating && regenerate.mutate(regenerating)}
      />
    </Sheet>
  );
}

function CreateToken({ open, nodeId, onClose, onCreated }: { open: boolean; nodeId: string; onClose: () => void; onCreated: (data: Schema['APITokenCreated']) => void }) {
  const [template, setTemplate] = useState(templates[0].id);
  const [name, setName] = useState('');
  const [level, setLevel] = useState<'read' | 'manage'>('read');
  const [expires, setExpires] = useState('');
  const [source, setSource] = useState('');
  const preset = templates.find((item) => item.id === template);
  const grants = useMemo<Grant[]>(
    () =>
      template === 'custom'
        ? modules.map((module) => ({ module: module.id, level, scope: 'all' as const, resources: [] }))
        : (preset?.grants ?? []),
    [template, level, preset],
  );

  const create = useMutation({
    mutationFn: async () =>
      value(
        await api.POST('/api-tokens', {
          params: { query: { node_id: nodeId } },
          body: {
            name: name || preset?.label || '令牌',
            enabled: true,
            revision: 0,
            grants,
            expires_at: expires ? new Date(expires).toISOString() : undefined,
            source_ips: source ? source.split(/[\s,]+/).filter(Boolean) : [],
          },
        }),
      ),
    onSuccess: onCreated,
  });

  return (
    <Dialog
      open={open}
      title="生成 API 令牌"

      onClose={onClose}
      footer={
        <>
          <span className="grow mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>完整令牌只显示一次</span>
          <Button onClick={onClose} disabled={create.isPending}>取消</Button>
          <Button variant="solid" busy={create.isPending} onClick={() => create.mutate()}>生成令牌</Button>
        </>
      }
    >
      <div className="form-grid">
        <Select
          label="模板"
          value={template}
          onChange={(event) => setTemplate(event.target.value)}
          options={[...templates.map((item) => [item.id, item.label] as const), ['custom', '自定义']]}
        />
        <TextInput label="名称" value={name} onChange={(event) => setName(event.target.value)} placeholder={preset?.label ?? '令牌'} />
        {template === 'custom' ? (
          <Select label="级别" value={level} onChange={(event) => setLevel(event.target.value as 'read' | 'manage')} options={[['read', '只读'], ['manage', '管理']]} />
        ) : null}
        <TextInput label="有效期（可选）" type="date" value={expires} onChange={(event) => setExpires(event.target.value)} />
        <TextInput label="来源 IP（可选）" value={source} onChange={(event) => setSource(event.target.value)} className="input--mono" placeholder="10.8.0.0/24" />
      </div>
      <div className="stack gap-6">
        <span className="kicker">将授予</span>
        <div className="default-row">
          {grants.map((grant) => (
            <span key={grant.module} className="default-chip">
              <b>{modules.find((module) => module.id === grant.module)?.label ?? grant.module}</b>
              {grant.level === 'read' ? '只读' : '管理'} · 全部
            </span>
          ))}
        </div>
      </div>
      <Failure error={create.error} />
    </Dialog>
  );
}

function CreatedToken({ data, onClose }: { data: Schema['APITokenCreated'] | null; onClose: () => void }) {
  const origin = window.location.origin;
  return (
    <Dialog
      open={Boolean(data)}
      title="令牌已生成"

      width="wide"
      onClose={onClose}
      footer={
        <>
          <span className="grow mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>关闭后无法再次查看完整值</span>
          <Button variant="solid" onClick={onClose}>已保存</Button>
        </>
      }
    >
      {data ? (
        <div className="token-created">
          <CopyLine value={data.value} label="复制令牌" />
          <CopyLine value={`curl -fsSL -H "Authorization: Bearer ${data.token}" ${origin}/api/v1/overview`} label="复制验证命令" />
          <CopyLine
            value={`scrape_configs:\n  - job_name: opsd\n    authorization:\n      credentials: ${data.token}\n    metrics_path: /api/v1/metrics\n    static_configs:\n      - targets: ["${window.location.hostname}:${window.location.port}"]`}
            label="复制 Prometheus 配置"
          />
        </div>
      ) : null}
      
    </Dialog>
  );
}

