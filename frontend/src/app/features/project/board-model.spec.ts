import { Block, Ticket, TicketState, Urgency } from '../../api/models';
import {
  arrange,
  boardUrgencies,
  cardAction,
  ColumnId,
  columnOf,
  columnSpecs,
  dropMove,
  dropTargets,
  menuMoves,
  placeOf,
  specOf,
} from './board-model';

function ticket(number: number, fields: Partial<Ticket> = {}): Ticket {
  return {
    key: `acme/COW-${number}`,
    number,
    state: 'in-progress',
    urgency: 'now',
    progress_derived: false,
    block: null,
    done_from: null,
    done_by_hand: false,
    ...fields,
  } as Ticket;
}

const blockedFrom = (from: TicketState, fields: Partial<Ticket> = {}) =>
  ticket(1, {
    state: 'blocked',
    block: { kind: 'external', from, reason: 'The provider' } as Block,
    ...fields,
  });

const keys = (tickets: Ticket[]) => tickets.map((each) => each.number);

describe('the columns (docs/adr/0018 D1, docs/adr/0019 D3)', () => {
  it('are Refinement, Ready, In Progress, Blocked and Review, each over its states and counted against a limit', () => {
    expect(columnSpecs.map(({ id, title, states, limit }) => [id, title, states, limit])).toEqual([
      ['refinement', 'Refinement', ['filed', 'analysed'], 'analysed'],
      ['ready', 'Ready', ['decided'], 'decided'],
      ['in-progress', 'In Progress', ['in-progress'], 'in-progress'],
      ['blocked', 'Blocked', ['blocked'], 'blocked'],
      ['review', 'Review', ['review'], 'review'],
    ]);
  });

  it.each([
    ['filed', 'refinement'],
    ['analysed', 'refinement'],
    ['decided', 'ready'],
    ['in-progress', 'in-progress'],
    ['blocked', 'blocked'],
    ['review', 'review'],
    ['done', null],
    ['dropped', null],
  ] as [TicketState, ColumnId | null][])('put %s in %s', (state, column) => {
    expect(columnOf(state)).toBe(column);
  });

  it('are found by their id', () => {
    expect(specOf('review').title).toBe('Review');
  });

  it('show the urgencies now, release and next', () => {
    expect(boardUrgencies).toEqual(['now', 'release', 'next']);
  });
});

describe('placeOf', () => {
  it.each(['now', 'release'] as Urgency[])(
    'puts an open leaf of urgency %s in the column of its state',
    (urgency) => {
      expect(placeOf(ticket(1, { urgency, state: 'filed' }))).toBe('refinement');
      expect(placeOf(ticket(1, { urgency, state: 'decided' }))).toBe('ready');
      expect(placeOf(ticket(1, { urgency, state: 'review' }))).toBe('review');
    },
  );

  it.each(['filed', 'decided', 'in-progress', 'review', 'blocked'] as TicketState[])(
    'puts a leaf of urgency next in the column next, also when it is %s',
    (state) => {
      expect(placeOf(ticket(1, { urgency: 'next', state }))).toBe('next');
    },
  );

  it.each(['later', 'icebox'] as Urgency[])(
    'leaves a ticket of urgency %s off the board',
    (urgency) => {
      expect(placeOf(ticket(1, { urgency }))).toBeNull();
    },
  );

  it('leaves a parent off the board: its children carry the work', () => {
    expect(placeOf(ticket(1, { progress_derived: true }))).toBeNull();
    expect(placeOf(ticket(1, { progress_derived: true, urgency: 'next' }))).toBeNull();
  });

  it.each(['done', 'dropped'] as TicketState[])('leaves a %s ticket off the board', (state) => {
    expect(placeOf(ticket(1, { state }))).toBeNull();
    expect(placeOf(ticket(1, { state, urgency: 'next' }))).toBeNull();
  });
});

