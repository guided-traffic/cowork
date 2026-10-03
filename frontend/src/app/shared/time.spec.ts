import { computed } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { ago, Clock, count, dateTime, duration, parseDuration, size, today } from './time';

const now = Date.parse('2026-10-03T12:00:00Z');
const day = 24 * 3600;
const iso = (secondsFromNow: number) => new Date(now + secondsFromNow * 1000).toISOString();

describe('ago', () => {
  it.each([
    [0, 'just now'],
    [-1, 'just now'],
    [-30, 'just now'],
    [-59, 'just now'],
    [-60, '1 minute ago'],
    [-3 * 60, '3 minutes ago'],
    [-45 * 60, '45 minutes ago'],
    [-3600, '1 hour ago'],
    [-5 * 3600, '5 hours ago'],
    [-day, 'yesterday'],
    [-3 * day, '3 days ago'],
    [-7 * day, 'last week'],
    [-14 * day, '2 weeks ago'],
    [-30 * day, 'last month'],
    [-150 * day, '5 months ago'],
    [-365 * day, 'last year'],
    [-2 * 365 * day, '2 years ago'],
  ])('puts a time %i seconds from now as %j', (seconds, text) => {
    expect(ago(iso(seconds), now)).toBe(text);
  });

  it.each([
    [1, 'just now'],
    [59, 'just now'],
    [3 * 60, 'in 3 minutes'],
    [3600, 'in 1 hour'],
    [day, 'tomorrow'],
    [14 * day, 'in 2 weeks'],
    [365 * day, 'next year'],
  ])('puts a time %i seconds ahead as %j', (seconds, text) => {
    expect(ago(iso(seconds), now)).toBe(text);
  });

  it('measures against the time it is given, not against the clock of the page', () => {
    const then = iso(-2 * 3600);

    expect(ago(then, now)).toBe('2 hours ago');
    expect(ago(then, now + 3600 * 1000)).toBe('3 hours ago');
  });

  // Whole units, cut toward zero: a unit that is not complete yet is not counted, so the words
  // never name the next unit's size too early ("60 minutes ago", "24 hours ago", "7 days ago").
  describe('at the end of a unit', () => {
    it.each([
      [-119, '1 minute ago'],
      [-3599, '59 minutes ago'],
      [-3600, '1 hour ago'],
      [-7199, '1 hour ago'],
      [-day + 1, '23 hours ago'],
      [-day, 'yesterday'],
      [-2 * day + 1, 'yesterday'],
      [-7 * day + 1, '6 days ago'],
      [-7 * day, 'last week'],
      [-14 * day + 1, 'last week'],
      [-30 * day + 1, '4 weeks ago'],
      [-60 * day + 1, 'last month'],
      [-365 * day + 1, '12 months ago'],
      [-365 * day, 'last year'],
      [-2 * 365 * day + 1, 'last year'],
    ])('puts %i seconds from now as %j', (seconds, text) => {
      expect(ago(iso(seconds), now)).toBe(text);
    });

    it.each([
      [119, 'in 1 minute'],
      [3599, 'in 59 minutes'],
      [7199, 'in 1 hour'],
      [day - 1, 'in 23 hours'],
      [2 * day - 1, 'tomorrow'],
      [7 * day - 1, 'in 6 days'],
      [14 * day - 1, 'next week'],
      [2 * 365 * day - 1, 'next year'],
    ])('puts %i seconds ahead as %j, cut toward zero as well', (seconds, text) => {
      expect(ago(iso(seconds), now)).toBe(text);
    });

    it('never says a unit has as many parts as the next unit is made of', () => {
      const phrases = [-3599, -day + 1, -7 * day + 1].map((seconds) => ago(iso(seconds), now));

      expect(phrases).toEqual(['59 minutes ago', '23 hours ago', '6 days ago']);
      expect(phrases.join(' ')).not.toMatch(/\b(60 minutes|24 hours|7 days)\b/);
    });
  });
});

