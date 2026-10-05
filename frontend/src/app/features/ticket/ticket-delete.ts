import { ChangeDetectionStrategy, Component, inject, input, signal } from '@angular/core';
import { Router } from '@angular/router';
import { ConfirmationService, MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Tooltip } from 'primeng/tooltip';
import { Ticket } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { routeOf, TicketActions } from '../../core/ticket-actions.service';

/**
 * Deleting a ticket from its page (docs/adr/0024 D1, D7), offered to a tenant administrator: the
 * page asks first, naming the open tickets that wait on this one — a deletion does not refuse
 * over them — and saying that the ticket can be restored from the bin until it is purged. The
 * question is the page's, which closes it when the page shows another ticket. A deleted ticket
 * leaves the page for its project's backlog.
 */
@Component({
  selector: 'app-ticket-delete',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Tooltip],
  template: `
    <button
      pButton
      type="button"
      size="small"
      [text]="true"
      severity="danger"
      [iconOnly]="true"
      [disabled]="busy()"
      pTooltip="Delete this ticket"
      [showDelay]="400"
      aria-label="Delete this ticket"
      data-testid="delete-ticket"
      (click)="ask()"
    >
      <i class="pi pi-trash"></i>
    </button>
  `,
})
export class TicketDelete {
  readonly ticket = input.required<Ticket>();

  private readonly actions = inject(TicketActions);
  private readonly confirm = inject(ConfirmationService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly router = inject(Router);

  /** The dependents are read, or the deletion is on its way. */
  protected readonly busy = signal(false);

  protected async ask(): Promise<void> {
    const ticket = this.ticket();
    const short = ticket.key.slice(ticket.key.indexOf('/') + 1);
    this.busy.set(true);
    let dependents: string[];
    try {
      dependents = await this.actions.dependents(ticket.key);
    } catch (error) {
      this.problems.report(error);
      return;
    } finally {
      this.busy.set(false);
    }
    const waiting =
      dependents.length === 0
        ? ''
        : ` ${dependents.length === 1 ? 'One open ticket waits' : `${dependents.length} open tickets wait`} on it: ` +
          `${dependents.join(', ')}. They stop counting it as a prerequisite.`;
    this.confirm.confirm({
      header: `Delete ${short}?`,
      message:
        `${short} — ${ticket.title} — leaves every list, board and search at once, and its links are hidden.` +
        waiting +
        ' An administrator can restore it from the deleted tickets for thirty days; then it is purged for good.',
      acceptLabel: 'Delete',
      rejectLabel: 'Keep it',
      acceptButtonProps: { severity: 'danger' },
      defaultFocus: 'reject',
      accept: () => void this.delete(ticket, short),
    });
  }

  private async delete(ticket: Ticket, short: string): Promise<void> {
    this.busy.set(true);
    try {
      await this.actions.delete(ticket.key);
      this.messages.add({
        severity: 'success',
        summary: 'Ticket deleted',
        detail: `${short} is in the deleted tickets, where an administrator can restore it.`,
        life: 4000,
      });
      const { tenant, project } = routeOf(ticket.key);
      await this.router.navigate(['/t', tenant, 'p', project, 'backlog']);
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.busy.set(false);
    }
  }
}
