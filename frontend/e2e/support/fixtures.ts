import { test as base, expect, Locator, Page } from '@playwright/test';
import { Seed, uniqueKey } from './api';
import { adminState, baseURL, seedToken } from './identities';

interface TestFixtures {
  /** A project of the fixture tenant that belongs to this test alone, by its key. */
  project: string;
}

interface WorkerFixtures {
  /** The administrator's token, seeding through the API (docs/adr/0056 D7). */
  seed: Seed;
}

/** Tests that start without a session: the login's own paths. */
export const test = base.extend<TestFixtures, WorkerFixtures>({
  seed: [
    // eslint-disable-next-line no-empty-pattern -- Playwright reads the fixtures a fixture takes from this pattern
    async ({}, use) => {
      const seed = await Seed.create(baseURL, seedToken());
      await use(seed);
      await seed.dispose();
    },
    { scope: 'worker' },
  ],
  project: async ({ seed }, use) => {
    const key = uniqueKey();
    await seed.project(key);
    await use(key);
  },
});

/** Tests that start as the local administrator, signed in by the global setup. */
export const asAdmin = test.extend({ storageState: adminState });

export { expect };

/**
 * The page shows the scheme the project emulates: the theme follows `prefers-color-scheme` until
 * the person picks one (docs/adr/0052 D3), and dark is `.app-dark` on `<html>`.
 */
export async function expectScheme(page: Page): Promise<void> {
  const dark = await page.evaluate(() => matchMedia('(prefers-color-scheme: dark)').matches);
  const html = page.locator('html');
  if (dark) {
    await expect(html).toHaveClass(/\bapp-dark\b/);
  } else {
    await expect(html).not.toHaveClass(/\bapp-dark\b/);
  }
}

/**
 * A drag as a person makes it, for the Angular CDK: press on the handle, move past the CDK's
 * threshold, travel to the target in steps — the CDK sorts and enters lists on pointer moves —,
 * rest, and release. `at` is where in the target the pointer goes, as fractions of its box.
 */
export async function drag(
  page: Page,
  handle: Locator,
  target: Locator,
  at: { x: number; y: number } = { x: 0.5, y: 0.5 },
): Promise<void> {
  await handle.scrollIntoViewIfNeeded();
  const from = await handle.boundingBox();
  if (!from) {
    throw new Error('the handle has no box');
  }
  const start = { x: from.x + from.width / 2, y: from.y + from.height / 2 };
  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  await page.mouse.move(start.x + 4, start.y + 8, { steps: 4 });
  const to = await target.boundingBox();
  if (!to) {
    throw new Error('the target has no box');
  }
  const end = { x: to.x + to.width * at.x, y: to.y + to.height * at.y };
  await page.mouse.move(end.x, end.y, { steps: 20 });
  await page.mouse.move(end.x, end.y + 1, { steps: 2 });
  await page.mouse.up();
}
