import { Block, Ticket, TicketState } from '../api/models';
import { Move, MoveKind, movesFrom, movesOf } from './transitions';

const states: TicketState[] = [
  'filed',
  'analysed',
  'decided',
  'in-progress',
  'review',
  'blocked',
  'done',
  'dropped',
];

/**
 * The matrix of docs/adr/0009 D2-D5, copied from TestClassifyMove in
 * backend/internal/domain/domain_test.go: every pair that moves, and how. That test classifies with
 * the origin `decided`, so the one unblock pair is blocked to decided and the one withdrawal done
 * to decided; the withdrawal is offered for a done by hand, which this table reads it as. A change
 * to the backend's table is a change to this one, and the other way round.
 */
const matrix: Record<string, MoveKind> = {
  'filed>analysed': 'forward',
  'analysed>decided': 'forward',
  'decided>in-progress': 'forward',
  'in-progress>review': 'forward',
  'in-progress>decided': 'backward',
  'in-progress>analysed': 'backward',
  'decided>analysed': 'backward',
  'review>in-progress': 'backward',
  'filed>blocked': 'block',
  'analysed>blocked': 'block',
  'decided>blocked': 'block',
  'in-progress>blocked': 'block',
  'review>blocked': 'block',
  'blocked>decided': 'unblock',
  'filed>done': 'done',
  'analysed>done': 'done',
  'decided>done': 'done',
  'in-progress>done': 'done',
  'review>done': 'done',
  'blocked>done': 'done',
  'done>decided': 'withdraw',
  'filed>dropped': 'drop',
  'analysed>dropped': 'drop',
  'decided>dropped': 'drop',
  'in-progress>dropped': 'drop',
  'review>dropped': 'drop',
  'blocked>dropped': 'drop',
  'dropped>filed': 'reopen',
};

/** The states a ticket is blocked from, or done from: any open state but blocked, or any open state. */
const blockOrigins: TicketState[] = ['filed', 'analysed', 'decided', 'in-progress', 'review'];
const doneOrigins: TicketState[] = [...blockOrigins, 'blocked'];

const kinds = (moves: Move[]) => moves.map((move) => [move.to, move.kind]);

