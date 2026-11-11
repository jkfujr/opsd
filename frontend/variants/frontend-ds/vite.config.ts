import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import { mockApi } from './src/dev/mock-api';

// 开发时可通过 OPSD_API 指向真实节点（例如 http://127.0.0.1:65533）。
// 未设置时由内置演示接口回答 /api/v1，页面结构与真实数据一致。
const target = process.env.OPSD_API;

function demoApi(): Plugin {
  return {
    name: 'opsd-demo-api',
    apply: 'serve',
    configureServer(server) {
      server.middlewares.use('/api/v1', (request, response, next) => {
        const url = new URL(request.url ?? '/', 'http://localhost');
        const method = (request.method ?? 'GET').toUpperCase();
        let raw = '';
        request.on('data', (chunk) => {
          raw += String(chunk);
        });
        request.on('end', () => {
          const result = mockApi(method, url.pathname, url.searchParams, raw);
          if (!result) {
            next();
            return;
          }
          if (result.status === 204) {
            response.statusCode = 204;
            response.end();
            return;
          }
          response.statusCode = result.status;
          response.setHeader('Content-Type', 'application/json; charset=utf-8');
          response.setHeader('Cache-Control', 'no-store');
          response.end(typeof result.body === 'string' ? result.body : JSON.stringify(result.body));
        });
      });
    },
  };
}

export default defineConfig({
  plugins: [react(), demoApi()],
  base: './',
  server: {
    port: 5290,
    proxy: target ? { '/api/v1': { target, changeOrigin: false } } : undefined,
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 700,
  },
});
