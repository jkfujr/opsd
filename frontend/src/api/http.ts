// 精简的 HTTP 层：直接对应后端契约（Problem、CSRF、Idempotency-Key、ETag、Retry-After）。
import type { Problem } from './types';

export const API = '/api/v2';

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly outcome?: 'not_accepted' | 'unknown';
  readonly operationId?: string;
  readonly resourceUrl?: string;
  readonly requestId?: string;
  readonly retryAt?: number;
  readonly fields: { pointer: string; message: string }[];

  constructor(status: number, message: string, extra: Partial<Problem> & { retryAt?: number; requestId?: string } = {}) {
    super(message);
    this.status = status;
    this.code = extra.code ?? (status === 0 ? 'network' : `http_${status}`);
    this.outcome = extra.outcome ?? (status === 0 ? 'unknown' : undefined);
    this.operationId = extra.operation_id;
    this.resourceUrl = extra.resource_url;
    this.requestId = extra.request_id || extra.requestId;
    this.retryAt = extra.retryAt;
    this.fields = (extra.errors ?? []).map((e) => ({ pointer: e.pointer, message: e.message }));
  }
}

/** 结果是否未知：网络中断、显式 unknown、或未声明 not_accepted 的 5xx。此时不能换键重发。 */
export function isUnknown(error: unknown): boolean {
  if (!(error instanceof ApiError)) return !(error instanceof DOMException && error.name === 'AbortError');
  if (error.code === 'secret_unavailable') return false;
  return error.status === 0 || error.outcome === 'unknown' || (error.status >= 500 && error.outcome !== 'not_accepted');
}

/** 版本冲突或缺少条件：需要重新读取后再提交。 */
export function needsReread(error: unknown) {
  return error instanceof ApiError && !isUnknown(error) && [409, 412, 428].includes(error.status) && error.code !== 'secret_unavailable';
}

export function errorText(error: unknown): string {
  if (!error) return '';
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return String(error);
}

/** 已随请求发出的请求键；用于判断“结果未知”时能否按原键核对。 */
export const sentKeys = new Set<string>();

let csrfToken = '';
export const setCsrf = (token: string) => { csrfToken = token; };

export type Query = Record<string, string | number | boolean | undefined | null>;
export interface CallOptions {
  query?: Query;
  body?: unknown;
  raw?: BodyInit;
  contentType?: string;
  key?: string;
  ifMatch?: string;
  ifNoneMatch?: string;
  signal?: AbortSignal;
  /** 401 时不广播会话结束（登录、会话探测）。 */
  quiet401?: boolean;
}
export interface Response_<T> {
  data: T;
  status: number;
  etag?: string;
  location?: string;
}

const cooldowns = new Map<string, ApiError>();

export function url(path: string, query?: Query) {
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query ?? {})) if (v !== undefined && v !== null && v !== '') params.set(k, String(v));
  const qs = params.toString();
  return API + path + (qs ? `?${qs}` : '');
}

export async function call<T = unknown>(method: string, path: string, options: CallOptions = {}): Promise<Response_<T>> {
  const target = url(path, options.query);
  const limiter = `${method} ${path}`;
  const cooling = cooldowns.get(limiter);
  if (cooling?.retryAt && cooling.retryAt > Date.now()) throw cooling;
  cooldowns.delete(limiter);

  const headers = new Headers({ Accept: 'application/json, application/problem+json' });
  let body: BodyInit | undefined;
  if (options.raw !== undefined) {
    body = options.raw;
    headers.set('Content-Type', options.contentType ?? 'application/octet-stream');
  } else if (options.body !== undefined) {
    body = JSON.stringify(options.body);
    headers.set('Content-Type', options.contentType ?? (method === 'PATCH' ? 'application/merge-patch+json' : 'application/json'));
  }
  if (method !== 'GET' && method !== 'HEAD' && csrfToken) headers.set('X-CSRF-Token', csrfToken);
  if (options.key) { headers.set('Idempotency-Key', options.key); sentKeys.add(options.key); }
  if (options.ifMatch) headers.set('If-Match', options.ifMatch);
  if (options.ifNoneMatch) headers.set('If-None-Match', options.ifNoneMatch);

  let response: Response;
  try {
    response = await fetch(target, { method, headers, body, credentials: 'same-origin', cache: 'no-store', signal: options.signal });
  } catch (reason) {
    if (reason instanceof DOMException && reason.name === 'AbortError') throw reason;
    throw new ApiError(0, '连接中断，结果未知');
  }

  if (response.status === 401 && !options.quiet401) window.dispatchEvent(new Event('opsd:unauthorized'));

  const type = response.headers.get('Content-Type') ?? '';
  if (!response.ok) {
    let problem: Partial<Problem> = {};
    if (type.includes('json')) {
      try { problem = await response.json(); } catch { /* 保留状态码 */ }
    }
    const after = response.headers.get('Retry-After');
    const retryAt = after ? (/^\d+$/.test(after) ? Date.now() + Number(after) * 1000 : Date.parse(after)) : undefined;
    const error = new ApiError(response.status, problem.detail || problem.title || fallback(response.status), {
      ...problem,
      retryAt: retryAt && Number.isFinite(retryAt) ? retryAt : undefined,
      requestId: problem.request_id || response.headers.get('X-Request-ID') || undefined,
    });
    if (response.status === 429) cooldowns.set(limiter, error);
    throw error;
  }
  let data: unknown = undefined;
  if (response.status !== 204 && type.includes('json')) data = await response.json();
  else if (response.status !== 204 && type.startsWith('text/')) data = await response.text();
  return {
    data: data as T,
    status: response.status,
    etag: response.headers.get('ETag') ?? undefined,
    location: response.headers.get('Location') ?? undefined,
  };
}

function fallback(status: number) {
  if (status === 403) return '没有权限执行此操作';
  if (status === 404) return '资源不存在或已移除';
  if (status === 409) return '状态已变化，请重新读取';
  if (status === 412) return '内容已被修改，请重新读取后再提交';
  if (status === 429) return '请求过于频繁，请稍后再试';
  if (status === 503) return '目标节点暂不可用';
  if (status === 504) return '目标节点响应超时';
  return `请求失败（${status}）`;
}

export const get = async <T>(path: string, options?: CallOptions) => (await call<T>('GET', path, options)).data;

/** 读取带版本的资源：配置类写入需要原 ETag。 */
export interface Versioned<T> { value: T; etag?: string }
export const getVersioned = async <T>(path: string, options?: CallOptions): Promise<Versioned<T>> => {
  const r = await call<T>('GET', path, options);
  return { value: r.data, etag: r.etag };
};

export function newKey(prefix = 't1') {
  const id = typeof crypto !== 'undefined' && 'randomUUID' in crypto ? crypto.randomUUID() : Math.random().toString(36).slice(2) + Date.now().toString(36);
  return `${prefix}-${id}`.slice(0, 128);
}

export const enc = encodeURIComponent;

/** 分页集合：沿 next_cursor 读完（上限防止异常游标循环）。 */
export async function readAll<T extends { items: unknown[]; next_cursor?: string }>(read: (cursor?: string) => Promise<T>, limit = 20): Promise<T> {
  let page = await read();
  const seen = new Set<string>();
  let merged = page;
  for (let i = 0; i < limit && page.next_cursor && !seen.has(page.next_cursor); i++) {
    seen.add(page.next_cursor);
    page = await read(page.next_cursor);
    merged = { ...merged, ...page, items: [...merged.items, ...page.items] } as T;
  }
  return merged;
}
