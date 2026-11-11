import { useEffect, useState } from 'react';
import { queryClient } from '../state';
import type { components } from './types';

export type PendingRequest = { key: string; nodeID: string; path: string; at: string; resource?: boolean; method?: string };
function storageKey() { const session = queryClient.getQueryData<components['schemas']['CurrentSession']>(['session']); return `opsd.frontend-gpt2.requests.${session?.local_node_id ?? 'anonymous'}`; }
export function pendingRequests(): PendingRequest[] { try { const value = JSON.parse(sessionStorage.getItem(storageKey()) ?? '[]'); return Array.isArray(value) ? value.filter(v => typeof v.key === 'string' && typeof v.nodeID === 'string' && typeof v.path === 'string' && typeof v.at === 'string') : []; } catch { return []; } }
function save(values: PendingRequest[]) { sessionStorage.setItem(storageKey(), JSON.stringify(values)); window.dispatchEvent(new Event('opsd:requests')); }
export function clearPendingRequests() { sessionStorage.removeItem(storageKey()); window.dispatchEvent(new Event('opsd:requests')); }
export function forgetRequest(key: string) { save(pendingRequests().filter(value => value.key !== key)); }
export function usePendingRequests() { const [values, setValues] = useState(pendingRequests); useEffect(() => { const update = () => setValues(pendingRequests()); window.addEventListener('opsd:requests', update); return () => window.removeEventListener('opsd:requests', update); }, []); return values; }
export function requestReference(request: Request) {
  if (['GET', 'HEAD'].includes(request.method)) return '';
  const path = new URL(request.url).pathname.replace(/^\/api\/v2/, '');
  if (path.startsWith('/auth/') || path.endsWith('/terminal') || path.endsWith('/discover') || path.endsWith('/account/actions')) return '';
  return request.headers.get('Idempotency-Key') ?? `resource:${request.method}:${path}`;
}
export async function rememberRequest(request: Request, targetNodeID?: string) {
  const key = requestReference(request);
  if (!key) return;
  const path = new URL(request.url).pathname.replace(/^\/api\/v2/, '');
  const session = queryClient.getQueryData<components['schemas']['CurrentSession']>(['session']);
  // 操作与数据库的所属节点由发送方固定，不依赖可能过期或缺失的查询缓存。
  if (!targetNodeID && /^\/(operations|database-clusters)(\/|$)/.test(path)) return;
  const nodeID = targetNodeID ?? path.match(/^\/nodes\/([^/]+)/)?.[1] ?? session?.local_node_id ?? '';
  const previous = pendingRequests();
  if (!previous.some(value => value.key === key)) save([...previous, { key, nodeID: decodeURIComponent(nodeID), path: !request.headers.has('Idempotency-Key') && path.endsWith('/actions') ? path.slice(0, -8) : path, at: new Date().toISOString(), resource: !request.headers.has('Idempotency-Key'), method: request.method }]);
}
export function requestName(path: string) {
  if (path.includes('enrollment-tokens')) return '生成接入码';
  if (path.includes('database')) return '数据库变更';
  if (path.includes('firewall')) return '防火墙变更';
  if (path.includes('compose')) return 'Compose 操作';
  if (path.includes('containers')) return '容器操作';
  if (path.includes('tasks')) return '任务操作';
  if (path.includes('theme')) return '主题操作';
  if (path.includes('account')) return '保存账号';
  if (path.includes('public-addresses')) return '保存公网地址';
  if (path.includes('api-tokens')) return 'API 令牌操作';
  return '保存配置';
}
