import {
  createContext,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ComponentProps,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { Icon, type IconName } from './Icon';
import { tone as toneOf } from '../lib/labels';
import { clock } from '../lib/format';

type ButtonProps = ComponentProps<'button'> & {
  variant?: 'solid' | 'line' | 'ghost' | 'danger';
  size?: 'sm' | 'md';
  busy?: boolean;
  icon?: IconName;
};

export function Button({ variant = 'line', size = 'md', busy, icon, children, className, disabled, ...rest }: ButtonProps) {
  return (
    <button
      type="button"
      className={['btn', `btn--${variant}`, size === 'sm' ? 'btn--sm' : '', className ?? ''].filter(Boolean).join(' ')}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
      {...rest}
    >
      {busy ? <Icon name="refresh" className="spin" /> : icon ? <Icon name={icon} /> : null}
      {children}
    </button>
  );
}

export function IconButton({ icon, label, ...rest }: { icon: IconName; label: string } & ButtonProps) {
  return (
    <Button variant="ghost" size="sm" className="btn--icon" aria-label={label} title={label} {...rest}>
      <Icon name={icon} />
    </Button>
  );
}

export function Field({ label, hint, children, htmlFor }: { label: string; hint?: ReactNode; children: ReactNode; htmlFor?: string }) {
  return (
    <div className="field">
      <label htmlFor={htmlFor}>{label}</label>
      {children}
      {hint ? <span className="field__hint">{hint}</span> : null}
    </div>
  );
}

export function TextInput({ label, hint, className, ...rest }: ComponentProps<'input'> & { label?: string; hint?: ReactNode }) {
  const id = useId();
  const input = <input id={rest.id ?? id} className={['input', className ?? ''].filter(Boolean).join(' ')} {...rest} />;
  return label ? (
    <Field label={label} hint={hint} htmlFor={rest.id ?? id}>
      {input}
    </Field>
  ) : (
    input
  );
}

export function Select({
  label,
  hint,
  options,
  className,
  ...rest
}: ComponentProps<'select'> & { label?: string; hint?: ReactNode; options: readonly (readonly [string, string])[] }) {
  const id = useId();
  const select = (
    <select id={rest.id ?? id} className={['select', className ?? ''].filter(Boolean).join(' ')} {...rest}>
      {options.map(([value, title]) => (
        <option key={value} value={value}>
          {title}
        </option>
      ))}
    </select>
  );
  return label ? (
    <Field label={label} hint={hint} htmlFor={rest.id ?? id}>
      {select}
    </Field>
  ) : (
    select
  );
}

export function Stamp({ state, children, size }: { state: string; children?: ReactNode; size?: 'lg' }) {
  return (
    <span className={['stamp', `stamp--${toneOf(state)}`, size === 'lg' ? 'stamp--lg' : ''].filter(Boolean).join(' ')}>
      {children}
    </span>
  );
}

export function Led({ state }: { state: 'on' | 'off' | 'warn' | 'idle' }) {
  return <i className={`led led--${state}`} aria-hidden="true" />;
}

export function Meter({ value, levelHint }: { value?: number; levelHint?: 'ok' | 'warn' | 'bad' }) {
  const clamped = Math.max(0, Math.min(100, value ?? 0));
  const level = levelHint ?? (clamped >= 90 ? 'bad' : clamped >= 75 ? 'warn' : 'ok');
  return (
    <div className="meter" data-level={level} role="presentation">
      <i style={{ width: `${clamped}%` }} />
    </div>
  );
}

export function Sheet({
  title,
  tools,
  children,
  flush,
  className,
}: {
  title?: ReactNode;
  tools?: ReactNode;
  children: ReactNode;
  flush?: boolean;
  className?: string;
}) {
  return (
    <section className={['sheet', className ?? ''].filter(Boolean).join(' ')}>
      {(title || tools) && (
        <header className="sheet__head">
          {title ? <h2>{title}</h2> : null}
          {tools ? <div className="sheet__tools">{tools}</div> : null}
        </header>
      )}
      <div className={flush ? 'sheet__body sheet__body--flush' : 'sheet__body'}>{children}</div>
    </section>
  );
}

export function Empty({ children, hint }: { children: ReactNode; hint?: ReactNode }) {
  return (
    <div className="empty">
      <span>{children}</span>
      {hint ? <span className="mono" style={{ fontSize: 11 }}>{hint}</span> : null}
    </div>
  );
}

export function Failure({ error, retry, busy }: { error: unknown; retry?: () => void; busy?: boolean }) {
  if (!error) return null;
  return (
    <div className="failure" role="alert">
      <Icon name="alert" width={15} height={15} style={{ color: 'var(--stamp)', flex: 'none' }} />
      <p>{error instanceof Error ? error.message : '请求失败，请重试'}</p>
      {retry ? (
        <Button size="sm" variant="line" busy={busy} onClick={retry}>
          重试
        </Button>
      ) : null}
    </div>
  );
}

export function Skeleton({ height = 12, width = '100%' }: { height?: number; width?: number | string }) {
  return <div className="skel" style={{ height, width }} aria-hidden="true" />;
}

export function Notice({ children, tone = 'warn' }: { children: ReactNode; tone?: 'warn' | 'info' | 'bad' }) {
  const color = tone === 'bad' ? 'var(--stamp)' : tone === 'info' ? 'var(--carbon)' : 'var(--amber)';
  return (
    <div className="notice" style={{ borderLeftColor: color }}>
      <Icon name={tone === 'info' ? 'info' : 'alert'} width={14} height={14} style={{ color, flex: 'none' }} />
      <span>{children}</span>
    </div>
  );
}

function useEscape(active: boolean, onClose: () => void) {
  useEffect(() => {
    if (!active) return;
    const handler = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handler);
    const previous = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      window.removeEventListener('keydown', handler);
      document.body.style.overflow = previous;
    };
  }, [active, onClose]);
}

