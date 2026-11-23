import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

/** 数字缓动：数值变化时平滑过渡（减少动态时直接跳变）。 */
export function useTween(value: number | undefined, duration = 650) {
  const [shown, setShown] = useState(value ?? 0);
  const from = useRef(value ?? 0);
  const frame = useRef(0);
  useEffect(() => {
    if (value === undefined || !Number.isFinite(value)) return;
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const start = performance.now();
    const origin = from.current;
    cancelAnimationFrame(frame.current);
    if (reduce || origin === value) { from.current = value; setShown(value); return; }
    const tick = (now: number) => {
      const t = Math.min(1, (now - start) / duration);
      const eased = 1 - Math.pow(1 - t, 4);
      const v = origin + (value - origin) * eased;
      from.current = v;
      setShown(v);
      if (t < 1) frame.current = requestAnimationFrame(tick);
    };
    frame.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame.current);
  }, [value, duration]);
  return shown;
}

export function useNow(interval = 1000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), interval);
    return () => window.clearInterval(id);
  }, [interval]);
  return now;
}

export function useVisible() {
  const [visible, setVisible] = useState(document.visibilityState !== 'hidden');
  useEffect(() => {
    const update = () => setVisible(document.visibilityState !== 'hidden');
    document.addEventListener('visibilitychange', update);
    return () => document.removeEventListener('visibilitychange', update);
  }, []);
  return visible;
}

export function useHotkey(combo: string, handler: (event: KeyboardEvent) => void, enabled = true) {
  const saved = useRef(handler);
  saved.current = handler;
  useEffect(() => {
    if (!enabled) return;
    const parts = combo.toLowerCase().split('+');
    const key = parts.pop()!;
    const mod = parts.includes('mod');
    const shift = parts.includes('shift');
    const listener = (event: KeyboardEvent) => {
      const k = event.key.toLowerCase();
      if (k !== key) return;
      if (mod !== (event.metaKey || event.ctrlKey)) return;
      if (shift !== event.shiftKey) return;
      if (!mod && isTyping(event.target)) return;
      event.preventDefault();
      saved.current(event);
    };
    window.addEventListener('keydown', listener);
    return () => window.removeEventListener('keydown', listener);
  }, [combo, enabled]);
}

export function isTyping(target: EventTarget | null) {
  const el = target as HTMLElement | null;
  if (!el) return false;
  return el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || !!el.closest('.xterm');
}

export function useSize<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => {
      const r = entry!.contentRect;
      setSize((s) => (s.width === r.width && s.height === r.height ? s : { width: r.width, height: r.height }));
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  return [ref, size] as const;
}

export function useLocalState<T>(key: string, initial: T) {
  const [value, setValue] = useState<T>(() => {
    try { const raw = localStorage.getItem(key); return raw === null ? initial : (JSON.parse(raw) as T); } catch { return initial; }
  });
  const update = useCallback((next: T) => { setValue(next); try { localStorage.setItem(key, JSON.stringify(next)); } catch { /* 忽略 */ } }, [key]);
  return [value, update] as const;
}

/** 保留最近 N 个采样，用于实时火花线。 */
export function useRolling(key: string, value: number | undefined, size = 40) {
  const buffers = useRef(new Map<string, (number | null)[]>());
  const last = useRef(new Map<string, number | undefined>());
  if (last.current.get(key) !== value || !buffers.current.has(key)) {
    const buf = buffers.current.get(key) ?? Array.from({ length: size }, () => null as number | null);
    if (last.current.has(key)) {
      buf.push(value ?? null);
      while (buf.length > size) buf.shift();
    } else if (value !== undefined) {
      buf[buf.length - 1] = value;
    }
    buffers.current.set(key, buf);
    last.current.set(key, value);
  }
  return buffers.current.get(key)!;
}
