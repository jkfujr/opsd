import { useState, type FormEvent } from 'react';
import { motion } from 'motion/react';
import { ArrowRight, Eye, EyeOff, LockKeyhole, UserRound } from 'lucide-react';
import { ApiError, call, errorText, setCsrf } from '../api/http';
import type { CurrentSession } from '../api/types';
import { queryClient } from '../app/query';
import { LOGIN_KEY, loginDestination } from '../app/session';
import { useNow } from '../lib/hooks';
import { Logo } from '../ui/logo';
import { Button, Notice } from '../ui/kit';

const consolePaths = /^\/(nodes|containers|databases|storage|monitoring|firewall|tools|terminal|settings|__opsd)(\/|$)/;

export function Login({ entrance }: { entrance: string }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [shake, setShake] = useState(0);
  const now = useNow(500);
  const isConsolePath = entrance === '/' || consolePaths.test(entrance);
  const known = localStorage.getItem(LOGIN_KEY);
  const retryIn = error instanceof ApiError && error.retryAt ? Math.max(0, Math.ceil((error.retryAt - now) / 1000)) : 0;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || retryIn) return;
    setBusy(true);
    setError(null);
    try {
      const r = await call<CurrentSession>('POST', '/auth/sessions', { body: { entrance, username, password }, quiet401: true });
      setCsrf(r.data.csrf_token);
      try { localStorage.setItem(LOGIN_KEY, r.data.login_address); } catch { /* 忽略 */ }
      queryClient.setQueryData(['session'], r.data);
      // 整页跳转：由服务端决定返回主题还是内置界面。
      window.location.replace(loginDestination());
    } catch (reason) {
      setError(reason);
      setShake((n) => n + 1);
      setBusy(false);
    }
  };

  return (
    <div className="login">
      <div className="login-bg" aria-hidden>
        <div className="grid" />
        <motion.div className="orb" style={{ width: 520, height: 520, background: 'var(--accent)', left: '8%', top: '-10%' }} animate={{ x: [0, 60, -20, 0], y: [0, 40, 80, 0] }} transition={{ duration: 22, repeat: Infinity, ease: 'easeInOut' }} />
        <motion.div className="orb" style={{ width: 460, height: 460, background: 'var(--accent-2)', right: '4%', bottom: '-14%', opacity: 0.35 }} animate={{ x: [0, -50, 30, 0], y: [0, -60, -10, 0] }} transition={{ duration: 26, repeat: Infinity, ease: 'easeInOut' }} />
      </div>
      <motion.div
        className="login-card"
        initial={{ opacity: 0, y: 24, scale: 0.97, x: 0 }}
        animate={{ opacity: 1, y: 0, scale: 1, x: shake ? (shake % 2 ? [0, -10, 9, -6, 4, 0] : [0, 10, -9, 6, -4, 0]) : 0 }}
        transition={{ default: { type: 'spring', stiffness: 260, damping: 26, delay: 0.05 }, x: { duration: 0.42 } }}
      >
        <header>
          <motion.div initial={{ rotate: -30, scale: 0.6, opacity: 0 }} animate={{ rotate: 0, scale: 1, opacity: 1 }} transition={{ type: 'spring', stiffness: 200, damping: 14, delay: 0.15 }}>
            <Logo size={52} />
          </motion.div>
          <div className="col" style={{ gap: 6, alignItems: 'center' }}>
            <h1>登录</h1>
            <p>对等节点控制台</p>
          </div>
        </header>
        {isConsolePath && (
          <Notice tone="info" title="需要从安全入口登录">
            {known ? <>上次的登录入口为 <a className="mono strong" href={known}>{known}</a></> : '请访问安装时提供的登录入口地址。'}
          </Notice>
        )}
        <form onSubmit={submit}>
          <label className="input-wrap">
            <UserRound />
            <input className="input" name="username" aria-label="账号" autoComplete="username" placeholder="账号" value={username} onChange={(e) => setUsername(e.target.value)} required maxLength={128} autoFocus spellCheck={false} />
          </label>
          <label className="input-wrap">
            <LockKeyhole />
            <input className="input" name="password" aria-label="密码" type={show ? 'text' : 'password'} autoComplete="current-password" placeholder="密码" value={password} onChange={(e) => setPassword(e.target.value)} required maxLength={1024} />
            <span className="input-suffix">
              <button type="button" className="icon-btn sm" aria-label={show ? '隐藏密码' : '显示密码'} onClick={() => setShow((v) => !v)}>{show ? <EyeOff /> : <Eye />}</button>
            </span>
          </label>
          {!!error && (
            <motion.div initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: 'auto' }} style={{ overflow: 'hidden' }}>
              <div className="field-error" style={{ fontSize: 13 }}>{retryIn ? `尝试次数过多，请 ${retryIn} 秒后重试` : errorText(error)}</div>
            </motion.div>
          )}
          <Button type="submit" variant="primary" size="lg" block busy={busy} disabled={!!retryIn}>
            {busy ? '正在登录' : '登录'}{!busy && <ArrowRight />}
          </Button>
        </form>
        <div className="login-foot">会话仅保存在当前浏览器 · 登录失败过多会暂时限制</div>
      </motion.div>
    </div>
  );
}
