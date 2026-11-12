import type { components } from './api/types';

type Operation = components['schemas']['Operation'];
type Chunk = components['schemas']['ExecutionLogs'];
export type ExecutionLogData = { bytes: Uint8Array; next_offset: number; has_more: boolean; truncated: boolean };

export const operationHasLogs = (value: Operation['subject'] | Operation) => typeof value === 'string' ? ['task_run', 'container', 'compose'].includes(value) : value.supports_logs ?? ['task_run', 'container', 'compose'].includes(value.subject);
export function operationLogURL(id: string, returnTo: string) {
  return '/tools?' + new URLSearchParams({ view: 'logs', operation: id, return_to: returnTo });
}
export function containerLogURL(item: { node_id: string; engine_id: string; container_id: string }, returnTo: string) {
  return '/containers?' + new URLSearchParams({ view: 'logs', node_id: item.node_id, engine_id: item.engine_id, container_id: item.container_id, return_to: returnTo });
}
export function logReturnURL(value: string | null, fallback: string) {
  if (!value || !/^\/(?:nodes|containers|databases|firewall|tools|settings|storage)?(?:[/?#]|$)/.test(value) || value.startsWith('//') || value.includes('\\')) return fallback;
  const url = new URL(value, 'http://opsd.local');
  return url.searchParams.get('view') === 'logs' ? fallback : url.pathname + url.search + url.hash;
}

// 执行日志按字节追加；仅在拼接后解码，避免中文跨页损坏或刷新时重复。
export function appendExecutionLog(previous: ExecutionLogData | undefined, chunk: Chunk): ExecutionLogData {
  const start = previous?.next_offset ?? 0;
  const next = Uint8Array.from(atob(chunk.data), value => value.charCodeAt(0));
  if (chunk.offset !== start || chunk.next_offset !== start + next.length || chunk.has_more && !next.length) throw new Error('日志分页位置无效，请重试');
  const bytes = new Uint8Array(start + next.length);
  if (previous) bytes.set(previous.bytes);
  bytes.set(next, start);
  return { bytes, next_offset: chunk.next_offset, has_more: chunk.has_more, truncated: chunk.truncated || !!previous?.truncated };
}
export function executionLogText(data?: ExecutionLogData) {
  return data ? new TextDecoder().decode(data.bytes, { stream: data.has_more }) : '';
}

// 清除 CSI、OSC、DCS 及单字符控制序列，只留下文本、制表与换行。
export function cleanLog(text: string) {
  return text.replace(/(?:\x1b\[|\x9b)[0-?]*[ -/]*[@-~]/g, '')
    .replace(/(?:\x1b\]|\x9d)[^\x07\x1b\x9c]*(?:\x07|\x1b\\|\x9c|$)/g, '')
    .replace(/\x1b[P^_][\s\S]*?(?:\x1b\\|$)/g, '')
    .replace(/\x1b[ -/]*[@-Z\\-_]/g, '')
    .replace(/(?:\x1b\[|\x9b)[0-?]*[ -/]*$/g, '')
    .replace(/\r\n?/g, '\n').replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]/g, '');
}
