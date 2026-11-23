import { createContext, useContext, useEffect, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ApiError, call, setCsrf } from '../api/http';
import type { CurrentSession } from '../api/types';
import { queryClient } from './query';

const Context = createContext<CurrentSession | null>(null);
export const LOGIN_KEY = 'opsd.t1.login';
/** 记录最近一次已知的登录入口。账号修改后服务端会返回新入口，立即写入以免 401 处理器用旧值跳转。 */
export function setLoginAddress(address: string) {
  try { localStorage.setItem(LOGIN_KEY, address); } catch { /* 忽略 */ }
}

export function useSession() {
  const value = useContext(Context);
  if (!value) throw new Error('需要管理员登录');
  return value;
}
export const SessionContext = Context;

export async function readSession(): Promise<CurrentSession | null> {
  try {
    const r = await call<CurrentSession>('GET', '/auth/sessions/current', { quiet401: true });
    setCsrf(r.data.csrf_token);
    setLoginAddress(r.data.login_address);
    return r.data;
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return null;
    throw error;
  }
}

export function useSessionQuery() {
  return useQuery({ queryKey: ['session'], queryFn: readSession, retry: 1, staleTime: Infinity, gcTime: Infinity, refetchOnWindowFocus: false });
}

/** 内置恢复入口：主题异常时由服务端在此路径下提供本界面。 */
export const BUILTIN = '/__opsd/builtin';
export const inBuiltin = window.location.pathname === BUILTIN || window.location.pathname.startsWith(BUILTIN + '/');
const forcedBuiltin = () => inBuiltin || new URLSearchParams(window.location.search).get('builtin') === '1';

/** 登录成功后的去向：恢复模式回到内置入口，普通模式回到根路径（由服务端选择主题或内置界面）。 */
export function loginDestination() {
  const params = new URLSearchParams(window.location.search);
  const builtin = params.get('builtin') === '1';
  const target = params.get('return_to');
  if (target && target.startsWith('/') && !target.startsWith('//') && !target.includes('\\')) {
    const url = new URL(target, window.location.origin);
    const isBuiltin = url.pathname === BUILTIN || url.pathname.startsWith(BUILTIN + '/');
    if (url.origin === window.location.origin && !url.pathname.startsWith('/api/') && (builtin ? isBuiltin : !url.pathname.startsWith('/__opsd'))) return url.pathname + url.search + url.hash;
  }
  return builtin ? BUILTIN + '/' : '/';
}

let ending = false;
let unauthorizedHolds = 0;
let deferredUnauthorized = false;
/**
 * 账号修改会主动注销会话，期间并发请求可能先返回 401，抢在成功应答之前用改前的入口跳走。
 * 用成对的 hold/release 绑定到这一次请求上：期间收到的 401 先缓存，release 时再补处理。
 */
export function holdUnauthorized() { unauthorizedHolds++; }
export function releaseUnauthorized(flush = true) {
  if (unauthorizedHolds > 0) unauthorizedHolds--;
  if (unauthorizedHolds > 0 || !deferredUnauthorized) return;
  deferredUnauthorized = false;
  if (flush) endSession(localStorage.getItem(LOGIN_KEY) || undefined);
}
/** 会话结束：清理缓存与待核对请求，回到登录入口并保留站内返回地址；恢复模式保持强制内置登录。 */
export function endSession(loginAddress?: string, keepReturn = true) {
  if (ending) return;
  ending = true;
  const address = loginAddress || localStorage.getItem(LOGIN_KEY) || '/';
  const target = new URL(address, window.location.origin);
  const here = window.location.pathname + window.location.search + window.location.hash;
  if (keepReturn && window.location.pathname !== target.pathname && here !== '/') target.searchParams.set('return_to', here);
  if (forcedBuiltin()) target.searchParams.set('builtin', '1');
  void queryClient.cancelQueries();
  queryClient.clear();
  setCsrf('');
  try { sessionStorage.removeItem('opsd.t1.requests'); } catch { /* 忽略 */ }
  window.location.assign(target.pathname + target.search);
}

export function SessionProvider({ value, children }: { value: CurrentSession; children: ReactNode }) {
  useEffect(() => {
    const onUnauthorized = () => {
      if (unauthorizedHolds > 0) { deferredUnauthorized = true; return; }
      endSession(value.login_address);
    };
    window.addEventListener('opsd:unauthorized', onUnauthorized);
    return () => window.removeEventListener('opsd:unauthorized', onUnauthorized);
  }, [value.login_address]);
  return <Context value={value}>{children}</Context>;
}

/** @deprecated 使用 loginDestination。 */
export function returnTarget() {
  const target = new URLSearchParams(window.location.search).get('return_to');
  if (target && target.startsWith('/') && !target.startsWith('//') && !target.includes('\\') && !target.startsWith('/api/') && !target.startsWith('/__opsd')) return target;
  return '/';
}
