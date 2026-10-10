import { Horizon, Ticket, TicketHead } from '../../api/models';

/** The groups of the backlog, in the order of docs/adr/0010 D1 (docs/adr/0018 D1). */
export const horizons: readonly Horizon[] = ['now', 'release', 'next', 'later', 'icebox'];

/**
 * The groups that show while they are empty. `release` and `icebox` show only while they hold a
 * ticket or while a ticket is dragged (docs/adr/0018 D1).
 */
export const alwaysShown: ReadonlySet<Horizon> = new Set<Horizon>(['now', 'next', 'later']);

/** A row of a group: the ticket, and where it stands in the group's tree. */
export interface Row {
  ticket: Ticket;
  /** 0 for a ticket whose parent is not in the group; one more for each parent above it in it. */
  depth: number;
  /** The key of the parent that is in the group, where the row is indented under it. */
  under: string | null;
  /**
   * The parent that is not in the group — in another group, another project or another team, or
   * one the reader may not see —, which the row names by its head (docs/adr/0005 D3); null without
   * one.
   */
  elsewhere: TicketHead | null;
}

export interface Group {
  horizon: Horizon;
  rows: Row[];
}

/**
 * Where a ticket goes in the project's rank: directly after, or directly before, a neighbour. The
 * route takes the neighbour's number (docs/adr/0014 D2); the key is what the page patches its own
 * order with.
 */
export interface Placement {
  side: 'after' | 'before';
  neighbour: Ticket;
}

/** What a drop or a menu choice asks for: a horizon, a place in the rank, or both. */
export interface Plan {
  ticket: Ticket;
  /** The group a move into another group sets the horizon to; absent within a group. */
  horizon?: Horizon;
  /** Absent where the ticket has no sibling to be placed next to. */
  placement: Placement | null;
}

/**
 * The open tickets, which arrive in the project's rank, as the five groups (docs/adr/0018 D1).
 * Each group keeps the order of the rank, and a ticket whose parent is in the same group stands
 * under it, indented, with the rest of the parent's children in their rank (docs/adr/0008 D4).
 * `horizonOf` lets a page show a ticket in the group it was just dragged to.
 */
export function arrange(
  tickets: readonly Ticket[],
  horizonOf: (ticket: Ticket) => Horizon = (ticket) => ticket.horizon,
): Group[] {
  const members: Record<Horizon, Ticket[]> = {
    now: [],
    release: [],
    next: [],
    later: [],
    icebox: [],
  };
  for (const ticket of tickets) {
    members[horizonOf(ticket)].push(ticket);
  }
  return horizons.map((horizon) => ({ horizon, rows: tree(members[horizon]) }));
}

/** The tickets of one group as the depth-first rows of their parent relation. */
function tree(tickets: readonly Ticket[]): Row[] {
  const keys = new Set(tickets.map((ticket) => ticket.key));
  const children = new Map<string, Ticket[]>();
  const roots: Ticket[] = [];
  for (const ticket of tickets) {
    if (ticket.parent !== null && keys.has(ticket.parent)) {
      children.set(ticket.parent, [...(children.get(ticket.parent) ?? []), ticket]);
    } else {
      roots.push(ticket);
    }
  }
  const rows: Row[] = [];
  const seen = new Set<string>();
  const walk = (ticket: Ticket, depth: number, under: string | null): void => {
    seen.add(ticket.key);
    rows.push({ ticket, depth, under, elsewhere: under === null ? ticket.parent_head : null });
    for (const child of children.get(ticket.key) ?? []) {
      if (!seen.has(child.key)) {
        walk(child, depth + 1, ticket.key);
      }
    }
  };
  for (const root of roots) {
    walk(root, 0, null);
  }
  // The server refuses a cycle (docs/adr/0008 D2), but two cached tickets of different times can
  // form one for a moment; its members have no root, and they must not drop out of the list.
  for (const ticket of tickets) {
    if (!seen.has(ticket.key)) {
      walk(ticket, 0, null);
    }
  }
  return rows;
}

