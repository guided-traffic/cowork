import { keyOf, tenant } from './support/api';
import { asAdmin as test, expect, expectScheme } from './support/fixtures';

/**
 * Filing a ticket and moving it (docs/adr/0056 D3): the dialog of the project's header files it
 * into the horizon `later`, where the backlog shows it; its page offers the moves the matrix allows
 * (docs/adr/0009), and the forward step needs no input.
 */
test(
  'a ticket filed in the backlog is moved forward on its page',
  { tag: '@smoke' },
  async ({ page, seed, project }) => {
    await page.goto(`/t/${tenant}/p/${project}/backlog`);
    await expectScheme(page);

    await page.getByTestId('new-ticket').click();
    await page.getByTestId('new-title').fill('The export forgets the attachments');
    await page.getByTestId('new-body').fill('Found while exporting a project with files.');
    await page.getByTestId('new-save').click();

    const key = keyOf(project, 1);
    const row = page.getByTestId('group-later').getByTestId(`row-${key}`);
    await expect(row).toContainText('The export forgets the attachments');
    await expect(row.locator('[data-state]')).toHaveAttribute('data-state', 'filed');

    await row.click();
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}/tickets/${project}-1$`));
    await expect(page.getByTestId('ticket-title')).toHaveText('The export forgets the attachments');
    await page.getByTestId('move-analysed').click();
    // The next forward step is offered once the move went through.
    await expect(page.getByTestId('move-decided')).toBeVisible();
    expect((await seed.ticket(project, 1)).state).toBe('analysed');

    await page.getByRole('link', { name: project, exact: true }).click();
    await page.getByTestId('tab-backlog').click();
    await expect(row.locator('[data-state]')).toHaveAttribute('data-state', 'analysed');
  },
);
