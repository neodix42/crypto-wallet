import { defineConfig } from 'vite';

export default defineConfig({
  base: '/',
  build: {
    target: 'es2022',
    sourcemap: false,
    rollupOptions: {
      input: { wallet: 'index.html', private: 'secret/index.html' },
    },
  },
});
