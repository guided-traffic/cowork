import { keyOf, tenant, uniqueKey } from './support/api';
import { asAdmin as test, drag, expect, expectScheme } from './support/fixtures';

/**
 * The tenant's board (docs/adr/0018 D4): a swimlane per project with the project board's columns
 * over the same tickets. A card dragged to another column of its swimlane is the transition of
 * docs/adr/0009 its column's state is; one dragged to another swimlane is refused — the swimlane
 * under it says no, a toast says why once it is let go, and the ticket stays where it was.
 */
test(
  'drags a card across columns and is refused across rows',
  { tag: '@smoke' },
  async ({ page, seed, project }) => {
    const other = uniqueKey();
    await seed.project(other);
    const filed = await seed.file(project, { title: 'Across the team', horizon: 'now' });
    await seed.transition(project, filed.number, 'filed', 'analysed');
    const neighbour = await seed.file(other, { title: 'In the other row', horizon: 'now' });
    const key = keyOf(project, filed.number);

    // The filter of the address shows the two swimlanes of this test, whatever else the tenant holds.
    await page.goto(`/t/${tenant}/board?project=${project}&project=${other}`);
    await expectScheme(page);
    const own = page.getByTestId(`lane-${project}`);
    const elsewhere = page.getByTestId(`lane-${other}`);
    await expect(own.getByTestId('cards-refinement').getByTestId(`card-${key}`)).toBeVisible();
    await expect(
      elsewhere
        .getByTestId('cards-refinement')
        .getByTestId(`card-${keyOf(other, neighbour.number)}`),
    ).toBeVisible();

    // Across the columns of its swimlane: the transition analysed → decided.
    await drag(
      page,
      own.getByTestId(`card-${key}`).getByTestId('card-title'),
      own.getByTestId('cards-ready'),
    );
    await expect(own.getByTestId('cards-ready').getByTestId(`card-${key}`)).toBeVisible();
    await expect(page.getByTestId('status')).toHaveText(
      `${project}-${filed.number} moved to Ready`,
    );
    await expect.poll(async () => (await seed.ticket(project, filed.number)).state).toBe('decided');

    // Across the swimlanes: the other row says no while the card is over it.
    const target = elsewhere.getByTestId('cards-refinement');
    await target.scrollIntoViewIfNeeded();
    const handle = own.getByTestId(`card-${key}`).getByTestId('card-title');
    await handle.scrollIntoViewIfNeeded();
    const from = await handle.boundingBox();
    const to = await target.boundingBox();
    if (!from || !to) {
      throw new Error('the card or the other swimlane has no box');
    }
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    await page.mouse.move(from.x + from.width / 2 + 4, from.y + from.height / 2 + 8, { steps: 4 });
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 20 });
    await expect(elsewhere).toHaveClass(/\brefusing\b/);
    await expect(elsewhere.getByTestId('lane-refusal')).toHaveText(
      'A ticket never changes project on a board',
    );

    // Let go there: refused, and a toast says why; nothing moved, here or in the API.
    await page.mouse.up();
    await expect(
      page.getByText(
        `A ticket never changes project on a board: ${project}-${filed.number} stays in ${project}.`,
      ),
    ).toBeVisible();
    await expect(elsewhere.getByTestId('lane-refusal')).toHaveCount(0);
    await expect(own.getByTestId('cards-ready').getByTestId(`card-${key}`)).toBeVisible();
    await expect(elsewhere.getByTestId(`card-${key}`)).toHaveCount(0);
    const after = await seed.ticket(project, filed.number);
    expect(after.state).toBe('decided');
    expect(after.project).toBe(project);
  },
);