describe('dateTime', () => {
  // Built from local fields, so the expectation holds in any time zone: the browser's zone is used.
  const local = new Date(2026, 9, 3, 14, 5).toISOString();

  it('writes the date and the time the way the given locale does', () => {
    expect(dateTime(local, 'en-US')).toMatch(/^Oct 3, 2026, 2:05\sPM$/);
    expect(dateTime(local, 'en-GB')).toBe('3 Oct 2026, 14:05');
    expect(dateTime(local, 'de-DE')).toBe('03.10.2026, 14:05');
  });

  it('follows the browser locale when none is given (docs/adr/0055 D2)', () => {
    expect(dateTime(local)).toBe(
      new Date(local).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }),
    );
  });
});

describe('count', () => {
  it('names one thing in the singular', () => {
    expect(count(1, 'open ticket')).toBe('1 open ticket');
  });

  it.each([0, 2, 3, 11, 100])(
    'puts %i things in the plural, with an s unless the plural is named',
    (n) => {
      expect(count(n, 'open ticket')).toBe(`${n} open tickets`);
    },
  );

  it('uses the plural it is given', () => {
    expect(count(1, 'person', 'people')).toBe('1 person');
    expect(count(2, 'person', 'people')).toBe('2 people');
    expect(count(0, 'person', 'people')).toBe('0 people');
  });
});

describe('Clock', () => {
  const start = Date.parse('2026-10-03T12:00:00Z');

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(start);
  });

  afterEach(() => vi.useRealTimers());

  it('starts at the current time', () => {
    expect(TestBed.inject(Clock).now()).toBe(start);
  });

  it('moves on every 30 seconds and not before', () => {
    const clock = TestBed.inject(Clock);

    vi.advanceTimersByTime(29_999);
    expect(clock.now()).toBe(start);

    vi.advanceTimersByTime(1);
    expect(clock.now()).toBe(start + 30_000);

    vi.advanceTimersByTime(30_000);
    expect(clock.now()).toBe(start + 60_000);
  });

  it('moves a relative time on with it', () => {
    const clock = TestBed.inject(Clock);
    const written = computed(() => ago(iso(0), clock.now()));
    expect(written()).toBe('just now');

    vi.advanceTimersByTime(60_000);

    expect(written()).toBe('1 minute ago');
  });

  it('stops ticking when its injector is destroyed', () => {
    const timers = vi.getTimerCount();
    TestBed.inject(Clock);
    expect(vi.getTimerCount()).toBe(timers + 1);

    TestBed.resetTestingModule();

    expect(vi.getTimerCount()).toBe(timers);
  });
});

