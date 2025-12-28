import { defineConfig } from 'vite';

export default defineConfig({
  define: {
    '__MAPBOX_TOKEN__': JSON.stringify(process.env.NAVALPLAN_MB_TOKEN || ''),
    '__MAPBOX_STYLE__': JSON.stringify(process.env.NAVALPLAN_MB_STYLE || 'mapbox://styles/mapbox/outdoors-v12'),
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