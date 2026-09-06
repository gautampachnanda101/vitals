// One-off: capture real screenshots of `vitals dashboard` for the public
// site, using the Playwright + Chromium already vendored in e2e/.
//
//   go build -o vitals . && node scripts/site-screenshots.mjs
//
// Writes PNGs into site/img/. Not wired into CI — the site's screenshots
// are refreshed by hand when the dashboard's look changes, same as its
// hero terminal example.
import { spawn } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import pw from './../e2e/node_modules/playwright/index.js';
const { chromium } = pw;

const PORT = 18191;
const BASE = `http://127.0.0.1:${PORT}`;

const srv = spawn('./vitals', ['dashboard', '--addr', `127.0.0.1:${PORT}`, '--no-open'], {
  stdio: 'inherit',
});
process.on('exit', () => srv.kill());

// wait for the server
for (let i = 0; i < 50; i++) {
  try {
    const r = await fetch(BASE + '/');
    if (r.ok) break;
  } catch {}
  await sleep(200);
}

const shots = [
  { path: '/', name: 'dashboard-overview', scheme: 'light' },
  { path: '/', name: 'dashboard-overview-dark', scheme: 'dark' },
];

const browser = await chromium.launch();
for (const s of shots) {
  const ctx = await browser.newContext({
    viewport: { width: 1280, height: 900 },
    deviceScaleFactor: 2,
    colorScheme: s.scheme,
  });
  const page = await ctx.newPage();
  await page.goto(BASE + s.path, { waitUntil: 'networkidle' });
  await page.waitForSelector('nav[aria-label="Primary"]');
  await sleep(600); // let sparklines / any fetch settle
  await page.screenshot({ path: `site/img/${s.name}.png`, fullPage: false });
  console.log('wrote site/img/' + s.name + '.png');
  await ctx.close();
}
await browser.close();
srv.kill();
process.exit(0);