export function Dialog({
  open,
  title,
  onClose,
  children,
  footer,
  width = 'normal',
}: {
  open: boolean;
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  width?: 'narrow' | 'normal' | 'wide';
}) {
  useEscape(open, onClose);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (open) ref.current?.querySelector<HTMLElement>('input, select, textarea, button')?.focus();
  }, [open]);
  if (!open) return null;
  return createPortal(
    <div className="overlay" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <div
        className={`dialog dialog--${width}`}
        role="dialog"
        aria-modal="true"
        aria-label={typeof title === 'string' ? title : undefined}
        ref={ref}
      >
        <header className="dialog__head">
          <h2>{title}</h2>
          <IconButton icon="close" label="关闭" onClick={onClose} style={{ marginLeft: 'auto' }} />
        </header>
        <div className="dialog__body">{children}</div>
        {footer ? <footer className="dialog__foot">{footer}</footer> : null}
      </div>
    </div>,
    document.body,
  );
}

export function Drawer({
  open,
  title,
  onClose,
  children,
  footer,
}: {
  open: boolean;
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  useEscape(open, onClose);
  if (!open) return null;
  return createPortal(
    <>
      <div className="scrim-drawer" onMouseDown={onClose} />
      <aside className="drawer" role="dialog" aria-modal="true" aria-label={typeof title === 'string' ? title : undefined}>
        <header className="drawer__head">
          <h2 className="truncate">{title}</h2>
          <IconButton icon="close" label="关闭" onClick={onClose} style={{ marginLeft: 'auto' }} />
        </header>
        <div className="drawer__body">{children}</div>
        {footer ? <footer className="dialog__foot">{footer}</footer> : null}
      </aside>
    </>,
    document.body,
  );
}

export function Confirm({
  open,
  title,
  description,
  confirmLabel = '确认',
  busy,
  error,
  dangerous,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel?: string;
  busy?: boolean;
  error?: unknown;
  dangerous?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog
      open={open}
      title={title}

      width="narrow"
      onClose={() => {
        if (!busy) onCancel();
      }}
      footer={
        <>
          <span className="grow" />
          <Button onClick={onCancel} disabled={busy}>
            取消
          </Button>
          <Button variant={dangerous ? 'danger' : 'solid'} busy={busy} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <div style={{ fontSize: 13, color: 'var(--ink-soft)', lineHeight: 1.7 }}>{description}</div>
      <Failure error={error} />
    </Dialog>
  );
}

export function Tabs<T extends string>({
  items,
  value,
  onChange,
  label,
}: {
  items: { id: T; label: string; count?: number }[];
  value: T;
  onChange: (next: T) => void;
  label?: string;
}) {
  return (
    <div className="tabs" role="tablist" aria-label={label}>
      {items.map((item) => (
        <button
          key={item.id}
          role="tab"
          className="tab"
          aria-selected={item.id === value}
          onClick={() => onChange(item.id)}
        >
          {item.label}
          {item.count !== undefined ? <span className="count">{item.count}</span> : null}
        </button>
      ))}
    </div>
  );
}

export function CopyLine({ value, label = '复制' }: { value: string; label?: string }) {
  const [done, setDone] = useState(false);
  useEffect(() => {
    if (!done) return;
    const timer = window.setTimeout(() => setDone(false), 1600);
    return () => window.clearTimeout(timer);
  }, [done]);
  return (
    <div className="copyline">
      <code>{value}</code>
      <Button
        size="sm"
        variant="ghost"
        icon={done ? 'check' : 'copy'}
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(value);
            setDone(true);
          } catch {
            setDone(false);
          }
        }}
      >
        {done ? '已复制' : label}
      </Button>
    </div>
  );
}

