import { Ticket, Urgency } from '../../api/models';
import {
  alwaysShown,
  arrange,
  Group,
  movedKeys,
  Placement,
  planDrop,
  planGroup,
  planStep,
  position,
  Move,
  rankBody,
  Row,
  scoreMarks,
  unanswered,
  urgencies,
  withMoves,
} from './backlog-model';

/** The part of a ticket that the model reads. */
function t(number: number, overrides: Partial<Ticket> = {}): Ticket {
  return {
    key: `acme/COW-${number}`,
    number,
    parent: null,
    urgency: 'later',
    ...overrides,
  } as Ticket;
}

const under = (parent: number) => `acme/COW-${parent}`;
const keys = (rows: readonly Row[]) => rows.map((row) => row.ticket.number);
const group = (groups: Group[], urgency: Urgency) =>
  groups.find((each) => each.urgency === urgency)?.rows ?? [];
/** `after 3`, `before 5` or `none`: a placement as a test reads it. */
const shown = (placement: Placement | null | undefined) =>
  placement ? `${placement.side} ${placement.neighbour.number}` : 'none';

/** The rows of a group of tickets, as the page shows them. */
const rowsOf = (...tickets: Ticket[]) => group(arrange(tickets), tickets[0]?.urgency ?? 'later');

describe('the groups', () => {
  it('are now, release, next, later and icebox, in this order (docs/adr/0018 D1)', () => {
    expect(urgencies).toEqual(['now', 'release', 'next', 'later', 'icebox']);
    expect(arrange([]).map((each) => each.urgency)).toEqual([...urgencies]);
  });

  it('show while empty only for now, next and later; release and icebox wait for a ticket or a drag', () => {
    expect([...alwaysShown].sort()).toEqual(['later', 'next', 'now']);
    expect(alwaysShown.has('release')).toBe(false);
    expect(alwaysShown.has('icebox')).toBe(false);
  });
});

describe('arrange', () => {
  it('has an empty group for every urgency that no ticket has', () => {
    const groups = arrange([t(1, { urgency: 'now' })]);

    expect(groups).toHaveLength(5);
    expect(keys(group(groups, 'now'))).toEqual([1]);
    expect(group(groups, 'release')).toEqual([]);
    expect(group(groups, 'icebox')).toEqual([]);
  });

  it('puts each ticket into the group of its urgency and keeps the rank inside it', () => {
    const groups = arrange([
      t(5, { urgency: 'later' }),
      t(2, { urgency: 'now' }),
      t(9, { urgency: 'later' }),
      t(1, { urgency: 'icebox' }),
      t(7, { urgency: 'now' }),
      t(3, { urgency: 'release' }),
      t(4, { urgency: 'next' }),
    ]);

    expect(keys(group(groups, 'now'))).toEqual([2, 7]);
    expect(keys(group(groups, 'release'))).toEqual([3]);
    expect(keys(group(groups, 'next'))).toEqual([4]);
    expect(keys(group(groups, 'later'))).toEqual([5, 9]);
    expect(keys(group(groups, 'icebox'))).toEqual([1]);
  });

  it('shows a ticket in the group that urgencyOf names, which is how a drag shows at once', () => {
    const groups = arrange([t(1, { urgency: 'later' }), t(2, { urgency: 'later' })], (ticket) =>
      ticket.number === 2 ? 'now' : ticket.urgency,
    );

    expect(keys(group(groups, 'later'))).toEqual([1]);
    expect(keys(group(groups, 'now'))).toEqual([2]);
  });

  it('indents a child under its parent where both are in the group, wherever the child is ranked', () => {
    const rows = rowsOf(t(1), t(2, { parent: under(3) }), t(3), t(4, { parent: under(3) }), t(5));

    expect(rows.map((row) => [row.ticket.number, row.depth, row.under])).toEqual([
      [1, 0, null],
      [3, 0, null],
      [2, 1, under(3)],
      [4, 1, under(3)],
      [5, 0, null],
    ]);
  });

  it('keeps the children of a parent in their rank, and the parents in theirs', () => {
    const rows = rowsOf(t(1, { parent: under(9) }), t(9), t(2, { parent: under(9) }), t(3));

    expect(keys(rows)).toEqual([9, 1, 2, 3]);
  });

  it('indents a grandchild twice', () => {
    const rows = rowsOf(t(1), t(2, { parent: under(1) }), t(3, { parent: under(2) }));

    expect(rows.map((row) => [row.ticket.number, row.depth, row.under])).toEqual([
      [1, 0, null],
      [2, 1, under(1)],
      [3, 2, under(2)],
    ]);
  });

  it('names the parent of a ticket whose parent sits in another group, and indents nothing', () => {
    const groups = arrange([
      t(1, { urgency: 'now' }),
      t(2, { urgency: 'later', parent: under(1) }),
    ]);

    const [child] = group(groups, 'later');
    expect(child.depth).toBe(0);
    expect(child.under).toBeNull();
    expect(child.elsewhere).toBe(under(1));
  });

  it('names the parent of a ticket whose parent is not in the list at all, as a filter leaves it', () => {
    const [child] = rowsOf(t(2, { parent: under(1) }));

    expect(child.elsewhere).toBe(under(1));
    expect(child.depth).toBe(0);
  });

  it('names no parent for a ticket that has none, nor for one that stands under its parent', () => {
    const rows = rowsOf(t(1), t(2, { parent: under(1) }));

    expect(rows.map((row) => row.elsewhere)).toEqual([null, null]);
  });

  it('keeps the members of a cycle in the list instead of dropping them or looping', () => {
    // Two cached tickets of different times can name each other as parent for a moment.
    const rows = rowsOf(t(1, { parent: under(2) }), t(2, { parent: under(1) }), t(3));

    expect(keys(rows).sort()).toEqual([1, 2, 3]);
    expect(rows).toHaveLength(3);
  });

  it('keeps a ticket that names itself as its parent', () => {
    const rows = rowsOf(t(1, { parent: under(1) }));

    expect(keys(rows)).toEqual([1]);
  });
});

