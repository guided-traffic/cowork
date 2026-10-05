import { ChangeDetectionStrategy, Component, input, ViewEncapsulation } from '@angular/core';

/**
 * Markdown as the server rendered it (docs/adr/0011 D6): a ticket's body, a comment, a question's
 * options and answer. The server renders and sanitises the HTML to an allow-list — no script, no
 * handler, no raw HTML, links with `rel="noopener noreferrer nofollow"`, images only of the
 * ticket's own raster attachments. The page binds it to `[innerHTML]` as a plain string, so
 * Angular's own sanitiser runs over it once more; it is never marked trusted
 * (`bypassSecurityTrust…`), and the server's allow-list is a subset of what Angular keeps, so the
 * second line removes nothing that the first let through on purpose.
 *
 * The styles are not encapsulated, because the elements come from `[innerHTML]`, which carries no
 * attribute of the component's; every rule is scoped under the host's element name instead.
 */
@Component({
  selector: 'app-rendered-text',
  changeDetection: ChangeDetectionStrategy.OnPush,
  encapsulation: ViewEncapsulation.None,
  template: `<div class="rendered" [innerHTML]="html()"></div>`,
  styles: `
    app-rendered-text {
      display: block;
      min-width: 0;
      overflow-wrap: anywhere;
    }
    app-rendered-text .rendered {
      line-height: 1.6;
      > :first-child {
        margin-top: 0;
      }
      > :last-child {
        margin-bottom: 0;
      }
      p,
      ul,
      ol,
      blockquote,
      pre,
      table {
        margin: 0 0 0.625rem;
      }
      h1,
      h2,
      h3,
      h4,
      h5,
      h6 {
        margin: 1rem 0 0.5rem;
        font-weight: 600;
        line-height: 1.3;
      }
      h1 {
        font-size: 1.25rem;
      }
      h2 {
        font-size: 1.125rem;
      }
      h3 {
        font-size: 1rem;
      }
      h4,
      h5,
      h6 {
        font-size: 0.875rem;
      }
      ul,
      ol {
        padding-left: 1.5rem;
      }
      li + li {
        margin-top: 0.125rem;
      }
      code {
        padding: 0.0625rem 0.3125rem;
        border-radius: var(--p-border-radius-sm);
        font-family: var(--p-font-family-mono, ui-monospace, monospace);
        font-size: 0.85em;
        background: var(--p-app-hover);
      }
      pre {
        overflow-x: auto;
        padding: 0.625rem 0.75rem;
        border: 1px solid var(--p-app-border);
        border-radius: var(--p-border-radius-md);
        background: var(--p-app-ground);
        code {
          padding: 0;
          background: none;
        }
      }
      blockquote {
        padding-left: 0.75rem;
        border-left: 3px solid var(--p-app-border);
        color: var(--p-text-muted-color);
      }
      hr {
        border: 0;
        border-top: 1px solid var(--p-app-border);
      }
      table {
        display: block;
        overflow-x: auto;
        border-collapse: collapse;
      }
      th,
      td {
        padding: 0.3125rem 0.625rem;
        border: 1px solid var(--p-app-border);
      }
      th {
        font-weight: 600;
        background: var(--p-app-hover);
      }
      img {
        max-width: 100%;
        height: auto;
        border-radius: var(--p-border-radius-md);
      }
    }
  `,
})
export class RenderedText {
  /** The HTML the server rendered and sanitised; bound as a string, sanitised again by Angular. */
  readonly html = input.required<string>();
}
