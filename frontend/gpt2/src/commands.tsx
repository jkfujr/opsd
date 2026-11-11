import { useRef } from 'react';
import { useMutation } from '@tanstack/react-query';
import { RequestError, unknownResult, useRetryAfter } from './api/client';
import { newRequestKey, Failure } from './business-ui';
import { Btn } from './ui';
import { useOperations } from './operations';
export function useCommand<T, V>(send: (value: V, key: string) => Promise<T>, done: (value: T) => void) {
  const key = useRef('');
  const mutation = useMutation({ retry: false, mutationFn: (value: V) => { key.current ||= newRequestKey(); return send(value, key.current); }, onSuccess: value => { key.current = ''; done(value); }, onError: error => { if (!unknownResult(error)) key.current = ''; } });
  const uncertain = mutation.isError && unknownResult(mutation.error);
  const cooldown = useRetryAfter(mutation.error);
  return { ...mutation, uncertain, blocked: mutation.isPending || uncertain || cooldown > 0, run: (value: V) => { if (!mutation.isPending && !uncertain && !cooldown) mutation.mutate(value); } };
}
export function CommandError({ error, read }: { error: unknown; read?: () => void }) {
  const operations = useOperations();
  return <><Failure error={error}/>{!!error && (unknownResult(error) ? <Btn onClick={() => operations.show('')}>查询原请求</Btn> : error instanceof RequestError && [409, 412, 428].includes(error.status) && read ? <Btn onClick={read}>重新读取</Btn> : null)}</>;
}