describe('position', () => {
  const rows = rowsOf(t(1), t(2, { parent: under(1) }), t(3, { parent: under(1) }), t(4), t(5));

  it('counts the siblings above a root and those besides it', () => {
    expect(position(rows, t(4))).toEqual({ index: 1, count: 2 });
    expect(position(rows, t(1))).toEqual({ index: 0, count: 2 });
    expect(position(rows, t(5, {}))).toEqual({ index: 2, count: 2 });
  });

  it('counts a child among the children of its parent only', () => {
    expect(position(rows, t(2, { parent: under(1) }))).toEqual({ index: 0, count: 1 });
    expect(position(rows, t(3, { parent: under(1) }))).toEqual({ index: 1, count: 1 });
  });

  it('is null for a ticket that is not in the rows', () => {
    expect(position(rows, t(9))).toBeNull();
  });
});

describe('planDrop within a group', () => {
  // The rank order A B C D; a drop at `at` is an index into the rows without the dragged one.
  const [a, b, c, d] = [t(1), t(2), t(3), t(4)];
  const rows = rowsOf(a, b, c, d);
  const drop = (ticket: Ticket, at: number) => planDrop(ticket, 'later', 'later', rows, at);

  it('places a row that lands at the top of the group before the row below it', () => {
    expect(shown(drop(c, 0)?.placement)).toBe('before 1');
  });

  it('places a row that lands in the middle after the row above it', () => {
    const plan = drop(a, 2);

    expect(shown(plan?.placement)).toBe('after 3');
    expect(plan?.ticket).toBe(a);
    expect(plan?.urgency).toBeUndefined();
  });

  it('places a row that lands at the bottom after the last row', () => {
    expect(shown(drop(a, 3)?.placement)).toBe('after 4');
  });

  it('places a row that moves up by one after the row above its new place', () => {
    expect(shown(drop(c, 1)?.placement)).toBe('after 1');
  });

  it.each([
    ['the top', a, 0],
    ['the middle', b, 1],
    ['the bottom', d, 3],
  ])('asks for nothing when a row is dropped where it was, at %s', (_, ticket, at) => {
    expect(drop(ticket, at)).toBeNull();
  });

  it('asks for nothing for the only row of a group, which has no neighbour to be placed by', () => {
    const only = rowsOf(a);

    expect(planDrop(a, 'later', 'later', only, 0)).toBeNull();
  });

  describe('where a family stands together', () => {
    // The tree: P with the children X and Y, then R and S.
    const p = t(1);
    const x = t(2, { parent: under(1) });
    const y = t(3, { parent: under(1) });
    const r = t(4);
    const s = t(5);
    const family = rowsOf(p, x, y, r, s);
    const dropInFamily = (ticket: Ticket, at: number) =>
      planDrop(ticket, 'later', 'later', family, at);

    it.each([
      ['right under the parent', 1],
      ['between the children', 2],
      ['under the last child', 3],
    ])('puts a root dropped %s behind the whole family', (_, at) => {
      expect(shown(dropInFamily(s, at)?.placement)).toBe('after 1');
    });

    it('puts a root dropped after the last root behind that one', () => {
      expect(shown(dropInFamily(r, 4)?.placement)).toBe('after 5');
    });

    it('puts a root dropped at the top before the first root', () => {
      expect(shown(dropInFamily(s, 0)?.placement)).toBe('before 1');
    });

    it('asks for nothing when a parent is dropped below its own children, where it stands anyway', () => {
      expect(dropInFamily(p, 2)).toBeNull();
    });

    it('moves a parent past the next root, its children going with it', () => {
      expect(shown(dropInFamily(p, 3)?.placement)).toBe('after 4');
    });

    it('moves a child among its siblings only: above the first one is before it', () => {
      expect(shown(dropInFamily(y, 1)?.placement)).toBe('before 2');
    });

    it('moves a child below its sibling after it', () => {
      const plan = dropInFamily(x, 2);

      expect(shown(plan?.placement)).toBe('after 3');
    });

    it('refuses a child dropped outside the family of its parent', () => {
      expect(dropInFamily(x, 0)).toBeNull();
      expect(dropInFamily(x, 3)).toBeNull();
      expect(dropInFamily(x, 4)).toBeNull();
    });

    it('asks for nothing for a child that is dropped where it was', () => {
      expect(dropInFamily(x, 1)).toBeNull();
      expect(dropInFamily(y, 2)).toBeNull();
    });
  });
});

