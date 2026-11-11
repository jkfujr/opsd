import createClient from 'openapi-fetch';
import type { paths, components } from './schema.gen';
import { queryClient } from '../state';

export const api = createClient<paths>({
  baseUrl: '/api/v1',
  credentials: 'same-origin',
});

api.use({
  onError({ request }) {
    if (!request.signal.aborted) {
      return new Error('连接中断，请重新读取或查询原操作');
    }
  },
  onRequest({ request }) {
    const session = queryClient.getQueryData<components['schemas']['CurrentSession']>(['session']);
    if (session && !['GET', 'HEAD'].includes(request.method)) {
      request.headers.set('X-CSRF-Token', session.csrf_token);
    }
    return request;
  },
  onResponse({ response, schemaPath }) {
    if (
      response.status === 401 &&
      schemaPath !== '/auth/login' &&
      queryClient.isMutating({ mutationKey: ['account-change'] }) === 0
    ) {
      const session = queryClient.getQueryData<components['schemas']['CurrentSession']>(['session']);
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

export function unwrap<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (!result.response.ok) {
    const error = result.error as components['schemas']['APIError'] | undefined;
    throw new RequestError(error?.message || '请求失败，请重试', result.response.status, error?.code);
  }
  return result.data as T;
}
