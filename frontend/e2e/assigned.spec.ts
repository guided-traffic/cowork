import { keyOf, Session, sessionContext, signInWithNewPassword, tenant } from './support/api';
import { expect, expectScheme, newContext, test } from './support/fixtures';
import { adminState, baseURL, freshPassword } from './support/identities';

/**
 * The phase's own path, two identities in one test (docs/adr/0056 D2, D3), each in a browser
 * context of its own:
 * 1. the local administrator makes a local account of the fixture tenant (seeded through the API,
 *    the account's temporary password changed), and files a ticket in the test's project through
 *    the filing dialog, assigned to that account;
 * 2. the account, whose page is open on "Assigned to me" before the ticket exists, sees it there and
 *    the bell count it within the event stream's latency, without reloading
 *    (docs/adr/0020 D2, D4), and finds it in its inbox, "assigned it to you";
 * 3. the account opens it from the inbox, edits its title in place and its body as Markdown
 *    (docs/adr/0018 D2, docs/adr/0011 D1), moves it forward, and closes it by hand with a
 *    verification note (docs/adr/0009 D5);
 * 4. the administrator's open page of the ticket shows the new title, the body as the server
 *    rendered it, and the ticket done, again through the stream.
 */
test(
  'the owner files a ticket, assigns it to a second identity, which sees it in assigned to me and its inbox, edits it, moves it and closes it',
  { tag: '@smoke' },
  async ({ page, browser, project }) => {
    const username = `e2e-${Math.random().toString(36).slice(2, 10)}`;
    const name = `E2E ${username}`;
    const temporary = freshPassword();
    const chosen = freshPassword();
    const key = keyOf(project, 1);

    // The second identity: a local account the administrator makes, signed in in the test's own
    // context, which the default `page` belongs to.
    const seeding = await sessionContext(baseURL, adminState);
    try {
      await new Session(seeding).createAccount(username, temporary, 'member');
    } finally {
      await seeding.dispose();
    }
    await signInWithNewPassword(page.context().request, baseURL, username, temporary, chosen);

    // The administrator, in a browser context of their own with the session of the global setup.
    const owner = await newContext(browser, adminState);
    try {
      const ownerPage = await owner.newPage();

      // The account waits on "Assigned to me", its stream live, before the ticket exists.
      await page.goto('/me/assigned');
      await expectScheme(page);
      await expect(page.getByTestId('me-menu')).toHaveAttribute(
        'aria-label',
        `Signed in as ${name}`,
      );
      await expect(page.getByTestId('assigned-empty')).toBeVisible();
      await expect(page.getByTestId('live-indicator')).toHaveAttribute('data-status', 'live');

      // The administrator files it in the backlog's dialog, assigned to the account.
      await ownerPage.goto(`/t/${tenant}/p/${project}/backlog`);
      await expectScheme(ownerPage);
      await ownerPage.getByTestId('new-ticket').click();
      await ownerPage.getByTestId('new-title').fill('Check the second identity');
      await ownerPage.getByTestId('new-assignee').click();
      await ownerPage.getByRole('option', { name, exact: true }).click();
      await ownerPage.getByTestId('new-save').click();
      const row = ownerPage.getByTestId('group-later').getByTestId(`row-${key}`);
      await expect(row).toContainText('Check the second identity');
      await row.click();
      await expect(ownerPage).toHaveURL(new RegExp(`/t/${tenant}/tickets/${project}-1$`));
      await expect(ownerPage.getByTestId('ticket-state').locator('[data-state]')).toHaveAttribute(
        'data-state',
        'filed',
      );
      await expect(ownerPage.getByTestId('live-indicator')).toHaveAttribute('data-status', 'live');

      // The account's open page shows it, and the bell counts it, without a reload.
      await expect(page.getByTestId(`assigned-${key}`)).toContainText('Check the second identity');
      await expect(page.getByTestId('bell-count')).toHaveText('1');
      await page.getByTestId('nav-inbox').click();
      const group = page.getByTestId(`group-${key}`);
      await expect(group).toContainText('Check the second identity');
      await expect(group).toContainText('assigned it to you');

      // The account opens it from the inbox, moves it forward and closes it with a note.
      await group.getByTestId(`open-${key}`).click();
      await expect(page).toHaveURL(new RegExp(`/t/${tenant}/tickets/${project}-1$`));

      // The account edits the title in place and the body as Markdown; the administrator's page,
      // open all along, shows both, the body as the server rendered it.
      await page.getByRole('button', { name: 'Edit the title' }).click();
      const title = page.getByRole('textbox', { name: 'Title' });
      await title.fill('Check the second identity in its own browser');
      await title.press('Enter');
      await expect(title).toBeHidden();
      await page.getByTestId('body-edit').click();
      await page
        .getByRole('textbox', { name: 'Description, in Markdown' })
        .fill('## Steps\n\n1. Sign in as the second identity\n2. Move it **forward**');
      await page.getByTestId('body-save').click();
      const body = page.getByTestId('body');
      await expect(body.getByRole('heading', { name: 'Steps' })).toBeVisible();
      await expect(body.getByRole('listitem')).toHaveText([
        'Sign in as the second identity',
        'Move it forward',
      ]);
      await expect(ownerPage.getByTestId('ticket-title')).toHaveText(
        'Check the second identity in its own browser',
      );
      const shown = ownerPage.getByTestId('body');
      await expect(shown.getByRole('heading', { name: 'Steps' })).toBeVisible();
      await expect(shown.locator('strong')).toHaveText('forward');

      await page.getByTestId('move-analysed').click();
      await expect(page.getByTestId('move-decided')).toBeVisible();
      await page.getByTestId('more-moves').click();
      await page.getByRole('menuitem', { name: 'Done by hand' }).click();
      const note = page.getByTestId('move-text');
      await expect(note).toBeVisible();
      await note.fill('Checked with the second identity in its own browser.');
      await page.getByTestId('move-send').click();
      await expect(note).toBeHidden();
      await expect(page.getByTestId('ticket-state').locator('[data-state]')).toHaveAttribute(
        'data-state',
        'done',
      );

      // The administrator's page, open all along, shows it done.
      await expect(ownerPage.getByTestId('ticket-state').locator('[data-state]')).toHaveAttribute(
        'data-state',
        'done',
      );
    } finally {
      await owner.close();
    }
  },
);
