import type { CSSProperties } from 'react';

const paths = {
  overview: <><rect x="3" y="3" width="7" height="7" rx="1.3"/><rect x="14" y="3" width="7" height="7" rx="1.3"/><rect x="3" y="14" width="7" height="7" rx="1.3"/><rect x="14" y="14" width="7" height="7" rx="1.3"/></>,
  nodes: <><rect x="3" y="3.5" width="18" height="7" rx="1.8"/><rect x="3" y="13.5" width="18" height="7" rx="1.8"/><path d="M7 7h.01M7 17h.01M12 7h5M12 17h5"/></>,
  containers: <><path d="m12 3 8.5 4.8v8.4L12 21l-8.5-4.8V7.8zM3.5 7.8 12 13l8.5-5.2M12 13v8M7.5 5.5l9 5"/></>,
  databases: <><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 4 16 4 16 0V5M4 12c0 4 16 4 16 0"/></>,
  storage: <><path d="m3 8 3-5h12l3 5v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zM3 8h18M7 13h10M7 17h5"/></>,
  firewall: <><path d="m12 3 8 3v5c0 5-8 10-8 10S4 16 4 11V6zM8.5 11.5l2.5 2.5 4.5-5"/></>,
  settings: <><path d="M3 7h18M3 17h18"/><circle cx="9" cy="7" r="3"/><circle cx="16" cy="17" r="3"/></>,
  search: <><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/></>,
  chevron: <path d="m9 5 7 7-7 7"/>,
  down: <path d="m6 9 6 6 6-6"/>,
  arrow: <path d="M4 12h16m-6-6 6 6-6 6"/>,
  external: <><path d="M13 4h7v7M20 4 10 14M10 4H5a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-5"/></>,
  refresh: <><path d="M20 8a8.5 8.5 0 0 0-15-2L3 9m0-5v5h5M4 16a8.5 8.5 0 0 0 15 2l2-3m0 5v-5h-5"/></>,
  plus: <path d="M12 5v14M5 12h14"/>,
  sun: <><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M5 19l1.5-1.5M17.5 6.5 19 5"/></>,
  moon: <path d="M20.5 14a8.5 8.5 0 0 1-10.5-10A8.5 8.5 0 1 0 20.5 14Z"/>,
  monitor: <><rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8M12 17v4"/></>,
  activity: <path d="M2 12h5l3-8 4 16 3-8h5"/>,
  clock: <><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></>,
  check: <path d="m5 12 4 4L19 6"/>,
  alert: <><path d="m12 3 10 18H2zM12 9v4M12 17h.01"/></>,
  info: <><circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/></>,
  close: <path d="m6 6 12 12M6 18 18 6"/>,
  menu: <path d="M4 6h16M4 12h16M4 18h16"/>,
  copy: <><rect x="8" y="8" width="12" height="13" rx="2"/><path d="M16 8V3H3v13h5"/></>,
  terminal: <><rect x="2.5" y="4" width="19" height="16" rx="2"/><path d="m6 9 4 3-4 3M13 15h5"/></>,
  play: <path d="m8 4 12 8-12 8z"/>,
  stop: <rect x="5" y="5" width="14" height="14" rx="1.5"/>,
  log: <><path d="M14 3H5v18h14V8zM14 3v5h5M8 12h8M8 16h6"/></>,
  more: <><circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/></>,
  sort: <><path d="M8 4v16m-4-4 4 4 4-4M16 20V4m-4 4 4-4 4 4"/></>,
  globe: <><circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4" ry="9"/><path d="M3 12h18"/></>,
  cpu: <><rect x="6" y="6" width="12" height="12" rx="2"/><rect x="9" y="9" width="6" height="6" rx="1"/><path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/></>,
  memory: <><rect x="3" y="6" width="18" height="12" rx="1.5"/><path d="M7 10v4M11 10v4M15 10v4M19 10v4M7 18v3M12 18v3M17 18v3"/></>,
  network: <><rect x="8" y="3" width="8" height="6" rx="1"/><rect x="2" y="16" width="8" height="5" rx="1"/><rect x="14" y="16" width="8" height="5" rx="1"/><path d="M12 9v4M6 16v-3h12v3"/></>,
  logout: <><path d="M9 4H4v16h5M9 12h12m-4-4 4 4-4 4"/></>,
  edit: <><path d="m15 4 5 5M4 20l5-1L21 7l-4-4L5 15z"/></>,
  trash: <><path d="M3 6h18M8 6V3h8v3M5 6l1 15h12l1-15M10 10v7M14 10v7"/></>,
  key: <><circle cx="8" cy="8" r="5"/><path d="m12 12 9 9m-4-4 3-3m-6 0 3-3"/></>,
  folder: <path d="M3 6a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v10H3z"/>,
  arrowDown: <path d="M12 4v16m-6-6 6 6 6-6"/>,
  arrowUp: <path d="M12 20V4m-6 6 6-6 6 6"/>,
};

export type IconName = keyof typeof paths;
export function Icon({ name, size = 18, className = '', style }: { name: IconName; size?: number; className?: string; style?: CSSProperties }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className={`icon ${className}`} style={style}>{paths[name]}</svg>;
}
export function BrandMark() {
  return <svg className="brand-mark" viewBox="0 0 32 32" fill="none" stroke="currentColor" strokeWidth="1.9" aria-hidden="true"><rect x="3" y="3" width="9" height="9" rx="1.5"/><rect x="20" y="20" width="9" height="9" rx="1.5"/><path d="M20 7.5h9v9M3 20v9h9M12 7.5h8M24.5 12v8M7.5 12v8M12 24.5h8"/></svg>;
}
