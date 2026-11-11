import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const themeInitScript = `
(function() {
  try {
    var stored = localStorage.getItem('opsd.theme') || 'system';
    var dark = stored === 'dark' || (stored === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
    document.documentElement.classList.toggle('dark', dark);
    document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
  } catch (e) {}
})();
`.trim();

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: 'opsd-theme',
      transformIndexHtml() {
        return [{ tag: 'script', children: themeInitScript, injectTo: 'head-prepend' }];
      },
    },
  ],
  server: { host: '127.0.0.1' },
});
