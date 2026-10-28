import { defineConfig } from 'vitest/config';

export default defineConfig({
  base: '/',
  build: {
    target: 'es2022',
    sourcemap: false,
    rollupOptions: {
      input: { wallet: 'index.html', private: 'secret/index.html' },
    },
  },
  test: {
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.test.{ts,tsx}', 'src/test-fixtures.ts'],
      reporter: ['json-summary', 'json', 'text'],
      reportsDirectory: '../../../reports/wails-frontend',
      thresholds: {
        perFile: true,
        statements: 90,
        branches: 85,
        'src/PrivateApp.tsx': { statements: 95, branches: 90 },
        'src/private-main.tsx': { statements: 95, branches: 90 },
        'src/protocol.ts': { statements: 95, branches: 90 },
      },
    },
  },
});
