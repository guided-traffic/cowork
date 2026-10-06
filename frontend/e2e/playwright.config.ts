import { defineConfig, devices } from '@playwright/test';
import { baseURL } from './support/identities';

/**
 * The end-to-end tier (docs/adr/0056): the built images behind the Ingress stand-in, reached over
 * HTTPS — WebKit stores no `Secure` cookie from plain-HTTP localhost (docs/adr/0031 D2) — with the
 * stand-in's certificate made for the run, hence `ignoreHTTPSErrors`. `make e2e` starts the stack
 * and sets `COWORK_BASE_URL`; the default is the stack of `make e2e-up` (support/identities.ts).
 *
 * Every path tagged `@smoke` runs in Chromium and WebKit, each in both colour schemes (D3, D8);
 * anything else in Chromium only. A path tagged `@dark` — a screenshot of the dark scheme alone —
 * runs in the dark projects only. No retries: a flaky test is a defect, not a setting (D7).
 */
const ci = !!process.env['CI'];

export default defineConfig({
  testDir: '.',
  outputDir: 'test-results',
  // One picture per test, browser and scheme, whatever the platform: the comparison is coarse and
  // the text in it is hidden (screenshot.css), so one baseline serves macOS and the Linux runners.
  snapshotPathTemplate: '{testDir}/screenshots/{testFileName}/{arg}-{projectName}{ext}',
  globalSetup: './global-setup.ts',
  fullyParallel: true,
  forbidOnly: ci,
  retries: 0,
  workers: ci ? 2 : 4,
  timeout: 30_000,
  reporter: [['list'], ['html', { outputFolder: 'playwright-report', open: 'never' }]],
  expect: {
    timeout: 10_000,
    toHaveScreenshot: { maxDiffPixelRatio: 0.02, animations: 'disabled', caret: 'hide' },
  },
  use: {
    baseURL,
    ignoreHTTPSErrors: true,
    testIdAttribute: 'data-testid',
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium-light',
      use: { ...devices['Desktop Chrome'], colorScheme: 'light' },
      grepInvert: /@dark/,
    },
    {
      name: 'chromium-dark',
      use: { ...devices['Desktop Chrome'], colorScheme: 'dark' },
    },
    {
      name: 'webkit-light',
      use: { ...devices['Desktop Safari'], colorScheme: 'light' },
      grep: /@smoke/,
      grepInvert: /@dark/,
    },
    {
      name: 'webkit-dark',
      use: { ...devices['Desktop Safari'], colorScheme: 'dark' },
      grep: /@smoke/,
    },
  ],
});
