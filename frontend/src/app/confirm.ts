import type { ReactNode } from 'react';
import { createStore } from '../lib/store';

export interface ConfirmRequest {
  id: number;
  title: string;
  body?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  /** 需要输入此文本才能确认（高危操作）。 */
  typeToConfirm?: string;
  resolve: (ok: boolean) => void;
}

export const confirmations = createStore<ConfirmRequest | null>(null);
let seq = 0;

export function confirm(options: Omit<ConfirmRequest, 'id' | 'resolve'>): Promise<boolean> {
  return new Promise((resolve) => {
    const previous = confirmations.get();
    previous?.resolve(false);
    confirmations.set({ ...options, id: ++seq, resolve });
  });
}

export function settle(ok: boolean) {
  const current = confirmations.get();
  if (!current) return;
  confirmations.set(null);
  current.resolve(ok);
}
