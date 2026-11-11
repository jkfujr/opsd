import { useMutation, useQuery } from '@tanstack/react-query';
import { api, value } from '../../api/client';
import { queryClient } from '../../state';
import { ago, stamp } from '../../lib/format';
import { Button, Confirm, Empty, Failure, Sheet, Skeleton, Stamp } from '../../components/ui';
import { useState } from 'react';

export function Sessions() {
  const [revoking, setRevoking] = useState<string | null>(null);
  const query = useQuery({
    queryKey: ['sessions'],
    queryFn: async ({ signal }) => value(await api.GET('/auth/sessions', { signal })),
  });
  const revoke = useMutation({
    mutationFn: async (id: string) => value(await api.DELETE('/auth/sessions/{id}', { params: { path: { id } } })),
    onSuccess: () => {
      setRevoking(null);
      void queryClient.invalidateQueries({ queryKey: ['sessions'] });
    },
  });

  return (
    <Sheet title="会话">
      <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
      {query.isPending ? <Skeleton height={120} /> : null}
      {query.data?.items.length === 0 ? <Empty>没有活动会话</Empty> : null}
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th scope="col">来源地址</th>
              <th scope="col">创建</th>
              <th scope="col">最近活跃</th>
              <th scope="col">到期</th>
              <th scope="col" className="right">操作</th>
            </tr>
          </thead>
          <tbody>
            {query.data?.items.map((session) => (
              <tr key={session.id}>
                <td>
                  <span className="row gap-8">
                    <span className="mono" style={{ fontSize: 11.5 }}>{session.remote_address}</span>
                    {session.current ? <Stamp state="online">当前</Stamp> : null}
                  </span>
                </td>
                <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{stamp(session.created_at)}</td>
                <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{ago(session.last_seen_at)}</td>
                <td className="mono" style={{ fontSize: 10.5, color: 'var(--ink-mute)' }}>{stamp(session.expires_at)}</td>
                <td className="right">
                  <Button size="sm" variant="ghost" disabled={revoke.isPending} onClick={() => setRevoking(session.id)}>
                    {session.current ? '退出' : '撤销'}
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Failure error={revoke.error} />

      <Confirm
        open={Boolean(revoking)}
        title="撤销这个会话？"
        description="该会话立即失效，需要重新登录。"
        confirmLabel="撤销会话"
        dangerous
        busy={revoke.isPending}
        error={revoke.error}
        onCancel={() => setRevoking(null)}
        onConfirm={() => revoking && revoke.mutate(revoking)}
      />
    </Sheet>
  );
}