describe('movesFrom', () => {
  describe.each(states)('from %s', (from) => {
    it('offers exactly the moves of the matrix, each of its kind', () => {
      const offered = Object.fromEntries(
        movesFrom(from, 'decided', true).map((move) => [move.to, move.kind]),
      );
      const expected = Object.fromEntries(
        states.filter((to) => matrix[`${from}>${to}`]).map((to) => [to, matrix[`${from}>${to}`]]),
      );

      expect(offered).toEqual(expected);
    });

    it('offers no move twice and none to its own state', () => {
      const targets = movesFrom(from, 'decided', true).map((move) => move.to);

      expect(new Set(targets).size).toBe(targets.length);
      expect(targets).not.toContain(from);
    });
  });

  describe.each(states.filter((state) => state !== 'blocked' && state !== 'done'))(
    'from %s, which has no use for an origin',
    (from) => {
      it('offers the same moves with an origin, a null origin and none, done by hand or not', () => {
        expect(movesFrom(from, 'decided')).toEqual(movesFrom(from));
        expect(movesFrom(from, null)).toEqual(movesFrom(from));
        expect(movesFrom(from, 'decided', true)).toEqual(movesFrom(from));
      });
    },
  );

  describe('from blocked', () => {
    it.each(blockOrigins)(
      'leaves only back to %s, where it came from, done by hand, or to dropped',
      (origin) => {
        expect(kinds(movesFrom('blocked', origin))).toEqual([
          [origin, 'unblock'],
          ['done', 'done'],
          ['dropped', 'drop'],
        ]);
      },
    );

    it('can only be done by hand or dropped when no origin is known', () => {
      const closing = [
        ['done', 'done'],
        ['dropped', 'drop'],
      ];
      expect(kinds(movesFrom('blocked'))).toEqual(closing);
      expect(kinds(movesFrom('blocked', null))).toEqual(closing);
      expect(kinds(movesFrom('blocked', undefined))).toEqual(closing);
    });

    it('cannot be blocked again, nor moved back or forward', () => {
      const offered = movesFrom('blocked', 'decided').map((move) => move.kind);

      expect(offered).not.toContain('block');
      expect(offered).not.toContain('backward');
      expect(offered).not.toContain('forward');
    });
  });

  describe('review (docs/adr/0009 D1, D3)', () => {
    it('is the step forward from in-progress, which needs nothing', () => {
      expect(movesFrom('in-progress')[0]).toEqual({
        to: 'review',
        kind: 'forward',
        label: 'Move to review',
        input: 'none',
      });
    });

    it('has no step forward: it ends in done', () => {
      expect(movesFrom('review').map((move) => move.kind)).not.toContain('forward');
    });

    it('goes back to in-progress with a reason, when the check found work to do', () => {
      expect(movesFrom('review')).toContainEqual({
        to: 'in-progress',
        kind: 'backward',
        label: 'Back to in-progress',
        input: 'reason',
      });
    });
  });

  describe('done by hand (docs/adr/0009 D5)', () => {
    it.each(doneOrigins)('is offered from %s, with the verification note', (from) => {
      expect(movesFrom(from, 'decided')).toContainEqual({
        to: 'done',
        kind: 'done',
        label: 'Done by hand',
        input: 'note',
      });
    });
  });

  describe('from done', () => {
    it.each(doneOrigins)(
      'by hand, withdraws to %s, where it was done from, with a reason',
      (origin) => {
        expect(movesFrom('done', origin, true)).toEqual([
          { to: origin, kind: 'withdraw', label: 'Withdraw done', input: 'reason' },
        ]);
      },
    );

    it('by its stages, offers nothing: a lower stage is its way out', () => {
      expect(movesFrom('done', 'review', false)).toEqual([]);
      expect(movesFrom('done', 'review')).toEqual([]);
    });

    it('offers nothing when it does not say where it was done from', () => {
      expect(movesFrom('done', null, true)).toEqual([]);
      expect(movesFrom('done', undefined, true)).toEqual([]);
    });

    it('is never reopened to filed: that is the way out of dropped only', () => {
      expect(movesFrom('done', 'review', true).map((move) => move.to)).not.toContain('filed');
    });
  });

  describe('from dropped', () => {
    it('can only be reopened to filed, with a reason, whatever else is said', () => {
      const reopen = [{ to: 'filed', kind: 'reopen', label: 'Reopen', input: 'reason' }];

      expect(movesFrom('dropped')).toEqual(reopen);
      expect(movesFrom('dropped', 'decided', true)).toEqual(reopen);
    });
  });

  describe('the order of the buttons', () => {
    it('is the way forward, done, the ways back, the block and the drop', () => {
      expect(kinds(movesFrom('review'))).toEqual([
        ['done', 'done'],
        ['in-progress', 'backward'],
        ['blocked', 'block'],
        ['dropped', 'drop'],
      ]);
      expect(kinds(movesFrom('in-progress'))).toEqual([
        ['review', 'forward'],
        ['done', 'done'],
        ['decided', 'backward'],
        ['analysed', 'backward'],
        ['blocked', 'block'],
        ['dropped', 'drop'],
      ]);
      expect(kinds(movesFrom('decided'))).toEqual([
        ['in-progress', 'forward'],
        ['done', 'done'],
        ['analysed', 'backward'],
        ['blocked', 'block'],
        ['dropped', 'drop'],
      ]);
      expect(kinds(movesFrom('analysed'))).toEqual([
        ['decided', 'forward'],
        ['done', 'done'],
        ['blocked', 'block'],
        ['dropped', 'drop'],
      ]);
      expect(kinds(movesFrom('filed'))).toEqual([
        ['analysed', 'forward'],
        ['done', 'done'],
        ['blocked', 'block'],
        ['dropped', 'drop'],
      ]);
    });
  });

  describe('what each kind of move asks for before it is sent (docs/adr/0009 D3-D5)', () => {
    const all = (): Move[] => [
      ...states.flatMap((state) => movesFrom(state, 'decided', true)),
      ...blockOrigins.flatMap((origin) => movesFrom('blocked', origin)),
      ...doneOrigins.flatMap((origin) => movesFrom('done', origin, true)),
    ];
    const inputOf = (kind: MoveKind) => [
      ...new Set(
        all()
          .filter((move) => move.kind === kind)
          .map((move) => move.input),
      ),
    ];

    it.each([
      ['forward', 'none'],
      ['unblock', 'none'],
      ['done', 'note'],
      ['backward', 'reason'],
      ['withdraw', 'reason'],
      ['drop', 'reason'],
      ['reopen', 'reason'],
      ['block', 'block'],
    ] as const)('a %s move needs input: %s', (kind, input) => {
      expect(inputOf(kind)).toEqual([input]);
    });

    it('asks for a reason exactly where the backend does: backward, drop, reopen, withdraw, and the text of a block', () => {
      const needsReason = (move: Move) => move.input === 'reason' || move.input === 'block';

      expect(
        new Set(
          all()
            .filter(needsReason)
            .map((move) => move.kind),
        ),
      ).toEqual(new Set(['backward', 'drop', 'reopen', 'withdraw', 'block']));
    });
  });

  describe('the words of the buttons', () => {
    it('spell the states as the API spells them (docs/adr/0055 D4)', () => {
      const labels = (state: TicketState, origin?: TicketState, doneByHand?: boolean) =>
        movesFrom(state, origin, doneByHand).map((move) => move.label);

      expect(labels('filed')).toEqual(['Move to analysed', 'Done by hand', 'Block', 'Drop']);
      expect(labels('decided')).toEqual([
        'Move to in-progress',
        'Done by hand',
        'Back to analysed',
        'Block',
        'Drop',
      ]);
      expect(labels('in-progress')).toEqual([
        'Move to review',
        'Done by hand',
        'Back to decided',
        'Back to analysed',
        'Block',
        'Drop',
      ]);
      expect(labels('review')).toEqual(['Done by hand', 'Back to in-progress', 'Block', 'Drop']);
      expect(labels('blocked', 'in-progress')).toEqual([
        'Unblock to in-progress',
        'Done by hand',
        'Drop',
      ]);
      expect(labels('done', 'review', true)).toEqual(['Withdraw done']);
      expect(labels('dropped')).toEqual(['Reopen']);
    });
  });

  it('gives a fresh list every time, so that a caller may change it', () => {
    const first = movesFrom('filed');

    first.pop();

    expect(movesFrom('filed')).toHaveLength(4);
  });
});

describe('movesOf', () => {
  const ticket = (fields: Partial<Ticket>) =>
    ({ state: 'filed', block: null, done_from: null, done_by_hand: false, ...fields }) as Ticket;

  it('reads where a blocked ticket came from out of its block', () => {
    const block = { kind: 'human', from: 'review', reason: 'Waiting' } as Block;

    expect(kinds(movesOf(ticket({ state: 'blocked', block })))[0]).toEqual(['review', 'unblock']);
  });

  it('reads where a done ticket was done from, and whether by hand', () => {
    expect(
      movesOf(ticket({ state: 'done', done_from: 'in-progress', done_by_hand: true })),
    ).toEqual([{ to: 'in-progress', kind: 'withdraw', label: 'Withdraw done', input: 'reason' }]);
    expect(
      movesOf(ticket({ state: 'done', done_from: 'in-progress', done_by_hand: false })),
    ).toEqual([]);
  });

  it('is movesFrom of the state for every other ticket', () => {
    for (const state of [
      'filed',
      'analysed',
      'decided',
      'in-progress',
      'review',
      'dropped',
    ] as const) {
      expect(movesOf(ticket({ state }))).toEqual(movesFrom(state));
    }
  });
});
