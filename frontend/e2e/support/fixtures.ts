import { test as base, Browser, BrowserContext, expect, Locator, Page } from '@playwright/test';
import { Session, Seed, sessionContext, signInWithNewPassword, tenant, uniqueKey } from './api';
import { adminState, baseURL, freshPassword, seedToken } from './identities';

/** A person of a path beside the one whose `page` the test has, in a browser context of their own. */
export interface Person {
  page: Page;
  /** The person's id, which a ticket is assigned to. */
  id: string;
  /** The name the page shows them by. */
  name: string;
}

interface TestFixtures {
  /** A project of the fixture tenant that belongs to this test alone, by its key. */
  project: string;
  /**
   * A member of the fixture tenant made for this test — a local account, its temporary password
   * changed —, signed in in a browser context of its own: the second identity of a path
   * (docs/adr/0056 D2).
   */
  member: Person;
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
  member: async ({ browser }, use) => {
    const member = await newAccount(browser, 'member');
    await use(member);
    await member.page.context().close();
  },
});

/** Tests that start as the local administrator, signed in by the global setup. */
export const asAdmin = test.extend({ storageState: adminState });

export { expect };

/**
 * A browser context beside the test's own, in the colour scheme of the test's project; with the
 * administrator's session of the global setup when `storageState` names it.
 */
export async function newContext(browser: Browser, storageState?: string): Promise<BrowserContext> {
  return browser.newContext({
    baseURL,
    ignoreHTTPSErrors: true,
    storageState,
    colorScheme: test.info().project.use.colorScheme,
  });
}

/**
 * A local account of a tenant, made by the administrator for this test, signed in in a browser
 * context of its own with a password it chose (docs/adr/0033 D4). The caller closes the context.
 */
export async function newAccount(
  browser: Browser,
  role: 'viewer' | 'member' | 'admin',
  slug = tenant,
): Promise<Person> {
  const username = `e2e-${Math.random().toString(36).slice(2, 10)}`;
  const temporary = freshPassword();
  const admin = await sessionContext(baseURL, adminState);
  const account = await new Session(admin)
    .createAccount(username, temporary, role, slug)
    .finally(() => admin.dispose());
  const context = await newContext(browser);
  await signInWithNewPassword(context.request, baseURL, username, temporary, freshPassword());
  return { page: await context.newPage(), id: account.id, name: account.display_name };
}

/**
 * Collects what the page's content-security policy refuses from now on, across navigations: the
 * `securitypolicyviolation` events of every document, and the browser's console errors that name
 * the policy. The shell's policy is frontend/nginx/default.conf's (docs/security/trust-boundaries.md).
 */
export async function policyViolations(page: Page): Promise<string[]> {
  const seen: string[] = [];
  await page.exposeFunction('__coworkPolicyViolation', (violation: string) => {
    seen.push(violation);
  });
  await page.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', (event) => {
      const report = (window as unknown as Record<string, (violation: string) => void>)[
        '__coworkPolicyViolation'
      ];
      report(`${event.effectiveDirective} refused ${event.blockedURI || '(inline)'}`);
    });
  });
  page.on('console', (message) => {
    if (message.type() === 'error' && /content security policy/i.test(message.text())) {
      seen.push(message.text());
    }
  });
  return seen;
}

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
 * The page says team, never tenant (docs/adr/0005 D1): neither its text — what a screen reader
 * reads of it included — nor a placeholder, a label, a title or an alternative text. The unit tier
 * holds the templates and the code to it; this holds a page once it shows its data. Each line or
 * attribute that says the word is named in the failure.
 */
export async function expectNoTenantShown(page: Page): Promise<void> {
  const said = await page.evaluate(() => {
    const word = /\btenants?\b/i;
    const attributes = ['placeholder', 'aria-label', 'title', 'alt'];
    const lines = document.body.innerText.split('\n').filter((line) => word.test(line));
    for (const element of document.body.querySelectorAll(
      '[placeholder], [aria-label], [title], [alt]',
    )) {
      for (const name of attributes) {
        const value = element.getAttribute(name);
        if (value !== null && word.test(value)) {
          lines.push(`${element.tagName.toLowerCase()} ${name}: ${value}`);
        }
      }
    }
    return lines;
  });
  expect(said).toEqual([]);
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
