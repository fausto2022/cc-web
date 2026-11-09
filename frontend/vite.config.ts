import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', 'VITE_');
  const candidate = env.VITE_BASE_PATH?.trim() || '/';
  const normalizedBase = candidate === '/' ? '/' : `/${candidate.replace(/^\/+|\/+$/g, '')}/`;
  const base = /^\/(?:[A-Za-z0-9._~-]+\/)*$/.test(normalizedBase) ? normalizedBase : '/';
  const apiPrefix = `${base}api`;

  return {
    base,
    plugins: [react()],
    server: {
      strictPort: true,
      proxy: {
        [apiPrefix]: {
          target: 'http://127.0.0.1:8089',
          rewrite: (path) => path.replace(apiPrefix, '/api'),
        },
      },
    },
  };
});
