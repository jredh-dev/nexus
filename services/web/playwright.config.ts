import { defineConfig } from '@playwright/test';

// Run against deployed portal.jredh.com by default, or a local URL via BASE_URL.
const baseURL = process.env.BASE_URL || 'https://portal.jredh.com';

export default defineConfig({
  testDir: 'tests',
  testMatch: '**/*.e2e.ts',
  timeout: 30_000,
  retries: 0,
  use: {
    baseURL,
    // Don't follow redirects automatically — we want to assert on them.
    headless: true,
  },
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],
});
