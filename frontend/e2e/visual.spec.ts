import { asAdmin as test, expect, expectScheme } from './support/fixtures';
import { visualProject, visualTenant } from './support/visual';

/**
 * Coarse comparisons of the scheme (docs/adr/0056 D3): pages of the visual fixture tenant, which the
 * global setup seeds alike for every run, against their picture per browser and scheme. They catch
 * what a scheme regression looks like — a light surface, a token that lost its value, text that
 * vanished — not a pixel; screenshot.css leaves out what differs by build or by the day of the run.
 * The board runs in the dark projects only (`@dark`), the dashboard in all four.
 */
test(
  'the board in the dark scheme looks as it did',
  { tag: ['@smoke', '@dark'] },
  async ({ page }) => {
    await page.goto(`/t/${visualTenant}/p/${visualProject}/board`);
    await expectScheme(page);
    await expect(page.locator('html')).toHaveClass(/\bapp-dark\b/);
    await expect(page.getByTestId('card-count')).toHaveText('9 on the board');
    await expect(page.getByTestId('cards-review').locator('app-board-card')).toHaveCount(1);
    await expect(page.getByText('Live', { exact: true })).toBeVisible();

    await expect(page).toHaveScreenshot('board.png', { stylePath: `${__dirname}/screenshot.css` });
  },
);

test('the dashboard looks as it did in either scheme', { tag: '@smoke' }, async ({ page }) => {
  await page.goto(`/t/${visualTenant}`);
  await expectScheme(page);
  await expect(page.getByTestId('open-count')).toHaveText('9 open tickets across 1 project');
  await expect(page.getByTestId(`project-${visualProject}`)).toBeVisible();
  await expect(page.getByText('Live', { exact: true })).toBeVisible();

  await expect(page).toHaveScreenshot('dashboard.png', {
    stylePath: `${__dirname}/screenshot.css`,
  });
});
