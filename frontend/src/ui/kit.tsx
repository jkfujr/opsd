import {
  cloneElement, isValidElement, type ReactElement,
  forwardRef, useEffect, useId, useLayoutEffect, useRef, useState,
  type ButtonHTMLAttributes, type CSSProperties, type HTMLAttributes, type InputHTMLAttributes, type MouseEvent as ReactMouseEvent, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes,
} from 'react';
import { createPortal } from 'react-dom';
import { AnimatePresence, motion, type Transition, type Variants } from 'motion/react';
import { TriangleAlert, Check, Copy, Info, LoaderCircle, RefreshCw, X, CircleX, CircleCheck, CircleAlert } from 'lucide-react';
import type { Tone } from '../lib/status';
import { copyText } from '../lib/format';
import { useTween } from '../lib/hooks';
import { ApiError, errorText } from '../api/http';
import { toast } from '../app/toast';

export const easeOut = [0.22, 1, 0.36, 1] as const;
export const spring: Transition = { type: 'spring', stiffness: 520, damping: 40, mass: 0.9 };
export const softSpring: Transition = { type: 'spring', stiffness: 300, damping: 32 };
export const fade = { initial: { opacity: 0 }, animate: { opacity: 1 }, exit: { opacity: 0 } };

const cx = (...parts: (string | false | null | undefined)[]) => parts.filter(Boolean).join(' ');
export { cx };

// ── 按钮 ─────────────────────────────────────────────
type BtnProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'danger' | 'ghost' | 'default';
  size?: 'sm' | 'md' | 'lg';
  icon?: ReactNode;
  busy?: boolean;
  block?: boolean;
};
export const Button = forwardRef<HTMLButtonElement, BtnProps>(function Button({ variant = 'default', size = 'md', icon, busy, block, className, children, disabled, type = 'button', ...rest }, ref) {
  return (
    <button ref={ref} type={type} className={cx('btn', variant !== 'default' && variant, size !== 'md' && size, block && 'block', className)} disabled={disabled || busy} data-busy={busy || undefined} aria-busy={busy || undefined} {...rest}>
      {busy ? <LoaderCircle className="spin" aria-hidden /> : icon}
      {children}
    </button>
  );
});

type IconBtnProps = ButtonHTMLAttributes<HTMLButtonElement> & { label: string; size?: 'sm' | 'md'; danger?: boolean };
export const IconButton = forwardRef<HTMLButtonElement, IconBtnProps>(function IconButton({ label, size = 'md', danger, className, children, type = 'button', ...rest }, ref) {
  return <button ref={ref} type={type} className={cx('icon-btn', size === 'sm' && 'sm', danger && 'danger', className)} aria-label={label} title={label} {...rest}>{children}</button>;
});

// ── 状态 ─────────────────────────────────────────────
export function Dot({ tone = 'idle', live }: { tone?: Tone; live?: boolean }) {
  return <span className={cx('dot', `tone-${tone}`, live && 'live')} aria-hidden />;
}
export function Badge({ tone = 'idle', children, dot, outline, title }: { tone?: Tone; children: ReactNode; dot?: boolean; outline?: boolean; title?: string }) {
  return <span className={cx('badge', `tone-${tone}`, outline && 'outline')} title={title}>{dot && <Dot tone={tone} />}{children}</span>;
}
export function Tag({ children, title }: { children: ReactNode; title?: string }) { return <span className="tag" title={title}>{children}</span>; }
export function Kbd({ children }: { children: ReactNode }) { return <kbd>{children}</kbd>; }
export function Spinner({ size = 16 }: { size?: number }) { return <LoaderCircle className="spin" width={size} height={size} style={{ animation: 'spin .9s linear infinite' }} aria-hidden />; }

