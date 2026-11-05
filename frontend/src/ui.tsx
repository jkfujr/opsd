import { createContext, useCallback, useContext, useEffect, useId, useState, type ComponentProps, type ReactNode } from 'react';
import { useBeforeUnload, useBlocker } from 'react-router';
import { Button } from '@appica/ui-react/button';
import { Field, FieldLabel } from '@appica/ui-react/field';
import { Input } from '@appica/ui-react/input';
import { AlertDialog, AlertDialogContent, AlertDialogHeader, AlertDialogBody, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter } from '@appica/ui-react/alert-dialog';
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@appica/ui-react/select';
import { Spinner } from '@appica/ui-react/spinner';
import { Skeleton } from '@appica/ui-react/skeleton';
import { TableRow, TableCell } from '@appica/ui-react/table';
import { Badge } from '@appica/ui-react/badge';

export function TextField({ label, loading, ...props }: ComponentProps<typeof Input> & { label: string; loading?: boolean }) {
  const id = useId();
  return <Field className="min-w-0 space-y-2"><FieldLabel htmlFor={props.id ?? id}>{label}</FieldLabel>{loading ? <Skeleton className="h-10 w-full" /> : <Input id={id} {...props} />}</Field>;
}

export function Failure({ error, retry, busy }: { error: unknown; retry?: () => void; busy?: boolean }) {
  return error ? <div className="error-message"><p role="alert" className="min-w-0 break-words text-sm text-error-intense">{error instanceof Error ? error.message : '请求失败，请重试'}</p>{retry && <BusyButton variant="outline" size="sm" busy={busy} onClick={retry}>重试</BusyButton>}</div> : null;
}

export function BusyButton({ busy, disabled, children, ...props }: ComponentProps<typeof Button> & { busy?: boolean }) {
  return <Button {...props} disabled={disabled || busy} aria-busy={busy || undefined}>{busy && <Spinner currentColor className="text-base" aria-label="正在处理" />}{children}</Button>;
}

export function LoadingRows({ columns, rows = 3 }: { columns: number; rows?: number }) {
  return <>{Array.from({ length: rows }, (_, row) => <TableRow key={row} aria-hidden="true">{Array.from({ length: columns }, (_, column) => <TableCell key={column}><Skeleton className="my-1 h-5 w-full min-w-12" /></TableCell>)}</TableRow>)}</>;
}

export function newRequestKey() { return Array.from(crypto.getRandomValues(new Uint8Array(16)), (v) => v.toString(16).padStart(2, '0')).join(''); }

export function PageTitle({ children }: { children: ReactNode }) {
  return <h1 className="sr-only">{children}</h1>;
}

export function PageToolbar({ children, className = '' }: { children: ReactNode; className?: string }) { return <div className={`page-toolbar ${className}`}>{children}</div>; }

export function FormSection({ title, children }: { title: string; children: ReactNode }) { return <fieldset className="form-section"><legend className="section-title">{title}</legend><div className="form-grid">{children}</div></fieldset>; }

export function StatusLabel({ state, children }: { state: string; children: ReactNode }) {
  const variant = ['online', 'in_sync', 'succeeded', 'confirmed', 'ready'].includes(state) ? 'success' : ['failed', 'error', 'rejected'].includes(state) ? 'error' : ['unknown', 'attention_required', 'conflict', 'awaiting_confirmation', 'partial', 'unsupported'].includes(state) ? 'warning' : ['pending', 'applying', 'accepted', 'prepared', 'reverting', 'removing'].includes(state) ? 'info' : 'soft';
  return <Badge variant={variant} size="sm">{children}</Badge>;
}

const UnsavedContext = createContext<(id: string, dirty: boolean) => void>(() => {});
export function UnsavedChangesProvider({ children }: { children: ReactNode }) {
  const [forms, setForms] = useState(new Set<string>());
  const mark = useCallback((id: string, dirty: boolean) => setForms((current) => { if (current.has(id) === dirty) return current; const next = new Set(current); if (dirty) next.add(id); else next.delete(id); return next; }), []);
  const dirty = forms.size > 0;
  const blocker = useBlocker(({ currentLocation, nextLocation }) => dirty && (currentLocation.pathname !== nextLocation.pathname || currentLocation.search !== nextLocation.search));
  useBeforeUnload((event) => { if (dirty) { event.preventDefault(); event.returnValue = ''; } });
  return <UnsavedContext value={mark}>{children}<Confirm open={blocker.state === 'blocked'} title="丢弃未提交的修改？" description="离开后将丢弃未提交的修改，已保存的配置继续应用。" confirmLabel="丢弃修改" busy={false} error={null} onCancel={() => blocker.reset?.()} onConfirm={() => { setForms(new Set()); blocker.proceed?.(); }} /></UnsavedContext>;
}
export function useUnsavedChanges(dirty: boolean) {
  const mark = useContext(UnsavedContext);
  const id = useId();
  useEffect(() => { mark(id, dirty); return () => mark(id, false); }, [id, dirty, mark]);
}

export function Confirm({ open, title, description, busy, error, onCancel, onConfirm, confirmLabel = '确认撤销', dangerous = true }: { open: boolean; title: string; description: string; busy: boolean; error: unknown; onCancel: () => void; onConfirm: () => void; confirmLabel?: string; dangerous?: boolean }) {
  return <AlertDialog open={open} onOpenChange={(value) => { if (!value && !busy) onCancel(); }}>
    <AlertDialogContent className="w-full max-w-md">
      <AlertDialogHeader><AlertDialogTitle>{title}</AlertDialogTitle></AlertDialogHeader>
      <AlertDialogBody className="space-y-3 overflow-y-auto"><AlertDialogDescription>{description}</AlertDialogDescription><Failure error={error} /></AlertDialogBody>
      <AlertDialogFooter>
        <Button variant="outline" disabled={busy} onClick={onCancel}>取消</Button>
        <BusyButton variant={dangerous ? 'destructive' : 'primary'} busy={busy} onClick={onConfirm}>{confirmLabel}</BusyButton>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
}

export function ChoiceField({ label, value, options, onChange, disabled, placeholder = '请选择' }: { label: string; value: string; options: readonly (readonly [string, string])[]; onChange: (value: string) => void; disabled?: boolean; placeholder?: string }) {
  const id = useId();
  return <Field className="min-w-0 space-y-2"><FieldLabel htmlFor={id}>{label}</FieldLabel><Select value={value || null} onValueChange={(next) => onChange(String(next ?? ''))} disabled={disabled}>
    <SelectTrigger id={id} className="w-full min-w-0"><SelectValue>{options.find(([key]) => key === value)?.[1] ?? placeholder}</SelectValue></SelectTrigger>
    <SelectContent>{options.map(([key, title]) => <SelectItem key={key} value={key}>{title}</SelectItem>)}</SelectContent>
  </Select></Field>;
}

export function date(value?: string) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'; }
