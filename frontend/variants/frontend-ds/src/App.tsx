import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { createBrowserRouter } from 'react-router';
import { RouterProvider } from 'react-router/dom';
import { api, message, value, type Schema } from './api/client';
import { queryClient } from './state';
import { Layout } from './components/Shell';
import { OperationsProvider } from './components/Operations';
import { Button, Failure, SessionProvider, TextInput } from './components/ui';
import { Overview } from './pages/Overview';
import { Nodes } from './pages/Nodes';
import { NodeDetail } from './pages/NodeDetail';
import { Containers } from './pages/Containers';
import { Databases } from './pages/Databases';
import { DatabaseDetail } from './pages/DatabaseDetail';
import { Firewall } from './pages/Firewall';
import { FirewallSettings } from './pages/FirewallSettings';
import { Storage } from './pages/Storage';
import { SettingsFrame } from './pages/settings/SettingsFrame';
import { Account as AccountSettings } from './pages/settings/Account';
import { Sessions } from './pages/settings/Sessions';
import { APIPanel } from './pages/settings/APIPanel';
import { Appearance } from './pages/settings/Appearance';
import { Version } from './pages/settings/Version';

const router = createBrowserRouter([
  {
    path: '/',
    element: (
      <OperationsProvider>
        <Layout />
      </OperationsProvider>
    ),
    children: [
      { index: true, element: <Overview /> },
      { path: 'nodes', element: <Nodes /> },
      { path: 'nodes/:id', element: <NodeDetail /> },
      { path: 'containers', element: <Containers /> },
      { path: 'databases', element: <Databases /> },
      { path: 'databases/:id', element: <DatabaseDetail /> },
      { path: 'firewall', element: <Firewall /> },
      { path: 'firewall/settings', element: <FirewallSettings /> },
      { path: 'storage', element: <Storage /> },
      {
        path: 'settings',
        element: <SettingsFrame />,
        children: [
          { index: true, element: <AccountSettings /> },
          { path: 'sessions', element: <Sessions /> },
          { path: 'api', element: <APIPanel /> },
          { path: 'appearance', element: <Appearance /> },
          { path: 'version', element: <Version /> },
        ],
      },
    ],
  },
]);

function Boot() {
  return (
    <div className="boot">
      <div className="boot__mark">OPSD</div>
      <div className="boot__line" />
      <span className="kicker">读取本机会话</span>
    </div>
  );
}

function Login() {
  const [entrance, setEntrance] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const login = useMutation({
    mutationFn: async () =>
      value(
        await api.POST('/auth/login', {
          body: { entrance, username, password },
        }),
      ),
    onSuccess: (session) => {
      queryClient.setQueryData(['session'], session);
      window.location.assign('/');
    },
  });
  return (
    <div className="login">
      <div className="login__sheet">
        <div className="login__head">
          <strong>OPSD</strong>
          <span className="kicker">对等节点调度台</span>
        </div>
        <form
          className="stack gap-16"
          onSubmit={(event) => {
            event.preventDefault();
            login.mutate();
          }}
        >
          <TextInput label="登录入口" value={entrance} onChange={(e) => setEntrance(e.target.value)} autoComplete="username" className="input--mono" placeholder="/console" />
          <TextInput label="账号" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
          <TextInput label="密码" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
          <Failure error={login.error} />
          <Button variant="solid" type="submit" busy={login.isPending} disabled={!entrance || !username || !password} style={{ height: 38 }}>
            进入本机控制台
          </Button>
        </form>
      </div>
    </div>
  );
}

export function App() {
  const session = useQuery({
    queryKey: ['session'],
    queryFn: async ({ signal }) => value(await api.GET('/auth/session', { signal })),
    retry: false,
    staleTime: Infinity,
  });

  if (session.isPending) return <Boot />;
  if (session.error) {
    const status = (session.error as { status?: number }).status;
    if (status === 401 || status === 403) return <Login />;
    return (
      <div className="boot">
        <div className="boot__mark">OPSD</div>
        <p style={{ maxWidth: 420, textAlign: 'center', fontSize: 13, color: 'var(--ink-soft)' }}>{message(session.error)}</p>
        <Button onClick={() => void session.refetch()}>重新读取</Button>
      </div>
    );
  }
  return (
    <SessionProvider value={session.data as Schema['CurrentSession']}>
      <RouterProvider router={router} />
    </SessionProvider>
  );
}