// ── 布局 ─────────────────────────────────────────────
export function Card({ children, className, pad, hoverable, ...rest }: { children: ReactNode; className?: string; pad?: boolean; hoverable?: boolean } & HTMLAttributes<HTMLDivElement>) {
  return <div className={cx('card', pad && 'pad', hoverable && 'hoverable', className)} {...rest}>{children}</div>;
}
export function CardHead({ title, sub, actions, icon }: { title: ReactNode; sub?: ReactNode; actions?: ReactNode; icon?: ReactNode }) {
  return (
    <div className="card-head">
      {icon}
      <div className="grow col" style={{ gap: 2 }}>
        <h2>{title}</h2>
        {sub && <span className="muted" style={{ fontSize: 12.5 }}>{sub}</span>}
      </div>
      {actions && <div className="row">{actions}</div>}
    </div>
  );
}
export function PageHead({ title, sub, actions, children }: { title: ReactNode; sub?: ReactNode; actions?: ReactNode; children?: ReactNode }) {
  return (
    <motion.header className="page-head" initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.4, ease: [0.22, 1, 0.36, 1] }}>
      <div className="titles">
        <h1>{title}</h1>
        {sub && <p>{sub}</p>}
      </div>
      {actions && <div className="actions">{actions}</div>}
      {children}
    </motion.header>
  );
}
export function SectionTitle({ children }: { children: ReactNode }) { return <div className="section-title">{children}</div>; }

/** 列表项依次出现。 */
export const listVariants: Variants = {
  hidden: {},
  show: { transition: { staggerChildren: 0.035, delayChildren: 0.04 } },
};
export const itemVariants: Variants = {
  hidden: { opacity: 0, y: 8 },
  show: { opacity: 1, y: 0, transition: { duration: 0.42, ease: easeOut } },
};

// ── 表单 ─────────────────────────────────────────────
export function Field({ label, hint, error, children, className, htmlFor }: { label?: ReactNode; hint?: ReactNode; error?: ReactNode; children: ReactNode; className?: string; htmlFor?: string }) {
  // 自动把标签关联到唯一的子控件，便于点击聚焦与辅助技术读出名称。
  const auto = useId();
  let id = htmlFor;
  let content = children;
  if (isValidElement(children)) {
    const own = (children.props as { id?: string }).id;
    id = htmlFor ?? own ?? auto;
    if (!own) content = cloneElement(children as ReactElement<{ id?: string }>, { id });
  }
  return (
    <div className={cx('field', className)}>
      {label && <label htmlFor={id}>{label}</label>}
      {content}
      {error ? <span className="field-error">{error}</span> : hint ? <span className="field-hint">{hint}</span> : null}
    </div>
  );
}
export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement> & { mono?: boolean }>(function Input({ className, mono, ...rest }, ref) {
  return <input ref={ref} className={cx('input', mono && 'mono', className)} spellCheck={false} {...rest} />;
});
export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement> & { mono?: boolean }>(function Textarea({ className, mono, ...rest }, ref) {
  return <textarea ref={ref} className={cx('textarea', mono && 'mono', className)} spellCheck={false} {...rest} />;
});
export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={cx('select', className)} {...rest}>{children}</select>;
}
export function SearchInput({ value, onChange, placeholder = '搜索', className, autoFocus }: { value: string; onChange: (v: string) => void; placeholder?: string; className?: string; autoFocus?: boolean }) {
  return (
    <div className={cx('input-wrap', className)}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
      <input className="input" value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} autoFocus={autoFocus} spellCheck={false} />
      {value && <span className="input-suffix"><IconButton label="清除" size="sm" onClick={() => onChange('')}><X /></IconButton></span>}
    </div>
  );
}
export function Switch({ checked, onChange, disabled, label }: { checked: boolean; onChange: (v: boolean) => void; disabled?: boolean; label: string }) {
  return <button type="button" role="switch" className="switch" aria-checked={checked} aria-label={label} disabled={disabled} onClick={() => onChange(!checked)} />;
}

export function Segmented<T extends string>({ value, onChange, options, size }: { value: T; onChange: (v: T) => void; options: readonly { value: T; label: ReactNode; count?: number; icon?: ReactNode }[]; size?: 'sm' }) {
  const id = useId();
  return (
    <div className="segmented" role="tablist" style={size === 'sm' ? ({ '--control': '30px' } as CSSProperties) : undefined}>
      {options.map((o) => (
        <button key={o.value} role="tab" type="button" aria-selected={o.value === value} onClick={() => onChange(o.value)}>
          {o.value === value && <motion.span layoutId={`seg-${id}`} className="seg-pill" transition={spring} />}
          {o.icon}
          {o.label}
          {o.count !== undefined && <span className="count">{o.count}</span>}
        </button>
      ))}
    </div>
  );
}

