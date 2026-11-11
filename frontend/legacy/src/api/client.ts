import createClient from 'openapi-fetch';
import type { paths } from './schema.gen';
import type { components } from './schema.gen';
import { queryClient } from '../state';
import { loginAddress } from '../location';

export const api = createClient<paths>({
  baseUrl: '/api/v2',
  credentials: 'same-origin',
});

api.use({
  onError({ request }) { if (!request.signal.aborted) return new Error('连接中断，请重新读取或查询原操作'); },
  onRequest({ request }) {
    const session = queryClient.getQueryData<components['schemas']['CurrentSession']>(['session']);
    if (session && !['GET', 'HEAD'].includes(request.method)) request.headers.set('X-CSRF-Token', session.csrf_token);
    if (request.method === 'PATCH') request.headers.set('Content-Type', 'application/merge-patch+json');
    return request;
  },
  onResponse({ response, schemaPath }) {
    if (response.status === 401 && schemaPath !== '/auth/sessions' && queryClient.isMutating({ mutationKey: ['account-change'] }) === 0) {
      const session = queryClient.getQueryData<components['schemas']['CurrentSession']>(['session']);
      if (session) { queryClient.clear(); window.location.assign(loginAddress(session.login_address)); }
    }
    return response;
  },
});

export class RequestError extends Error {
  constructor(message: string, readonly status: number, readonly code?: string, readonly outcome?: components['schemas']['Problem']['outcome'], readonly operationID?: string, readonly resourceURL?: string) { super(message); }
}

export function unknownResult(error: unknown) {
  return !(error instanceof RequestError) || error.outcome === 'unknown' || (error.status >= 500 && error.outcome !== 'not_accepted');
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

export async function allPages<T extends { items: unknown[]; next_cursor?: string }>(read: (cursor?: string) => Promise<T>): Promise<T> {
  const first = await read();
  const items = [...first.items];
  let cursor = first.next_cursor;
  let last = first;
  const partial = (value: T) => {
    const state = value as T & { unavailable?: unknown[]; nodes?: { read?: { state: string } }[] };
    return Boolean(state.unavailable?.length || state.nodes?.some((node) => ['offline', 'unavailable'].includes(node.read?.state ?? '')));
  };
  const seen = new Set<string>();
  while (cursor) {
    if (seen.has(cursor)) { if (partial(last)) break; throw new Error('分页位置重复，请重新读取'); }
    seen.add(cursor);
    let next: T;
    try { next = await read(cursor); } catch (error) {
      if (items.length && partial(last) && error instanceof RequestError && error.status === 503) break;
      throw error;
    }
    items.push(...next.items);
    cursor = next.next_cursor;
    last = next;
    if (!next.items.length && partial(next)) break;
  }
  return { ...last, items, next_cursor: cursor };
}

export function unwrap<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (!result.response.ok) {
    const error = result.error as components['schemas']['Problem'] | undefined;
    throw new RequestError(error?.detail || error?.title || '请求失败，请重试', result.response.status, error?.code, error?.outcome, error?.operation_id, error?.resource_url);
  }
  const tag = result.response.headers.get('ETag');
  if (tag && result.data && typeof result.data === 'object') revisions.set(result.data, tag);
  return result.data as T;
}
