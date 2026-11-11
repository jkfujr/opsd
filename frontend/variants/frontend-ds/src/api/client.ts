import createClient from 'openapi-fetch';
import type { components, paths } from './schema.gen';
import { queryClient } from '../state';

export type Schema = components['schemas'];

export const api = createClient<paths>({
  baseUrl: '/api/v1',
  credentials: 'same-origin',
});

api.use({
  onError({ request }) {
    if (!request.signal.aborted) return new Error('连接中断，请重新读取或查询原操作');
    return undefined;
  },
  onRequest({ request }) {
    const session = queryClient.getQueryData<Schema['CurrentSession']>(['session']);
    if (session && !['GET', 'HEAD'].includes(request.method)) {
      request.headers.set('X-CSRF-Token', session.csrf_token);
      // 写请求都带幂等键：应答丢失时客户端可以凭它查询原操作。
      if (!request.headers.has('Idempotency-Key')) request.headers.set('Idempotency-Key', newKey());
    }
    return request;
  },
  onResponse({ response, schemaPath }) {
    if (response.status === 401 && schemaPath !== '/auth/login') {
      const session = queryClient.getQueryData<Schema['CurrentSession']>(['session']);
      if (session) {
        queryClient.clear();
        window.location.assign(session.login_address);
      }
    }
    return response;
  },
});

export class RequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
  ) {
    super(message);
  }
}

/** 统一拆包：非 2xx 一律抛出带后端 message 的错误。 */
export function value<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (!result.response.ok) {
    const error = result.error as Schema['APIError'] | undefined;
    throw new RequestError(error?.message || `请求失败（${result.response.status}）`, result.response.status, error?.code);
  }
  return result.data as T;
}

export const idempotency = () => ({ 'Idempotency-Key': newKey() });

export function newKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

export function message(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  return '请求失败，请重试';
}

