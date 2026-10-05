import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Component, computed, signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { MessageService } from 'primeng/api';
import { Paginator } from 'primeng/paginator';
import { Select } from 'primeng/select';
import { Subject } from 'rxjs';
import type { Mock } from 'vitest';
import { provideApiConfiguration } from '../../api/api-configuration';
import { ListTenantTickets$Params } from '../../api/functions';
import { Member, Problem, Project, SavedFilter, Ticket, TicketList } from '../../api/models';
import { EntityCache } from '../../core/entity-cache';
import { EventStreamService, StreamEvent } from '../../core/event-stream.service';
import { MembersService } from '../../core/members.service';
import { ProjectsService } from '../../core/projects.service';
import { SavedFiltersService } from '../../core/saved-filters.service';
import { SessionService } from '../../core/session.service';
import { listReloadDelay, TicketPage, TicketsService } from '../../core/tickets.service';
import { Clock } from '../../shared/time';
import { SavedFilters } from '../project/saved-filters';
import { TenantTickets, textDelay } from './tenant-tickets';

const now = Date.parse('2026-10-05T12:00:00Z');

function ticket(key: string, overrides: Partial<Ticket> = {}): Ticket {
  const [, short] = key.split('/');
  const [project, number] = short.split('-');
  return {
    id: `t-${short}`,
    key,
    number: Number(number),
    project,
    title: `Ticket ${short}`,
    body: '',
    type: 'task',
    state: 'filed',
    severity: 'medium',
    security: 'none',
    confidential: false,
    assignee: null,
    reporter: { id: 'p-ada', display_name: 'Ada Lovelace' },
    reporter_agent: null,
    reporter_token: null,
    block: null,
    threat: null,
    parent: null,
    effort: 'M',
    progress: 0,
    progress_refinement: 0,
    progress_review: 0,
    progress_derived: false,
    urgency: 'later',
    urgency_derived: 'later',
    urgency_override: null,
    urgency_rule: 'v2:default',
    opened_at: '2026-10-01T09:00:00Z',
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-05T11:00:00Z',
    decided_at: null,
    done_at: null,
    done_from: null,
    done_by_hand: false,
    open_prerequisites: 0,
    version: 1,
    ...overrides,
  };
}

function project(key: string, name: string): Project {
  return {
    id: `id-${key}`,
    key,
    name,
    description: '',
    restricted: false,
    wip_limits: {},
    version: 1,
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-01T09:00:00Z',
  };
}

function member(id: string, name: string, username: string | null): Member {
  return {
    person: { id, display_name: name, username },
    role: 'member',
    origins: [{ source: 'grant', role: 'member' }],
    local: true,
    email: null,
  } as Member;
}

function saved(parameters: SavedFilter['parameters']): SavedFilter {
  return {
    id: 'f-1',
    name: 'Across',
    owner: { id: 'p-ada', display_name: 'Ada', username: 'ada' },
    shared: false,
    parameters,
    redacted: false,
    warnings: [],
    version: 1,
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
  };
}

/** Where a row of the list leads: a ticket's page, which this test does not render. */
@Component({ template: '' })
class Elsewhere {}

const routes = [
  { path: 't/:tenant/tickets', component: TenantTickets },
  { path: '**', component: Elsewhere },
];

/** What the page reads of the session, made of signals the test sets. */
function sessionOf(tenant: WritableSignal<string | null>, oversight: WritableSignal<boolean>) {
  return {
    tenant,
    oversight,
    workTenant: computed(() => (oversight() ? null : tenant())),
    person: signal({ id: 'p-ada' }),
  };
}

/** Lets what a click, a choice or a navigation started finish, and shows it. */
async function settle(harness: RouterTestingHarness): Promise<void> {
  for (let round = 0; round < 3; round++) {
    await new Promise((resolve) => setTimeout(resolve));
    harness.detectChanges();
  }
}