/** The ticket's place among its siblings in a group's rows: the ones it shares a parent with. */
function siblingsOf(
  rows: readonly Row[],
  ticket: Ticket,
): { under: string | null; siblings: Row[] } {
  const others = rows.filter((row) => row.ticket.key !== ticket.key);
  const under =
    ticket.parent !== null && others.some((row) => row.ticket.key === ticket.parent)
      ? ticket.parent
      : null;
  return { under, siblings: others.filter((row) => row.under === under) };
}

/** Where a ticket stands among its siblings: how many are above it, and how many there are. */
export function position(
  rows: readonly Row[],
  ticket: Ticket,
): { index: number; count: number } | null {
  const at = rows.findIndex((row) => row.ticket.key === ticket.key);
  if (at < 0) {
    return null;
  }
  const { under, siblings } = siblingsOf(rows, ticket);
  return {
    index: rows.slice(0, at).filter((row) => row.under === under).length,
    count: siblings.length,
  };
}

/** The placement that puts a ticket at `index` among `siblings`, which do not hold it. */
function placementAt(siblings: readonly Row[], index: number): Placement | null {
  if (index > 0) {
    return { side: 'after', neighbour: siblings[index - 1].ticket };
  }
  return siblings.length > 0 ? { side: 'before', neighbour: siblings[0].ticket } : null;
}

/**
 * What dropping `ticket` at index `at` of a group's rows asks for. The CDK counts `at` in the rows
 * with the ticket in its new place, so the rows without it are what it is inserted into. A drop
 * means a place among the siblings (`after` the sibling above, or `before` the first), because the
 * list shows the family of a ticket together: a drop between a ticket and its children puts the
 * ticket behind the whole family, and a child only moves among its siblings.
 *
 * Returns null for a move within the group that changes nothing, or that the group's tree does not
 * allow (a child dropped outside its parent's family). Into another group the horizon is set
 * whatever the place is, and a child dropped outside its parent's family keeps its rank.
 */
export function planDrop(
  ticket: Ticket,
  from: Horizon,
  to: Horizon,
  rows: readonly Row[],
  at: number,
): Plan | null {
  const others = rows.filter((row) => row.ticket.key !== ticket.key);
  const { under, siblings } = siblingsOf(rows, ticket);
  const moves = from !== to;
  const index = slotIndex(others, under, at);
  const before = moves ? null : position(rows, ticket);
  if (index === null) {
    return moves ? { ticket, horizon: to, placement: null } : null;
  }
  if (before !== null && before.index === index) {
    return null;
  }
  const placement = placementAt(siblings, index);
  return moves ? { ticket, horizon: to, placement } : { ticket, placement };
}

/**
 * How many siblings are above a drop at index `at` of the rows without the dragged ticket; null for
 * a child dropped outside the family of its parent.
 */
function slotIndex(others: readonly Row[], under: string | null, at: number): number | null {
  if (under === null) {
    return others.slice(0, at).filter((row) => row.under === null).length;
  }
  const parent = others.findIndex((row) => row.ticket.key === under);
  let end = parent + 1;
  while (end < others.length && others[end].depth > others[parent].depth) {
    end++;
  }
  if (at <= parent || at > end) {
    return null;
  }
  return others.slice(parent + 1, at).filter((row) => row.under === under).length;
}

/** The ticket at `index` among its siblings in its group, as a keyboard asks for it. */
export function planStep(rows: readonly Row[], ticket: Ticket, index: number): Plan | null {
  const { siblings } = siblingsOf(rows, ticket);
  const now = position(rows, ticket);
  if (now === null || index === now.index || index < 0 || index > siblings.length) {
    return null;
  }
  return { ticket, placement: placementAt(siblings, index) };
}

/** The ticket at the end of the group it is moved to: the keyboard has no pointer to choose a place. */
export function planGroup(rows: readonly Row[], ticket: Ticket, to: Horizon): Plan {
  const { siblings } = siblingsOf(rows, ticket);
  return { ticket, horizon: to, placement: placementAt(siblings, siblings.length) };
}

