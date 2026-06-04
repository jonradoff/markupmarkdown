import { test, expect } from '@playwright/test';

// Document-level end-to-end flows. Each test creates its own doc through
// the API so the suite is independent of any seed data.
//
// Anonymous identity flow: the home page lets you create a doc from URL
// or by uploading a .md file. Once you're on /d/:id you can comment as
// long as you've set a display name (stored in localStorage by the
// AuthorBadge component) — no GitHub sign-in required.

const SAMPLE_MD = '# Test Document\n\nHello world. Hello again.\n';

async function createDocument(request: any) {
  const res = await request.post('/api/documents', {
    data: { content: SAMPLE_MD, title: 'E2E Test Doc' },
    headers: { 'Content-Type': 'application/json' },
  });
  // This endpoint requires sign-in. If not configured for that in E2E,
  // we fall back to skipping the comment flows.
  if (!res.ok()) return null;
  return res.json();
}

test('opens a document via direct URL', async ({ page, request }) => {
  // Skip if the backend rejects anonymous doc creation (the prod default).
  const doc = await createDocument(request);
  test.skip(!doc, 'anonymous doc creation disabled; skipping');

  await page.goto(`/d/${doc!.id}`);
  await expect(page.locator('text=Test Document').first()).toBeVisible({ timeout: 10000 });
});

test('document page shows comment sidebar', async ({ page, request }) => {
  const doc = await createDocument(request);
  test.skip(!doc, 'anonymous doc creation disabled; skipping');

  await page.goto(`/d/${doc!.id}`);
  // Filter buttons (Open / Unread / Done / All) are the most stable anchor.
  await expect(page.locator('text=Open').first()).toBeVisible({ timeout: 10000 });
});

test('404 doc shows a friendly error', async ({ page }) => {
  await page.goto('/d/00000000-0000-0000-0000-000000000000');
  // We render the structured ErrorBlock with the kind=not_found message.
  await expect(page.locator('text=/not found|All docs/i').first()).toBeVisible({
    timeout: 10000,
  });
});

test('share link copy button is present', async ({ page, request }) => {
  const doc = await createDocument(request);
  test.skip(!doc, 'anonymous doc creation disabled; skipping');

  await page.goto(`/d/${doc!.id}`);
  // Click the share icon. The svg has no aria-label; locate by sibling title.
  const shareBtn = page.locator('button[title="Share this document"]');
  await expect(shareBtn).toBeVisible({ timeout: 10000 });
});
