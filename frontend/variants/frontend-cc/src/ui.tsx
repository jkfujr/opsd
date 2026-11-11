import {
  createContext,
  forwardRef,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type ComponentProps,
  type HTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type RefObject,
  type TableHTMLAttributes,
  type TdHTMLAttributes,
  type TextareaHTMLAttributes,
  type ThHTMLAttributes,
} from 'react';
import { createPortal } from 'react-dom';
import { useBeforeUnload, useBlocker } from 'react-router';

/* ============================================================================
 * Button & BusyButton
 * ========================================================================== */

export type ButtonVariant = 'primary' | 'secondary' | 'outline' | 'ghost' | 'destructive';
export type ButtonSize = 'sm' | 'md';

export function buttonVariants({
  variant = 'primary',
  size = 'md',
  className = '',
}: {
  variant?: ButtonVariant;
  size?: ButtonSize;
  className?: string;
} = {}) {
  const base =
    'inline-flex shrink-0 cursor-pointer items-center justify-center gap-2 font-medium whitespace-nowrap transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-45';
  const sizeClass =
    size === 'sm' ? 'h-8 px-2.5 text-xs' : 'h-10 px-3.5 text-sm';
  const variantClass: Record<ButtonVariant, string> = {
    primary:
      'bg-[var(--ink-strong)] text-[var(--bg-surface)] border border-[var(--ink-strong)] hover:opacity-90',
    secondary:
      'bg-[var(--bg-elevated)] text-[var(--ink-strong)] border border-[var(--grid-strong)] hover:bg-[var(--bg-muted)]',
    outline:
      'bg-[var(--bg-surface)] text-[var(--ink-strong)] border border-[var(--grid-rule)] hover:border-[var(--grid-strong)] hover:bg-[var(--bg-elevated)]',
    ghost:
      'bg-transparent text-[var(--ink-body)] border border-transparent hover:bg-[var(--bg-elevated)] hover:text-[var(--ink-strong)]',
    destructive:
      'bg-[var(--signal-red)] text-white border border-[var(--signal-red)] hover:opacity-90',
  };
  return `${base} ${sizeClass} ${variantClass[variant]} ${className}`.trim();
}

export const Button = forwardRef<
  HTMLButtonElement,
  ButtonHTMLAttributes<HTMLButtonElement> & {
    variant?: ButtonVariant;
    size?: ButtonSize;
  }
>(function Button({ variant = 'primary', size = 'md', className = '', type = 'button', ...props }, ref) {
  return (
    <button
      ref={ref}
      type={type}
      data-slot="button"
      className={buttonVariants({ variant, size, className })}
      {...props}
    />
  );
});

export function Spinner({
  className = '',
  'aria-label': ariaLabel = '正在处理',
}: {
  className?: string;
  currentColor?: boolean;
  'aria-label'?: string;
}) {
  return (
    <svg
      role="status"
      aria-label={ariaLabel}
      viewBox="0 0 24 24"
      className={`size-4 animate-spin shrink-0 ${className}`}
      fill="none"
    >
      <circle
        cx="12"
        cy="12"
        r="9"
        stroke="currentColor"
        strokeOpacity="0.25"
        strokeWidth="2.5"
      />
      <path
        d="M21 12a9 9 0 0 0-9-9"
        stroke="currentColor"
        strokeWidth="2.5"
        strokeLinecap="square"
      />
    </svg>
  );
}

export function BusyButton({
  busy,
  disabled,
  children,
  ...props
}: ComponentProps<typeof Button> & { busy?: boolean }) {
  return (
    <Button
      {...props}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
    >
      {busy && <Spinner aria-label="正在处理" />}
      {children}
    </Button>
  );
}

/* ============================================================================
 * Skeleton & Progress
 * ========================================================================== */

export function Skeleton({ className = '' }: { className?: string }) {
  return (
    <div
      data-slot="skeleton"
      aria-hidden="true"
      className={`animate-pulse bg-[var(--bg-muted)] ${className}`}
    />
  );
}