/**
 * The project's order of keys after a move, the way the server will have it: the ticket directly
 * after or before its neighbour. A ticket or a neighbour that is not in the order leaves it as it
 * was.
 */
export function movedKeys(keys: readonly string[], key: string, placement: Placement): string[] {
  const rest = keys.filter((each) => each !== key);
  const at = rest.indexOf(placement.neighbour.key);
  if (at < 0 || rest.length === keys.length) {
    return [...keys];
  }
  rest.splice(placement.side === 'after' ? at + 1 : at, 0, key);
  return rest;
}

/**
 * A move in the rank that the page shows before the list does: the ticket, where it went, and the
 * version the write gave it — unknown while the write is on its way.
 */
export interface Move {
  key: string;
  placement: Placement;
  version?: number;
}

/**
 * The moves that a list answer does not show yet, given the version each ticket had in it. An
 * answer shows a move once it has the ticket at the version the move's write gave it, or a later
 * one; until then the move stays on top of it, so that an answer the server gave before the move
 * cannot take it back. A ticket the answer does not have any more takes its moves with it, and an
 * answer that names no versions shows everything there is.
 */
export function unanswered(
  moves: readonly Move[],
  versions: ReadonlyMap<string, number> | undefined,
): Move[] {
  return moves.filter((move) => {
    const version = versions?.get(move.key);
    return version !== undefined && (move.version === undefined || version < move.version);
  });
}

/** The order the page shows: the list's, with the moves it does not show yet, oldest first. */
export function withMoves(keys: readonly string[], moves: readonly Move[]): string[] {
  return moves.reduce((order, move) => movedKeys(order, move.key, move.placement), [...keys]);
}

/** The body of the rank route for a placement. */
export function rankBody(placement: Placement): { after: number } | { before: number } {
  return placement.side === 'after'
    ? { after: placement.neighbour.number }
    : { before: placement.neighbour.number };
}

/** What the score says of a ticket's place in the rank (docs/adr/0014 D3). */
export type ScoreMark = 'higher' | 'lower';

/**
 * Where the score's order and the rank's disagree (docs/adr/0014 D3), among each set of siblings of
 * a group — the places a drag chooses between: the longest run of siblings whose scores do not rise
 * from the top down agrees with the score, and every other sibling is out of its place; the score
 * says higher where a sibling of that run above it scores less, lower otherwise. So one ticket out
 * of place marks one ticket, not every one it pushed down, and a backlog sorted by the score marks
 * none. Equal scores agree; a ticket without a score is neither marked nor counted.
 */
export function scoreMarks(groups: readonly Group[]): Map<string, ScoreMark> {
  const marks = new Map<string, ScoreMark>();
  for (const group of groups) {
    const sets = new Map<string | null, Ticket[]>();
    for (const row of group.rows) {
      if (row.ticket.score !== null) {
        sets.set(row.under, [...(sets.get(row.under) ?? []), row.ticket]);
      }
    }
    for (const siblings of sets.values()) {
      markSiblings(siblings, marks);
    }
  }
  return marks;
}

/** Marks the siblings, in the rank's order, that the longest agreeing run leaves out. */
function markSiblings(siblings: readonly Ticket[], marks: Map<string, ScoreMark>): void {
  const scores = siblings.map((ticket) => ticket.score ?? 0);
  // The longest run whose scores do not rise, ending at each sibling, and its sibling before.
  const length = scores.map(() => 1);
  const before = scores.map(() => -1);
  for (let i = 0; i < scores.length; i++) {
    for (let j = 0; j < i; j++) {
      if (scores[j] >= scores[i] && length[j] + 1 > length[i]) {
        length[i] = length[j] + 1;
        before[i] = j;
      }
    }
  }
  const kept = new Set<number>();
  for (let i = length.indexOf(Math.max(...length)); i >= 0; i = before[i]) {
    kept.add(i);
  }
  scores.forEach((score, i) => {
    if (!kept.has(i)) {
      const higher = [...kept].some((k) => k < i && scores[k] < score);
      marks.set(siblings[i].key, higher ? 'higher' : 'lower');
    }
  });
}
