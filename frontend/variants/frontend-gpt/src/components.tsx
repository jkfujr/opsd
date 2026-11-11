import { useEffect, useId, useState, type FormEvent, type ReactNode } from 'react';
import { Button, type ButtonProps } from '@appica/ui-react/button';
import { Input, type InputProps } from '@appica/ui-react/input';
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@appica/ui-react/select';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogBody, DialogFooter } from '@appica/ui-react/dialog';
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle, DrawerBody, DrawerFooter } from '@appica/ui-react/drawer';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@appica/ui-react/tabs';
import { Skeleton } from '@appica/ui-react/skeleton';
import { Switch } from '@appica/ui-react/switch';
import { Badge } from '@appica/ui-react/badge';
import { DropdownMenu as Menu, DropdownMenuTrigger as MenuTrigger, DropdownMenuContent as MenuPopup, DropdownMenuItem as MenuItem } from '@appica/ui-react/dropdown-menu';
import { Icon, type IconName } from './icons';
import { useDemo } from './store';

export { TableRow as Row, TableCell as Cell, Switch };
export function Btn({ icon, children, className = '', variant = 'outline', size = 'sm', ...props }: ButtonProps & { icon?: IconName }) {
  return <Button variant={variant} size={size} className={`g-btn ${className}`} {...props}>{icon && <Icon name={icon} size={16}/>} {children}</Button>;
}
export function IconButton({ label, icon, ...props }: Omit<ButtonProps, 'children'> & { label: string; icon: IconName }) {
  return <Button variant="ghost" size="icon-sm" {...props} className={`g-icon-button ${props.className ?? ''}`} aria-label={label} title={label}><Icon name={icon}/></Button>;
}
export function Field({ label, hint, error, className = '', ...props }: InputProps & { label: string; hint?: string; error?: string }) {
  const id = useId();
  const describedBy = [props['aria-describedby'], hint && id + '-hint', error && id + '-error'].filter(Boolean).join(' ') || undefined;
  return <div className={`field ${className}`}><label htmlFor={id}>{label}</label><Input {...props} id={id} aria-describedby={describedBy} aria-invalid={error ? true : props['aria-invalid']} className="g-input" inputSize="md"/>{hint && <span id={id + '-hint'} className="field-hint">{hint}</span>}{error && <span id={id + '-error'} className="field-error" role="alert">{error}</span>}</div>;
}
export function SearchBox({ value, onChange, placeholder = '搜索节点…' }: { value: string; onChange: (value: string) => void; placeholder?: string }) {
  return <Input className="search-input" aria-label={placeholder.replace('…', '')} placeholder={placeholder} value={value} onChange={e => onChange(e.target.value)} startSlot={<Icon name="search" size={16}/>} inputSize="sm"/>;
}
export function Choice({ label, value, onChange, options, className = '', disabled = false }: { label: string; value: string; onChange: (value: string) => void; options: readonly (readonly [string, string])[]; className?: string; disabled?: boolean }) {
  return <Select items={options.map(([value, label]) => ({ value, label }))} value={value} onValueChange={value => onChange(String(value))} size="sm" disabled={disabled} alignItemWithTrigger={false}>
    <SelectTrigger aria-label={label} className={`g-select ${className}`}><SelectValue/></SelectTrigger><SelectContent className="g-select-popup">{options.map(([v, l]) => <SelectItem key={v} value={v}>{l}</SelectItem>)}</SelectContent>
  </Select>;
}
export type Tone = 'success' | 'warning' | 'error' | 'muted' | 'primary';
export function Status({ children, tone = 'success', dot = true }: { children: ReactNode; tone?: Tone; dot?: boolean }) {
  return <Badge variant="light" size="xs" className={`status status-${tone}`}>{dot && <span className="status-dot"/>}{children}</Badge>;
}
export function Copy({ value, compact = false }: { value: string; compact?: boolean }) {
  const { setToast } = useDemo();
  return <button type="button" className={`copy-value ${compact ? 'compact' : ''}`} aria-label={`复制 ${value}`} title={value} onClick={() => { void navigator.clipboard.writeText(value).then(() => setToast('已复制')).catch(() => setToast('复制失败，请选择文本复制')); }}>{!compact && <span>{value}</span>}<Icon name="copy" size={13}/></button>;
}
export function Empty({ title = '没有匹配的结果', detail, icon = 'search', action }: { title?: string; detail?: string; icon?: IconName; action?: ReactNode }) {
  return <div className="empty-state"><Icon name={icon} size={30}/><h3>{title}</h3>{detail && <p>{detail}</p>}{action}</div>;
}
export function Panel({ title, meta, actions, children, className = '' }: { title?: string; meta?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string }) {
  return <section className={`panel ${className}`}>{title && <div className="panel-heading"><div className="panel-heading-title"><h2>{title}</h2>{meta && <span className="panel-meta">{meta}</span>}</div>{actions && <div className="panel-actions">{actions}</div>}</div>}{children}</section>;
}
export function PageHeading({ title, metadata, children, back }: { title: string; metadata?: ReactNode; children?: ReactNode; back?: ReactNode }) {
  return <div className="page-heading"><div className="page-heading-main">{back}<h1>{title}</h1>{metadata && <div className="page-meta">{metadata}</div>}</div>{children && <div className="heading-actions">{children}</div>}</div>;
}
export function DataTable({ headings, children, className = '' }: { headings: ReactNode[]; children: ReactNode; className?: string }) {
  return <div className={`table-scroll ${className}`} tabIndex={0} role="region" aria-label="数据列表"><Table className="g-table" hoverableRows><TableHeader><TableRow>{headings.map((h, i) => <TableHead key={i}>{h}</TableHead>)}</TableRow></TableHeader><TableBody>{children}</TableBody></Table></div>;
}
export function LoadingRows({ columns = 6 }: { columns?: number }) {
  return <>{Array.from({ length: 5 }, (_, i) => <TableRow key={i}>{Array.from({ length: columns }, (_, j) => <TableCell key={j}><Skeleton className="loading-cell"/></TableCell>)}</TableRow>)}</>;
}
export function TabSet({ tabs, value, onChange, children }: { tabs: readonly (readonly [string, string])[]; value: string; onChange: (value: string) => void; children: ReactNode }) {
  return <Tabs value={value} onValueChange={value => onChange(String(value))} variant="line" className="g-tabs"><div className="tabs-scroll"><TabsList className="g-tab-list">{tabs.map(([id, label]) => <TabsTrigger key={id} value={id} className="g-tab">{label}</TabsTrigger>)}</TabsList></div><TabsContent value={value} className="g-tab-content">{children}</TabsContent></Tabs>;
}
export function Modal({ open, onClose, title, children, footer, wide = false }: { open: boolean; onClose: () => void; title: string; children: ReactNode; footer?: ReactNode; wide?: boolean }) {
  const [dirty, setDirty] = useState(false), [discard, setDiscard] = useState(false);
  useEffect(() => { if (!open) { setDirty(false); setDiscard(false); } }, [open]);
  return <><Dialog open={open} onOpenChange={v => { if (!v) { if (dirty) setDiscard(true); else onClose(); } }}><DialogContent frame={false} closeLabel="关闭弹窗" className={`g-modal ${wide ? 'wide' : ''}`} aria-describedby={undefined} onChangeCapture={event => { if (event.target instanceof HTMLElement && event.target.closest('form')) setDirty(true); }}><DialogHeader className="g-modal-header"><DialogTitle>{title}</DialogTitle></DialogHeader><DialogBody className="g-modal-body">{children}</DialogBody>{footer && <DialogFooter className="g-modal-footer">{footer}</DialogFooter>}</DialogContent></Dialog><Dialog open={discard} onOpenChange={setDiscard}><DialogContent frame={false} closeLabel="关闭确认" className="g-modal" aria-describedby={undefined}><DialogHeader className="g-modal-header"><DialogTitle>放弃未保存的修改？</DialogTitle></DialogHeader><DialogBody className="g-modal-body">表单中的修改尚未保存。</DialogBody><DialogFooter className="g-modal-footer"><Btn onClick={() => setDiscard(false)}>继续编辑</Btn><Btn onClick={() => { setDiscard(false); setDirty(false); onClose(); }}>放弃修改</Btn></DialogFooter></DialogContent></Dialog></>;
}
export function Sheet({ open, onClose, title, children, footer }: { open: boolean; onClose: () => void; title: string; children: ReactNode; footer?: ReactNode }) {
  return <Drawer side="right" open={open} onOpenChange={v => { if (!v) onClose(); }}><DrawerContent frame={false} closeLabel="关闭详情" className="g-sheet" aria-describedby={undefined}><DrawerHeader className="g-modal-header"><DrawerTitle>{title}</DrawerTitle></DrawerHeader><DrawerBody className="g-modal-body">{children}</DrawerBody>{footer && <DrawerFooter className="g-modal-footer">{footer}</DrawerFooter>}</DrawerContent></Drawer>;
}
export type Confirmation = { title: string; description?: string; confirmLabel: string; action: () => void; danger?: boolean };
export function Confirm({ value, close }: { value: Confirmation | null; close: () => void }) {
  return <Modal open={!!value} onClose={close} title={value?.title ?? '确认操作'} footer={<><Btn onClick={close}>取消</Btn><Btn variant={value?.danger ? 'destructive' : 'primary'} onClick={() => { value?.action(); close(); }}>{value?.confirmLabel}</Btn></>}>{value?.description && <p className="confirm-text">{value.description}</p>}</Modal>;
}
export function Notice({ children, action, tone = 'warning' }: { children: ReactNode; action?: { label: string; onClick: () => void }; tone?: Tone }) {
  return <div className={`notice notice-${tone}`} role={tone === 'error' ? 'alert' : 'status'}><Icon name={tone === 'success' ? 'check' : 'alert'} size={17}/><span>{children}</span>{action && <Btn variant="ghost" size="sm" onClick={action.onClick}>{action.label}</Btn>}</div>;
}
export function MoreActions({ label, items }: { label: string; items: { label: string; onClick: () => void; disabled?: boolean }[] }) {
  return <Menu><MenuTrigger aria-label={label} className="more-trigger"><Icon name="more"/></MenuTrigger><MenuPopup className="action-menu">{items.map(item => <MenuItem key={item.label} disabled={item.disabled} onClick={item.onClick}>{item.label}</MenuItem>)}</MenuPopup></Menu>;
}
export function KeyValue({ items }: { items: [string, ReactNode][] }) {
  return <dl className="key-values">{items.map(([key, value]) => <div key={key}><dt>{key}</dt><dd>{value}</dd></div>)}</dl>;
}
export function EditorForm({ id, onSubmit, children }: { id?: string; onSubmit: (data: FormData) => void; children: ReactNode }) {
  const [dirty, setDirty] = useState(false);
  useEffect(() => { const before = (event: BeforeUnloadEvent) => { if (dirty) event.preventDefault(); }; window.addEventListener('beforeunload', before); return () => window.removeEventListener('beforeunload', before); }, [dirty]);
  return <form id={id} className="editor-form" onChange={() => setDirty(true)} onSubmit={(event: FormEvent<HTMLFormElement>) => { event.preventDefault(); onSubmit(new FormData(event.currentTarget)); }}>{children}</form>;
}
export function RouteLoading() { return <div className="route-loading" role="status" aria-label="页面加载中"><Skeleton className="route-title-skeleton"/><Skeleton className="route-content-skeleton"/></div>; }
export function Refresh({ onClick }: { onClick?: () => void }) {
  const { setToast, later } = useDemo();
  const [busy, setBusy] = useState(false);
  return <Btn icon="refresh" className={busy ? 'refreshing' : ''} disabled={busy} onClick={() => { setBusy(true); later(() => { setBusy(false); onClick?.(); setToast('读数已刷新'); }, 650); }}>{busy ? '刷新中' : '刷新'}</Btn>;
}
