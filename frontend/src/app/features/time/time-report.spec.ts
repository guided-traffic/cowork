import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Problem, TimeReport as TimeReportBody } from '../../api/models';
import { SessionService } from '../../core/session.service';
import { Clock } from '../../shared/time';
import { monthStart, TimeReport } from './time-report';

const base = '/api/v1/teams/acme/time-report';

/** An instant by the clock on the wall of whoever runs the test: the page counts days that way. */
const local = (year: number, month: number, day: number, hour = 12, minute = 0) =>
  new Date(year, month - 1, day, hour, minute).getTime();

function report(
  items: { key: string; label: string; minutes: number }[],
  groupBy: TimeReportBody['group_by'] = 'project',
): TimeReportBody {
  return {
    group_by: groupBy,
    items,
    total_minutes: items.reduce((sum, item) => sum + item.minutes, 0),
  };
}

describe('monthStart', () => {
  it.each([
    ['2026-10-03', '2026-10-01'],
    ['2026-10-01', '2026-10-01'],
    ['2026-01-31', '2026-01-01'],
    ['2026-12-15', '2026-12-01'],
    ['2028-02-29', '2028-02-01'],
  ])('is the first of the month for %s: %s', (day, first) => {
    expect(monthStart(day)).toBe(first);
  });
});

