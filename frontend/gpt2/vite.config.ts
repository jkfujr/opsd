import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { getThemeScript } from '@appica/ui-react/providers/theme-provider';
import { themeOptions } from './src/theme.ts';

export default defineConfig(({ mode }) => {
  const environment = loadEnv(mode, '.', 'OPSD_GPT2_');
  return {
  base: './',
  publicDir: false,
  server: { proxy: environment.OPSD_GPT2_API_URL ? { '/api': { target: environment.OPSD_GPT2_API_URL, ws: true }, '/__opsd': { target: environment.OPSD_GPT2_API_URL }, '/assets': { target: environment.OPSD_GPT2_API_URL } } : undefined },
  plugins: [react(), tailwindcss(), {
    name: 'opsd-gpt2-theme',
    transformIndexHtml: () => [{ tag: 'script', children: getThemeScript(themeOptions), injectTo: 'head-prepend' }],
  }],
  };
});