export function Progress({
  value = 0,
  'aria-label': ariaLabel,
}: {
  value?: number;
  'aria-label'?: string;
}) {
  const clamped = Math.max(0, Math.min(100, value));
  const fillClass =
    clamped >= 85
      ? 'bg-[var(--signal-red)]'
      : clamped >= 70
        ? 'bg-[var(--signal-amber)]'
        : 'bg-[var(--signal-emerald)]';
  return (
    <div
      role="progressbar"
      aria-label={ariaLabel}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(clamped)}
      className="h-1.5 w-full overflow-hidden bg-[var(--bg-muted)]"
    >
      <div
        className={`h-full transition-all duration-200 ${fillClass}`}
        style={{ width: `${clamped}%` }}
      />
    </div>
  );
}

/* ============================================================================
 * Form Controls: Field, Input, Textarea, TextField, ChoiceField, Switch, Checkbox
 * ========================================================================== */

export function Field({
  className = '',
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <div data-slot="field" className={`min-w-0 space-y-2 ${className}`}>
      {children}
    </div>
  );
}

export function FieldLabel({
  htmlFor,
  className = '',
  children,
}: {
  htmlFor?: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <label
      htmlFor={htmlFor}
      className={`block text-xs font-bold tracking-wide text-[var(--ink-muted)] ${className}`}
    >
      {children}
    </label>
  );
}

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  function Input({ className = '', ...props }, ref) {
    return (
      <input
        ref={ref}
        className={`h-10 w-full min-w-0 border border-[var(--grid-rule)] bg-[var(--bg-surface)] px-3 text-sm text-[var(--ink-strong)] placeholder:text-[var(--ink-muted)] focus:border-[var(--grid-strong)] disabled:cursor-not-allowed disabled:bg-[var(--bg-elevated)] disabled:opacity-60 read-only:bg-[var(--bg-elevated)] ${className}`}
        {...props}
      />
    );
  },
);

export const Textarea = forwardRef<
  HTMLTextAreaElement,
  TextareaHTMLAttributes<HTMLTextAreaElement>
>(function Textarea({ className = '', ...props }, ref) {
  return (
    <textarea
      ref={ref}
      className={`w-full min-w-0 border border-[var(--grid-rule)] bg-[var(--bg-surface)] p-3 text-sm text-[var(--ink-strong)] placeholder:text-[var(--ink-muted)] focus:border-[var(--grid-strong)] disabled:cursor-not-allowed disabled:bg-[var(--bg-elevated)] disabled:opacity-60 read-only:bg-[var(--bg-elevated)] ${className}`}
      {...props}
    />
  );
});

export function TextField({
  label,
  loading,
  id: providedId,
  ...props
}: ComponentProps<typeof Input> & { label: string; loading?: boolean }) {
  const generatedId = useId();
  const id = providedId ?? generatedId;
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {loading ? <Skeleton className="h-10 w-full" /> : <Input id={id} {...props} />}
    </Field>
  );
}

