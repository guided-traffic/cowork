import { convertToParamMap } from '@angular/router';
import { SavedFilterParameters } from '../../api/models';
import {
  barNames,
  beyondBar,
  choosing,
  filterOf,
  parameterKinds,
  plain,
  queryOf,
  sameList,
  withChosen,
} from './tenant-tickets-model';

/** A filter that holds every parameter of the ticket lists once. */
const everything: SavedFilterParameters = {
  project: ['COW', '!OPS'],
  state: ['filed', 'blocked'],
  type: ['bug'],
  severity: ['high', 'critical'],
  security: ['live'],
  horizon: ['now'],
  effort: ['S'],
  assignee: ['me', 'none'],
  reporter: ['p-2'],
  q: 'flicker',
  parent: ['none'],
  interest: ['me'],
  blocked: true,
  has_open_questions: false,
  include_terminal: true,
  progress_min: 20,
  progress_max: 80,
  opened_after: '2026-10-01T00:00:00Z',
  opened_before: '2026-10-05T00:00:00Z',
  updated_after: '2026-10-02T00:00:00Z',
  updated_before: '2026-10-04T00:00:00Z',
  done_after: '2026-09-21T00:00:00Z',
  // The name horizon had before (docs/adr/0010 D1), which an address may still name.
  urgency: ['next'],
};

describe('the address of the tenant’s ticket list', () => {
  it('reads every filter of the ticket lists from the address (docs/adr/0049 D1)', () => {
    expect(filterOf(convertToParamMap(queryOf(everything)))).toEqual(everything);
  });

  it('writes every filter of the ticket lists, a repeated one repeated, the others as text', () => {
    expect(queryOf(everything)).toMatchObject({
      project: ['COW', '!OPS'],
      q: 'flicker',
      blocked: 'true',
      has_open_questions: 'false',
      progress_min: '20',
    });
    expect(Object.keys(queryOf(everything))).toEqual(Object.keys(parameterKinds));
  });

  it('leaves out what holds no condition, in both directions', () => {
    expect(queryOf({ state: [], q: '', blocked: undefined })).toEqual({});
    expect(filterOf(convertToParamMap({ state: ['', ' '], q: '  ', progress_min: '' }))).toEqual(
      {},
    );
  });

  it('reads a repeated value once, without the spaces around it', () => {
    expect(filterOf(convertToParamMap({ state: ['filed', ' filed ', 'review'] }))).toEqual({
      state: ['filed', 'review'],
    });
  });

  it('ignores what is no filter of the lists', () => {
    expect(filterOf(convertToParamMap({ page: '3', cursor: 'abc', state: 'filed' }))).toEqual({
      state: ['filed'],
    });
  });

  // docs/adr/0049 D4: a value is refused by name, never dropped unseen.
  it('keeps a number or a flag that does not read as one as written, for the server to refuse by name', () => {
    expect(
      filterOf(convertToParamMap({ progress_min: 'half', blocked: 'yes', progress_max: '-5' })),
    ).toEqual({ progress_min: 'half', progress_max: -5, blocked: 'yes' });
  });
});

describe('the bar of the tenant’s ticket list', () => {
  it('has a select for each of the backlog’s filters and the project, the text a field of its own', () => {
    expect(barNames).toEqual([
      'project',
      'state',
      'type',
      'severity',
      'security',
      'horizon',
      'effort',
      'assignee',
      'reporter',
    ]);
  });

  it('shows the plain values in a select and none of the negated', () => {
    expect(plain(['filed', '!blocked', 'review'])).toEqual(['filed', 'review']);
    expect(plain(undefined)).toEqual([]);
  });

  it('writes a select’s choice and keeps the negated values of its parameter', () => {
    const filter: SavedFilterParameters = { state: ['filed', '!blocked'], severity: ['high'] };

    expect(choosing(filter, 'state', ['review'])).toEqual({
      state: ['review', '!blocked'],
      severity: ['high'],
    });
    expect(choosing(filter, 'severity', [])).toEqual({ state: ['filed', '!blocked'] });
    expect(choosing({ state: ['filed'] }, 'state', [])).toEqual({});
    expect(filter).toEqual({ state: ['filed', '!blocked'], severity: ['high'] });
  });

  it('names what the bar has no control for: the other parameters and the negated values', () => {
    expect(beyondBar(everything)).toEqual({
      project: ['!OPS'],
      parent: ['none'],
      interest: ['me'],
      blocked: true,
      has_open_questions: false,
      include_terminal: true,
      progress_min: 20,
      progress_max: 80,
      opened_after: '2026-10-01T00:00:00Z',
      opened_before: '2026-10-05T00:00:00Z',
      updated_after: '2026-10-02T00:00:00Z',
      updated_before: '2026-10-04T00:00:00Z',
      done_after: '2026-09-21T00:00:00Z',
      urgency: ['next'],
    });
    expect(beyondBar({ state: ['filed'], q: 'crash' })).toEqual({});
  });

  it('offers a chosen value the options do not hold under its own name', () => {
    const options = [
      { value: 'COW', label: 'COW · Cowork' },
      { value: 'OPS', label: 'OPS · Operations' },
    ];

    expect(withChosen(options, ['OPS', 'OLD'])).toEqual([
      ...options,
      { value: 'OLD', label: 'OLD' },
    ]);
    expect(withChosen(options, [])).toEqual(options);
  });

  it('tells two lists that hold the same apart from two that do not', () => {
    expect(sameList(['a', 'b'], ['a', 'b'])).toBe(true);
    expect(sameList(['a', 'b'], ['b', 'a'])).toBe(false);
    expect(sameList([{ value: 'a', label: 'A' }], [{ value: 'a', label: 'B' }])).toBe(false);
  });
});