describe('TimeReport', () => {
  let tenant: WritableSignal<string | null>;
  let now: WritableSignal<number>;
  let http: HttpTestingController;

  beforeEach(() => {
    // The page asks for the month so far by the clock it is given, whatever day the suite runs on.
    now = signal(local(2026, 10, 3));
    tenant = signal<string | null>('acme');
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: SessionService, useValue: { tenant } },
        { provide: Clock, useValue: { now } },
      ],
    });
    http = TestBed.inject(HttpTestingController);
  });

  /** Renders the page; the request of the report is pending until `answer` flushes it. */
  function render() {
    const fixture = TestBed.createComponent(TimeReport);
    fixture.detectChanges();
    return fixture;
  }

  /**
   * Lets the answer reach the page and shows it. The select button's toggles register their model
   * a moment after they are created, so the page is shown twice.
   */
  async function settle(fixture: ComponentFixture<TimeReport>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  async function answer(fixture: ComponentFixture<TimeReport>, url: string, body: TimeReportBody) {
    http.expectOne(url).flush(body);
    await settle(fixture);
  }

  const host = (fixture: ComponentFixture<TimeReport>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<TimeReport>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  const text = (fixture: ComponentFixture<TimeReport>, testId: string) =>
    el(fixture, testId)?.textContent?.replace(/\s+/g, ' ').trim();

  function typeInto(fixture: ComponentFixture<TimeReport>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  describe('the request', () => {
    it('asks for the month so far, per project, in the tenant of the page', async () => {
      const fixture = render();

      await answer(fixture, `${base}?from=2026-10-01&to=2026-10-03&group_by=project`, report([]));

      expect((el(fixture, 'report-from') as HTMLInputElement).value).toBe('2026-10-01');
      expect((el(fixture, 'report-to') as HTMLInputElement).value).toBe('2026-10-03');
      expect(
        el(fixture, 'report-group')?.querySelector('.p-togglebutton-checked')?.textContent?.trim(),
      ).toBe('project');
    });

    it('asks for nothing outside a tenant', () => {
      tenant.set(null);

      render();

      http.expectNone(() => true);
    });

    it('asks again when the first day is changed', async () => {
      const fixture = render();
      await answer(fixture, `${base}?from=2026-10-01&to=2026-10-03&group_by=project`, report([]));

      typeInto(fixture, 'report-from', '2026-09-15');
      await answer(fixture, `${base}?from=2026-09-15&to=2026-10-03&group_by=project`, report([]));
    });

    it('asks again when the last day is changed', async () => {
      const fixture = render();
      await answer(fixture, `${base}?from=2026-10-01&to=2026-10-03&group_by=project`, report([]));

      typeInto(fixture, 'report-to', '2026-10-02');
      await answer(fixture, `${base}?from=2026-10-01&to=2026-10-02&group_by=project`, report([]));
    });

    it.each(['ticket', 'person', 'team'] as const)(
      'asks again when it is grouped by %s',
      async (group) => {
        const fixture = render();
        await answer(fixture, `${base}?from=2026-10-01&to=2026-10-03&group_by=project`, report([]));

        fixture.debugElement
          .query(By.css('[data-testid="report-group"]'))
          .triggerEventHandler('ngModelChange', group);
        fixture.detectChanges();

        await answer(
          fixture,
          `${base}?from=2026-10-01&to=2026-10-03&group_by=${group}`,
          report([], group),
        );
      },
    );

    it('offers the four groupings, project first', async () => {
      const fixture = render();
      await answer(fixture, `${base}?from=2026-10-01&to=2026-10-03&group_by=project`, report([]));

      const group = fixture.debugElement.query(By.css('[data-testid="report-group"]'))
        .componentInstance as { options(): string[] };
      expect(group.options()).toEqual(['project', 'ticket', 'person', 'team']);
    });
  });

  describe('the period as the clock goes on', () => {
    const asked = (from: string, to: string, group = 'project') =>
      `${base}?from=${from}&to=${to}&group_by=${group}`;

    const days = (fixture: ComponentFixture<TimeReport>) => [
      (el(fixture, 'report-from') as HTMLInputElement).value,
      (el(fixture, 'report-to') as HTMLInputElement).value,
    ];

    /** The clock goes on to `instant`, and the page is shown again. */
    function passTo(fixture: ComponentFixture<TimeReport>, instant: number) {
      now.set(instant);
      fixture.detectChanges();
    }

    it('asks again, up to the new date and from the same first day, when the clock passes midnight', async () => {
      const fixture = render();
      await answer(fixture, asked('2026-10-01', '2026-10-03'), report([]));

      passTo(fixture, local(2026, 10, 4, 0, 1));
      await answer(fixture, asked('2026-10-01', '2026-10-04'), report([]));

      expect(days(fixture)).toEqual(['2026-10-01', '2026-10-04']);
      http.verify();
    });

    it.each([
      [
        'into the next month',
        local(2026, 10, 31, 23, 59),
        '2026-10-31',
        local(2026, 11, 1, 0, 1),
        '2026-11-01',
      ],
      [
        'into the next year',
        local(2026, 12, 31, 23, 59),
        '2026-12-31',
        local(2027, 1, 1, 0, 1),
        '2027-01-01',
      ],
      [
        'past the end of February in a leap year',
        local(2028, 2, 29, 23, 59),
        '2028-02-29',
        local(2028, 3, 1, 0, 1),
        '2028-03-01',
      ],
    ])('moves both days of the month so far %s', async (_, before, dayBefore, after, dayAfter) => {
      now.set(before);
      const fixture = render();
      await answer(fixture, asked(monthStart(dayBefore), dayBefore), report([]));
      expect(days(fixture)).toEqual([monthStart(dayBefore), dayBefore]);

      passTo(fixture, after);
      await answer(fixture, asked(dayAfter, dayAfter), report([]));

      expect(days(fixture)).toEqual([dayAfter, dayAfter]);
      http.verify();
    });

    it('asks for nothing while the clock goes on within the day', async () => {
      const fixture = render();
      await answer(fixture, asked('2026-10-01', '2026-10-03'), report([]));

      passTo(fixture, local(2026, 10, 3, 18, 30));
      passTo(fixture, local(2026, 10, 3, 23, 59));
      await settle(fixture);

      http.expectNone(() => true);
      expect(days(fixture)).toEqual(['2026-10-01', '2026-10-03']);
    });

    it('holds a period the person chose while the clock goes on within the day', async () => {
      const fixture = render();
      await answer(fixture, asked('2026-10-01', '2026-10-03'), report([]));
      typeInto(fixture, 'report-from', '2026-09-15');
      await answer(fixture, asked('2026-09-15', '2026-10-03'), report([]));
      typeInto(fixture, 'report-to', '2026-09-30');
      await answer(fixture, asked('2026-09-15', '2026-09-30'), report([]));

      passTo(fixture, local(2026, 10, 3, 23, 59));
      await settle(fixture);

      http.expectNone(() => true);
      expect(days(fixture)).toEqual(['2026-09-15', '2026-09-30']);
    });

    it('replaces a period the person chose with the month so far once the clock passes midnight', async () => {
      const fixture = render();
      await answer(fixture, asked('2026-10-01', '2026-10-03'), report([]));
      typeInto(fixture, 'report-from', '2026-09-15');
      await answer(fixture, asked('2026-09-15', '2026-10-03'), report([]));
      typeInto(fixture, 'report-to', '2026-09-30');
      await answer(fixture, asked('2026-09-15', '2026-09-30'), report([]));

      passTo(fixture, local(2026, 10, 4, 0, 1));
      await answer(fixture, asked('2026-10-01', '2026-10-04'), report([]));

      expect(days(fixture)).toEqual(['2026-10-01', '2026-10-04']);
      http.verify();
    });

    it('holds the grouping that was chosen when the clock passes midnight', async () => {
      const fixture = render();
      await answer(fixture, asked('2026-10-01', '2026-10-03'), report([]));
      fixture.debugElement
        .query(By.css('[data-testid="report-group"]'))
        .triggerEventHandler('ngModelChange', 'person');
      fixture.detectChanges();
      await answer(fixture, asked('2026-10-01', '2026-10-03', 'person'), report([], 'person'));

      passTo(fixture, local(2026, 10, 4, 0, 1));
      await answer(fixture, asked('2026-10-01', '2026-10-04', 'person'), report([], 'person'));

      expect(
        el(fixture, 'report-group')?.querySelector('.p-togglebutton-checked')?.textContent?.trim(),
      ).toBe('person');
      http.verify();
    });

    it('asks for nothing outside a tenant, whatever the clock does', async () => {
      tenant.set(null);
      const fixture = render();

      passTo(fixture, local(2026, 10, 4, 0, 1));
      await settle(fixture);

      http.expectNone(() => true);
      expect(days(fixture)).toEqual(['2026-10-01', '2026-10-04']);
    });
  });

  describe('the rows', () => {
    const url = `${base}?from=2026-10-01&to=2026-10-03&group_by=project`;

    it('shows each row with its label and its duration, and the total', async () => {
      const fixture = render();

      await answer(
        fixture,
        url,
        report([
          { key: 'COW', label: 'Cowork', minutes: 135 },
          { key: 'OPS', label: 'Operations', minutes: 45 },
        ]),
      );

      const row = (key: string) =>
        [...(el(fixture, `report-${key}`)?.querySelectorAll('.label, .minutes') ?? [])].map(
          (cell) => cell.textContent,
        );
      expect(row('COW')).toEqual(['Cowork', '2 h 15 min']);
      expect(row('OPS')).toEqual(['Operations', '45 min']);
      expect(text(fixture, 'report-total')).toBe('3 h');
    });

    it('sizes each bar by the share of the largest row', async () => {
      const fixture = render();

      await answer(
        fixture,
        url,
        report([
          { key: 'COW', label: 'Cowork', minutes: 120 },
          { key: 'OPS', label: 'Operations', minutes: 60 },
          { key: 'DOC', label: 'Docs', minutes: 30 },
        ]),
      );

      const widths = ['COW', 'OPS', 'DOC'].map(
        (row) => el(fixture, `report-${row}`)?.querySelector<HTMLElement>('.bar span')?.style.width,
      );
      expect(widths).toEqual(['100%', '50%', '25%']);
    });

    it('shows an empty bar for a row of no time, when no row has any', async () => {
      const fixture = render();

      await answer(fixture, url, report([{ key: 'COW', label: 'Cowork', minutes: 0 }]));

      expect(el(fixture, 'report-COW')?.querySelector<HTMLElement>('.bar span')?.style.width).toBe(
        '0%',
      );
    });

    it('says that nothing was booked when the report is empty, with a total of nothing', async () => {
      const fixture = render();

      await answer(fixture, url, report([]));

      expect(host(fixture).querySelector('.panel .empty')?.textContent?.trim()).toBe(
        'Nothing booked in this period.',
      );
      expect(text(fixture, 'report-total')).toBe('0 min');
    });

    it('shows a total of nothing and no rows before the report is in', () => {
      const fixture = render();

      expect(text(fixture, 'report-total')).toBe('0 min');
      expect(host(fixture).querySelector('.row')).toBeNull();
      http.expectOne(url).flush(report([]));
    });
  });

  describe('a report that could not be loaded', () => {
    const url = `${base}?from=2026-10-01&to=2026-10-03&group_by=project`;

    async function failed(error: HttpErrorResponse) {
      const fixture = render();
      http
        .expectOne(url)
        .flush(error.error, { status: error.status, statusText: error.statusText });
      await settle(fixture);
      return fixture;
    }

    it('says why, with the detail of the problem, instead of the rows', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'Forbidden',
        status: 403,
        detail: 'Members see their own time only.',
        code: 'forbidden',
      };

      const fixture = await failed(
        new HttpErrorResponse({ status: 403, statusText: 'Forbidden', error: body }),
      );

      expect(text(fixture, 'report-failed')).toBe(
        'The report could not be loaded: Members see their own time only.',
      );
      expect(host(fixture).querySelector('.panel .empty:not([data-testid])')).toBeNull();
    });

    it('says why with the title when the problem has no detail', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'Forbidden',
        status: 403,
        code: 'forbidden',
      };

      const fixture = await failed(
        new HttpErrorResponse({ status: 403, statusText: 'Forbidden', error: body }),
      );

      expect(text(fixture, 'report-failed')).toBe('The report could not be loaded: Forbidden');
    });
  });
});
