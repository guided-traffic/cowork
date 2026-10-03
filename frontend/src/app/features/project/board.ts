import { CdkDrag, CdkDragDrop, CdkDropList, CdkDropListGroup } from '@angular/cdk/drag-drop';
import { CdkScrollable } from '@angular/cdk/scrolling';
import {
  afterNextRender,
  ChangeDetectionStrategy,
  ChangeDetectorRef,
  Component,
  computed,
  ElementRef,
  inject,
  Injector,
  input,
  linkedSignal,
  signal,
  viewChild,
} from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { MenuItem, MessageService } from 'primeng/api';
import { Menu } from 'primeng/menu';
import { Tooltip } from 'primeng/tooltip';
import { Ticket } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { TicketPage, TicketsService } from '../../core/tickets.service';
import { Clock, count } from '../../shared/time';
import { Move } from '../../shared/transitions';
import { MoveDialog, MoveRequest } from '../ticket/move-dialog';
import { BoardCard } from './board-card';
import {
  arrange,
  boardUrgencies,
  BoardView,
  cardAction,
  Column,
  ColumnId,
  columnOf,
  dropMove,
  dropTargets,
  menuMoves,
  placeOf,
  specOf,
} from './board-model';
import { ProjectHeader } from './project-header';

/** The board shows every card of the current work: its list follows the cursor to the end. */
const everyPage = Number.POSITIVE_INFINITY;

/** How far back the header counts the tickets done, in days (docs/adr/0018 D1). */
const doneDays = 14;

const hour = 3_600_000;

/** A move a person started: the card shows in its column until the write is in. */
interface Pending {
  key: string;
  column: ColumnId;
  request: MoveRequest;
}

/**
 * A project's board (docs/adr/0018 D1): the current work. The open leaves of urgency `now` and
 * `release` stand in the columns Refinement, Ready, In Progress, Blocked and Review — a view over
 * their states, each in the project's rank and counted against its WIP limit (docs/adr/0019 D3) —
 * and the leaves of urgency `next` in the column `next` on the left, whatever their state, each
 * with the button that makes it `now`. A card dragged into another column is that transition of
 * docs/adr/0009; a column the matrix does not allow takes no drop, and the ones that do are marked
 * while a card is dragged. A move that needs input asks for it in {@link MoveDialog}, and the card's
 * menu offers the same moves to the keyboard. No move changes the rank. The header counts the
 * tickets done in the last fourteen days and leads to them. The event stream moves the cards, and
 * holds back while one is dragged.
 */
@Component({
  selector: 'app-board',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    BoardCard,
    CdkDrag,
    CdkDropList,
    CdkDropListGroup,
    CdkScrollable,
    Menu,
    MoveDialog,
    ProjectHeader,
    RouterLink,
    Tooltip,
  ],
  templateUrl: './board.html',
  styleUrl: './board.scss',
})
export class Board {
  readonly project = input.required<string>();

  protected readonly session = inject(SessionService);
  private readonly tickets = inject(TicketsService);
  private readonly actions = inject(TicketActions);
  private readonly projects = inject(ProjectsService);
  private readonly router = inject(Router);
  private readonly clock = inject(Clock);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly injector = inject(Injector);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly view = inject(ChangeDetectorRef);

  /** The open tickets of the board's urgencies, every page of them, in the project's rank. */
  private readonly list = this.tickets.projectTicketPages(() => {
    const tenant = this.session.tenant();
    return tenant
      ? { tenant, project: this.project(), pages: everyPage, urgency: [...boardUrgencies] }
      : undefined;
  });
  /** What the list answered last for this project, kept while it loads again. */
  private readonly answer = linkedSignal<
    { project: string; page: TicketPage | undefined },
    TicketPage | undefined
  >({
    source: () => ({
      project: this.project(),
      page: this.list.hasValue() ? this.list.value() : undefined,
    }),
    computation: (source, previous) =>
      source.page ?? (previous?.source.project === source.project ? previous.value : undefined),
  });

