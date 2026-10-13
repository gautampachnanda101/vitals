// Real-browser regression tests for `vitals dashboard`. These exist for
// what internal/dashboard's Go tests (asserting on server-rendered HTML
// strings) structurally cannot see: whether a client-side fetch() actually
// resolves in a real page, whether a real click does anything, whether
// dark mode actually renders, whether the browser logs a console error.
// See e2e/package.json for why this is the repo's only Node toolchain.
const { test, expect } = require('@playwright/test');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

// power is deliberately excluded: it's Available: HasBattery-gated, and a
// CI runner has no battery -- exercising it here would just be testing
// unavailablePage() again, already covered by internal/dashboard's own
// Go tests (TestRouteRendersUnavailablePageWithItsReasonNotA404).
const PAGES = ['/', '/cpu', '/mem', '/disk', '/net', '/gpu', '/advice', '/clean', '/dupes', '/processes', '/llm', '/system'];

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

test('Disk renders before its scan and resolves the async result', async ({ page }) => {
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  await page.route('**/disk/usage**', async route => {
    await gate;
    await route.fulfill({
      status: 200,
      contentType: 'text/html',
      body: '<p data-testid="disk-scan-proof">Known fixture: 9010 logical bytes; no deletion</p>',
    });
  });
  try {
    const response = await page.goto('/disk');
    expect(response.status()).toBe(200);
    await expect(page.locator('nav[aria-label="Primary"]')).toBeVisible();
    await expect(page.locator('#disk-usage')).toContainText(/Loading|Scanning/i);
    release();
    await expect(page.getByTestId('disk-scan-proof')).toBeVisible();
  } finally {
    release();
  }
});

test('Disk scan requests reject a cross-origin browser trigger', async ({ request }) => {
  const response = await request.get('/disk/usage', {
    headers: { Origin: 'https://evil.example', 'Sec-Fetch-Site': 'cross-site' },
  });
  expect(response.status()).toBe(403);
});

