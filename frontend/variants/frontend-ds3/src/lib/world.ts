import { useSyncExternalStore } from 'react';

export type WorldId = 'board' | 'lab' | 'boarding' | 'timetable' | 'neutral';
export type ThemeId = 'light' | 'dark';

export const worlds: { id: WorldId; label: string; hint: string }[] = [
  { id: 'board', label: '行情板', hint: '夜盘 · 排位变动即通知' },
  { id: 'lab', label: '化验单', hint: '白板 · 实测值对参考区间' },
  { id: 'boarding', label: '登机牌', hint: '登机口大屏 · 签收倒计时' },
  { id: 'timetable', label: '时刻表', hint: '单字号栅格 · 现在线' },
  { id: 'neutral', label: '中性', hint: '目录标准 · 无签名动作' },
];

const defaultTheme: Record<WorldId, ThemeId> = {
  board: 'dark',
  lab: 'light',
  boarding: 'light',
  timetable: 'light',
  neutral: 'light',
};

function readWorld(): WorldId {
  const stored = localStorage.getItem('opsd3.world') as WorldId | null;
  return stored && worlds.some((item) => item.id === stored) ? stored : 'board';
}

function readTheme(): ThemeId {
  const stored = localStorage.getItem('opsd3.theme') as ThemeId | null;
  return stored === 'light' || stored === 'dark' ? stored : defaultTheme[readWorld()];
}

const listeners = new Set<() => void>();
function emit() {
  listeners.forEach((listener) => listener());
}

export function currentWorld(): WorldId {
  return readWorld();
}

export function currentTheme(): ThemeId {
  return readTheme();
}

export function apply() {
  document.documentElement.dataset.world = readWorld();
  document.documentElement.dataset.theme = readTheme();
}

export function setWorld(id: WorldId) {
  localStorage.setItem('opsd3.world', id);
  if (!localStorage.getItem('opsd3.theme')) localStorage.setItem('opsd3.theme', defaultTheme[id]);
  apply();
  emit();
}

export function setTheme(theme: ThemeId) {
  localStorage.setItem('opsd3.theme', theme);
  apply();
  emit();
}

export function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useWorld(): WorldId {
  return useSyncExternalStore(subscribe, readWorld);
}

export function useTheme(): ThemeId {
  return useSyncExternalStore(subscribe, readTheme);
}