  /**
   * Since when a done ticket counts: fourteen days back, to the hour, so that the count asks again
   * once an hour and not with every tick of the clock. The count's link hands it to the backlog.
   */
  protected readonly doneSince = computed(() =>
    new Date(Math.floor((this.clock.now() - doneDays * 24 * hour) / hour) * hour).toISOString(),
  );
  /** The tickets done since then, numbered so that the answer carries the total (docs/adr/0048 D2). */
  private readonly doneList = this.tickets.projectTickets(() => {
    const tenant = this.session.tenant();
    return tenant
      ? {
          tenant,
          project: this.project(),
          state: ['done'],
          done_after: this.doneSince(),
          page: 1,
          per_page: 25,
        }
      : undefined;
  });
  protected readonly doneCount = computed(() =>
    this.doneList.hasValue() ? this.doneList.value()?.total : undefined,
  );
  protected readonly doneText = computed(() => {
    const done = this.doneCount();
    return done === undefined ? '' : `${count(done, 'ticket')} done in the last ${doneDays} days`;
  });

  private readonly limits = computed(() => this.projects.byKey(this.project())?.wip_limits ?? {});
  /**
   * What the moves on their way change of their tickets — the state, or the urgency `now` — which
   * the cards show, in the column it puts them in, until the writes are in.
   */
  private readonly shownAs = signal<ReadonlyMap<string, Partial<Ticket>>>(new Map());
  private readonly arranged = computed(() => {
    const shown = this.shownAs();
    const tickets = (this.answer()?.keys ?? [])
      .map((key) => this.tickets.cache.entry(key)()?.value)
      .filter((ticket): ticket is Ticket => ticket !== undefined)
      .map((ticket) => {
        const moving = shown.get(ticket.key);
        return moving ? { ...ticket, ...moving } : ticket;
      });
    return arrange(tickets, placeOf, this.limits());
  });
  /** The board as it was when a card was picked up: nothing moves under the pointer. */
  private readonly held = signal<BoardView | undefined>(undefined);
  protected readonly board = computed(() => this.held() ?? this.arranged());
  protected readonly cardCount = computed(() => {
    const board = this.board();
    return board.next.length + board.columns.reduce((sum, column) => sum + column.cards.length, 0);
  });

  /** The card that is dragged, and the columns it may go to. */
  protected readonly dragged = signal<Ticket | null>(null);
  protected readonly targets = computed(() => {
    const ticket = this.dragged();
    return ticket ? dropTargets(ticket) : new Set<ColumnId>();
  });
  /** A drag ends with a click on the card it dragged, which must not open the ticket. */
  private justDragged = false;

