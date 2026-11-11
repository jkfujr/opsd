import type { SVGProps } from 'react';

const paths: Record<string, React.ReactNode> = {
  overview: (
    <>
      <path d="M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z" />
    </>
  ),
  nodes: (
    <>
      <rect x="3" y="4" width="18" height="6.5" rx="1.4" />
      <rect x="3" y="13.5" width="18" height="6.5" rx="1.4" />
      <path d="M7 7.25h.01M7 16.75h.01M11 7.25h6M11 16.75h6" />
    </>
  ),
  containers: (
    <>
      <path d="M12 3.5 20 8v8l-8 4.5L4 16V8z" />
      <path d="M4 8l8 4.5L20 8M12 12.5v8" />
    </>
  ),
  database: (
    <>
      <ellipse cx="12" cy="5.5" rx="7.5" ry="2.8" />
      <path d="M4.5 5.5v13c0 1.55 3.36 2.8 7.5 2.8s7.5-1.25 7.5-2.8v-13M4.5 12c0 1.55 3.36 2.8 7.5 2.8s7.5-1.25 7.5-2.8" />
    </>
  ),
  storage: (
    <>
      <rect x="3" y="4" width="18" height="7" rx="1.4" />
      <rect x="3" y="13" width="18" height="7" rx="1.4" />
      <path d="M7 7.5h.01M7 16.5h.01M11 7.5h6M11 16.5h6" />
    </>
  ),
  firewall: (
    <>
      <path d="M12 3 4.5 6v5.2c0 4.8 7.5 9.8 7.5 9.8s7.5-5 7.5-9.8V6z" />
      <path d="M9.2 11.8h5.6M12 9v5.6" />
    </>
  ),
  settings: (
    <>
      <path d="M4 7.5h16M4 16.5h16" />
      <circle cx="9" cy="7.5" r="2.6" />
      <circle cx="15" cy="16.5" r="2.6" />
    </>
  ),
  plus: <path d="M12 5v14M5 12h14" />,
  minus: <path d="M5 12h14" />,
  close: <path d="M6 6l12 12M18 6L6 18" />,
  refresh: (
    <>
      <path d="M20 11a8 8 0 1 0-2.3 6.7" />
      <path d="M20 5v6h-6" />
    </>
  ),
  menu: <path d="M4 7h16M4 12h16M4 17h16" />,
  chevron: <path d="M9 6l6 6-6 6" />,
  down: <path d="M6 9l6 6 6-6" />,
  search: (
    <>
      <circle cx="11" cy="11" r="6.2" />
      <path d="M16 16l4 4" />
    </>
  ),
  more: (
    <>
      <circle cx="6" cy="12" r="1.2" fill="currentColor" stroke="none" />
      <circle cx="12" cy="12" r="1.2" fill="currentColor" stroke="none" />
      <circle cx="18" cy="12" r="1.2" fill="currentColor" stroke="none" />
    </>
  ),
  external: (
    <>
      <path d="M14 5h5v5M19 5l-7.5 7.5" />
      <path d="M18 14v4.5a1.5 1.5 0 0 1-1.5 1.5H5.5A1.5 1.5 0 0 1 4 18.5V7.5A1.5 1.5 0 0 1 5.5 6H10" />
    </>
  ),
  copy: (
    <>
      <rect x="9" y="9" width="11" height="11" rx="1.6" />
      <path d="M15 5.5A1.5 1.5 0 0 0 13.5 4h-8A1.5 1.5 0 0 0 4 5.5v8A1.5 1.5 0 0 0 5.5 15" />
    </>
  ),
  check: <path d="M5 12.5l4.5 4.5L19 7" />,
  alert: (
    <>
      <path d="M12 4 2.8 20h18.4z" />
      <path d="M12 10v4.5M12 17.2h.01" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="8.4" />
      <path d="M12 7.4V12l3.2 2" />
    </>
  ),
  terminal: (
    <>
      <rect x="3" y="4.5" width="18" height="15" rx="1.8" />
      <path d="M7 9.5l3 2.5-3 2.5M12.5 15h4.5" />
    </>
  ),
  logs: (
    <>
      <path d="M5 6h14M5 10.5h14M5 15h9M5 19.5h6" />
    </>
  ),
  play: <path d="M7.5 5.5l11 6.5-11 6.5z" />,
  stop: <rect x="6.5" y="6.5" width="11" height="11" rx="1" />,
  restart: (
    <>
      <path d="M4.5 12a7.5 7.5 0 1 0 2.2-5.3" />
      <path d="M4 4.5V10h5.5" />
    </>
  ),
  trash: (
    <>
      <path d="M4.5 7h15M9.5 7V4.8h5V7M6.5 7l.9 12.2a1.5 1.5 0 0 0 1.5 1.3h6.2a1.5 1.5 0 0 0 1.5-1.3L17.5 7" />
    </>
  ),
  edit: (
    <>
      <path d="M4 20h4.5L20 8.5 15.5 4 4 15.5z" />
      <path d="M13.5 6l4.5 4.5" />
    </>
  ),
  up: <path d="M12 19V5M6 11l6-6 6 6" />,
  downarrow: <path d="M12 5v14M6 13l6 6 6-6" />,
  arrow: <path d="M5 12h14M13 6l6 6-6 6" />,
  key: (
    <>
      <circle cx="8" cy="12" r="4" />
      <path d="M12 12h9M17.5 12v3.5M20 12v2.5" />
    </>
  ),
  shield: <path d="M12 3 4.5 6v5.2c0 4.8 7.5 9.8 7.5 9.8s7.5-5 7.5-9.8V6z" />,
  wifi: (
    <>
      <path d="M4 9.5a12 12 0 0 1 16 0M7 13a8 8 0 0 1 10 0M10 16.4a3.6 3.6 0 0 1 4 0" />
      <path d="M12 19.6h.01" />
    </>
  ),
  moon: <path d="M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5z" />,
  sun: (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19" />
    </>
  ),
  upload: (
    <>
      <path d="M12 16V4M7 9l5-5 5 5" />
      <path d="M4 16v3.5A1.5 1.5 0 0 0 5.5 21h13a1.5 1.5 0 0 0 1.5-1.5V16" />
    </>
  ),
  filter: <path d="M3 5h18l-7 8v6l-4 2v-8z" />,
  info: (
    <>
      <circle cx="12" cy="12" r="8.4" />
      <path d="M12 11v5.2M12 8h.01" />
    </>
  ),
};

export type IconName = keyof typeof paths;

export function Icon({ name, ...rest }: { name: IconName } & SVGProps<SVGSVGElement>) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...rest}
    >
      {paths[name]}
    </svg>
  );
}
