import { useCallback, useRef, useState } from 'react';
import { ApiError, errorText, isUnknown, needsReread, newKey, sentKeys } from '../api/http';
import { isOperation, rememberRequest, track } from './ops';
import { toast } from './toast';

export interface CommandOptions<V, R> {
  /** 操作名称：用于提示、操作坞与原请求核对。 */
  label: string | ((value: V) => string);
  /** 写入所属节点（原请求回执按此节点查询）。 */
  node: string | ((value: V) => string);
  run: (value: V, key: string) => Promise<R>;
  onSuccess?: (result: R, value: V) => void;
  /** 版本冲突时重新读取资源。 */
  onConflict?: () => void;
  /** 成功提示；返回 false 关闭默认提示。 */
  success?: string | false | ((result: R, value: V) => string | false);
}

/**
 * 统一写入流程：
 * - 每次意图生成一个请求键；
 * - 网络中断或结果未知时保留原键并登记“待核对”，禁止换键重发；
 * - 202 Operation 自动进入操作坞追踪。
 */
export function useCommand<V = void, R = unknown>(options: CommandOptions<V, R>) {
  const saved = useRef(options);
  saved.current = options;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const inflight = useRef(false);

  const run = useCallback(async (value: V): Promise<R | undefined> => {
    if (inflight.current) return undefined;
    const o = saved.current;
    const label = typeof o.label === 'function' ? o.label(value) : o.label;
    const node = typeof o.node === 'function' ? o.node(value) : o.node;
    const key = newKey();
    inflight.current = true;
    setBusy(true);
    setError(null);
    try {
      const result = await o.run(value, key);
      if (isOperation(result)) track(result, label);
      const message = typeof o.success === 'function' ? o.success(result, value) : o.success;
      if (message !== false) toast(message ?? (isOperation(result) ? `${label}已受理` : `${label}已完成`), isOperation(result) ? 'info' : 'ok');
      o.onSuccess?.(result, value);
      return result;
    } catch (reason) {
      setError(reason);
      if (isUnknown(reason) && !sentKeys.has(key)) {
        // 请求没有请求键，无法按原键核对：重新读取目标现状。
        toast(`${label}结果未知`, 'warn', { detail: '请先重新读取确认当前状态，再决定是否重试', action: o.onConflict ? { label: '重新读取', run: o.onConflict } : undefined });
        o.onConflict?.();
      } else if (isUnknown(reason)) {
        rememberRequest(key, node, label);
        const message = reason instanceof ApiError && reason.operationId ? errorText(reason) : `${label}结果未知`;
        toast(message, 'warn', { detail: '已保留原请求，可在操作坞核对，不会自动重发', sticky: false });
      } else if (needsReread(reason)) {
        toast(`${label}：${errorText(reason)}`, 'warn', o.onConflict ? { action: { label: '重新读取', run: o.onConflict } } : {});
        o.onConflict?.();
      } else if (reason instanceof ApiError && reason.status === 429) {
        toast(errorText(reason), 'warn');
      } else {
        toast(`${label}失败：${errorText(reason)}`, 'bad');
      }
      return undefined;
    } finally {
      sentKeys.delete(key);
      inflight.current = false;
      setBusy(false);
    }
  }, []);

  return { run, busy, error, reset: () => setError(null) };
}