export function Sparkline({ values, width = 120, height = 34 }: { values: number[]; width?: number; height?: number }) {
  if (values.length < 2) {
    return <div style={{ height, width }} aria-hidden="true" />;
  }
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const step = width / (values.length - 1);
  const points = values.map((v, i) => `${(i * step).toFixed(2)},${(height - 3 - ((v - min) / span) * (height - 6)).toFixed(2)}`);
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden="true" style={{ overflow: 'visible' }}>
      <polyline points={points.join(' ')} fill="none" stroke="var(--ink-soft)" strokeWidth="1.4" strokeLinejoin="round" strokeLinecap="round" />
      <circle cx={width} cy={Number(points[points.length - 1].split(',')[1])} r="1.8" fill="var(--ink)" />
    </svg>
  );
}

export function Trace({
  series,
  height = 92,
  ceiling,
  xLabels,
  yFormat,
  aria,
  fill,
}: {
  series: { values: (number | undefined)[]; color: string; dashed?: boolean }[];
  height?: number;
  ceiling?: number;
  xLabels?: [string, string];
  yFormat?: (value: number) => string;
  aria: string;
  fill?: boolean;
}) {
  const width = 640;
  const padTop = 6;
  const padBottom = 4;
  const flat = series.flatMap((s) => s.values.filter((v): v is number => typeof v === 'number'));
  if (!flat.length) return <div style={{ height }} className="empty">暂无监控数据</div>;
  const max = ceiling ?? Math.max(...flat, 1) * 1.1;
  const count = Math.max(...series.map((s) => s.values.length), 2);
  const x = (index: number) => (index / Math.max(count - 1, 1)) * width;
  const y = (value: number) => padTop + (1 - Math.min(value / max, 1)) * (height - padTop - padBottom);
  const baseline = height - padBottom;
  const paths = series.map((s) => {
    let connected = false;
    return s.values
      .map((v, i) => {
        if (typeof v !== 'number') {
          connected = false;
          return '';
        }
        const command = connected ? 'L' : 'M';
        connected = true;
        return `${command}${x(i).toFixed(2)},${y(v).toFixed(2)}`;
      })
      .join(' ');
  });
  const area = fill ? `${paths[0]} L${width},${baseline} L0,${baseline} Z` : '';
  const lastIndex = (() => {
    for (let i = series[0].values.length - 1; i >= 0; i -= 1) if (typeof series[0].values[i] === 'number') return i;
    return -1;
  })();
  return (
    <div className="chart">
      <div style={{ position: 'relative' }}>
        <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" role="img" aria-label={aria} style={{ height }}>
          {[0, 0.5, 1].map((p) => (
            <line
              key={p}
              x1="0"
              x2={width}
              y1={padTop + p * (height - padTop - padBottom)}
              y2={padTop + p * (height - padTop - padBottom)}
              stroke="var(--rule-soft)"
              strokeWidth="1"
              strokeDasharray={p === 1 ? undefined : '2 4'}
              vectorEffect="non-scaling-stroke"
            />
          ))}
          {area ? <path d={area} fill="var(--ink)" opacity="0.055" stroke="none" /> : null}
          {paths.map((d, index) => (
            <path
              key={index}
              d={d}
              fill="none"
              stroke={series[index].color}
              strokeWidth="1.7"
              strokeDasharray={series[index].dashed ? '5 3' : undefined}
              strokeLinejoin="round"
              strokeLinecap="round"
              vectorEffect="non-scaling-stroke"
            />
          ))}
          {lastIndex >= 0 && typeof series[0].values[lastIndex] === 'number' ? (
            <circle cx={x(lastIndex)} cy={y(series[0].values[lastIndex] as number)} r="2.3" fill={series[0].color} stroke="var(--sheet)" strokeWidth="1.5" />
          ) : null}
        </svg>
        <span
          className="mono"
          style={{ position: 'absolute', top: -1, left: 0, fontSize: 9.5, color: 'var(--ink-mute)', background: 'var(--sheet)', paddingRight: 4 }}
        >
          0
        </span>
        <span
          className="mono"
          style={{ position: 'absolute', top: -1, right: 0, fontSize: 9.5, color: 'var(--ink-mute)', background: 'var(--sheet)', paddingLeft: 4 }}
        >
          {yFormat ? yFormat(max) : `${Math.round(max)}%`}
        </span>
      </div>
      {xLabels ? (
        <div className="chart__axis">
          <span>{xLabels[0]}</span>
          <span>{xLabels[1]}</span>
        </div>
      ) : null}
    </div>
  );
}

export function PageHead({ title, tools }: { title: string; tools?: ReactNode }) {
  return (
    <header className="pagehead">
      <h1>{title}</h1>
      {tools ? <div className="pagehead__tools">{tools}</div> : null}
    </header>
  );
}

export function Clock({ label = '更新' }: { label?: string }) {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  return (
    <span className="topbar__clock">
      {label} {clock(now)}
    </span>
  );
}

export type Session = import('../api/client').Schema['CurrentSession'];

const SessionContext = createContext<Session | null>(null);
export const SessionProvider = SessionContext.Provider;
export function useSession(): Session {
  const session = useContext(SessionContext);
  if (!session) throw new Error('会话尚未就绪');
  return session;
}
