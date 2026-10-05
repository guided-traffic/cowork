import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { ButtonDirective } from 'primeng/button';
import { Message } from 'primeng/message';

/**
 * What an editor says when its write met a `412` (docs/adr/0050 D5): somebody changed what it
 * edits since the editing began. The server never merges; the person decides — write theirs over
 * the new version, or take the new version into the editor and go on from there. The editor stays
 * open either way, so nothing typed is lost by a click beside it.
 */
@Component({
  selector: 'app-conflict-note',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Message],
  template: `
    <p-message severity="warn" size="small" data-testid="conflict">
      <div class="note">
        <span>{{ what() }} changed while you edited it.</span>
        <span class="actions">
          <button
            pButton
            type="button"
            size="small"
            severity="warn"
            [disabled]="busy()"
            (click)="overwrite.emit()"
            data-testid="conflict-overwrite"
          >
            Write mine over it
          </button>
          <button
            pButton
            type="button"
            size="small"
            severity="secondary"
            [text]="true"
            [disabled]="busy()"
            (click)="takeTheirs.emit()"
            data-testid="conflict-take-theirs"
          >
            Take the new version
          </button>
        </span>
      </div>
    </p-message>
  `,
  styles: `
    .note {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: 0.5rem;
    }
    .actions {
      display: flex;
      gap: 0.25rem;
    }
  `,
})
export class ConflictNote {
  /** What changed, as the start of a sentence: `The description`. */
  readonly what = input.required<string>();
  readonly busy = input(false);
  readonly overwrite = output<void>();
  readonly takeTheirs = output<void>();
}
