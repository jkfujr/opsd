import { Children, createContext, useCallback, useContext, useEffect, useId, useState, type ComponentProps, type ReactNode } from 'react';
import { useBeforeUnload, useBlocker } from 'react-router';
import { Button as AppicaButton, buttonVariants as appicaButtonVariants } from '@appica/ui-react/button';
import { Pencil, RotateCw, ArrowUp, ArrowDown, FileText } from 'lucide-react';
import { IconButton, Heading, Status } from './ui';
import { RequestError, useRetryAfter } from './api/client';
export function buttonVariants(...args: Parameters<typeof appicaButtonVariants>) { return `${appicaButtonVariants(...args)} ui-button`; }
export function Button({ children, ...props }: ComponentProps<typeof AppicaButton>) {
  const label = Children.toArray(children).filter(child => typeof child === 'string').join('');
  const icon = label ? ({ '刷新': RotateCw, '编辑': Pencil, '日志': FileText, '上移': ArrowUp, '下移': ArrowDown } as const)[label as '刷新'] : undefined;
  return icon ? <IconButton icon={icon} label={props['aria-label'] ?? label} {...props}/> : <AppicaButton {...props} className={`ui-button ${props.className ?? ''}`}>{children}</AppicaButton>;
}
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
  return <Field className="min-w-0 space-y-2"><FieldLabel htmlFor={props.id ?? id}>{label}</FieldLabel>{loading ? <Skeleton className="h-10 w-full" /> : <Input id={id} {...props} className={`ui-input ${props.className ?? ""}`} />}</Field>;
}

export function Failure({ error, retry, busy }: { error: unknown; retry?: () => void; busy?: boolean }) {
  const seconds = useRetryAfter(error), problem = error instanceof RequestError ? error : undefined;
  return error ? <div className="error-message"><div className="min-w-0"><p role="alert" className="break-words text-sm text-error-intense">{error instanceof Error ? error.message : '请求失败，请重试'}</p>{problem?.problem?.errors?.map((field, index) => <p key={index} className="text-sm text-error-intense">{field.pointer}：{field.message}</p>)}{seconds > 0 && <p role="status" className="text-sm">请于 {seconds} 秒后重试</p>}{problem?.requestID && <p className="object-meta">请求 ID：{problem.requestID}</p>}</div>{retry && <BusyButton variant="outline" size="sm" busy={busy} disabled={seconds > 0} onClick={retry}>重试</BusyButton>}</div> : null;
}

export function ContinueReading({ query }: { query: { hasMore: boolean; isFetching: boolean; isReadingMore: boolean; pageError: unknown; readMore: () => void; refetch: () => unknown } }) {
  const seconds = useRetryAfter(query.pageError);
  return <><Failure error={query.pageError} retry={query.hasMore ? undefined : () => void query.refetch()}/>{query.hasMore && <div className="heading-actions"><BusyButton busy={query.isReadingMore} disabled={query.isFetching || seconds > 0} onClick={query.readMore}>继续读取</BusyButton></div>}</>;
}

export function BusyButton({ busy, disabled, children, ...props }: ComponentProps<typeof Button> & { busy?: boolean }) {
  return <Button {...props} disabled={disabled || busy} aria-busy={busy || undefined}>{busy && <Spinner currentColor className="text-base" aria-label="正在处理" />}{children}</Button>;
}

export function LoadingRows({ columns, rows = 3 }: { columns: number; rows?: number }) {
  return <>{Array.from({ length: rows }, (_, row) => <TableRow key={row} aria-hidden="true">{Array.from({ length: columns }, (_, column) => <TableCell key={column}><Skeleton className="my-1 h-5 w-full min-w-12" /></TableCell>)}</TableRow>)}</>;
}

export function newRequestKey() { return Array.from(crypto.getRandomValues(new Uint8Array(16)), (v) => v.toString(16).padStart(2, '0')).join(''); }

export function PageTitle({ children }: { children: ReactNode }) {
  return <Heading title={String(children)}/>;
}

export function PageToolbar({ children, className = '' }: { children: ReactNode; className?: string }) { return <div className={`page-toolbar ${className}`}>{children}</div>; }

export function FormSection({ title, children }: { title: string; children: ReactNode }) { return <fieldset className="form-section"><legend className="section-title">{title}</legend><div className="form-grid">{children}</div></fieldset>; }

export function StatusLabel({ state, children }: { state: string; children: ReactNode }) {
  const tone = ['online', 'in_sync', 'succeeded', 'confirmed', 'ready'].includes(state) ? 'success' : ['failed', 'error', 'rejected'].includes(state) ? 'error' : ['unknown', 'attention_required', 'conflict', 'awaiting_confirmation', 'partial', 'unsupported', 'pending', 'applying', 'accepted', 'prepared', 'reverting'].includes(state) ? 'warning' : 'neutral';
  return <Status tone={tone}>{children}</Status>;
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

export function Confirm({ open, title, description, busy, error, onCancel, onConfirm, confirmLabel = '确认撤销', dangerous = true, disabled = false }: { open: boolean; title: string; description: string; busy: boolean; error: unknown; onCancel: () => void; onConfirm: () => void; confirmLabel?: string; dangerous?: boolean; disabled?: boolean }) {
  return <AlertDialog open={open} onOpenChange={(value) => { if (!value && !busy) onCancel(); }}>
    <AlertDialogContent frame={false} className="ui-modal confirm-modal">
      <AlertDialogHeader><AlertDialogTitle>{title}</AlertDialogTitle></AlertDialogHeader>
      <AlertDialogBody className="space-y-3 overflow-y-auto"><AlertDialogDescription>{description}</AlertDialogDescription><Failure error={error} /></AlertDialogBody>
      <AlertDialogFooter>
        <Button variant="outline" disabled={busy} onClick={onCancel}>取消</Button>
        <BusyButton variant={dangerous ? 'destructive' : 'primary'} busy={busy} disabled={disabled} onClick={onConfirm}>{confirmLabel}</BusyButton>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
}

export function ChoiceField({ label, value, options, onChange, disabled, placeholder = '请选择' }: { label: string; value: string; options: readonly (readonly [string, string])[]; onChange: (value: string) => void; disabled?: boolean; placeholder?: string }) {
  const id = useId();
  return <Field className="min-w-0 space-y-2"><FieldLabel htmlFor={id}>{label}</FieldLabel><Select value={value || null} onValueChange={(next) => onChange(String(next ?? ''))} disabled={disabled}>
    <SelectTrigger id={id} className="ui-select w-full min-w-0"><SelectValue>{options.find(([key]) => key === value)?.[1] ?? placeholder}</SelectValue></SelectTrigger>
    <SelectContent>{options.map(([key, title]) => <SelectItem key={key} value={key}>{title}</SelectItem>)}</SelectContent>
  </Select></Field>;
}

export function date(value?: string) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'; }
