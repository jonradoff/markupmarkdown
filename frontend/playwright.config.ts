import { defineConfig } from '@playwright/test';

// E2E tests for markupmarkdown. The Playwright config assumes:
//   - the backend dev server is running on :4721 with MARKUPMARKDOWN_ENV=test
//     and DATABASE_NAME=markupmarkdown-test (the testutil-level safety guard
//     also enforces this at the package level)
//   - the frontend dev server is reachable on :4720 (vite default for this
//     project)
//
// In CI we spin up both via the workflow before running playwright test.

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  timeout: 30_000,

  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:4720',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],

  webServer: process.env.CI
    ? undefined
    : {
        // Local: assume vite dev server is already running (or start it).
        command: 'npm run dev',
        url: 'http://localhost:4720',
        reuseExistingServer: true,
        timeout: 30_000,
      },
});