export function Tabs<T extends string>({ value, onChange, options }: { value: T; onChange: (v: T) => void; options: readonly { value: T; label: ReactNode }[] }) {
  const id = useId();
  return (
    <div className="tabs" role="tablist">
      {options.map((o) => (
        <button key={o.value} role="tab" type="button" aria-selected={o.value === value} onClick={() => onChange(o.value)}>
          {o.label}
          {o.value === value && <motion.span layoutId={`tab-${id}`} className="tab-line" transition={spring} />}
        </button>
      ))}
    </div>
  );
}

// ── 反馈 ─────────────────────────────────────────────
export function Skeleton({ width, height = 14, radius, style }: { width?: number | string; height?: number | string; radius?: number; style?: CSSProperties }) {
  return <span className="skeleton" style={{ display: 'block', width: width ?? '100%', height, borderRadius: radius, ...style }} aria-hidden />;
}
export function Empty({ icon, title, children, action }: { icon?: ReactNode; title: ReactNode; children?: ReactNode; action?: ReactNode }) {
  return (
    <motion.div className="empty" initial={{ opacity: 0, scale: 0.98 }} animate={{ opacity: 1, scale: 1 }} transition={{ duration: 0.4, ease: [0.22, 1, 0.36, 1] }}>
      {icon && <div className="empty-icon">{icon}</div>}
      <h3>{title}</h3>
      {children && <p>{children}</p>}
      {action}
    </motion.div>
  );
}
export function Notice({ tone = 'info', title, children, actions, icon }: { tone?: Tone; title?: ReactNode; children?: ReactNode; actions?: ReactNode; icon?: ReactNode }) {
  const Icon = tone === 'bad' ? CircleX : tone === 'warn' ? TriangleAlert : tone === 'ok' ? CircleCheck : Info;
  return (
    <motion.div className={cx('notice', `tone-${tone}`)} initial={{ opacity: 0, y: -4 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.3 }}>
      {icon ?? <Icon aria-hidden />}
      <div className="notice-body">
        {title && <strong style={{ fontWeight: 600 }}>{title}</strong>}
        {children && <div className={title ? 'muted' : undefined}>{children}</div>}
      </div>
      {actions && <div className="notice-actions">{actions}</div>}
    </motion.div>
  );
}
export function ErrorNotice({ error, retry, title = '读取失败' }: { error: unknown; retry?: () => void; title?: string }) {
  if (!error) return null;
  const id = error instanceof ApiError ? error.requestId : undefined;
  return (
    <Notice tone="bad" title={title} actions={retry && <Button size="sm" icon={<RefreshCw />} onClick={retry}>重试</Button>}>
      {errorText(error)}{id && <span className="dim mono" style={{ marginLeft: 8 }}>#{id.slice(0, 10)}</span>}
    </Notice>
  );
}

export function CopyText({ value, display, className }: { value: string; display?: ReactNode; className?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button type="button" className={cx('copy', className)} title={`复制 ${value}`} onClick={async (e) => { e.stopPropagation(); if (await copyText(value)) { setDone(true); toast('已复制', 'ok'); window.setTimeout(() => setDone(false), 1400); } }}>
      <span>{display ?? value}</span>
      {done ? <Check style={{ color: 'var(--ok)' }} /> : <Copy />}
    </button>
  );
}

// ── 计量 ─────────────────────────────────────────────
export function Meter({ value, tone, thin }: { value?: number; tone?: Tone; thin?: boolean }) {
  return <div className={cx('meter', thin && 'thin', tone && `tone-${tone}`)} role="meter" aria-valuenow={value ?? 0} aria-valuemin={0} aria-valuemax={100}><span style={{ width: `${Math.max(0, Math.min(100, value ?? 0))}%` }} /></div>;
}
export function Ring({ value, size = 56, stroke = 5, tone, children }: { value?: number; size?: number; stroke?: number; tone?: Tone; children?: ReactNode }) {
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const v = Math.max(0, Math.min(100, value ?? 0));
  return (
    <div style={{ position: 'relative', width: size, height: size, flex: 'none' }} className={tone ? `tone-${tone}` : undefined}>
      <svg className="ring" width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden>
        <circle className="ring-track" cx={size / 2} cy={size / 2} r={r} strokeWidth={stroke} />
        <circle className="ring-value" cx={size / 2} cy={size / 2} r={r} strokeWidth={stroke} strokeDasharray={c} strokeDashoffset={value === undefined ? c : c * (1 - v / 100)} />
      </svg>
      {children && <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center' }}>{children}</div>}
    </div>
  );
}
export function Ticker({ value, format, className }: { value?: number; format: (v: number) => string; className?: string }) {
  const shown = useTween(value);
  return <span className={cx('num', className)}>{value === undefined || !Number.isFinite(value) ? '—' : format(shown)}</span>;
}

// ── 弹出菜单 ─────────────────────────────────────────
export interface MenuItem { label: ReactNode; icon?: ReactNode; onSelect?: () => void; danger?: boolean; disabled?: boolean; separator?: boolean; hint?: ReactNode }
export function Menu({ trigger, items, align = 'end', width }: { trigger: (props: { onClick: (e: ReactMouseEvent<HTMLElement>) => void; 'aria-expanded': boolean }) => ReactNode; items: MenuItem[]; align?: 'start' | 'end'; width?: number }) {
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const close = () => setAnchor(null);
  return (
    <>
      {trigger({ onClick: (e) => { e.stopPropagation(); setAnchor(anchor ? null : e.currentTarget.getBoundingClientRect()); }, 'aria-expanded': !!anchor })}
      <Popover anchor={anchor} onClose={close} align={align} width={width}>
        {items.map((item, i) => item.separator
          ? <div key={i} className="menu-sep" />
          : (
            <button key={i} type="button" role="menuitem" className={cx('menu-item', item.danger && 'danger')} disabled={item.disabled} onClick={(e) => { e.stopPropagation(); close(); item.onSelect?.(); }}>
              {item.icon}
              <span className="grow">{item.label}</span>
              {item.hint && <span className="dim" style={{ fontSize: 12 }}>{item.hint}</span>}
            </button>
          ))}
      </Popover>
    </>
  );
}

export function Popover({ anchor, onClose, align = 'end', width, children }: { anchor: DOMRect | null; onClose: () => void; align?: 'start' | 'end'; width?: number; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number; origin: string } | null>(null);
  useLayer(!!anchor, onClose);
  useLayoutEffect(() => {
    if (!anchor) { setPos(null); return; }
    const el = ref.current;
    const w = el?.offsetWidth ?? width ?? 220;
    const h = el?.offsetHeight ?? 200;
    let left = align === 'end' ? anchor.right - w : anchor.left;
    left = Math.max(8, Math.min(left, innerWidth - w - 8));
    let top = anchor.bottom + 6;
    let origin = align === 'end' ? 'top right' : 'top left';
    if (top + h > innerHeight - 8) { top = Math.max(8, anchor.top - h - 6); origin = origin.replace('top', 'bottom'); }
    setPos({ top, left, origin });
  }, [anchor, align, width]);
  useEffect(() => {
    if (!anchor) return;
    const onDown = (e: PointerEvent) => { if (!ref.current?.contains(e.target as Node)) onClose(); };
    const onScroll = () => onClose();
    window.addEventListener('pointerdown', onDown, true);
    window.addEventListener('resize', onScroll);
    window.addEventListener('scroll', onScroll, true);
    return () => { window.removeEventListener('pointerdown', onDown, true); window.removeEventListener('resize', onScroll); window.removeEventListener('scroll', onScroll, true); };
  }, [anchor, onClose]);
  return createPortal(
    <AnimatePresence>
      {anchor && (
        <motion.div
          ref={ref}
          role="menu"
          className="popover"
          style={{ top: pos?.top ?? -9999, left: pos?.left ?? -9999, width, transformOrigin: pos?.origin }}
          initial={{ opacity: 0, scale: 0.96, y: -4 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          exit={{ opacity: 0, scale: 0.97, transition: { duration: 0.12 } }}
          transition={{ type: 'spring', stiffness: 600, damping: 38 }}
        >
          {children}
        </motion.div>
      )}
    </AnimatePresence>,
    document.body,
  );
}

// ── 对话框与侧板 ─────────────────────────────────────
// 浮层栈：Esc 只关闭最上层（确认框 → 侧板 → 菜单），终端内的 Esc 交给 shell。
const layerStack: number[] = [];
let layerSeq = 0;
export function useLayer(open: boolean, onClose: () => void) {
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    if (!open) return;
    const id = ++layerSeq;
    layerStack.push(id);
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || layerStack[layerStack.length - 1] !== id) return;
      if ((e.target as HTMLElement | null)?.closest?.('.xterm')) return;
      e.preventDefault();
      e.stopImmediatePropagation();
      close.current();
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      const i = layerStack.indexOf(id);
      if (i >= 0) layerStack.splice(i, 1);
    };
  }, [open]);
}
const useEscape = useLayer;
function useRestoreFocus(open: boolean) {
  const previous = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (open) previous.current = document.activeElement as HTMLElement | null;
    else previous.current?.focus?.();
  }, [open]);
}

