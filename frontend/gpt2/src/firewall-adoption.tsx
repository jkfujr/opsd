import { useRef, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@appica/ui-react/dialog';
import { api, findOperation, targetNode, unwrap, unknownResult } from './api/client';
import { BusyButton, Button, date, Failure, newRequestKey } from './business-ui';
import { useOperations, invalidateOperations } from './operations';
import { queryClient } from './state';
import type { components } from './api/types';

export function FirewallAdoption({ node, disabled }: { node: string; disabled: boolean }) {
  const [open, setOpen] = useState(false), [uncertain, setUncertain] = useState(false), key = useRef('');
  const operations = useOperations();
  const preview = useMutation({ mutationFn: async () => unwrap(await api.GET('/nodes/{node_id}/firewall/adoption', { params: { path: { node_id: node } } })), onSuccess: () => { key.current = newRequestKey(); setOpen(true); } });
  const completed = (operation: components['schemas']['Operation']) => { invalidateOperations(); void queryClient.invalidateQueries({ queryKey: ['firewall', node] }); operations.show(operation.id); setOpen(false); setUncertain(false); };
  const save = useMutation({ mutationFn: async () => unwrap(await api.POST('/nodes/{node_id}/firewall/actions', { ...targetNode(node), params: { path: { node_id: node }, header: { 'Idempotency-Key': key.current } }, body: { action: 'adopt_current', expected_revision: preview.data!.expected_revision, fingerprint: preview.data!.fingerprint } })), onSuccess: completed, onError: error => { if (unknownResult(error)) setUncertain(true); } });
  const locate = useMutation({ mutationFn: () => findOperation(node, key.current), onSuccess: operation => { if (operation) completed(operation); else { setUncertain(false); save.reset(); } } });
  const locked = save.isPending || uncertain, value = preview.data;
  return <div className="space-y-3"><div><BusyButton variant="outline" busy={preview.isPending} disabled={disabled || locked} onClick={() => { save.reset(); preview.mutate(); }}>预览当前配置</BusyButton></div><Failure error={preview.error}/><Dialog open={open} onOpenChange={next => { if (!locked) setOpen(next); }}><DialogContent frame={false} className="ui-modal w-full max-w-xl" closeLabel="关闭当前防火墙配置"><DialogHeader><DialogTitle>接纳当前防火墙配置</DialogTitle></DialogHeader><DialogBody className="space-y-4 overflow-y-auto">{value && <><dl className="detail-fields"><div><dt>实际后端</dt><dd>{value.configuration.backend}</dd></div><div><dt>采集时间</dt><dd>{date(value.checked_at)}</dd></div><div><dt>规则数量</dt><dd>{value.configuration.rules.length}</dd></div></dl>{value.configuration.rules.map(rule => <div key={rule.id} className="break-words text-sm">{rule.spec.family} · {rule.spec.source || '全部来源'} · {rule.spec.protocol} {rule.spec.port} · {rule.spec.action}</div>)}{value.configuration.default_policies.map((policy, index) => <div key={index} className="break-words text-sm">{policy.family} · {policy.chain || policy.zone || '默认入站'} · {policy.action}</div>)}</>}<Failure error={save.error ?? locate.error}/></DialogBody><DialogFooter><Button variant="outline" disabled={locked} onClick={() => setOpen(false)}>取消</Button>{uncertain ? <BusyButton busy={locate.isPending} onClick={() => locate.mutate()}>查询原操作</BusyButton> : <BusyButton busy={save.isPending} disabled={!value || disabled} onClick={() => save.mutate()}>接纳当前配置</BusyButton>}</DialogFooter></DialogContent></Dialog></div>;
}