test('Disk drill-down, Up and rescan use real read-only fixture results', async ({ page }) => {
  const root = fs.mkdtempSync(path.join(os.homedir(), '.vitals-disk-e2e-'));
  const relativeRoot = path.basename(root);
  const files = {
    'large/nested.txt': 'a'.repeat(6000),
    'small/tiny.txt': '1234567890',
    'loose.txt': 'b'.repeat(3000),
  };
  const metadata = {};
  const requests = [];
  let initial = true;
  try {
    for (const [name, content] of Object.entries(files)) {
      const filename = path.join(root, name);
      fs.mkdirSync(path.dirname(filename), { recursive: true });
      fs.writeFileSync(filename, content);
      metadata[name] = fs.statSync(filename);
    }
    await page.route('**/disk/usage**', async route => {
      const url = new URL(route.request().url());
      requests.push(url.searchParams.get('root') || '.');
      if (initial) {
        initial = false;
        await route.fulfill({
          status: 200,
          contentType: 'text/html',
          body: `<a data-disk-scan data-disk-root="${relativeRoot}" href="/disk/usage?root=${relativeRoot}">Open fixture</a>`,
        });
      } else {
        await route.continue();
      }
    });
    await page.goto('/disk');
    await page.getByRole('link', { name: 'Open fixture' }).click();
    await expect(page.locator('#disk-usage')).toContainText('8.80 KB observed');
    await expect(page.locator('#disk-usage')).toContainText('Complete');
    await expect(page.locator('svg[aria-label="Folders chart"]')).toBeVisible();
    await expect(page.locator('svg[aria-label="Files chart"]')).toBeVisible();
    expect(requests).toContain(relativeRoot);
    await page.locator(`[data-disk-root="${relativeRoot}/large"]`).click();
    await expect(page.locator('#disk-usage')).toContainText('5.86 KB observed');
    await expect(page.locator('#disk-usage')).toContainText(`${relativeRoot}/large`);
    await page.getByRole('link', { name: 'Up', exact: true }).click();
    await expect(page.locator('#disk-usage')).toContainText('8.80 KB observed');
    files['large/nested.txt'] = 'a'.repeat(7000);
    fs.writeFileSync(path.join(root, 'large/nested.txt'), files['large/nested.txt']);
    metadata['large/nested.txt'] = fs.statSync(path.join(root, 'large/nested.txt'));
    await page.locator('#disk-rescan').click();
    await expect(page.locator('#disk-usage')).toContainText('9.78 KB observed');
    await page.locator('#disk-scan-options input[name="budget"]').fill('1ns');
    await page.getByRole('button', { name: 'Rescan with these limits' }).click();
    await expect(page.locator('#disk-usage')).toContainText('Partial');
    await expect(page).toHaveURL(/\/disk$/);
    await expect(page.locator('nav[aria-label="Primary"]')).toBeVisible();
    for (const [name, content] of Object.entries(files)) {
      const filename = path.join(root, name);
      expect(fs.readFileSync(filename, 'utf8')).toBe(content);
      const after = fs.statSync(filename);
      expect(after.mode).toBe(metadata[name].mode);
      expect(after.mtimeMs).toBe(metadata[name].mtimeMs);
    }
    expect(fs.readdirSync(root).sort()).toEqual(['large', 'loose.txt', 'small']);
    expect(fs.readdirSync(path.join(root, 'large'))).toEqual(['nested.txt']);
    expect(fs.readdirSync(path.join(root, 'small'))).toEqual(['tiny.txt']);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test('Disk rejects an escaped scope visibly and recovery keeps the dashboard', async ({ page }) => {
  await page.route('**/disk/usage**', async route => {
    const url = new URL(route.request().url());
    if (url.searchParams.get('root') === '..') {
      await route.continue();
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: '<p>Choose a test scope</p><a data-disk-scan href="/disk/usage?root=..">Outside scope</a>',
      });
    }
  });
  await page.goto('/disk');
  await page.getByRole('link', { name: 'Outside scope' }).click();
  await expect(page.locator('#disk-usage')).toContainText(/real directory inside your home/i);
  await page.getByRole('link', { name: 'Return to home scope' }).click();
  await expect(page.locator('#disk-usage')).toContainText('Choose a test scope');
  await expect(page).toHaveURL(/\/disk$/);
  await expect(page.locator('nav[aria-label="Primary"]')).toBeVisible();
});

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

test('Advice page resolves the AI-commentary fragment, never stays on the placeholder', async ({ page }) => {
  await page.goto('/advice');
  await expect(page.locator('nav[aria-label="Primary"]')).toBeVisible();
  await expect(page.locator('#ai-commentary')).toContainText(
    'Ollama stub response: deterministic dashboard commentary.', { timeout: 10_000 });
  await expect(page.locator('#ai-commentary')).not.toContainText('Asking the LLM');
});

test('Clean page Preview button populates a real result via a real click', async ({ page }) => {
  await page.goto('/clean');
  await page.click('#clean-preview-btn');
  await expect(page.locator('#clean-preview-result')).not.toBeEmpty({ timeout: 15_000 });
  // Read-only by design (ReclaimableSummary never deletes) -- see
  // docs/roadmap/items/005-dashboard-write-actions/design.md §4 -- so a
  // real click here is safe to run in CI.
});

test('Clean selection invalidates preview and apply sends only previewed options', async ({ page }) => {
  const previews = [];
  const applies = [];
  await page.route('**/clean/preview', async route => {
    previews.push(route.request().postDataJSON());
    await route.fulfill({ status: 200, contentType: 'text/html', body: '<div data-clean-complete="true">Read-only test preview</div>' });
  });
  // Never let this test execute a real cleanup.
  await page.route('**/clean/apply', async route => {
    applies.push(route.request().postDataJSON());
    await route.fulfill({ status: 200, contentType: 'text/html', body: '<div>Test result; no deletion performed</div>' });
  });
  await page.goto('/clean');
  await expect(page.locator('#clean-categories input[value="trash"]')).not.toBeChecked();
  await expect(page.locator('#clean-categories input[value="tools"]')).not.toBeChecked();
  await page.locator('#clean-categories input[value="temp"]').uncheck();
  await page.click('#clean-preview-btn');
  await expect(page.locator('#clean-apply-btn')).toBeVisible();
  expect(previews[0].categories).toBe('caches');
  await page.fill('#clean-min-age', '336h');
  await expect(page.locator('#clean-apply-btn')).toBeHidden();
  await page.click('#clean-preview-btn');
  await expect(page.locator('#clean-apply-btn')).toBeVisible();
  page.once('dialog', dialog => dialog.accept());
  await page.click('#clean-apply-btn');
  await expect(page.locator('#clean-apply-result')).toContainText('no deletion performed');
  expect(applies).toEqual([{ confirm: true, ...previews[1] }]);
  expect(applies[0].categories).toBe('caches');
  expect(applies[0].min_age).toBe('336h');
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

// Every resource page must answer "what's using this" with a top-processes
// table, even when the resource itself is healthy -- a maintainer
// requirement. The per-renderer HTML is asserted deterministically in
// internal/dashboard's Go tests (with a fake process cache); here we just
// confirm it survives a real page load on the two pages that reach it via
// data the CI runner reliably has. A generous timeout because the first
// hit blocks on a real ~500ms process sample.
for (const path of ['/cpu', '/mem']) {
  test(`resource page ${path} shows a top-processes section`, async ({ page }) => {
    await page.goto(path);
    await expect(page.locator('main')).toContainText(/Top processes/, { timeout: 15000 });
  });
}

test('dark mode renders the page without breaking the layout', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/');
  await expect(page.locator('nav[aria-label="Primary"]')).toBeVisible();
  await expect(page.locator('header.top')).toBeVisible();
});