export function ChoiceField({
  label,
  value,
  options,
  onChange,
  disabled,
  placeholder = '请选择',
}: {
  label: string;
  value: string;
  options: readonly (readonly [string, string])[];
  onChange: (value: string) => void;
  disabled?: boolean;
  placeholder?: string;
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const [coords, setCoords] = useState<{ top: number; left: number; width: number }>({
    top: 0,
    left: 0,
    width: 160,
  });

  useEffect(() => {
    if (!open || !triggerRef.current) return;
    const rect = triggerRef.current.getBoundingClientRect();
    setCoords({
      top: rect.bottom + 2,
      left: Math.max(4, Math.min(rect.left, window.innerWidth - rect.width - 4)),
      width: Math.max(rect.width, 140),
    });
    const onPointerDown = (e: MouseEvent) => {
      if (
        listRef.current &&
        !listRef.current.contains(e.target as Node) &&
        !triggerRef.current?.contains(e.target as Node)
      ) {
        setOpen(false);
      }
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    window.addEventListener('mousedown', onPointerDown);
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('mousedown', onPointerDown);
      window.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  const currentLabel = options.find(([key]) => key === value)?.[1] ?? placeholder;

  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <div className="relative">
        <button
          ref={triggerRef}
          id={id}
          type="button"
          role="combobox"
          aria-expanded={open}
          aria-haspopup="listbox"
          disabled={disabled}
          onClick={() => setOpen((v) => !v)}
          className="flex h-10 w-full min-w-0 cursor-pointer items-center justify-between border border-[var(--grid-rule)] bg-[var(--bg-surface)] px-3 text-left text-sm text-[var(--ink-strong)] focus:border-[var(--grid-strong)] disabled:cursor-not-allowed disabled:bg-[var(--bg-elevated)] disabled:opacity-60"
        >
          <span className="min-w-0 truncate">{currentLabel}</span>
          <svg
            aria-hidden="true"
            viewBox="0 0 16 16"
            className="ml-2 size-3.5 shrink-0 text-[var(--ink-muted)]"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.75"
          >
            <path d="M4 6l4 4 4-4" />
          </svg>
        </button>
        {open &&
          createPortal(
            <div
              ref={listRef}
              role="listbox"
              aria-label={label}
              style={{
                position: 'fixed',
                top: coords.top,
                left: coords.left,
                minWidth: coords.width,
                zIndex: 90,
              }}
              className="max-h-64 overflow-y-auto border border-[var(--grid-strong)] bg-[var(--bg-surface)] py-1 shadow-[4px_6px_18px_rgba(0,0,0,0.24)]"
            >
              {options.map(([key, title]) => {
                const selected = key === value;
                return (
                  <button
                    key={key}
                    type="button"
                    role="option"
                    aria-selected={selected}
                    onClick={() => {
                      onChange(String(key));
                      setOpen(false);
                    }}
                    className={`flex w-full cursor-pointer items-center justify-between px-3 py-2 text-left text-sm ${
                      selected
                        ? 'bg-[var(--bg-elevated)] font-bold text-[var(--ink-strong)]'
                        : 'text-[var(--ink-body)] hover:bg-[var(--bg-elevated)] hover:text-[var(--ink-strong)]'
                    }`}
                  >
                    <span className="truncate">{title}</span>
                    {selected && (
                      <span
                        aria-hidden="true"
                        className="ml-2 size-1.5 shrink-0 bg-[var(--signal-red)]"
                      />
                    )}
                  </button>
                );
              })}
            </div>,
            document.body,
          )}
      </div>
    </Field>
  );
}

export function Switch({
  id,
  checked,
  disabled,
  onCheckedChange,
}: {
  id?: string;
  checked: boolean;
  disabled?: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <button
      id={id}
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onCheckedChange(!checked)}
      className={`relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center border transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-50 ${
        checked
          ? 'border-[var(--signal-emerald)] bg-[var(--signal-emerald)]'
          : 'border-[var(--grid-strong)] bg-[var(--bg-muted)]'
      }`}
    >
      <span
        className={`inline-block size-4 bg-white transition-transform duration-150 ${
          checked ? 'translate-x-5' : 'translate-x-0.5'
        }`}
      />
    </button>
  );
}

export function Checkbox({
  checked,
  disabled,
  onCheckedChange,
}: {
  checked: boolean;
  disabled?: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <input
      type="checkbox"
      checked={checked}
      disabled={disabled}
      onChange={(e) => onCheckedChange(e.target.checked)}
      className="size-4 shrink-0 cursor-pointer accent-[var(--signal-emerald)] disabled:cursor-not-allowed"
    />
  );
}

/* ============================================================================
 * StatusLabel & Signal Badges
 * ========================================================================== */

export function StatusLabel({
  state,
  children,
}: {
  state: string;
  children: ReactNode;
}) {
  const tone = ['online', 'in_sync', 'succeeded', 'confirmed', 'ready', 'running', 'healthy'].includes(state)
    ? 'bg-[var(--signal-emerald-bg)] text-[var(--signal-emerald-ink)] border-[var(--signal-emerald)]'
    : ['failed', 'error', 'rejected', 'dead', 'unhealthy'].includes(state)
      ? 'bg-[var(--signal-red-bg)] text-[var(--signal-red-ink)] border-[var(--signal-red)]'
      : [
            'unknown',
            'attention_required',
            'conflict',
            'awaiting_confirmation',
            'partial',
            'unsupported',
            'mismatch',
            'stale',
            'timeout',
          ].includes(state)
        ? 'bg-[var(--signal-amber-bg)] text-[var(--signal-amber-ink)] border-[var(--signal-amber)]'
        : ['pending', 'applying', 'accepted', 'prepared', 'reverting', 'removing', 'queued', 'reading', 'restarting'].includes(state)
          ? 'bg-[var(--signal-cobalt-bg)] text-[var(--signal-cobalt-ink)] border-[var(--signal-cobalt)]'
          : 'bg-[var(--bg-elevated)] text-[var(--ink-muted)] border-[var(--grid-rule)]';

  return (
    <span
      className={`mono-num inline-flex items-center gap-1.5 border px-2 py-0.5 text-xs font-semibold leading-tight ${tone}`}
    >
      <span className="size-1.5 bg-current" aria-hidden="true" />
      {children}
    </span>
  );
}

/* ============================================================================
 * Structural Table Primitives
 * ========================================================================== */

export function Table({
  className = '',
  hoverableRows: _hoverableRows,
  children,
  ...props
}: TableHTMLAttributes<HTMLTableElement> & { hoverableRows?: boolean }) {
  return (
    <div data-slot="table-container" className="swiss-table-wrap">
      <table className={`swiss-table ${className}`} {...props}>
        {children}
      </table>
    </div>
  );
}

export function TableHeader({
  children,
  ...props
}: HTMLAttributes<HTMLTableSectionElement>) {
  return <thead {...props}>{children}</thead>;
}

export function TableBody({
  children,
  ...props
}: HTMLAttributes<HTMLTableSectionElement>) {
  return <tbody {...props}>{children}</tbody>;
}

export function TableRow({
  children,
  ...props
}: HTMLAttributes<HTMLTableRowElement>) {
  return <tr {...props}>{children}</tr>;
}

export function TableHead({
  className = '',
  children,
  ...props
}: ThHTMLAttributes<HTMLTableCellElement>) {
  return (
    <th data-slot="table-head" className={className} {...props}>
      {children}
    </th>
  );
}

export function TableCell({
  className = '',
  children,
  ...props
}: TdHTMLAttributes<HTMLTableCellElement>) {
  return (
    <td data-slot="table-cell" className={className} {...props}>
      {children}
    </td>
  );
}

export function LoadingRows({
  columns,
  rows = 3,
}: {
  columns: number;
  rows?: number;
}) {
  return (
    <>
      {Array.from({ length: rows }, (_, row) => (
        <TableRow key={row} aria-hidden="true">
          {Array.from({ length: columns }, (_, column) => (
            <TableCell key={column}>
              <Skeleton className="my-1 h-5 w-full min-w-12" />
            </TableCell>
          ))}
        </TableRow>
      ))}
    </>
  );
}

/* ============================================================================
 * Tabs
 * ========================================================================== */

const TabsContext = createContext<{
  value: string;
  onValueChange: (val: string) => void;
}>({ value: '', onValueChange: () => {} });

export function Tabs({
  value,
  onValueChange,
  className = '',
  children,
}: {
  variant?: 'line';
  value: string;
  onValueChange: (value: string) => void;
  className?: string;
  children: ReactNode;
}) {
  return (
    <TabsContext value={{ value, onValueChange }}>
      <div className={` flex flex-col ${className}`}>{children}</div>
    </TabsContext>
  );
}

export function TabsList({
  className = '',
  'aria-label': ariaLabel,
  children,
}: {
  className?: string;
  'aria-label'?: string;
  children: ReactNode;
}) {
  return (
    <div
      role="tablist"
      aria-label={ariaLabel}
      className={`flex border-b border-[var(--grid-strong)] ${className}`}
    >
      {children}
    </div>
  );
}

export function TabsTrigger({
  value,
  children,
}: {
  value: string;
  children: ReactNode;
}) {
  const ctx = useContext(TabsContext);
  const selected = ctx.value === value;
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      onClick={() => ctx.onValueChange(value)}
      className={`cursor-pointer whitespace-nowrap px-4 py-2.5 text-sm font-semibold transition-colors ${
        selected
          ? 'border-b-2 border-[var(--signal-red)] bg-[var(--bg-elevated)] text-[var(--ink-strong)]'
          : 'text-[var(--ink-muted)] hover:bg-[var(--bg-elevated)] hover:text-[var(--ink-strong)]'
      }`}
    >
      {children}
    </button>
  );
}

export function TabsContent({
  value,
  children,
}: {
  value: string;
  children: ReactNode;
}) {
  const ctx = useContext(TabsContext);
  if (ctx.value !== value) return null;
  return <div role="tabpanel" className="pt-2">{children}</div>;
}

/* ============================================================================
 * DropdownMenu (Portal-based so it never clips inside overflow-x-auto tables)
 * ========================================================================== */

const DropdownContext = createContext<{
  open: boolean;
  setOpen: (v: boolean) => void;
  triggerRef: RefObject<HTMLButtonElement | null>;
} | null>(null);

export function DropdownMenu({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  return (
    <DropdownContext value={{ open, setOpen, triggerRef }}>
      <div className="relative inline-block">{children}</div>
    </DropdownContext>
  );
}

export function DropdownMenuTrigger({
  className = '',
  disabled,
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  const ctx = useContext(DropdownContext)!;
  return (
    <button
      ref={ctx.triggerRef}
      type="button"
      aria-haspopup="menu"
      aria-expanded={ctx.open}
      disabled={disabled}
      onClick={() => ctx.setOpen(!ctx.open)}
      onKeyDown={(e) => {
        if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          ctx.setOpen(true);
        }
      }}
      className={className}
      {...props}
    >
      {children}
    </button>
  );
}

export function DropdownMenuContent({
  align = 'end',
  children,
}: {
  align?: 'start' | 'end';
  children: ReactNode;
}) {
  const ctx = useContext(DropdownContext)!;
  const menuRef = useRef<HTMLDivElement>(null);
  const [coords, setCoords] = useState<{ top: number; left: number }>({ top: 0, left: 0 });

  useEffect(() => {
    if (!ctx.open || !ctx.triggerRef.current) return;
    const rect = ctx.triggerRef.current.getBoundingClientRect();
    setCoords({
      top: rect.bottom + 4,
      left: align === 'end' ? Math.max(8, rect.right - 144) : rect.left,
    });
    requestAnimationFrame(() => {
      const firstItem = menuRef.current?.querySelector<HTMLButtonElement>(
        'button[role="menuitem"]:not(:disabled)',
      );
      firstItem?.focus();
    });
    const onPointerDown = (e: MouseEvent) => {
      if (
        menuRef.current &&
        !menuRef.current.contains(e.target as Node) &&
        !ctx.triggerRef.current?.contains(e.target as Node)
      ) {
        ctx.setOpen(false);
      }
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        ctx.setOpen(false);
        ctx.triggerRef.current?.focus();
      } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        const items = Array.from(
          menuRef.current?.querySelectorAll<HTMLButtonElement>(
            'button[role="menuitem"]:not(:disabled)',
          ) ?? [],
        );
        if (!items.length) return;
        e.preventDefault();
        const idx = items.indexOf(document.activeElement as HTMLButtonElement);
        const nextIdx =
          e.key === 'ArrowDown'
            ? (idx + 1) % items.length
            : (idx - 1 + items.length) % items.length;
        items[nextIdx]?.focus();
      }
    };
    window.addEventListener('mousedown', onPointerDown);
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('mousedown', onPointerDown);
      window.removeEventListener('keydown', onKeyDown);
    };
  }, [ctx, align]);

  if (!ctx.open) return null;

  return createPortal(
    <div
      ref={menuRef}
      role="menu"
      style={{ position: 'fixed', top: coords.top, left: coords.left, zIndex: 80 }}
      className="min-w-36 border border-[var(--grid-strong)] bg-[var(--bg-surface)] py-1 shadow-[4px_6px_18px_rgba(0,0,0,0.22)]"
    >
      {children}
    </div>,
    document.body,
  );
}

