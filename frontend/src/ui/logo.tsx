export function Logo({ size = 32, spinning }: { size?: number; spinning?: boolean }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden>
      <defs>
        <linearGradient id="logo-g" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="var(--accent)" />
          <stop offset="1" stopColor="var(--accent-2)" />
        </linearGradient>
      </defs>
      <rect width="64" height="64" rx="18" fill="var(--surface-2)" />
      <rect width="64" height="64" rx="18" fill="none" stroke="var(--line-2)" />
      <circle cx="32" cy="32" r="15" fill="none" stroke="url(#logo-g)" strokeWidth="5" strokeDasharray={spinning ? '60 40' : undefined} strokeLinecap="round">
        {spinning && <animateTransform attributeName="transform" type="rotate" from="0 32 32" to="360 32 32" dur="1.1s" repeatCount="indefinite" />}
      </circle>
      <circle cx="32" cy="32" r="4.5" fill="url(#logo-g)" />
    </svg>
  );
}
