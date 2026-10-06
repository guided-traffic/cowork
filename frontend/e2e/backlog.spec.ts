import { Page } from '@playwright/test';
import { keyOf, tenant } from './support/api';
import { asAdmin as test, drag, expect, expectScheme } from './support/fixtures';

/**
 * The backlog (docs/adr/0018 D1, docs/adr/0014): a table grouped by horizon whose rows are dragged
 * by their handle — within a group a move in the rank, into another group the horizon set by a
 * person (docs/adr/0010 D3) and the place there.
 */
const rows = (page: Page, horizon: string) =>
  page.getByTestId(`group-${horizon}`).locator(`tr[data-testid^="row-${tenant}/"]`);

test(
  'a row dragged to the top of its horizon moves first in the rank',
  { tag: '@smoke' },
  async ({ page, seed, project }) => {
    for (const title of ['Alpha', 'Bravo', 'Charlie']) {
      await seed.file(project, { title });
    }
    await page.goto(`/t/${tenant}/p/${project}/backlog`);
    await expectScheme(page);
    await expect(rows(page, 'later')).toHaveText([/Alpha/, /Bravo/, /Charlie/]);

    const charlie = page.getByTestId(`row-${keyOf(project, 3)}`);
    const alpha = page.getByTestId(`row-${keyOf(project, 1)}`);
    await drag(page, charlie.getByTestId('grip'), alpha, { x: 0.5, y: 0.2 });

    await expect(rows(page, 'later')).toHaveText([/Charlie/, /Alpha/, /Bravo/]);
    await expect(page.getByTestId('status')).toHaveText(
      `${project}-3 moved to later, place 1 of 3`,
    );
    await expect
      .poll(async () => (await seed.horizon(project, 'later')).map((ticket) => ticket.title))
      .toEqual(['Charlie', 'Alpha', 'Bravo']);
  },
);

test(
  'a row dragged into another horizon takes that horizon',
  { tag: '@smoke' },
  async ({ page, seed, project }) => {
    for (const title of ['Alpha', 'Bravo']) {
      await seed.file(project, { title });
    }
    await page.goto(`/t/${tenant}/p/${project}/backlog`);
    await expectScheme(page);
    await expect(rows(page, 'next')).toHaveCount(0);

    const bravo = page.getByTestId(`row-${keyOf(project, 2)}`);
    await drag(page, bravo.getByTestId('grip'), page.getByTestId('empty-next'));

    await expect(rows(page, 'next')).toHaveText([/Bravo/]);
    await expect(rows(page, 'later')).toHaveText([/Alpha/]);
    await expect.poll(async () => (await seed.ticket(project, 2)).horizon).toBe('next');
    // The reason a person may give for the horizon is offered in the row, and may be left out.
    const reason = bravo.getByTestId('reason');
    await expect(reason).toBeVisible();
    await reason.press('Escape');
    await expect(reason).toBeHidden();
  },
);

/**
 * The score warns, the rank decides (docs/adr/0014 D1–D3): a ticket the rank puts below tickets of a
 * lower score is marked among its siblings, and *Sort by score* — after its question — orders the
 * horizon by the score as one act, and the marker is gone.
 */
test(
  'the score marks a ticket the rank puts too low, and the sort by score puts it first',
  { tag: '@smoke' },
  async ({ page, seed, project }) => {
    await seed.file(project, { title: 'Alpha', severity: 'low' });
    await seed.file(project, { title: 'Bravo', severity: 'low' });
    await seed.file(project, { title: 'Charlie', severity: 'high' });
    await page.goto(`/t/${tenant}/p/${project}/backlog`);
    await expectScheme(page);
    await expect(rows(page, 'later')).toHaveText([/Alpha/, /Bravo/, /Charlie/]);

    const marks = page.locator('[data-testid^="score-mark-"]');
    const mark = page.getByTestId(`score-mark-${keyOf(project, 3)}`);
    await expect(marks).toHaveCount(1);
    await expect(mark).toHaveAttribute('data-mark', 'higher');
    await expect(mark).toContainText('says higher than the rank puts it');

    await page.getByRole('button', { name: 'Sort by score' }).click();
    const ask = page.getByRole('alertdialog', { name: `Sort ${project} by score?` });
    await ask.getByRole('button', { name: 'Sort by score' }).click();
    await expect(ask).toBeHidden();

    await expect(rows(page, 'later')).toHaveText([/Charlie/, /Alpha/, /Bravo/]);
    await expect(marks).toHaveCount(0);
    await expect(page.getByTestId('status')).toHaveText('Sorted by score: 3 tickets moved');
    await expect
      .poll(async () => (await seed.horizon(project, 'later')).map((ticket) => ticket.title))
      .toEqual(['Charlie', 'Alpha', 'Bravo']);
  },
);
