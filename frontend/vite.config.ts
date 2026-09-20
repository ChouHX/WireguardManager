import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';

// 开发环境通过 Vite 代理访问后端；生产环境由 Nginx 反代 /api
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const proxyTarget = env.VITE_API_PROXY_TARGET || env.API_PROXY_TARGET || 'http://localhost:8080';

  return {
    plugins: [react()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: {
      host: true,
      port: 3000,
      proxy: {
        '/api': { target: proxyTarget, changeOrigin: true },
        '/health': { target: proxyTarget, changeOrigin: true },
      },
    },
    build: {
      outDir: 'dist',
      sourcemap: false,
      chunkSizeWarningLimit: 1200,
      rollupOptions: {
        output: {
          manualChunks(id: string) {
            if (!id.includes('node_modules')) return undefined;
            if (id.includes('/recharts/') || id.includes('/d3-') || id.includes('/victory-'))
              return 'charts';
            if (id.includes('/@mantine/')) return 'mantine';
            if (id.includes('/react-dom/') || id.includes('/react-router') || id.includes('/react/'))
              return 'react';
            return undefined;
          },
        },
      },
    },
  };
});