describe('parseDuration', () => {
  it.each([
    ['90', 90],
    ['1:30', 90],
    ['1h 30m', 90],
    ['1.5h', 90],
    ['1,5h', 90],
    ['45m', 45],
    ['45min', 45],
  ])('reads %j as %i minutes', (text, minutes) => {
    expect(parseDuration(text)).toBe(minutes);
  });

  describe('plain minutes', () => {
    it.each([
      ['1', 1],
      ['120', 120],
      ['  90  ', 90],
    ])('reads %j as %i', (text, minutes) => {
      expect(parseDuration(text)).toBe(minutes);
    });
  });

  describe('hours and minutes with a colon', () => {
    it.each([
      ['0:45', 45],
      ['01:30', 90],
      ['2:00', 120],
      ['10:05', 605],
      ['25:00', 1500],
      ['1:59', 119],
    ])('reads %j as %i', (text, minutes) => {
      expect(parseDuration(text)).toBe(minutes);
    });

    it.each(['1:60', '1:75', '1:5', '1:', ':30', '1:30:00', '-1:30', '1;30'])(
      'does not read %j, whose minutes are not two digits below 60',
      (text) => {
        expect(parseDuration(text)).toBeNull();
      },
    );
  });

  describe('units', () => {
    it.each([
      ['1h', 60],
      ['2h', 120],
      ['1h30m', 90],
      ['1 h 30 m', 90],
      ['1h 30min', 90],
      ['90m', 90],
      ['90 m', 90],
      ['90min', 90],
      ['0.5h', 30],
      ['1.25h', 75],
      ['1,25h', 75],
      ['1.5 h', 90],
      ['1h 0m', 60],
    ])('reads %j as %i', (text, minutes) => {
      expect(parseDuration(text)).toBe(minutes);
    });

    it.each([
      ['1H 30M', 90],
      ['45MIN', 45],
      ['1.5H', 90],
    ])('reads the units in any case: %j as %i', (text, minutes) => {
      expect(parseDuration(text)).toBe(minutes);
    });

    it('rounds a fraction of an hour to the nearest minute', () => {
      expect(parseDuration('1.33h')).toBe(80);
      expect(parseDuration('0.01h')).toBe(1);
    });

    it('adds the minutes to the hours it was given', () => {
      expect(parseDuration('1.5h 30m')).toBe(120);
      expect(parseDuration('1h 90m')).toBe(150);
    });
  });

  describe('nothing, or zero', () => {
    it.each([
      '',
      '   ',
      '\n',
      '0',
      '00',
      '0m',
      '0min',
      '0h',
      '0h 0m',
      '0:00',
      '00:00',
      '0.0h',
      '0.001h',
    ])('is null for %j, there being no time in it', (text) => {
      expect(parseDuration(text)).toBeNull();
    });
  });

  describe('anything else', () => {
    it.each([
      ['abc', 'a word'],
      ['-30', 'a negative number of minutes'],
      ['-1h', 'a negative number of hours'],
      ['-0:30', 'a negative time'],
      ['+30', 'a signed number'],
      ['1.5', 'a decimal without a unit, which could be hours or minutes'],
      ['2h30', 'minutes without their unit'],
      ['1h 30', 'minutes without their unit after hours'],
      ['m', 'a unit alone'],
      ['h', 'a unit alone'],
      ['.5h', 'a fraction without its integer part'],
      ['1h -30m', 'a negative part'],
      ['30m 1h', 'the units the other way round'],
      ['1h1h', 'a unit twice'],
      ['1d', 'a unit that is not one'],
      ['1h 30m!', 'a stray character'],
    ])('is null for %j: %s', (text) => {
      expect(parseDuration(text)).toBeNull();
    });
  });

  it('hands back whole minutes, never a fraction', () => {
    for (const text of ['90', '1:30', '1h 30m', '1.5h', '1,5h', '45m', '1.33h', '0.01h']) {
      expect(Number.isInteger(parseDuration(text))).toBe(true);
    }
  });

  it('is what duration() writes down again', () => {
    for (const minutes of [1, 45, 59, 60, 61, 95, 120, 1500]) {
      expect(parseDuration(duration(minutes).replace(/ h/, 'h').replace(/ min/, 'm'))).toBe(
        minutes,
      );
    }
  });
});

describe('duration', () => {
  it.each([
    [0, '0 min'],
    [1, '1 min'],
    [45, '45 min'],
    [59, '59 min'],
    [60, '1 h'],
    [61, '1 h 1 min'],
    [95, '1 h 35 min'],
    [120, '2 h'],
    [605, '10 h 5 min'],
    [1500, '25 h'],
  ])('writes %i minutes as %j', (minutes, text) => {
    expect(duration(minutes)).toBe(text);
  });

  it('leaves out the hours when there are none and the minutes when there are none', () => {
    expect(duration(45)).not.toContain(' h');
    expect(duration(120)).not.toContain('min');
  });
});

