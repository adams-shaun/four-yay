/// <reference types="vitest/config" />
import { resolve } from 'node:path';
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  build: { outDir: '../cmd/gorged/webdist', emptyOutDir: true },
  // Worktrees share node_modules; callers may point Vite's mutable cache at
  // their own scratch directory rather than writing through that symlink.
  cacheDir: process.env.VITE_CACHE_DIR,
  server: { proxy: { '/api': { target: 'http://localhost:8080', changeOrigin: true } } },
  test: {
    // Persist Vitest's transformed test-module graph across invocations. Keep it
    // in the caller's isolated Vite cache when VITE_CACHE_DIR is set; otherwise
    // fall back to Vitest's documented node_modules/.vitest-cache, which already
    // tracks Vite's own node_modules/.vite default and is gitignored, so a plain
    // `npm test` never dirties the worktree. Never a shared mutable cache.
    fsModuleCache: true,
    fsModuleCachePath: process.env.VITE_CACHE_DIR
      ? resolve(process.env.VITE_CACHE_DIR, 'vitest-fs-cache')
      : resolve('node_modules', '.vitest-cache'),
    environment: 'node',
    include: ['src/**/*.test.ts'],
    globalSetup: './src/test/browser.global.ts',
  },
});
