import { otherTenant, Seed, Session, sessionContext, tenant, uniqueKey } from './support/api';
import {
  asAdmin as test,
  expect,
  expectNoTenantShown,
  expectScheme,
  newAccount,
  Person,
} from './support/fixtures';
import { adminState, baseURL, seedToken } from './support/identities';

/** A word no other ticket of the run holds, so that the search finds this test's ticket alone. */
function uniqueWord(): string {
  const letters = 'abcdefghijklmnopqrstuvwxyz';
  let word = 'wombat';
  while (word.length < 13) {
    word += letters[Math.floor(Math.random() * letters.length)];
  }
  return word;
}

/**
 * A ticket's parent in another team (docs/adr/0005 D3, docs/adr/0008 D2), two identities each in a
 * browser context of its own (docs/adr/0056 D2):
 * 1. a member of the fixture team who is a member of the second team as well opens a ticket of the
 *    second team and picks a ticket of the fixture team as its parent through the chooser's search
 *    across their teams; the page shows the parent by its head with the way to open it, and the
 *    API holds the parent's canonical key;
 * 2. a member of the second team alone sees that parent by its team and head — key, title, type,
 *    state —, unlinked, and a confidential parent of another ticket as `<team> [Confidential]`,
 *    nothing of its title (docs/adr/0065 D5); neither page says "tenant".
 */
test(
  'a member of two teams picks a parent of the other team through the search, and a member of one team sees its head, unlinked, and a confidential parent as its placeholder',
  { tag: '@smoke' },
  async ({ browser, seed, project }) => {
    const word = uniqueWord();
    const parent = await seed.file(project, {
      title: `Plan the ${word} release`,
      type: 'feature',
    });
    const secret = await seed.file(project, { title: `The ${word} secret plan` });
    await seed.confidential(project, secret.number);

    // The second team's project, its child and the child of the confidential ticket, which the
    // administrator relates through the API: an administrator of both teams reads both ends.
    const other = await Seed.create(baseURL, seedToken(), otherTenant);
    const otherProject = uniqueKey();
    let child: { number: number };
    let secretChild: { number: number };
    try {
      await other.project(otherProject);
      child = await other.file(otherProject, { title: 'Ship the release notes' });
      secretChild = await other.file(otherProject, { title: 'Prepare the secret part' });
      await other.setParent(
        otherProject,
        secretChild.number,
        `${tenant}/${project}-${secret.number}`,
      );
    } finally {
      await other.dispose();
    }
    const parentKey = `${tenant}/${project}-${parent.number}`;
    const parentShort = `${project}-${parent.number}`;

    // Identity 1: a member of the fixture team, granted a role in the second team as well.
    const both: Person = await newAccount(browser, 'member', tenant);
    const admin = await sessionContext(baseURL, adminState);
    try {
      await new Session(admin).addMember(otherTenant, both.username, 'member');
    } finally {
      await admin.dispose();
    }
    // Identity 2: a member of the second team alone.
    const alone: Person = await newAccount(browser, 'member', otherTenant);
    try {
      const page = both.page;
      await page.goto(`/t/${otherTenant}/tickets/${otherProject}-${child.number}`);
      await expectScheme(page);
      await expect(page.getByTestId('ticket-title')).toHaveText('Ship the release notes');

      // The chooser searches every team of the person once they type.
      await page.getByTestId('parent-picker').click();
      await page.getByRole('searchbox', { name: 'Search the tickets of your teams' }).fill(word);
      const offered = page.getByRole('option', {
        name: `End to end · ${parentShort} Plan the ${word} release`,
      });
      await expect(offered).toBeVisible();
      await expect(page.getByRole('option', { name: /secret plan/ })).toHaveCount(0);
      await offered.click();

      // The page shows the parent by its head, and the way to open it, which the member reads.
      const chosen = page.getByTestId('parent-chosen');
      await expect(chosen).toContainText('End to end');
      await expect(chosen).toContainText(parentShort);
      await expect(chosen).toContainText(`Plan the ${word} release`);
      await expect(page.getByRole('link', { name: 'Open the parent' })).toHaveAttribute(
        'href',
        `/t/${tenant}/tickets/${parentShort}`,
      );
      await expect
        .poll(async () => (await seed.ticket(project, parent.number)).progress_derived)
        .toBe(true);
      const reread = await Seed.create(baseURL, seedToken(), otherTenant);
      try {
        expect((await reread.ticket(otherProject, child.number)).parent).toBe(parentKey);
      } finally {
        await reread.dispose();
      }
      await expectNoTenantShown(page);

      // Identity 2 sees the parent by its team and head, without a way to open it.
      const outsider = alone.page;
      await outsider.goto(`/t/${otherTenant}/tickets/${otherProject}-${child.number}`);
      await expectScheme(outsider);
      const head = outsider.getByTestId('parent-chosen');
      await expect(head).toContainText('End to end');
      await expect(head).toContainText(parentShort);
      await expect(head).toContainText(`Plan the ${word} release`);
      await expect(head.locator('[data-state]')).toHaveAttribute('data-state', 'filed');
      await expect(outsider.getByTestId('parent-open')).toHaveCount(0);
      await expect(outsider.locator(`a[href="/t/${tenant}/tickets/${parentShort}"]`)).toHaveCount(
        0,
      );
      await expectNoTenantShown(outsider);

      // And a confidential parent as its placeholder, nothing of its title.
      await outsider.goto(`/t/${otherTenant}/tickets/${otherProject}-${secretChild.number}`);
      await expect(outsider.getByTestId('ticket-title')).toHaveText('Prepare the secret part');
      await expect(outsider.getByTestId('parent-chosen')).toHaveText('End to end [Confidential]');
      await expect(outsider.getByTestId('parent-open')).toHaveCount(0);
      await expect(outsider.locator('body')).not.toContainText('secret plan');
      await expectNoTenantShown(outsider);
    } finally {
      await both.page.context().close();
      await alone.page.context().close();
    }
  },
);

