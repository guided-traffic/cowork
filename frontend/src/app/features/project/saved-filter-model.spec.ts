import { SavedFilter, TicketState } from '../../api/models';
import {
  backlogLeftOut,
  describe as describeFilter,
  fromBacklog,
  listParameters,
  notesOf,
  toBacklog,
  withoutEmpty,
} from './saved-filter-model';

const open: TicketState[] = ['filed', 'analysed', 'decided', 'in-progress', 'review', 'blocked'];

describe('the saved filter in a backlog', () => {
  it('puts the text and the plain open states into the bar and keeps the rest, project aside', () => {
    expect(
      toBacklog(
        { q: 'crash', state: ['blocked', 'done', '!review'], severity: ['high'], project: ['OPS'] },
        open,
      ),
    ).toEqual({
      q: 'crash',
      states: ['blocked'],
      extra: { state: ['done', '!review'], severity: ['high'] },
    });
    expect(toBacklog({}, open)).toEqual({ q: '', states: [], extra: {} });
  });

  it('saves the bar and the rest as one set, without what is empty', () => {
    expect(fromBacklog('  crash ', ['blocked'], { state: ['!review'], type: ['bug'] })).toEqual({
      q: 'crash',
      state: ['blocked', '!review'],
      type: ['bug'],
    });
    expect(fromBacklog('', [], {})).toEqual({});
  });

  it('hands a list the rest with the chosen states merged in', () => {
    expect(listParameters({ severity: ['high'], state: ['!review'] }, ['filed'])).toEqual({
      severity: ['high'],
      state: ['filed', '!review'],
    });
    expect(listParameters({ assignee: [] }, [])).toEqual({});
  });

  it('reads a set as a line', () => {
    expect(describeFilter({ severity: ['high', 'critical'], q: 'crash', blocked: true })).toBe(
      'severity=high,critical · q="crash" · blocked=true',
    );
    expect(describeFilter({})).toBe('no condition');
  });

  it('notes the warnings of a filter and a project condition a backlog leaves out', () => {
    const filter = {
      warnings: [{ parameter: 'state', message: 'not a value of state: triaged' }],
      parameters: { project: ['OPS'] },
    } as SavedFilter;

    expect(notesOf(filter, backlogLeftOut)).toEqual([
      'state: not a value of state: triaged',
      'project: a backlog is one project; the tenant’s ticket list applies this condition',
    ]);
  });

  it('notes a condition left out only where the filter holds it, and none for a list that applies every one', () => {
    const filter = (parameters: SavedFilter['parameters']) =>
      ({ warnings: [], parameters }) as unknown as SavedFilter;

    expect(notesOf(filter({ severity: ['high'] }), backlogLeftOut)).toEqual([]);
    expect(notesOf(filter({ project: [] }), backlogLeftOut)).toEqual([]);
    // The tenant's ticket list applies `project`: it hands the bar nothing to leave out.
    expect(notesOf(filter({ project: ['OPS'] }))).toEqual([]);
  });

  it('leaves out what holds no condition', () => {
    expect(
      withoutEmpty({ state: [], q: 'crash', blocked: false, parent: undefined, project: ['COW'] }),
    ).toEqual({ q: 'crash', blocked: false, project: ['COW'] });
  });
});
