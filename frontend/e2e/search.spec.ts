import { keyOf, otherTenant, Seed, tenant, uniqueKey } from './support/api';
import {
  asAdmin as test,
  expect,
  expectNoTenantShown,
  expectScheme,
  policyViolations,
} from './support/fixtures';
import { baseURL, seedToken } from './support/identities';

/** A word no other ticket of the run holds, so that the hits are this test's alone. */
function uniqueWord(): string {
  const letters = 'abcdefghijklmnopqrstuvwxyz';
  let word = 'quoll';
  while (word.length < 12) {
    word += letters[Math.floor(Math.random() * letters.length)];
  }
  return word;
}

/**
 * The search (docs/adr/0018 D7, docs/adr/0025): the top bar's box, inside a tenant, lists that
 * tenant's hits with their snippets, the found word marked; *Search all your teams* lists a second
 * tenant's beside them, each with its tenant, for the person in both; a hit in a comment opens the
 * ticket scrolled to that comment; the shell's content-security policy refuses nothing on the way.
 */
test(
  'the search box finds a tenant’s hits, then every tenant’s, and a hit opens its comment',
  { tag: '@smoke' },
  async ({ page, seed, project }) => {
    const violations = await policyViolations(page);
    const word = uniqueWord();
    const titled = await seed.file(project, {
      title: `The ${word} export forgets the files`,
      body: `Found while exporting the ${word} of a project with files.`,
    });
    // A long body puts the comments below the fold: the page has to scroll to the one linked.
    const talked = await seed.file(project, {
      title: 'Plan the archive',
      body: Array.from({ length: 60 }, (_, i) => `Paragraph ${i + 1} of the plan.`).join('\n\n'),
    });
    const comment = await seed.comment(project, talked.number, `The ${word} goes into it too.`);
    const elsewhere = await Seed.create(baseURL, seedToken(), otherTenant);
    const other = uniqueKey();
    let found: { number: number };
    try {
      await elsewhere.project(other);
      found = await elsewhere.file(other, { title: `Count the ${word} in the other team` });
    } finally {
      await elsewhere.dispose();
    }
    const titledHit = page.getByTestId(`hit-${keyOf(project, titled.number)}`);
    const talkedHit = page.getByTestId(`hit-${keyOf(project, talked.number)}`);
    const otherHit = page.getByTestId(`hit-${otherTenant}/${other}-${found.number}`);

    // Inside the tenant, the box searches the tenant.
    await page.goto(`/t/${tenant}/p/${project}/backlog`);
    await expectScheme(page);
    const box = page.getByRole('searchbox', { name: 'Search End to end' });
    await box.fill(word);
    await box.press('Enter');
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}/search\\?q=${word}$`));
    await expect(page.getByTestId('search-headline')).toContainText('2 tickets for');
    await expect(titledHit.getByTestId('found-in')).toHaveText('in the ticket');
    await expect(titledHit.getByTestId('snippet').locator('mark')).toHaveText([word]);
    await expect(talkedHit.getByTestId('found-in')).toHaveText('in a comment');
    await expect(talkedHit.getByTestId('snippet').locator('mark')).toHaveText([word]);
    await expect(otherHit).toHaveCount(0);
    await expectNoTenantShown(page);

    // Every tenant of the person: the other tenant's hit beside them, each with its tenant.
    await page.getByRole('link', { name: 'Search all your teams' }).click();
    await expect(page).toHaveURL(new RegExp(`/me/search\\?q=${word}$`));
    await expect(page.getByTestId('search-headline')).toContainText('3 tickets for');
    await expect(otherHit.getByTestId('tenant')).toHaveText('Other end');
    await expect(titledHit.getByTestId('tenant')).toHaveText('End to end');
    await expect(talkedHit.getByTestId('tenant')).toHaveText('End to end');
    await expectNoTenantShown(page);

    // The hit in a comment opens the ticket at that comment.
    await talkedHit.click();
    await expect(page).toHaveURL(
      new RegExp(`/t/${tenant}/tickets/${project}-${talked.number}#comment-${comment.id}$`),
    );
    const linked = page.getByTestId(`comment-${comment.id}`);
    await expect(linked).toContainText(`The ${word} goes into it too.`);
    await expect(linked).toBeInViewport();

    expect(violations).toEqual([]);
  },
);