describe('TenantTickets', () => {
  let tenant: WritableSignal<string | null>;
  let oversight: WritableSignal<boolean>;
  let projects: WritableSignal<Project[]>;
  let cache: EntityCache<Ticket>;
  let request: () => ListTenantTickets$Params | undefined;
  /** The list of the service as the page sees it: signals the test sets. */
  let list: {
    value: WritableSignal<TicketPage | undefined>;
    hasValue: () => boolean;
    isLoading: WritableSignal<boolean>;
    error: WritableSignal<unknown>;
    reload: Mock<() => boolean>;
  };
  let savedFilters: WritableSignal<SavedFilter[]>;
  let harness: RouterTestingHarness;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    oversight = signal(false);
    projects = signal([project('COW', 'Cowork'), project('OPS', 'Operations')]);
    cache = new EntityCache<Ticket>();
    const value = signal<TicketPage | undefined>(undefined);
    list = {
      value,
      hasValue: () => value() !== undefined,
      isLoading: signal(false),
      error: signal<unknown>(undefined),
      reload: vi.fn<() => boolean>(() => true),
    };
    savedFilters = signal<SavedFilter[]>([]);
    TestBed.configureTestingModule({
      providers: [
        provideRouter(routes),
        MessageService,
        { provide: SessionService, useValue: sessionOf(tenant, oversight) },
        {
          provide: ProjectsService,
          useValue: {
            list: projects,
            byKey: (key: string) => projects().find((each) => each.key === key),
          },
        },
        {
          provide: MembersService,
          useValue: {
            list: signal([
              member('p-ada', 'Ada Lovelace', 'ada'),
              member('p-sam', 'Sam Rivera', null),
            ]),
          },
        },
        {
          provide: TicketsService,
          useValue: {
            cache,
            tenantTickets: (params: () => ListTenantTickets$Params | undefined) => {
              request = params;
              return list;
            },
          },
        },
        {
          provide: SavedFiltersService,
          useValue: {
            list: savedFilters,
            reload: vi.fn(),
            create: vi.fn(),
            update: vi.fn(),
            remove: vi.fn(),
          },
        },
        { provide: Clock, useValue: { now: signal(now) } },
      ],
    });
  });

  async function open(url = '/t/acme/tickets'): Promise<HTMLElement> {
    harness = await RouterTestingHarness.create();
    await harness.navigateByUrl(url, TenantTickets);
    await settle(harness);
    return harness.routeNativeElement as HTMLElement;
  }

  /** Puts the tickets into the cache and shows their keys as the page of the list the server answered. */
  async function load(tickets: Ticket[], total = tickets.length) {
    for (const each of tickets) {
      cache.put(each.key, each);
    }
    list.value.set({ keys: tickets.map((each) => each.key), total, nextCursor: null });
    await settle(harness);
  }

  const page = () => harness.routeNativeElement as HTMLElement;
  const el = (testId: string) => page().querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const url = () => TestBed.inject(Router).url;
  const select = (name: string) =>
    harness.fixture.debugElement.query(By.css(`[data-testid="filter-${name}"]`))
      .componentInstance as Select;
  async function choose(name: string, values: string[] | null) {
    harness.fixture.debugElement
      .query(By.css(`[data-testid="filter-${name}"]`))
      .triggerEventHandler('ngModelChange', values);
    await settle(harness);
  }
  const paginator = () => harness.fixture.debugElement.query(By.directive(Paginator));
  async function turn(pageIndex: number, rows: number) {
    paginator().triggerEventHandler('onPageChange', {
      page: pageIndex,
      rows,
      first: pageIndex * rows,
    });
    await settle(harness);
  }
  const bar = () =>
    harness.fixture.debugElement.query(By.directive(SavedFilters))
      .componentInstance as SavedFilters;

  describe('the list', () => {
    it('asks for the first page of fifty of the tenant’s tickets, filtered as the address says', async () => {
      await open(
        '/t/acme/tickets?state=filed&state=!blocked&severity=high&project=COW&q=crash&blocked=true',
      );

      expect(request()).toEqual({
        tenant: 'acme',
        project: ['COW'],
        state: ['filed', '!blocked'],
        severity: ['high'],
        q: 'crash',
        blocked: true,
        page: 1,
        per_page: 50,
      });
    });

    it('asks for every open ticket of the tenant without a filter', async () => {
      await open();

      expect(request()).toEqual({ tenant: 'acme', page: 1, per_page: 50 });
    });

    // docs/adr/0034 D2: the tenant's work is its members'.
    it('asks for nothing and shows none of the work where a global administrator only oversees', async () => {
      oversight.set(true);

      await open();

      expect(request()).toBeUndefined();
      expect(el('tickets-filters')).toBeNull();
      expect(el('tickets-table')).toBeNull();
    });

    it('shows each ticket with its key and its project beside it, each a link', async () => {
      await open();
      await load(
        [
          ticket('acme/COW-2', {
            title: 'The export forgets the files',
            urgency: 'now',
            state: 'in-progress',
            assignee: { id: 'p-sam', display_name: 'Sam Rivera' },
          }),
          ticket('acme/OLD-7'),
        ],
        2,
      );

      const row = el('row-acme/COW-2');
      expect(row?.querySelector('[data-testid="row-key"]')?.getAttribute('href')).toBe(
        '/t/acme/tickets/COW-2',
      );
      expect(row?.querySelector('[data-testid="row-key"]')?.textContent?.trim()).toBe('COW-2');
      const projectLink = row?.querySelector('[data-testid="row-project"]');
      expect(projectLink?.textContent?.trim()).toBe('Cowork');
      expect(projectLink?.getAttribute('href')).toBe('/t/acme/p/COW/board');
      expect(row?.querySelector('[data-testid="row-title"]')?.textContent).toBe(
        'The export forgets the files',
      );
      expect(row?.querySelector('[data-testid="row-horizon"]')?.textContent?.trim()).toBe('now');
      expect(row?.textContent).toContain('Sam Rivera');
      expect(row?.textContent).toContain('1 hour ago');
      // A project the list of projects does not hold, an archived one, by its key.
      expect(
        el('row-acme/OLD-7')?.querySelector('[data-testid="row-project"]')?.textContent?.trim(),
      ).toBe('OLD');
      expect(el('tickets-total')?.textContent).toBe('2 tickets');
    });

    it('opens a ticket from its row', async () => {
      await open();
      await load([ticket('acme/COW-2')]);

      el('row-acme/COW-2')?.click();
      await settle(harness);

      expect(url()).toBe('/t/acme/tickets/COW-2');
    });

    it('says when there is no open ticket, and when none matches the filters', async () => {
      await open();
      await load([]);
      expect(el('tickets-empty')?.textContent?.trim()).toBe('No open tickets.');

      await choose('severity', ['critical']);
      list.value.set({ keys: [], total: 0, nextCursor: null });
      await settle(harness);

      expect(el('tickets-empty')?.textContent?.trim()).toBe('No ticket matches the filters.');
    });

    // docs/adr/0049 D4: a link with a value the API does not take says which.
    it('says why the list could not be loaded, each refused filter by its name', async () => {
      await open('/t/acme/tickets?state=triaged');
      const body: Problem = {
        type: 'about:blank',
        title: 'Validation failed',
        status: 400,
        code: 'validation_failed',
        detail: 'the list request has values this route does not take',
        errors: [{ pointer: 'query:state', message: 'not a value of state: triaged' }],
      };
      list.error.set(new HttpErrorResponse({ status: 400, error: body }));
      await settle(harness);

      expect(el('tickets-failure')?.textContent).toBe(
        'The tickets could not be loaded: the list request has values this route does not take ' +
          '(state: not a value of state: triaged)',
      );
      expect(el('tickets-failure')?.getAttribute('role')).toBe('alert');
      expect(el('tickets-pages')).toBeNull();
    });
  });

  describe('the filters (docs/adr/0049 D1)', () => {
    it('offers a select for each filter of the backlog and for the project, the values in their order', async () => {
      await open();

      const values = (name: string) =>
        (select(name).options() as { value: string }[]).map((option) => option.value);
      expect(values('project')).toEqual(['COW', 'OPS']);
      expect(select('project').options()?.[0].label).toBe('COW · Cowork');
      expect(values('state')).toEqual([
        'filed',
        'analysed',
        'decided',
        'in-progress',
        'review',
        'blocked',
        'done',
        'dropped',
      ]);
      expect(values('type')).toEqual(['task', 'bug', 'feature', 'decision', 'question']);
      expect(values('severity')).toEqual(['critical', 'high', 'medium', 'low', 'cosmetic']);
      expect(values('security')).toEqual(['live', 'boundary', 'hardening', 'none']);
      expect(values('urgency')).toEqual(['now', 'release', 'next', 'later', 'icebox']);
      expect(values('effort')).toEqual(['XS', 'S', 'M', 'L']);
      expect(select('assignee').options()).toEqual([
        { value: 'me', label: 'Me' },
        { value: 'none', label: 'Unassigned' },
        { value: 'p-ada', label: 'Ada Lovelace (ada)' },
        { value: 'p-sam', label: 'Sam Rivera' },
      ]);
      expect(values('reporter')).toEqual(['me', 'p-ada', 'p-sam']);
      // The page says horizon where the API says urgency (docs/adr/0010 D3).
      expect(page().querySelector('#tickets-urgency-label')?.textContent).toBe('Horizon');
      for (const name of ['state', 'type', 'assignee']) {
        expect(select(name).multiple()).toBe(true);
      }
    });

    it('writes a choice into the address, which the list follows', async () => {
      await open();

      await choose('severity', ['high', 'critical']);

      expect(url()).toBe('/t/acme/tickets?severity=high&severity=critical');
      expect(request()).toMatchObject({ severity: ['high', 'critical'], page: 1 });
      expect(select('severity').value).toEqual(['high', 'critical']);
    });

    it('takes a select’s choice away from the address when it is cleared', async () => {
      await open('/t/acme/tickets?severity=high&type=bug');

      await choose('severity', null);

      expect(url()).toBe('/t/acme/tickets?type=bug');
    });

    it('keeps the negated values of a parameter when its select changes, and names them under the bar', async () => {
      await open('/t/acme/tickets?state=filed&state=!blocked');
      expect(select('state').value).toEqual(['filed']);
      expect(el('tickets-beyond')?.textContent).toBe('Also filtered by state=!blocked');

      await choose('state', ['review']);

      expect(url()).toBe('/t/acme/tickets?state=review&state=!blocked');
    });

    it('names under the bar the conditions it has no control for', async () => {
      await open('/t/acme/tickets?blocked=true&interest=me&severity=high');

      expect(el('tickets-beyond')?.textContent).toBe('Also filtered by interest=me · blocked=true');
    });

    it('shows a chosen value its select does not offer, under its own name', async () => {
      await open('/t/acme/tickets?project=OLD&assignee=p-gone');

      expect(select('project').value).toEqual(['OLD']);
      expect(select('project').options()?.at(-1)).toEqual({ value: 'OLD', label: 'OLD' });
      expect(select('assignee').options()?.at(-1)).toEqual({ value: 'p-gone', label: 'p-gone' });
    });

    it('clears every filter at once', async () => {
      await open('/t/acme/tickets?severity=high&blocked=true&q=crash');

      el('tickets-clear')?.click();
      await settle(harness);

      expect(url()).toBe('/t/acme/tickets');
      expect(el('tickets-clear')).toBeNull();
    });

    describe('the text', () => {
      const typing = async (text: string) => {
        const input = el('tickets-text') as HTMLInputElement;
        input.value = text;
        input.dispatchEvent(new Event('input'));
        harness.detectChanges();
      };
      const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

      it('shows the address’s text', async () => {
        await open('/t/acme/tickets?q=flicker');

        expect((el('tickets-text') as HTMLInputElement).value).toBe('flicker');
      });

      it('goes into the address once the person stops typing, and keeps a space typed at its end', async () => {
        await open();

        await typing('crash ');
        expect(url()).toBe('/t/acme/tickets');
        await wait(textDelay + 50);
        await settle(harness);

        expect(url()).toBe('/t/acme/tickets?q=crash');
        expect(request()?.q).toBe('crash');
        expect((el('tickets-text') as HTMLInputElement).value).toBe('crash ');
      });

      it('takes the text away from the address when the field is emptied', async () => {
        await open('/t/acme/tickets?q=crash&type=bug');

        await typing('');
        await wait(textDelay + 50);
        await settle(harness);

        expect(url()).toBe('/t/acme/tickets?type=bug');
      });
    });
  });

  describe('the numbered pages (docs/adr/0048 D2, D4)', () => {
    it('shows the total and pages of 25, 50 or 100, fifty to begin with', async () => {
      await open();
      await load([ticket('acme/COW-1')], 120);

      const pages = paginator().componentInstance as Paginator;
      expect(pages.totalRecords()).toBe(120);
      expect(pages.rows()).toBe(50);
      expect(pages.first()).toBe(0);
      expect(pages.rowsPerPageOptions()).toEqual([25, 50, 100]);
      expect(el('tickets-total')?.textContent).toBe('120 tickets');
    });

    it('turns to the page asked for, and keeps the rows and the total while it loads', async () => {
      await open();
      await load([ticket('acme/COW-1')], 120);

      await turn(1, 50);
      list.value.set(undefined);
      list.isLoading.set(true);
      await settle(harness);

      expect(request()).toMatchObject({ page: 2, per_page: 50 });
      expect(el('row-acme/COW-1')).not.toBeNull();
      expect(el('tickets-total')?.textContent).toBe('120 tickets');
      expect((paginator().componentInstance as Paginator).first()).toBe(50);
      expect(page().querySelector('.panel')?.getAttribute('aria-busy')).toBe('true');
    });

    it('starts at the first page with another size, and with another filter', async () => {
      await open();
      await load([ticket('acme/COW-1')], 120);
      await turn(2, 50);
      expect(request()).toMatchObject({ page: 3 });

      await turn(2, 25);
      expect(request()).toMatchObject({ page: 1, per_page: 25 });

      await turn(3, 25);
      expect(request()).toMatchObject({ page: 4 });
      await choose('type', ['bug']);
      expect(request()).toMatchObject({ page: 1, per_page: 25, type: ['bug'] });
    });

    // The tickets of the later pages were closed meanwhile: the answer is empty, its total smaller.
    // The paginator steps back one page by itself; the page goes to the last one in one step, and
    // to the first where nothing is left, which shows no paginator at all.
    it.each([
      [30, 1],
      [60, 2],
      [0, 1],
    ])(
      'goes to the last page when the list shrank under a later one, to %i tickets',
      async (total, last) => {
        await open();
        await load([ticket('acme/COW-1')], 120);
        await turn(2, 50);
        expect(request()).toMatchObject({ page: 3 });

        list.value.set({ keys: [], total, nextCursor: null });
        await settle(harness);

        expect(request()).toMatchObject({ page: last });
      },
    );

    it('starts another tenant at its first page', async () => {
      await open();
      await load([ticket('acme/COW-1')], 120);
      await turn(1, 50);

      tenant.set('globex');
      await settle(harness);

      expect(request()).toMatchObject({ tenant: 'globex', page: 1 });
    });
  });

  describe('a saved filter (docs/adr/0018 D5, docs/adr/0049 D6)', () => {
    const apply = async (filter: SavedFilter | null) => {
      bar().chosen.emit(filter);
      await settle(harness);
    };

    it('hands the bar what the address applies, to save', async () => {
      await open('/t/acme/tickets?project=COW&severity=high&q=crash&blocked=true');

      expect(bar().current()).toEqual({
        project: ['COW'],
        severity: ['high'],
        q: 'crash',
        blocked: true,
      });
    });

    it('replaces the address’s conditions with its own, the project among them, and leaves none out', async () => {
      await open('/t/acme/tickets?severity=low');
      const filter = saved({ project: ['OPS'], state: ['!done'], q: 'deploy', blocked: true });

      await apply(filter);

      expect(url()).toBe('/t/acme/tickets?project=OPS&state=!done&q=deploy&blocked=true');
      expect(request()).toEqual({
        tenant: 'acme',
        project: ['OPS'],
        state: ['!done'],
        q: 'deploy',
        blocked: true,
        page: 1,
        per_page: 50,
      });
      expect(bar().applied()).toBe(filter);
      expect(bar().leftOut()).toEqual({});
      expect(el('filter-notes')).toBeNull();
      expect((el('tickets-text') as HTMLInputElement).value).toBe('deploy');
    });

    it('clears every condition when none is chosen', async () => {
      await open();
      await apply(saved({ severity: ['high'] }));

      await apply(null);

      expect(url()).toBe('/t/acme/tickets');
      expect(bar().applied()).toBeNull();
    });

    it('starts another tenant without the filter applied', async () => {
      await open();
      await apply(saved({ severity: ['high'] }));

      tenant.set('globex');
      await settle(harness);

      expect(bar().applied()).toBeNull();
    });
  });
});