  protected readonly loading = computed(() => this.list.isLoading());
  protected readonly failure = computed(() => {
    const error = this.list.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The board could not be loaded: ${problem.detail || problem.title}`;
  });

  protected readonly menuItems = signal<MenuItem[]>([]);
  private readonly menu = viewChild.required<Menu>('cardMenu');

  /** The move whose input the dialog asks for. */
  protected readonly pending = signal<Pending | null>(null);
  protected readonly pendingTicket = computed(() => {
    const pending = this.pending();
    return pending ? this.tickets.cache.entry(pending.key)()?.value : undefined;
  });
  /** What a screen reader is told about a move, which no one sees. */
  protected readonly status = signal('');

  /** Whether the column takes a drop of the card that is dragged: its own column always does. */
  protected readonly accepts = (drag: CdkDrag<Ticket>, drop: CdkDropList<ColumnId>): boolean =>
    columnOf(drag.data.state) === drop.data || dropMove(drag.data, drop.data) !== null;

  protected action(ticket: Ticket): Move | null {
    return cardAction(ticket);
  }

  protected movable(ticket: Ticket): boolean {
    return menuMoves(ticket).length > 0;
  }

  protected limitText(column: Column): string {
    if (column.max === undefined) {
      return `${count(column.cards.length, 'ticket')}, no WIP limit`;
    }
    const state = column.over ? 'over' : column.cards.length === column.max ? 'at' : 'under';
    return `${count(column.cards.length, 'ticket')}, ${state} its WIP limit of ${column.max}`;
  }

  protected shortKey(key: string): string {
    return key.slice(key.indexOf('/') + 1);
  }

  protected open(ticket: Ticket): void {
    if (this.justDragged) {
      return;
    }
    void this.router.navigate(['/t', this.session.tenant(), 'tickets', this.shortKey(ticket.key)]);
  }

  // The drag. The CDK measures its lists as the drag starts, right after it announces it, so the
  // board is held and the columns are marked by then.

  protected dragStarted(ticket: Ticket): void {
    this.held.set(this.arranged());
    this.dragged.set(ticket);
    this.view.detectChanges();
  }

  protected dragEnded(): void {
    this.held.set(undefined);
    this.dragged.set(null);
    this.justDragged = true;
    setTimeout(() => (this.justDragged = false));
  }

  protected dropped(event: CdkDragDrop<ColumnId, ColumnId, Ticket>): void {
    this.held.set(undefined);
    this.dragged.set(null);
    // Dropped beside the board, or back in its own column: the card goes back where it was.
    if (!event.isPointerOverContainer || event.container === event.previousContainer) {
      return;
    }
    const ticket = this.tickets.cache.value(event.item.data.key) ?? event.item.data;
    const move = dropMove(ticket, event.container.data);
    if (move) {
      this.start(ticket, move, event.container.data);
    }
  }

  // The card's menu does what a drag does, for those who cannot drag.

  protected openMenu(event: Event, ticket: Ticket): void {
    this.menuItems.set(
      menuMoves(ticket).map(({ move, column, label }) => ({
        label,
        command: () => {
          this.start(ticket, move, column);
          this.refocus(ticket.key);
        },
      })),
    );
    this.menu().toggle(event);
  }

  /** The card's action, `filed → analysed`, which stays in its column. */
  protected act(ticket: Ticket, move: Move): void {
    // A forward move needs no input; the card stays in its own column.
    this.start(ticket, move, columnOf(ticket.state)!);
    // The button goes with the state the card leaves; the keyboard stays on the card.
    this.refocus(ticket.key);
  }

  // A move. The card shows in its new column at once; a move that needs input asks for it first.

  private start(ticket: Ticket, move: Move, column: ColumnId): void {
    const key = ticket.key;
    // A ticket that leaves blocked leaves its block; one that enters it shows its block once it is in.
    this.show(key, { state: move.to, ...(move.to === 'blocked' ? {} : { block: null }) });
    if (move.input === 'none') {
      void this.transit(key, move, column);
    } else {
      this.pending.set({ key, column, request: { kind: 'move', move } });
    }
  }

  private async transit(key: string, move: Move, column: ColumnId): Promise<void> {
    try {
      await this.actions.transition(key, { to: move.to });
      this.announce(key, column);
    } catch (error) {
      this.problems.report(error);
      refresh(this.list, this.injector);
    } finally {
      this.unshow(key);
    }
  }

  /** The dialog is over: the cache holds the move it wrote, or the card goes back. */
  protected closed(written: boolean): void {
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

  /** `→ Now` (docs/adr/0018 D1): the urgency `now`, without a reason (docs/adr/0010 D3). */
  protected async makeNow(ticket: Ticket): Promise<void> {
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
    this.status.set(`${this.shortKey(key)} moved to ${specOf(column).title}`);
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
        detail: `${this.shortKey(key)} was changed by someone else: its urgency is ${error.current.urgency} now.`,
        life: 6000,
      });
    } else {
      this.problems.report(error);
    }
  }
}