export function Dialog({ open, onClose, title, sub, children, footer, width = 520, icon, role = 'dialog' }: { open: boolean; onClose: () => void; title: ReactNode; sub?: ReactNode; children: ReactNode; footer?: ReactNode; width?: number; icon?: ReactNode; role?: 'dialog' | 'alertdialog' }) {
  useEscape(open, onClose);
  useRestoreFocus(open);
  return createPortal(
    <AnimatePresence>
      {open && (
        <>
          <motion.div key="overlay" className="overlay" {...fade} transition={{ duration: 0.22 }} onClick={onClose} />
          <div className="dialog-layer" key="layer" style={{ '--dialog-w': `${width}px` } as CSSProperties}>
            <motion.div
              role={role}
              aria-modal="true"
              className="dialog"
              initial={{ opacity: 0, scale: 0.96, y: 12 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.98, y: 6, transition: { duration: 0.16 } }}
              transition={{ type: 'spring', stiffness: 420, damping: 34 }}
            >
              <div className="dialog-head">
                {icon}
                <div className="dialog-titles"><h2>{title}</h2>{sub && <p>{sub}</p>}</div>
                <IconButton label="关闭" size="sm" onClick={onClose}><X /></IconButton>
              </div>
              <div className="dialog-body">{children}</div>
              {footer && <div className="dialog-foot">{footer}</div>}
            </motion.div>
          </div>
        </>
      )}
    </AnimatePresence>,
    document.body,
  );
}

