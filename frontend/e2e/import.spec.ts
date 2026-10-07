import { keyOf, tenant } from './support/api';
import { asAdmin as test, expect, expectScheme, policyViolations } from './support/fixtures';

/** A repository's ticket file as docs/tickets/README.md writes one: open, in the horizon later. */
function ticketFile(number: number, title: string) {
  const name = `${String(number).padStart(3, '0')}-${title.replaceAll(' ', '-')}.md`;
  const text = [
    '---',
    `id: T${number}`,
    `title: ${title}`,
    'state: filed',
    'severity: medium',
    'security: none',
    'urgency: later',
    'effort: S',
    'opened: 2026-10-01',
    '---',
    '',
    '## Current state',
    '',
    `Found while the import was tried: ${title}.`,
    '',
  ].join('\n');
  return { name, mimeType: 'text/markdown', buffer: Buffer.from(text) };
}

/**
 * The import of a project (docs/adr/0051, docs/adr/0063): a member reads the project with its export
 * and no way to the import; the administrator chooses two ticket files, makes the dry run, reads its
 * report at the job's own address — which a reload keeps —, leaves nothing out, executes after the
 * question, and finds both tickets in the backlog, then exports the project as an archive. The
 * shell's content-security policy refuses nothing on the way.
 */
test(
  'an administrator imports two ticket files into a project and finds them in its backlog',
  { tag: '@smoke' },
  async ({ page, project, member }) => {
    const violations = await policyViolations(page);
    const first = ticketFile(1, 'the export forgets the attachments');
    const second = ticketFile(2, 'the board wants a swimlane per project');

    // A member reads the project: its export, and no way to its import.
    await member.page.goto(`/t/${tenant}/p/${project}/backlog`);
    await expectScheme(member.page);
    await expect(member.page.getByTestId('me')).toBeVisible();
    await expect(member.page.getByTestId('project-name')).toHaveText(`Project ${project}`);
    await expect(member.page.getByRole('button', { name: 'Export the tickets' })).toBeVisible();
    await expect(member.page.getByRole('link', { name: 'Import tickets' })).toHaveCount(0);
    await member.page.goto(`/t/${tenant}/p/${project}/imports`);
    await expect(member.page.getByTestId('import-not-admin')).toBeVisible();

    // The administrator's dry run.
    await page.goto(`/t/${tenant}/p/${project}/backlog`);
    await expectScheme(page);
    await page.getByRole('link', { name: 'Import tickets' }).click();
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}/p/${project}/imports$`));
    await page.getByTestId('import-input').setInputFiles([first, second]);
    await expect(page.getByTestId('import-chosen')).toContainText(first.name);
    await expect(page.getByTestId('import-chosen')).toContainText(second.name);
    await page.getByRole('button', { name: 'Start the dry run' }).click();

    // The report, at the job's own address, which a reload keeps.
    await expect(page).toHaveURL(new RegExp(`/t/${tenant}/p/${project}/imports/[0-9a-f-]{36}$`));
    await page.reload();
    await expect(page.getByTestId('summary-files')).toHaveText('2');
    await expect(page.getByTestId('summary-create')).toHaveText('2');
    await expect(page.getByTestId('summary-conflict')).toHaveText('0');
    await expect(page.getByTestId('summary-error')).toHaveText('0');
    await expect(page.getByTestId('import-blocking')).toHaveCount(0);
    for (const file of [first, second]) {
      await expect(page.getByTestId(`outcome-${file.name}`)).toHaveText('create');
    }
    await expect(page.getByTestId(`file-${first.name}`)).toContainText(
      'the export forgets the attachments',
    );
    await expect(page.getByTestId(`file-${second.name}`)).toContainText(
      'the board wants a swimlane per project',
    );

    // Nothing is left out; the execution asks first.
    await page.getByRole('button', { name: 'Import 2 tickets' }).click();
    const question = page.getByRole('alertdialog', { name: `Import 2 tickets into ${project}?` });
    await expect(question).toContainText('cannot be undone as a whole');
    await question.getByRole('button', { name: 'Import 2 tickets' }).click();
    await expect(
      page.getByRole('heading', { name: `2 tickets imported into ${project}` }),
    ).toBeVisible();

    // Both tickets are in the backlog, with the numbers of their files.
    await page.getByRole('link', { name: 'Open the backlog' }).click();
    const later = page.getByTestId('group-later');
    await expect(later.getByTestId(`row-${keyOf(project, 1)}`)).toContainText(
      'the export forgets the attachments',
    );
    await expect(later.getByTestId(`row-${keyOf(project, 2)}`)).toContainText(
      'the board wants a swimlane per project',
    );

    // The project leaves again as an archive, named as the server names it.
    const download = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Export the tickets' }).click();
    expect((await download).suggestedFilename()).toMatch(
      new RegExp(`^${tenant}-${project}-\\d{8}\\.tar\\.gz$`),
    );
    await expect(page.locator('p-toast')).toContainText(`${project} exported`);

    expect(violations).toEqual([]);
  },
);