export function DropdownMenuItem({
  disabled,
  className = '',
  onClick,
  children,
}: {
  disabled?: boolean;
  className?: string;
  onClick?: () => void;
  children: ReactNode;
}) {
  const ctx = useContext(DropdownContext)!;
  return (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      onClick={() => {
        if (disabled) return;
        ctx.setOpen(false);
        onClick?.();
      }}
      className={`flex w-full cursor-pointer items-center px-3 py-2 text-left text-sm text-[var(--ink-body)] hover:bg-[var(--bg-elevated)] hover:text-[var(--ink-strong)] disabled:cursor-not-allowed disabled:opacity-45 ${className}`}
    >
      {children}
    </button>
  );
}

/* ============================================================================
 * Dialog & Drawer Overlays
 * ========================================================================== */

const DialogContext = createContext<{
  onClose: () => void;
}>({ onClose: () => {} });

export function Dialog({
  open,
  onOpenChange,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onOpenChange(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onOpenChange]);

  if (!open) return null;
  return createPortal(
    <DialogContext value={{ onClose: () => onOpenChange(false) }}>
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4">
        {children}
      </div>
    </DialogContext>,
    document.body,
  );
}

export function DialogContent({
  className = '',
  closeLabel = '关闭',
  closeButton = true,
  finalFocus,
  children,
}: {
  className?: string;
  closeLabel?: string;
  closeButton?: boolean;
  finalFocus?: RefObject<HTMLElement | null>;
  children: ReactNode;
}) {
  const { onClose } = useContext(DialogContext);
  useEffect(() => {
    return () => {
      finalFocus?.current?.focus();
    };
  }, [finalFocus]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      className={`dialog-enter relative flex max-h-[90vh] flex-col border border-[var(--grid-strong)] bg-[var(--bg-surface)] text-[var(--ink-body)] shadow-[0_12px_32px_rgba(0,0,0,0.32)] ${className}`}
    >
      {closeButton && (
        <button
          type="button"
          aria-label={closeLabel}
          onClick={onClose}
          className="absolute right-3 top-3 z-10 inline-flex size-8 cursor-pointer items-center justify-center text-[var(--ink-muted)] hover:bg-[var(--bg-elevated)] hover:text-[var(--ink-strong)]"
        >
          ✕
        </button>
      )}
      {children}
    </div>
  );
}