describe('arrange', () => {
  const tickets = [
    ticket(1, { state: 'in-progress' }),
    ticket(2, { state: 'filed', urgency: 'next' }),
    ticket(3, { state: 'analysed' }),
    ticket(4, { state: 'filed', urgency: 'release' }),
    ticket(5, { state: 'review', urgency: 'later' }),
    ticket(6, { state: 'in-progress' }),
    ticket(7, { state: 'decided', urgency: 'next' }),
  ];

  it('puts every ticket where it belongs, in the order it came in, which is the rank', () => {
    const board = arrange(tickets, placeOf, {});

    expect(keys(board.next)).toEqual([2, 7]);
    expect(board.columns.map((column) => [column.id, keys(column.cards)])).toEqual([
      ['refinement', [3, 4]],
      ['ready', []],
      ['in-progress', [1, 6]],
      ['blocked', []],
      ['review', []],
    ]);
  });

  it('shows a ticket where the place it is given says, as a move on its way does', () => {
    const board = arrange(tickets, (each) => (each.number === 1 ? 'review' : placeOf(each)), {});

    expect(keys(board.columns.find((column) => column.id === 'review')!.cards)).toEqual([1]);
    expect(keys(board.columns.find((column) => column.id === 'in-progress')!.cards)).toEqual([6]);
  });

  it('counts each column against its limit, Refinement against the analysed one, and marks a column over it', () => {
    const board = arrange(tickets, placeOf, { analysed: 1, 'in-progress': 2, review: 3 });
    const of = (id: ColumnId) => board.columns.find((column) => column.id === id)!;

    expect([of('refinement').max, of('refinement').over]).toEqual([1, true]);
    expect([of('in-progress').max, of('in-progress').over]).toEqual([2, false]);
    expect([of('review').max, of('review').over]).toEqual([3, false]);
    expect([of('ready').max, of('ready').over]).toEqual([undefined, false]);
  });

  it('marks no column of a project without limits', () => {
    expect(arrange(tickets, placeOf, {}).columns.some((column) => column.over)).toBe(false);
  });
});

describe('dropMove (docs/adr/0009 through the board)', () => {
  it.each([
    ['analysed', 'ready', 'decided', 'forward', 'none'],
    ['decided', 'in-progress', 'in-progress', 'forward', 'none'],
    ['in-progress', 'review', 'review', 'forward', 'none'],
    ['decided', 'refinement', 'analysed', 'backward', 'reason'],
    ['in-progress', 'ready', 'decided', 'backward', 'reason'],
    ['in-progress', 'refinement', 'analysed', 'backward', 'reason'],
    ['review', 'in-progress', 'in-progress', 'backward', 'reason'],
    ['filed', 'blocked', 'blocked', 'block', 'block'],
    ['analysed', 'blocked', 'blocked', 'block', 'block'],
    ['decided', 'blocked', 'blocked', 'block', 'block'],
    ['in-progress', 'blocked', 'blocked', 'block', 'block'],
    ['review', 'blocked', 'blocked', 'block', 'block'],
  ] as [TicketState, ColumnId, TicketState, string, string][])(
    'moves a %s card dropped on %s to %s, a %s move that asks for %s',
    (state, column, to, kind, input) => {
      expect(dropMove(ticket(1, { state }), column)).toEqual(
        expect.objectContaining({ to, kind, input }),
      );
    },
  );

  it.each([
    ['filed', 'refinement'],
    ['analysed', 'refinement'],
    ['decided', 'ready'],
    ['in-progress', 'in-progress'],
    ['review', 'review'],
  ] as [TicketState, ColumnId][])(
    'unblocks a card blocked from %s when it is dropped on %s, which asks for nothing',
    (from, column) => {
      expect(dropMove(blockedFrom(from), column)).toEqual(
        expect.objectContaining({ to: from, kind: 'unblock', input: 'none' }),
      );
    },
  );

  it('is no move for a blocked card dropped back on Blocked', () => {
    expect(dropMove(blockedFrom('decided'), 'blocked')).toBeNull();
  });

  it.each([
    ['filed', 'ready'],
    ['filed', 'in-progress'],
    ['filed', 'review'],
    ['analysed', 'in-progress'],
    ['analysed', 'review'],
    ['decided', 'review'],
    ['review', 'refinement'],
    ['review', 'ready'],
  ] as [TicketState, ColumnId][])(
    'refuses a %s card on %s, where the matrix has no move',
    (state, column) => {
      expect(dropMove(ticket(1, { state }), column)).toBeNull();
    },
  );

  it('refuses a blocked card on every column but the one it came from', () => {
    expect(dropTargets(blockedFrom('analysed'))).toEqual(new Set(['refinement']));
  });

  it('is no move inside the own column: filed to analysed is a card action, not a drop', () => {
    expect(dropMove(ticket(1, { state: 'filed' }), 'refinement')).toBeNull();
    expect(dropMove(ticket(1, { state: 'analysed' }), 'refinement')).toBeNull();
  });

  it('never closes, drops or reopens a ticket: those are the moves of the detail page', () => {
    for (const state of [
      'filed',
      'analysed',
      'decided',
      'in-progress',
      'review',
    ] as TicketState[]) {
      for (const spec of columnSpecs) {
        const move = dropMove(ticket(1, { state }), spec.id);
        expect(['forward', 'backward', 'block', 'unblock', undefined]).toContain(move?.kind);
      }
    }
  });
});

