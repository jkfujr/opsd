import { useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Button } from '@appica/ui-react/button';
import { Dialog, DialogContent, DialogTitle } from '@appica/ui-react/dialog';
import { Textarea } from '@appica/ui-react/textarea';
import { Field, FieldLabel } from '@appica/ui-react/field';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { api, unwrap } from '../api/client';
import type { components } from '../api/schema.gen';
import { useSession } from '../auth';
import { queryClient } from '../state';
import { Confirm, date, Failure, PageTitle } from '../ui';

const shellQuote = (value: string) => `'${value.replaceAll("'", "'\\''")}'`;

export function Tokens() {
  const session = useSession();
  const header = { 'X-CSRF-Token': session.csrf_token };
  const query = useQuery({ queryKey: ['tokens'], queryFn: async ({ signal }) => unwrap(await api.GET('/enrollment-tokens', { signal })) });
  const [created, setCreated] = useState<components['schemas']['EnrollmentTokenCreated'] | null>(null);
  const [revoke, setRevoke] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState<Error | null>(null);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const create = useMutation({ gcTime: 0, mutationFn: async () => unwrap(await api.POST('/enrollment-tokens', { params: { header } })), onSuccess: (data) => { setCreated(data); setCopied(false); setCopyError(null); void queryClient.invalidateQueries({ queryKey: ['tokens'] }); } });
  const revoked = useMutation({ mutationFn: async () => unwrap(await api.DELETE('/enrollment-tokens/{id}', { params: { path: { id: revoke! }, header } })), onSuccess: () => { setRevoke(null); void queryClient.invalidateQueries({ queryKey: ['tokens'] }); } });
  const command = created ? `./opsd-agent install --non-interactive --control-url ${shellQuote(created.control_address)} --control-public-key ${shellQuote(created.control_public_key)} --enrollment-token ${shellQuote(created.token)}` : '';
  const copy = async () => {
    try {
      if (navigator.clipboard) await navigator.clipboard.writeText(command);
      else { textarea.current?.select(); if (!document.execCommand('copy')) throw new Error(); }
      setCopied(true);
    } catch { setCopyError(new Error('复制失败，请选择命令后手动复制')); }
  };
  return <>
    <PageTitle action={<Button disabled={create.isPending} onClick={() => create.mutate()}>{create.isPending ? '创建中…' : '创建接入码'}</Button>}>接入码</PageTitle>
    <Failure error={query.error || create.error} />
    {query.isPending ? <p className="text-foreground-muted">加载中…</p> : query.data && (query.data.items.length ? <div className="overflow-x-auto"><Table className="min-w-[38rem]">
      <TableHeader><TableRow><TableHead>创建时间</TableHead><TableHead>到期时间</TableHead><TableHead>状态</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
      <TableBody>{query.data.items.map((item) => {
        const status = item.revoked_at ? '已撤销' : item.consumed_at ? '已使用' : new Date(item.expires_at).getTime() <= Date.now() ? '已过期' : '可用';
        return <TableRow key={item.id}><TableCell className="whitespace-nowrap">{date(item.created_at)}</TableCell><TableCell className="whitespace-nowrap">{date(item.expires_at)}</TableCell><TableCell>{status}</TableCell><TableCell className="text-right"><Button variant="ghost" size="sm" disabled={status !== '可用'} onClick={() => { revoked.reset(); setRevoke(item.id); }}>撤销</Button></TableCell></TableRow>;
      })}</TableBody>
    </Table></div> : <p className="py-10 text-foreground-muted">暂无接入码</p>)}
    <Dialog open={created !== null} onOpenChange={(open) => { if (!open) { setCreated(null); create.reset(); } }}>
      <DialogContent closeLabel="关闭" className="w-full max-w-xl">
        <DialogTitle>接入码已创建</DialogTitle>
        <div className="mt-5 space-y-5">
          <p className="text-sm text-foreground-muted">命令仅展示本次，有效期 24 小时，成功接入后失效。</p>
          <Field className="space-y-2"><FieldLabel>接入命令</FieldLabel><Textarea ref={textarea} value={command} readOnly rows={5} className="font-mono text-sm" /></Field>
          <Failure error={copyError} /><Button onClick={() => void copy()}>{copied ? '已复制' : '复制命令'}</Button>
        </div>
      </DialogContent>
    </Dialog>
    <Confirm open={revoke !== null} title="撤销接入码？" description="撤销后不能再用于添加节点。" busy={revoked.isPending} error={revoked.error} onCancel={() => setRevoke(null)} onConfirm={() => revoked.mutate()} />
  </>;
}
