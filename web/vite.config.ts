import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { outDir: '../server/internal/webui/static/build', emptyOutDir: true },
  // The knowledge base preview reads the help center's HTML allowlist (server/internal/kbhtml/allowlist.json).
  server: { proxy: { '/api': 'http://localhost:8080' }, fs: { allow: ['.', '../server/internal/kbhtml'] } },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
    exclude: ['e2e/**', '**/node_modules/**', '**/dist/**'],
  },
});
