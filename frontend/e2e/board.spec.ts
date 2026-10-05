import { keyOf, tenant } from './support/api';
import { asAdmin as test, drag, expect, expectScheme } from './support/fixtures';

/**
 * The project board (docs/adr/0018 D1): a project's address opens its board, and a card dragged to
 * another column is the transition of docs/adr/0009 that the column's state is.
 */
test('a project opens on its board', { tag: '@smoke' }, async ({ page, seed, project }) => {
  await seed.file(project, { title: 'On the board', horizon: 'now' });

  await page.goto(`/t/${tenant}/p/${project}`);
  await expect(page).toHaveURL(new RegExp(`/t/${tenant}/p/${project}/board$`));
  await expect(page.getByTestId('tab-board')).toHaveAttribute('aria-current', 'page');
  await expectScheme(page);
  await expect(
    page.getByTestId('cards-refinement').getByTestId(`card-${keyOf(project, 1)}`),
  ).toContainText('On the board');

  // The sidebar's link to the project opens the board as well.
  await page.getByTestId('tab-backlog').click();
  await page.getByTestId(`nav-project-${project}`).click();
  await expect(page).toHaveURL(new RegExp(`/p/${project}/board$`));
});

test(
  'a card dragged from Refinement to Ready is decided',
  { tag: '@smoke' },
  async ({ page, seed, project }) => {
    const filed = await seed.file(project, { title: 'Ready to be decided', horizon: 'now' });
    await seed.transition(project, filed.number, 'filed', 'analysed');
    const key = keyOf(project, filed.number);

    await page.goto(`/t/${tenant}/p/${project}/board`);
    await expectScheme(page);
    const card = page.getByTestId(`card-${key}`);
    await expect(page.getByTestId('cards-refinement').getByTestId(`card-${key}`)).toBeVisible();
    await expect(page.getByTestId('count-ready')).toHaveText(/^\s*0/);

    await drag(page, card.getByTestId('card-title'), page.getByTestId('cards-ready'));

    await expect(page.getByTestId('cards-ready').getByTestId(`card-${key}`)).toBeVisible();
    await expect(page.getByTestId('count-ready')).toHaveText(/^\s*1/);
    await expect(page.getByTestId('status')).toHaveText(
      `${project}-${filed.number} moved to Ready`,
    );
    await expect.poll(async () => (await seed.ticket(project, filed.number)).state).toBe('decided');

    // The move is the server's, not only the page's: a new document shows it where it went.
    await page.reload();
    await expect(page.getByTestId('cards-ready').getByTestId(`card-${key}`)).toBeVisible();
  },
);
