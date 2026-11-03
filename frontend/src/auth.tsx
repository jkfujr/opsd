import { createContext, useContext, useEffect } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Navigate, Outlet, useLocation } from 'react-router';
import { Button } from '@appica/ui-react/button';
import { Spinner } from '@appica/ui-react/spinner';
import { api, unwrap } from './api/client';
import type { components } from './api/schema.gen';
import { queryClient } from './state';
import { BusyButton, Failure, TextField } from './ui';

const SessionContext = createContext<components['schemas']['CurrentSession'] | null>(null);
export function useSession() { return useContext(SessionContext)!; }

export function SessionBoundary() {
  const location = useLocation();
  const session = useQuery({ queryKey: ['session'], retry: false, queryFn: async ({ signal }) => {
    const result = await api.GET('/auth/session', { signal });
    if (result.response.status === 401) return null;
    return unwrap(result);
  }});
  const id = session.data?.session.id;
  useEffect(() => {
    if (!id) return;
    const events = new EventSource('/api/v1/events', { withCredentials: true });
    const seen = new Map<string, number>();
    let connected = false;
    const refresh = () => { void queryClient.invalidateQueries({ type: 'active' }); };
    events.addEventListener('ready', () => { if (connected) { seen.clear(); refresh(); } connected = true; });
    events.addEventListener('invalidate', (event) => {
      try {
        const data = JSON.parse((event as MessageEvent).data);
        const changes: { resource: string; node_id?: string; revision?: number }[] = data.changes ?? [data];
        for (const { resource, node_id, revision } of changes) {
          const key = `${resource}/${node_id ?? ''}`;
          if (revision && revision <= (seen.get(key) ?? 0)) continue;
          if (revision) seen.set(key, revision);
          if (resource === 'all') { refresh(); continue; }
          if (resource === 'nodes') void queryClient.invalidateQueries({ predicate: (q) => q.queryKey[0] === 'nodes' && q.queryKey.length <= 2 && (!node_id || q.queryKey.length === 1 || q.queryKey[1] === node_id) });
          if (resource === 'operations') void queryClient.invalidateQueries({ queryKey: ['operations'] });
          if (resource === 'firewall') void queryClient.invalidateQueries({ queryKey: node_id ? ['firewall', node_id] : ['firewall'] });
          if (resource === 'settings' || resource === 'account') void queryClient.invalidateQueries({ queryKey: ['nodes', node_id, resource] });
          if (resource === 'settings') void queryClient.invalidateQueries({ queryKey: ['nodes', node_id, 'storage'] });
          if (['tokens', 'sessions', 'monitoring'].includes(resource)) void queryClient.invalidateQueries({ queryKey: [resource] });
          if (resource === 'peer_trust' || resource === 'nodes') void queryClient.invalidateQueries({ queryKey: ['peer-trust'] });
          if (resource === 'public_addresses') void queryClient.invalidateQueries({ queryKey: node_id ? ['public-addresses', node_id] : ['public-addresses'] });
          if (['nodes', 'firewall', 'operations', 'monitoring'].includes(resource)) void queryClient.invalidateQueries({ queryKey: ['overview'] });
        }
      } catch { /* 无效事件不触发全页面重复读取。 */ }
    });
    events.onerror = () => { if (queryClient.isMutating({ mutationKey: ['account-change'] }) === 0) void api.GET('/auth/session').catch(() => {}); };
    return () => events.close();
  }, [id]);
  if (session.isPending) return <div className="grid min-h-screen place-items-center"><Spinner aria-label="正在检查会话" /></div>;
  if (session.error) return <div className="m-auto max-w-md space-y-4 p-8"><Failure error={session.error} /><Button onClick={() => void session.refetch()}>重试</Button></div>;
  if (!session.data) {
    if (location.pathname === '/' || location.pathname.startsWith('/firewall') || location.pathname.startsWith('/nodes') || location.pathname.startsWith('/settings')) return <p className="p-8">页面不存在</p>;
    return <Login entrance={location.pathname} />;
  }
  return <SessionContext value={session.data}><Outlet /></SessionContext>;
}

function Login({ entrance }: { entrance: string }) {
  const mutation = useMutation({ mutationFn: async (form: FormData) => unwrap(await api.POST('/auth/login', { body: { entrance, username: String(form.get('username')), password: String(form.get('password')) } })), onSuccess: (data) => {
    queryClient.setQueryData(['session'], data);
    window.location.replace('/');
  }});
  return <div className="grid min-h-screen place-items-center p-4 sm:p-6"><main className="panel w-full max-w-sm">
    <div className="mb-6 text-xl font-semibold tracking-tight text-foreground-intense">OPSD</div>
    <h1 className="mb-4 text-base font-semibold">登录</h1>
    <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); mutation.mutate(new FormData(event.currentTarget)); }}>
      <TextField label="账号" name="username" autoComplete="username" required maxLength={128} />
      <TextField label="密码" name="password" type="password" autoComplete="current-password" required maxLength={1024} />
      <Failure error={mutation.error} />
      <BusyButton type="submit" className="w-full" busy={mutation.isPending}>登录</BusyButton>
    </form>
  </main></div>;
}

export function AuthenticatedEntry() { return <Navigate to="/" replace />; }
