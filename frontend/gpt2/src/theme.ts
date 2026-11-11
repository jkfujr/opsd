export const themeOptions = {
  storageKey: 'opsd.frontend-gpt2.theme',
  defaultTheme: 'dark',
  enableSystem: true,
  enableColorScheme: true,
};

export type TerminalFont = 'jetbrains' | 'sarasa';
export const terminalFonts: Record<TerminalFont, { label: string; family: string }> = {
  jetbrains: { label: 'JetBrains Mono', family: '"JetBrains Mono", "Sarasa Fixed SC", monospace' },
  sarasa: { label: '更纱等宽黑体', family: '"Sarasa Fixed SC", monospace' },
};
