import { useQuery } from '@tanstack/react-query';
import { api, value } from '../../api/client';
import { Failure, Sheet, Skeleton } from '../../components/ui';

export function Version() {
  const query = useQuery({
    queryKey: ['settings', 'version'],
    queryFn: async ({ signal }) => value(await api.GET('/settings/version', { signal })),
    retry: false,
  });
  return (
    <Sheet title="版本">
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      {query.isPending ? <Skeleton height={80} /> : null}
      <dl className="facts" style={{ fontSize: 13 }}>
        <dt>节点程序</dt>
        <dd className="mono">{query.data?.node_version ?? '—'}</dd>
        <dt>前端资源</dt>
        <dd className="mono">{query.data?.frontend_version ?? '—'}</dd>
        <dt>接口契约</dt>
        <dd className="mono">api/openapi.json · 59 条路径</dd>
      </dl>
    </Sheet>
  );
}
