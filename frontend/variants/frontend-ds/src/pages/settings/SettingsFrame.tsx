import { NavLink, Outlet } from 'react-router';
import { PageHead } from '../../components/ui';

const items = [
  { path: '', label: '账号', end: true },
  { path: 'sessions', label: '会话' },
  { path: 'api', label: 'API' },
  { path: 'appearance', label: '外观' },
  { path: 'version', label: '版本' },
];

export function SettingsFrame() {
  return (
    <div className="page">
      <PageHead title="设置" />
      <div className="settings">
        <nav className="settings__nav" aria-label="设置分类">
          {items.map((item) => (
            <NavLink key={item.path} to={item.path} end={item.end}>
              {item.label}
            </NavLink>
          ))}
        </nav>
        <Outlet />
      </div>
    </div>
  );
}
