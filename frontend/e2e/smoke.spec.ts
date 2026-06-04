import { test, expect } from '@playwright/test';

// Smoke tests verify the frontend boots and the basic anonymous flows
// work without needing GitHub OAuth or an Anthropic key.

test('home page renders', async ({ page }) => {
  await page.goto('/');
  // The hero copy is the most stable anchor for a smoke test.
  await expect(page.getByRole('heading', { name: /Comment on any markdown file/i })).toBeVisible();
});

test('SKILL.md is served raw', async ({ page }) => {
  const res = await page.request.get('/SKILL.md');
  expect(res.ok()).toBeTruthy();
  // Should be served as plain markdown, not HTML.
  expect(res.headers()['content-type']).toContain('text/markdown');
  const body = await res.text();
  expect(body).toContain('# markupmarkdown');
});

test('API health endpoint returns ok', async ({ page }) => {
  const res = await page.request.get('/api/health');
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(body.status).toBe('ok');
});

test('auth/me returns null for anonymous viewers', async ({ page }) => {
  const res = await page.request.get('/api/auth/me');
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(body.user).toBeNull();
});
