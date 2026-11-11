import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { api, value } from '../api/client';
import { queryClient } from '../state';
import { ago } from '../lib/format';
import { firewallNames, managedFirewallStates, peerTrustStates } from '../lib/labels';
import {
  Button,
  Confirm,
  Empty,
  Failure,
  Notice,
  PageHead,
  Select,
  Sheet,
  Skeleton,
  Stamp,
} from '../components/ui';

export function FirewallSettings() {
  const [params] = useSearchParams();
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const nodeList = nodes.data?.items.filter((node) => !node.revoked_at) ?? [];
  const nodeId = params.get('node') || nodeList.find((node) => node.connected)?.id || '';
  const nodeName = nodeList.find((node) => node.id === nodeId)?.name ?? '节点';

  const firewall = useQuery({
    queryKey: ['firewall', nodeId],
    enabled: Boolean(nodeId),
    queryFn: async ({ signal }) => value(await api.GET('/nodes/{id}/firewall', { signal, params: { path: { id: nodeId } } })),
  });
  const trust = useQuery({
    queryKey: ['peer-trust', 'status'],
    queryFn: async ({ signal }) => value(await api.GET('/peer-trust/status', { signal })),
  });

  const [confirmTrust, setConfirmTrust] = useState(false);
  const install = useMutation({
    mutationFn: async () =>
      value(
        await api.POST('/nodes/{id}/firewall/install', {
          params: { path: { id: nodeId }, header: { 'Idempotency-Key': crypto.randomUUID() } },
          body: { backend: 'ufw' },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['firewall', nodeId] });
      void queryClient.invalidateQueries({ queryKey: ['operations'] });
    },
  });
  const toggleTrust = useMutation({
    mutationFn: async (enabled: boolean) =>
      value(
        await api.PATCH('/peer-trust', {
          params: {},
          body: { expected_version: trust.data!.policy.version, enabled },
        }),
      ),
    onSuccess: () => {
      setConfirmTrust(false);
      void queryClient.invalidateQueries({ queryKey: ['peer-trust'] });
      void queryClient.invalidateQueries({ queryKey: ['firewall'] });
    },
  });

  const managed = firewall.data;
  const dockerCapability = managed?.capabilities.docker_ports;

  return (
    <div className="page">
      <PageHead
        title="防火墙设置"
        tools={
          <Link to={`/firewall?node=${nodeId}`}>
            <Button size="sm" icon="arrow">返回规则</Button>
          </Link>
        }
      />

      <div className="node-grid">
        <Sheet
          title="防火墙组件"
          tools={
            <>
              <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
                {nodeName}
              </span>
              {managed ? (
                <Stamp state={managed.state === 'in_sync' ? 'in_sync' : managed.state}>
                  {managedFirewallStates[managed.state]}
                </Stamp>
              ) : null}
            </>
          }
        >
          {firewall.isPending ? <Skeleton height={80} /> : null}
          <Failure error={firewall.error} retry={() => void firewall.refetch()} busy={firewall.isFetching} />
          {managed ? (
            <div className="stack gap-14">
              <dl className="facts">
                <dt>后端</dt>
                <dd className="mono">{firewallNames[managed.backend]}</dd>
                <dt>服务</dt>
                <dd>{managed.service_active ? '已启用' : '已停用'}</dd>
                <dt>配置修订</dt>
                <dd className="mono">{managed.revision} · 已确认 {managed.confirmed_revision}</dd>
                <dt>检查</dt>
                <dd className="mono">{ago(managed.checked_at)}</dd>
              </dl>
              <div className="spread" style={{ paddingTop: 12, borderTop: '1px solid var(--rule-soft)' }}>
                <div className="stack gap-2">
                  <span className="kicker">Docker 发布端口策略</span>
                  <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
                    {dockerCapability?.supported ? '当前后端支持' : dockerCapability?.reason || '尚未支持'}
                  </span>
                </div>
                <Stamp state={dockerCapability?.supported ? 'enabled' : 'unsupported'}>
                  {dockerCapability?.supported ? '可用' : '未启用'}
                </Stamp>
              </div>
              {!managed.service_active || managed.backend === 'none' ? (
                <div className="stack gap-8">
                  <Notice>安装不会自动启用防火墙。</Notice>
                  <Button size="sm" icon="plus" busy={install.isPending} onClick={() => install.mutate()}>
                    安装 UFW 组件
                  </Button>
                  <Failure error={install.error} />
                </div>
              ) : null}
            </div>
          ) : null}
        </Sheet>

        <Sheet title="集群互信">
          {trust.isPending ? <Skeleton height={100} /> : null}
          <Failure error={trust.error} retry={() => void trust.refetch()} busy={trust.isFetching} />
          {trust.data ? (
            <div className="stack gap-14">
              <div className="spread">
                <div className="stack gap-2">
                  <span className="kicker">互信开关</span>
                  <span style={{ fontSize: 12, color: 'var(--ink-soft)' }}>
                    {trust.data.policy.enabled ? '已开启' : '已关闭'}
                  </span>
                </div>
                <Button
                  size="sm"
                  variant={trust.data.policy.enabled ? 'line' : 'solid'}
                  onClick={() => (trust.data.policy.enabled ? setConfirmTrust(true) : toggleTrust.mutate(true))}
                  busy={toggleTrust.isPending}
                >
                  {trust.data.policy.enabled ? '关闭互信' : '开启互信'}
                </Button>
              </div>
              <Notice>
                互信会放行成员的主机入站端口，含数据库端口。
              </Notice>
              <div className="table-wrap">
                <table className="table tiny-table">
                  <thead>
                    <tr>
                      <th scope="col">节点</th>
                      <th scope="col">状态</th>
                      <th scope="col" className="right">规则</th>
                    </tr>
                  </thead>
                  <tbody>
                    {trust.data.nodes.map((node) => (
                      <tr key={node.node_id}>
                        <td className="mono" style={{ fontSize: 11 }}>{nodeList.find((item) => item.id === node.node_id)?.name ?? node.node_id}</td>
                        <td><Stamp state={node.state === 'in_sync' ? 'in_sync' : node.state}>{peerTrustStates[node.state]}</Stamp></td>
                        <td className="right mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
                          {node.rules.length} 条 · 修订 {node.applied_revision}/{node.target_revision}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {trust.data.nodes.length === 0 ? <Empty>还没有成员</Empty> : null}
              <Failure error={toggleTrust.error} />
            </div>
          ) : null}
        </Sheet>
      </div>

      <Confirm
        open={confirmTrust}
        title="关闭集群互信？"
        description="关闭后成员之间的入站放行与互信规则会被撤除。"
        confirmLabel="关闭互信"
        dangerous
        busy={toggleTrust.isPending}
        error={toggleTrust.error}
        onCancel={() => setConfirmTrust(false)}
        onConfirm={() => toggleTrust.mutate(false)}
      />
    </div>
  );
}

export { Select };


