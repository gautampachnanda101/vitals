// Playwright config for `vitals dashboard`'s real-browser tests.
//
// The dashboard normally picks a random ephemeral port and prints it to
// stdout (see main.go's "vitals dashboard", internal/guide/serve.go);
// that's the right default for a real user (no fixed port to collide
// with), but Playwright's built-in webServer health-check needs one known
// URL to poll. --addr pins a fixed one here, only for this test run.
//
// chromium only, deliberately: this suite exists to catch real
// browser-behavior bugs a Go HTML-string test can't (a client-side
// fetch() that never resolves, a button click that does nothing, a
// console error) -- not to catch engine-specific rendering differences.
// Add firefox/webkit projects if that specific need ever comes up; three
// engines x three OSes in CI for a hand-rolled, no-framework page is cost
// without a matching bug class it would catch.
const PORT = 18173;
const binary = process.platform === 'win32' ? '..\\vitals.exe' : '../vitals';

module.exports = {
  testDir: './tests',
  timeout: 30_000,
  fullyParallel: false, // every test hits the same one dashboard process
  workers: 1,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  webServer: {
    // ../vitals: built by the CI step (or `make build`) right before this
    // runs -- see AGENTS.md's "Build, test, lint" and the e2e CI job.
    command: `${binary} dashboard --addr 127.0.0.1:${PORT} --no-open`,
    url: `http://127.0.0.1:${PORT}/`,
    reuseExistingServer: false,
    timeout: 15_000,
  },
};
