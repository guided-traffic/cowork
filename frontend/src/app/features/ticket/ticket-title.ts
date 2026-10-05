import {
  afterNextRender,
  ChangeDetectionStrategy,
  Component,
  computed,
  ElementRef,
  inject,
  Injector,
  input,
  linkedSignal,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ConfirmationService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { Ticket } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';

/**
 * The ticket's title as the heading of its page, edited in place (docs/adr/0018 D2): a `PATCH`
 * over the version the editing began with, so that a title somebody else set meanwhile is never
 * written over unseen (docs/adr/0050 D3); a `412` asks the person in the page's dialog, as the
 * fields do. The editor belongs to the ticket it was opened on and closes when the page turns to
 * another.
 */
@Component({
  selector: 'app-ticket-title',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, InputText],
  template: `
    @if (since()) {
      <form class="edit" ngNoForm novalidate (submit)="$event.preventDefault(); save()">
        <input
          pInputText
          class="input"
          name="title"
          maxlength="300"
          autocomplete="off"
          aria-label="Title"
          data-testid="title-input"
          [ngModel]="draft()"
          (ngModelChange)="draft.set($event)"
          [readOnly]="saving()"
          (keydown.escape)="cancel()"
        />
        <button
          pButton
          type="submit"
          size="small"
          data-testid="title-save"
          [disabled]="!draft().trim() || saving()"
        >
          @if (saving()) {
            <i class="pi pi-spinner pi-spin"></i>
          }
          Save
        </button>
        <button
          pButton
          type="button"
          size="small"
          severity="secondary"
          [text]="true"
          data-testid="title-cancel"
          [disabled]="saving()"
          (click)="cancel()"
        >
          Cancel
        </button>
      </form>
    } @else {
      <div class="shown">
        <h1 data-testid="ticket-title">{{ ticket().title }}</h1>
        <button
          pButton
          type="button"
          size="small"
          severity="secondary"
          [text]="true"
          [rounded]="true"
          [iconOnly]="true"
          aria-label="Edit the title"
          data-testid="title-edit"
          (click)="edit()"
        >
          <i class="pi pi-pencil"></i>
        </button>
      </div>
    }
  `,
  styles: `
    .shown {
      display: flex;
      align-items: flex-start;
      gap: 0.25rem;
    }
    h1 {
      font-size: 1.5rem;
      line-height: 1.3;
    }
    .edit {
      display: flex;
      align-items: center;
      gap: 0.5rem;
    }
    .input {
      flex: 1;
      min-width: 0;
      font-size: 1.25rem;
      font-weight: 600;
    }
  `,
})
export class TicketTitle {
  readonly ticket = input.required<Ticket>();

  private readonly actions = inject(TicketActions);
  private readonly problems = inject(ProblemService);
  private readonly confirm = inject(ConfirmationService);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly injector = inject(Injector);

  private readonly key = computed(() => this.ticket().key);
  /** The ticket as the editing began; null while the title is not edited. Another ticket closes it. */
  protected readonly since = linkedSignal<string, Ticket | null>({
    source: this.key,
    computation: () => null,
  });
  protected readonly draft = signal('');
  protected readonly saving = signal(false);

  protected edit(): void {
    this.draft.set(this.ticket().title);
    this.since.set(this.ticket());
    afterNextRender(
      () =>
        this.host.nativeElement
          .querySelector<HTMLInputElement>('[data-testid="title-input"]')
          ?.focus(),
      { injector: this.injector },
    );
  }

  protected cancel(): void {
    if (!this.saving()) {
      this.since.set(null);
    }
  }

  protected async save(): Promise<void> {
    const since = this.since();
    const title = this.draft().trim();
    if (!since || !title || this.saving()) {
      return;
    }
    if (title === since.title) {
      this.since.set(null);
      return;
    }
    this.saving.set(true);
    try {
      await this.actions.update(since.key, { title }, since);
      if (this.since() === since) {
        this.since.set(null);
      }
    } catch (error) {
      if (error instanceof StaleWrite) {
        this.ask(since, error.current, title);
      } else {
        this.problems.report(error);
      }
    } finally {
      this.saving.set(false);
    }
  }

  /** Somebody else set the title meanwhile: theirs stands, or the person's goes over it. */
  private ask(since: Ticket, current: Ticket, title: string): void {
    this.confirm.confirm({
      header: 'Changed meanwhile',
      message: `Someone changed the title while you edited it: now “${current.title}”, yours “${title}”. Write yours over it?`,
      acceptLabel: 'Write mine',
      rejectLabel: 'Keep theirs',
      accept: () => {
        if (this.since() === since) {
          this.since.set(current);
          void this.save();
        }
      },
      reject: () => {
        if (this.since() === since) {
          this.since.set(null);
        }
      },
    });
  }
}