describe('TenantTickets, live (docs/adr/0054)', () => {
  const tenantUrl = '/api/v1/tenants/acme/tickets';
  let stream: Subject<StreamEvent>;
  let http: HttpTestingController;
  let harness: RouterTestingHarness;

  beforeEach(() => {
    stream = new Subject<StreamEvent>();
    const tenant = signal<string | null>('acme');
    TestBed.configureTestingModule({
      providers: [
        provideRouter(routes),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        MessageService,
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
        { provide: SessionService, useValue: sessionOf(tenant, signal(false)) },
        {
          provide: ProjectsService,
          useValue: { list: signal([project('COW', 'Cowork')]), byKey: () => undefined },
        },
        { provide: MembersService, useValue: { list: signal([]) } },
        {
          provide: SavedFiltersService,
          useValue: { list: signal([]), reload: vi.fn() },
        },
        { provide: Clock, useValue: { now: signal(now) } },
      ],
    });
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  const page = () => harness.routeNativeElement as HTMLElement;
  const listOf = (items: Ticket[], total: number): TicketList => ({
    items,
    next_cursor: null,
    total,
    page: 1,
    per_page: 50,
  });
  /** The one request the list has open, its query as a record of strings. */
  const asked = () => {
    const request = http.expectOne((each) => each.url === tenantUrl);
    const query = Object.fromEntries(
      request.request.params.keys().map((name) => [name, request.request.params.getAll(name)]),
    );
    return { request, query };
  };
  const event = (key: string): StreamEvent => ({
    name: 'ticket.changed',
    id: `e-${key}`,
    key,
    version: 1,
    kind: 'created',
  });

  it('loads its page again on a change in its tenant, and shows what changed', async () => {
    harness = await RouterTestingHarness.create();
    await harness.navigateByUrl('/t/acme/tickets?severity=high', TenantTickets);
    await settle(harness);
    const first = asked();
    expect(first.query).toEqual({ severity: ['high'], page: ['1'], per_page: ['50'] });
    first.request.flush(listOf([ticket('acme/COW-1', { severity: 'high' })], 1), {
      headers: { ETag: 'W/"one"' },
    });
    await settle(harness);
    expect(page().querySelector('[data-testid="row-acme/COW-1"]')).not.toBeNull();

    stream.next(event('acme/COW-2'));
    await new Promise((resolve) => setTimeout(resolve, listReloadDelay + 50));
    await settle(harness);

    const again = asked();
    expect(again.query).toEqual({ severity: ['high'], page: ['1'], per_page: ['50'] });
    expect(again.request.request.headers.get('If-None-Match')).toBe('W/"one"');
    again.request.flush(
      listOf(
        [ticket('acme/COW-2', { severity: 'high' }), ticket('acme/COW-1', { severity: 'high' })],
        2,
      ),
    );
    await settle(harness);
    expect(
      [...page().querySelectorAll('tbody tr.row')].map((row) => row.getAttribute('data-testid')),
    ).toEqual(['row-acme/COW-2', 'row-acme/COW-1']);
  });

  it('loads the page it shows again, and nothing on a change in another tenant', async () => {
    harness = await RouterTestingHarness.create();
    await harness.navigateByUrl('/t/acme/tickets', TenantTickets);
    await settle(harness);
    asked().request.flush(listOf([ticket('acme/COW-1')], 120));
    await settle(harness);
    harness.fixture.debugElement
      .query(By.directive(Paginator))
      .triggerEventHandler('onPageChange', { page: 1, rows: 50, first: 50 });
    await settle(harness);
    asked().request.flush({ ...listOf([ticket('acme/COW-60')], 120), page: 2 });
    await settle(harness);

    // The person-level stream carries every tenant of the person (docs/adr/0054 D1).
    stream.next(event('globex/OPS-1'));
    await new Promise((resolve) => setTimeout(resolve, listReloadDelay + 50));
    await settle(harness);
    http.expectNone((each) => each.url === tenantUrl);

    stream.next(event('acme/COW-61'));
    await new Promise((resolve) => setTimeout(resolve, listReloadDelay + 50));
    await settle(harness);
    const again = asked();
    expect(again.query).toEqual({ page: ['2'], per_page: ['50'] });
    again.request.flush({ ...listOf([ticket('acme/COW-61')], 121), page: 2 });
    await settle(harness);
  });
});
