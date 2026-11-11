import createClient, { type Middleware } from 'openapi-fetch';
import { useEffect, useState } from 'react';
import type { paths } from './types';
import type { components } from './types';
import { queryClient } from '../state';
import { rememberRequest, forgetRequest, requestReference } from './requests';

export const api = createClient<paths>({
  baseUrl: '/api/v2',
  credentials: 'same-origin',
});

const cooldowns = new Map<string, RequestError>();
const cooldownKey = (request: Request) => `${request.method} ${new URL(request.url).pathname}`;
window.addEventListener('opsd:session-clearing', () => cooldowns.clear());

export function targetNode(nodeID: string): { middleware: Middleware[] } {
  return { middleware: [{ onRequest: async ({ request }) => { await rememberRequest(request, nodeID); } }] };
}

api.use({
  onError({ request, error }) { if (error instanceof RequestError) return error; if (!request.signal.aborted) return new Error('连接中断，请重新读取或查询原操作'); },
  async onRequest({ request }) {
    const cooldown = cooldowns.get(cooldownKey(request));
    if (cooldown?.retryAt && cooldown.retryAt > Date.now()) throw cooldown;
    cooldowns.delete(cooldownKey(request));
    const session = queryClient.getQueryData<components['schemas']['CurrentSession']>(['session']);
    if (session && !['GET', 'HEAD'].includes(request.method)) request.headers.set('X-CSRF-Token', session.csrf_token);
    if (request.method === 'PATCH') request.headers.set('Content-Type', 'application/merge-patch+json');
    await rememberRequest(request);
    return request;
  },
  async onResponse({ response, schemaPath, request }) {
    if (response.status === 429) {
      let problem: components['schemas']['Problem'] | undefined;
      try { problem = await response.clone().json(); } catch { /* 保留 HTTP 状态及等待时间。 */ }
      cooldowns.set(cooldownKey(request), responseError(response, problem));
    }
    const key = requestReference(request);
    if (key) {
      let problem: { outcome?: string; code?: string } = {};
      if (!response.ok) { try { problem = await response.clone().json(); } catch { /* 非 JSON 错误保留原请求引用。 */ } }
      if (response.ok || problem.outcome === 'not_accepted' || response.status < 500 && problem.outcome !== 'unknown' && problem.code !== 'secret_unavailable') forgetRequest(key);
    }
    if (response.status === 401 && schemaPath !== '/auth/sessions' && queryClient.isMutating({ mutationKey: ['account-change'] }) === 0) {
      const session = queryClient.getQueryData<components['schemas']['CurrentSession']>(['session']);
      if (session) { window.dispatchEvent(new Event('opsd:session-ended')); }
    }
    return response;
  },
});

export class RequestError extends Error {
  constructor(message: string, readonly status: number, readonly code?: string, readonly outcome?: components['schemas']['Problem']['outcome'], readonly operationID?: string, readonly resourceURL?: string, readonly problem?: components['schemas']['Problem'], readonly retryAt?: number, readonly requestID?: string) { super(message); }
}

function responseError(response: Response, problem?: components['schemas']['Problem']) {
  const after = response.headers.get('Retry-After');
  const retryAt = after ? /^\d+$/.test(after) ? Date.now() + Number(after) * 1000 : Date.parse(after) : undefined;
  return new RequestError(problem?.detail || problem?.title || '请求失败，请重试', response.status, problem?.code, problem?.outcome, problem?.operation_id, problem?.resource_url, problem, retryAt && Number.isFinite(retryAt) ? retryAt : undefined, problem?.request_id || response.headers.get('X-Request-ID') || undefined);
}

export function useRetryAfter(error: unknown) {
  const deadline = error instanceof RequestError ? error.retryAt ?? 0 : 0;
  const remaining = () => Math.max(0, Math.ceil((deadline - Date.now()) / 1000));
  const [seconds, setSeconds] = useState(remaining);
  useEffect(() => { setSeconds(remaining()); if (!deadline) return; const timer = window.setInterval(() => setSeconds(remaining()), 250); return () => window.clearInterval(timer); }, [deadline]);
  return deadline ? seconds : 0;
}

export function unknownResult(error: unknown) {
  return !(error instanceof RequestError) || error.code === 'secret_unavailable' || error.outcome === 'unknown' || (error.status >= 500 && error.outcome !== 'not_accepted');
}

const revisions = new WeakMap<object, string>();

export function condition(value: object | undefined): { 'If-Match': string } {
  const revision = value && revisions.get(value);
  if (!revision) throw new RequestError('请重新读取资源后再修改', 428, 'precondition_required');
  return { 'If-Match': revision };
}

