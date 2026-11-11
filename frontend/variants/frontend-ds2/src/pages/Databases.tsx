import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { api, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { ago } from '../lib/format';
import { databaseStates } from '../lib/labels';
import {
  Button,
  Dialog,
  Empty,
  Failure,
  Led,
  PageHead,
  Select,
  Sheet,
  Skeleton,
  Stamp,
  TextInput,
} from '../components/ui';

export const clustersQuery = {
  queryKey: ['databases', 'clusters'],
  queryFn: async ({ signal }: { signal: AbortSignal }) => value(await api.GET('/databases/clusters', { signal })),
  refetchInterval: 15_000,
};

export function Databases() {
  const query = useQuery(clustersQuery);
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const [onboarding, setOnboarding] = useState(false);
  const names = new Map(nodes.data?.items.map((node) => [node.id, node.name]) ?? []);
  const items = query.data?.items ?? [];

  return (
    <div className="page">
      <PageHead
        title="数据库"
        meta={[<>集群 {String(items.length).padStart(2, '0')} 个</>]}
        tools={
          <>
            <Button size="sm" icon="refresh" busy={query.isFetching} onClick={() => void query.refetch()}>
              重新读取
            </Button>
            <Button size="sm" variant="solid" icon="plus" onClick={() => setOnboarding(true)}>
              接入集群
            </Button>
          </>
        }
      />

      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />

      {query.isPending ? (
        <div className="cluster-grid">
          {Array.from({ length: 2 }, (_, index) => (
            <div key={index} className="cluster-card">
              <Skeleton height={16} width={140} />
              <Skeleton height={12} width={220} />
              <Skeleton height={12} width={180} />
            </div>
          ))}
        </div>
      ) : null}

      {!query.isPending && items.length === 0 ? (
        <Sheet>
          <Empty>还没有登记数据库集群</Empty>
        </Sheet>
      ) : null}

      <div className="cluster-grid">
        {items.map((item) => {
          const members = item.cluster.members.length;
          return (
            <Link key={item.cluster.id} to={`/databases/${item.cluster.id}`} className="cluster-card">
              <div className="spread">
                <h3>{item.cluster.name}</h3>
                <Stamp state="ready">已登记</Stamp>
              </div>
              <div className="row gap-10 mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
                <span>集群 {item.cluster.cluster_id}</span>
                <span>· 协调 {names.get(item.cluster.coordinator) ?? item.cluster.coordinator}</span>
              </div>
              <div className="cluster-card__members">
                {item.cluster.members.map((member) => (
                  <span key={`${member.node_id}-${member.instance_id}`} className="db-member">
                    <Led state="on" />
                    {member.name || member.instance_id}
                  </span>
                ))}
              </div>
              <div className="spread mono" style={{ fontSize: 10, color: 'var(--ink-mute)', paddingTop: 8, borderTop: '1px solid var(--rule-soft)' }}>
                <span>{members} 个成员 · {item.cluster.proxies.length} 个代理</span>
                <span>读取 {item.checked_at ? ago(item.checked_at) : '—'}</span>
              </div>
            </Link>
          );
        })}
      </div>


      <Onboarding open={onboarding} onClose={() => setOnboarding(false)} />
    </div>
  );
}

function Onboarding({ open, onClose }: { open: boolean; onClose: () => void }) {
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const nodeList = (nodes.data?.items ?? []).filter((node) => !node.revoked_at);
  const [nodeId, setNodeId] = useState('');
  const [name, setName] = useState('');
  const [clusterId, setClusterId] = useState('');
  const [workbench, setWorkbench] = useState('');
  const [picked, setPicked] = useState<string[]>([]);

  const discovered = useQuery({
    queryKey: ['databases', 'discover', nodeId],
    enabled: open && Boolean(nodeId),
    queryFn: async ({ signal }) =>
      value(await api.POST('/nodes/{id}/databases/discover', { params: { path: { id: nodeId } }, signal })),
    retry: false,
  });

  const found = discovered.data?.items ?? [];
  const create = useMutation({
    mutationFn: async () =>
      value(
        await api.POST('/databases/clusters', {
          body: {
            name,
            coordinator: nodeId,
            cluster_id: clusterId || name,
            revision: 0,
            members: found.filter((item) => picked.includes(item.id) && item.kind === 'galera').map(toRef),
            proxies: found.filter((item) => picked.includes(item.id) && item.kind === 'proxysql').map(toRef),
            workbench_url: workbench,
          },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: clustersQuery.queryKey });
      onClose();
    },
  });

  return (
    <Dialog
      open={open}
      title="接入数据库集群"
     
      width="wide"
      onClose={onClose}
      footer={
        <>
          <span className="grow mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
            
          </span>
          <Button onClick={onClose} disabled={create.isPending}>取消</Button>
          <Button
            variant="solid"
            busy={create.isPending}
            disabled={!name || !nodeId || !picked.length}
            onClick={() => create.mutate()}
          >
            保存并登记
          </Button>
        </>
      }
    >
      <div className="form-grid">
        <Select
          label="协调节点"
          value={nodeId}
          onChange={(event) => {
            setNodeId(event.target.value);
            setPicked([]);
          }}
          options={[['', '选择节点'], ...nodeList.map((node) => [node.id, `${node.name}${node.connected ? '' : ' · 离线'}`] as const)]}
        />
        <TextInput label="集群名称" value={name} onChange={(event) => setName(event.target.value)} placeholder="例如 上海主库" />
        <TextInput label="集群标识" value={clusterId} onChange={(event) => setClusterId(event.target.value)} className="input--mono" placeholder="留空使用名称" />
        <TextInput label="外部工作台地址" value={workbench} onChange={(event) => setWorkbench(event.target.value)} className="input--mono" placeholder="https://…" />
      </div>

      <div className="stack gap-8">
        <div className="spread">
          <span className="kicker">本机实例</span>
          {nodeId ? (
            <Button size="sm" icon="search" busy={discovered.isFetching} onClick={() => void discovered.refetch()}>
              重新发现
            </Button>
          ) : null}
        </div>
        {!nodeId ? <span style={{ fontSize: 12, color: 'var(--ink-mute)' }}>先选择协调节点</span> : null}
        {discovered.isPending && nodeId ? <Skeleton height={60} /> : null}
        <Failure error={discovered.error} />
        {found.length ? (
          <div className="stack gap-6">
            {found.map((instance) => (
              <label key={instance.id} className="order-row" style={{ cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={picked.includes(instance.id)}
                  onChange={(event) =>
                    setPicked(event.target.checked ? [...picked, instance.id] : picked.filter((id) => id !== instance.id))
                  }
                />
                <span className="grow">
                  <b style={{ fontWeight: 500 }}>{instance.name}</b>
                  <span className="mono" style={{ marginLeft: 8, fontSize: 10.5, color: 'var(--ink-mute)' }}>
                    {instance.kind === 'galera' ? 'Galera 成员' : 'ProxySQL 代理'} · {instance.address}
                  </span>
                </span>
                <Led state={instance.running ? 'on' : 'off'} />
              </label>
            ))}
          </div>
        ) : null}
        {nodeId && !discovered.isPending && found.length === 0 && !discovered.error ? (
          <Empty>没有可用实例</Empty>
        ) : null}
      </div>

      
      <Failure error={create.error} />
    </Dialog>
  );
}

function toRef(item: Schema['DiscoveredDatabaseInstance']): Schema['DatabaseRef'] {
  return { node_id: item.node_id, instance_id: item.id, name: item.name, address: item.address };
}

export { databaseStates };




