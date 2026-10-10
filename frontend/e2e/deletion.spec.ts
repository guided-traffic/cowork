import { Page } from '@playwright/test';
import { tenant } from './support/api';
import { asAdmin as test, expect, expectScheme } from './support/fixtures';

const rows = (page: Page) =>
  page.getByTestId('group-later').locator(`tr[data-testid^="row-${tenant}/"]`);

/**
 * Opens the bin: the team's gear in the sidebar, then the tab of the deleted tickets in the dialog of
 * the team's configuration (docs/adr/0023 D4 as amended 2026-10-10).
 */
async function openTheBin(page: Page): Promise<void> {
  await page.getByRole('link', { name: 'Configuration of End to end' }).click();
  await page
    .getByRole('dialog', { name: 'Configuration of End to end' })
    .getByRole('link', { name: 'Deleted tickets' })
    .click();
  await expect(page).toHaveURL(new RegExp(`/t/${tenant}/deleted-tickets$`));
}

/** Deletes the ticket from its page, after its question, which lands on the project's backlog. */
async function deleteFromItsPage(page: Page, short: string): Promise<void> {
  await page.goto(`/t/${tenant}/tickets/${short}`);
  await expectScheme(page);
  await page.getByRole('button', { name: 'Delete this ticket' }).click();
  await page
    .getByRole('alertdialog', { name: `Delete ${short}?` })
    .getByRole('button', { name: 'Delete' })
    .click();
  await expect(page).toHaveURL(new RegExp(`/backlog$`));
}

/**
 * Deletion (docs/adr/0024 D1, D2, D7): an administrator deletes a ticket from its page after the
 * question; to a member it is no such ticket at its address and absent from the backlog; the
 * administrator restores it from the deleted tickets, and the member's backlog shows it again;
 * deleted again, it is purged after the two questions, and the bin no longer holds it.
 */
test(
  'an administrator deletes a ticket, restores it from the bin, deletes it again and purges it',
  { tag: '@smoke' },
  async ({ page, seed, project, member }) => {
    const doomed = await seed.file(project, { title: 'Filed by mistake' });
    await seed.file(project, { title: 'Stays where it is' });
    const short = `${project}-${doomed.number}`;
    const backlog = `/t/${tenant}/p/${project}/backlog`;

    await deleteFromItsPage(page, short);
    await expect(rows(page)).toHaveText([/Stays where it is/]);

    // To a member it does not exist.
    await member.page.goto(`/t/${tenant}/tickets/${short}`);
    await expectScheme(member.page);
    await expect(member.page.getByRole('heading', { name: 'No such ticket' })).toBeVisible();
    await member.page.goto(backlog);
    await expect(rows(member.page)).toHaveText([/Stays where it is/]);
    await expect(member.page.getByTestId('live-indicator')).toHaveAttribute('data-status', 'live');

    // Restored from the bin: back in the member's open backlog.
    await openTheBin(page);
    const entry = page.getByTestId(`bin-${short}`);
    await expect(entry).toContainText('Filed by mistake');
    await entry.getByRole('button', { name: 'Restore' }).click();
    await expect(entry).toHaveCount(0);
    await expect(rows(member.page)).toHaveText([/Filed by mistake/, /Stays where it is/]);

    // Deleted again, and purged after the two questions.
    await deleteFromItsPage(page, short);
    await openTheBin(page);
    await entry.getByRole('button', { name: 'Purge' }).click();
    await page
      .getByRole('alertdialog', { name: `Purge ${short} now?` })
      .getByRole('button', { name: 'Continue' })
      .click();
    await page
      .getByRole('alertdialog', { name: `Purge ${short} for good?` })
      .getByRole('button', { name: 'Purge for good' })
      .click();
    await expect(entry).toHaveCount(0);
    await page.reload();
    await expect(page.getByTestId('bin')).toBeVisible();
    await expect(entry).toHaveCount(0);
    await expect(rows(member.page)).toHaveText([/Stays where it is/]);
  },
);
