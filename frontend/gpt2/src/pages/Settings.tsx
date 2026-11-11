import { usePagedQuery } from '../api/pagination';
import { ContinueReading } from '../business-ui';
import { useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Button } from '../business-ui';
import { Fieldset, FieldsetLegend } from '@appica/ui-react/fieldset';
import { Radio } from '@appica/ui-react/radio';
import { RadioGroup } from '@appica/ui-react/radio-group';
import { useTheme } from '@appica/ui-react/hooks/use-theme';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { Skeleton } from '@appica/ui-react/skeleton';
import { allPages, api, condition, RequestError, unknownResult, unwrap } from '../api/client';
import type { components } from '../api/types';
import { finishSession, useSession } from '../auth';
import { queryClient } from '../state';
import { defaultNavigationOrder, navigation, useNavigation } from '../navigation';
import { BusyButton, date, Failure, LoadingRows, newRequestKey, PageTitle, TextField, useUnsavedChanges } from '../business-ui';
import { ConsoleThemes } from '../console-themes';
import { CommandError } from '../commands';
import { TerminalPreferences } from '../terminal';

export function Account() {
  const session = useSession();
  const query = useQuery({ queryKey: ['account'], queryFn: async ({ signal }) => unwrap(await api.GET('/settings/account', { signal })), staleTime: Infinity });
  const key = useRef('');
  const base = useRef(query.data);
  const [dirty, setDirty] = useState(false);
  const [needsRead, setNeedsRead] = useState(false);
  useUnsavedChanges(dirty);
  const mutation = useMutation({ gcTime: 0, mutationKey: ['account-change'], mutationFn: async (form: FormData) => {
    const body: components['schemas']['AccountChange'] = { current_password: String(form.get('current_password')), username: String(form.get('username')), entrance: String(form.get('entrance')) };
    if (form.get('password')) body.password = String(form.get('password'));
    key.current ||= newRequestKey();
    return unwrap(await api.PATCH('/settings/account', { params: { header: { ...condition(base.current ?? query.data), 'Idempotency-Key': key.current } }, body }));
  }, onSuccess: (data) => { setDirty(false); finishSession(data.login_address); }, onError: async (error, form) => {
    setNeedsRead(error instanceof RequestError && [409, 412, 428].includes(error.status));
    if (!unknownResult(error)) { key.current = ''; return; }
    const current = await api.GET('/auth/sessions/current').catch(() => undefined);
    if (current?.response.status === 401) { setDirty(false); finishSession(String(form.get('entrance')) || session.login_address); }
  } });
  const uncertain = mutation.isError && unknownResult(mutation.error);
  const read = async () => { const result = await query.refetch(); if (result.data && !result.isError) { base.current = result.data; key.current = ''; setNeedsRead(false); mutation.reset(); } };
  return <div className="page-stack"><PageTitle>账号</PageTitle><form className="max-w-lg editor-form" onChange={() => { if (!dirty) base.current = query.data; setDirty(true); }} onSubmit={(event) => { event.preventDefault(); if (!uncertain && !needsRead && !query.isError) mutation.mutate(new FormData(event.currentTarget)); }}><fieldset disabled={mutation.isPending || uncertain} className="editor-form">
    <TextField label="账号" name="username" defaultValue={session.session.username} autoComplete="username" required maxLength={128} />
    <TextField label="安全入口" name="entrance" defaultValue={session.login_address} required />
    <TextField label="新密码" name="password" type="password" autoComplete="new-password" minLength={8} maxLength={1024} />
    <TextField label="当前密码" name="current_password" type="password" autoComplete="current-password" required maxLength={1024} />
    </fieldset><Failure error={query.error}/><CommandError error={mutation.error} read={() => void read()}/>
    <BusyButton type="submit" busy={mutation.isPending} disabled={!query.data || query.isError || uncertain || needsRead}>保存并重新登录</BusyButton>
  </form></div>;
}

export function Sessions() {
  const session = useSession();
  const query = usePagedQuery({ queryKey: ['sessions'], queryFn: async ({ signal }) => allPages(async (cursor) => unwrap(await api.GET('/auth/sessions', { signal, params: { query: { cursor, limit: 100 } } }))) });
  const mutation = useMutation({ mutationFn: async (id: string) => { unwrap(await api.DELETE('/auth/sessions/{id}', { params: { path: { id }, header: { 'X-CSRF-Token': session.csrf_token } } })); return id; }, onSuccess: (id) => {
    if (id === session.session.id) finishSession(session.login_address); else void queryClient.invalidateQueries({ queryKey: ['sessions'] });
  } });
  return <div className="page-stack"><PageTitle>登录会话</PageTitle><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} /><Failure error={mutation.error} /><div className="overflow-x-auto" aria-busy={query.isPending}><Table className="min-w-[40rem]">
    <TableHeader><TableRow><TableHead>来源</TableHead><TableHead>登录时间</TableHead><TableHead>到期时间</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
    <TableBody>{query.isPending ? <LoadingRows columns={4} /> : query.data?.items.map((item) => <TableRow key={item.id}><TableCell>{item.remote_address}{item.current && <span className="ml-2 text-xs text-foreground-muted">当前</span>}</TableCell><TableCell className="whitespace-nowrap">{date(item.created_at)}</TableCell><TableCell className="whitespace-nowrap">{date(item.expires_at)}</TableCell><TableCell className="text-right"><BusyButton variant="ghost" size="sm" busy={mutation.isPending && mutation.variables === item.id} disabled={mutation.isPending || query.isError} onClick={() => mutation.mutate(item.id)}>退出会话</BusyButton></TableCell></TableRow>)}{!query.isError && query.data?.items.length === 0 && <TableRow><TableCell colSpan={4} className="py-8 text-center text-foreground-muted">暂无会话</TableCell></TableRow>}</TableBody>
  </Table></div><ContinueReading query={query}/></div>;
}

