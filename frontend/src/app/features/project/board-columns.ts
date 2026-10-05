import {
  CdkDrag,
  CdkDragDrop,
  CdkDragEnd,
  CdkDragMove,
  CdkDropList,
  CdkDropListGroup,
} from '@angular/cdk/drag-drop';
import { CdkScrollable } from '@angular/cdk/scrolling';
import {
  ChangeDetectionStrategy,
  ChangeDetectorRef,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  output,
  signal,
  viewChild,
} from '@angular/core';
import { Router } from '@angular/router';
import { MenuItem } from 'primeng/api';
import { Menu } from 'primeng/menu';
import { Tooltip } from 'primeng/tooltip';
import { Ticket } from '../../api/models';
import { count } from '../../shared/time';
import { Move } from '../../shared/transitions';
import { BoardCard } from './board-card';
import {
  BoardView,
  cardAction,
  Column,
  ColumnId,
  columnOf,
  dropMove,
  dropTargets,
  menuMoves,
} from './board-model';
import { BoardMoves, shortKey } from './board-moves';

/** A point of the window, as the CDK reports where the pointer is. */
export interface Point {
  x: number;
  y: number;
}

/**
 * The columns of a board (docs/adr/0018 D1, D4), for the project's board and for each swimlane of
 * the tenant's: on the left the column Next, its cards compact with the button Now, then the state
 * columns Refinement, Ready, In Progress, Blocked and Review with their counts against the WIP
 * limits. A card dragged to another state column is the transition of docs/adr/0009 its state is; a
 * column the matrix does not allow takes no drop, and the ones that do are marked while a card is
 * dragged. No list sorts: no drop changes the rank. The card's menu makes the same moves for the
 * keyboard. The moves are the page's {@link BoardMoves}.
 *
 * While a card is dragged — here, or anywhere on the page (`hold`) — the columns stay as they were
 * when it was picked up: the CDK measures its lists as the drag starts, and nothing may move from
 * under the pointer. What changed meanwhile shows once the card is put down.
 */
@Component({
  selector: 'app-board-columns',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [BoardCard, CdkDrag, CdkDropList, CdkDropListGroup, CdkScrollable, Menu, Tooltip],
  templateUrl: './board-columns.html',
  styleUrl: './board-columns.scss',
})
export class BoardColumns {
  /** The board as its list and the moves on their way have it now. */
  readonly view = input.required<BoardView>();
  readonly tenant = input.required<string>();
  readonly loading = input(false);
  /** Whether the list has answered: before, an empty column says nothing. */
  readonly loaded = input(true);
  /** A card is dragged elsewhere on the page: nothing here moves either. */
  readonly hold = input(false);
  /** Makes the ids of the columns' headings unique on a page that shows more than one board. */
  readonly idPrefix = input('');
  /** Loads the board's list again, after a write that failed. */
  readonly reload = input<() => void>(() => undefined);

  /** A card of these columns was picked up. */
  readonly dragStart = output<Ticket>();
  /** The pointer of that drag moved, to this point of the window. */
  readonly dragMove = output<Point>();
  /** That drag ended, with the pointer at this point. */
  readonly dragEnd = output<Point | undefined>();

  protected readonly moves = inject(BoardMoves);
  private readonly router = inject(Router);
  private readonly changes = inject(ChangeDetectorRef);

  /** The card that is dragged, and the columns it may go to. */
  protected readonly dragged = signal<Ticket | null>(null);
  protected readonly targets = computed(() => {
    const ticket = this.dragged();
    return ticket ? dropTargets(ticket) : new Set<ColumnId>();
  });
  /** The board as it is shown: as it was when a drag began, until it ends. */
  protected readonly board = linkedSignal<{ view: BoardView; held: boolean }, BoardView>({
    source: () => ({ view: this.view(), held: this.hold() || this.dragged() !== null }),
    computation: (source, previous) =>
      source.held && previous?.source.held ? previous.value : source.view,
  });
  /** A drag ends with a click on the card it dragged, which must not open the ticket. */
  private justDragged = false;

  protected readonly menuItems = signal<MenuItem[]>([]);
  private readonly menu = viewChild.required<Menu>('cardMenu');

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

  protected open(ticket: Ticket): void {
    if (this.justDragged) {
      return;
    }
    void this.router.navigate(['/t', this.tenant(), 'tickets', shortKey(ticket.key)]);
  }

  // The drag. The CDK measures its lists as the drag starts, right after it announces it, so the
  // board is held and the columns are marked by then.

  protected dragStarted(ticket: Ticket): void {
    this.dragged.set(ticket);
    this.dragStart.emit(ticket);
    this.changes.detectChanges();
  }

  protected dragMoved(event: CdkDragMove<Ticket>): void {
    this.dragMove.emit(event.pointerPosition);
  }

  protected dragEnded(event: Partial<CdkDragEnd<Ticket>>): void {
    this.dragged.set(null);
    this.justDragged = true;
    setTimeout(() => (this.justDragged = false));
    this.dragEnd.emit(event.dropPoint);
  }

  protected dropped(event: CdkDragDrop<ColumnId, ColumnId, Ticket>): void {
    this.dragged.set(null);
    // Dropped beside the board, or back in its own column: the card goes back where it was.
    if (!event.isPointerOverContainer || event.container === event.previousContainer) {
      return;
    }
    this.moves.drop(event.item.data, event.container.data, this.reload());
  }

  // The card's menu does what a drag does, for those who cannot drag.

  protected openMenu(event: Event, ticket: Ticket): void {
    this.menuItems.set(
      menuMoves(ticket).map(({ move, column, label }) => ({
        label,
        command: () => this.moves.choose(ticket, move, column, this.reload()),
      })),
    );
    this.menu().toggle(event);
  }

  /** The card's action, `filed → analysed`, which stays in its column. */
  protected act(ticket: Ticket, move: Move): void {
    // A forward move needs no input; the card stays in its own column.
    this.moves.choose(ticket, move, columnOf(ticket.state)!, this.reload());
  }
}