describe('planDrop into another group', () => {
  const x = t(11, { urgency: 'now' });
  const y = t(12, { urgency: 'now' });
  const target = rowsOf(x, y);
  const ticket = t(1, { urgency: 'later' });
  const drop = (rows: readonly Row[], at: number, subject = ticket) =>
    planDrop(subject, 'later', 'now', rows, at);

  it('sets the urgency and places the row before the first row where it lands at the top', () => {
    const plan = drop(target, 0);

    expect(plan?.urgency).toBe('now');
    expect(shown(plan?.placement)).toBe('before 11');
    expect(plan?.ticket).toBe(ticket);
  });

  it('places the row after the row above it where it lands in the middle', () => {
    expect(shown(drop(target, 1)?.placement)).toBe('after 11');
  });

  it('places the row after the last row where it lands at the bottom', () => {
    expect(shown(drop(target, 2)?.placement)).toBe('after 12');
  });

  it('sets the urgency and places nothing when the group was empty', () => {
    const plan = drop([], 0);

    expect(plan?.urgency).toBe('now');
    expect(plan?.placement).toBeNull();
  });

  it('puts a row behind the whole family where it lands inside one', () => {
    const family = rowsOf(
      t(11, { urgency: 'now' }),
      t(12, { urgency: 'now', parent: under(11) }),
      t(13, { urgency: 'now' }),
    );

    expect(shown(drop(family, 1)?.placement)).toBe('after 11');
    expect(shown(drop(family, 2)?.placement)).toBe('after 11');
    expect(shown(drop(family, 3)?.placement)).toBe('after 13');
  });

  it('is a plan whatever the place, for a child whose parent is in the group it lands in', () => {
    const child = t(1, { urgency: 'later', parent: under(11) });
    const family = rowsOf(
      t(11, { urgency: 'now' }),
      t(12, { urgency: 'now', parent: under(11) }),
      t(13, { urgency: 'now' }),
    );

    // Inside the family: among the children of the parent.
    expect(shown(drop(family, 1, child)?.placement)).toBe('before 12');
    expect(shown(drop(family, 2, child)?.placement)).toBe('after 12');
    // Outside the family it is shown under its parent anyway: the urgency is set, the rank kept.
    const outside = drop(family, 3, child);
    expect(outside?.urgency).toBe('now');
    expect(outside?.placement).toBeNull();
    expect(drop(family, 0, child)?.placement).toBeNull();
  });

  it('treats a child whose parent is in another group as a root of the group it lands in', () => {
    const child = t(1, { urgency: 'later', parent: under(99) });

    expect(shown(drop(target, 1, child)?.placement)).toBe('after 11');
  });
});