export function Appearance() {
  const { theme, setTheme, mounted } = useTheme();
  return <div className="page-stack"><PageTitle>外观</PageTitle><Fieldset className="border-t border-border pt-4"><FieldsetLegend className="section-title">配色</FieldsetLegend>
    <RadioGroup aria-label="配色" value={mounted ? theme : 'system'} onValueChange={(value) => setTheme(String(value))} orientation="horizontal" className="flex flex-wrap gap-x-10 gap-y-6">
      {[['light', '浅色'], ['dark', '深色'], ['system', '跟随系统']].map(([value, label]) => <label key={value} className="flex min-h-11 cursor-pointer items-center gap-3"><Radio value={value} /><span>{label}</span></label>)}
    </RadioGroup>
  </Fieldset><section className="appearance-terminal"><h2>终端</h2><TerminalPreferences/></section><NavigationSettings /><ConsoleThemes /></div>;
}

function NavigationSettings() {
  const nodeID = useSession().local_node_id;
  const query = useNavigation();
  const [draft, setDraft] = useState<components['schemas']['NavigationPreferences'] | null>(null);
  const [needsRead, setNeedsRead] = useState(false);
  useUnsavedChanges(draft !== null);
  const value = draft ?? query.data;
  const base = useRef(query.data);
  const save = useMutation({ mutationFn: async (input: components['schemas']['NavigationPreferences']) => unwrap(await api.PUT('/settings/navigation', { params: { header: condition(base.current ?? query.data) }, body: { order: input.order } })), onSuccess: (data) => {
    queryClient.setQueryData(['navigation', nodeID], data);
    setDraft(null);
    setNeedsRead(false);
  }, onError: () => setNeedsRead(true) });
  const change = (order: components['schemas']['NavigationItem'][]) => { if (!draft) base.current = query.data; setDraft({ ...value!, order }); save.reset(); };
  const move = (index: number, step: number) => {
    const order = [...value!.order];
    [order[index], order[index + step]] = [order[index + step], order[index]];
    change(order);
  };
  const read = async () => {
    const result = await query.refetch();
    if (!result.data || result.isError) return;
    setDraft((current) => current && (current.order.every((id, index) => id === result.data!.order[index]) ? null : { ...current, revision: result.data!.revision }));
    base.current = result.data;
    setNeedsRead(false);
    save.reset();
  };
  return <Fieldset className="mt-8 border-t border-border pt-4"><FieldsetLegend className="section-title">菜单顺序</FieldsetLegend>
    <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
    <ol aria-label="菜单顺序" aria-busy={query.isPending} className="max-w-lg divide-y divide-border">{(value?.order ?? defaultNavigationOrder).map((id, index) => {
      const label = navigation.find((item) => item.id === id)?.label ?? (id === 'tools' ? '工具' : id);
      return <li key={id} className="flex min-h-14 items-center justify-between gap-3 py-2"><span>{label}</span><div className="flex gap-2"><Button size="sm" variant="outline" aria-label={`${label}上移`} disabled={!value || save.isPending || index === 0} onClick={() => move(index, -1)}>上移</Button><Button size="sm" variant="outline" aria-label={`${label}下移`} disabled={!value || save.isPending || index === defaultNavigationOrder.length - 1} onClick={() => move(index, 1)}>下移</Button></div></li>;
    })}</ol>
    <Failure error={save.error} />
    <div className="mt-4 flex flex-wrap items-center gap-3"><Button variant="outline" disabled={!value || save.isPending} onClick={() => change([...defaultNavigationOrder])}>恢复默认</Button><BusyButton busy={save.isPending} disabled={!draft || needsRead || query.isError} onClick={() => save.mutate(draft!)}>保存</BusyButton>{needsRead && <BusyButton variant="outline" busy={query.isFetching} onClick={() => void read()}>重新读取</BusyButton>}{save.isSuccess && !draft && <span role="status" className="text-sm text-foreground-muted">已保存</span>}</div>
  </Fieldset>;
}

export function Version() {
  const query = useQuery({ queryKey: ['version'], queryFn: async ({ signal }) => unwrap(await api.GET('/', { signal })) });
  return <div className="page-stack"><PageTitle>版本</PageTitle><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} /><dl className="max-w-lg divide-y divide-border border-y border-border" aria-busy={query.isPending}>{[['节点', query.data?.node_version], ['界面', query.data?.frontend_version]].map(([name, version]) => <div key={name} className="flex justify-between gap-6 py-5"><dt className="text-foreground-muted">{name}</dt><dd className="font-medium">{query.isPending ? <Skeleton className="h-5 w-24" /> : version ?? '—'}</dd></div>)}</dl></div>;
}
