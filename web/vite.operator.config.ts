import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The operator console is its own build (operator.html, src/operator), never an input of the workspace app's:
// two inputs in one build would share chunks and grow the app's first bundle (docs/internals/operator-console.md).
// It is served under /operator/ on the app's host, or at the root of OPERATOR_ORIGIN with its assets still under
// /operator/, so one build serves both.
export default defineConfig({
  plugins: [react()],
  base: '/operator/',
  publicDir: 'public-operator',
  build: {
    outDir: '../server/internal/webui/static/build/operator',
    emptyOutDir: true,
    rollupOptions: { input: 'operator.html' },
  },
});
