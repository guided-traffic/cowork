import { keyOf, tenant, uniqueKey } from './support/api';
import { asAdmin as test, expect, expectScheme } from './support/fixtures';

/**
 * The tenant's ticket list (docs/adr/0018 D5, docs/adr/0023 D4): the tickets of every project, the
 * project beside each key, filtered by the address — a filtered list is a link that comes back as
 * it was —, live through the event stream, and the bar writing the address.
 */
test(
  'lists the tickets of two projects, filtered by the address and live',
  { tag: '@smoke' },
  async ({ page, seed, project }) => {
    const other = uniqueKey();
    await seed.project(other);
    const urgent = await seed.file(project, {
      title: 'The export forgets the files',
      severity: 'high',
    });
    const calm = await seed.file(other, { title: 'A typo in the footer', severity: 'low' });
    const urgentRow = page.getByTestId(`row-${keyOf(project, urgent.number)}`);
    const calmRow = page.getByTestId(`row-${keyOf(other, calm.number)}`);

    // The two projects of this test, whatever else the tenant holds.
    const both = `/t/${tenant}/tickets?project=${project}&project=${other}`;
    await page.goto(both);
    await expectScheme(page);
    // The list is a tab of the team's dashboard (docs/adr/0018 D6 as amended 2026-10-10).
    await expect(page.getByTestId('team-tab-tickets')).toHaveAttribute('aria-current', 'page');
    await expect(urgentRow.getByTestId('row-project')).toHaveText(`Project ${project}`);
    await expect(calmRow.getByTestId('row-project')).toHaveText(`Project ${other}`);
    await expect(page.getByTestId('tickets-total')).toHaveText('2 tickets');

    // Narrowed by the address, and the same after a reload.
    await page.goto(`${both}&severity=high`);
    await expect(urgentRow).toBeVisible();
    await expect(calmRow).toHaveCount(0);
    await page.reload();
    await expect(urgentRow).toBeVisible();
    await expect(calmRow).toHaveCount(0);

    // Live: a ticket filed elsewhere, once the stream is open, shows without a reload, newest first.
    await expect(page.getByTestId('live-indicator')).toHaveAttribute('data-status', 'live');
    const filed = await seed.file(other, { title: 'Filed meanwhile', severity: 'high' });
    const filedRow = page.getByTestId(`row-${keyOf(other, filed.number)}`);
    await expect(filedRow).toBeVisible();
    await expect(page.locator('tbody tr.row').first()).toHaveAttribute(
      'data-testid',
      `row-${keyOf(other, filed.number)}`,
    );

    // A row opens its ticket; its project's name beside it is a link of its own.
    await urgentRow.getByTestId('row-title').click();
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}/tickets/${project}-${urgent.number}$`));

    // The bar writes the address: clearing the filters leaves the list unfiltered.
    await page.goto(`${both}&severity=high`);
    await page.getByTestId('tickets-clear').click();
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}/tickets$`));
  },
);
