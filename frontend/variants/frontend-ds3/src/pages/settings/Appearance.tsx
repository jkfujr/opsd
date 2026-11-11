import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, value } from '../../api/client';
import { queryClient } from '../../state';
import { navById, navigation, mergeOrder, visibleOrder, type NavId } from '../../lib/nav';
import { setTheme, setWorld, useTheme, useWorld, worlds } from '../../lib/world';
import { Icon } from '../../components/Icon';
import { Button, Failure, Sheet, Skeleton, useSession } from '../../components/ui';

export function Appearance() {
  const session = useSession();
  const world = useWorld();
  const theme = useTheme();
  const query = useQuery({
    queryKey: ['navigation', session.local_node_id],
    queryFn: async ({ signal }) => value(await api.GET('/settings/navigation', { signal })),
  });
  const [order, setOrder] = useState<NavId[]>(navigation.map((item) => item.id));
  useEffect(() => {
    setOrder(visibleOrder(query.data?.order));
  }, [query.data]);

  const save = useMutation({
    mutationFn: async () =>
      value(
        await api.PATCH('/settings/navigation', {
          body: { order: mergeOrder(query.data?.order, order) as never, revision: query.data?.revision ?? 0 },
        }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['navigation'] }),
  });

  const move = (index: number, delta: number) => {
    const next = [...order];
    const target = index + delta;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    setOrder(next);
  };

  const dirty = JSON.stringify(order) !== JSON.stringify(visibleOrder(query.data?.order));

  return (
    <div className="stack gap-16">
      <Sheet title="视觉世界">
        <div className="stack gap-12">
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(200px,1fr))', gap: 10 }}>
            {worlds.map((item) => (
              <button
                key={item.id}
                type="button"
                aria-pressed={world === item.id}
                className="world-card"
                style={{
                  '--card-accent': item.id === 'board' ? '#e2a93e' : item.id === 'lab' ? '#1e7a52' : item.id === 'boarding' ? '#c24433' : item.id === 'timetable' ? '#8f6116' : '#2e6390',
                  borderColor: world === item.id ? 'var(--ink)' : undefined,
                } as React.CSSProperties}
                onClick={() => setWorld(item.id)}
              >
                <span className="mono" style={{ fontSize: 9.5, letterSpacing: '.14em', color: 'var(--ink-mute)' }}>WORLD</span>
                <b style={{ fontSize: 13.5 }}>{item.label}</b>
                <span style={{ fontSize: 11, color: 'var(--ink-soft)' }}>{item.hint}</span>
                <i aria-hidden="true" />
              </button>
            ))}
          </div>
          <div className="spread">
            <span style={{ fontSize: 12, color: 'var(--ink-soft)' }}>
              当前：{worlds.find((item) => item.id === world)?.label}
            </span>
            <div className="segmented">
              {(['light', 'dark'] as const).map((item) => (
                <button key={item} type="button" aria-pressed={theme === item} onClick={() => setTheme(item)}>
                  {item === 'light' ? '白板' : '夜盘'}
                </button>
              ))}
            </div>
          </div>
        </div>
      </Sheet>

      <Sheet title="菜单顺序">
        <div className="stack gap-12">
          {query.isPending ? <Skeleton height={160} /> : null}
          <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
          <div className="stack gap-6" style={{ maxWidth: 420 }}>
            {order.map((id, index) => (
              <div key={id} className="order-row">
                <span className="idx">{String(index + 1).padStart(2, '0')}</span>
                <Icon name={navById.get(id)?.icon ?? 'overview'} width={14} height={14} />
                <span className="grow">{navById.get(id)?.label ?? id}</span>
                <Button size="sm" variant="ghost" icon="up" aria-label="上移" disabled={index === 0} onClick={() => move(index, -1)} />
                <Button size="sm" variant="ghost" icon="downarrow" aria-label="下移" disabled={index === order.length - 1} onClick={() => move(index, 1)} />
              </div>
            ))}
          </div>
          <div className="row gap-8">
            <Button size="sm" variant="solid" busy={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
              保存顺序
            </Button>
            <Button size="sm" onClick={() => setOrder(navigation.map((item) => item.id))}>恢复默认</Button>
            {dirty ? <span className="mono" style={{ fontSize: 9.5, color: 'var(--warn)' }}>未保存</span> : null}
          </div>
          <Failure error={save.error} />
        </div>
      </Sheet>
    </div>
  );
}
