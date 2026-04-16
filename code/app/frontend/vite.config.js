import { defineConfig } from 'vite';

export default defineConfig({
  define: {
    '__GOOGLE_MAPS_API_KEY__':    JSON.stringify(process.env.NAVALPLAN_FRONTEND_MAPS_API_KEY || ''),
    '__GOOGLE_MAPS_MAP_ID__':    JSON.stringify(process.env.NAVALPLAN_MAP_ID || ''),
    '__GOOGLE_MAPS_MAP_ID_MM__': JSON.stringify(process.env.NAVALPLAN_MAP_ID_MM || ''),
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
      '/auth': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
    },
  },
  build: {
    outDir: '../backend/static.min',
    emptyOutDir: true,
    sourcemap: true,
  },
});