import { HttpErrorResponse } from '@angular/common/http';
import { computed, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Dashboard, Membership, Problem, Project } from '../../api/models';
import { DashboardQuery, DashboardService } from '../../core/dashboard.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { Clock } from '../../shared/time';
import { TenantDashboard } from './dashboard';

/** Noon of 7 October 2026 by the clock on the wall of whoever runs the test: the page counts days so. */
const now = new Date(2026, 9, 7, 12, 0).getTime();

/** The instant `days` days before the clock, as the API writes it. */
const before = (days: number) => new Date(now - days * 86_400_000).toISOString();

function project(key: string, name: string, description = ''): Project {
  return {
    id: `id-${key}`,
    key,
    name,
    description,
    restricted: false,
    wip_limits: {},
    version: 1,
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-01T09:00:00Z',
  };
}

/** A dashboard of nothing, which each test fills with the tile it reads. */
function empty(): Dashboard {
  return {
    period: { from: '2026-09-08', to: '2026-10-07' },
    open_by_state: [],
    open_by_severity: (['critical', 'high', 'medium', 'low', 'cosmetic'] as const).map(
      (severity) => ({ severity, count: 0 }),
    ),
    security: [
      { class: 'live', count: 0, oldest: null },
      { class: 'boundary', count: 0, oldest: null },
    ],
    blocked: { count: 0, oldest: null },
    age: [
      { from_days: 0, to_days: 7, count: 0 },
      { from_days: 7, to_days: 30, count: 0 },
      { from_days: 30, to_days: 90, count: 0 },
      { from_days: 90, to_days: 365, count: 0 },
      { from_days: 365, to_days: null, count: 0 },
    ],
    throughput: [],
    lead_time: { from: '2026-09-08', to: '2026-10-07', done: 0, median_seconds: null },
    decisions: { count: 0, oldest: null },
    time: { total_minutes: 0, projects: [] },
    recent: [],
  };
}

