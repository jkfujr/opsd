import { createStore } from '../lib/store';
import type { Tone } from '../lib/status';

export interface Toast {
  id: number;
  tone: Tone;
  text: string;
  detail?: string;
  action?: { label: string; run: () => void };
  sticky?: boolean;
}

export const toasts = createStore<Toast[]>([]);
let seq = 0;
const timers = new Map<number, number>();

export function toast(text: string, tone: Tone = 'ok', extra: Omit<Partial<Toast>, 'id' | 'text' | 'tone'> = {}) {
  const id = ++seq;
  toasts.set((list) => [...list.filter((t) => t.text !== text).slice(-3), { id, tone, text, ...extra }]);
  if (!extra.sticky) schedule(id, tone === 'bad' ? 8000 : 4800);
  return id;
}

export function schedule(id: number, ms: number) {
  window.clearTimeout(timers.get(id));
  timers.set(id, window.setTimeout(() => dismiss(id), ms));
}
export function pause(id: number) { window.clearTimeout(timers.get(id)); }
export function dismiss(id: number) {
  window.clearTimeout(timers.get(id));
  timers.delete(id);
  toasts.set((list) => list.filter((t) => t.id !== id));
}
