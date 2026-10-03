import { TicketState } from '../api/models';
import { Move, MoveKind, movesFrom } from './transitions';

const states: TicketState[] = [
  'filed',
  'analysed',
  'decided',
  'in-progress',
  'blocked',
  'done',
  'dropped',
];

/**
 * The matrix of docs/adr/0009 D2-D5, copied from TestClassifyMove in
 * backend/internal/domain/domain_test.go: every pair that moves, and how. That test classifies with
 * the origin `decided`, so the one unblock pair is blocked to decided. A change to the backend's
 * table is a change to this one, and the other way round.
 */
const matrix: Record<string, MoveKind> = {
  'filed>analysed': 'forward',
  'analysed>decided': 'forward',
  'decided>in-progress': 'forward',
  'in-progress>done': 'done',
  'in-progress>decided': 'backward',
  'in-progress>analysed': 'backward',
  'decided>analysed': 'backward',
  'filed>blocked': 'block',
  'analysed>blocked': 'block',
  'decided>blocked': 'block',
  'in-progress>blocked': 'block',
  'blocked>decided': 'unblock',
  'filed>dropped': 'drop',
  'analysed>dropped': 'drop',
  'decided>dropped': 'drop',
  'in-progress>dropped': 'drop',
  'blocked>dropped': 'drop',
  'done>filed': 'reopen',
  'dropped>filed': 'reopen',
};

/** The states a ticket is blocked from: any open state but blocked. */
const origins: TicketState[] = ['filed', 'analysed', 'decided', 'in-progress'];

const kinds = (moves: Move[]) => moves.map((move) => [move.to, move.kind]);

describe('movesFrom', () => {
  describe.each(states)('from %s', (from) => {
    it('offers exactly the moves of the matrix, each of its kind', () => {
      const offered = Object.fromEntries(
        movesFrom(from, 'decided').map((move) => [move.to, move.kind]),
      );
      const expected = Object.fromEntries(
        states.filter((to) => matrix[`${from}>${to}`]).map((to) => [to, matrix[`${from}>${to}`]]),
      );

      expect(offered).toEqual(expected);
    });

    it('offers no move twice and none to its own state', () => {
      const targets = movesFrom(from, 'decided').map((move) => move.to);

      expect(new Set(targets).size).toBe(targets.length);
      expect(targets).not.toContain(from);
    });
  });

  describe.each(states.filter((state) => state !== 'blocked'))(
    'from %s, which has no use for an origin',
    (from) => {
      it('offers the same moves with an origin, a null origin and none', () => {
        expect(movesFrom(from, 'decided')).toEqual(movesFrom(from));
        expect(movesFrom(from, null)).toEqual(movesFrom(from));
      });
    },
  );

  describe('from blocked', () => {
    it.each(origins)('leaves only back to %s, where it came from, or to dropped', (origin) => {
      const moves = movesFrom('blocked', origin);

      expect(kinds(moves)).toEqual([
        [origin, 'unblock'],
        ['dropped', 'drop'],
      ]);
    });

    it('can only be dropped when no origin is known', () => {
      expect(kinds(movesFrom('blocked'))).toEqual([['dropped', 'drop']]);
      expect(kinds(movesFrom('blocked', null))).toEqual([['dropped', 'drop']]);
      expect(kinds(movesFrom('blocked', undefined))).toEqual([['dropped', 'drop']]);
    });

    it('cannot be blocked again', () => {
      expect(movesFrom('blocked', 'decided').map((move) => move.kind)).not.toContain('block');
    });

    it('leaves to no other state than the origin and dropped', () => {
      const targets = movesFrom('blocked', 'analysed').map((move) => move.to);

      expect(states.filter((to) => targets.includes(to))).toEqual(['analysed', 'dropped']);
    });
  });

  describe.each(['done', 'dropped'] as const)('from %s', (from) => {
    it('can only be reopened to filed, with a reason', () => {
      expect(movesFrom(from)).toEqual([
        { to: 'filed', kind: 'reopen', label: 'Reopen', input: 'reason' },
      ]);
    });

    it('is reopened the same way with an origin that means nothing here', () => {
      expect(movesFrom(from, 'decided')).toEqual(movesFrom(from));
    });
  });

  describe('the order of the buttons', () => {
    it('is the way forward, done, the ways back, the block and the drop', () => {
      expect(kinds(movesFrom('in-progress'))).toEqual([
        ['done', 'done'],
        ['decided', 'backward'],
        ['analysed', 'backward'],
        ['blocked', 'block'],
        ['dropped', 'drop'],
      ]);
      expect(kinds(movesFrom('decided'))).toEqual([
        ['in-progress', 'forward'],
        ['analysed', 'backward'],
        ['blocked', 'block'],
        ['dropped', 'drop'],
      ]);
      expect(kinds(movesFrom('analysed'))).toEqual([
        ['decided', 'forward'],
        ['blocked', 'block'],
        ['dropped', 'drop'],
      ]);
      expect(kinds(movesFrom('filed'))).toEqual([
        ['analysed', 'forward'],
        ['blocked', 'block'],
        ['dropped', 'drop'],
      ]);
    });
  });

  describe('what each kind of move asks for before it is sent (docs/adr/0009 D3-D5)', () => {
    const all = (): Move[] => [
      ...states.flatMap((state) => movesFrom(state, 'decided')),
      ...origins.flatMap((origin) => movesFrom('blocked', origin)),
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
      ['drop', 'reason'],
      ['reopen', 'reason'],
      ['block', 'block'],
    ] as const)('a %s move needs input: %s', (kind, input) => {
      expect(inputOf(kind)).toEqual([input]);
    });

    it('asks for a reason exactly where the backend does: backward, drop, reopen, and the text of a block', () => {
      const needsReason = (move: Move) => move.input === 'reason' || move.input === 'block';

      expect(
        new Set(
          all()
            .filter(needsReason)
            .map((move) => move.kind),
        ),
      ).toEqual(new Set(['backward', 'drop', 'reopen', 'block']));
    });
  });

  describe('the words of the buttons', () => {
    it('spell the states as the API spells them (docs/adr/0055 D4)', () => {
      const labels = (state: TicketState, origin?: TicketState) =>
        movesFrom(state, origin).map((move) => move.label);

      expect(labels('filed')).toEqual(['Move to analysed', 'Block', 'Drop']);
      expect(labels('decided')).toEqual([
        'Move to in-progress',
        'Back to analysed',
        'Block',
        'Drop',
      ]);
      expect(labels('in-progress')).toEqual([
        'Done',
        'Back to decided',
        'Back to analysed',
        'Block',
        'Drop',
      ]);
      expect(labels('blocked', 'in-progress')).toEqual(['Unblock to in-progress', 'Drop']);
      expect(labels('done')).toEqual(['Reopen']);
    });
  });

  it('gives a fresh list every time, so that a caller may change it', () => {
    const first = movesFrom('filed');

    first.pop();

    expect(movesFrom('filed')).toHaveLength(3);
  });
});
