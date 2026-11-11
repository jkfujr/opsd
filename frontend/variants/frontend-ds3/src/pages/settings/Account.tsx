import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, value } from '../../api/client';
import { queryClient } from '../../state';
import { Button, Failure, Sheet, Skeleton, TextInput } from '../../components/ui';

export function Account() {
  const query = useQuery({
    queryKey: ['settings', 'account'],
    queryFn: async ({ signal }) => value(await api.GET('/settings/account', { signal })),
  });
  const [username, setUsername] = useState('');
  const [entrance, setEntrance] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [current, setCurrent] = useState('');
  const [done, setDone] = useState(false);

  useEffect(() => {
    if (query.data) setUsername(query.data.username);
  }, [query.data]);

  const mutation = useMutation({
    mutationFn: async () =>
      value(
        await api.PATCH('/settings/account', {
          body: {
            current_password: current,
            username: username || undefined,
            password: password || undefined,
            entrance: entrance || undefined,
          },
        }),
      ),
    onSuccess: () => {
      setDone(true);
      setPassword('');
      setConfirm('');
      setCurrent('');
      void queryClient.invalidateQueries({ queryKey: ['session'] });
    },
  });

  const valid = current.length > 0 && (password === confirm) && (username !== '' || password !== '' || entrance !== '');

  return (
    <Sheet title="账号">
      <div className="stack gap-16" style={{ maxWidth: 560 }}>
        {query.isPending ? <Skeleton height={120} /> : null}
        <Failure error={query.error} retry={() => void query.refetch()} />
        <div className="stack gap-4">
          <span className="kicker">当前账号</span>
          <span className="mono" style={{ fontSize: 13 }}>{query.data?.username ?? '—'}</span>
        </div>
        <hr className="rule" />
        <div className="form-grid" style={{ maxWidth: 'none' }}>
          <TextInput label="新账号" value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" />
          <TextInput label="新登录入口" value={entrance} onChange={(event) => setEntrance(event.target.value)} className="input--mono" placeholder="留空不修改" />
          <TextInput label="新密码" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" />
          <TextInput label="确认新密码" type="password" value={confirm} onChange={(event) => setConfirm(event.target.value)} autoComplete="new-password" />
        </div>
        {password && confirm && password !== confirm ? (
          <span style={{ fontSize: 12, color: 'var(--stamp)' }}>两次输入的密码不一致</span>
        ) : null}
        <hr className="rule" />
        <TextInput label="当前密码" type="password" value={current} onChange={(event) => setCurrent(event.target.value)} autoComplete="current-password" />
        <div className="row gap-10">
          <Button variant="solid" busy={mutation.isPending} disabled={!valid} onClick={() => mutation.mutate()}>
            保存账号
          </Button>
          {done ? <span className="mono" style={{ fontSize: 10.5, color: 'var(--seal)' }}>已保存，下次登录生效</span> : null}
        </div>
        <Failure error={mutation.error} />
      </div>
    </Sheet>
  );
}
