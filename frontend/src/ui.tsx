import type { ComponentProps, ReactNode } from 'react';
import { Button } from '@appica/ui-react/button';
import { Field, FieldLabel } from '@appica/ui-react/field';
import { Input } from '@appica/ui-react/input';
import { AlertDialog, AlertDialogContent, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter } from '@appica/ui-react/alert-dialog';

export function TextField({ label, ...props }: ComponentProps<typeof Input> & { label: string }) {
  return <Field className="min-w-0 space-y-2"><FieldLabel>{label}</FieldLabel><Input {...props} /></Field>;
}

export function Failure({ error }: { error: unknown }) {
  return error ? <p role="alert" className="break-words text-sm text-foreground-intense">{error instanceof Error ? error.message : '请求失败，请重试'}</p> : null;
}

export function PageTitle({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return <div className="mb-8 flex flex-wrap items-center justify-between gap-4"><h1 className="text-2xl font-semibold tracking-tight text-foreground-intense">{children}</h1>{action}</div>;
}

export function Confirm({ open, title, description, busy, error, onCancel, onConfirm }: { open: boolean; title: string; description: string; busy: boolean; error: unknown; onCancel: () => void; onConfirm: () => void }) {
  return <AlertDialog open={open} onOpenChange={(value) => { if (!value && !busy) onCancel(); }}>
    <AlertDialogContent className="w-full max-w-md">
      <AlertDialogTitle>{title}</AlertDialogTitle>
      <AlertDialogDescription>{description}</AlertDialogDescription>
      <Failure error={error} />
      <AlertDialogFooter>
        <Button variant="outline" disabled={busy} onClick={onCancel}>取消</Button>
        <Button variant="destructive" disabled={busy} onClick={onConfirm}>{busy ? '处理中…' : '确认撤销'}</Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
}

export function date(value?: string) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'; }
