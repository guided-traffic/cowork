import { TicketState } from '../api/models';

/** The kinds of move of docs/adr/0009, as backend/internal/domain/transition.go classifies them. */
export type MoveKind = 'forward' | 'backward' | 'block' | 'unblock' | 'done' | 'drop' | 'reopen';

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
};
const backward: Partial<Record<TicketState, TicketState[]>> = {
  'in-progress': ['decided', 'analysed'],
  decided: ['analysed'],
};

/**
 * The moves a ticket in `state` can make (docs/adr/0009 D2–D5): the next step forward, a
 * reasoned step back, `done` from `in-progress` with a verification note, `blocked` from any open
 * state with a block, back from `blocked` only to where it came from, `dropped` from any open
 * state with a reason, and the reopen of a terminal ticket. The server checks the same matrix.
 */
export function movesFrom(state: TicketState, origin?: TicketState | null): Move[] {
  if (state === 'done' || state === 'dropped') {
    return [{ to: 'filed', kind: 'reopen', label: 'Reopen', input: 'reason' }];
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
    if (state === 'in-progress') {
      moves.push({ to: 'done', kind: 'done', label: 'Done', input: 'note' });
    }
    for (const back of backward[state] ?? []) {
      moves.push({ to: back, kind: 'backward', label: `Back to ${back}`, input: 'reason' });
    }
    moves.push({ to: 'blocked', kind: 'block', label: 'Block', input: 'block' });
  }
  moves.push({ to: 'dropped', kind: 'drop', label: 'Drop', input: 'reason' });
  return moves;
}
