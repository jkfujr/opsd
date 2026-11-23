import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';

// 主题包使用相对资源基址；OPSD 会在入口注入 <base href="/__opsd/themes/{id}/">。
// 作为内置界面嵌入时可设置 OPSD_BASE=/。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', 'OPSD_');
  const target = env.OPSD_API_URL || 'http://127.0.0.1:5183';
  return {
    base: env.OPSD_BASE || './',
    plugins: [react()],
    server: {
      proxy: {
        '/api': { target, ws: true, changeOrigin: false },
        '/__opsd': { target },
      },
    },
    build: {
      outDir: 'dist',
      assetsDir: 'assets',
      target: 'es2022',
      cssCodeSplit: true,
      sourcemap: false,
      chunkSizeWarningLimit: 900,
      rollupOptions: {
        output: {
          manualChunks(id) {
            if (id.includes('@xterm')) return 'xterm';
            if (id.includes('motion') || id.includes('framer')) return 'motion';
            if (id.includes('node_modules/react') || id.includes('react-router') || id.includes('scheduler')) return 'react';
            if (id.includes('@tanstack')) return 'query';
          },
        },
      },
    },
  };
});
