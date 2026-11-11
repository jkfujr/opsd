import { useSyncExternalStore } from 'react';

export type PaletteId = 'blue' | 'teal' | 'violet' | 'amber';
export const palettes: { id: PaletteId; name: string }[] = [
  { id: 'blue', name: '雾蓝' }, { id: 'teal', name: '青绿' },
  { id: 'violet', name: '鸢紫' }, { id: 'amber', name: '琥珀' },
];
const paletteKey = 'opsd.frontend-gpt.palette';
const listeners = new Set<() => void>();
function readPalette(): PaletteId {
  try {
    const saved = localStorage.getItem(paletteKey);
    return palettes.find(item => item.id === saved)?.id ?? 'blue';
  } catch { return 'blue'; }
}
let palette = readPalette();
document.documentElement.dataset.palette = palette;
function applyPalette(value: PaletteId) {
  palette = value;
  document.documentElement.dataset.palette = value;
  listeners.forEach(listener => listener());
}
function setPalette(value: PaletteId) {
  try { localStorage.setItem(paletteKey, value); } catch { /* 浏览器禁用存储时仍可切换当前页面。 */ }
  applyPalette(value);
}
function subscribe(listener: () => void) {
  listeners.add(listener);
  const sync = (event: StorageEvent) => { if (event.key === paletteKey || event.key === null) applyPalette(readPalette()); };
  window.addEventListener('storage', sync);
  return () => { listeners.delete(listener); window.removeEventListener('storage', sync); };
}
export function usePalette() {
  return { palette: useSyncExternalStore(subscribe, () => palette), setPalette };
}
