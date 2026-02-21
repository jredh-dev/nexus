// @ts-check
import { defineConfig } from 'astro/config';

import preact from '@astrojs/preact';
import node from '@astrojs/node';

// https://astro.build/config
export default defineConfig({
  output: 'server',

  security: {
    checkOrigin: false,
  },

  integrations: [preact()],

  adapter: node({
    mode: 'standalone'
  }),

  vite: {
    server: {
      proxy: {
        // Proxy Connect RPC calls to the Go backend during dev
        '/portal.v1.': {
          target: 'http://localhost:8080',
          changeOrigin: true,
        },
        // Proxy legacy API calls during migration
        '/api/': {
          target: 'http://localhost:8080',
          changeOrigin: true,
        },
      },
    },
  },
});