describe('planStep', () => {
  const rows = rowsOf(t(1), t(2, { parent: under(1) }), t(3, { parent: under(1) }), t(4), t(5));
  const root = (n: number) => rows.find((row) => row.ticket.number === n)?.ticket as Ticket;

  it('moves a root up by one, over the family above it', () => {
    expect(shown(planStep(rows, root(4), 0)?.placement)).toBe('before 1');
    expect(shown(planStep(rows, root(5), 1)?.placement)).toBe('after 1');
  });

  it('moves a root down by one', () => {
    expect(shown(planStep(rows, root(1), 1)?.placement)).toBe('after 4');
  });

  it('moves a root to the top and to the bottom', () => {
    expect(shown(planStep(rows, root(5), 0)?.placement)).toBe('before 1');
    expect(shown(planStep(rows, root(1), 2)?.placement)).toBe('after 5');
  });

  it('moves a child among its siblings', () => {
    expect(shown(planStep(rows, root(3), 0)?.placement)).toBe('before 2');
    expect(shown(planStep(rows, root(2), 1)?.placement)).toBe('after 3');
  });

  it('never sets an urgency', () => {
    expect(planStep(rows, root(5), 0)?.urgency).toBeUndefined();
  });

  it.each([
    ['where the row is', 4, 1],
    ['above the top', 4, -1],
    ['below the bottom', 4, 3],
  ])('asks for nothing %s', (_, number, index) => {
    expect(planStep(rows, root(number), index)).toBeNull();
  });

  it('asks for nothing for a ticket that is not in the rows', () => {
    expect(planStep(rows, t(99), 0)).toBeNull();
  });

  it('asks for nothing for a ticket that is alone', () => {
    const alone = rowsOf(t(1));

    expect(planStep(alone, t(1), 0)).toBeNull();
  });
});

describe('planGroup', () => {
  const ticket = t(1, { urgency: 'later' });

  it('puts the ticket at the end of the group and sets its urgency', () => {
    const plan = planGroup(
      rowsOf(t(11, { urgency: 'next' }), t(12, { urgency: 'next' })),
      ticket,
      'next',
    );

    expect(plan.urgency).toBe('next');
    expect(shown(plan.placement)).toBe('after 12');
    expect(plan.ticket).toBe(ticket);
  });

  it('places nothing in an empty group', () => {
    const plan = planGroup([], ticket, 'release');

    expect(plan.urgency).toBe('release');
    expect(plan.placement).toBeNull();
  });

  it('puts a child behind the children of its parent where the parent is in the group', () => {
    const child = t(1, { urgency: 'later', parent: under(11) });
    const rows = rowsOf(
      t(11, { urgency: 'now' }),
      t(12, { urgency: 'now', parent: under(11) }),
      t(13, { urgency: 'now' }),
    );

    expect(shown(planGroup(rows, child, 'now').placement)).toBe('after 12');
  });
});

describe('movedKeys', () => {
  const order = ['a', 'b', 'c', 'd'];
  const neighbour = (key: string) => ({ key, number: 0 }) as Ticket;

  it('puts the ticket directly after its neighbour', () => {
    expect(movedKeys(order, 'a', { side: 'after', neighbour: neighbour('c') })).toEqual([
      'b',
      'c',
      'a',
      'd',
    ]);
  });

  it('puts the ticket directly before its neighbour', () => {
    expect(movedKeys(order, 'd', { side: 'before', neighbour: neighbour('b') })).toEqual([
      'a',
      'd',
      'b',
      'c',
    ]);
  });

  it('moves a ticket to the end and to the front', () => {
    expect(movedKeys(order, 'a', { side: 'after', neighbour: neighbour('d') })).toEqual([
      'b',
      'c',
      'd',
      'a',
    ]);
    expect(movedKeys(order, 'd', { side: 'before', neighbour: neighbour('a') })).toEqual([
      'd',
      'a',
      'b',
      'c',
    ]);
  });

  it('leaves the order as it was when the neighbour is no longer in it', () => {
    const moved = movedKeys(order, 'a', { side: 'after', neighbour: neighbour('gone') });

    expect(moved).toEqual(order);
    expect(moved).not.toBe(order);
  });

  it('leaves the order as it was, and adds nothing, when the ticket is no longer in it', () => {
    const moved = movedKeys(order, 'gone', { side: 'after', neighbour: neighbour('b') });

    expect(moved).toEqual(order);
    expect(moved).not.toBe(order);
  });

  it('does not change the order it is given', () => {
    const before = [...order];

    movedKeys(order, 'a', { side: 'after', neighbour: neighbour('c') });

    expect(order).toEqual(before);
  });
});

describe('rankBody', () => {
  it('names the number of the neighbour on the side of the placement', () => {
    expect(rankBody({ side: 'after', neighbour: t(7) })).toEqual({ after: 7 });
    expect(rankBody({ side: 'before', neighbour: t(8) })).toEqual({ before: 8 });
  });
});

