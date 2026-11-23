import { createStore, useStore } from './store';

export type ThemeChoice = 'dark' | 'light' | 'system';
export interface Prefs {
  theme: ThemeChoice;
  hue: number;
  density: 'comfortable' | 'compact';
  pinned: boolean;
  terminalSize: number;
}
const KEY = 'opsd.t1.prefs';
const defaults: Prefs = { theme: 'system', hue: 166, density: 'comfortable', pinned: false, terminalSize: 14 };

function read(): Prefs {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? '{}');
    return {
      theme: v.theme === 'dark' || v.theme === 'light' ? v.theme : 'system',
      hue: Number.isFinite(v.hue) ? Math.max(0, Math.min(360, v.hue)) : defaults.hue,
      density: v.density === 'compact' ? 'compact' : 'comfortable',
      pinned: v.pinned === true,
      terminalSize: Number.isInteger(v.terminalSize) ? Math.max(11, Math.min(22, v.terminalSize)) : defaults.terminalSize,
    };
  } catch {
    return defaults;
  }
}

export const prefs = createStore<Prefs>(read());
const media = window.matchMedia('(prefers-color-scheme: light)');

export function resolvedTheme(p = prefs.get()): 'dark' | 'light' {
  return p.theme === 'system' ? (media.matches ? 'light' : 'dark') : p.theme;
}

let viewTransitioning = false;

function apply(p: Prefs) {
  const root = document.documentElement;
  const next = resolvedTheme(p);
  if (root.dataset.theme !== next) {
    if (viewTransitioning) root.dataset.theme = next;
    else {
      root.classList.add('theme-switching');
      root.dataset.theme = next;
      window.setTimeout(() => root.classList.remove('theme-switching'), 450);
    }
  }
  root.style.setProperty('--hue', String(p.hue));
  if (p.density === 'compact') root.dataset.density = 'compact';
  else delete root.dataset.density;
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', next === 'dark' ? '#0b0c0f' : '#f8f9fb');
}

prefs.subscribe(() => {
  const p = prefs.get();
  localStorage.setItem(KEY, JSON.stringify(p));
  apply(p);
});
media.addEventListener('change', () => apply(prefs.get()));
apply(prefs.get());

export const usePrefs = () => useStore(prefs);
export const setPrefs = (patch: Partial<Prefs>) => prefs.set((p) => ({ ...p, ...patch }));

/** 带圆形扩散的主题切换（支持 View Transitions 时）。 */
export function toggleTheme(origin?: { x: number; y: number }) {
  const next = resolvedTheme() === 'dark' ? 'light' : 'dark';
  const doc = document as Document & { startViewTransition?: (cb: () => void) => { ready: Promise<void>; finished: Promise<void> } };
  const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  if (!doc.startViewTransition || reduce || !origin) {
    setPrefs({ theme: next });
    return;
  }
  const radius = Math.hypot(Math.max(origin.x, innerWidth - origin.x), Math.max(origin.y, innerHeight - origin.y));
  viewTransitioning = true;
  const transition = doc.startViewTransition(() => setPrefs({ theme: next }));
  transition.finished.finally(() => { viewTransitioning = false; }).catch(() => {});
  transition.ready.then(() => {
    document.documentElement.animate(
      { clipPath: [`circle(0px at ${origin.x}px ${origin.y}px)`, `circle(${radius}px at ${origin.x}px ${origin.y}px)`] },
      { duration: 560, easing: 'cubic-bezier(0.22, 1, 0.36, 1)', pseudoElement: '::view-transition-new(root)' },
    );
  }).catch(() => {});
}
