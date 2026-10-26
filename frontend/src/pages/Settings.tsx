import { useMutation, useQuery } from '@tanstack/react-query';
import { Button } from '@appica/ui-react/button';
import { Fieldset, FieldsetLegend } from '@appica/ui-react/fieldset';
import { Radio } from '@appica/ui-react/radio';
import { RadioGroup } from '@appica/ui-react/radio-group';
import { useTheme } from '@appica/ui-react/hooks/use-theme';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { api, unwrap } from '../api/client';
import type { components } from '../api/schema.gen';
import { useSession } from '../auth';
import { queryClient } from '../state';
import { date, Failure, PageTitle, TextField } from '../ui';

export function Account() {
  const session = useSession();
  const mutation = useMutation({ mutationKey: ['account-change'], mutationFn: async (form: FormData) => {
    const body: components['schemas']['AccountChange'] = { current_password: String(form.get('current_password')), username: String(form.get('username')), entrance: String(form.get('entrance')) };
    if (form.get('password')) body.password = String(form.get('password'));
    return unwrap(await api.PATCH('/settings/account', { params: { header: { 'X-CSRF-Token': session.csrf_token } }, body }));
  }, onSuccess: (data) => { queryClient.clear(); window.location.assign(data.login_address); } });
  return <><PageTitle>账号</PageTitle><form className="max-w-lg space-y-6" onSubmit={(event) => { event.preventDefault(); mutation.mutate(new FormData(event.currentTarget)); }}>
    <TextField label="账号" name="username" defaultValue={session.session.username} autoComplete="username" required maxLength={128} />
    <TextField label="安全入口" name="entrance" defaultValue={session.login_address} required />
    <TextField label="新密码" name="password" type="password" autoComplete="new-password" minLength={8} maxLength={1024} />
    <TextField label="当前密码" name="current_password" type="password" autoComplete="current-password" required maxLength={1024} />
    <Failure error={mutation.error} />
    <Button type="submit" disabled={mutation.isPending}>{mutation.isPending ? '保存中…' : '保存并重新登录'}</Button>
  </form></>;
}

export function Sessions() {
  const session = useSession();
  const query = useQuery({ queryKey: ['sessions'], queryFn: async ({ signal }) => unwrap(await api.GET('/auth/sessions', { signal })) });
  const mutation = useMutation({ mutationFn: async (id: string) => { unwrap(await api.DELETE('/auth/sessions/{id}', { params: { path: { id }, header: { 'X-CSRF-Token': session.csrf_token } } })); return id; }, onSuccess: (id) => {
    if (id === session.session.id) { queryClient.clear(); window.location.assign(session.login_address); } else void queryClient.invalidateQueries({ queryKey: ['sessions'] });
  } });
  return <><PageTitle>会话</PageTitle><Failure error={query.error || mutation.error} />{query.isPending ? <p className="text-foreground-muted">加载中…</p> : query.data && <div className="overflow-x-auto"><Table className="min-w-[40rem]">
    <TableHeader><TableRow><TableHead>来源</TableHead><TableHead>登录时间</TableHead><TableHead>到期时间</TableHead><TableHead className="text-right">操作</TableHead></TableRow></TableHeader>
    <TableBody>{query.data.items.map((item) => <TableRow key={item.id}><TableCell>{item.remote_address}{item.current && <span className="ml-2 text-xs text-foreground-muted">当前</span>}</TableCell><TableCell className="whitespace-nowrap">{date(item.created_at)}</TableCell><TableCell className="whitespace-nowrap">{date(item.expires_at)}</TableCell><TableCell className="text-right"><Button variant="ghost" size="sm" disabled={mutation.isPending} onClick={() => mutation.mutate(item.id)}>退出会话</Button></TableCell></TableRow>)}</TableBody>
  </Table></div>}</>;
}

export function Appearance() {
  const { theme, setTheme, mounted } = useTheme();
  return <><PageTitle>外观</PageTitle><Fieldset className="border-t border-border pt-6"><FieldsetLegend className="mb-5 text-sm font-medium text-foreground-strong">主题</FieldsetLegend>
    <RadioGroup aria-label="主题" value={mounted ? theme : 'system'} onValueChange={(value) => setTheme(String(value))} orientation="horizontal" className="flex flex-wrap gap-x-10 gap-y-6">
      {[['light', '浅色'], ['dark', '深色'], ['system', '跟随系统']].map(([value, label]) => <label key={value} className="flex min-h-11 cursor-pointer items-center gap-3"><Radio value={value} /><span>{label}</span></label>)}
    </RadioGroup>
  </Fieldset></>;
}

export function Version() {
  const query = useQuery({ queryKey: ['version'], queryFn: async ({ signal }) => unwrap(await api.GET('/settings/version', { signal })) });
  return <><PageTitle>版本</PageTitle><Failure error={query.error} />{query.isPending ? <p className="text-foreground-muted">加载中…</p> : query.data && <dl className="max-w-lg divide-y divide-border border-y border-border">{[['控制端', query.data.control_version], ['界面', query.data.frontend_version]].map(([name, version]) => <div key={name} className="flex justify-between gap-6 py-5"><dt className="text-foreground-muted">{name}</dt><dd className="font-medium">{version}</dd></div>)}</dl>}</>;
}
