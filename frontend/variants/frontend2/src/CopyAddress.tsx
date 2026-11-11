import { useEffect, useRef, useState } from 'react';

export function CopyAddress({ value, className = '' }: { value: string; className?: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  const mark = (next: 'copied' | 'failed') => {
    window.clearTimeout(timer.current);
    setState(next);
    timer.current = window.setTimeout(() => setState('idle'), 1500);
  };

  const copy = async () => {
    try {
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(value);
        mark('copied');
        return;
      }
    } catch {
      // Fallback below when the browser blocks Clipboard API access.
    }
    const input = document.createElement('textarea');
    input.value = value;
    input.setAttribute('readonly', '');
    input.style.position = 'fixed';
    input.style.opacity = '0';
    document.body.append(input);
    input.select();
    const ok = document.execCommand('copy');
    input.remove();
    mark(ok ? 'copied' : 'failed');
  };

  return (
    <span className="inline-flex max-w-full flex-wrap items-center gap-1.5 align-middle">
      <button
        type="button"
        onClick={() => void copy()}
        title="点击复制"
        className={`group inline-flex max-w-full items-center gap-1.5 rounded-md border border-border/80 bg-background-muted/60 px-2 py-0.5 text-left font-mono text-xs tabular-nums text-foreground transition-colors hover:border-primary/40 hover:bg-background-muted hover:text-foreground-intense focus-visible:outline-none ${className}`}
      >
        <span className="break-all">{value}</span>
        <svg
          aria-hidden="true"
          viewBox="0 0 16 16"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          className="size-3 shrink-0 text-foreground-muted opacity-70 transition-opacity group-hover:opacity-100"
        >
          <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" />
          <path d="M3.5 10.5H3a1.5 1.5 0 0 1-1.5-1.5V3A1.5 1.5 0 0 1 3 1.5h6A1.5 1.5 0 0 1 10.5 3v.5" />
        </svg>
      </button>
      {state !== 'idle' && (
        <span
          role="status"
          className={`rounded px-1.5 py-0.5 font-sans text-[11px] font-medium ${
            state === 'failed'
              ? 'bg-error-subtle text-error-intense'
              : 'bg-success-subtle text-success-intense'
          }`}
        >
          {state === 'failed' ? '复制失败' : '已复制'}
        </span>
      )}
    </span>
  );
}
