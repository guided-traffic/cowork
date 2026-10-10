import { Page } from '@playwright/test';
import { Seed, Session, sessionContext, uniqueKey } from './support/api';
import { asAdmin as test, expect, expectScheme, newAccount } from './support/fixtures';
import { adminState, baseURL, seedToken } from './support/identities';

/** The headline and the figure of the open tickets' tile. */
async function expectOpen(page: Page, headline: string, figure: string): Promise<void> {
  await expect(page.getByTestId('open-count')).toHaveText(headline);
  await expect(
    page.getByRole('region', { name: 'Open tickets' }).getByRole('paragraph'),
  ).toHaveText(figure);
}

/**
 * The tenant's dashboard (docs/adr/0018 D6) with two identities, in a tenant of the test's own, so
 * that the tiles count this test's tickets alone: the administrator files a confidential ticket
 * and one in a project restricted away from the member; both open the tenant's front page, and the
 * member's tiles leave out what the member cannot see; a filter the member chooses lands in the
 * address and survives a reload; a ticket the member moves moves the administrator's tile within a
 * few seconds, and one the administrator deletes leaves both dashboards.
 */
test(
  'the dashboard counts what each identity sees, keeps its filter and follows the tenant live',
  { tag: '@smoke' },
  async ({ page, browser }) => {
    const slug = `e2e-${uniqueKey('d').toLowerCase()}`;
    const admin = await sessionContext(baseURL, adminState);
    try {
      const session = new Session(admin);
      await session.ensureTeam(slug, `Dashboard ${slug}`);
      const seed = await Seed.create(baseURL, seedToken(), slug);
      try {
        await seed.project('OPEN', 'Open to all');
        await seed.project('HIDDEN', 'Restricted');
        await session.restrict(slug, 'HIDDEN');
        await seed.file('OPEN', { title: 'Counted by both' });
        const secret = await seed.file('OPEN', { title: 'Confidential', severity: 'high' });
        await seed.confidential('OPEN', secret.number);
        await seed.file('HIDDEN', { title: 'Restricted away', severity: 'high' });
      } finally {
        await seed.dispose();
      }
    } finally {
      await admin.dispose();
    }
    const member = await newAccount(browser, 'member', slug);
    const recent = (who: Page, project: string, number: number) =>
      who.getByTestId(`recent-${slug}/${project}-${number}`);

    try {
      // Both open the tenant's front page; the member's tiles leave out what they cannot see.
      await page.goto(`/t/${slug}`);
      await expectScheme(page);
      await expectOpen(page, '3 open tickets across 2 projects', '3');
      await expect(page.getByTestId('severity-high')).toHaveText(/2$/);
      await expect(recent(page, 'OPEN', 2)).toBeVisible();
      await expect(recent(page, 'HIDDEN', 1)).toBeVisible();

      await member.page.goto(`/t/${slug}`);
      await expectScheme(member.page);
      await expectOpen(member.page, '1 open ticket across 1 project', '1');
      await expect(member.page.getByTestId('severity-high')).toHaveText(/0$/);
      await expect(recent(member.page, 'OPEN', 1)).toBeVisible();
      await expect(recent(member.page, 'OPEN', 2)).toHaveCount(0);
      await expect(member.page.getByTestId('project-HIDDEN')).toHaveCount(0);

      // A filter chosen in the page lands in the address and survives a reload.
      await member.page.getByRole('combobox', { name: 'Projects' }).click();
      await member.page.getByRole('option', { name: 'OPEN · Open to all' }).click();
      await member.page.keyboard.press('Escape');
      await expect(member.page).toHaveURL(new RegExp(`/t/${slug}\\?project=OPEN$`));
      await member.page.reload();
      await expect(member.page).toHaveURL(new RegExp(`/t/${slug}\\?project=OPEN$`));
      await expect(member.page.getByRole('combobox', { name: 'Projects' })).toContainText('OPEN');
      await expectOpen(member.page, '1 open ticket across 1 project', '1');

      // The member moves a ticket on its page; the administrator's open dashboard follows.
      await expect(page.getByTestId('live-indicator')).toHaveAttribute('data-status', 'live');
      const ticketTab = await member.page.context().newPage();
      await ticketTab.goto(`/t/${slug}/tickets/OPEN-1`);
      await ticketTab.getByTestId('move-analysed').click();
      await expect(ticketTab.getByTestId('move-decided')).toBeVisible();
      const states = page.getByRole('region', { name: 'Open tickets' });
      await expect(states.getByTestId('state-analysed')).toHaveText(/1$/, { timeout: 5_000 });
      await expect(states.getByTestId('state-filed')).toHaveText(/2$/);
      await ticketTab.close();

      // The administrator deletes it: it leaves both dashboards.
      await expect(member.page.getByTestId('live-indicator')).toHaveAttribute(
        'data-status',
        'live',
      );
      const deleting = await Seed.create(baseURL, seedToken(), slug);
      await deleting.remove('OPEN', 1).finally(() => deleting.dispose());
      await expectOpen(page, '2 open tickets across 2 projects', '2');
      await expect(recent(page, 'OPEN', 1)).toHaveCount(0);
      await expectOpen(member.page, '0 open tickets across 1 project', '0');
      await expect(member.page.getByText('Nothing open', { exact: true }).first()).toBeVisible();
    } finally {
      await member.page.context().close();
    }
  },
);
