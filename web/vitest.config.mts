import { fileURLToPath } from 'node:url';

import { defineConfig } from 'vitest/config';

// The stream client and the API module are plain TypeScript, deliberately, so
// they need no DOM: their tests inject a fetch and a WebSocket. Components are
// covered by checks in a real browser rather than by unit tests here.
export default defineConfig({
  resolve: {
    alias: { '@': fileURLToPath(new URL('.', import.meta.url)) },
  },
  test: {
    environment: 'node',
    include: ['lib/**/*.test.ts'],
  },
});
