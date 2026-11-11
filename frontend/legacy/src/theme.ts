import type { ThemeScriptOptions } from '@appica/ui-react/providers/theme-provider';

export const themeOptions = {
  storageKey: 'opsd.theme',
  defaultTheme: 'system',
  enableSystem: true,
  enableColorScheme: true,
} satisfies ThemeScriptOptions;