describe('dropTargets', () => {
  it.each([
    ['filed', ['blocked']],
    ['analysed', ['ready', 'blocked']],
    ['decided', ['refinement', 'in-progress', 'blocked']],
    ['in-progress', ['refinement', 'ready', 'blocked', 'review']],
    ['review', ['in-progress', 'blocked']],
  ] as [TicketState, ColumnId[]][])('lets a %s card go to %j', (state, columns) => {
    expect(dropTargets(ticket(1, { state }))).toEqual(new Set(columns));
  });
});

describe('cardAction', () => {
  it('is filed to analysed, which stays in Refinement', () => {
    expect(cardAction(ticket(1, { state: 'filed' }))).toEqual(
      expect.objectContaining({ to: 'analysed', kind: 'forward', input: 'none' }),
    );
  });

  it.each(['analysed', 'decided', 'in-progress', 'review', 'blocked'] as TicketState[])(
    'is none for a %s card, whose moves leave its column',
    (state) => {
      expect(cardAction(ticket(1, { state }))).toBeNull();
    },
  );
});

describe('menuMoves', () => {
  const labels = (fields: Partial<Ticket>) =>
    menuMoves(ticket(1, fields)).map((item) => [item.label, item.column, item.move.to]);

  it('offers the card action first, then the drops in the order of the columns', () => {
    expect(labels({ state: 'filed' })).toEqual([
      ['Move to analysed', 'refinement', 'analysed'],
      ['Block…', 'blocked', 'blocked'],
    ]);
  });

  it('words each move as the detail page does, with … where it asks for something first', () => {
    expect(labels({ state: 'in-progress' })).toEqual([
      ['Back to Refinement…', 'refinement', 'analysed'],
      ['Back to Ready…', 'ready', 'decided'],
      ['Block…', 'blocked', 'blocked'],
      ['Move to Review', 'review', 'review'],
    ]);
    expect(labels({ state: 'review' })).toEqual([
      ['Back to In Progress…', 'in-progress', 'in-progress'],
      ['Block…', 'blocked', 'blocked'],
    ]);
  });

  it('offers a blocked card the way back to where it came from', () => {
    expect(
      menuMoves(blockedFrom('decided')).map((item) => [item.label, item.column, item.move.kind]),
    ).toEqual([['Unblock to Ready', 'ready', 'unblock']]);
  });

  it('offers nothing to a blocked card that does not say where it came from', () => {
    expect(menuMoves(ticket(1, { state: 'blocked', block: null }))).toEqual([]);
  });
});