describe('the moves the list does not show yet', () => {
  const neighbour = (key: string) => ({ key, number: 0 }) as Ticket;
  const move = (key: string, after: string, version?: number): Move => ({
    key,
    placement: { side: 'after', neighbour: neighbour(after) },
    ...(version === undefined ? {} : { version }),
  });
  const versions = (entries: [string, number][]) => new Map(entries);

  describe('unanswered', () => {
    it('keeps a move whose write is on its way, whatever version the answer has', () => {
      const moves = [move('a', 'c')];

      expect(unanswered(moves, versions([['a', 9]]))).toEqual(moves);
    });

    it('keeps a move that the answer has an older version of the ticket for: it left the server before the move', () => {
      const moves = [move('a', 'c', 4)];

      expect(unanswered(moves, versions([['a', 3]]))).toEqual(moves);
    });

    it('lets go of a move once the answer has the version the write gave the ticket, or a later one', () => {
      expect(unanswered([move('a', 'c', 4)], versions([['a', 4]]))).toEqual([]);
      expect(unanswered([move('a', 'c', 4)], versions([['a', 5]]))).toEqual([]);
    });

    it('lets go of the moves of a ticket the answer no longer has, closed or filtered out', () => {
      expect(unanswered([move('a', 'c'), move('a', 'b', 2)], versions([['b', 1]]))).toEqual([]);
    });

    it('lets go of every move for an answer that names no versions, which then shows as it is', () => {
      expect(unanswered([move('a', 'c')], undefined)).toEqual([]);
    });

    it('settles each move on its own, the moves of one ticket included, and keeps their order', () => {
      const first = move('a', 'c', 2);
      const second = move('a', 'b', 3);
      const other = move('d', 'a');

      expect(
        unanswered(
          [first, other, second],
          versions([
            ['a', 2],
            ['d', 1],
          ]),
        ),
      ).toEqual([other, second]);
    });
  });

  describe('withMoves', () => {
    const order = ['a', 'b', 'c', 'd'];

    it('is the order of the list while no move is open', () => {
      expect(withMoves(order, [])).toEqual(order);
    });

    it('puts the moves on top of the order of the list, the oldest first', () => {
      expect(withMoves(order, [move('a', 'c'), move('d', 'a')])).toEqual(['b', 'c', 'a', 'd']);
      expect(withMoves(order, [move('d', 'a'), move('a', 'c')])).toEqual(['d', 'b', 'c', 'a']);
      expect(withMoves(order, [move('a', 'd'), move('a', 'b')])).toEqual(['b', 'a', 'c', 'd']);
    });

    it('changes nothing where the list shows the move already', () => {
      expect(withMoves(['b', 'c', 'a', 'd'], [move('a', 'c')])).toEqual(['b', 'c', 'a', 'd']);
    });

    it('does not change the order it is given', () => {
      const given = [...order];

      withMoves(given, [move('a', 'c')]);

      expect(given).toEqual(order);
    });
  });
});

// docs/adr/0014 D3: the backlog marks where the score's order and the rank's disagree.
describe('scoreMarks', () => {
  /** The marks of the tickets, which arrive in the rank's order, as `number: mark`. */
  const marksOf = (...tickets: Ticket[]) =>
    Object.fromEntries(
      [...scoreMarks(arrange(tickets))].map(([key, mark]) => [key.split('-')[1], mark]),
    );
  const s = (number: number, score: number | null, overrides: Partial<Ticket> = {}) =>
    t(number, { score, ...overrides });

  it('marks nothing where the rank follows the score, equal scores included', () => {
    expect(marksOf(s(1, 9), s(2, 5), s(3, 5), s(4, 2))).toEqual({});
  });

  it('marks the one ticket out of its place, not every ticket it pushed aside', () => {
    expect(marksOf(s(1, 2), s(2, 9), s(3, 5), s(4, 4))).toEqual({ '1': 'lower' });
    expect(marksOf(s(1, 9), s(2, 5), s(3, 4), s(4, 8))).toEqual({ '4': 'higher' });
  });

  it('says higher where the score would lift a ticket above one the run keeps', () => {
    expect(marksOf(s(1, 5), s(2, 9), s(3, 3))).toEqual({ '2': 'higher' });
  });

  it('compares within a horizon, and among the children of one parent', () => {
    expect(
      marksOf(s(1, 9, { urgency: 'now' }), s(2, 2), s(3, 9, { urgency: 'now' }), s(4, 1)),
    ).toEqual({});
    expect(
      marksOf(s(1, 1), s(2, 2, { parent: under(1) }), s(3, 7, { parent: under(1) }), s(4, 0.5)),
    ).toEqual({ '3': 'higher' });
  });

  it('leaves a ticket without a score out', () => {
    expect(marksOf(s(1, null), s(2, 3), s(3, 4))).toEqual({ '3': 'higher' });
    expect(marksOf(s(1, null))).toEqual({});
  });
});
