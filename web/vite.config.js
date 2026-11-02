import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: false, assetsInlineLimit: 0, sourcemap: false, rollupOptions: { input: { main: 'index.html', terminal: 'terminal.html' } } },
  server: { proxy: { '/api': 'https://localhost:8443' } },
});
