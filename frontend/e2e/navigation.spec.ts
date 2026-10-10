import { otherTenant, Seed, Session, sessionContext, tenant, uniqueKey } from './support/api';
import {
  asAdmin as test,
  expect,
  expectNoTenantShown,
  expectScheme,
  newAccount,
} from './support/fixtures';
import { adminState, baseURL, seedToken } from './support/identities';

/**
 * The sidebar of the daily work (docs/adr/0023 D4 as amended 2026-10-10): every team of the person
 * with its projects, on every page — a person-level page included —, and a team's configuration as
 * one dialog behind the gear beside its name, each tab at the address its page had.
 */
test(
  'a person with two teams reaches a project of the other team from a page of their own in one click',
  { tag: '@smoke' },
  async ({ browser, seed, project }) => {
    // The second identity: a member of the fixture team whom the administrator grants a role in
    // the administrator's second team, which holds a project of this test.
    const person = await newAccount(browser, 'member');
    const other = await Seed.create(baseURL, seedToken(), otherTenant);
    try {
      const key = uniqueKey();
      await other.project(key);
      const admin = await sessionContext(baseURL, adminState);
      try {
        await new Session(admin).addMember(otherTenant, person.username, 'member');
      } finally {
        await admin.dispose();
      }
      const { page } = person;
      const nav = page.getByRole('navigation', { name: 'Main' });

      await page.goto('/me/assigned');
      await expectScheme(page);
      await expect(page.getByRole('heading', { name: 'Assigned to me', level: 1 })).toBeVisible();
      await expect(nav.getByTestId(`nav-team-${tenant}`)).toHaveText('End to end');
      await expect(nav.getByTestId(`nav-team-${otherTenant}`)).toHaveText('Other end');
      await expect(nav.getByTestId(`nav-project-${tenant}-${project}`)).toBeVisible();
      await expectNoTenantShown(page);

      // One click from a page of the person's own into the other team's project.
      await nav.getByTestId(`nav-project-${otherTenant}-${key}`).click();
      await expect(page).toHaveURL(new RegExp(`/t/${otherTenant}/p/${key}/board$`));
      await expect(page.getByTestId('project-name')).toHaveText(`Project ${key}`);
      await expect(nav.getByTestId(`nav-project-${otherTenant}-${key}`)).toHaveAttribute(
        'aria-current',
        'true',
      );
      await expect(nav.getByTestId(`nav-team-${otherTenant}`)).toHaveAttribute(
        'aria-current',
        'true',
      );

      // What another person made in a team the pages do not show arrives when the pages leave a
      // team, and when the window gets the focus back: project acts are on no event stream.
      const made = uniqueKey();
      await seed.project(made);
      await nav.getByTestId('nav-next').click();
      await expect(page).toHaveURL(/\/me\/next$/);
      await expect(nav.getByTestId(`nav-project-${tenant}-${made}`)).toBeVisible();
      const meanwhile = uniqueKey();
      await other.project(meanwhile);
      await page.evaluate(() => window.dispatchEvent(new Event('focus')));
      await expect(nav.getByTestId(`nav-project-${otherTenant}-${meanwhile}`)).toBeVisible();
    } finally {
      await other.dispose();
      await person.page.context().close();
    }
  },
);

test(
  "opens and closes a team's configuration by its address",
  { tag: '@smoke' },
  async ({ page, project }) => {
    const nav = page.getByRole('navigation', { name: 'Main' });
    const dialog = page.getByRole('dialog', { name: 'Configuration of End to end' });

    // The gear over a page: the dialog on its settings, over the team's dashboard.
    await page.goto(`/t/${tenant}/p/${project}/board`);
    await expectScheme(page);
    await nav.getByRole('link', { name: 'Configuration of End to end' }).click();
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}/settings$`));
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('link', { name: 'Settings' })).toHaveAttribute(
      'aria-current',
      'page',
    );
    await expect(dialog.getByTestId('tenant-name-input')).toHaveValue('End to end');
    await expect(page.getByTestId('tile-state')).toBeAttached();
    await expectNoTenantShown(page);

    // A tab is its page at its own address, in place of the address before.
    await dialog.getByRole('link', { name: 'Members' }).click();
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}/members$`));
    await expect(dialog.getByRole('link', { name: 'Members' })).toHaveAttribute(
      'aria-current',
      'page',
    );

    // A reload keeps the dialog on its tab, and closing still goes back to the page before.
    await page.reload();
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('link', { name: 'Members' })).toHaveAttribute(
      'aria-current',
      'page',
    );
    await dialog.getByRole('button', { name: 'Close the configuration' }).click();
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}/p/${project}/board$`));
    await expect(dialog).toHaveCount(0);

    // Its address opened directly: the dialog over the dashboard, and closing leads to it.
    await page.goto(`/t/${tenant}/audit`);
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('link', { name: 'Audit record' })).toHaveAttribute(
      'aria-current',
      'page',
    );
    await page.keyboard.press('Escape');
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}$`));
    await expect(dialog).toHaveCount(0);
    await expect(page.getByTestId('tile-state')).toBeVisible();
  },
);
