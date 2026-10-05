import { DashboardStateCount } from '../../api/models';
import {
  addDays,
  ageLabel,
  leadTime,
  openStates,
  partialWeek,
  periodOf,
  projectCards,
  share,
  shortDay,
  shortKey,
  stateTotals,
} from './dashboard-model';

describe('addDays', () => {
  it.each([
    ['2026-10-07', -29, '2026-09-08'],
    ['2026-03-01', -1, '2026-02-28'],
    ['2028-03-01', -1, '2028-02-29'],
    ['2026-12-31', 1, '2027-01-01'],
    ['2026-10-25', 1, '2026-10-26'],
  ])('moves %s by %i days to %s, whatever the browser time zone', (day, days, moved) => {
    expect(addDays(day, days)).toBe(moved);
  });
});

describe('periodOf', () => {
  it('is the thirty days that end today without a day in the address', () => {
    expect(periodOf(null, null, '2026-10-07')).toEqual({ from: '2026-09-08', to: '2026-10-07' });
  });

  it('is the thirty days that end with the last day the address names', () => {
    expect(periodOf(null, '2026-09-30', '2026-10-07')).toEqual({
      from: '2026-09-01',
      to: '2026-09-30',
    });
  });

  it('takes both days from the address', () => {
    expect(periodOf('2026-01-01', '2026-03-31', '2026-10-07')).toEqual({
      from: '2026-01-01',
      to: '2026-03-31',
    });
  });

  it('ends today when the address names only the first day', () => {
    expect(periodOf('2026-10-01', null, '2026-10-07')).toEqual({
      from: '2026-10-01',
      to: '2026-10-07',
    });
  });
});

const row = (project: string, state: DashboardStateCount['state'], count: number) => ({
  project,
  state,
  count,
});

describe('stateTotals (tile 1)', () => {
  it('sums every project per state, in the order of docs/adr/0009 D1, zero included', () => {
    expect(
      stateTotals([row('ALPHA', 'blocked', 1), row('ALPHA', 'filed', 2), row('BETA', 'filed', 3)]),
    ).toEqual([
      { state: 'filed', count: 5 },
      { state: 'analysed', count: 0 },
      { state: 'decided', count: 0 },
      { state: 'in-progress', count: 0 },
      { state: 'review', count: 0 },
      { state: 'blocked', count: 1 },
    ]);
    expect(openStates).toEqual([
      'filed',
      'analysed',
      'decided',
      'in-progress',
      'review',
      'blocked',
    ]);
  });
});

describe('projectCards', () => {
  const alpha = { key: 'ALPHA', name: 'Alpha', description: 'The first' };
  const beta = { key: 'BETA', name: 'Beta', description: '' };
  const rows = [row('ALPHA', 'decided', 2), row('ALPHA', 'filed', 1), row('BETA', 'review', 4)];

  it('counts each project of the list in the order of the list, the states without one left out', () => {
    expect(projectCards([beta, alpha], rows, [])).toEqual([
      {
        key: 'BETA',
        name: 'Beta',
        description: '',
        open: 4,
        byState: [{ state: 'review', count: 4 }],
      },
      {
        key: 'ALPHA',
        name: 'Alpha',
        description: 'The first',
        open: 3,
        byState: [
          { state: 'filed', count: 1 },
          { state: 'decided', count: 2 },
        ],
      },
    ]);
  });

  it('counts zero for a project without an open ticket', () => {
    expect(projectCards([alpha], [], [])[0]).toMatchObject({ open: 0, byState: [] });
  });

  it('keeps the projects the filter names, and leaves out the negated ones', () => {
    expect(projectCards([alpha, beta], rows, ['BETA']).map((card) => card.key)).toEqual(['BETA']);
    expect(projectCards([alpha, beta], rows, ['!BETA']).map((card) => card.key)).toEqual(['ALPHA']);
    expect(projectCards([alpha, beta], rows, ['NOSUCH'])).toEqual([]);
  });
});

describe('share', () => {
  it('is the length of a bar against the largest of its tile', () => {
    expect(share(1, 4)).toBe('25%');
    expect(share(4, 4)).toBe('100%');
    expect(share(0, 0)).toBe('0%');
  });
});

describe('ageLabel (tile 5)', () => {
  it.each([
    [0, 7, 'Under a week'],
    [7, 30, '1 to 4 weeks'],
    [30, 90, '1 to 3 months'],
    [90, 365, '3 to 12 months'],
    [365, null, 'A year or more'],
    [3, 5, '3 to 5 days'],
    [500, null, '500 days or more'],
  ])('names the bucket from %i to %s days %s', (from, to, label) => {
    expect(ageLabel({ from_days: from, to_days: to, count: 0 })).toBe(label);
  });
});

describe('leadTime (tile 7)', () => {
  it.each([
    [0, '0 min'],
    [45 * 60, '45 min'],
    [3590, '1 h'],
    [5 * 3600, '5 h'],
    [47 * 3600, '47 h'],
    [48 * 3600, '2.0 days'],
    [3.5 * 86400, '3.5 days'],
    [10 * 86400 + 3600, '10.0 days'],
  ])('says %i seconds as %s', (seconds, words) => {
    expect(leadTime(seconds)).toBe(words);
  });
});

describe('shortDay', () => {
  it('shows a day without its year in the locale, on the calendar day the API names', () => {
    expect(shortDay('2026-10-05', 'en-US')).toBe('Oct 5');
    expect(shortDay('2026-01-01', 'en-US')).toBe('Jan 1');
  });
});

describe('partialWeek (tile 6)', () => {
  it('is a week the period cut before its Sunday', () => {
    expect(partialWeek({ week: '2026-W41', from: '2026-10-05', to: '2026-10-07', done: 0 })).toBe(
      true,
    );
    expect(partialWeek({ week: '2026-W40', from: '2026-09-28', to: '2026-10-04', done: 0 })).toBe(
      false,
    );
  });
});

describe('shortKey', () => {
  it('drops the tenant of a canonical key', () => {
    expect(shortKey('acme/ALPHA-12')).toBe('ALPHA-12');
  });
});
