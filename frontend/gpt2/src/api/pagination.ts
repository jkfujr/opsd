import { useMutation, useQuery, type QueryKey, type UseQueryOptions } from '@tanstack/react-query';
import { queryClient } from '../state';
import { canContinue, continuePages, pageIssue } from './client';

// 继续读取仅合并到发起时的同一集合；筛选变化或后台重新读取后不覆盖新数据。
export function usePagedQuery<T extends { items: unknown[]; next_cursor?: string }>(options: UseQueryOptions<T, Error, T, QueryKey>) {
  const query = useQuery(options);
  const more = useMutation({ mutationFn: (current: T) => continuePages(current), onSuccess: (data, current) => {
    queryClient.setQueryData<T>(options.queryKey, value => value === current ? data : value);
  } });
  return { ...query, pageError: more.error ?? pageIssue(query.data), hasMore: canContinue(query.data), isReadingMore: more.isPending,
    readMore: () => { if (query.data && !query.isFetching && !more.isPending) more.mutate(query.data); } };
}
