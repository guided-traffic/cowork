import { SavedFilter, TicketState } from '../../api/models';
import {
  describe as describeFilter,
  fromBacklog,
  listParameters,
  notesOf,
  toBacklog,
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

    expect(notesOf(filter)).toEqual([
      'state: not a value of state: triaged',
      'project: a backlog is one project; this condition applies to the tenant’s lists',
    ]);
  });
});
