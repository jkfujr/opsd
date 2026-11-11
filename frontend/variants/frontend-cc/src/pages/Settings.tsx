import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, unwrap } from '../api/client';
import type { components } from '../api/schema.gen';
import { useSession } from '../auth';
import { queryClient } from '../state';
import { defaultNavigationOrder, navigation, useNavigation } from '../navigation';
import { useTheme, type ThemeMode } from '../theme';
import {
  BusyButton,
  Button,
  date,
  Failure,
  LoadingRows,
  PageTitle,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TextField,
  useUnsavedChanges,
} from '../ui';

export function Account() {
  const session = useSession();
  const [dirty, setDirty] = useState(false);
  useUnsavedChanges(dirty);

  const mutation = useMutation({
    mutationKey: ['account-change'],
    mutationFn: async (form: FormData) => {
      const body: components['schemas']['AccountChange'] = {
        current_password: String(form.get('current_password')),
        username: String(form.get('username')),
        entrance: String(form.get('entrance')),
      };
      if (form.get('password')) body.password = String(form.get('password'));
      return unwrap(
        await api.PATCH('/settings/account', {
          params: { header: { 'X-CSRF-Token': session.csrf_token } },
          body,
        }),
      );
    },
    onSuccess: (data) => {
      setDirty(false);
      queryClient.clear();
      window.location.assign(data.login_address);
    },
  });

  return (
    <>
      <PageTitle>账号</PageTitle>
      <form
        className="max-w-lg space-y-4"
        onChange={() => setDirty(true)}
        onSubmit={(event) => {
          event.preventDefault();
          mutation.mutate(new FormData(event.currentTarget));
        }}
      >
        <TextField
          label="账号"
          name="username"
          defaultValue={session.session.username}
          autoComplete="username"
          required
          maxLength={128}
        />
        <TextField
          label="安全入口"
          name="entrance"
          defaultValue={session.login_address}
          required
          className="mono-num"
        />
        <TextField
          label="新密码"
          name="password"
          type="password"
          autoComplete="new-password"
          minLength={8}
          maxLength={1024}
        />
        <TextField
          label="当前密码"
          name="current_password"
          type="password"
          autoComplete="current-password"
          required
          maxLength={1024}
        />
        <Failure error={mutation.error} />
        <BusyButton type="submit" busy={mutation.isPending}>
          保存并重新登录
        </BusyButton>
      </form>
    </>
  );
}

