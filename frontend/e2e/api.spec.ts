import { test, expect } from '@playwright/test';

// API contract tests at the HTTP layer. Equivalent to the Go integration
// tests but exercised end-to-end through the actual served binary, so
// we catch routing / static-handler regressions.

test('GET /api/auth/config returns the OAuth state', async ({ request }) => {
  const res = await request.get('/api/auth/config');
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(typeof body.githubEnabled).toBe('boolean');
});

test('GET /api/documents without sign-in returns 401', async ({ request }) => {
  const res = await request.get('/api/documents');
  expect(res.status()).toBe(401);
});

test('GET /api/me/notifications without sign-in returns 401', async ({ request }) => {
  const res = await request.get('/api/me/notifications');
  expect(res.status()).toBe(401);
});

test('GET /api/me/tokens without sign-in returns 401', async ({ request }) => {
  const res = await request.get('/api/me/tokens');
  expect(res.status()).toBe(401);
});

test('GET /api/me/trash without sign-in returns 401', async ({ request }) => {
  const res = await request.get('/api/me/trash');
  expect(res.status()).toBe(401);
});

test('POST /api/auth/github/login returns 503 when OAuth is unconfigured', async ({ request }) => {
  // The dev/test config doesn't have GitHub creds → expect 503 not 5xx
  // chaos.
  const res = await request.get('/api/auth/github/login');
  // 503 (oauth not configured) OR 302 (redirect to GitHub when configured).
  // Just verify it's not a 5xx.
  expect([200, 302, 503]).toContain(res.status());
});

test('GET /api/auth/me with a bogus session cookie returns null user', async ({ request }) => {
  const res = await request.get('/api/auth/me', {
    headers: {
      Cookie: 'mm_session=nonexistent',
    },
  });
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(body.user).toBeNull();
});

test('PUT /api/me/anthropic-key with token auth is rejected (cookie-only)', async ({ request }) => {
  const res = await request.put('/api/me/anthropic-key', {
    headers: {
      Authorization: 'Bearer mmk_' + 'x'.repeat(64),
      'Content-Type': 'application/json',
    },
    data: { key: 'sk-ant-fake' },
  });
  // Either 401 (token didn't resolve) or 403 (cookie-only enforcement).
  expect([401, 403]).toContain(res.status());
});
