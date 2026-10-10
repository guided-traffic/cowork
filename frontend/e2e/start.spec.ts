import { keyOf, tenant } from './support/api';
import { asAdmin as test, expect, expectNoTenantShown, expectScheme } from './support/fixtures';

/**
 * The start page is "next for me" (docs/adr/0018 D3, docs/adr/0023 D4 as amended 2026-10-05): a
 * person of one team lands on it at `/` and finds the ticket assigned to them there, beside its
 * team; the top bar names no team (D4 as amended 2026-10-10), the sidebar does, and the team's name
 * there leads to its dashboard.
 */
test(
  'a person with one team lands on next for me and reaches the team by its name in the sidebar',
  { tag: '@smoke' },
  async ({ seed, project, member }) => {
    // The highest score a ticket without stakes has: on the list's first page whatever else the
    // tenant holds open.
    const filed = await seed.file(project, {
      title: 'Pick this up next',
      severity: 'critical',
      horizon: 'now',
      assignee: member.id,
    });
    const { page } = member;

    await page.goto('/');
    await expectScheme(page);
    await expect(page.getByRole('heading', { name: 'Next for me', level: 1 })).toBeVisible();
    await expect(page).toHaveURL(/^https:\/\/[^/]+\/$/);
    const row = page.getByTestId(`next-${keyOf(project, filed.number)}`);
    await expect(row).toContainText('Pick this up next');
    await expect(row.getByTestId('tenant')).toHaveText('End to end');
    await expect(row.getByTestId('whose')).toHaveText('yours');
    await expectNoTenantShown(page);

    // No switcher and no team in the top bar; the team's name in the sidebar, a link to it.
    const banner = page.getByRole('banner');
    await expect(banner.getByRole('combobox')).toHaveCount(0);
    await expect(banner.getByText('End to end')).toHaveCount(0);
    await page
      .getByRole('navigation', { name: 'Main' })
      .getByRole('link', { name: 'End to end', exact: true })
      .click();
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}$`));
    await expect(page.getByRole('heading', { name: 'End to end', level: 1 })).toBeVisible();
    await expect(page.getByTestId(`nav-team-${tenant}`)).toHaveAttribute('aria-current', 'page');
    await expect(page.getByTestId('team-tab-overview')).toHaveAttribute('aria-current', 'page');
    await expect(page.getByTestId('tile-state')).toBeVisible();
    await expectNoTenantShown(page);
  },
);