/**
 * A relation another team keeps onto a ticket is removed from the ticket's own side
 * (docs/adr/0008 D2, docs/adr/0012 D2 as amended again 2026-10-10): a member of the fixture team who
 * holds no role in the second team opens a ticket of the fixture team that a ticket of the second
 * team names as its parent and that another ticket of the second team blocks; the page shows both
 * by their heads, unlinked, and the member removes the child and the incoming link, which leave the
 * page at once and the API.
 */
test(
  'a member of one team removes a child and an incoming blocks link that another team keeps onto its ticket',
  { tag: '@smoke' },
  async ({ browser, seed, project }) => {
    const parent = await seed.file(project, { title: 'Coordinate the launch' });
    const parentKey = `${tenant}/${project}-${parent.number}`;
    const other = await Seed.create(baseURL, seedToken(), otherTenant);
    const otherProject = uniqueKey();
    let child: { number: number };
    let blocker: { number: number };
    try {
      await other.project(otherProject);
      child = await other.file(otherProject, { title: 'Translate the launch notes' });
      blocker = await other.file(otherProject, { title: 'Clear the launch with legal' });
      await other.setParent(otherProject, child.number, parentKey);
      await other.link(otherProject, blocker.number, 'blocks', parentKey);
    } finally {
      await other.dispose();
    }

    const member: Person = await newAccount(browser, 'member', tenant);
    try {
      const page = member.page;
      await page.goto(`/t/${tenant}/tickets/${project}-${parent.number}`);
      await expectScheme(page);
      await expect(page.getByTestId('ticket-title')).toHaveText('Coordinate the launch');

      // The child of the other team, by its head: the member holds no role there.
      const children = page.getByTestId('children');
      const kept = children.getByTestId('child-0');
      await expect(kept).toContainText('Other end');
      await expect(kept).toContainText(`${otherProject}-${child.number}`);
      await expect(kept).toContainText('Translate the launch notes');
      await expect(kept.locator('a')).toHaveCount(0);
      await kept.getByRole('button', { name: 'Remove this child' }).click();
      await expect(children).toContainText('No children.');

      // The link the other team keeps onto the ticket, blocked by its ticket.
      const blockedBy = page.getByTestId(/^link-/).filter({ hasText: 'blocked by' });
      await expect(blockedBy).toContainText(`${otherProject}-${blocker.number}`);
      await expect(blockedBy).toContainText('Clear the launch with legal');
      await blockedBy.getByRole('button', { name: 'Remove this link' }).click();
      await expect(page.getByText('No links.')).toBeVisible();
      await expectNoTenantShown(page);
    } finally {
      await member.page.context().close();
    }

    // Both are gone in the API as well.
    const reread = await Seed.create(baseURL, seedToken(), otherTenant);
    try {
      expect((await reread.ticket(otherProject, child.number)).parent).toBeNull();
    } finally {
      await reread.dispose();
    }
    expect((await seed.relations(project, parent.number, 'child')).items).toEqual([]);
    expect((await seed.relations(project, parent.number, 'link')).items).toEqual([]);
  },
);
