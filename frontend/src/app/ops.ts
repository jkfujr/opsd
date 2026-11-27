// 操作追踪：202 受理后持续读取原 Operation，直到进入终态；同时合并其他会话产生的待处理操作。
import { call, get, isUnknown, newKey, ApiError, enc } from '../api/http';
import type { Operation, OperationList, RequestReceipt } from '../api/types';
import { createStore } from '../lib/store';
import { isTerminal, operationTitle } from '../lib/status';
import { invalidateResource } from './query';
import { toast } from './toast';

export interface Tracked {
  op: Operation;
  label: string;
  /** 由本页发起（终态时提示）。 */
  mine: boolean;
  finishedAt?: number;
}

export interface PendingRequest {
  key: string;
  nodeId: string;
  label: string;
  at: number;
  message?: string;
  checking?: boolean;
}

export interface OpsState {
  items: Record<string, Tracked>;
  requests: PendingRequest[];
  open: boolean;
  focus?: string;
}

const REQ_KEY = 'opsd.t1.requests';
const loadRequests = (): PendingRequest[] => {
  try { const v = JSON.parse(sessionStorage.getItem(REQ_KEY) ?? '[]'); return Array.isArray(v) ? v.filter((r) => typeof r.key === 'string' && typeof r.nodeId === 'string') : []; } catch { return []; }
};

export const ops = createStore<OpsState>({ items: {}, requests: loadRequests(), open: false });
ops.subscribe(() => {
  try { sessionStorage.setItem(REQ_KEY, JSON.stringify(ops.get().requests.map(({ checking: _c, ...r }) => r))); } catch { /* 忽略 */ }
});

const subjectResources: Record<Operation['subject'], string[]> = {
  firewall: ['firewall'], firewall_config: ['firewall'], component: ['firewall'], config: ['nodes'], name: ['nodes'], account: ['nodes', 'account'],
  container: ['containers'], compose: ['compose'], compose_register: ['compose'], database: ['databases'], database_step: ['databases'],
  task_save: ['tasks'], task_run: ['tasks'], shared_initialize: ['databases', 'nodes'], storage_root: ['storage'], storage_pool_create: ['storage'], storage_pool_ec: ['storage'],
};

export function isOperation(value: unknown): value is Operation {
  return !!value && typeof value === 'object' && 'id' in value && 'state' in value && 'subject' in value && 'node_id' in value;
}

export function upsert(op: Operation, mine = false, label?: string) {
  ops.set((s) => {
    const prev = s.items[op.id];
    if (prev && prev.op.revision > op.revision) return s;
    const finished = isTerminal(op);
    const next: Tracked = {
      op,
      label: label ?? prev?.label ?? operationTitle(op),
      mine: mine || prev?.mine || false,
      finishedAt: finished ? prev?.finishedAt ?? Date.now() : undefined,
    };
    if (prev && finished && !isTerminal(prev.op)) announce(next);
    if (next.mine && op.state === 'awaiting_confirmation' && prev?.op.state !== 'awaiting_confirmation') queueMicrotask(() => openDock(op.id));
    return { ...s, items: { ...s.items, [op.id]: next } };
  });
  ensureLoop();
}

function announce(t: Tracked) {
  for (const r of subjectResources[t.op.subject] ?? []) invalidateResource(r);
  invalidateResource('operations');
  if (!t.mine) return;
  const name = t.op.target_name ? `${t.label} · ${t.op.target_name}` : t.label;
  if (t.op.state === 'succeeded') toast(`${name}已完成`, 'ok');
  else if (t.op.state === 'failed' || t.op.state === 'rejected') toast(`${name}失败`, 'bad', { detail: t.op.error?.message, action: { label: '查看', run: () => openDock(t.op.id) } });
  else if (t.op.state === 'reverted') toast(`${name}已回滚`, 'warn');
  else if (t.op.state === 'cancelled') toast(`${name}已取消`, 'idle');
}

export function track(op: Operation, label?: string) {
  upsert(op, true, label);
  if (op.state === 'awaiting_confirmation' || op.state === 'attention_required') openDock(op.id);
}

export function openDock(focus?: string) { ops.set((s) => ({ ...s, open: true, focus })); }
export function closeDock() { ops.set((s) => ({ ...s, open: false, focus: undefined })); }
export function toggleDock() { ops.set((s) => ({ ...s, open: !s.open, focus: undefined })); }
export function forget(id: string) { ops.set((s) => { const items = { ...s.items }; delete items[id]; return { ...s, items }; }); }
export function clearFinished() {
  ops.set((s) => ({ ...s, items: Object.fromEntries(Object.entries(s.items).filter(([, t]) => !isTerminal(t.op))) }));
}

