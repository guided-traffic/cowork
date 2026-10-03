import { Ticket, TicketState } from '../api/models';

/** The kinds of move of docs/adr/0009, as backend/internal/domain/transition.go classifies them. */
export type MoveKind =
  'forward' | 'backward' | 'block' | 'unblock' | 'done' | 'withdraw' | 'drop' | 'reopen';

/** What a move asks for before it is sent. */
export type MoveInput = 'none' | 'reason' | 'note' | 'block';

export interface Move {
  to: TicketState;
  kind: MoveKind;
  /** The button's words; states are spelt as the API spells them (docs/adr/0055 D4). */
  label: string;
  input: MoveInput;
}

const forward: Partial<Record<TicketState, TicketState>> = {
  filed: 'analysed',
  analysed: 'decided',
  decided: 'in-progress',
  'in-progress': 'review',
};
const backward: Partial<Record<TicketState, TicketState[]>> = {
  'in-progress': ['decided', 'analysed'],
  decided: ['analysed'],
  review: ['in-progress'],
};

/**
 * The moves a ticket in `state` can make (docs/adr/0009 D2–D5): the next step forward up to
 * `review`, a reasoned step back, done by hand from any open state with a verification note,
 * `blocked` from any open state with a block, back from `blocked` only to where it came from,
 * `dropped` from any open state with a reason, the withdrawal of a done by hand to the state it
 * was done from with a reason, and the reopen of a dropped ticket. `origin` is where a blocked
 * ticket came from, or the state a done one was done from. A ticket done by its stages has no
 * move: a lower stage is its way out, and the server refuses every transition. The server checks
 * the same matrix; the page is a person's, so done by hand is offered from every open state,
 * which the server allows a person and refuses an agent outside `in-progress` and `review`.
 */
export function movesFrom(
  state: TicketState,
  origin?: TicketState | null,
  doneByHand = false,
): Move[] {
  if (state === 'dropped') {
    return [{ to: 'filed', kind: 'reopen', label: 'Reopen', input: 'reason' }];
  }
  if (state === 'done') {
    return doneByHand && origin
      ? [{ to: origin, kind: 'withdraw', label: 'Withdraw done', input: 'reason' }]
      : [];
  }
  const moves: Move[] = [];
  if (state === 'blocked') {
    if (origin) {
      moves.push({ to: origin, kind: 'unblock', label: `Unblock to ${origin}`, input: 'none' });
    }
  } else {
    const next = forward[state];
    if (next) {
      moves.push({ to: next, kind: 'forward', label: `Move to ${next}`, input: 'none' });
    }
  }
  moves.push({ to: 'done', kind: 'done', label: 'Done by hand', input: 'note' });
  if (state !== 'blocked') {
    for (const back of backward[state] ?? []) {
      moves.push({ to: back, kind: 'backward', label: `Back to ${back}`, input: 'reason' });
    }
    moves.push({ to: 'blocked', kind: 'block', label: 'Block', input: 'block' });
  }
  moves.push({ to: 'dropped', kind: 'drop', label: 'Drop', input: 'reason' });
  return moves;
}

/** The moves of a ticket as it is: its origin and whether it is done by hand read from it. */
export function movesOf(
  ticket: Pick<Ticket, 'state' | 'block' | 'done_from' | 'done_by_hand'>,
): Move[] {
  const origin = ticket.state === 'done' ? ticket.done_from : ticket.block?.from;
  return movesFrom(ticket.state, origin, ticket.done_by_hand);
}
