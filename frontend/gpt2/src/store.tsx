import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
type Preferences = { terminalFont: 'jetbrains' | 'sarasa'; terminalSize: number };
const preferenceKey = 'opsd.frontend-gpt2.preferences';
function readPreferences(): Preferences {
  try { const v = JSON.parse(localStorage.getItem(preferenceKey) ?? '{}'); return { terminalFont: v.terminalFont === 'sarasa' ? 'sarasa' : 'jetbrains', terminalSize: Number.isInteger(v.terminalSize) ? Math.max(12, Math.min(24, v.terminalSize)) : 16 }; }
  catch { return { terminalFont: 'jetbrains', terminalSize: 16 }; }
}
function useModel() {
  const [preferences, setPreferences] = useState(readPreferences), [toast, setToast] = useState('');
  useEffect(() => { localStorage.setItem(preferenceKey, JSON.stringify(preferences)); }, [preferences]);
  useEffect(() => { if (!toast) return; const timer = setTimeout(() => setToast(''), 6000); return () => clearTimeout(timer); }, [toast]);
  return { preferences, setPreferences, toast, setToast };
}
const Context = createContext<ReturnType<typeof useModel> | null>(null);
export function UIProvider({ children }: { children: ReactNode }) { return <Context value={useModel()}>{children}</Context>; }
export function useUI() { const value = useContext(Context); if (!value) throw new Error('界面尚未初始化'); return value; }
