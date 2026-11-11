import { useId, useState, type FormEvent, type ReactNode, type RefObject } from 'react';
import { Link } from 'react-router';
import { Button, buttonVariants, type ButtonProps } from '@appica/ui-react/button';
import { Input, type InputProps } from '@appica/ui-react/input';
import { Select, SelectTrigger, SelectContent, SelectItem, SelectValue } from '@appica/ui-react/select';
import { Card } from '@appica/ui-react/card';
import { Badge } from '@appica/ui-react/badge';
import { Progress } from '@appica/ui-react/progress';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogBody, DialogFooter } from '@appica/ui-react/dialog';
import { AlertDialog, AlertDialogContent, AlertDialogHeader, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter } from '@appica/ui-react/alert-dialog';
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle, DrawerBody } from '@appica/ui-react/drawer';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@appica/ui-react/table';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@appica/ui-react/tabs';
import { Skeleton } from '@appica/ui-react/skeleton';
import { Tooltip, TooltipTrigger, TooltipContent } from '@appica/ui-react/tooltip';
import { Copy as CopyIcon, Search, RotateCw, Check, SearchX, ChevronRight, type LucideIcon } from 'lucide-react';
import { useUI } from './store';
import { queryClient } from './state';

export { TableRow as Row, TableCell as Cell, Skeleton };
export function Btn({ icon: Icon, children, className = '', variant = 'outline', ...props }: ButtonProps & { icon?: LucideIcon }) {
  return <Button variant={variant} {...props} className={`ui-button ${className}`}>{Icon && <Icon size={18} aria-hidden/>}{children}</Button>;
}
export function IconButton({ icon: Icon, label, className = '', ...props }: Omit<ButtonProps, 'children'> & { icon: LucideIcon; label: string }) {
  return <Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon-md" {...props} className={`ui-icon-button ${className}`} aria-label={label}><Icon size={20} aria-hidden/></Button>}/><TooltipContent arrow={false} className="ui-tooltip" container={document.fullscreenElement instanceof HTMLElement ? document.fullscreenElement : undefined}>{label}</TooltipContent></Tooltip>;
}
export function IconLink({ to, icon: Icon, label }: { to: string; icon: LucideIcon; label: string }) {
  return <Tooltip><TooltipTrigger render={<Link to={to} className={`${buttonVariants({ variant: 'ghost', size: 'icon-md' })} ui-icon-button`} aria-label={label}><Icon size={20} aria-hidden/></Link>}/><TooltipContent arrow={false} className="ui-tooltip">{label}</TooltipContent></Tooltip>;
}
export function ActionLink({ to, children, icon: Icon, primary = false }: { to: string; children: ReactNode; icon?: LucideIcon; primary?: boolean }) {
  return <Link to={to} className={`${buttonVariants({ variant: primary ? 'primary' : 'outline' })} ui-button`}>{Icon && <Icon size={18} aria-hidden/>}{children}</Link>;
}
export function Field({ label, error, hint, ...props }: InputProps & { label: string; error?: string; hint?: string }) {
  const id = useId();
  return <div className="field"><label htmlFor={id}>{label}</label><Input {...props} id={id} className="ui-input" aria-invalid={!!error} aria-describedby={error ? `${id}-error` : hint ? `${id}-hint` : undefined}/>{error && <span id={`${id}-error`} className="field-error" role="alert">{error}</span>}{hint && !error && <span id={`${id}-hint`} className="meta">{hint}</span>}</div>;
}
export function SearchBox({ value, onChange, label = '搜索节点', placeholder, className = '' }: { value: string; onChange: (value: string) => void; label?: string; placeholder?: string; className?: string }) {
  return <Input className={`ui-input search-input ${className}`} aria-label={label} placeholder={placeholder ?? `${label}…`} value={value} onChange={event => onChange(event.target.value)} startSlot={<Search size={18} aria-hidden/>}/>;
}
export function Choice({ label, value, onChange, options, className = '', popupClassName = '', container, disabled = false, resource = false }: { label: string; value: string; onChange: (value: string) => void; options: readonly (readonly [string, string])[]; className?: string; popupClassName?: string; container?: RefObject<HTMLDivElement | null>; disabled?: boolean; resource?: boolean }) {
  return <Select items={options.map(([value, label]) => ({ value, label }))} value={value} onValueChange={value => onChange(String(value))} alignItemWithTrigger={false} disabled={disabled}><SelectTrigger className={`ui-select ${className}`} aria-label={label}><SelectValue/></SelectTrigger><SelectContent className={`ui-select-popup ${resource ? 'resource-select' : ''} ${popupClassName}`} container={container}>{options.map(([id, text]) => <SelectItem key={id} value={id}>{text}</SelectItem>)}</SelectContent></Select>;
}
export type Tone = 'success' | 'warning' | 'error' | 'neutral';
export function Status({ children, tone = 'success' }: { children: ReactNode; tone?: Tone }) { return <Badge variant="light" className={`status status-${tone}`}><span className="status-dot"/>{children}</Badge>; }
export function Panel({ title, actions, children, className = '', meta }: { title?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string; meta?: ReactNode }) {
  return <Card className={`panel ${title ? 'panel-titled' : ''} ${className}`} contentProps={{ className: 'panel-content' }}>{title ? <><div className="panel-heading"><div className="panel-heading-text"><h2>{title}</h2>{meta && <span className="meta">{meta}</span>}</div>{actions && <div className="panel-actions">{actions}</div>}</div><div className="panel-body content-stack">{children}</div></> : children}</Card>;
}
export function Heading({ title, meta, children, back }: { title: string; meta?: ReactNode; children?: ReactNode; back?: ReactNode }) {
  return <header className="page-heading"><div className="page-title"><div className="page-title-row">{back}<h1>{title}</h1></div>{meta && <div className="page-meta">{meta}</div>}</div>{children && <div className="heading-actions">{children}</div>}</header>;
}
export function Refresh({ onRefresh }: { onRefresh?: () => void | Promise<unknown> }) { const [busy, setBusy] = useState(false); return <IconButton icon={RotateCw} label={busy ? '刷新中…' : '刷新'} disabled={busy} aria-busy={busy} onClick={() => { setBusy(true); void Promise.resolve(onRefresh ? onRefresh() : queryClient.invalidateQueries({ type: 'active' })).finally(() => setBusy(false)); }}/>; }
export function Copy({ value }: { value: string }) { const { setToast } = useUI(); const [copied, setCopied] = useState(false); return <button type="button" className="copy-value" title={`复制 ${value}`} aria-label={`复制 ${value}`} onClick={() => { void navigator.clipboard.writeText(value).then(() => { setCopied(true); setToast('已复制'); }).catch(() => setToast('复制失败，请选择文本复制')); }}><span>{value}</span>{copied ? <Check size={14} aria-hidden/> : <CopyIcon size={14} aria-hidden/>}</button>; }
export function Meter({ value, label, color, className = '' }: { value: number; label: string; color?: string; className?: string }) { return <Progress className={`meter ${className}`} value={value} aria-label={label} thickness={6} indicatorColor={color ?? (value >= 90 ? 'var(--error)' : value >= 80 ? 'var(--warning)' : 'var(--success)')}/>; }
export function Empty({ title = '没有匹配的结果', action, icon: Icon = SearchX }: { title?: string; action?: ReactNode; icon?: LucideIcon }) { return <div className="empty-state"><Icon size={32} aria-hidden/><h3>{title}</h3>{action}</div>; }
export function DataTable({ headings, children, className = '' }: { headings: ReactNode[]; children: ReactNode; className?: string }) { return <div className={`table-scroll ${className}`} role="region" aria-label="数据列表" tabIndex={0}><Table className="ui-table" hoverableRows><TableHeader><TableRow>{headings.map((heading, i) => <TableHead key={i}>{heading}</TableHead>)}</TableRow></TableHeader><TableBody>{children}</TableBody></Table></div>; }
export function LoadingRows({ columns }: { columns: number }) { return <>{[0, 1, 2, 3].map(i => <TableRow key={i}>{Array.from({ length: columns }, (_, j) => <TableCell key={j}><Skeleton className="row-skeleton"/></TableCell>)}</TableRow>)}</>; }
export function TabSet({ options, value, onChange, children, className = '', context = 'page', actions, label = '页面分类' }: { options: readonly (readonly [string, string])[]; value: string; onChange: (value: string) => void; children: ReactNode; className?: string; context?: 'page' | 'panel' | 'modal' | 'ranking'; actions?: ReactNode; label?: string }) { return <Tabs className={`tab-set tabs-${context} ${className}`} value={value} onValueChange={value => onChange(String(value))} variant="line"><div className="tab-bar"><TabsList className="tab-list" aria-label={label}>{options.map(([id, label]) => <TabsTrigger key={id} value={id} className="tab-trigger">{label}</TabsTrigger>)}</TabsList>{actions && <div className="tab-actions">{actions}</div>}</div><TabsContent value={value} className="tab-content content-stack">{children}</TabsContent></Tabs>; }
export function Modal({ open, onClose, title, children, footer, wide = false, className = '' }: { open: boolean; onClose: () => void; title: string; children: ReactNode; footer?: ReactNode; wide?: boolean; className?: string }) { return <Dialog open={open} onOpenChange={value => { if (!value) onClose(); }}><DialogContent frame={false} closeLabel="关闭弹窗" className={`ui-modal ${wide ? 'wide' : ''} ${className}`} aria-describedby={undefined}><DialogHeader className="modal-header"><DialogTitle>{title}</DialogTitle></DialogHeader><DialogBody className="modal-body">{children}</DialogBody>{footer && <DialogFooter className="modal-footer">{footer}</DialogFooter>}</DialogContent></Dialog>; }
export function Sheet({ open, onClose, title, children }: { open: boolean; onClose: () => void; title: string; children: ReactNode }) { return <Drawer side="right" open={open} onOpenChange={value => { if (!value) onClose(); }}><DrawerContent frame={false} className="ui-sheet" closeLabel="关闭侧栏" aria-describedby={undefined}><DrawerHeader className="modal-header"><DrawerTitle>{title}</DrawerTitle></DrawerHeader><DrawerBody className="modal-body">{children}</DrawerBody></DrawerContent></Drawer>; }
export function Confirm({ title, detail, onConfirm, onClose, confirmLabel = '确认' }: { title?: string; detail?: string; onConfirm: () => void; onClose: () => void; confirmLabel?: string }) { return <AlertDialog open={!!title} onOpenChange={value => { if (!value) onClose(); }}><AlertDialogContent frame={false} className="ui-modal confirm-modal"><AlertDialogHeader className="modal-header"><AlertDialogTitle>{title}</AlertDialogTitle>{detail && <AlertDialogDescription className="confirm-detail">{detail}</AlertDialogDescription>}</AlertDialogHeader><AlertDialogFooter className="modal-footer"><Btn onClick={onClose}>取消</Btn><Btn variant="primary" onClick={onConfirm}>{confirmLabel}</Btn></AlertDialogFooter></AlertDialogContent></AlertDialog>; }
export function Form({ id, onSubmit, children }: { id: string; onSubmit: (form: FormData, event: FormEvent<HTMLFormElement>) => void; children: ReactNode }) { return <form id={id} className="editor-form" onSubmit={event => { event.preventDefault(); onSubmit(new FormData(event.currentTarget), event); }}>{children}</form>; }
export function DetailLink({ to, children }: { to: string; children: ReactNode }) { return <Link to={to} className="detail-link">{children}<ChevronRight size={16} aria-hidden/></Link>; }