describe('TenantDashboard', () => {
  let tenant: WritableSignal<string | null>;
  let oversight: WritableSignal<boolean>;
  let membership: WritableSignal<Membership | undefined>;
  let projects: {
    list: WritableSignal<Project[]>;
    projects: { isLoading: WritableSignal<boolean> };
  };
  let value: WritableSignal<Dashboard | undefined>;
  let error: WritableSignal<unknown>;
  let params: () => DashboardQuery | undefined;
  let navigate: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    oversight = signal(false);
    membership = signal<Membership | undefined>({
      role: 'member',
      team: { name: 'Acme Corp', slug: 'acme' },
      tenant: { name: 'Acme Corp', slug: 'acme' },
      origins: [{ source: 'grant', role: 'member' }],
      can_create_projects: true,
    });
    projects = { list: signal<Project[]>([]), projects: { isLoading: signal(false) } };
    value = signal<Dashboard | undefined>(undefined);
    error = signal<unknown>(undefined);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        {
          provide: SessionService,
          useValue: {
            tenant,
            oversight,
            workTenant: computed(() => (oversight() ? null : tenant())),
            shown: computed(() => {
              const held = membership();
              return held ? { ...held.team, role: held.role } : undefined;
            }),
          },
        },
        { provide: ProjectsService, useValue: projects },
        {
          provide: DashboardService,
          useValue: {
            dashboard: (given: () => DashboardQuery | undefined) => {
              params = given;
              return { value, hasValue: () => value() !== undefined, error };
            },
          },
        },
        { provide: Clock, useValue: { now: signal(now) } },
      ],
    });
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
  });

  async function render(inputs: Record<string, unknown> = {}) {
    const fixture = TestBed.createComponent(TenantDashboard);
    for (const [name, input] of Object.entries(inputs)) {
      fixture.componentRef.setInput(name, input);
    }
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  async function show(fixture: ComponentFixture<TenantDashboard>, dashboard: Dashboard) {
    value.set(dashboard);
    await fixture.whenStable();
  }

  const text = (page: HTMLElement, selector: string) =>
    page.querySelector(selector)?.textContent?.replace(/\s+/g, ' ').trim();
  const testId = (id: string) => `[data-testid="${id}"]`;

  describe('the request', () => {
    it('asks for every project and the thirty days that end today, in the tenant of the page', async () => {
      await render();

      expect(params()).toEqual({
        team: 'acme',
        project: [],
        from: '2026-09-08',
        to: '2026-10-07',
      });
    });

    it('asks with the projects and the period of the address', async () => {
      await render({ project: ['ALPHA', '!BETA'], from: '2026-09-01', to: '2026-09-30' });

      expect(params()).toEqual({
        team: 'acme',
        project: ['ALPHA', '!BETA'],
        from: '2026-09-01',
        to: '2026-09-30',
      });
    });

    it('takes one project of the address as a list of one', async () => {
      await render({ project: 'ALPHA' });

      expect(params()?.project).toEqual(['ALPHA']);
    });

    it('asks for nothing for a period that ends before it begins, and says so', async () => {
      const { page } = await render({ from: '2026-10-08', to: '2026-10-07' });

      expect(params()).toBeUndefined();
      expect(text(page, testId('dashboard-backwards'))).toContain('ends before it begins');
    });

    it('asks for nothing in a tenant a global administrator holds no role in (docs/adr/0034 D2)', async () => {
      oversight.set(true);
      membership.set(undefined);

      const { page } = await render();

      expect(params()).toBeUndefined();
      expect(page.querySelector('.tiles')).toBeNull();
      expect(text(page, 'h1')).toBe('acme');
    });
  });

  describe('the filters', () => {
    it('shows the period in the date fields', async () => {
      const { page } = await render({ from: '2026-09-01' });

      expect(page.querySelector<HTMLInputElement>(testId('dashboard-from'))?.value).toBe(
        '2026-09-01',
      );
      expect(page.querySelector<HTMLInputElement>(testId('dashboard-to'))?.value).toBe(
        '2026-10-07',
      );
    });

    it('puts a day the person picks into the address, keeping the other filters', async () => {
      const { fixture, page } = await render({ project: ['ALPHA'] });

      const field = page.querySelector<HTMLInputElement>(testId('dashboard-from'))!;
      field.value = '2026-09-15';
      field.dispatchEvent(new Event('input'));
      await fixture.whenStable();

      expect(navigate).toHaveBeenCalledWith(['/t', 'acme'], {
        queryParams: { project: ['ALPHA'], from: '2026-09-15', to: undefined },
      });
    });

    it('puts the projects chosen into the address and keeps the ones the filter leaves out', async () => {
      const { fixture } = await render({ project: ['!BETA'] });

      (
        fixture.componentInstance as unknown as { chooseProjects(keys: string[]): void }
      ).chooseProjects(['ALPHA']);

      expect(navigate).toHaveBeenCalledWith(['/t', 'acme'], {
        queryParams: { project: ['ALPHA', '!BETA'], from: undefined, to: undefined },
      });
    });

    it('offers to go back to every project and the last thirty days once a filter is set', async () => {
      const unfiltered = await render();
      expect(unfiltered.page.querySelector(testId('dashboard-clear'))).toBeNull();

      const { page } = await render({ to: '2026-09-30' });
      page.querySelector<HTMLButtonElement>(testId('dashboard-clear'))?.click();

      expect(navigate).toHaveBeenCalledWith(['/t', 'acme']);
    });

    it('offers the projects of the tenant by key and name', async () => {
      projects.list.set([project('ALPHA', 'Alpha')]);
      const { fixture } = await render();

      expect(
        (fixture.componentInstance as unknown as { projectOptions(): unknown }).projectOptions(),
      ).toEqual([{ label: 'ALPHA · Alpha', value: 'ALPHA' }]);
    });
  });

  describe('a dashboard that could not be loaded', () => {
    it('says why', async () => {
      const body: Problem = {
        type: 'about:blank',
        title: 'Not ready',
        status: 503,
        detail: 'The database is starting.',
        code: 'not_ready',
      };
      error.set(new HttpErrorResponse({ status: 503, error: body }));

      const { page } = await render();

      expect(text(page, testId('dashboard-failed'))).toBe(
        'The dashboard could not be loaded: The database is starting.',
      );
    });
  });

  describe('while it loads', () => {
    it('shows a skeleton in every tile and no count', async () => {
      const { page } = await render();

      expect(page.querySelectorAll('.tile p-skeleton')).toHaveLength(9);
      expect(page.querySelector(testId('open-count'))).toBeNull();
    });
  });

  describe('tile 1, open tickets by state', () => {
    it('sums every project per state, in the order of the states, with its bar', async () => {
      projects.list.set([project('ALPHA', 'Alpha'), project('BETA', 'Beta')]);
      const { fixture, page } = await render();
      await show(fixture, {
        ...empty(),
        open_by_state: [
          { project: 'ALPHA', state: 'blocked', count: 1 },
          { project: 'ALPHA', state: 'filed', count: 2 },
          { project: 'BETA', state: 'filed', count: 1 },
        ],
      });

      const tile = page.querySelector(testId('tile-state'))!;
      expect(text(tile as HTMLElement, '.figure')).toBe('4');
      expect(
        [...tile.querySelectorAll('.legend > span')].map((part) => part.textContent?.trim()),
      ).toEqual(['filed 3', 'blocked 1']);
      const segments = [...tile.querySelectorAll<HTMLElement>('.segment')];
      expect(segments.map((segment) => segment.style.width)).toEqual(['75%', '25%']);
      expect(segments.map((segment) => segment.style.background)).toEqual([
        'var(--p-state-filed)',
        'var(--p-state-blocked)',
      ]);
      expect(text(page, testId('open-count'))).toBe('4 open tickets across 2 projects');
    });

    it('says nothing is open', async () => {
      const { fixture, page } = await render();
      await show(fixture, empty());

      expect(text(page, testId('tile-state'))).toContain('Nothing open');
      expect(text(page, testId('open-count'))).toBe('0 open tickets across 0 projects');
    });
  });

  describe('tile 2, open by severity', () => {
    it('draws every severity against the largest, coloured by its token', async () => {
      const { fixture, page } = await render();
      await show(fixture, {
        ...empty(),
        open_by_severity: [
          { severity: 'critical', count: 1 },
          { severity: 'high', count: 4 },
          { severity: 'medium', count: 2 },
          { severity: 'low', count: 0 },
          { severity: 'cosmetic', count: 0 },
        ],
      });

      const row = (severity: string) => page.querySelector(testId(`severity-${severity}`))!;
      expect(row('high').querySelector('.count')?.textContent).toBe('4');
      expect(row('critical').querySelector<HTMLElement>('.track span')?.style.width).toBe('25%');
      expect(row('critical').querySelector<HTMLElement>('.track span')?.style.background).toBe(
        'var(--p-severity-critical)',
      );
      expect(row('low').querySelector<HTMLElement>('.track span')?.style.width).toBe('0%');
    });
  });

  describe('tile 3, open security findings', () => {
    it('counts live and boundary and links the oldest of each', async () => {
      const { fixture, page } = await render();
      await show(fixture, {
        ...empty(),
        security: [
          {
            class: 'live',
            count: 2,
            oldest: { key: 'acme/ALPHA-3', title: 'A guarantee broken', since: before(7) },
          },
          { class: 'boundary', count: 0, oldest: null },
        ],
      });

      const live = page.querySelector(testId('security-live'))!;
      expect(live.querySelector('.count')?.textContent).toBe('2');
      const oldest = page.querySelector(testId('security-live-oldest'))!;
      expect(oldest.getAttribute('href')).toBe('/t/acme/tickets/ALPHA-3');
      expect(oldest.textContent).toContain('A guarantee broken');
      expect(live.textContent).toContain('oldest, filed last week');
      expect(text(page, testId('security-boundary'))).toContain('None open');
    });
  });

  describe('tile 4, blocked', () => {
    it('counts the blocked tickets and names the one blocked longest with its kind', async () => {
      const { fixture, page } = await render();
      await show(fixture, {
        ...empty(),
        blocked: {
          count: 3,
          oldest: {
            key: 'acme/ALPHA-9',
            title: 'Waits for legal',
            since: before(3),
            kind: 'human',
          },
        },
      });

      expect(text(page, `${testId('tile-blocked')} .figure`)).toBe('3');
      expect(page.querySelector(testId('blocked-oldest'))?.getAttribute('href')).toBe(
        '/t/acme/tickets/ALPHA-9',
      );
      expect(text(page, testId('blocked-since'))).toBe(
        'blocked longest, since 3 days ago, waits on human',
      );
    });

    it('says nothing is blocked', async () => {
      const { fixture, page } = await render();
      await show(fixture, empty());

      expect(text(page, testId('tile-blocked'))).toContain('Nothing is blocked');
    });
  });

  describe('tile 5, age of the open tickets', () => {
    it('names the five buckets and draws them against the largest', async () => {
      const { fixture, page } = await render();
      const dashboard = empty();
      dashboard.age = dashboard.age.map((bucket, index) => ({
        ...bucket,
        count: [4, 2, 1, 0, 1][index],
      }));
      await show(fixture, dashboard);

      const rows = [...page.querySelectorAll(`${testId('tile-age')} .bar-row`)];
      expect(rows.map((row) => row.querySelector('.label')?.textContent)).toEqual([
        'Under a week',
        '1 to 4 weeks',
        '1 to 3 months',
        '3 to 12 months',
        'A year or more',
      ]);
      expect(rows.map((row) => row.querySelector('.count')?.textContent)).toEqual([
        '4',
        '2',
        '1',
        '0',
        '1',
      ]);
      expect(rows[1].querySelector<HTMLElement>('.track span')?.style.width).toBe('50%');
    });
  });

  describe('tile 6, done per week', () => {
    it('draws a column per week against the busiest, the week the period cut said so far', async () => {
      const { fixture, page } = await render();
      await show(fixture, {
        ...empty(),
        throughput: [
          { week: '2026-W40', from: '2026-09-28', to: '2026-10-04', done: 4 },
          { week: '2026-W41', from: '2026-10-05', to: '2026-10-07', done: 1 },
        ],
      });

      const full = page.querySelector(testId('week-2026-W40'))!;
      const cut = page.querySelector(testId('week-2026-W41'))!;
      expect(full.querySelector('.count')?.textContent).toBe('4');
      expect(full.querySelector<HTMLElement>('.column span')?.style.height).toBe('100%');
      expect(cut.querySelector<HTMLElement>('.column span')?.style.height).toBe('25%');
      expect(full.textContent).not.toContain('so far');
      expect(cut.textContent).toContain('so far');
      expect(cut.querySelector('[title]')?.getAttribute('title')).toBe(
        '2026-W41, 2026-10-05 to 2026-10-07',
      );
    });
  });

  describe('tile 7, lead time', () => {
    it('says the median and over how many tickets', async () => {
      const { fixture, page } = await render();
      await show(fixture, {
        ...empty(),
        lead_time: { from: '2026-09-08', to: '2026-10-07', done: 5, median_seconds: 3.5 * 86400 },
      });

      expect(text(page, testId('lead-time'))).toBe('3.5 days');
      expect(text(page, testId('tile-lead-time'))).toContain(
        'median from filed to done, of 5 done',
      );
    });

    it('says nothing was done in the window', async () => {
      const { fixture, page } = await render();
      await show(fixture, empty());

      expect(text(page, testId('lead-time'))).toBe('—');
      expect(text(page, testId('tile-lead-time'))).toContain('Nothing done');
    });
  });

  describe('tile 8, open decisions', () => {
    it('counts the open questions and links the one asked first', async () => {
      const { fixture, page } = await render();
      await show(fixture, {
        ...empty(),
        decisions: {
          count: 2,
          oldest: {
            key: 'acme/ALPHA-4',
            title: 'Pick a store',
            number: 2,
            question: 'PostgreSQL or SQLite?',
            since: before(1),
          },
        },
      });

      expect(text(page, `${testId('tile-decisions')} .figure`)).toBe('2');
      const oldest = page.querySelector(testId('decisions-oldest'))!;
      expect(oldest.getAttribute('href')).toBe('/t/acme/tickets/ALPHA-4');
      expect(text(oldest as HTMLElement, '.question')).toBe('PostgreSQL or SQLite?');
      expect(text(oldest as HTMLElement, '.small')).toBe('Q2 on ALPHA-4, asked yesterday');
    });

    it('says no question is open', async () => {
      const { fixture, page } = await render();
      await show(fixture, empty());

      expect(text(page, testId('tile-decisions'))).toContain('No question is open');
    });
  });

  describe('tile 9, time booked', () => {
    it('sums the period and draws each project against the largest', async () => {
      const { fixture, page } = await render();
      await show(fixture, {
        ...empty(),
        time: {
          total_minutes: 150,
          projects: [
            { project: 'ALPHA', name: 'Alpha', minutes: 120 },
            { project: 'BETA', name: 'Beta', minutes: 30 },
          ],
        },
      });

      expect(text(page, `${testId('tile-time')} .figure`)).toBe('2 h 30 min');
      expect(text(page, `${testId('time-ALPHA')} .count`)).toBe('2 h');
      expect(
        page.querySelector<HTMLElement>(`${testId('time-BETA')} .track span`)?.style.width,
      ).toBe('25%');
      expect(page.querySelector(`${testId('time-ALPHA')} .label`)?.getAttribute('title')).toBe(
        'Alpha',
      );
    });

    it('says nothing was booked in the period', async () => {
      const { fixture, page } = await render();
      await show(fixture, empty());

      expect(text(page, testId('tile-time'))).toContain('Nothing booked');
    });
  });

  describe('the projects', () => {
    it('shows a card per project of the filter with its open tickets by state, linked to its board', async () => {
      projects.list.set([project('ALPHA', 'Alpha', 'The first'), project('BETA', 'Beta')]);
      const { fixture, page } = await render({ project: ['!BETA'] });
      await show(fixture, {
        ...empty(),
        open_by_state: [{ project: 'ALPHA', state: 'decided', count: 2 }],
      });

      const card = page.querySelector(testId('project-ALPHA'))!;
      expect(card.getAttribute('href')).toBe('/t/acme/p/ALPHA/board');
      expect(card.querySelector('.count')?.textContent).toBe('2');
      expect(card.querySelector('.description')?.textContent).toBe('The first');
      expect(text(card as HTMLElement, '.legend')).toBe('decided 2');
      expect(page.querySelector(testId('project-BETA'))).toBeNull();
    });

    it('says no project matches a filter that names none of them', async () => {
      projects.list.set([project('ALPHA', 'Alpha')]);
      const { page } = await render({ project: 'NOSUCH' });

      expect(text(page, '.projects')).toBe('No project of the team matches the filter.');
    });

    it('says the tenant has no projects yet', async () => {
      const { page } = await render();

      expect(text(page, '.projects')).toBe('This team has no projects yet.');
    });
  });

  describe('the recently updated tickets', () => {
    it('lists them as the server orders them, each linked to its page', async () => {
      const { fixture, page } = await render();
      await show(fixture, {
        ...empty(),
        recent: [
          {
            key: 'acme/ALPHA-7',
            type: 'bug',
            title: 'The board flickers',
            state: 'blocked',
            updated_at: '2026-10-07T11:55:00Z',
          },
          {
            key: 'acme/ALPHA-2',
            type: 'task',
            title: 'Older',
            state: 'filed',
            updated_at: '2026-10-06T12:00:00Z',
          },
        ],
      });

      const rows = [...page.querySelectorAll('.recent [data-testid^="recent-"]')];
      expect(rows.map((row) => row.getAttribute('data-testid'))).toEqual([
        'recent-acme/ALPHA-7',
        'recent-acme/ALPHA-2',
      ]);
      expect(rows[0].getAttribute('href')).toBe('/t/acme/tickets/ALPHA-7');
      expect(rows[0].querySelector('.ticket-key')?.textContent).toBe('ALPHA-7');
      expect(rows[0].querySelector('app-state [data-state]')?.getAttribute('data-state')).toBe(
        'blocked',
      );
    });

    it('says no ticket is open', async () => {
      const { fixture, page } = await render();
      await show(fixture, empty());

      expect(text(page, '.recent .empty')).toBe('No open tickets.');
    });
  });
});
