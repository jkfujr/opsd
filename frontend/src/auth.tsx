import { createContext, useContext, useEffect } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Navigate, Outlet, useLocation } from 'react-router';
import { Button } from '@appica/ui-react/button';
import { api, unwrap } from './api/client';
import type { components } from './api/schema.gen';
import { queryClient } from './state';
import { Failure, TextField } from './ui';

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
    const refreshNodes = () => { void queryClient.invalidateQueries({ queryKey: ['nodes'] }); void queryClient.invalidateQueries({ queryKey: ['tokens'] }); };
    const refresh = () => { refreshNodes(); void queryClient.invalidateQueries({ queryKey: ['sessions'] }); };
    events.onopen = refresh;
    events.addEventListener('invalidate', (event) => {
      try {
        const { resource } = JSON.parse((event as MessageEvent).data);
        if (resource === 'nodes') refreshNodes();
      } catch { refresh(); }
    });
    events.onerror = () => { if (queryClient.isMutating({ mutationKey: ['account-change'] }) === 0) void api.GET('/auth/session').catch(() => {}); };
    return () => events.close();
  }, [id]);
  if (session.isPending) return <div className="p-8 text-foreground-muted">加载中…</div>;
  if (session.error) return <div className="m-auto max-w-md space-y-4 p-8"><Failure error={session.error} /><Button onClick={() => void session.refetch()}>重试</Button></div>;
  if (!session.data) {
    if (location.pathname === '/' || location.pathname.startsWith('/settings')) return <p className="p-8">页面不存在</p>;
    return <Login entrance={location.pathname} />;
  }
  return <SessionContext value={session.data}><Outlet /></SessionContext>;
}

function Login({ entrance }: { entrance: string }) {
  const mutation = useMutation({ mutationFn: async (form: FormData) => unwrap(await api.POST('/auth/login', { body: { entrance, username: String(form.get('username')), password: String(form.get('password')) } })), onSuccess: (data) => {
    queryClient.setQueryData(['session'], data);
    window.location.replace('/');
  }});
  return <div className="grid min-h-screen place-items-center px-6 py-12"><main className="w-full max-w-sm">
    <div className="mb-12 border-l-4 border-primary pl-5"><span className="text-3xl font-semibold tracking-tight text-foreground-intense">OPSD</span></div>
    <h1 className="mb-8 text-xl font-semibold">登录</h1>
    <form className="space-y-6" onSubmit={(event) => { event.preventDefault(); mutation.mutate(new FormData(event.currentTarget)); }}>
      <TextField label="账号" name="username" autoComplete="username" required maxLength={128} />
      <TextField label="密码" name="password" type="password" autoComplete="current-password" required maxLength={1024} />
      <Failure error={mutation.error} />
      <Button type="submit" className="w-full" disabled={mutation.isPending}>{mutation.isPending ? '登录中…' : '登录'}</Button>
    </form>
  </main></div>;
}

export function AuthenticatedEntry() { return <Navigate to="/" replace />; }
