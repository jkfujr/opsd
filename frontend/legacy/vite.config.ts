import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { getThemeScript } from '@appica/ui-react/providers/theme-provider';
import { themeOptions } from './src/theme';

export default defineConfig({
  plugins: [react(), tailwindcss(), {
    name: 'opsd-theme',
    transformIndexHtml() {
      return [{ tag: 'script', children: getThemeScript(themeOptions), injectTo: 'head-prepend' }];
    },
  }],
  server: { host: '127.0.0.1' },
});
