import { Page } from '@playwright/test';
import { tenant } from './support/api';
import { asAdmin as test, expect, expectScheme } from './support/fixtures';

const rows = (page: Page) =>
  page.getByTestId('group-later').locator(`tr[data-testid^="row-${tenant}/"]`);

/**
 * A saved filter shared with the tenant (docs/adr/0018 D5, docs/adr/0049 D7): a member narrows the
 * backlog, saves the filter under a name and shares it; the administrator, a second person in a
 * browser of their own, picks it among the saved filters by its name and owner, and the backlog
 * applies its conditions, the owner named beside it.
 */
test(
  'a member saves the backlog’s filter shared, and a second person applies it and sees its owner',
  { tag: '@smoke' },
  async ({ page, seed, project, member }) => {
    await seed.file(project, { title: 'The export forgets the files' });
    await seed.file(project, { title: 'A typo in the footer' });
    const backlog = `/t/${tenant}/p/${project}/backlog`;
    const name = `Exports of ${project}`;

    // The member narrows the backlog and saves the filter, shared.
    await member.page.goto(backlog);
    await expectScheme(member.page);
    await expect(rows(member.page)).toHaveCount(2);
    await member.page.getByRole('searchbox', { name: 'Search', exact: true }).fill('export');
    await expect(rows(member.page)).toHaveText([/The export forgets the files/]);
    await member.page.getByRole('button', { name: 'Save filter' }).click();
    const dialog = member.page.getByRole('dialog', { name: 'Save filter' });
    await dialog.getByRole('textbox', { name: 'Name' }).fill(name);
    await dialog.getByRole('switch', { name: /Share with the tenant/ }).check();
    await dialog.getByRole('button', { name: 'Save' }).click();
    await expect(dialog).toBeHidden();
    await expect(member.page.getByRole('button', { name: `Stop sharing ${name}` })).toHaveAttribute(
      'aria-pressed',
      'true',
    );

    // The administrator applies it from the saved filters and sees whose it is.
    await page.goto(backlog);
    await expectScheme(page);
    await expect(rows(page)).toHaveCount(2);
    await page.getByRole('combobox', { name: 'Saved filters' }).click();
    await page.getByRole('option', { name: `${name} · ${member.name}`, exact: true }).click();
    await expect(page.getByRole('searchbox', { name: 'Search', exact: true })).toHaveValue(
      'export',
    );
    await expect(rows(page)).toHaveText([/The export forgets the files/]);
    await expect(page.getByTestId('filter-owner')).toHaveText(`by ${member.name}`);
    await expect(page.getByRole('button', { name: `Delete the saved filter ${name}` })).toHaveCount(
      0,
    );
  },
);
