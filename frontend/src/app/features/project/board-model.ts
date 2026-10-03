import { Ticket, TicketState, Urgency, WipLimits } from '../../api/models';
import { Move, MoveKind, movesOf } from '../../shared/transitions';

/**
 * The board's state columns (docs/adr/0018 D1): a view over the states of docs/adr/0009 D1, each
 * counted against a WIP limit of docs/adr/0019 D3 — Refinement, which holds `filed` and
 * `analysed`, against the `analysed` one.
 */
export type ColumnId = 'refinement' | 'ready' | 'in-progress' | 'blocked' | 'review';

export interface ColumnSpec {
  id: ColumnId;
  title: string;
  states: readonly TicketState[];
  limit: keyof WipLimits;
}

export const columnSpecs: readonly ColumnSpec[] = [
  { id: 'refinement', title: 'Refinement', states: ['filed', 'analysed'], limit: 'analysed' },
  { id: 'ready', title: 'Ready', states: ['decided'], limit: 'decided' },
  { id: 'in-progress', title: 'In Progress', states: ['in-progress'], limit: 'in-progress' },
  { id: 'blocked', title: 'Blocked', states: ['blocked'], limit: 'blocked' },
  { id: 'review', title: 'Review', states: ['review'], limit: 'review' },
];

/** The urgencies the board shows, each of its own column or columns. */
export const boardUrgencies: readonly Urgency[] = ['now', 'release', 'next'];

/** The column of a state; none for `done` and `dropped`, which the board does not show. */
export function columnOf(state: TicketState): ColumnId | null {
  return columnSpecs.find((spec) => spec.states.includes(state))?.id ?? null;
}

export function specOf(id: ColumnId): ColumnSpec {
  // Every id names a column.
  return columnSpecs.find((spec) => spec.id === id)!;
}

/** Where the board shows a ticket: the column `next`, a state column, or nowhere. */
export type Place = 'next' | ColumnId | null;

/**
 * Where a ticket belongs on the board: an open ticket without children (a leaf — `progress_derived`
 * is true exactly for a ticket with children) of urgency `now` or `release` in the column of its
 * state, and one of urgency `next` in the column `next`, whatever its state; `later`, `icebox`, a
 * parent and a closed ticket nowhere.
 */
export function placeOf(ticket: Pick<Ticket, 'state' | 'urgency' | 'progress_derived'>): Place {
  const column = columnOf(ticket.state);
  if (ticket.progress_derived || column === null) {
    return null;
  }
  if (ticket.urgency === 'next') {
    return 'next';
  }
  return ticket.urgency === 'now' || ticket.urgency === 'release' ? column : null;
}

export interface Column extends ColumnSpec {
  cards: Ticket[];
  /** The project's WIP limit of the column, when it has one. */
  max: number | undefined;
  /** More cards than the limit: the column is marked, and takes cards all the same. */
  over: boolean;
}

export interface BoardView {
  next: Ticket[];
  columns: Column[];
}

/**
 * The board of the tickets, which come in the project's rank: each column holds its cards in that
 * order, which the backlog owns. `place` says where a ticket is shown — where it belongs, or where
 * a move on its way puts it.
 */
export function arrange(
  tickets: readonly Ticket[],
  place: (ticket: Ticket) => Place,
  limits: WipLimits,
): BoardView {
  const next: Ticket[] = [];
  const cards = new Map<ColumnId, Ticket[]>(columnSpecs.map((spec) => [spec.id, []]));
  for (const ticket of tickets) {
    const at = place(ticket);
    if (at === 'next') {
      next.push(ticket);
    } else if (at !== null) {
      cards.get(at)!.push(ticket);
    }
  }
  return {
    next,
    columns: columnSpecs.map((spec) => {
      const held = cards.get(spec.id)!;
      const max = limits[spec.limit];
      return { ...spec, cards: held, max, over: max !== undefined && held.length > max };
    }),
  };
}

/** The moves of the matrix a board carries out: done, dropped and the reopens are the detail's. */
const boardKinds: ReadonlySet<MoveKind> = new Set(['forward', 'backward', 'block', 'unblock']);

type Movable = Pick<Ticket, 'state' | 'block' | 'done_from' | 'done_by_hand'>;

/**
 * The transition a drop of the ticket into a column is (docs/adr/0009): the move of the matrix to
 * a state of that column, or null where the matrix allows none — the column then takes no drop. A
 * drop into its own column is no move: `filed → analysed` is a card action, not a drop.
 */
export function dropMove(ticket: Movable, target: ColumnId): Move | null {
  if (columnOf(ticket.state) === target) {
    return null;
  }
  const states = specOf(target).states;
  return (
    movesOf(ticket).find((move) => boardKinds.has(move.kind) && states.includes(move.to)) ?? null
  );
}

/** The columns a drop of the ticket goes to, for the highlight while it is dragged. */
export function dropTargets(ticket: Movable): Set<ColumnId> {
  return new Set(columnSpecs.map((spec) => spec.id).filter((id) => dropMove(ticket, id) !== null));
}

/** The move inside the ticket's own column, a button on its card: `filed → analysed`. */
export function cardAction(ticket: Movable): Move | null {
  const own = columnOf(ticket.state);
  return (
    movesOf(ticket).find((move) => move.kind === 'forward' && columnOf(move.to) === own) ?? null
  );
}

/** A move the card's menu offers: the card action, and every drop the matrix allows. */
export interface MenuMove {
  move: Move;
  column: ColumnId;
  label: string;
}

/** The words of a move to a column, as the detail page words the moves to a state. */
const verbs: Partial<Record<MoveKind, string>> = {
  forward: 'Move to',
  backward: 'Back to',
  unblock: 'Unblock to',
};

/**
 * The card's menu, the keyboard's way to do what a drag does: the card action first, then a move
 * to each column that takes the card, in the order of the columns. A label ends in `…` where the
 * move asks for something first.
 */
export function menuMoves(ticket: Movable): MenuMove[] {
  const own = columnOf(ticket.state);
  const action = cardAction(ticket);
  const moves: MenuMove[] =
    action && own ? [{ move: action, column: own, label: `Move to ${action.to}` }] : [];
  for (const spec of columnSpecs) {
    const move = dropMove(ticket, spec.id);
    if (move) {
      const words = move.kind === 'block' ? 'Block' : `${verbs[move.kind]} ${spec.title}`;
      moves.push({
        move,
        column: spec.id,
        label: `${words}${move.input === 'none' ? '' : '…'}`,
      });
    }
  }
  return moves;
}
