import { useEffect, useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { Check, ChevronDown, CircleHelp, History, RotateCcw, Search, ShieldAlert, Trash2, X, Zap } from 'lucide-react';
import { Link } from 'react-router';
import type { Operation } from '../api/types';
import { useStore } from '../lib/store';
import { ago } from '../lib/format';
import { isTerminal, operationStateLabel, operationTone, stepStateTone } from '../lib/status';
import { useNow } from '../lib/hooks';
import { errorText } from '../api/http';
import { checkRequest, clearFinished, closeDock, decide, forget, forgetRequest, ops, toggleDock, type PendingRequest, type Tracked } from './ops';
import { confirm } from './confirm';
import { toast } from './toast';
import { Badge, Button, Dot, IconButton, Ring, useLayer } from '../ui/kit';

export function Dock() {
  const state = useStore(ops);
  const items = useMemo(() => Object.values(state.items).sort((a, b) => rank(a) - rank(b) || Date.parse(b.op.created_at) - Date.parse(a.op.created_at)), [state.items]);
  const active = items.filter((t) => !isTerminal(t.op));
  const urgent = active.filter((t) => t.op.state === 'awaiting_confirmation' || t.op.state === 'attention_required');
  const count = active.length + state.requests.length;
  const visible = state.open || count > 0 || items.length > 0;

  useLayer(state.open, closeDock);

  return (
    <div className="dock">
      <AnimatePresence>
        {state.open && (
          <motion.section
            key="panel"
            className="dock-panel"
            initial={{ opacity: 0, y: 16, scale: 0.96 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 12, scale: 0.97, transition: { duration: 0.16 } }}
            transition={{ type: 'spring', stiffness: 440, damping: 36 }}
            aria-label="操作坞"
          >
            <div className="dock-head">
              <h2>操作</h2>
              <Link to="/operations" onClick={closeDock} className="btn ghost sm"><History />全部记录</Link>
              {items.some((t) => isTerminal(t.op)) && <IconButton size="sm" label="清除已结束" onClick={clearFinished}><Trash2 /></IconButton>}
              <IconButton size="sm" label="收起" onClick={closeDock}><ChevronDown /></IconButton>
            </div>
            <div className="dock-list">
              <AnimatePresence initial={false}>
                {state.requests.map((r) => <RequestCard key={r.key} request={r} />)}
                {items.map((t) => <OperationCard key={t.op.id} tracked={t} focused={state.focus === t.op.id} />)}
              </AnimatePresence>
              {!items.length && !state.requests.length && <div className="empty" style={{ padding: '36px 12px' }}><p>当前没有进行中的操作</p></div>}
            </div>
          </motion.section>
        )}
      </AnimatePresence>
      <AnimatePresence>
        {visible && !state.open && (
          <motion.button
            key="pill"
            type="button"
            className="dock-pill"
            onClick={toggleDock}
            initial={{ opacity: 0, y: 20, scale: 0.9 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 16, scale: 0.92 }}
            transition={{ type: 'spring', stiffness: 460, damping: 32 }}
            whileHover={{ y: -2 }}
            whileTap={{ scale: 0.96 }}
          >
            {urgent.length ? (
              <><span className="tone-warn" style={{ display: 'grid', placeItems: 'center' }}><ShieldAlert width={18} height={18} style={{ color: 'var(--warn)' }} /></span>{urgent.length} 项待处理</>
            ) : count ? (
              <><Ring value={66} size={20} stroke={2.5} tone="accent" /><span>进行中</span><span className="dock-count">{count}</span></>
            ) : (
              <><Zap width={17} height={17} style={{ color: 'var(--text-3)' }} /><span>操作</span><span className="dock-count">{items.length}</span></>
            )}
          </motion.button>
        )}
      </AnimatePresence>
    </div>
  );
}

function rank(t: Tracked) {
  if (t.op.state === 'awaiting_confirmation') return 0;
  if (t.op.state === 'attention_required') return 1;
  if (!isTerminal(t.op)) return 2;
  return 3;
}

const cardMotion = {
  layout: true,
  initial: { opacity: 0, y: 10, scale: 0.98 },
  animate: { opacity: 1, y: 0, scale: 1 },
  exit: { opacity: 0, scale: 0.96, transition: { duration: 0.15 } },
  transition: { type: 'spring', stiffness: 500, damping: 40 },
} as const;

function OperationCard({ tracked, focused }: { tracked: Tracked; focused: boolean }) {
  const { op, label } = tracked;
  const tone = operationTone(op.state);
  const [busy, setBusy] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(focused || op.state === 'attention_required' || op.state === 'failed');
  const terminal = isTerminal(op);
  const act = async (action: 'confirm' | 'cancel' | 'recover' | 'resolve', ask?: { title: string; body: string; danger?: boolean; label: string }) => {
    if (ask && !(await confirm({ title: ask.title, body: ask.body, danger: ask.danger, confirmLabel: ask.label }))) return;
    setBusy(action);
    try { await decide(op, action); } catch (e) { toast(errorText(e), 'bad'); } finally { setBusy(null); }
  };
  const steps = op.steps.filter((s) => s.name);
  return (
    <motion.article className="op-card" data-urgent={op.state === 'awaiting_confirmation'} {...cardMotion}>
      <div className="op-top">
        {op.state === 'awaiting_confirmation' ? <Countdown op={op} /> : <Dot tone={tone} live={!terminal} />}
        <div className="op-title">
          <strong>{label}</strong>
          <span>{[op.target_name, ago(op.updated_at)].filter(Boolean).join(' · ')}</span>
        </div>
        <Badge tone={tone}>{operationStateLabel[op.state]}</Badge>
        {terminal && <IconButton size="sm" label="移除" onClick={() => forget(op.id)}><X /></IconButton>}
      </div>
      {op.error && <div className="field-error" style={{ fontSize: 12.5 }}>{op.error.message}</div>}
      {op.waiting_reason && !terminal && <div className="muted" style={{ fontSize: 12.5 }}>{op.waiting_reason}</div>}
      {steps.length > 0 && (
        <>
          <button type="button" className="row muted" style={{ fontSize: 12 }} onClick={() => setExpanded((v) => !v)}>
            <motion.span animate={{ rotate: expanded ? 180 : 0 }} style={{ display: 'grid' }}><ChevronDown width={14} height={14} /></motion.span>
            {steps.length} 个步骤 · {steps.filter((s) => s.state === 'completed' || s.state === 'applied').length} 已完成
          </button>
          <AnimatePresence initial={false}>
            {expanded && (
              <motion.div className="op-steps" initial={{ height: 0, opacity: 0 }} animate={{ height: 'auto', opacity: 1 }} exit={{ height: 0, opacity: 0 }} transition={{ duration: 0.25, ease: [0.22, 1, 0.36, 1] }} style={{ overflow: 'hidden' }}>
                {steps.map((s, i) => (
                  <div key={i} className="op-step"><Dot tone={stepStateTone(s.state)} /><span className="grow ellipsis">{s.name}</span>{s.message && <span className="dim ellipsis" style={{ maxWidth: 160 }} title={s.message}>{s.message}</span>}</div>
                ))}
              </motion.div>
            )}
          </AnimatePresence>
        </>
      )}
      {!terminal && (
        <div className="op-actions">
          {op.state === 'awaiting_confirmation' && (
            <>
              <Button size="sm" variant="primary" icon={<Check />} busy={busy === 'confirm'} onClick={() => act('confirm')}>确认保留</Button>
              <span className="muted" style={{ fontSize: 12, alignSelf: 'center' }}>到期未确认将自动恢复原配置</span>
            </>
          )}
          {op.state === 'attention_required' && (
            <>
              <Button size="sm" icon={<RotateCcw />} busy={busy === 'recover'} onClick={() => act('recover', { title: '重新处理原操作？', body: '只继续尚未完成的步骤；已完成的步骤不会重复执行。', label: '重新处理' })}>重新处理</Button>
              <Button size="sm" icon={<Check />} busy={busy === 'resolve'} onClick={() => act('resolve', { title: '接受当前状态？', body: '操作将以“已人工处理”结束，系统不再尝试修改目标。', label: '接受现状' })}>接受现状</Button>
            </>
          )}
          {(op.state === 'accepted' || op.state === 'prepared' || (op.subject === 'task_run' && op.state === 'applying')) && (
            <Button size="sm" variant="ghost" icon={<X />} busy={busy === 'cancel'} onClick={() => act('cancel', { title: op.state === 'applying' ? '终止正在执行的任务？' : '取消排队中的操作？', body: op.state === 'applying' ? '任务进程组会被终止，已执行的部分不会回滚。' : '尚未开始执行的操作会被取消。', label: op.state === 'applying' ? '终止任务' : '取消操作', danger: true })}>{op.state === 'applying' ? '终止' : '取消排队'}</Button>
          )}
        </div>
      )}
      {!terminal && op.state !== 'awaiting_confirmation' && op.state !== 'attention_required' && <span className={`op-progress tone-${tone}`} />}
    </motion.article>
  );
}

function Countdown({ op }: { op: Operation }) {
  const now = useNow(250);
  const deadline = op.confirmation_deadline ? Date.parse(op.confirmation_deadline) : 0;
  const total = 120;
  const left = deadline ? Math.max(0, Math.round((deadline - now) / 1000)) : op.confirmation_remaining_seconds ?? 0;
  return (
    <div className="countdown tone-warn">
      <Ring value={(left / total) * 100} size={34} stroke={3} tone="warn" />
      <span>{left}</span>
    </div>
  );
}

function RequestCard({ request }: { request: PendingRequest }) {
  return (
    <motion.article className="op-card" data-urgent="true" {...cardMotion}>
      <div className="op-top">
        <CircleHelp width={18} height={18} style={{ color: 'var(--warn)', flex: 'none' }} />
        <div className="op-title">
          <strong>{request.label}</strong>
          <span>结果未知 · {ago(new Date(request.at).toISOString())}</span>
        </div>
        <IconButton size="sm" label="不再追踪" onClick={async () => { if (await confirm({ title: '不再追踪此请求？', body: '原请求可能已被受理。停止追踪后将无法从这里核对结果。', confirmLabel: '停止追踪' })) forgetRequest(request.key); }}><X /></IconButton>
      </div>
      {request.message && <div className="muted" style={{ fontSize: 12.5 }}>{request.message}</div>}
      <div className="op-actions">
        <Button size="sm" icon={<Search />} busy={request.checking} onClick={() => void checkRequest(request.key)}>核对原请求</Button>
        <span className="dim mono" style={{ fontSize: 11, alignSelf: 'center' }}>{request.key.slice(0, 18)}…</span>
      </div>
    </motion.article>
  );
}
