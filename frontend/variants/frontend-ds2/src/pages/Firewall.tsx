import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router';
import { api, idempotency, value, type Schema } from '../api/client';
import { queryClient } from '../state';
import { ago, clock, stamp } from '../lib/format';
import { firewallNames, managedFirewallStates, peerTrustStates, tone } from '../lib/labels';
import {
  Button,
  Confirm,
  Dialog,
  Empty,
  Failure,
  Notice,
  PageHead,
  Select,
  Sheet,
  Skeleton,
  Stamp,
  Tabs,
  TextInput,
} from '../components/ui';

type Rule = Schema['ManagedRule'];
type Change = Schema['ManagedFirewallChange'];
type Action = 'allow' | 'deny' | 'reject';

const actionLabels: Record<Action, string> = { allow: '允许', deny: '丢弃', reject: '拒绝' };
const protocolLabels: Record<string, string> = { any: '全部', tcp: 'TCP', udp: 'UDP', icmp: 'ICMP', icmpv6: 'ICMPv6' };

const blankRule: Schema['RuleSpec'] = { family: 'ipv4', source: '', destination: '', protocol: 'tcp', port: '', action: 'allow', comment: '' };

export function Firewall() {
  const [params, setParams] = useSearchParams();
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const nodeList = nodes.data?.items.filter((node) => !node.revoked_at) ?? [];
  const nodeId = params.get('node') || nodeList.find((node) => node.connected)?.id || '';
  const [family, setFamily] = useState<'all' | 'ipv4' | 'ipv6'>('all');
  const [tab, setTab] = useState<'port' | 'ip' | 'trust'>('port');
  const [editing, setEditing] = useState<{ rule?: Rule; draft: Schema['RuleSpec'] } | null>(null);
  const [deleting, setDeleting] = useState<Rule | null>(null);
  const [policyOpen, setPolicyOpen] = useState(false);
  const [saved, setSaved] = useState<string>('');

  const state = useQuery({
    queryKey: ['firewall', nodeId],
    enabled: Boolean(nodeId),
    queryFn: async ({ signal }) => value(await api.GET('/nodes/{id}/firewall', { signal, params: { path: { id: nodeId } } })),
    refetchInterval: 10_000,
  });

  const change = useMutation({
    mutationFn: async (body: Change) =>
      value(
        await api.PATCH('/nodes/{id}/firewall', {
          params: { path: { id: nodeId }, header: idempotency() },
          body: { expected_revision: Math.max(state.data?.revision ?? 1, 1), change: body },
        }),
      ),
    onSuccess: (data) => {
      setEditing(null);
      setDeleting(null);
      setPolicyOpen(false);
      setSaved(data.operation_id);
      void queryClient.invalidateQueries({ queryKey: ['firewall', nodeId] });
      void queryClient.invalidateQueries({ queryKey: ['operations'] });
    },
  });

  const rules = state.data?.rules ?? [];
  const visible = rules.filter((rule) => family === 'all' || rule.spec.family === family);
  const portRules = visible.filter((rule) => Boolean(rule.spec.port));
  const ipRules = visible.filter((rule) => !rule.spec.port);
  const list = tab === 'port' ? portRules : tab === 'ip' ? ipRules : [];
  const backend = firewallNames[state.data?.backend ?? 'unknown'];

  return (
    <div className="page">
      <PageHead
        title="防火墙"
        meta={[
          <span key="n">{nodeList.find((node) => node.id === nodeId)?.name ?? '选择节点'}</span>,
          <span key="b" className="mono">{backend}</span>,
          state.data ? (
            <Stamp key="s" state={state.data.state === 'in_sync' ? 'in_sync' : state.data.state}>
              {managedFirewallStates[state.data.state]}
            </Stamp>
          ) : null,
          state.data ? <span key="t">检查 {clock(state.data.checked_at)}</span> : null,
        ]}
        tools={
          <>
            <Button size="sm" icon="refresh" busy={state.isFetching} onClick={() => void state.refetch()}>
              重新读取
            </Button>
            <Link to="/firewall/settings">
              <Button size="sm" icon="settings">
                互信与组件
              </Button>
            </Link>
          </>
        }
      />

      <Failure error={state.error} retry={() => void state.refetch()} busy={state.isFetching} />
      {saved ? (
        <Notice tone="info">
          已受理 <span className="mono">{saved}</span>
          <Link to="/" style={{ marginLeft: 8, textDecoration: 'underline' }}>
            查看队列
          </Link>
        </Notice>
      ) : null}
      {state.data?.error ? <Notice tone="bad">{state.data.error.message}</Notice> : null}

      <Sheet className="firewall">
        <div className="stack gap-14">
          <div className="firewall-head">
            <Select
              label="目标节点"
              value={nodeId}
              onChange={(event) => {
                const next = new URLSearchParams(params);
                next.set('node', event.target.value);
                setParams(next, { replace: true });
                setSaved('');
              }}
              options={nodeList.map((node) => [node.id, `${node.name}${node.connected ? '' : ' · 离线'}`] as const)}
            />
            <Select
              label="地址族"
              value={family}
              onChange={(event) => setFamily(event.target.value as 'all' | 'ipv4' | 'ipv6')}
              options={[
                ['all', '全部'],
                ['ipv4', 'IPv4'],
                ['ipv6', 'IPv6'],
              ]}
            />
            <div className="grow" />
            <div className="row gap-6" style={{ paddingBottom: 2 }}>
              <Button size="sm" icon="plus" onClick={() => setEditing({ draft: { ...blankRule, family: family === 'ipv6' ? 'ipv6' : 'ipv4' } })} disabled={!nodeId}>
                添加规则
              </Button>
              <Button size="sm" variant="line" icon="shield" onClick={() => setPolicyOpen(true)} disabled={!nodeId}>
                入站策略
              </Button>
            </div>
          </div>

          <div className="spread" style={{ paddingTop: 12, borderTop: '1px solid var(--rule)' }}>
            <div className="row gap-10 wrap">
              <span className="row gap-6">
                <span className="kicker">服务</span>
                <Stamp state={state.data?.service_active ? 'enabled' : 'disabled'}>{state.data?.service_active ? '已启用' : '已停用'}</Stamp>
              </span>
              <span className="row gap-6">
                <span className="kicker">配置</span>
                <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
                  修订 {state.data?.revision ?? '—'} · 已确认 {state.data?.confirmed_revision ?? '—'}
                </span>
              </span>
              {state.data ? <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>读取 {ago(state.data.checked_at)}</span> : null}
            </div>

          </div>

          <div className="default-row">
            {(state.data?.default_policies ?? []).map((policy, index) => (
              <span key={index} className="default-chip">
                <b>{policy.family.toUpperCase()}</b>
                {policy.chain ? <span>{policy.chain}</span> : null}
                <span style={{ color: policy.action === 'allow' ? 'var(--stamp)' : 'var(--ink-soft)' }}>
                  {policy.action === 'default' ? '随系统' : actionLabels[policy.action as Action] ?? policy.action}
                </span>
              </span>
            ))}
          </div>

          <Tabs
            items={[
              { id: 'port', label: '端口规则', count: portRules.length },
              { id: 'ip', label: 'IP 规则', count: ipRules.length },
              { id: 'trust', label: '集群互信' },
            ]}
            value={tab}
            onChange={(next) => setTab(next as 'port' | 'ip' | 'trust')}
            label="规则分类"
          />

          {tab === 'trust' ? (
            <PeerTrustTable />
          ) : state.isPending ? (
            <Skeleton height={140} />
          ) : list.length === 0 ? (
            <Empty>
              {family !== 'all' ? '这个地址族没有规则' : `没有${tab === 'port' ? '端口' : 'IP'}规则`}
            </Empty>
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th scope="col" style={{ width: 46 }}>#</th>
                    <th scope="col">动作</th>
                    <th scope="col">来源</th>
                    <th scope="col">端口</th>
                    <th scope="col">协议</th>
                    <th scope="col">目标</th>
                    <th scope="col">备注</th>
                    <th scope="col">链</th>
                    <th scope="col" className="right">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {list.map((rule, index) => (
                    <tr key={rule.id}>
                      <td className="table__serial">{String(index + 1).padStart(2, '0')}</td>
                      <td>
                        <span className={`stamp stamp--${rule.spec.action === 'allow' ? 'ok' : rule.spec.action === 'deny' ? 'bad' : 'warn'}`}>
                          {actionLabels[rule.spec.action]}
                        </span>
                      </td>
                      <td className="mono" style={{ fontSize: 11.5 }}>{rule.spec.source || '任意'}</td>
                      <td className="mono" style={{ fontSize: 11.5 }}>{rule.spec.port || '—'}</td>
                      <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{protocolLabels[rule.spec.protocol] ?? rule.spec.protocol}</td>
                      <td className="mono" style={{ fontSize: 11.5 }}>{rule.spec.destination || '任意'}</td>
                      <td style={{ maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis', color: 'var(--ink-soft)' }}>{rule.spec.comment || '—'}</td>
                      <td className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>{rule.chain || '—'}</td>
                      <td className="right">
                        <span className="row gap-2" style={{ justifyContent: 'flex-end' }}>
                          <Button size="sm" variant="ghost" icon="up" aria-label="上移" disabled={index === 0 || change.isPending} onClick={() => change.mutate({ kind: 'move_rule', rule_id: rule.id, before_rule_id: list[index - 1]?.id })} />
                          <Button size="sm" variant="ghost" icon="downarrow" aria-label="下移" disabled={index === list.length - 1 || change.isPending} onClick={() => change.mutate({ kind: 'move_rule', rule_id: rule.id, before_rule_id: list[index + 2]?.id })} />
                          <Button size="sm" variant="ghost" icon="edit" aria-label="编辑" onClick={() => setEditing({ rule, draft: rule.spec })} />
                          <Button size="sm" variant="ghost" icon="trash" aria-label="删除" onClick={() => setDeleting(rule)} />
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <Failure error={change.error} />
        </div>
      </Sheet>

      <RuleDialog
        editing={editing}
        busy={change.isPending}
        error={change.error}
        onClose={() => setEditing(null)}
        onSave={(draft) => {
          if (!editing) return;
          if (editing.rule) change.mutate({ kind: 'edit_rule', rule_id: editing.rule.id, rule: draft });
          else change.mutate({ kind: 'add_rule', rule: draft });
        }}
      />

      <Confirm
        open={Boolean(deleting)}
        title="删除这条规则？"
        description={
          <>
            删除后会立即提交到 <span className="mono">{nodeList.find((node) => node.id === nodeId)?.name}</span> 并应用。
            运行规则变更需读回确认，未确认时自动撤销。
          </>
        }
        confirmLabel="删除规则"
        dangerous
        busy={change.isPending}
        error={change.error}
        onCancel={() => setDeleting(null)}
        onConfirm={() => deleting && change.mutate({ kind: 'delete_rule', rule_id: deleting.id })}
      />

      <PolicyDialog
        open={policyOpen}
        policies={state.data?.default_policies ?? []}
        busy={change.isPending}
        error={change.error}
        onClose={() => setPolicyOpen(false)}
        onSave={(policy, action) => change.mutate({ kind: 'set_default_policy', chain: policy.chain, action })}
      />
    </div>
  );
}

function RuleDialog({
  editing,
  busy,
  error,
  onClose,
  onSave,
}: {
  editing: { rule?: Rule; draft: Schema['RuleSpec'] } | null;
  busy: boolean;
  error: unknown;
  onClose: () => void;
  onSave: (draft: Schema['RuleSpec']) => void;
}) {
  const [draft, setDraft] = useState<Schema['RuleSpec']>(blankRule);
  useEffect(() => {
    if (editing) setDraft({ ...editing.draft });
  }, [editing]);
  return (
    <Dialog
      open={Boolean(editing)}
      title={editing?.rule ? '编辑规则' : '添加规则'}
     
      width="wide"
      onClose={onClose}
      footer={
        <>
          <span className="grow mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>
            保存后按修订号提交，需要节点读回确认
          </span>
          <Button onClick={onClose} disabled={busy}>取消</Button>
          <Button variant="solid" busy={busy} onClick={() => onSave(draft)} disabled={!draft.source && !draft.destination}>
            {editing?.rule ? '保存规则' : '添加并应用'}
          </Button>
        </>
      }
    >
      <div className="form-grid">
        <Select
          label="地址族"
          value={draft.family}
          onChange={(event) => setDraft({ ...draft, family: event.target.value as 'ipv4' | 'ipv6' })}
          options={[['ipv4', 'IPv4'], ['ipv6', 'IPv6']]}
        />
        <Select
          label="动作"
          value={draft.action}
          onChange={(event) => setDraft({ ...draft, action: event.target.value as Action })}
          options={[['allow', '允许'], ['deny', '丢弃'], ['reject', '拒绝并回应']]}
        />
        <TextInput label="来源" value={draft.source} onChange={(event) => setDraft({ ...draft, source: event.target.value })} className="input--mono" placeholder="0.0.0.0/0 或 10.8.0.0/24" />
        <TextInput label="端口" value={draft.port ?? ''} onChange={(event) => setDraft({ ...draft, port: event.target.value })} className="input--mono" placeholder="留空表示任意端口（IP 规则）" />
        <Select
          label="协议"
          value={draft.protocol}
          onChange={(event) => setDraft({ ...draft, protocol: event.target.value as Schema['RuleSpec']['protocol'] })}
          options={[['any', '全部'], ['tcp', 'TCP'], ['udp', 'UDP'], ['icmp', 'ICMP'], ['icmpv6', 'ICMPv6']]}
        />
        <TextInput label="目标" value={draft.destination ?? ''} onChange={(event) => setDraft({ ...draft, destination: event.target.value })} className="input--mono" placeholder="留空表示本机任意地址" />
        <TextInput label="备注" value={draft.comment ?? ''} onChange={(event) => setDraft({ ...draft, comment: event.target.value })} placeholder="写清这条规则为什么存在" />
      </div>
      <Notice tone="info">
        端口留空即按 IP 规则保存。
      </Notice>
      <Failure error={error} />
    </Dialog>
  );
}

function PolicyDialog({
  open,
  policies,
  busy,
  error,
  onClose,
  onSave,
}: {
  open: boolean;
  policies: Schema['ManagedPolicy'][];
  busy: boolean;
  error: unknown;
  onClose: () => void;
  onSave: (policy: Schema['ManagedPolicy'], action: Schema['ManagedPolicy']['action']) => void;
}) {
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!open) return;
    setDrafts(Object.fromEntries(policies.map((policy) => [`${policy.family}:${policy.chain ?? ''}`, policy.action])));
  }, [open, policies]);
  return (
    <Dialog
      open={open}
      title="入站默认策略"
     
      onClose={onClose}
      footer={
        <>
          <span className="grow" />
          <Button onClick={onClose} disabled={busy}>取消</Button>
          <Button
            variant="solid"
            busy={busy}
            onClick={() => {
              policies.forEach((policy) => {
                const next = (drafts[`${policy.family}:${policy.chain ?? ''}`] ?? policy.action) as Schema['ManagedPolicy']['action'];
                if (next !== policy.action) onSave(policy, next);
              });
            }}
          >
            保存策略
          </Button>
        </>
      }
    >
      {policies.length === 0 ? <Empty>没有默认策略</Empty> : null}
      <div className="stack gap-10">
        {policies.map((policy) => {
          const key = `${policy.family}:${policy.chain ?? ''}`;
          return (
            <div key={key} className="spread">
              <span className="mono" style={{ fontSize: 11 }}>
                {policy.family.toUpperCase()} · {policy.chain || 'input'}
              </span>
              <Select
                aria-label={`${policy.family} ${policy.chain ?? ''} 策略`}
                value={drafts[key] ?? policy.action}
                onChange={(event) => setDrafts({ ...drafts, [key]: event.target.value as Schema['ManagedPolicy']['action'] })}
                options={[['default', '随系统'], ['allow', '允许'], ['deny', '丢弃'], ['reject', '拒绝']]}
                style={{ width: 120 }}
              />
            </div>
          );
        })}
      </div>
      <Notice>改为允许前请确认已覆盖全部管理入口。</Notice>
      <Failure error={error} />
    </Dialog>
  );
}

function PeerTrustTable() {
  const query = useQuery({
    queryKey: ['peer-trust', 'status'],
    queryFn: async ({ signal }) => value(await api.GET('/peer-trust/status', { signal })),
    refetchInterval: 30_000,
  });
  const nodes = useQuery({
    queryKey: ['nodes'],
    queryFn: async ({ signal }) => value(await api.GET('/nodes', { signal })),
    staleTime: 10_000,
  });
  const names = useMemo(() => new Map(nodes.data?.items.map((node) => [node.id, node.name]) ?? []), [nodes.data]);

  return (
    <div className="stack gap-12">
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      {query.isPending ? <Skeleton height={100} /> : null}
      {query.data ? (
        <>
          <div className="spread">
            <span className="row gap-8">
              <span className="kicker">集群互信</span>
              <Stamp state={query.data.policy.enabled ? 'enabled' : 'disabled'}>{query.data.policy.enabled ? '已开启' : '已关闭'}</Stamp>
              <span className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>计数器 {query.data.policy.version.counter}</span>
            </span>
            <Link to="/firewall/settings">
              <Button size="sm" icon="settings">互信设置</Button>
            </Link>
          </div>
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th scope="col">来源节点</th>
                  <th scope="col">地址</th>
                  <th scope="col">地址族</th>
                  <th scope="col">成员</th>
                  <th scope="col">状态</th>
                </tr>
              </thead>
              <tbody>
                {query.data.nodes.flatMap((node) =>
                  node.rules.length === 0
                    ? [
                        <tr key={node.node_id}>
                          <td>{names.get(node.node_id) ?? node.node_id}</td>
                          <td className="mono" colSpan={2} style={{ color: 'var(--ink-mute)' }}>无互信规则</td>
                          <td />
                          <td><Stamp state={node.state}>{peerTrustStates[node.state]}</Stamp></td>
                        </tr>,
                      ]
                    : node.rules.map((rule, index) => (
                        <tr key={`${node.node_id}-${index}`}>
                          <td>{names.get(node.node_id) ?? node.node_id}</td>
                          <td className="mono" style={{ fontSize: 11.5 }}>{rule.address || '—'}</td>
                          <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{rule.family.toUpperCase()}</td>
                          <td style={{ color: 'var(--ink-soft)', fontSize: 12 }}>{rule.members.map((member) => member.name).join('、')}</td>
                          <td><Stamp state={rule.state === 'in_sync' ? 'in_sync' : tone(rule.state)}>{rule.state === 'in_sync' ? '已生效' : rule.state}</Stamp></td>
                        </tr>
                      )),
                )}
              </tbody>
            </table>
          </div>
          <p className="mono" style={{ fontSize: 10, color: 'var(--ink-mute)' }}>
            互信会放行成员的主机入站端口，含数据库端口。
          </p>
        </>
      ) : null}
    </div>
  );
}

export { managedFirewallStates, stamp };



