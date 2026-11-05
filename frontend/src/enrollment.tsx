import { useEffect, useId, useRef, useState, type RefObject } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogBody, DialogFooter } from '@appica/ui-react/dialog';
import { Button } from '@appica/ui-react/button';
import { Textarea } from '@appica/ui-react/textarea';
import { Field, FieldLabel } from '@appica/ui-react/field';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { api, RequestError, unwrap } from './api/client';
import type { components } from './api/schema.gen';
import { queryClient } from './state';
import { BusyButton, Confirm, date, Failure, LoadingRows, TextField } from './ui';

const shellQuote = (value: string) => `'${value.replaceAll("'", "'\\''")}'`;
const bootstrap = `set -eu; [ "$(id -u)" -eq 0 ] || { printf '%s\\n' '请以 root 执行安装命令' >&2; exit 1; }; if { ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; } || [ ! -s /etc/ssl/certs/ca-certificates.crt ]; then . /etc/os-release; case "$ID" in debian|ubuntu) ;; *) exit 1 ;; esac; apt-get update -qq; DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ca-certificates curl; fi; file=$(mktemp); trap 'rm -f "$file"' 0 1 2 15; url=$1; shift; if command -v curl >/dev/null 2>&1; then curl -fsSL --proto '=https' --proto-redir '=https' "$url" -o "$file"; else wget --https-only -q -O "$file" "$url"; fi; sh "$file" "$@"`;

