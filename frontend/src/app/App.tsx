import { lazy, Suspense, useEffect, type ReactNode } from 'react';
import { createBrowserRouter, Navigate, Outlet, useLocation, useSearchParams } from 'react-router';
import { RouterProvider } from 'react-router/dom';
import { AnimatePresence, motion } from 'motion/react';
import { BUILTIN, inBuiltin, SessionProvider, useSessionQuery } from './session';
import { Shell } from './Shell';
import { Login } from '../pages/Login';
import { Overview } from '../pages/Overview';
import { Spinner, ErrorNotice } from '../ui/kit';
import { Logo } from '../ui/logo';

const Nodes = lazy(() => import('../pages/Nodes').then((m) => ({ default: m.Nodes })));
const NodeDetail = lazy(() => import('../pages/NodeDetail').then((m) => ({ default: m.NodeDetail })));
const Containers = lazy(() => import('../pages/Containers').then((m) => ({ default: m.Containers })));
const Databases = lazy(() => import('../pages/Databases').then((m) => ({ default: m.Databases })));
const DatabaseDetail = lazy(() => import('../pages/DatabaseDetail').then((m) => ({ default: m.DatabaseDetail })));
const Firewall = lazy(() => import('../pages/Firewall').then((m) => ({ default: m.Firewall })));
const Terminal = lazy(() => import('../pages/Terminal').then((m) => ({ default: m.Terminal })));
const Tasks = lazy(() => import('../pages/Tasks').then((m) => ({ default: m.Tasks })));
const Monitoring = lazy(() => import('../pages/Monitoring').then((m) => ({ default: m.Monitoring })));
const Storage = lazy(() => import('../pages/Storage').then((m) => ({ default: m.Storage })));
const Operations = lazy(() => import('../pages/Operations').then((m) => ({ default: m.Operations })));
const Settings = lazy(() => import('../pages/Settings').then((m) => ({ default: m.Settings })));

function Root() {
  const query = useSessionQuery();
  const location = useLocation();
  useEffect(() => { document.title = 'OPSD'; }, []);
  if (query.isPending) return <Splash />;
  if (query.error) return <div className="center" style={{ height: '100%', padding: 24 }}><div style={{ width: 'min(480px, 100%)' }}><ErrorNotice error={query.error} title="无法连接控制台" retry={() => void query.refetch()} /></div></div>;
  return (
    <AnimatePresence mode="wait">
      {query.data ? (
        <motion.div key="app" style={{ height: '100%' }} initial={{ opacity: 0, scale: 0.985 }} animate={{ opacity: 1, scale: 1 }} transition={{ duration: 0.5, ease: [0.22, 1, 0.36, 1] }}>
          <SessionProvider value={query.data}><Shell><Outlet /></Shell></SessionProvider>
        </motion.div>
      ) : (
        <motion.div key="login" style={{ height: '100%' }} exit={{ opacity: 0, scale: 1.02, filter: 'blur(6px)' }} transition={{ duration: 0.35 }}>
          <Login entrance={location.pathname} />
        </motion.div>
      )}
    </AnimatePresence>
  );
}

function Splash() {
  return (
    <div className="center" style={{ height: '100%' }}>
      <motion.div initial={{ opacity: 0, scale: 0.9 }} animate={{ opacity: 1, scale: 1 }} transition={{ delay: 0.15 }} className="col" style={{ alignItems: 'center', gap: 14 }}>
        <Logo size={44} spinning />
      </motion.div>
    </div>
  );
}

function Page({ children }: { children: ReactNode }) {
  return <Suspense fallback={<div className="center" style={{ padding: 80 }}><Spinner size={20} /></div>}>{children}</Suspense>;
}

const router = createBrowserRouter([
  {
    path: '/',
    element: <Root />,
    children: [
      { index: true, element: <Overview /> },
      { path: 'nodes', element: <Page><Nodes /></Page> },
      { path: 'nodes/:id', element: <Page><NodeDetail /></Page> },
      { path: 'monitoring', element: <Page><Monitoring /></Page> },
      { path: 'containers', element: <Page><Containers /></Page> },
      { path: 'databases', element: <Page><Databases /></Page> },
      { path: 'databases/:id', element: <Page><DatabaseDetail /></Page> },
      { path: 'storage', element: <Page><Storage /></Page> },
      { path: 'firewall', element: <Page><Firewall /></Page> },
      { path: 'tools', element: <Navigate to="/tools/tasks" replace /> },
      { path: 'tools/terminal', element: <Page><Terminal /></Page> },
      { path: 'tools/tasks', element: <Page><Tasks /></Page> },
      { path: 'tools/operations', element: <Page><Operations /></Page> },
      // 兼容旧地址：保留查询参数与锚点。
      { path: 'terminal', element: <Alias to="/tools/terminal" /> },
      { path: 'tasks', element: <Alias to="/tools/tasks" /> },
      { path: 'operations', element: <Alias to="/tools/operations" /> },
      { path: 'firewall/settings', element: <FirewallSettingsAlias /> },
      { path: 'settings', element: <Navigate to="/settings/account" replace /> },
      { path: 'settings/:section', element: <Page><Settings /></Page> },
      { path: '*', element: <Overview /> },
    ],
  },
], { basename: inBuiltin ? BUILTIN : '/' });

function Alias({ to }: { to: string }) {
  const { search, hash } = useLocation();
  return <Navigate to={{ pathname: to, search, hash }} replace />;
}
function FirewallSettingsAlias() {
  const [params] = useSearchParams();
  params.set('tab', 'trust');
  return <Navigate to={{ pathname: '/firewall', search: `?${params}` }} replace />;
}

export function App() {
  return <RouterProvider router={router} />;
}
