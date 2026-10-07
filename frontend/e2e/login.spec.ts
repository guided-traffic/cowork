import { Page } from '@playwright/test';
import { keyOf, Session, sessionContext, tenant } from './support/api';
import { expect, expectScheme, test } from './support/fixtures';
import {
  adminPassword,
  adminState,
  adminUser,
  dexMember,
  freshPassword,
} from './support/identities';

/**
 * The login's paths (docs/adr/0031–0033, 0029, 0037): the local form, a temporary password changed
 * at the first sign-in, the identity provider's way through Dex, the session cookie the browser
 * keeps, a write that passes the CSRF check, and the sign-out back to the login page.
 */

const sessionCookie = '__Host-cowork-session';

async function signInWithForm(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/');
  await expect(page).toHaveURL(/\/login/);
  await page.getByTestId('login-username').fill(username);
  await page.getByTestId('login-password').fill(password);
  await page.getByTestId('login-submit').click();
}

async function signOut(page: Page): Promise<void> {
  await page.getByTestId('me-menu').click();
  // A PrimeNG menu item takes no data-testid of its own; it is found by its role and name.
  await page.getByRole('menuitem', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByTestId('login-submit')).toBeVisible();
}

test(
  'the local administrator signs in with the form, keeps the session, writes past the CSRF check and signs out',
  { tag: '@smoke' },
  async ({ page, context, seed, project }) => {
    await signInWithForm(page, adminUser, adminPassword);
    await expect(page.getByTestId('me-menu')).toBeVisible();
    await expectScheme(page);

    // The browser stored the cookie — WebKit only does so from an HTTPS origin — with the
    // attributes of docs/adr/0031 D2, and no script of the page can read it.
    const cookie = (await context.cookies()).find((c) => c.name === sessionCookie);
    expect(cookie, 'the session cookie').toBeDefined();
    expect(cookie).toMatchObject({ secure: true, httpOnly: true, sameSite: 'Lax', path: '/' });
    expect(await page.evaluate(() => document.cookie)).not.toContain(sessionCookie);

    // A new document keeps the session.
    await page.goto(`/t/${tenant}/p/${project}/backlog`);
    await page.reload();
    await expect(page.getByTestId('me-menu')).toBeVisible();

    // A write through the session: the page sends the origin and X-Requested-With: cowork.
    await page.getByTestId('new-ticket').click();
    await page.getByTestId('new-title').fill('Written through the session');
    await page.getByTestId('new-save').click();
    await expect(page.getByTestId(`row-${keyOf(project, 1)}`)).toContainText(
      'Written through the session',
    );
    expect((await seed.ticket(project, 1)).title).toBe('Written through the session');

    // The same write without the header is refused by the CSRF check (docs/adr/0037).
    const refused = await page.evaluate(
      async ({ tenant, project }) => {
        const response = await fetch(`/api/v1/tenants/${tenant}/projects/${project}/tickets`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            type: 'task',
            title: 'Forged',
            severity: 'low',
            security: 'none',
            effort: 'S',
          }),
        });
        return { status: response.status, code: (await response.json()).code };
      },
      { tenant, project },
    );
    expect(refused).toEqual({ status: 403, code: 'csrf' });

    await signOut(page);
    // The session is gone, not just the page: a new document asks for the login again.
    await page.goto(`/t/${tenant}/p/${project}/backlog`);
    await expect(page).toHaveURL(/\/login\?return=/);
  },
);

test(
  'a local account changes its temporary password at the first sign-in',
  { tag: '@smoke' },
  async ({ page, baseURL }) => {
    const username = `e2e-${Math.random().toString(36).slice(2, 10)}`;
    const temporary = freshPassword();
    const chosen = freshPassword();
    const context = await sessionContext(baseURL!, adminState);
    try {
      await new Session(context).createAccount(username, temporary);
    } finally {
      await context.dispose();
    }

    await signInWithForm(page, username, temporary);
    await expect(page).toHaveURL(/\/password/);
    await expect(page.getByTestId('password-required')).toBeVisible();
    await expectScheme(page);
    await page.getByTestId('password-current').fill(temporary);
    await page.getByTestId('password-new').fill(chosen);
    await page.getByTestId('password-again').fill(chosen);
    await page.getByTestId('password-submit').click();
    await expect(page).not.toHaveURL(/\/password/);
    await expect(page.getByTestId('me-menu')).toBeVisible();
    await expect(page.getByTestId('tenant-name')).toHaveText('End to end');

    await signOut(page);
    await signInWithForm(page, username, chosen);
    await expect(page.getByTestId('me-menu')).toBeVisible();
    await expect(page).not.toHaveURL(/\/password/);
  },
);

test(
  'a person of the identity provider signs in with Dex and signs out',
  { tag: '@smoke' },
  async ({ page }) => {
    await page.goto('/login');
    await expectScheme(page);
    await expect(page.getByTestId('login-oidc')).toHaveText(/Sign in with Dex/);
    await page.getByTestId('login-oidc').click();

    // Dex's own form, on the issuer's origin.
    await expect(page).toHaveURL(/localhost:\d+\/dex\//);
    await page.locator('input[name="login"]').fill(dexMember.email);
    await page.locator('input[name="password"]').fill(dexMember.password);
    await page.locator('button[type="submit"]').click();

    // Back on cowork's origin with a session: a member of the tenant by the mapping of team-red.
    await expect(page.getByTestId('me-menu')).toBeVisible();
    await expect(page.getByTestId('me-menu')).toHaveAttribute(
      'aria-label',
      `Signed in as ${dexMember.name}`,
    );
    await expect(page.getByTestId('tenant-name')).toHaveText('End to end');

    await signOut(page);
  },
);

test(
  "a person of the identity provider whose session ended is sent to the provider's sign-in without a click",
  { tag: '@smoke' },
  async ({ page, context }) => {
    // Signed in with the button: the browser remembers the identity provider (docs/adr/0029 D6).
    await page.goto('/login');
    await page.getByTestId('login-oidc').click();
    await expect(page).toHaveURL(/localhost:\d+\/dex\//);
    await page.locator('input[name="login"]').fill(dexMember.email);
    await page.locator('input[name="password"]').fill(dexMember.password);
    await page.locator('button[type="submit"]').click();
    await expect(page.getByTestId('me-menu')).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem('cowork.sign-in'))).toBe('oidc');

    // The session ends without a sign-out — here the browser loses its cookie — and the next page
    // the person opens sends them to the login page, which says that it signs them in again.
    await context.clearCookies({ name: sessionCookie });
    const starts: string[] = [];
    page.on('request', (request) => {
      if (new URL(request.url()).pathname === '/auth/oidc/login') {
        starts.push(request.url());
      }
    });
    await page.goto('/');
    await expect(page).toHaveURL(/\/login\?return=/);
    await expect(page.getByTestId('login-resuming')).toContainText(
      'Signing you in again through Dex',
    );
    expect(starts, 'nothing before a sign of the person').toEqual([]);

    // The pointer moves: the page leaves for the provider by itself, with silent=true. Dex keeps no
    // session and ignores prompt=none, so its form is where the person lands — without a click.
    await page.mouse.move(40, 40);
    await page.mouse.move(160, 120, { steps: 4 });
    await expect(page).toHaveURL(/localhost:\d+\/dex\//);
    await expect(page.locator('input[name="login"]')).toBeVisible();
    expect(starts).toHaveLength(1);
    expect(new URL(starts[0]).searchParams.get('silent')).toBe('true');
    expect(new URL(starts[0]).searchParams.get('return_to')).toBe('/');
  },
);
