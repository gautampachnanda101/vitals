// Real-browser regression tests for `vitals dashboard`. These exist for
// what internal/dashboard's Go tests (asserting on server-rendered HTML
// strings) structurally cannot see: whether a client-side fetch() actually
// resolves in a real page, whether a real click does anything, whether
// dark mode actually renders, whether the browser logs a console error.
// See e2e/package.json for why this is the repo's only Node toolchain.
const { test, expect } = require('@playwright/test');

// power is deliberately excluded: it's Available: HasBattery-gated, and a
// CI runner has no battery -- exercising it here would just be testing
// unavailablePage() again, already covered by internal/dashboard's own
// Go tests (TestRouteRendersUnavailablePageWithItsReasonNotA404).
const PAGES = ['/', '/cpu', '/mem', '/disk', '/net', '/gpu', '/advice', '/clean', '/dupes', '/processes', '/llm', '/system'];

for (const path of PAGES) {
  test(`page ${path || '/'} loads with no console error`, async ({ page }) => {
    const errors = [];
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('console', (msg) => {
      if (msg.type() === 'error') errors.push(msg.text());
    });

    const res = await page.goto(path);
    expect(res.status()).toBe(200);
    await expect(page.locator('nav[aria-label="Primary"]')).toBeVisible();
    expect(errors, `console errors on ${path}: ${errors.join('; ')}`).toEqual([]);
  });
}

test('unknown path 404s with the nav still rendered, not a bare error page', async ({ page }) => {
  const res = await page.goto('/nope-does-not-exist');
  expect(res.status()).toBe(404);
  await expect(page.locator('nav[aria-label="Primary"]')).toBeVisible();
});

test('GPU page never shows the bare zero-VRAM placeholder', async ({ page }) => {
  // Regression guard for a real user report: an Apple Silicon GPU with a
  // real utilization/memory-in-use reading (see internal/gpu/gpu.go's
  // parseIORegApple) rendered as "0% util, 0 B / 0 B VRAM" -- meaningless
  // telemetry masquerading as real data. Only "0 B / 0 B" is asserted
  // here: "0% util" alone is real, legitimate telemetry for a genuinely
  // idle GPU (confirmed live on an unloaded CI runner, not a guess) and
  // must not be flagged as if it were the placeholder.
  await page.goto('/gpu');
  const body = await page.locator('main').innerText();
  expect(body).not.toMatch(/0\s*B\s*\/\s*0\s*B/);
});

test('Advice page resolves the AI-commentary fragment, never stays on the placeholder', async ({ page }) => {
  // Regression guard for the Prepare-blocking bug: renderAdvice's
  // placeholder must always resolve via the real /advice/commentary
  // fetch() -- either real AI commentary or the "no LLM reachable"
  // message -- within a bounded time, not hang indefinitely (that hang
  // is exactly what a real user saw as "the Advice page is blank").
  //
  // The bound is generous, not tight: /advice/commentary's own budget is
  // internal/llm's completeTimeout (60s) plus however long probing every
  // local runtime (Ollama/LM Studio/llama.cpp/vLLM) takes first -- a CI
  // runner reaching none of them quickly is the expected case, not a
  // flake, so this must comfortably clear 60s rather than assume a fast
  // failure.
  test.setTimeout(90_000);
  await page.goto('/advice');
  await expect(page.locator('#ai-commentary')).not.toContainText('Asking the LLM', { timeout: 85_000 });
  const text = await page.locator('#ai-commentary').innerText();
  expect(text.length).toBeGreaterThan(0);
});

test('Clean page Preview button populates a real result via a real click', async ({ page }) => {
  await page.goto('/clean');
  await page.click('#clean-preview-btn');
  await expect(page.locator('#clean-preview-result')).not.toBeEmpty({ timeout: 15_000 });
  // Read-only by design (ReclaimableSummary never deletes) -- see
  // docs/roadmap/items/005-dashboard-write-actions/design.md §4 -- so a
  // real click here is safe to run in CI.
});

test('a cross-origin POST to a write action is rejected, not silently accepted', async ({ page, request }) => {
  // The actual CSRF-shaped regression this guards: guide.ServeLocal's
  // sameOriginOnly must reject a POST carrying a foreign Origin header --
  // verified here via a real HTTP request with a real browser-shaped
  // header, not just internal/guide's own unit tests.
  await page.goto('/clean'); // establish baseURL context; not required for the request below
  const res = await request.post('/clean/preview', {
    headers: { Origin: 'https://evil.example' },
  });
  expect(res.status()).toBe(403);
});

test('the sidebar groups nav links into real sections and highlights the current page', async ({ page }) => {
  await page.goto('/cpu');
  const sidebar = page.locator('nav[aria-label="Primary"]');
  await expect(sidebar.locator('h4')).toContainText(['Overview', 'Resources', 'Intelligence', 'Tools', 'System']);
  await expect(sidebar.locator('a[aria-current="page"]')).toHaveText('CPU');
});

test('clicking a Resources link navigates to that page', async ({ page }) => {
  await page.goto('/');
  await page.locator('nav[aria-label="Primary"] a', { hasText: 'Processes' }).click();
  await expect(page).toHaveURL(/\/processes$/);
  await expect(page.locator('main')).toContainText(/CPU|No processes to show/);
});

test('dark mode renders the page without breaking the layout', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/');
  await expect(page.locator('nav[aria-label="Primary"]')).toBeVisible();
  await expect(page.locator('header.top')).toBeVisible();
});
