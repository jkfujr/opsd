import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { AnimatePresence, motion } from 'motion/react';
import { TriangleAlert } from 'lucide-react';
import { useStore } from '../lib/store';
import { dismiss, pause, schedule, toasts } from './toast';
import { confirmations, settle } from './confirm';
import { Button, Dialog, IconButton, Input, ToneIcon } from '../ui/kit';
import { X } from 'lucide-react';

export function ToastHost() {
  const list = useStore(toasts);
  return createPortal(
    <div className="toasts" aria-live="polite">
      <AnimatePresence initial={false}>
        {list.map((t) => (
          <motion.div
            key={t.id}
            layout
            className={`toast tone-${t.tone}`}
            role={t.tone === 'bad' ? 'alert' : 'status'}
            initial={{ opacity: 0, y: 24, scale: 0.94 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 8, scale: 0.96, transition: { duration: 0.16 } }}
            transition={{ type: 'spring', stiffness: 520, damping: 34 }}
            onPointerEnter={() => pause(t.id)}
            onPointerLeave={() => schedule(t.id, 2500)}
          >
            <ToneIcon tone={t.tone} />
            <div className="toast-text">
              <div>{t.text}</div>
              {t.detail && <div className="muted" style={{ fontSize: 12.5 }}>{t.detail}</div>}
            </div>
            {t.action && <Button size="sm" onClick={() => { dismiss(t.id); t.action!.run(); }}>{t.action.label}</Button>}
            <IconButton size="sm" label="关闭" onClick={() => dismiss(t.id)}><X /></IconButton>
          </motion.div>
        ))}
      </AnimatePresence>
    </div>,
    document.body,
  );
}

export function ConfirmHost() {
  const request = useStore(confirmations);
  const last = useRef(request);
  if (request) last.current = request;
  const r = request ?? last.current;
  const [typed, setTyped] = useState('');
  useEffect(() => setTyped(''), [request?.id]);
  const blocked = !!request?.typeToConfirm && typed.trim() !== request.typeToConfirm;
  return (
    <Dialog
      role="alertdialog"
      open={!!request}
      onClose={() => settle(false)}
      width={440}
      title={r?.title ?? ''}
      icon={r?.danger ? <span className="tone-bad" style={{ display: 'grid', placeItems: 'center', width: 36, height: 36, borderRadius: 10, background: 'var(--bad-soft)', flex: 'none' }}><TriangleAlert width={18} height={18} style={{ color: 'var(--bad)' }} /></span> : undefined}
      footer={
        <>
          <Button variant="ghost" onClick={() => settle(false)}>{r?.cancelLabel ?? '取消'}</Button>
          <Button variant={r?.danger ? 'danger' : 'primary'} disabled={blocked} onClick={() => settle(true)} autoFocus>{r?.confirmLabel ?? '确认'}</Button>
        </>
      }
    >
      {r?.body && <div className="muted" style={{ fontSize: 13.5 }}>{r.body}</div>}
      {r?.typeToConfirm && (
        <div className="field">
          <label>输入 <span className="mono strong">{r.typeToConfirm}</span> 以确认</label>
          <Input mono value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus onKeyDown={(e) => { if (e.key === 'Enter' && !blocked) settle(true); }} />
        </div>
      )}
    </Dialog>
  );
}
