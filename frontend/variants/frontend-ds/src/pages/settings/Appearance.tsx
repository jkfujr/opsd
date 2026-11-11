import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, value } from '../../api/client';
import { queryClient } from '../../state';
import { navById, navigation, mergeOrder, visibleOrder, type NavId } from '../../lib/nav';
import { Icon } from '../../components/Icon';
import { Button, Failure, Sheet, Skeleton, useSession } from '../../components/ui';

type Theme = 'light' | 'dark' | 'system';

export function Appearance() {
  const session = useSession();
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

  const [theme, setTheme] = useState<Theme>(() => (localStorage.getItem('opsd.theme') as Theme | null) ?? 'system');
  const applyTheme = (next: Theme) => {
    setTheme(next);
    if (next === 'system') localStorage.removeItem('opsd.theme');
    else localStorage.setItem('opsd.theme', next);
    const resolved = next === 'system' ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light') : next;
    document.documentElement.dataset.theme = resolved;
  };

  const move = (index: number, delta: number) => {
    const next = [...order];
    const target = index + delta;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    setOrder(next);
  };

  const dirty = JSON.stringify(order) !== JSON.stringify(visibleOrder(query.data?.order));

  return (
    <Sheet title="外观">
      <div className="stack gap-20">
        <div className="setting-block">
          <h2>菜单顺序</h2>
          <p className="hint">侧栏与移动端抽屉共用这份顺序。</p>
          {query.isPending ? <Skeleton height={180} /> : null}
          <Failure error={query.error} retry={() => void query.refetch()} busy={query.isFetching} />
          <div className="order-list">
            {order.map((id, index) => (
              <div key={id} className="order-row">
                <span className="idx">{String(index + 1).padStart(2, '0')}</span>
                <Icon name={navById.get(id)?.icon ?? 'overview'} width={14} height={14} />
                <span className="grow">{navById.get(id)?.label ?? id}</span>
                <span className="mono" style={{ fontSize: 9.5, color: 'var(--ink-mute)' }}>{navById.get(id)?.hint}</span>
                <Button size="sm" variant="ghost" icon="up" aria-label="上移" disabled={index === 0} onClick={() => move(index, -1)} />
                <Button size="sm" variant="ghost" icon="downarrow" aria-label="下移" disabled={index === order.length - 1} onClick={() => move(index, 1)} />
              </div>
            ))}
          </div>
          <div className="row gap-8" style={{ marginTop: 12 }}>
            <Button size="sm" variant="solid" busy={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
              保存顺序
            </Button>
            <Button size="sm" onClick={() => setOrder(navigation.map((item) => item.id))}>恢复默认</Button>
            {dirty ? <span className="mono" style={{ fontSize: 10, color: 'var(--amber)' }}>未保存</span> : null}
          </div>
          <Failure error={save.error} />
        </div>

        <div className="setting-block">
          <h2>主题</h2>
          <div className="segmented">
            {(['light', 'dark', 'system'] as const).map((item) => (
              <button key={item} type="button" aria-pressed={theme === item} onClick={() => applyTheme(item)}>
                {item === 'light' ? '浅色' : item === 'dark' ? '深色' : '跟随系统'}
              </button>
            ))}
          </div>
        </div>
      </div>
    </Sheet>
  );
}