describe('size', () => {
  describe('bytes', () => {
    it.each([
      [0, '0 B'],
      [1, '1 B'],
      [840, '840 B'],
      [1023, '1023 B'],
    ])('writes %i as %j', (bytes, text) => {
      expect(size(bytes, 'en-US')).toBe(text);
    });

    it('does not group or round them, whatever the locale', () => {
      expect(size(1023, 'de-DE')).toBe('1023 B');
      expect(size(840, 'de-DE')).toBe('840 B');
    });
  });

  describe('kilobytes, from 1024 bytes', () => {
    it.each([
      [1024, '1 KB'],
      [1536, '1.5 KB'],
      [12700, '12.4 KB'],
      [102400, '100 KB'],
      [1048523, '1,023.9 KB'],
    ])('writes %i as %j with one fraction digit at most', (bytes, text) => {
      expect(size(bytes, 'en-US')).toBe(text);
    });

    it('writes the fraction and the grouping the way the given locale does', () => {
      expect(size(12700, 'de-DE')).toBe('12,4 KB');
      expect(size(1536, 'de-DE')).toBe('1,5 KB');
      expect(size(1048523, 'de-DE')).toBe('1.023,9 KB');
    });
  });

  describe('megabytes, from 1024 kilobytes', () => {
    it.each([
      [1024 * 1024, '1 MB'],
      [3250586, '3.1 MB'],
      [2621440, '2.5 MB'],
      [10 * 1024 * 1024, '10 MB'],
      [1024 * 1024 * 1024, '1,024 MB'],
    ])('writes %i as %j with one fraction digit at most', (bytes, text) => {
      expect(size(bytes, 'en-US')).toBe(text);
    });

    it('writes the fraction and the grouping the way the given locale does', () => {
      expect(size(3250586, 'de-DE')).toBe('3,1 MB');
      expect(size(1024 * 1024 * 1024, 'de-DE')).toBe('1.024 MB');
    });
  });

  it('switches the unit at 1024 and at 1024 times 1024', () => {
    expect(size(1023, 'en-US')).toBe('1023 B');
    expect(size(1024, 'en-US')).toBe('1 KB');
    expect(size(1024 * 1024 - 1024, 'en-US')).toBe('1,023 KB');
    expect(size(1024 * 1024, 'en-US')).toBe('1 MB');
  });

  it('follows the browser locale when none is given (docs/adr/0055 D2)', () => {
    const expected = (12700 / 1024).toLocaleString(undefined, { maximumFractionDigits: 1 });

    expect(size(12700)).toBe(`${expected} KB`);
    expect(size(5)).toBe('5 B');
  });
});

