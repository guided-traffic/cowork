import {
  afterNextRender,
  computed,
  ElementRef,
  inject,
  Injectable,
  Injector,
  signal,
} from '@angular/core';
import { MessageService } from 'primeng/api';
import { Ticket } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { TicketsService } from '../../core/tickets.service';
import { Move } from '../../shared/transitions';
import { MoveRequest } from '../ticket/move-dialog';
import { ColumnId, columnOf, dropMove, specOf } from './board-model';

/** A move a person started: the card shows in its column until the write is in. */
interface Pending {
  key: string;
  column: ColumnId;
  request: MoveRequest;
}

/**
 * The moves of a board's cards, for the project's board and the tenant's alike (docs/adr/0018 D1,
 * D4): a transition of docs/adr/0009 — at once where it needs nothing, after {@link MoveDialog}
 * where it does — and the button Now of a card in Next. A move shows at once: the card stands in
 * its new column with what the move changes, while its dialog is open too, and goes back when the
 * dialog is cancelled or a write without a dialog fails. No move changes the rank. The page
 * provides it and shows its dialog and its live region; after a move from a button or a menu the
 * keyboard finds the card on the page where it went.
 */
@Injectable()
export class BoardMoves {
  private readonly tickets = inject(TicketsService);
  private readonly actions = inject(TicketActions);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly injector = inject(Injector);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  /**
   * What the moves on their way change of their tickets — the state, or the urgency `now` — which
   * the cards show, in the column it puts them in, until the writes are in.
   */
  private readonly shownAs = signal<ReadonlyMap<string, Partial<Ticket>>>(new Map());

  /** The move whose input the dialog asks for. */
  readonly pending = signal<Pending | null>(null);
  readonly pendingTicket = computed(() => {
    const pending = this.pending();
    return pending ? this.tickets.cache.entry(pending.key)()?.value : undefined;
  });
  /** What a screen reader is told about a move, which no one sees. */
  readonly status = signal('');

  /** The tickets of a list's keys as the board shows them: from the cache, as the moves on their way make them. */
  read(keys: readonly string[]): Ticket[] {
    const shown = this.shownAs();
    return keys
      .map((key) => this.tickets.cache.entry(key)()?.value)
      .filter((ticket): ticket is Ticket => ticket !== undefined)
      .map((ticket) => {
        const moving = shown.get(ticket.key);
        return moving ? { ...ticket, ...moving } : ticket;
      });
  }

  /**
   * A card put down on a column: the move of the matrix to it, from the ticket as the cache holds it
   * now rather than as it was picked up — or nothing, where the matrix has none. `reload` loads the
   * card's list again when the write fails.
   */
  drop(ticket: Ticket, column: ColumnId, reload: () => void): void {
    const current = this.tickets.cache.value(ticket.key) ?? ticket;
    const move = dropMove(current, column);
    if (move) {
      this.start(current, move, column, reload);
    }
  }

  /**
   * A move from the card's menu or its action, which the keyboard made: the button it started from
   * may go with the card, so the keyboard follows the card.
   */
  choose(ticket: Ticket, move: Move, column: ColumnId, reload: () => void): void {
    this.start(ticket, move, column, reload);
    this.refocus(ticket.key);
  }

  /** The dialog is over: the cache holds the move it wrote, or the card goes back. */
  closed(written: boolean): void {
    const pending = this.pending();
    this.pending.set(null);
    if (!pending) {
      return;
    }
    this.unshow(pending.key);
    if (written) {
      this.announce(pending.key, pending.column);
    }
    this.refocus(pending.key);
  }

  /** `→ Now` (docs/adr/0018 D1): the horizon `now`, without a reason (docs/adr/0010 D3). */
  async makeNow(ticket: Ticket): Promise<void> {
    const key = ticket.key;
    const column = columnOf(ticket.state);
    this.show(key, { urgency: 'now' });
    // The card leaves Next at once, and the button with it; the keyboard follows the card.
    this.refocus(key);
    try {
      if (ticket.urgency_derived === 'now') {
        await this.actions.withdrawUrgency(key);
      } else {
        await this.actions.overrideUrgency(key, 'now');
      }
      if (column) {
        this.announce(key, column);
      }
    } catch (error) {
      this.report(error, key);
      // The card goes back to Next, and the keyboard with it.
      this.refocus(key);
    } finally {
      this.unshow(key);
    }
  }

  // A move. The card shows in its new column at once; a move that needs input asks for it first.

  private start(ticket: Ticket, move: Move, column: ColumnId, reload: () => void): void {
    const key = ticket.key;
    // A ticket that leaves blocked leaves its block; one that enters it shows its block once it is in.
    this.show(key, { state: move.to, ...(move.to === 'blocked' ? {} : { block: null }) });
    if (move.input === 'none') {
      void this.transit(key, move, column, reload);
    } else {
      this.pending.set({ key, column, request: { kind: 'move', move } });
    }
  }

  private async transit(key: string, move: Move, column: ColumnId, reload: () => void) {
    try {
      await this.actions.transition(key, { to: move.to });
      this.announce(key, column);
    } catch (error) {
      this.problems.report(error);
      reload();
    } finally {
      this.unshow(key);
    }
  }

  private show(key: string, moving: Partial<Ticket>): void {
    this.shownAs.update((shown) => new Map(shown).set(key, moving));
  }

  private unshow(key: string): void {
    this.shownAs.update((shown) => {
      const next = new Map(shown);
      next.delete(key);
      return next;
    });
  }

  private announce(key: string, column: ColumnId): void {
    this.status.set(`${shortKey(key)} moved to ${specOf(column).title}`);
  }

  /**
   * After a move from the card's menu, its action or its button Now, the keyboard finds the card
   * where it went: its menu button, or in Next, where a card has none, its button Now.
   */
  private refocus(key: string): void {
    afterNextRender(
      () => {
        const host = this.host.nativeElement;
        (
          host.querySelector<HTMLElement>(`[data-testid="card-menu-${key}"]`) ??
          host.querySelector<HTMLElement>(`[data-testid="card-now-${key}"]`)
        )?.focus();
      },
      { injector: this.injector },
    );
  }

  private report(error: unknown, key: string): void {
    if (error instanceof StaleWrite) {
      this.messages.add({
        severity: 'warn',
        summary: 'Changed meanwhile',
        detail: `${shortKey(key)} was changed by someone else: its horizon is ${error.current.urgency} now.`,
        life: 6000,
      });
    } else {
      this.problems.report(error);
    }
  }
}

/** `acme/COW-12` → `COW-12`. */
export function shortKey(key: string): string {
  return key.slice(key.indexOf('/') + 1);
}