export function Sheet({ open, onClose, title, sub, children, footer, width = 640, actions, icon }: { open: boolean; onClose: () => void; title: ReactNode; sub?: ReactNode; children: ReactNode; footer?: ReactNode; width?: number; actions?: ReactNode; icon?: ReactNode }) {
  useEscape(open, onClose);
  useRestoreFocus(open);
  return createPortal(
    <AnimatePresence>
      {open && (
        <>
          <motion.div key="overlay" className="overlay" {...fade} transition={{ duration: 0.25 }} onClick={onClose} />
          <div className="sheet-layer" key="layer" style={{ '--sheet-w': `${width}px` } as CSSProperties}>
            <motion.aside
              role="dialog"
              aria-modal="true"
              className="sheet"
              initial={{ opacity: 0, x: 48 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 36, transition: { duration: 0.2, ease: [0.4, 0, 1, 1] } }}
              transition={{ type: 'spring', stiffness: 380, damping: 38 }}
            >
              <div className="sheet-head">
                {icon}
                <div className="grow col" style={{ gap: 2 }}>
                  <h2 className="ellipsis">{title}</h2>
                  {sub && <span className="muted ellipsis" style={{ fontSize: 12.5 }}>{sub}</span>}
                </div>
                {actions}
                <IconButton label="关闭" onClick={onClose}><X /></IconButton>
              </div>
              <div className="sheet-body">{children}</div>
              {footer && <div className="sheet-foot">{footer}</div>}
            </motion.aside>
          </div>
        </>
      )}
    </AnimatePresence>,
    document.body,
  );
}

export function ToneIcon({ tone }: { tone: Tone }) {
  const Icon = tone === 'ok' ? CircleCheck : tone === 'bad' ? CircleX : tone === 'warn' ? CircleAlert : Info;
  return <Icon className={`tone-${tone}`} style={{ color: 'var(--tone)' }} aria-hidden />;
}