describe('today', () => {
  /** A zone, and what Date.getTimezoneOffset() says there: minutes UTC is ahead of the wall clock. */
  const zones: [string, number][] = [
    ['Pacific/Kiritimati', -840], // UTC+14:00, the furthest east
    ['Asia/Kolkata', -330], // UTC+05:30
    ['UTC', 0],
    ['Pacific/Marquesas', 570], // UTC-09:30
    ['Pacific/Pago_Pago', 660], // UTC-11:00, the furthest west
  ];
  /** The instant at which the wall clock of a zone reads the given time on the given day. */
  const instant = (offset: number, month: number, dayOfMonth: number, time: string) => {
    const [hours, minutes, seconds] = time.split(':').map(Number);
    return new Date(
      Date.UTC(2026, month - 1, dayOfMonth, hours, minutes, seconds) + offset * 60_000,
    );
  };

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.useRealTimers();
  });

  describe.each(zones)('in %s', (zone, offset) => {
    beforeEach(() => {
      vi.stubEnv('TZ', zone);
      // The runtime must have taken the zone, or the expectations below would prove nothing.
      expect(new Date(Date.UTC(2026, 9, 3, 12)).getTimezoneOffset()).toBe(offset);
    });

    it('is the day of the wall clock a second before midnight', () => {
      expect(today(instant(offset, 10, 3, '23:59:59'))).toBe('2026-10-03');
    });

    it('is the next day at midnight', () => {
      expect(today(instant(offset, 10, 4, '00:00:00'))).toBe('2026-10-04');
    });

    it('is the day of the wall clock in the middle of the day', () => {
      expect(today(instant(offset, 10, 3, '12:00:00'))).toBe('2026-10-03');
    });

    it('is right across the end of a month and of a year', () => {
      expect(today(instant(offset, 10, 31, '23:59:59'))).toBe('2026-10-31');
      expect(today(instant(offset, 11, 1, '00:00:00'))).toBe('2026-11-01');
      expect(today(instant(offset, 12, 31, '23:59:59'))).toBe('2026-12-31');
      expect(today(new Date(Date.UTC(2027, 0, 1, 0, 0, 0) + offset * 60_000))).toBe('2027-01-01');
    });

    it('follows the system clock when it is not given a time', () => {
      vi.useFakeTimers();
      vi.setSystemTime(instant(offset, 10, 3, '23:59:30'));
      expect(today()).toBe('2026-10-03');

      vi.advanceTimersByTime(30_000);
      expect(today()).toBe('2026-10-04');
    });
  });

  it('is not the day of UTC where the zone is far east: it is ahead of it for most of the day', () => {
    vi.stubEnv('TZ', 'Pacific/Kiritimati');

    expect(today(new Date('2026-10-03T12:00:00Z'))).toBe('2026-10-04');
    expect(today(new Date('2026-10-03T09:59:59Z'))).toBe('2026-10-03');
    expect(today(new Date('2026-10-03T10:00:00Z'))).toBe('2026-10-04');
  });

  it('is not the day of UTC where the zone is far west: it is behind it for part of the day', () => {
    vi.stubEnv('TZ', 'Pacific/Pago_Pago');

    expect(today(new Date('2026-10-04T05:00:00Z'))).toBe('2026-10-03');
    expect(today(new Date('2026-10-04T10:59:59Z'))).toBe('2026-10-03');
    expect(today(new Date('2026-10-04T11:00:00Z'))).toBe('2026-10-04');
  });

  describe('from the offset of the time it is given, whatever zone the runtime is in', () => {
    /** A time that reports the zone: the engine's own Date, with the offset of the wall clock it should show. */
    const reporting = (utc: string, offset: number) => {
      const time = new Date(utc);
      time.getTimezoneOffset = () => offset;
      return time;
    };

    it.each([
      [
        'east, UTC+14',
        -840,
        '2026-10-03T09:59:59Z',
        '2026-10-03',
        '2026-10-03T10:00:00Z',
        '2026-10-04',
      ],
      [
        'east, UTC+5:30',
        -330,
        '2026-10-03T18:29:59Z',
        '2026-10-03',
        '2026-10-03T18:30:00Z',
        '2026-10-04',
      ],
      ['UTC', 0, '2026-10-03T23:59:59Z', '2026-10-03', '2026-10-04T00:00:00Z', '2026-10-04'],
      [
        'west, UTC-9:30',
        570,
        '2026-10-04T09:29:59Z',
        '2026-10-03',
        '2026-10-04T09:30:00Z',
        '2026-10-04',
      ],
      [
        'west, UTC-11',
        660,
        '2026-10-04T10:59:59Z',
        '2026-10-03',
        '2026-10-04T11:00:00Z',
        '2026-10-04',
      ],
    ])('is the day on the wall clock in the %s', (_zone, offset, before, day, after, nextDay) => {
      expect(today(reporting(before, offset))).toBe(day);
      expect(today(reporting(after, offset))).toBe(nextDay);
    });

    it('does not change the time it is given', () => {
      const time = reporting('2026-10-03T09:59:59Z', -840);

      today(time);

      expect(time.toISOString()).toBe('2026-10-03T09:59:59.000Z');
    });
  });

  it('is written as the API writes a day: four digits, two digits, two digits', () => {
    expect(today(new Date(Date.UTC(2026, 0, 5, 12)))).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(today(new Date(2026, 0, 5, 12))).toBe('2026-01-05');
  });
});
