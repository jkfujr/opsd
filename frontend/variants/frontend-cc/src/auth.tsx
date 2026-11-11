import { createContext, useContext, useEffect } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Navigate, Outlet, useLocation } from 'react-router';
import { api, unwrap } from './api/client';
import type { components } from './api/schema.gen';
import { queryClient } from './state';
import { BusyButton, Button, Failure, Spinner, TextField } from './ui';

const SessionContext = createContext<components['schemas']['CurrentSession'] | null>(null);

export function useSession() {
  return useContext(SessionContext)!;
}

export function SessionBoundary() {
  const location = useLocation();
  const session = useQuery({
    queryKey: ['session'],
    retry: false,
    queryFn: async ({ signal }) => {
      const result = await api.GET('/auth/session', { signal });
      if (result.response.status === 401) return null;
      return unwrap(result);
    },
  });

  const id = session.data?.session.id;

  useEffect(() => {
    if (!id) return;
    const events = new EventSource('/api/v1/events', { withCredentials: true });
    const seen = new Map<string, number>();
    let connected = false;
    const refresh = () => {
      void queryClient.invalidateQueries({ type: 'active' }, { cancelRefetch: false });
    };
    events.addEventListener('ready', () => {
      if (connected) {
        seen.clear();
        refresh();
      }
      connected = true;
    });
    events.addEventListener('invalidate', (event) => {
      try {
        const data = JSON.parse((event as MessageEvent).data);
        const changes: { resource: string; node_id?: string; revision?: number }[] =
          data.changes ?? [data];
        const predicates: ((key: readonly unknown[]) => boolean)[] = [];
        for (const { resource, node_id, revision } of changes) {
          const key = `${resource}/${node_id ?? ''}`;
          if (revision && revision <= (seen.get(key) ?? 0)) continue;
          if (revision) seen.set(key, revision);
          predicates.push(
            (key) =>
              resource === 'all' ||
              (resource === 'nodes' &&
                key[0] === 'nodes' &&
                key.length <= 2 &&
                (!node_id || key.length === 1 || key[1] === node_id)) ||
              (['operations', 'containers', 'databases', 'tokens', 'sessions', 'monitoring', 'tasks', 'compose'].includes(
                resource,
              ) &&
                key[0] === resource) ||
              (resource === 'firewall' &&
                key[0] === 'firewall' &&
                (!node_id || key[1] === node_id)) ||
              (resource === 'navigation' &&
                key[0] === 'navigation' &&
                key[1] === node_id) ||
              (['settings', 'account'].includes(resource) &&
                key[0] === 'nodes' &&
                key[1] === node_id &&
                (key[2] === resource || (resource === 'settings' && key[2] === 'storage'))) ||
              (['peer_trust', 'nodes'].includes(resource) && key[0] === 'peer-trust') ||
              (resource === 'public_addresses' &&
                key[0] === 'public-addresses' &&
                (!node_id || key[1] === node_id)) ||
              (['nodes', 'firewall', 'operations', 'monitoring'].includes(resource) &&
                key[0] === 'overview'),
          );
        }
        if (predicates.length) {
          void queryClient.invalidateQueries(
            { predicate: (q) => predicates.some((matches) => matches(q.queryKey)) },
            { cancelRefetch: false },
          );
        }
      } catch {
        /* 无效事件不触发全页面重复读取 */
      }
    });
    events.onerror = () => {
      if (queryClient.isMutating({ mutationKey: ['account-change'] }) === 0) {
        void api.GET('/auth/session').catch(() => {});
      }
    };
    return () => events.close();
  }, [id]);

  if (session.isPending) {
    return (
      <div className="grid min-h-screen place-items-center bg-[var(--bg-canvas)]">
        <Spinner aria-label="正在检查会话" />
      </div>
    );
  }

  if (session.error) {
    return (
      <div className="m-auto max-w-md space-y-4 p-8">
        <Failure error={session.error} />
        <Button onClick={() => void session.refetch()}>重试</Button>
      </div>
    );
  }

  if (!session.data) {
    if (
      location.pathname === '/' ||
      location.pathname.startsWith('/firewall') ||
      location.pathname.startsWith('/nodes') ||
      location.pathname.startsWith('/containers') ||
      location.pathname.startsWith('/databases') ||
      location.pathname.startsWith('/tools') ||
      location.pathname.startsWith('/settings') ||
      location.pathname.startsWith('/storage')
    ) {
      return (
        <div className="grid min-h-screen place-items-center bg-[var(--bg-canvas)] p-6">
          <div className="swiss-board w-full max-w-sm p-6">
            <p className="mono-num text-sm font-bold text-[var(--ink-strong)]">404 · 页面不存在</p>
          </div>
        </div>
      );
    }
    return <Login entrance={location.pathname} />;
  }

  return (
    <SessionContext value={session.data}>
      <Outlet />
    </SessionContext>
  );
}

function Login({ entrance }: { entrance: string }) {
  const mutation = useMutation({
    mutationFn: async (form: FormData) =>
      unwrap(
        await api.POST('/auth/login', {
          body: {
            entrance,
            username: String(form.get('username')),
            password: String(form.get('password')),
          },
        }),
      ),
    onSuccess: (data) => {
      queryClient.setQueryData(['session'], data);
      window.location.replace('/');
    },
  });

  return (
    <div className="grid min-h-screen place-items-center bg-[var(--bg-canvas)] p-4 sm:p-6">
      <main className="swiss-board w-full max-w-sm overflow-hidden">
        <div className="flex items-center justify-between border-b border-[var(--grid-strong)] bg-[var(--rail-bg)] px-5 py-3.5 text-white">
          <span className="text-lg font-extrabold tracking-tight">OPSD</span>
          <span className="mono-num bg-[var(--signal-red)] px-2 py-0.5 text-xs font-bold text-white">
            AUTH
          </span>
        </div>
        <div className="p-5 sm:p-6">
          <div className="mb-5 flex items-baseline justify-between border-b border-[var(--grid-rule)] pb-3">
            <h1 className="text-base font-bold text-[var(--ink-strong)]">登录</h1>
            <span className="mono-num max-w-48 truncate text-xs text-[var(--ink-muted)]" title={entrance}>
              {entrance}
            </span>
          </div>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              mutation.mutate(new FormData(event.currentTarget));
            }}
          >
            <TextField
              label="账号"
              name="username"
              autoComplete="username"
              required
              maxLength={128}
            />
            <TextField
              label="密码"
              name="password"
              type="password"
              autoComplete="current-password"
              required
              maxLength={1024}
            />
            <Failure error={mutation.error} />
            <BusyButton type="submit" className="w-full" busy={mutation.isPending}>
              登录
            </BusyButton>
          </form>
        </div>
      </main>
    </div>
  );
}

export function AuthenticatedEntry() {
  return <Navigate to="/" replace />;
}
