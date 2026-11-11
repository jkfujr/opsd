import { useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Button } from './business-ui';
import { Fieldset, FieldsetLegend } from '@appica/ui-react/fieldset';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { allPages, api, condition, RequestError, unwrap, unknownResult } from './api/client';
import { usePagedQuery } from './api/pagination';
import type { components } from './api/types';
import { BusyButton, Confirm, ContinueReading, Failure, LoadingRows, newRequestKey, TextField, useUnsavedChanges } from './business-ui';
import { useSession } from './auth';
import { queryClient } from './state';
import { forgetRequest } from './api/requests';

type Installation = components['schemas']['ThemeInstallation'];
type Command = { kind: 'repository'; input: components['schemas']['ThemeRepositoryInstall'] }
  | { kind: 'upload'; file: File }
  | { kind: 'activate'; id: string }
  | { kind: 'delete'; id: string };

export function ConsoleThemes() {
  const session = useSession();
  const query = usePagedQuery({ queryKey: ['themes'], queryFn: async ({ signal }) => allPages(async (cursor) => unwrap(await api.GET('/console/theme-installations', { signal, params: { query: { cursor, limit: 100 } } }))) });
  const selection = useQuery({ queryKey: ['themes', 'selection'], queryFn: async ({ signal }) => unwrap(await api.GET('/console/theme-selection', { signal })) });
  const key = useRef('');
  const [repository, setRepository] = useState('');
  const [tag, setTag] = useState('');
  const [file, setFile] = useState<File | null>(null);
  const [fileKey, setFileKey] = useState(0);
  const [deleting, setDeleting] = useState<Installation | null>(null);
  const [needsRead, setNeedsRead] = useState(false);
  const [message, setMessage] = useState('');
  const [refreshNeeded, setRefreshNeeded] = useState(false);
  const [readError, setReadError] = useState<unknown>(null);
  useUnsavedChanges(Boolean(repository || tag || file));
  const mutation = useMutation({ mutationFn: async (command: Command) => {
    switch (command.kind) {
      case 'repository': {
        key.current ||= newRequestKey();
        const item = unwrap(await api.POST('/console/theme-installations', { params: { header: { 'Idempotency-Key': key.current } }, body: command.input }));
        return `${item.manifest.name} ${item.manifest.version} 已安装`;
      }
      case 'upload': {
        if (command.file.size > 100 * 1024 * 1024) throw new RequestError('主题 ZIP 不能超过 100 MiB', 422);
        key.current ||= newRequestKey();
        const item = unwrap(await api.POST('/console/theme-installations', { params: { header: { 'Idempotency-Key': key.current } }, headers: { 'Content-Type': 'application/zip' }, body: '', bodySerializer: () => command.file }));
        return `${item.manifest.name} ${item.manifest.version} 已安装`;
      }
      case 'activate': {
        const value = unwrap(await api.PUT('/console/theme-selection', { params: { header: condition(selection.data) }, body: { installation_id: command.id || 'builtin' } }));
        queryClient.setQueryData(['themes', 'selection'], value);
        return '已切换主题';
      }
      case 'delete': {
        const item = unwrap(await api.GET('/console/theme-installations/{installation_id}', { params: { path: { installation_id: command.id } } }));
        unwrap(await api.DELETE('/console/theme-installations/{installation_id}', { params: { path: { installation_id: command.id }, header: condition(item) } }));
        return '已删除主题';
      }
    }
  }, onMutate: () => { setMessage(''); setReadError(null); }, onSuccess: async (result, command) => {
    key.current = '';
    setMessage(result);
    setNeedsRead(false);
    if (command.kind === 'repository') { setRepository(''); setTag(''); }
    if (command.kind === 'upload') { setFile(null); setFileKey((current) => current + 1); }
    if (command.kind === 'activate') setRefreshNeeded(true);
    const read = await query.refetch();
    const selected = await selection.refetch();
    if (read.isError || selected.isError) setNeedsRead(true);
  }, onError: (error) => {
    setNeedsRead(unknownResult(error) || error instanceof RequestError && [409, 412, 428].includes(error.status));
    if (!unknownResult(error)) key.current = '';
  }, onSettled: () => setDeleting(null) });
  const busy = mutation.isPending;
  const disabled = busy || needsRead || !query.data || query.isError || !selection.data || selection.isError;
  const current = selection.data?.installation_id;
  const read = async () => {
    setReadError(null);
    try {
      if (key.current && unknownResult(mutation.error)) {
        const receipt = unwrap(await api.GET('/requests/{request_key}', { params: { path: { request_key: key.current }, query: { node_id: session.local_node_id } } }));
        if (receipt.state !== 'completed') throw new Error('原请求尚未完成，请稍后继续查询');
      }
      const [result, selected] = await Promise.all([query.refetch(), selection.refetch()]);
      if (result.isError || selected.isError) return;
      if (mutation.variables?.kind === 'activate' && selected.data?.installation_id === (mutation.variables.id || 'builtin')) setRefreshNeeded(true);
      if (key.current) {
        forgetRequest(key.current); key.current = '';
        setRepository(''); setTag(''); setFile(null); setFileKey(value => value + 1);
      }
      setNeedsRead(false); mutation.reset();
    } catch (error) { setReadError(error); }
  };
  return <>
    <Fieldset className="mt-8 min-w-0 border-t border-border pt-4"><FieldsetLegend className="section-title">前端主题</FieldsetLegend>
      <Failure error={query.error ?? selection.error ?? readError} retry={() => void read()} busy={query.isFetching || selection.isFetching} />
      <div className="overflow-x-auto" aria-busy={query.isPending}><Table className="min-w-[42rem]">
        <TableHeader><TableRow><TableHead>名称</TableHead><TableHead>版本</TableHead><TableHead>登录页</TableHead><TableHead>状态</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
        <TableBody>{query.isPending ? <LoadingRows columns={5} /> : <>
          <TableRow><TableCell className="font-medium">内置界面</TableCell><TableCell>随 OPSD</TableCell><TableCell>内置</TableCell><TableCell>{current === 'builtin' ? '已启用' : selection.data?.effective_installation_id === 'builtin' ? '当前使用' : '可用'}</TableCell><TableCell className="text-right"><BusyButton variant="outline" size="sm" disabled={disabled || current === 'builtin'} busy={busy && mutation.variables?.kind === 'activate' && mutation.variables.id === ''} onClick={() => mutation.mutate({ kind: 'activate', id: '' })}>切回内置</BusyButton></TableCell></TableRow>
          {query.data?.items.map((item) => <TableRow key={item.installation_id}>
            <TableCell><div className="flex items-center gap-3">{item.preview_url && <img src={item.preview_url} alt={`${item.manifest.name}预览`} loading="lazy" className="h-14 w-24 shrink-0 rounded-md object-cover" />}<div className="min-w-0"><div className="font-medium">{item.manifest.url ? <a href={item.manifest.url} target="_blank" rel="noreferrer" className="underline underline-offset-4">{item.manifest.name}</a> : item.manifest.name}</div>{item.manifest.author && <div className="text-xs text-foreground-muted">{item.manifest.author}</div>}<div className="mt-1 text-xs text-foreground-muted">{item.source === 'upload' ? 'ZIP 上传' : <a href={item.source} target="_blank" rel="noreferrer" className="underline underline-offset-4">GitHub Release</a>}</div></div></div></TableCell>
            <TableCell className="whitespace-nowrap">{item.manifest.version}</TableCell><TableCell>{item.manifest.login === 'theme' ? '主题' : '内置'}</TableCell>
            <TableCell className="max-w-64"><span>{current === item.installation_id ? item.available ? '已启用' : '当前选择' : item.available ? '可用' : '不可用'}</span>{item.problem && <p className="mt-1 text-xs text-error-intense">{item.problem}</p>}</TableCell>
            <TableCell><div className="flex justify-end gap-2"><BusyButton variant="outline" size="sm" busy={busy && mutation.variables?.kind === 'activate' && mutation.variables.id === item.installation_id} disabled={disabled || !item.available || current === item.installation_id} onClick={() => mutation.mutate({ kind: 'activate', id: item.installation_id })}>启用</BusyButton><Button variant="ghost" size="sm" disabled={disabled || current === item.installation_id} onClick={() => { mutation.reset(); setDeleting(item); }}>删除</Button></div></TableCell>
          </TableRow>)}
        </>}</TableBody>
      </Table></div>
      <ContinueReading query={query}/>
      <div className="mt-4 space-y-3"><Failure error={mutation.error} />{message && <p role="status" className="text-sm text-foreground-muted">{message}</p>}{(needsRead || refreshNeeded) && <div className="flex flex-wrap gap-3">{needsRead && <BusyButton variant="outline" busy={query.isFetching} onClick={() => void read()}>重新读取</BusyButton>}{refreshNeeded && <Button disabled={busy} onClick={() => window.location.assign('/')}>刷新界面</Button>}</div>}</div>
    </Fieldset>
    <div className="mt-8 grid gap-8 xl:grid-cols-2">
      <form className="min-w-0 space-y-4" onSubmit={(event) => { event.preventDefault(); mutation.mutate({ kind: 'repository', input: { repository, ...(tag ? { tag } : {}) } }); }}>
        <h2 className="section-title">从仓库安装</h2>
        <TextField label="GitHub 仓库" type="url" placeholder="https://github.com/作者/仓库" required maxLength={2048} value={repository} onChange={(event) => setRepository(event.target.value)} disabled={busy || needsRead} />
        <TextField label="Release 标签（可选）" placeholder="最新正式版本" maxLength={256} value={tag} onChange={(event) => setTag(event.target.value)} disabled={busy || needsRead} />
        <BusyButton type="submit" busy={busy && mutation.variables?.kind === 'repository'} disabled={disabled}>安装</BusyButton>
      </form>
      <form className="min-w-0 space-y-4" onSubmit={(event) => { event.preventDefault(); if (file) mutation.mutate({ kind: 'upload', file }); }}>
        <h2 className="section-title">上传主题</h2>
        <TextField key={fileKey} label="ZIP 文件（最大 100 MiB）" type="file" accept=".zip,application/zip" required onChange={(event) => setFile(event.target.files?.[0] ?? null)} disabled={busy || needsRead} />
        <BusyButton type="submit" busy={busy && mutation.variables?.kind === 'upload'} disabled={disabled || !file}>上传并安装</BusyButton>
      </form>
    </div>
    <Confirm open={deleting !== null} title="删除主题" description={deleting ? `${deleting.manifest.name} ${deleting.manifest.version}` : ''} confirmLabel="删除" busy={busy} error={null} onCancel={() => setDeleting(null)} onConfirm={() => { if (deleting) mutation.mutate({ kind: 'delete', id: deleting.installation_id }); }} />
  </>;
}