export function DialogHeader({ children }: { children: ReactNode }) {
  return (
    <div
      data-slot="dialog-header"
      className="min-w-0 border-b border-[var(--grid-rule)] bg-[var(--bg-elevated)] px-5 py-3.5"
    >
      {children}
    </div>
  );
}

export function DialogTitle({ children }: { children: ReactNode }) {
  return (
    <h2
      data-slot="dialog-title"
      className="pr-8 text-base font-bold text-[var(--ink-strong)] break-words"
    >
      {children}
    </h2>
  );
}

export function DialogBody({
  className = '',
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return <div className={`p-5 ${className}`}>{children}</div>;
}

export function DialogFooter({ children }: { children: ReactNode }) {
  return (
    <div
      data-slot="dialog-footer"
      className="flex shrink-0 flex-wrap items-center justify-end gap-2.5 border-t border-[var(--grid-rule)] bg-[var(--bg-elevated)] px-5 py-3"
    >
      {children}
    </div>
  );
}

const DrawerContext = createContext<{
  side: 'left' | 'right';
  onClose: () => void;
}>({ side: 'right', onClose: () => {} });

export function Drawer({
  side = 'right',
  open,
  onOpenChange,
  children,
}: {
  side?: 'left' | 'right';
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onOpenChange(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onOpenChange]);

  if (!open) return null;
  return createPortal(
    <DrawerContext value={{ side, onClose: () => onOpenChange(false) }}>
      <div
        className="fixed inset-0 z-50 flex bg-black/55"
        onClick={(e) => {
          if (e.target === e.currentTarget) onOpenChange(false);
        }}
      >
        {children}
      </div>
    </DrawerContext>,
    document.body,
  );
}

export function DrawerContent({
  className = '',
  closeLabel = '关闭',
  children,
}: {
  frame?: boolean;
  className?: string;
  closeLabel?: string;
  children: ReactNode;
}) {
  const { side, onClose } = useContext(DrawerContext);
  const posClass =
    side === 'right'
      ? 'ml-auto border-l border-[var(--grid-strong)] drawer-enter-right'
      : 'mr-auto border-r border-[var(--grid-strong)] drawer-enter-left';
  return (
    <div
      role="dialog"
      aria-modal="true"
      className={`relative flex flex-col bg-[var(--bg-surface)] text-[var(--ink-body)] shadow-[0_12px_32px_rgba(0,0,0,0.35)] ${posClass} ${className}`}
    >
      <button
        type="button"
        aria-label={closeLabel}
        onClick={onClose}
        className="absolute right-3 top-3 z-10 inline-flex size-8 cursor-pointer items-center justify-center text-[var(--ink-muted)] hover:bg-[var(--bg-elevated)] hover:text-[var(--ink-strong)]"
      >
        ✕
      </button>
      {children}
    </div>
  );
}

export function DrawerHeader({ children }: { children: ReactNode }) {
  return (
    <div
      data-slot="drawer-header"
      className="min-w-0 border-b border-[var(--grid-strong)] bg-[var(--bg-elevated)] px-5 py-3.5"
    >
      {children}
    </div>
  );
}

export function DrawerTitle({ children }: { children: ReactNode }) {
  return (
    <h2
      data-slot="drawer-title"
      className="pr-8 text-base font-bold text-[var(--ink-strong)] break-words"
    >
      {children}
    </h2>
  );
}

export function DrawerBody({
  className = '',
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return <div className={`p-5 ${className}`}>{children}</div>;
}

export function DrawerFooter({ children }: { children: ReactNode }) {
  return (
    <div
      data-slot="drawer-footer"
      className="mt-auto flex shrink-0 flex-wrap items-center justify-end gap-2.5 border-t border-[var(--grid-rule)] bg-[var(--bg-elevated)] px-5 py-3"
    >
      {children}
    </div>
  );
}

/* ============================================================================
 * Collapsible
 * ========================================================================== */

const CollapsibleContext = createContext<{
  open: boolean;
  setOpen: (v: boolean) => void;
}>({ open: false, setOpen: () => {} });

export function Collapsible({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <CollapsibleContext value={{ open, setOpen }}>
      <div>{children}</div>
    </CollapsibleContext>
  );
}

export function CollapsibleTrigger({
  className = '',
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  const ctx = useContext(CollapsibleContext);
  return (
    <button
      type="button"
      aria-expanded={ctx.open}
      onClick={() => ctx.setOpen(!ctx.open)}
      className={className}
    >
      {children}
    </button>
  );
}

export function CollapsibleContent({
  className = '',
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  const ctx = useContext(CollapsibleContext);
  if (!ctx.open) return null;
  return <div className={className}>{children}</div>;
}

/* ============================================================================
 * Domain Shared Helpers: Failure, Confirm, UnsavedChanges, PageTitle, etc.
 * ========================================================================== */

export function Failure({
  error,
  retry,
  busy,
}: {
  error: unknown;
  retry?: () => void;
  busy?: boolean;
}) {
  if (!error) return null;
  return (
    <div className="error-message">
      <p
        role="alert"
        className="min-w-0 flex-1 break-words text-sm font-medium text-[var(--signal-red-ink)]"
      >
        {error instanceof Error ? error.message : '请求失败，请重试'}
      </p>
      {retry && (
        <BusyButton variant="outline" size="sm" busy={busy} onClick={retry}>
          重试
        </BusyButton>
      )}
    </div>
  );
}

export function newRequestKey() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (v) =>
    v.toString(16).padStart(2, '0'),
  ).join('');
}

export function PageTitle({ children }: { children: ReactNode }) {
  return <h1 className="sr-only">{children}</h1>;
}

export function PageToolbar({
  children,
  className = '',
}: {
  children: ReactNode;
  className?: string;
}) {
  return <div className={`page-toolbar ${className}`}>{children}</div>;
}

export function FormSection({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <fieldset className="form-section">
      <legend className="section-title">{title}</legend>
      <div className="form-grid">{children}</div>
    </fieldset>
  );
}

export function Confirm({
  open,
  title,
  description,
  busy,
  error,
  onCancel,
  onConfirm,
  confirmLabel = '确认撤销',
  dangerous = true,
}: {
  open: boolean;
  title: string;
  description: string;
  busy: boolean;
  error: unknown;
  onCancel: () => void;
  onConfirm: () => void;
  confirmLabel?: string;
  dangerous?: boolean;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !busy) onCancel();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, busy, onCancel]);

  if (!open) return null;

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4">
      <div
        role="alertdialog"
        aria-modal="true"
        className="dialog-enter relative flex w-full max-w-md flex-col border border-[var(--grid-strong)] bg-[var(--bg-surface)] text-[var(--ink-body)] shadow-[0_12px_32px_rgba(0,0,0,0.32)]"
      >
        <div
          data-slot="alert-dialog-header"
          className="min-w-0 border-b border-[var(--grid-rule)] bg-[var(--bg-elevated)] px-5 py-3.5"
        >
          <h2
            data-slot="alert-dialog-title"
            className="text-base font-bold text-[var(--ink-strong)] break-words"
          >
            {title}
          </h2>
        </div>
        <div className="space-y-3 overflow-y-auto p-5">
          <p
            data-slot="alert-dialog-description"
            className="text-sm text-[var(--ink-body)] break-words"
          >
            {description}
          </p>
          <Failure error={error} />
        </div>
        <div
          data-slot="alert-dialog-footer"
          className="flex shrink-0 flex-wrap items-center justify-end gap-2.5 border-t border-[var(--grid-rule)] bg-[var(--bg-elevated)] px-5 py-3"
        >
          <Button variant="outline" disabled={busy} onClick={onCancel}>
            取消
          </Button>
          <BusyButton
            variant={dangerous ? 'destructive' : 'primary'}
            busy={busy}
            onClick={onConfirm}
          >
            {confirmLabel}
          </BusyButton>
        </div>
      </div>
    </div>,
    document.body,
  );
}

