import { tenant } from './support/api';
import { asAdmin as test, expect, expectScheme, policyViolations } from './support/fixtures';

/** A PNG of one pixel: a raster attachment, which a text may show (docs/adr/0016 D5). */
const pixel = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=',
  'base64',
);

/**
 * The body as the server rendered it, in a real browser under the shell's content-security policy
 * (docs/adr/0011 D6, docs/security/rendered-markdown.md): headings, a table, code, a link and the
 * image of the ticket's own PNG attachment show as markup, the image loaded from the attachment's
 * path; a raw `<img>` with a handler and a `javascript:` link stay text, and nothing of them runs;
 * the policy refuses nothing on the way.
 */
test(
  'the rendered body shows its Markdown and its own image, its hostile lines as text, under the policy',
  { tag: '@smoke' },
  async ({ page, seed, project }) => {
    const violations = await policyViolations(page);
    const ticket = await seed.file(project, { title: 'Describe the export' });
    const image = await seed.attach(project, ticket.number, {
      name: 'diagram.png',
      mimeType: 'image/png',
      buffer: pixel,
    });
    await seed.replaceBody(
      project,
      ticket.number,
      [
        '## What the export writes',
        '',
        '| Format | Since |',
        '| --- | --- |',
        '| CSV | 0.4 |',
        '',
        '```',
        'cowork export --format csv',
        '```',
        '',
        'See [the format](https://example.com/format) and the diagram:',
        '',
        `![diagram](${image.content_url})`,
        '',
        '<img src=x onerror="window.coworkPwned=1">',
        '',
        '[run this](javascript:window.coworkPwned=2)',
      ].join('\n'),
    );

    await page.goto(`/t/${tenant}/tickets/${project}-${ticket.number}`);
    await expectScheme(page);
    const body = page.getByTestId('body');
    await expect(body.getByRole('heading', { name: 'What the export writes' })).toBeVisible();
    await expect(body.getByRole('table').getByRole('cell', { name: 'CSV' })).toBeVisible();
    await expect(body.locator('pre code')).toHaveText('cowork export --format csv');
    const link = body.getByRole('link', { name: 'the format' });
    await expect(link).toHaveAttribute('href', 'https://example.com/format');
    await expect(link).toHaveAttribute('rel', 'noopener noreferrer nofollow');
    await expect(link).toHaveAttribute('target', '_blank');

    // The image is the attachment's, from its path on this origin, and it loaded.
    const shown = body.getByRole('img', { name: 'diagram' });
    await expect(shown).toHaveAttribute('src', image.content_url);
    await expect
      .poll(() => shown.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth))
      .toBe(1);

    // The hostile lines are text: no second image, no link to a script, nothing ran.
    await expect(body).toContainText('<img src=x onerror="window.coworkPwned=1">');
    await expect(body.getByRole('img')).toHaveCount(1);
    await expect(body.getByText('run this')).toBeVisible();
    await expect(body.getByRole('link', { name: 'run this' })).toHaveCount(0);
    expect(await page.evaluate(() => 'coworkPwned' in window)).toBe(false);

    expect(violations).toEqual([]);
  },
);