async function refresh(id: string) {
  try {
    const op = await get<Operation>(`/operations/${enc(id)}`);
    upsert(op);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) forget(id);
  }
}

let timer = 0;
let lastPending = 0;
function ensureLoop() {
  if (timer) return;
  timer = window.setInterval(tick, 1500);
}
async function tick() {
  if (document.visibilityState === 'hidden') return;
  const active = Object.values(ops.get().items).filter((t) => !isTerminal(t.op));
  await Promise.all(active.map((t) => refresh(t.op.id)));
  // 终态记录保留 3 分钟供回看。
  const now = Date.now();
  ops.set((s) => {
    const stale = Object.values(s.items).filter((t) => t.finishedAt && now - t.finishedAt > 180_000 && s.focus !== t.op.id);
    if (!stale.length) return s;
    const items = { ...s.items };
    for (const t of stale) delete items[t.op.id];
    return { ...s, items };
  });
  if (now - lastPending > 12_000) void syncPending();
  if (!Object.values(ops.get().items).some((t) => !isTerminal(t.op)) && !ops.get().requests.length) {
    window.clearInterval(timer);
    timer = 0;
  }
}

/** 读取全部未结束操作（包括其他会话或 API 发起的）。 */
export async function syncPending() {
  lastPending = Date.now();
  try {
    const list = await get<OperationList>('/operations', { query: { pending: true, limit: 100 } });
    for (const op of list.items) upsert(op);
  } catch { /* 下次重试 */ }
}

export function onOperationsChanged() {
  void syncPending();
  for (const t of Object.values(ops.get().items)) if (!isTerminal(t.op)) void refresh(t.op.id);
}

/** 对原操作执行确认、取消、重新处理或接受现状。 */
export async function decide(op: Operation, action: 'confirm' | 'cancel' | 'recover' | 'resolve') {
  const body: Record<string, unknown> = { action, expected_revision: op.revision };
  if (action === 'resolve') body.accept_current = true;
  try {
    const r = await call<Operation>('POST', `/operations/${enc(op.id)}/actions`, { body, key: newKey(`op-${action}`) });
    upsert(r.data, true);
    return r.data;
  } catch (error) {
    if (error instanceof ApiError && [409, 412].includes(error.status)) void refresh(op.id);
    throw error;
  }
}

// ── 结果未知的请求：保留原键，只查询原回执，不换键重发 ─────────────
export function rememberRequest(key: string, nodeId: string, label: string) {
  ops.set((s) => (s.requests.some((r) => r.key === key) ? s : { ...s, requests: [...s.requests, { key, nodeId, label, at: Date.now() }] }));
  ensureLoop();
}
export function forgetRequest(key: string) { ops.set((s) => ({ ...s, requests: s.requests.filter((r) => r.key !== key) })); }

export async function checkRequest(key: string): Promise<'operation' | 'completed' | 'pending' | 'missing'> {
  const req = ops.get().requests.find((r) => r.key === key);
  if (!req) return 'missing';
  const patch = (p: Partial<PendingRequest>) => ops.set((s) => ({ ...s, requests: s.requests.map((r) => (r.key === key ? { ...r, ...p } : r)) }));
  patch({ checking: true, message: undefined });
  try {
    const receipt = await get<RequestReceipt>(`/requests/${enc(key)}`, { query: { node_id: req.nodeId } });
    if (receipt.operation_id) {
      const op = await get<Operation>(`/operations/${enc(receipt.operation_id)}`);
      forgetRequest(key);
      track(op, req.label);
      return 'operation';
    }
    if (receipt.state === 'completed') {
      forgetRequest(key);
      toast(`${req.label}已完成`, 'ok');
      invalidateResource('all');
      return 'completed';
    }
    patch({ checking: false, message: receipt.state === 'accepted' ? '已受理，等待结果' : '结果仍未知，请稍后再查' });
    return 'pending';
  } catch (error) {
    const missing = error instanceof ApiError && error.status === 404;
    patch({ checking: false, message: missing ? '尚未找到原请求记录（不代表不会受理），请稍后再查' : isUnknown(error) ? '目标暂时无法核对' : (error as Error).message });
    return 'pending';
  }
}