const UnsavedContext = createContext<(id: string, dirty: boolean) => void>(() => {});

export function UnsavedChangesProvider({ children }: { children: ReactNode }) {
  const [forms, setForms] = useState(new Set<string>());
  const mark = useCallback((id: string, dirty: boolean) => {
    setForms((current) => {
      if (current.has(id) === dirty) return current;
      const next = new Set(current);
      if (dirty) next.add(id);
      else next.delete(id);
      return next;
    });
  }, []);
  const dirty = forms.size > 0;
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      dirty &&
      (currentLocation.pathname !== nextLocation.pathname ||
        currentLocation.search !== nextLocation.search),
  );
  useBeforeUnload((event) => {
    if (dirty) {
      event.preventDefault();
      event.returnValue = '';
    }
  });
  return (
    <UnsavedContext value={mark}>
      {children}
      <Confirm
        open={blocker.state === 'blocked'}
        title="丢弃未提交的修改？"
        description="离开后将丢弃未提交的修改，已保存的配置继续应用。"
        confirmLabel="丢弃修改"
        busy={false}
        error={null}
        onCancel={() => blocker.reset?.()}
        onConfirm={() => {
          setForms(new Set());
          blocker.proceed?.();
        }}
      />
    </UnsavedContext>
  );
}

export function useUnsavedChanges(dirty: boolean) {
  const mark = useContext(UnsavedContext);
  const id = useId();
  useEffect(() => {
    mark(id, dirty);
    return () => mark(id, false);
  }, [id, dirty, mark]);
}

export function date(value?: string) {
  return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';
}