export function Sessions() {
  const session = useSession();
  const query = useQuery({
    queryKey: ['sessions'],
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/auth/sessions', { signal })),
  });
  const mutation = useMutation({
    mutationFn: async (id: string) => {
      unwrap(
        await api.DELETE('/auth/sessions/{id}', {
          params: {
            path: { id },
            header: { 'X-CSRF-Token': session.csrf_token },
          },
        }),
      );
      return id;
    },
    onSuccess: (id) => {
      if (id === session.session.id) {
        queryClient.clear();
        window.location.assign(session.login_address);
      } else {
        void queryClient.invalidateQueries({ queryKey: ['sessions'] });
      }
    },
  });

  return (
    <>
      <PageTitle>会话</PageTitle>
      <Failure
        error={query.error}
        retry={() => void query.refetch()}
        busy={query.isFetching}
      />
      <Failure error={mutation.error} />
      <div aria-busy={query.isPending}>
        <Table className="min-w-[40rem]">
          <TableHeader>
            <TableRow>
              <TableHead>来源</TableHead>
              <TableHead>登录时间</TableHead>
              <TableHead>到期时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {query.isPending ? (
              <LoadingRows columns={4} />
            ) : (
              query.data?.items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="mono-num font-semibold text-[var(--ink-strong)]">
                    {item.remote_address}
                    {item.current && (
                      <span className="ml-2 border border-[var(--signal-emerald)] bg-[var(--signal-emerald-bg)] px-1.5 py-0.5 text-xs font-bold text-[var(--signal-emerald-ink)]">
                        当前
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="mono-num whitespace-nowrap text-xs">
                    {date(item.created_at)}
                  </TableCell>
                  <TableCell className="mono-num whitespace-nowrap text-xs">
                    {date(item.expires_at)}
                  </TableCell>
                  <TableCell className="text-right">
                    <BusyButton
                      variant="ghost"
                      size="sm"
                      busy={mutation.isPending && mutation.variables === item.id}
                      disabled={mutation.isPending || query.isError}
                      onClick={() => mutation.mutate(item.id)}
                    >
                      退出会话
                    </BusyButton>
                  </TableCell>
                </TableRow>
              ))
            )}
            {!query.isError && query.data?.items.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={4}
                  className="py-8 text-center text-[var(--ink-muted)]"
                >
                  暂无会话
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </>
  );
}

export function Appearance() {
  const { theme, setTheme, mounted } = useTheme();
  const current = mounted ? theme : 'system';

  return (
    <>
      <PageTitle>外观</PageTitle>
      <fieldset className="form-section border-t-0 pt-0">
        <legend className="section-title">主题</legend>
        <div
          role="radiogroup"
          aria-label="主题"
          className="flex flex-wrap gap-3"
        >
          {(
            [
              ['light', '浅色'],
              ['dark', '深色'],
              ['system', '跟随系统'],
            ] as const
          ).map(([value, label]) => {
            const active = current === value;
            return (
              <label
                key={value}
                className={`flex min-h-10 cursor-pointer items-center gap-2.5 border px-4 py-2 text-sm font-semibold transition-colors ${
                  active
                    ? 'border-[var(--grid-strong)] bg-[var(--ink-strong)] text-[var(--bg-surface)]'
                    : 'border-[var(--grid-rule)] bg-[var(--bg-surface)] text-[var(--ink-body)] hover:bg-[var(--bg-elevated)]'
                }`}
              >
                <input
                  type="radio"
                  name="theme-mode"
                  value={value}
                  checked={active}
                  onChange={() => setTheme(value as ThemeMode)}
                  className="sr-only"
                />
                <span
                  aria-hidden="true"
                  className={`size-2 ${
                    active ? 'bg-[var(--signal-red)]' : 'bg-[var(--grid-rule)]'
                  }`}
                />
                <span>{label}</span>
              </label>
            );
          })}
        </div>
      </fieldset>
      <NavigationSettings />
    </>
  );
}

function NavigationSettings() {
  const nodeID = useSession().local_node_id;
  const query = useNavigation();
  const [draft, setDraft] = useState<
    components['schemas']['NavigationPreferences'] | null
  >(null);
  const [needsRead, setNeedsRead] = useState(false);
  useUnsavedChanges(draft !== null);
  const value = draft ?? query.data;

  const save = useMutation({
    mutationFn: async (
      input: components['schemas']['NavigationPreferences'],
    ) => unwrap(await api.PATCH('/settings/navigation', { body: input })),
    onSuccess: (data) => {
      queryClient.setQueryData(['navigation', nodeID], data);
      setDraft(null);
      setNeedsRead(false);
    },
    onError: () => setNeedsRead(true),
  });

  const change = (order: components['schemas']['NavigationItem'][]) => {
    setDraft({ ...value!, order });
    save.reset();
  };

  const move = (index: number, step: number) => {
    const order = [...value!.order];
    [order[index], order[index + step]] = [order[index + step], order[index]];
    change(order);
  };

  const read = async () => {
    const result = await query.refetch();
    if (!result.data || result.isError) return;
    setDraft((current) =>
      current &&
      (current.order.every((id, index) => id === result.data!.order[index])
        ? null
        : { ...current, revision: result.data!.revision }),
    );
    setNeedsRead(false);
    save.reset();
  };

  return (
    <fieldset className="mt-8 border-t border-[var(--grid-rule)] pt-5">
      <legend className="section-title">菜单顺序</legend>
      <Failure
        error={query.error}
        retry={() => void query.refetch()}
        busy={query.isFetching}
      />
      <ol
        aria-label="菜单顺序"
        aria-busy={query.isPending}
        className="max-w-lg divide-y divide-[var(--grid-rule)] border border-[var(--grid-rule)]"
      >
        {(value?.order ?? defaultNavigationOrder).map((id, index) => {
          const label =
            navigation.find((item) => item.id === id)?.label ??
            (id === 'tools' ? '工具' : id);
          return (
            <li
              key={id}
              className="flex min-h-12 items-center justify-between gap-3 px-3.5 py-2"
            >
              <span className="font-semibold text-[var(--ink-strong)]">
                {label}
              </span>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  aria-label={`${label}上移`}
                  disabled={!value || save.isPending || index === 0}
                  onClick={() => move(index, -1)}
                >
                  上移
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  aria-label={`${label}下移`}
                  disabled={
                    !value ||
                    save.isPending ||
                    index === defaultNavigationOrder.length - 1
                  }
                  onClick={() => move(index, 1)}
                >
                  下移
                </Button>
              </div>
            </li>
          );
        })}
      </ol>
      <Failure error={save.error} />
      <div className="mt-4 flex flex-wrap items-center gap-3">
        <Button
          variant="outline"
          disabled={!value || save.isPending}
          onClick={() => change([...defaultNavigationOrder])}
        >
          恢复默认
        </Button>
        <BusyButton
          busy={save.isPending}
          disabled={!draft || needsRead || query.isError}
          onClick={() => save.mutate(draft!)}
        >
          保存
        </BusyButton>
        {needsRead && (
          <BusyButton
            variant="outline"
            busy={query.isFetching}
            onClick={() => void read()}
          >
            重新读取
          </BusyButton>
        )}
        {save.isSuccess && !draft && (
          <span role="status" className="text-sm text-[var(--signal-emerald)]">
            已保存
          </span>
        )}
      </div>
    </fieldset>
  );
}

export function Version() {
  const query = useQuery({
    queryKey: ['version'],
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/settings/version', { signal })),
  });

  return (
    <>
      <PageTitle>版本</PageTitle>
      <Failure
        error={query.error}
        retry={() => void query.refetch()}
        busy={query.isFetching}
      />
      <dl
        className="max-w-lg divide-y divide-[var(--grid-rule)] border-y border-[var(--grid-rule)]"
        aria-busy={query.isPending}
      >
        {[
          ['节点', query.data?.node_version],
          ['界面', query.data?.frontend_version],
        ].map(([name, version]) => (
          <div key={name} className="flex justify-between gap-6 py-4">
            <dt className="font-bold text-[var(--ink-muted)]">{name}</dt>
            <dd className="mono-num font-bold text-[var(--ink-strong)]">
              {query.isPending ? (
                <Skeleton className="h-5 w-24" />
              ) : (
                version ?? '—'
              )}
            </dd>
          </div>
        ))}
      </dl>
    </>
  );
}
