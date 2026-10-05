import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  signal,
  untracked,
} from '@angular/core';
import { ConfirmationService, MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { TableModule } from 'primeng/table';
import { Tooltip } from 'primeng/tooltip';
import { DeletedTicket } from '../../api/models';
import { DeletedTicketsService } from '../../core/deleted-tickets.service';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { StateBadge, TypeIcon } from '../../shared/badges';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { ago, Clock, dateTime } from '../../shared/time';

/** `acme/VKO-12` → `VKO-12`. */
function shortKey(entry: DeletedTicket): string {
  return `${entry.project}-${entry.number}`;
}

/**
 * The tenant's bin (docs/adr/0024 D1, D2), for its administrators: the deleted tickets they can
 * see, the last deleted first, each with who deleted it and when the purge job removes it.
 * Restoring brings a ticket back as it was, at once — it is the undoing of a deletion. Purging
 * removes it for good, before its thirty days: the page asks twice, the second time saying that
 * nothing brings it back, with the focus on the button that does nothing.
 */
@Component({
  selector: 'app-deleted-tickets',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, ConfirmDialog, StateBadge, TableModule, Tooltip, TypeIcon],
  providers: [ConfirmationService],
  templateUrl: './deleted-tickets.html',
  styleUrl: './deleted-tickets.scss',
})
export class DeletedTickets {
  protected readonly deleted = inject(DeletedTicketsService);
  protected readonly session = inject(SessionService);
  protected readonly tenant = inject(TenantService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly confirm = inject(ConfirmationService);
  private readonly clock = inject(Clock);

  protected readonly dateTime = dateTime;
  protected readonly shortKey = shortKey;
  /** The entry whose act is on its way: its buttons wait for it. */
  protected readonly busy = signal<string | null>(null);

  protected readonly failure = computed(() => {
    const error = this.deleted.bin.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The bin could not be loaded: ${problem.detail || problem.title}`;
  });

  constructor() {
    this.deleted.bin.reload();
    // A question belongs to the tenant it was asked in: another tenant's page starts clean, and a
    // purge confirmed after the switch would name a ticket of the tenant shown then.
    effect(() => {
      this.session.tenant();
      untracked(() => this.confirm.close());
    });
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  protected restore(entry: DeletedTicket): void {
    void this.run(entry, async () => {
      await this.deleted.restore(entry);
      this.messages.add({
        severity: 'success',
        summary: 'Ticket restored',
        detail: `${shortKey(entry)} is back where it was, with its links and its place in the rank.`,
        life: 4000,
      });
    });
  }

  /** Purging is irreversible: a first question, then a second that says so (docs/adr/0024 D2). */
  protected purge(entry: DeletedTicket): void {
    this.confirm.confirm({
      header: `Purge ${shortKey(entry)} now?`,
      message:
        `${shortKey(entry)} — ${entry.title} — would be purged by itself on ${dateTime(entry.purge_at)}. ` +
        'Purging removes it now, with its comments, questions, links, files and time entries. ' +
        'The audit record keeps its key and who did what, not what it said.',
      acceptLabel: 'Continue',
      rejectLabel: 'Keep it',
      defaultFocus: 'reject',
      accept: () => setTimeout(() => this.purgeForGood(entry)),
    });
  }

  private purgeForGood(entry: DeletedTicket): void {
    this.confirm.confirm({
      header: `Purge ${shortKey(entry)} for good?`,
      message: `Nothing brings ${shortKey(entry)} back once it is purged. Its key stays taken.`,
      acceptLabel: 'Purge for good',
      rejectLabel: 'Keep it',
      acceptButtonProps: { severity: 'danger' },
      // Nothing undoes it: an Enter that comes a moment late must not confirm it.
      defaultFocus: 'reject',
      accept: () =>
        this.run(entry, async () => {
          await this.deleted.purge(entry);
          this.messages.add({
            severity: 'success',
            summary: 'Ticket purged',
            detail: `${shortKey(entry)} is gone for good.`,
            life: 4000,
          });
        }),
    });
  }

  private async run(entry: DeletedTicket, act: () => Promise<void>): Promise<void> {
    this.busy.set(entry.key);
    try {
      await act();
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.busy.set(null);
    }
  }
}
