import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { Textarea } from 'primeng/textarea';
import { Ticket } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { ConflictNote } from '../../shared/conflict-note';

/**
 * The ticket's description: its body, the current state, shown as text and edited as Markdown
 * (docs/adr/0011 D1). Saving replaces it as a whole with `replaceTicketBody`, over the version the
 * editing began with (docs/adr/0050 D3): a change of another field meanwhile is written over at
 * once, a change of the body is shown in the editor, which asks whether to write over it or to take
 * the new body and go on from there. The editor belongs to the ticket it was opened on and closes
 * when the page turns to another.
 */
@Component({
  selector: 'app-ticket-body',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, ConflictNote, FormsModule, Textarea],
  template: `
    <div class="head">
      <h2>Description</h2>
      @if (!since()) {
        <button
          pButton
          type="button"
          size="small"
          severity="secondary"
          [text]="true"
          data-testid="body-edit"
          (click)="edit()"
        >
          <i class="pi pi-pencil"></i>Edit
        </button>
      }
    </div>
    @if (since()) {
      <form class="edit" ngNoForm novalidate (submit)="$event.preventDefault(); save()">
        <textarea
          pTextarea
          name="body"
          rows="14"
          maxlength="200000"
          aria-label="Description, in Markdown"
          data-testid="body-input"
          [ngModel]="draft()"
          (ngModelChange)="draft.set($event)"
          [readOnly]="saving()"
        ></textarea>
        <small class="muted"
          >Markdown. The description is the ticket's current state: saving replaces it as a whole,
          and the activity keeps the change.</small
        >
        @if (conflict()) {
          <app-conflict-note
            what="The description"
            [busy]="saving()"
            (overwrite)="overwrite()"
            (takeTheirs)="takeTheirs()"
          />
        }
        <div class="actions">
          <button
            pButton
            type="button"
            size="small"
            severity="secondary"
            [text]="true"
            data-testid="body-cancel"
            [disabled]="saving()"
            (click)="cancel()"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            size="small"
            data-testid="body-save"
            [disabled]="saving() || conflict() !== null"
          >
            @if (saving()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Save
          </button>
        </div>
      </form>
    } @else if (ticket().body) {
      <div class="body" data-testid="body">{{ ticket().body }}</div>
    } @else {
      <p class="muted">No description.</p>
    }
  `,
  styles: `
    .head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      margin-bottom: 0.75rem;
      h2 {
        margin: 0;
        font-size: 0.875rem;
      }
    }
    .edit {
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
      textarea {
        width: 100%;
        font-family: var(--p-font-family-mono, ui-monospace, monospace);
        font-size: 0.8125rem;
        line-height: 1.5;
      }
      small {
        font-size: 0.75rem;
      }
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
    .body {
      white-space: pre-wrap;
      line-height: 1.6;
    }
  `,
})
export class TicketBody {
  readonly ticket = input.required<Ticket>();

  private readonly actions = inject(TicketActions);
  private readonly problems = inject(ProblemService);

  private readonly key = computed(() => this.ticket().key);
  /** The ticket as the editing began; null while the body is not edited. Another ticket closes it. */
  protected readonly since = linkedSignal<string, Ticket | null>({
    source: this.key,
    computation: () => null,
  });
  /** The ticket as it is after somebody else changed its body meanwhile. */
  protected readonly conflict = linkedSignal<string, Ticket | null>({
    source: this.key,
    computation: () => null,
  });
  protected readonly draft = signal('');
  protected readonly saving = signal(false);

  protected edit(): void {
    this.draft.set(this.ticket().body);
    this.conflict.set(null);
    this.since.set(this.ticket());
  }

  protected cancel(): void {
    if (!this.saving()) {
      this.since.set(null);
      this.conflict.set(null);
    }
  }

  protected async save(): Promise<void> {
    const since = this.since();
    if (!since || this.saving() || this.conflict()) {
      return;
    }
    const body = this.draft();
    if (body === since.body) {
      this.since.set(null);
      return;
    }
    this.saving.set(true);
    try {
      await this.actions.replaceBody(since.key, body, since);
      if (this.since() === since) {
        this.since.set(null);
      }
    } catch (error) {
      if (error instanceof StaleWrite) {
        if (this.since() === since) {
          this.conflict.set(error.current);
        }
      } else {
        this.problems.report(error);
      }
    } finally {
      this.saving.set(false);
    }
  }

  /** The person's text goes over the body somebody else wrote meanwhile. */
  protected overwrite(): void {
    const current = this.conflict();
    if (current) {
      this.conflict.set(null);
      this.since.set(current);
      void this.save();
    }
  }

  /** The editor goes on from the body somebody else wrote; the person's text is dropped. */
  protected takeTheirs(): void {
    const current = this.conflict();
    if (current) {
      this.conflict.set(null);
      this.since.set(current);
      this.draft.set(current.body);
    }
  }
}
