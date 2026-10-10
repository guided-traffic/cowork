import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  output,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { Textarea } from 'primeng/textarea';
import { Ticket } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { keepOpenWhile } from '../../shared/keep-open';

/**
 * A tenant administrator sets or lifts a ticket's confidential flag (docs/adr/0065): set, it hides
 * the ticket from everybody but the tenant's administrators, its assignee and its reporter (D1);
 * lifted, which only a person does and only with a reason (D3, D6), everybody who sees the project
 * reads it again. The reason is required to lift and may be given to set. The dialog stays open
 * while its request runs and shows a refusal in its form; `closed` says once it is over.
 */
@Component({
  selector: 'app-confidential-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Dialog, FormsModule, Textarea],
  template: `
    <p-dialog
      [visible]="open()"
      (visibleChange)="$event || cancel()"
      [modal]="true"
      [draggable]="false"
      [closable]="!busy()"
      [dismissableMask]="!busy()"
      [style]="{ width: '30rem' }"
      [header]="lifting() ? 'Lift the confidential flag' : 'Make it confidential'"
      data-testid="confidential-dialog"
    >
      <form class="form" ngNoForm novalidate (submit)="$event.preventDefault(); send()">
        <p class="explain">
          @if (lifting()) {
            Everybody who sees the project {{ project() }} will read {{ shortKey() }} again: its
            body, its comments, its questions, its files and its time. The activity records who
            lifted the flag, and why.
          } @else {
            Only the team's administrators, the assignee and the reporter will see
            {{ shortKey() }} — not the other members, and nobody's agent beyond those. Assigning the
            ticket admits the new assignee.
          }
        </p>
        <label class="field">
          <span>{{ lifting() ? 'Why may it be read now?' : 'Why? (optional)' }}</span>
          <textarea
            pTextarea
            name="reason"
            rows="3"
            maxlength="2000"
            data-testid="confidential-reason"
            [ngModel]="reason()"
            (ngModelChange)="reason.set($event)"
          ></textarea>
        </label>
        @if (error()) {
          <small class="error" data-testid="confidential-error">{{ error() }}</small>
        }
        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            [disabled]="busy()"
            (click)="cancel()"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            [severity]="lifting() ? 'warn' : undefined"
            data-testid="confidential-send"
            [disabled]="!canSend()"
          >
            @if (busy()) {
              <i class="pi pi-spinner pi-spin"></i>
            } @else {
              <i [class]="lifting() ? 'pi pi-lock-open' : 'pi pi-lock'"></i>
            }
            {{ lifting() ? 'Lift it' : 'Make it confidential' }}
          </button>
        </div>
      </form>
    </p-dialog>
  `,
  styles: `
    .form {
      display: flex;
      flex-direction: column;
      gap: 0.75rem;
    }
    .explain {
      margin: 0;
      font-size: 0.875rem;
      line-height: 1.5;
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
      font-size: 0.8125rem;
      textarea {
        width: 100%;
      }
    }
    .error {
      color: var(--p-severity-critical);
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
  `,
})
export class ConfidentialDialog {
  readonly ticket = input.required<Ticket>();
  readonly open = input(false);
  readonly closed = output<void>();

  private readonly actions = inject(TicketActions);
  private readonly problems = inject(ProblemService);

  /** Every opening starts with an empty form. */
  protected readonly reason = linkedSignal({ source: this.open, computation: () => '' });
  protected readonly error = linkedSignal({ source: this.open, computation: () => '' });
  protected readonly busy = signal(false);

  protected readonly lifting = computed(() => this.ticket().confidential);
  protected readonly shortKey = computed(() => {
    const key = this.ticket().key;
    return key.slice(key.indexOf('/') + 1);
  });
  protected readonly project = computed(() => this.ticket().project);
  protected readonly canSend = computed(
    () => !this.busy() && (!this.lifting() || this.reason().trim() !== ''),
  );

  constructor() {
    keepOpenWhile(() => this.busy());
  }

  protected cancel(): void {
    if (!this.busy()) {
      this.closed.emit();
    }
  }

  protected async send(): Promise<void> {
    if (!this.open() || !this.canSend()) {
      return;
    }
    const ticket = this.ticket();
    const reason = this.reason().trim();
    this.busy.set(true);
    this.error.set('');
    try {
      await this.actions.setConfidential(ticket.key, !ticket.confidential, reason || undefined);
      this.closed.emit();
    } catch (error) {
      if (error instanceof StaleWrite) {
        this.error.set(
          `Someone ${error.current.confidential ? 'set' : 'lifted'} the flag meanwhile. Check what the ticket shows now.`,
        );
      } else {
        const problem = this.problems.read(error);
        this.error.set(problem.detail || problem.title);
      }
    } finally {
      this.busy.set(false);
    }
  }
}
