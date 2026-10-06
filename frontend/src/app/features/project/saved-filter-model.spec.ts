import { SavedFilter, TicketState } from '../../api/models';
import {
  backlogLeftOut,
  boardLeftOut,
  describe as describeFilter,
  fromBacklog,
  fromBoard,
  listParameters,
  notesOf,
  toBacklog,
  toBoard,
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

describe('the saved filter on a board', () => {
  it('puts the projects into the swimlanes and keeps the rest, include_terminal aside', () => {
    expect(
      toBoard({
        project: ['OPS', '!DOC'],
        state: ['!blocked'],
        horizon: ['now'],
        assignee: ['me'],
        q: 'crash',
        include_terminal: true,
      }),
    ).toEqual({
      projects: ['OPS', '!DOC'],
      extra: { state: ['!blocked'], horizon: ['now'], assignee: ['me'], q: 'crash' },
    });
    expect(toBoard({})).toEqual({ projects: [], extra: {} });
  });

  it('saves the projects and the rest as one set, without what is empty', () => {
    expect(fromBoard(['COW', '!OPS'], { severity: ['high'], q: 'crash' })).toEqual({
      project: ['COW', '!OPS'],
      severity: ['high'],
      q: 'crash',
    });
    expect(fromBoard([], { type: [] })).toEqual({});
  });

  it('notes an include_terminal condition a board leaves out, and no project', () => {
    const filter = (parameters: SavedFilter['parameters']) =>
      ({ warnings: [], parameters }) as unknown as SavedFilter;

    expect(notesOf(filter({ include_terminal: true, project: ['OPS'] }), boardLeftOut)).toEqual([
      'include_terminal: a board shows no closed ticket; the tenant’s ticket list applies this condition',
    ]);
    expect(notesOf(filter({ project: ['OPS'], horizon: ['now'] }), boardLeftOut)).toEqual([]);
  });
});
