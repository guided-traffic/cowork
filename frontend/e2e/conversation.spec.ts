import { Page } from '@playwright/test';
import { tenant } from './support/api';
import { asAdmin as test, expect, expectScheme } from './support/fixtures';

/** A ticket's page, open, its event stream live. */
async function openTicket(page: Page, short: string): Promise<void> {
  await page.goto(`/t/${tenant}/tickets/${short}`);
  await expectScheme(page);
  await expect(page.getByTestId('live-indicator')).toHaveAttribute('data-status', 'live');
}

/**
 * The conversation's writes reach a second browser (docs/adr/0054 D2): the administrator writes a
 * comment, edits it and withdraws it (docs/adr/0015 D3), edits the question they asked
 * (docs/adr/0011 D2), and links another ticket as blocking this one (docs/adr/0012 D6); a member's
 * page of the ticket, open all along, shows each through the event stream — the comment's earlier
 * text on request, the withdrawn comment without its text, the new question, the prerequisite tree
 * with the blocker in it.
 */
test(
  'a comment edited and withdrawn, a question edited and a new blocker reach a second browser',
  { tag: '@smoke' },
  async ({ page, seed, project, member }) => {
    const watched = await seed.file(project, { title: 'Ship the export' });
    const blocker = await seed.file(project, { title: 'Fix the file names' });
    const question = await seed.ask(project, watched.number, 'Which format comes first?');
    const short = `${project}-${watched.number}`;
    const blockerShort = `${project}-${blocker.number}`;

    await openTicket(member.page, short);
    await expect(member.page.getByTestId('tree-empty')).toHaveText('Nothing blocks this ticket.');
    await openTicket(page, short);

    // A comment, seen by the member.
    await page.getByRole('textbox', { name: 'Comment', exact: true }).fill('CSV goes first.');
    await page.getByRole('button', { name: 'Comment', exact: true }).click();
    const written = page.getByRole('article').filter({ hasText: 'CSV goes first.' });
    const id = await written.getAttribute('data-testid');
    const comment = page.getByTestId(id ?? 'no comment');
    const seen = member.page.getByTestId(id ?? 'no comment');
    await expect(seen).toContainText('CSV goes first.');

    // Edited: the member sees the new text, and the earlier one on request.
    await comment.getByRole('button', { name: 'Edit' }).click();
    await comment.getByRole('textbox', { name: 'Comment, in Markdown' }).fill('JSON goes first.');
    await comment.getByRole('button', { name: 'Save' }).click();
    await expect(comment).toContainText('JSON goes first.');
    await expect(seen).toContainText('JSON goes first.');
    await seen.getByRole('button', { name: 'edited' }).click();
    await expect(seen.getByRole('listitem')).toContainText('CSV goes first.');

    // Withdrawn after the question: the member sees the entry without its text.
    await comment.getByRole('button', { name: 'Withdraw' }).click();
    await page
      .getByRole('alertdialog', { name: 'Withdraw the comment' })
      .getByRole('button', { name: 'Withdraw' })
      .click();
    await expect(seen).toContainText('withdrawn');
    await expect(seen).not.toContainText('goes first');

    // The question's text edited by its asker.
    const asked = page.getByTestId(`question-${question.number}`);
    await asked.getByRole('button', { name: 'Edit the question' }).click();
    await asked.getByRole('textbox', { name: 'Question' }).fill('Which format ships first?');
    await asked.getByRole('button', { name: 'Save' }).click();
    await expect(member.page.getByTestId(`question-${question.number}`)).toContainText(
      'Which format ships first?',
    );

    // A link from the blocker's page: the member's tree of this ticket holds it.
    await openTicket(page, blockerShort);
    await page.getByRole('combobox', { name: 'Link type' }).click();
    await page.getByRole('option', { name: 'blocks', exact: true }).click();
    await page.getByRole('combobox', { name: 'The other ticket' }).fill(short);
    await page.getByRole('button', { name: 'Add the link' }).click();
    const tree = member.page.getByTestId('tree');
    await expect(tree.getByRole('link', { name: blockerShort, exact: true })).toBeVisible();
    await expect(tree).toContainText('Fix the file names');
    await expect(member.page.getByTestId('tree-open')).toHaveText('1 open');
  },
);
