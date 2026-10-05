import { ChangeDetectionStrategy, Component, computed, input, linkedSignal } from '@angular/core';
import { Attachment } from '../../api/models';

/** The types the backend delivers inline (docs/adr/0016 D5); everything else is a download. */
const raster: ReadonlySet<Attachment['content_type']> = new Set([
  'image/png',
  'image/jpeg',
  'image/gif',
  'image/webp',
]);

/** Whether a file is a raster image, which the page may show. */
export function previewable(file: Attachment): boolean {
  return raster.has(file.content_type);
}

/**
 * The preview of a raster attachment (docs/adr/0016 D5): the backend delivers the four raster
 * types inline, under `Content-Security-Policy: sandbox` and `nosniff`, from the installation's
 * own origin, which the shell's policy admits as an image source. An SVG is no raster image and is
 * never shown. The image loads lazily and opens whole in a tab on a click; each load is a download
 * the backend records (docs/adr/0026 D5). An image that fails to load leaves its row as a link.
 */
@Component({
  selector: 'app-file-preview',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (shown()) {
      <a
        class="preview"
        [href]="file().content_url"
        target="_blank"
        rel="noopener"
        [attr.aria-label]="'Open ' + file().file_name"
      >
        <img
          [src]="file().content_url"
          [alt]="file().file_name"
          loading="lazy"
          decoding="async"
          (error)="failed.set(true)"
          data-testid="preview"
        />
      </a>
    }
  `,
  styles: `
    .preview {
      display: inline-block;
      margin-top: 0.375rem;
      border: 1px solid var(--p-app-border);
      border-radius: var(--p-border-radius-md);
      overflow: hidden;
      background: var(--p-app-raised);
      line-height: 0;
    }
    img {
      display: block;
      max-width: 100%;
      max-height: 10rem;
      object-fit: contain;
    }
  `,
})
export class FilePreview {
  readonly file = input.required<Attachment>();

  private readonly id = computed(() => this.file().id);
  /** The image did not load; another file starts again, the same one reloaded does not. */
  protected readonly failed = linkedSignal({ source: this.id, computation: () => false });
  protected readonly shown = computed(() => previewable(this.file()) && !this.failed());
}