export function EnrollmentDialog({ mode, onClose, onManage, trigger }: { mode: 'add' | 'tokens'; onClose: () => void; onManage: () => void; trigger: RefObject<HTMLButtonElement | null> }) {
  const commandID = useId();
  const [created, setCreated] = useState<{ result: components['schemas']['EnrollmentTokenCreated']; name: string; region: string } | null>(null);
  const [revoke, setRevoke] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState<Error | null>(null);
  const [uncertain, setUncertain] = useState(false);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const query = useQuery({ queryKey: ['tokens'], enabled: mode === 'tokens', queryFn: async ({ signal }) => unwrap(await api.GET('/enrollment-tokens', { signal })) });
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (mode !== 'tokens') return;
    const next = Math.min(...(query.data?.items.filter((item) => !item.revoked_at && !item.consumed_at).map((item) => new Date(item.expires_at).getTime()).filter((time) => time > now) ?? []));
    if (!Number.isFinite(next)) return;
    const timer = setTimeout(() => setNow(Date.now()), Math.min(Math.max(next - Date.now(), 1), 2147483647));
    return () => clearTimeout(timer);
  }, [mode, query.data, now]);
  const create = useMutation({ gcTime: 0, mutationFn: async (values: { name: string; region: string }) => ({ result: unwrap(await api.POST('/enrollment-tokens', { params: { header: { 'X-CSRF-Token': '' } } })), ...values }), onSuccess: (data) => { setCreated(data); void queryClient.invalidateQueries({ queryKey: ['tokens'] }); }, onError: (error) => setUncertain(!(error instanceof RequestError && [403, 409, 422].includes(error.status))) });
  const revoked = useMutation({ mutationFn: async () => unwrap(await api.DELETE('/enrollment-tokens/{id}', { params: { path: { id: revoke! }, header: { 'X-CSRF-Token': '' } } })), onSuccess: () => { setRevoke(null); void queryClient.invalidateQueries({ queryKey: ['tokens'] }); } });
  const command = created ? `sh -c ${shellQuote(bootstrap)} sh ${shellQuote(created.result.installer_url)} --non-interactive --expected-version ${shellQuote(created.result.version)} --peer-address ${shellQuote(created.result.peer_address)} --peer-public-key ${shellQuote(created.result.peer_public_key)} --enrollment-token ${shellQuote(created.result.token)}${created.name ? ` --name ${shellQuote(created.name)}` : ''}${created.region ? ` --region ${shellQuote(created.region)}` : ''}` : '';
  const copy = async () => {
    setCopyError(null);
    try {
      try { if (!navigator.clipboard) throw new Error(); await navigator.clipboard.writeText(command); }
      catch { textarea.current?.select(); if (!document.execCommand('copy')) throw new Error(); }
      setCopied(true);
    } catch { setCopyError(new Error('复制失败，请选择命令后手动复制')); }
  };
  return <Dialog open onOpenChange={(open) => { if (!open && !create.isPending && !revoked.isPending) onClose(); }}>
    <DialogContent className={`w-full ${mode === 'tokens' ? 'max-w-3xl' : 'max-w-xl'}`} closeLabel="关闭" closeButton={!create.isPending && !revoked.isPending} finalFocus={trigger}>
      <DialogHeader><DialogTitle>{mode === 'add' ? '添加节点' : '接入码'}</DialogTitle></DialogHeader>
      <DialogBody className="min-w-0 space-y-5 overflow-y-auto">
        {mode === 'add' ? created ? <>
          <dl className="flex flex-wrap gap-x-6 gap-y-2 text-sm">{created.name && <div><dt className="text-foreground-muted">名称</dt><dd className="break-all">{created.name}</dd></div>}{created.region && <div><dt className="text-foreground-muted">地域</dt><dd className="break-all">{created.region}</dd></div>}<div><dt className="text-foreground-muted">到期时间</dt><dd>{date(created.result.record.expires_at)}</dd></div></dl>
          <Field className="space-y-2"><FieldLabel htmlFor={commandID}>一次性安装命令</FieldLabel><Textarea id={commandID} ref={textarea} value={command} readOnly rows={6} className="font-mono text-sm" /></Field><Failure error={copyError} />
        </> : <form id="add-node" className="space-y-5" onSubmit={(event) => { event.preventDefault(); const form = new FormData(event.currentTarget); create.mutate({ name: String(form.get('name')).trim(), region: String(form.get('region')).trim() }); }}>
          <TextField label="名称（可选）" name="name" maxLength={255} placeholder="主机名" disabled={create.isPending || uncertain} />
          <TextField label="地域（可选）" name="region" disabled={create.isPending || uncertain} />
          <Failure error={create.error} />
        </form> : <>
          <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
          <div className="max-w-full overflow-x-auto" aria-busy={query.isPending}><Table className="min-w-128"><TableHeader><TableRow><TableHead>创建时间</TableHead><TableHead>到期时间</TableHead><TableHead>状态</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader><TableBody>
            {query.isPending ? <LoadingRows columns={4} /> : query.data?.items.map((item) => {
              const status = item.revoked_at ? '已撤销' : item.consumed_at ? '已使用' : new Date(item.expires_at).getTime() <= Date.now() ? '已过期' : '可用';
              return <TableRow key={item.id}><TableCell className="whitespace-nowrap">{date(item.created_at)}</TableCell><TableCell className="whitespace-nowrap">{date(item.expires_at)}</TableCell><TableCell>{status}</TableCell><TableCell className="text-right"><Button variant="ghost" size="sm" disabled={status !== '可用' || query.isError} onClick={() => { revoked.reset(); setRevoke(item.id); }}>撤销</Button></TableCell></TableRow>;
            })}
            {!query.isError && query.data?.items.length === 0 && <TableRow><TableCell colSpan={4} className="py-8 text-center text-foreground-muted">暂无接入码</TableCell></TableRow>}
          </TableBody></Table></div>
          <Confirm open={revoke !== null} title="撤销接入码？" description="撤销后不能再用于添加节点。" busy={revoked.isPending} error={revoked.error} onCancel={() => setRevoke(null)} onConfirm={() => revoked.mutate()} />
        </>}
      </DialogBody>
      <DialogFooter><Button variant="outline" disabled={create.isPending || revoked.isPending} onClick={onClose}>关闭</Button>{mode === 'add' && (created ? <Button onClick={() => void copy()}>{copied ? '已复制' : '复制命令'}</Button> : uncertain ? <Button onClick={onManage}>查看接入码</Button> : <BusyButton type="submit" form="add-node" busy={create.isPending}>生成安装命令</BusyButton>)}</DialogFooter>
    </DialogContent>
  </Dialog>;
}
