import { keyOf, tenant } from './support/api';
import { asAdmin as test, expect, expectNoTenantShown, expectScheme } from './support/fixtures';

/**
 * The start page is "next for me" (docs/adr/0018 D3, docs/adr/0023 D4 as amended 2026-10-05): a
 * person of one tenant lands on it at `/` and finds the ticket assigned to them there, beside its
 * tenant; the top bar names the only tenant, and its name leads to the tenant's front page.
 */
test(
  'a person with one tenant lands on next for me and reaches the tenant by its name in the top bar',
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

    // One tenant: no switcher, its name in the top bar, a link to it.
    const banner = page.getByRole('banner');
    await expect(banner.getByRole('combobox', { name: 'Team' })).toHaveCount(0);
    await banner.getByRole('link', { name: 'End to end', exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}$`));
    await expect(page.getByRole('heading', { name: 'End to end', level: 1 })).toBeVisible();
    await expect(page.getByTestId('nav-overview')).toHaveClass(/\bactive\b/);
    await expect(page.getByTestId('tile-state')).toBeVisible();
    await expectNoTenantShown(page);
  },
);
