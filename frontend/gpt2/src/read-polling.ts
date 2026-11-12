import { useEffect, useRef, useState } from 'react';

export function usePageVisible() {
  const [visible, setVisible] = useState(document.visibilityState !== 'hidden');
  useEffect(() => {
    const update = () => setVisible(document.visibilityState !== 'hidden');
    document.addEventListener('visibilitychange', update);
    return () => document.removeEventListener('visibilitychange', update);
  }, []);
  return visible;
}

export function beginReadRefresh() { window.dispatchEvent(new Event('opsd:read-refresh')); }

export function useReadPolling(scope: string, interval = 10000) {
  const visible = usePageVisible();
  const window = useRef({ scope, until: Date.now() + 5000 });
  if (window.current.scope !== scope) window.current = { scope, until: Date.now() + 5000 };
  const begin = () => { window.current.until = Date.now() + 5000; };
  useEffect(() => {
    globalThis.window.addEventListener('opsd:read-refresh', begin);
    return () => globalThis.window.removeEventListener('opsd:read-refresh', begin);
  }, []);
  return { begin, interval: (waiting: boolean, error: boolean) => !visible ? false : waiting && !error && Date.now() < window.current.until ? 500 : interval };
}
