import { createContext, useContext, useEffect, useRef, type ReactNode } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Outlet, useLocation } from 'react-router';
import type { components } from './api/types';
import { api, unwrap, useRetryAfter } from './api/client';
import { queryClient } from './state';
import { clearPendingRequests } from './api/requests';
import { Btn, Field, Heading, Panel } from './ui';
import { Failure } from './business-ui';
import { inBuiltinConsole, isConsolePage, loginAddress, loginDestination } from './location';
const Context = createContext<components['schemas']['CurrentSession'] | null>(null);
export function useSession() { const value = useContext(Context); if (!value) throw new Error('需要管理员登录'); return value; }
let endingSession = false;
export function finishSession(address: string) {
  if (endingSession) return;
  endingSession = true;
  window.dispatchEvent(new Event('opsd:session-clearing'));
  clearPendingRequests();
  void queryClient.cancelQueries();
  queryClient.clear();
  window.location.assign(loginAddress(address));
}
export function SessionBoundary() {
  const location = useLocation();
  const query = useQuery({ queryKey: ['session'], retry: false, queryFn: async ({ signal }) => { const result = await api.GET('/auth/sessions/current', { signal }); return result.response.status === 401 ? null : unwrap(result); } });
  const session = query.data, login = useRef('');
  if (session) login.current = session.login_address;
  useEffect(() => {
    const expired = () => { if (login.current) finishSession(login.current); else void query.refetch(); };
    window.addEventListener('opsd:session-ended', expired); return () => window.removeEventListener('opsd:session-ended', expired);
  }, []);
  useEffect(() => {
    if (!session) return;
    const events = new EventSource('/api/v2/events', { withCredentials: true });
    const revisions = new Map<string, number>(); let connected = false;
    events.addEventListener('ready', () => { if (connected) { revisions.clear(); void queryClient.invalidateQueries({ type: 'active' }, { cancelRefetch: false }); } connected = true; });
    events.addEventListener('invalidate', event => {
      try {
        const data = JSON.parse((event as MessageEvent).data);
        const changes: { resource: string; node_id?: string; revision?: number }[] = data.changes ?? [data];
        const fresh = changes.filter(change => { const id = `${change.resource}/${change.node_id ?? ''}`; if (change.revision && change.revision <= (revisions.get(id) ?? 0)) return false; if (change.revision) revisions.set(id, change.revision); return true; });
        const related: Record<string, string[]> = {
          nodes: ['nodes', 'overview', 'peer-trust', 'containers', 'compose', 'tasks', 'databases'], monitoring: ['overview', 'monitoring'], settings: ['nodes', 'api-tokens'], account: ['nodes', 'account'],
          operations: ['operations', 'overview', 'containers', 'compose', 'tasks'], firewall: ['firewall', 'overview', 'peer-trust'], peer_trust: ['peer-trust', 'firewall'],
          public_addresses: ['public-addresses', 'nodes'], tokens: ['tokens', 'api-tokens'], containers: ['containers', 'compose'], databases: ['databases'],
          tasks: ['tasks'], compose: ['compose', 'containers'], navigation: ['navigation'], sessions: ['sessions'], themes: ['themes'],
        };
        void queryClient.invalidateQueries({ predicate: q => fresh.some(({ resource, node_id }) => {
          const [kind, target] = q.queryKey;
          if (resource === 'all') return true;
          if (!related[resource]?.includes(String(kind))) return false;
          if (!node_id) return true;
          if (kind === 'navigation') return target === node_id;
          if (['themes', 'sessions', 'tokens', 'api-tokens', 'account'].includes(String(kind))) return node_id === session.local_node_id;
          if (kind === 'nodes') return q.queryKey.length === 1 || target === node_id;
          if (['firewall', 'public-addresses', 'peer-trust'].includes(String(kind))) return !target || target === node_id;
          if (['containers', 'compose', 'tasks'].includes(String(kind))) return !target || typeof target === 'object' && target !== null && 'node_id' in target && target.node_id === node_id || target === node_id;
          return true;
        }) }, { cancelRefetch: false });
      } catch { /* 无效通知不触发重复读取。 */ }
    });
    events.onerror = () => { if (!queryClient.isMutating({ mutationKey: ['account-change'] })) void api.GET('/auth/sessions/current').catch(() => {}); };
    return () => events.close();
  }, [session?.session.id]);
  if (query.isPending) return <Entry><p role="status">正在检查会话…</p></Entry>;
  if (query.error) return <Entry><Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching}/></Entry>;
  if (!session) return inBuiltinConsole || isConsolePage(location.pathname) ? <Entry><Heading title="页面不存在"/></Entry> : <Login entrance={window.location.pathname}/>;
  return <Context value={session}><Outlet/></Context>;
}
function Entry({ children }: { children: ReactNode }) { return <main className="auth-entry"><Panel><div className="auth-content">{children}</div></Panel></main>; }
function Login({ entrance }: { entrance: string }) {
  const mutation = useMutation({ gcTime: 0, mutationFn: async (form: FormData) => unwrap(await api.POST('/auth/sessions', { body: { entrance, username: String(form.get('username')), password: String(form.get('password')) } })), onSuccess: data => { queryClient.setQueryData(['session'], data); window.location.replace(loginDestination()); } });
  const cooldown = useRetryAfter(mutation.error);
  return <Entry><Heading title="登录"/><form className="editor-form" onSubmit={event => { event.preventDefault(); mutation.mutate(new FormData(event.currentTarget)); }}>
    <Field label="账号" name="username" required maxLength={128} autoComplete="username"/><Field label="密码" name="password" type="password" required maxLength={1024} autoComplete="current-password"/>
    <Failure error={mutation.error}/><Btn variant="primary" type="submit" disabled={mutation.isPending || cooldown > 0} aria-busy={mutation.isPending}>{mutation.isPending ? '正在登录…' : '登录'}</Btn>
  </form></Entry>;
}
