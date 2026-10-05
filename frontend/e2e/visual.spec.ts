import { asAdmin as test, expect, expectScheme } from './support/fixtures';
import { visualProject, visualTenant } from './support/visual';

/**
 * A coarse dark-mode comparison (docs/adr/0056 D3): the board of the visual fixture tenant, which
 * the global setup seeds alike for every run, against its picture per browser. It catches what a
 * scheme regression looks like — a light surface, a token that lost its dark value, text that
 * vanished — not a pixel; screenshot.css leaves out what differs by build. Runs in the dark
 * projects only.
 */
test('the board in the dark scheme looks as it did', { tag: '@smoke' }, async ({ page }) => {
  await page.goto(`/t/${visualTenant}/p/${visualProject}/board`);
  await expectScheme(page);
  await expect(page.locator('html')).toHaveClass(/\bapp-dark\b/);
  await expect(page.getByTestId('card-count')).toHaveText('9 on the board');
  await expect(page.getByTestId('cards-review').locator('app-board-card')).toHaveCount(1);
  await expect(page.getByText('Live', { exact: true })).toBeVisible();

  await expect(page).toHaveScreenshot('board.png', { stylePath: `${__dirname}/screenshot.css` });
});
