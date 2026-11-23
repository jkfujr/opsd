import { useEffect } from 'react';
import { createStore, useStore } from '../lib/store';
import { invalidateResource, queryClient } from './query';
import { onOperationsChanged, syncPending } from './ops';
import { readSession } from './session';

export type LinkState = 'connecting' | 'live' | 'reconnecting';
export const link = createStore<{ state: LinkState; since: number; events: number }>({ state: 'connecting', since: Date.now(), events: 0 });
export const useLink = () => useStore(link);

/** 订阅 /api/v2/events：事件只用于失效，断线重连后整体重读。 */
export function useEventStream(sessionId: string) {
  useEffect(() => {
    let source: EventSource | null = null;
    let retry = 0;
    let closed = false;
    let connectedOnce = false;
    const revisions = new Map<string, number>();
    let reconnectTimer = 0;

    const open = () => {
      if (closed) return;
      source = new EventSource('/api/v2/events', { withCredentials: true });
      source.addEventListener('ready', () => {
        retry = 0;
        link.set((s) => ({ ...s, state: 'live', since: Date.now() }));
        if (connectedOnce) {
          revisions.clear();
          void queryClient.invalidateQueries({ type: 'active' }, { cancelRefetch: false });
          void syncPending();
        }
        connectedOnce = true;
      });
      source.addEventListener('invalidate', (event) => {
        link.set((s) => ({ ...s, events: s.events + 1 }));
        try {
          const data = JSON.parse((event as MessageEvent<string>).data) as { changes?: { resource: string; node_id?: string; revision?: number }[]; resource?: string };
          const changes = data.changes ?? (data.resource ? [{ resource: data.resource }] : []);
          const seen = new Set<string>();
          for (const change of changes) {
            const id = `${change.resource}/${'node_id' in change ? change.node_id ?? '' : ''}`;
            const revision = 'revision' in change ? change.revision : undefined;
            if (revision && revision <= (revisions.get(id) ?? 0)) continue;
            if (revision) revisions.set(id, revision);
            if (seen.has(change.resource)) continue;
            seen.add(change.resource);
            invalidateResource(change.resource);
            if (change.resource === 'operations' || change.resource === 'all') onOperationsChanged();
          }
        } catch { /* 无效事件忽略 */ }
      });
      source.onerror = () => {
        source?.close();
        source = null;
        if (closed) return;
        link.set((s) => ({ ...s, state: 'reconnecting', since: Date.now() }));
        // 检查会话是否仍有效；失效时由 401 处理流程跳转登录。
        void readSession().then((s) => { if (!s) window.dispatchEvent(new Event('opsd:unauthorized')); }).catch(() => {});
        const delay = Math.min(1000 * 2 ** retry++, 15_000);
        reconnectTimer = window.setTimeout(open, delay);
      };
    };
    open();
    void syncPending();
    return () => {
      closed = true;
      window.clearTimeout(reconnectTimer);
      source?.close();
    };
  }, [sessionId]);
}
