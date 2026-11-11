import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useSearchParams } from 'react-router';
import { api, message, newKey, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { duration, stamp } from '../lib/format';
import { Button, Confirm, CopyLine, Dialog, Empty, Failure, Skeleton, Stamp, TextInput, useSession } from '../components/ui';
import { FleetList } from '../components/Fleet';
import { NodeWorkspace } from './NodeWorkspace';

export const nodesQuery = {
  queryKey: ['nodes'],
  queryFn: async ({ signal }: { signal: AbortSignal }) => value(await api.GET('/nodes', { signal })),
  staleTime: 8_000,
};

type Node = Schema['Node'];
type Token = Schema['EnrollmentToken'];
type Created = Schema['EnrollmentTokenCreated'];

function tokenState(token: Token) {
  if (token.revoked_at) return { state: 'revoked', label: '已撤销' };
  if (token.consumed_at) return { state: 'consumed', label: '已使用' };
  if (new Date(token.expires_at) < new Date()) return { state: 'expired', label: '已过期' };
  return { state: 'active', label: '有效' };
}

export function Nodes() {
  const session = useSession();
  const [params, setParams] = useSearchParams();
  const nodeId = params.get('node') || session.local_node_id;
  const [adding, setAdding] = useState(false);
  const [tokenList, setTokenList] = useState(false);
  const [renaming, setRenaming] = useState<Node | null>(null);
  const [revoking, setRevoking] = useState<Node | null>(null);

  const revoke = useMutation({
    mutationFn: async (node: Node) => value(await api.POST('/nodes/{id}/revoke', { params: { path: { id: node.id } } })),
    onSuccess: () => {
      setRevoking(null);
      void queryClient.invalidateQueries({ queryKey: ['nodes'] });
      void queryClient.invalidateQueries({ queryKey: ['overview'] });
    },
  });

  return (
    <div className="page">
      <div className="split">
        <FleetList
          selected={nodeId}
          onSelect={(id) => {
            const next = new URLSearchParams(params);
            next.set('node', id);
            setParams(next, { replace: true });
          }}
        />
        <NodeWorkspace
          id={nodeId}
          tools={
            <>
              <Button size="sm" icon="key" onClick={() => setTokenList(true)}>
                接入码
              </Button>
              <Button size="sm" variant="solid" icon="plus" onClick={() => setAdding(true)}>
                添加节点
              </Button>
            </>
          }
          onRename={(node) => setRenaming(node)}
          onRevoke={(node) => setRevoking(node)}
        />
      </div>

      <AddNode open={adding} onClose={() => setAdding(false)} onDone={() => void queryClient.invalidateQueries({ queryKey: ['nodes'] })} />
      <TokenList open={tokenList} onClose={() => setTokenList(false)} />
      <RenameNode
        node={renaming}
        onClose={() => setRenaming(null)}
        onDone={() => {
          setRenaming(null);
          void queryClient.invalidateQueries({ queryKey: ['nodes'] });
        }}
      />
      <Confirm
        open={Boolean(revoking)}
        title={`撤销 ${revoking?.name ?? ''}？`}
        description={
          <>
            撤销后该节点的新请求和后续密码授权都会被拒绝，并移除数据库访问。节点离线时，只有恢复通信后才会得知撤销；已经获得的旧密码无法收回。
          </>
        }
        confirmLabel="撤销节点"
        dangerous
        busy={revoke.isPending}
        error={revoke.error}
        onCancel={() => setRevoking(null)}
        onConfirm={() => revoking && revoke.mutate(revoking)}
      />
    </div>
  );
}

function RenameNode({ node, onClose, onDone }: { node: Node | null; onClose: () => void; onDone: () => void }) {
  const [name, setName] = useState('');
  useEffect(() => setName(node?.name ?? ''), [node]);
  const mutation = useMutation({
    mutationFn: async () =>
      value(await api.PATCH('/nodes/{id}', { params: { path: { id: node!.id } }, body: { name } })),
    onSuccess: onDone,
  });
  return (
    <Dialog
      open={Boolean(node)}
      title="重命名节点"

      width="narrow"
      onClose={onClose}
      footer={
        <>
          <span className="grow" />
          <Button onClick={onClose} disabled={mutation.isPending}>取消</Button>
          <Button variant="solid" busy={mutation.isPending} onClick={() => mutation.mutate()} disabled={!name.trim() || name === node?.name}>
            保存名称
          </Button>
        </>
      }
    >
      <TextInput label="节点名称" value={name} onChange={(event) => setName(event.target.value)} placeholder="例如 sh-01" />
      <Failure error={mutation.error} />
    </Dialog>
  );
}

function AddNode({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: () => void }) {
  const [name, setName] = useState('');
  const [region, setRegion] = useState('');
  const [shell, setShell] = useState<'curl' | 'wget'>('curl');
  const [created, setCreated] = useState<Created | null>(null);
  const create = useMutation({
    mutationFn: async () => value(await api.POST('/enrollment-tokens', { params: { header: { 'X-CSRF-Token': '' } } })),
    onSuccess: (data) => {
      setCreated(data);
      onDone();
    },
  });
  useEffect(() => {
    if (!open) {
      setCreated(null);
      setName('');
      setRegion('');
      create.reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const flags = created
    ? `-e ${created.peer_address} -k ${created.peer_public_key} -t ${created.token}${name ? ` -n ${name}` : ''}${region ? ` -r ${region}` : ''}`
    : '';
  const command = created
    ? shell === 'curl'
      ? `curl -fsSL ${created.installer_url} | sudo sh -s -- ${flags}`
      : `wget -qO- ${created.installer_url} | sudo sh -s -- ${flags}`
    : '';

  return (
    <Dialog
      open={open}
      title={created ? '在目标机器上执行' : '添加节点'}
      width="wide"
      onClose={onClose}
      footer={
        <>
          <span className="grow mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
            {created ? `接入地址 ${created.peer_address} · 版本 ${created.version}` : ''}
          </span>
          {created ? (
            <Button onClick={onClose}>完成</Button>
          ) : (
            <>
              <Button onClick={onClose}>取消</Button>
              <Button variant="solid" icon="key" busy={create.isPending} onClick={() => create.mutate()}>
                生成安装命令
              </Button>
            </>
          )}
        </>
      }
    >
      {created ? (
        <div className="stack gap-14">
          <div className="segmented" style={{ alignSelf: 'flex-start' }}>
            {(['curl', 'wget'] as const).map((item) => (
              <button key={item} type="button" aria-pressed={shell === item} onClick={() => setShell(item)}>
                {item}
              </button>
            ))}
          </div>
          <CopyLine value={command} label="复制命令" />
          <dl className="facts">
            <dt>接入码</dt>
            <dd className="mono">{created.token}</dd>
            <dt>有效期</dt>
            <dd className="mono">至 {stamp(created.record.expires_at)}</dd>
            <dt>名称</dt>
            <dd>{name || '使用目标机器主机名'}</dd>
            <dt>地域</dt>
            <dd>{region || '保持未设置'}</dd>
          </dl>
          <p style={{ fontSize: 12, color: 'var(--ink-mute)', lineHeight: 1.7 }}>
            接入码成功加入即被消费，重复执行不会覆盖已有身份。
          </p>
        </div>
      ) : (
        <div className="stack gap-14">
          <div className="form-grid">
            <TextInput label="节点名称" value={name} onChange={(event) => setName(event.target.value)} placeholder="留空使用主机名" />
            <TextInput label="地域" value={region} onChange={(event) => setRegion(event.target.value)} placeholder="例如 香港" />
          </div>
          <p style={{ fontSize: 12, color: 'var(--ink-mute)', lineHeight: 1.7 }}>
            生成命令时才创建接入码。
          </p>
          <Failure error={create.error} />
        </div>
      )}
    </Dialog>
  );
}

function TokenList({ open, onClose }: { open: boolean; onClose: () => void }) {
  const session = useSession();
  const query = useQuery({
    queryKey: ['enrollment-tokens'],
    enabled: open,
    queryFn: async ({ signal }) => value(await api.GET('/enrollment-tokens', { signal })),
  });
  const revoke = useMutation({
    mutationFn: async (id: string) =>
      value(await api.DELETE('/enrollment-tokens/{id}', { params: { path: { id }, header: { 'X-CSRF-Token': session.csrf_token } } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['enrollment-tokens'] }),
  });
  return (
    <Dialog open={open} title="接入码" onClose={onClose} width="wide">
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      {query.isPending ? <Skeleton height={80} /> : null}
      {query.data?.items.length === 0 ? <Empty>没有待使用的接入码</Empty> : null}
      {query.data?.items.length ? (
        <table className="table tiny-table">
          <thead>
            <tr>
              <th scope="col">接入码</th>
              <th scope="col">生成</th>
              <th scope="col">到期</th>
              <th scope="col">状态</th>
              <th scope="col" className="right">操作</th>
            </tr>
          </thead>
          <tbody>
            {query.data.items.map((token) => {
              const state = tokenState(token);
              return (
                <tr key={token.id}>
                  <td className="mono" style={{ fontSize: 11 }}>{token.id}</td>
                  <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{stamp(token.created_at)}</td>
                  <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
                    {state.state === 'active' ? `剩 ${duration((new Date(token.expires_at).getTime() - Date.now()) / 1000)}` : stamp(token.expires_at)}
                  </td>
                  <td><Stamp state={state.state === 'active' ? 'pending' : 'disabled'}>{state.label}</Stamp></td>
                  <td className="right">
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={state.state !== 'active' || revoke.isPending}
                      onClick={() => revoke.mutate(token.id)}
                    >
                      撤销
                    </Button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      ) : null}
      <Failure error={revoke.error} />
    </Dialog>
  );
}

export { message, newKey };