export async function findOperation(nodeID: string, key: string) {
  const receipt = unwrap(await api.GET('/requests/{request_key}', { params: { path: { request_key: key }, query: { node_id: nodeID } } }));
  if (!receipt.operation_id) throw new Error('原请求尚未返回操作记录，请保留请求键后再次查询');
  return unwrap(await api.GET('/operations/{id}', { params: { path: { id: receipt.operation_id } } }));
}

type Collection = { items: unknown[]; next_cursor?: string };
const pagination = new WeakMap<object, { read: (cursor?: string) => Promise<Collection>; error?: unknown; resumable: boolean }>();
export const pageIssue = (data: object | undefined) => data && pagination.get(data)?.error;
export const canContinue = (data: Collection | undefined) => !!data?.next_cursor && pagination.get(data)?.resumable !== false;
export async function continuePages<T extends Collection>(data: T): Promise<T> {
  const context = pagination.get(data);
  return context ? allPages(context.read as (cursor?: string) => Promise<T>, data) : data;
}
function itemKey(value: unknown) {
  if (!value || typeof value !== 'object') return JSON.stringify(value);
  const fields = value as Record<string, unknown>, cluster = fields.cluster as { id?: string } | undefined;
  return [fields.node_id, fields.engine_id, fields.instance_id, fields.container_id, fields.id ?? cluster?.id].filter(Boolean).join('/') || JSON.stringify(value);
}
function mergeItems(first: unknown[], next: unknown[]) { return [...new Map([...first, ...next].map(value => [itemKey(value), value])).values()]; }
export async function allPages<T extends Collection>(read: (cursor?: string) => Promise<T>, previous?: T): Promise<T> {
  let value = previous ?? await read(), error: unknown, resumable = true;
  const seen = new Set<string>();
  const partial = (page: T) => {
    const fields = page as T & { unavailable?: unknown[]; nodes?: { read?: { state: string }; state?: string }[] };
    return Boolean(fields.unavailable?.length || fields.nodes?.some(node => node.read?.state !== 'ready' || node.state === 'unavailable'));
  };
  while (value.next_cursor) {
    const cursor = value.next_cursor;
    if (seen.has(cursor)) { if (!partial(value)) { error = new Error('分页位置重复，请重新读取'); resumable = false; } break; }
    seen.add(cursor);
    let next: T;
    try { next = await read(cursor); } catch (reason) {
      if (reason instanceof DOMException && reason.name === 'AbortError') throw reason;
      error = reason; break;
    }
    const merged = { ...value, ...next, items: mergeItems(value.items, next.items) } as T;
    for (const key of ['nodes', 'unavailable']) {
      const older = (value as Record<string, unknown>)[key], newer = (next as Record<string, unknown>)[key];
      if (Array.isArray(older) || Array.isArray(newer)) (merged as Record<string, unknown>)[key] = mergeItems(Array.isArray(older) ? older : [], Array.isArray(newer) ? newer : []);
    }
    const fields = merged as Record<string, unknown>;
    if (Array.isArray(fields.unavailable) && Array.isArray(fields.nodes)) {
      const recovered = new Set(fields.nodes.filter(node => node?.read?.state === 'ready').map(node => node.node_id));
      fields.unavailable = fields.unavailable.filter(node => !recovered.has(node.node_id));
    }
    value = merged;
    if (!next.items.length && partial(next)) break;
  }
  pagination.set(value, { read, error, resumable });
  return value;
}

export function unwrap<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (!result.response.ok) {
    const error = result.error as components['schemas']['Problem'] | undefined;
    throw responseError(result.response, error);
  }
  const tag = result.response.headers.get('ETag');
  if (tag && result.data && typeof result.data === 'object') revisions.set(result.data, tag);
  return result.data as T;
}

export async function readResourceReference(path: string, method?: string) {
  const url = new URL(path.startsWith('/api/v2/') || /^https?:\/\//.test(path) ? path : '/api/v2' + path, window.location.origin);
  if (url.origin !== window.location.origin || !url.pathname.startsWith('/api/v2/')) throw new RequestError('原资源地址无效', 422);
  const response = await fetch(url, { credentials: 'same-origin', cache: 'no-store' });
  if (response.status === 401) window.dispatchEvent(new Event('opsd:session-ended'));
  if (response.status === 404 && method === 'DELETE') return { resource: true as const, removed: true };
  if (!response.ok) { let problem: components['schemas']['Problem'] | undefined; try { problem = await response.json(); } catch {} throw responseError(response, problem); }
  return { resource: true as const, removed: false };
}
