import {
  HttpErrorResponse,
  HttpInterceptorFn,
  provideHttpClient,
  withInterceptors,
} from '@angular/common/http';
import {
  HttpTestingController,
  provideHttpClientTesting,
  TestRequest,
} from '@angular/common/http/testing';
import {
  Component,
  createEnvironmentInjector,
  EnvironmentInjector,
  inject,
  ResourceRef,
  signal,
} from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Subject, throwError } from 'rxjs';
import { provideApiConfiguration } from '../api/api-configuration';
import { Ticket, TicketList } from '../api/models';
import {
  EVENT_SOURCE,
  EventSourceLike,
  EventStreamService,
  fallback,
  StreamEvent,
  TicketEvent,
} from './event-stream.service';
import { SessionService } from './session.service';
import { listReloadDelay, pageSize, splitKey, TicketPage, TicketsService } from './tickets.service';

function ticket(key: string, version = 1, overrides: Partial<Ticket> = {}): Ticket {
  const [, short] = key.split('/');
  const [project, number] = short.split('-');
  return {
    id: `0199aaaa-0000-7000-8000-${number.padStart(12, '0')}`,
    key,
    number: Number(number),
    project,
    title: `Ticket ${key}`,
    body: '',
    type: 'task',
    state: 'filed',
    severity: 'medium',
    security: 'none',
    effort: 'M',
    urgency: 'later',
    urgency_derived: 'later',
    urgency_override: null,
    urgency_rule: 'v1:default',
    assignee: null,
    reporter: { id: 'p1', display_name: 'Hans' },
    reporter_agent: null,
    reporter_token: null,
    block: null,
    confidential: false,
    parent: null,
    progress: 0,
    progress_derived: false,
    progress_refinement: 0,
    progress_review: 0,
    threat: null,
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-01T10:00:00Z',
    opened_at: '2026-10-01T10:00:00Z',
    decided_at: null,
    done_at: null,
    done_from: null,
    done_by_hand: false,
    open_prerequisites: 0,
    version,
    ...overrides,
  };
}

const listOf = (items: Ticket[], next: string | null = null, total?: number): TicketList => ({
  items,
  next_cursor: next,
  ...(total === undefined ? {} : { total }),
});

const changed = (name: TicketEvent['name'], key: string, version: number): TicketEvent => ({
  name,
  id: `${name}-${key}-${version}`,
  key,
  version,
  kind: 'edited',
});

const problem = (status: number) => ({
  type: 'about:blank',
  title: 'Problem',
  status,
  code: status === 404 ? 'not_found' : 'internal',
});

const projectUrl = '/api/v1/tenants/acme/projects/VKO/tickets';
const otherProjectUrl = '/api/v1/tenants/acme/projects/COW/tickets';
const tenantUrl = '/api/v1/tenants/acme/tickets';
const ticketUrl = (key: string) => `/api/v1/tickets/${key}`;

/** Answers a request the way a backend that is down or refusing does. */
function fail(request: TestRequest, status: number): void {
  if (status === 0) {
    request.error(new ProgressEvent('error'));
  } else {
    request.flush(problem(status), { status, statusText: `Status ${status}` });
  }
}

/** What the interceptor below throws instead of passing a request on, when a test sets it. */
let interceptorFailure: Error | null = null;
/** The HTTP client turns every failure of the transport into an HttpErrorResponse; an interceptor can still throw something else. */
const failOnDemand: HttpInterceptorFn = (request, next) =>
  interceptorFailure ? throwError(() => interceptorFailure) : next(request);

/** A view that creates a list in its own injection context, as the pages do. */
@Component({ template: '' })
class ListHost {
  readonly list = inject(TicketsService).projectTickets(() => ({
    tenant: 'acme',
    project: 'COW',
  }));
}

describe('splitKey', () => {
  it('splits a canonical key into the tenant and the short key of the ticket routes', () => {
    expect(splitKey('acme/VKO-12')).toEqual({ tenant: 'acme', key: 'VKO-12' });
  });

  it('splits at the first slash only', () => {
    expect(splitKey('acme/VKO-12/extra')).toEqual({ tenant: 'acme', key: 'VKO-12/extra' });
  });
});

describe('TicketsService', () => {
  let service: TicketsService;
  let session: SessionService;
  let http: HttpTestingController;
  let stream: Subject<StreamEvent>;

  /**
   * Runs the effects, which start the loads that are due, lets the promise chains of answered
   * requests finish, and runs the effects they feed. Fake timers are on, so this does not wait for
   * Angular's own scheduling: it goes through the macrotask that fake timers yield.
   */
  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  /** Moves the clock and settles, which is how a debounce timer fires. */
  const wait = async (ms: number) => {
    await vi.advanceTimersByTimeAsync(ms);
    await settle();
  };
  /** A child injector that a view would own; the test destroys it where a view would go. */
  const view = () => createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));
  const projectList = (injector?: EnvironmentInjector) =>
    TestBed.runInInjectionContext(() =>
      service.projectTickets(() => ({ tenant: 'acme', project: 'VKO' }), injector),
    );
  const tenantList = (injector?: EnvironmentInjector) =>
    TestBed.runInInjectionContext(() =>
      service.tenantTickets(() => ({ tenant: 'acme' }), injector),
    );
  /** Takes every open request to the url, so that a test can count them and answer them. */
  const take = (url: string): TestRequest[] => http.match((request) => request.url === url);
  const answer = async (url: string, body: object = listOf([])) => {
    http.expectOne((request) => request.url === url).flush(body);
    await settle();
  };
  /** Shows the ticket the way a detail view does, and answers its load. */
  const show = async (key: string, version = 1, injector = view()) => {
    const shown = signal<string | undefined>(key);
    const ref = service.ticket(shown, injector);
    await settle();
    await answer(ticketUrl(key), ticket(key, version));
    return { shown, ref, injector };
  };

  beforeEach(async () => {
    vi.useFakeTimers();
    interceptorFailure = null;
    stream = new Subject<StreamEvent>();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptors([failOnDemand])),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
      ],
    });
    service = TestBed.inject(TicketsService);
    session = TestBed.inject(SessionService);
    http = TestBed.inject(HttpTestingController);
    await settle();
    await answer('/api/v1/me', { id: 'p1', display_name: 'Hans', memberships: [] });
    session.enter('acme');
    await settle();
  });

  afterEach(() => {
    try {
      // The session asks who is working again on a resync, a poll and a membership event, which
      // its own spec covers; this one is about the tickets.
      http.match('/api/v1/me');
      http.verify();
    } finally {
      vi.useRealTimers();
      // A failing check must not leave the test bed dirty for the tests that follow.
      TestBed.resetTestingModule();
    }
  });

  describe('projectTickets', () => {
    it('asks for the tickets of the project with the parameters it is given', async () => {
      const list = TestBed.runInInjectionContext(() =>
        service.projectTickets(() => ({
          tenant: 'acme',
          project: 'VKO',
          state: ['filed', 'blocked'],
          limit: 50,
        })),
      );
      await settle();

      const request = http.expectOne((r) => r.url === projectUrl);

      expect(request.request.method).toBe('GET');
      expect(request.request.params.getAll('state')).toEqual(['filed', 'blocked']);
      expect(request.request.params.get('limit')).toBe('50');
      request.flush(listOf([]));
      await settle();
      expect(list.status()).toBe('resolved');
    });

    it('keeps the keys in the order of the server and every ticket in the cache', async () => {
      const list = projectList();
      await settle();

      await answer(
        projectUrl,
        listOf([ticket('acme/VKO-3'), ticket('acme/VKO-1', 4), ticket('acme/VKO-2')], 'c1', 3),
      );

      expect(list.value()).toEqual<TicketPage>({
        keys: ['acme/VKO-3', 'acme/VKO-1', 'acme/VKO-2'],
        total: 3,
        nextCursor: 'c1',
      });
      expect(service.cache.ids().sort()).toEqual(['acme/VKO-1', 'acme/VKO-2', 'acme/VKO-3']);
      expect(service.cache.value('acme/VKO-1')?.version).toBe(4);
      expect(service.cache.etag('acme/VKO-1')).toBe('"4"');
    });

    it('has no total when the server names none, and no cursor at the last page', async () => {
      const list = projectList();
      await settle();

      await answer(projectUrl, listOf([ticket('acme/VKO-1')]));

      expect(list.value()?.total).toBeUndefined();
      expect(list.value()?.nextCursor).toBeNull();
    });

    it('does not let an older answer undo a newer ticket that the cache already holds', async () => {
      service.cache.put('acme/VKO-1', ticket('acme/VKO-1', 5, { title: 'Newer' }));
      const list = projectList();
      await settle();

      await answer(projectUrl, listOf([ticket('acme/VKO-1', 4, { title: 'Older' })]));

      expect(list.value()?.keys).toEqual(['acme/VKO-1']);
      expect(service.cache.value('acme/VKO-1')?.title).toBe('Newer');
    });

    it('asks for nothing while the parameters say there is nothing to ask for', async () => {
      const wanted = signal<string | undefined>(undefined);
      const list = TestBed.runInInjectionContext(() =>
        service.projectTickets(() =>
          wanted() ? { tenant: 'acme', project: wanted() as string } : undefined,
        ),
      );
      await settle();

      expect(list.status()).toBe('idle');

      wanted.set('VKO');
      await settle();
      await answer(projectUrl);
      expect(list.status()).toBe('resolved');
    });

    it('asks again when the parameters change', async () => {
      const state = signal<string[]>(['filed']);
      const list = TestBed.runInInjectionContext(() =>
        service.projectTickets(() => ({ tenant: 'acme', project: 'VKO', state: state() })),
      );
      await settle();
      await answer(projectUrl, listOf([ticket('acme/VKO-1')]));

      state.set(['done']);
      await settle();
      const request = http.expectOne((r) => r.url === projectUrl);
      expect(request.request.params.getAll('state')).toEqual(['done']);
      request.flush(listOf([ticket('acme/VKO-2')]));
      await settle();

      expect(list.value()?.keys).toEqual(['acme/VKO-2']);
    });

    it('fails with the HTTP error and leaves the cache alone', async () => {
      const list = projectList();
      await settle();

      http
        .expectOne(projectUrl)
        .flush(problem(500), { status: 500, statusText: 'Internal Server Error' });
      await settle();

      expect(list.status()).toBe('error');
      expect(list.error()).toBeInstanceOf(HttpErrorResponse);
      expect(service.cache.ids()).toEqual([]);
    });
  });

  describe('projectTicketPages', () => {
    const pagesList = (pages = signal(1), extra: object = {}) =>
      TestBed.runInInjectionContext(() =>
        service.projectTicketPages(() => ({
          tenant: 'acme',
          project: 'VKO',
          pages: pages(),
          ...extra,
        })),
      );

    it('asks for the first page at the largest size the server allows, without a numbered page', async () => {
      pagesList();
      await settle();

      const request = http.expectOne((r) => r.url === projectUrl);

      expect(request.request.method).toBe('GET');
      expect(request.request.params.get('limit')).toBe(String(pageSize));
      expect(pageSize).toBe(200);
      expect(request.request.params.has('cursor')).toBe(false);
      expect(request.request.params.has('page')).toBe(false);
      expect(request.request.params.has('per_page')).toBe(false);
      expect(request.request.params.has('pages')).toBe(false);
      request.flush(listOf([]));
      await settle();
    });

    it('passes the filters on', async () => {
      pagesList(signal(1), { state: ['blocked', 'filed'], q: 'flicker' });
      await settle();

      const request = http.expectOne((r) => r.url === projectUrl);

      expect(request.request.params.getAll('state')).toEqual(['blocked', 'filed']);
      expect(request.request.params.get('q')).toBe('flicker');
      request.flush(listOf([]));
      await settle();
    });

    it('keeps the keys in the order of the server and every ticket in the cache', async () => {
      const list = pagesList();
      await settle();

      await answer(
        projectUrl,
        listOf([ticket('acme/VKO-3'), ticket('acme/VKO-1', 4), ticket('acme/VKO-2')]),
      );

      expect(list.value()).toEqual<TicketPage>({
        keys: ['acme/VKO-3', 'acme/VKO-1', 'acme/VKO-2'],
        nextCursor: null,
        versions: new Map([
          ['acme/VKO-3', 1],
          ['acme/VKO-1', 4],
          ['acme/VKO-2', 1],
        ]),
      });
      expect(service.cache.value('acme/VKO-1')?.version).toBe(4);
      expect(service.cache.etag('acme/VKO-1')).toBe('"4"');
    });

    it('stops after the pages it was asked for and says that there is more', async () => {
      const list = pagesList(signal(1));
      await settle();

      await answer(projectUrl, listOf([ticket('acme/VKO-1')], 'c1'));

      expect(list.value()).toEqual<TicketPage>({
        keys: ['acme/VKO-1'],
        nextCursor: 'c1',
        versions: new Map([['acme/VKO-1', 1]]),
      });
      http.expectNone((r) => r.url === projectUrl);
    });

    it('follows the cursor for as many pages as it was asked for', async () => {
      const list = pagesList(signal(2));
      await settle();

      await answer(projectUrl, listOf([ticket('acme/VKO-1'), ticket('acme/VKO-2')], 'c1'));
      const second = http.expectOne((r) => r.url === projectUrl);
      expect(second.request.params.get('cursor')).toBe('c1');
      expect(second.request.params.get('limit')).toBe(String(pageSize));
      second.flush(listOf([ticket('acme/VKO-3')], 'c2'));
      await settle();

      expect(list.value()).toEqual<TicketPage>({
        keys: ['acme/VKO-1', 'acme/VKO-2', 'acme/VKO-3'],
        nextCursor: 'c2',
        versions: new Map([
          ['acme/VKO-1', 1],
          ['acme/VKO-2', 1],
          ['acme/VKO-3', 1],
        ]),
      });
      http.expectNone((r) => r.url === projectUrl);
    });

    it('stops at the end of the list although more pages were asked for', async () => {
      const list = pagesList(signal(3));
      await settle();

      await answer(projectUrl, listOf([ticket('acme/VKO-1')], 'c1'));
      await answer(projectUrl, listOf([ticket('acme/VKO-2')], null));

      expect(list.value()).toEqual<TicketPage>({
        keys: ['acme/VKO-1', 'acme/VKO-2'],
        nextCursor: null,
        versions: new Map([
          ['acme/VKO-1', 1],
          ['acme/VKO-2', 1],
        ]),
      });
      http.expectNone((r) => r.url === projectUrl);
    });

    it('says the version each ticket had in the answer, also where the cache holds a newer one', async () => {
      const list = pagesList();
      await settle();
      service.cache.put('acme/VKO-1', ticket('acme/VKO-1', 7));

      await answer(projectUrl, listOf([ticket('acme/VKO-1', 5)]));

      expect(list.value()?.versions?.get('acme/VKO-1')).toBe(5);
      expect(service.cache.value('acme/VKO-1')?.version).toBe(7);
    });

    it('shows a ticket that both answers had once, where the later answer has it', async () => {
      const list = pagesList(signal(2));
      await settle();

      await answer(
        projectUrl,
        listOf([ticket('acme/VKO-1'), ticket('acme/VKO-2'), ticket('acme/VKO-3')], 'c1'),
      );
      await answer(projectUrl, listOf([ticket('acme/VKO-4'), ticket('acme/VKO-2', 2)]));

      expect(list.value()?.keys).toEqual(['acme/VKO-1', 'acme/VKO-3', 'acme/VKO-4', 'acme/VKO-2']);
      expect(list.value()?.versions?.get('acme/VKO-2')).toBe(2);
      expect(service.cache.value('acme/VKO-2')?.version).toBe(2);
    });

    it('loads again from the first page, for all its pages, when it is reloaded', async () => {
      const list = pagesList(signal(2));
      await settle();
      await answer(projectUrl, listOf([ticket('acme/VKO-1')], 'c1'));
      await answer(projectUrl, listOf([ticket('acme/VKO-2')]));

      list.reload();
      await settle();

      const first = http.expectOne((r) => r.url === projectUrl);
      expect(first.request.params.has('cursor')).toBe(false);
      first.flush(listOf([ticket('acme/VKO-2'), ticket('acme/VKO-1')], 'c1'));
      await settle();
      const second = http.expectOne((r) => r.url === projectUrl);
      expect(second.request.params.get('cursor')).toBe('c1');
      second.flush(listOf([ticket('acme/VKO-3')]));
      await settle();
      expect(list.value()?.keys).toEqual(['acme/VKO-2', 'acme/VKO-1', 'acme/VKO-3']);
    });

    it('loads again when it is asked for more pages', async () => {
      const pages = signal(1);
      const list = pagesList(pages);
      await settle();
      await answer(projectUrl, listOf([ticket('acme/VKO-1')], 'c1'));

      pages.set(2);
      await settle();

      await answer(projectUrl, listOf([ticket('acme/VKO-1')], 'c1'));
      await answer(projectUrl, listOf([ticket('acme/VKO-2')]));
      expect(list.value()?.keys).toEqual(['acme/VKO-1', 'acme/VKO-2']);
    });

    it('asks for nothing while the parameters say there is nothing to ask for', async () => {
      const wanted = signal(false);
      const list = TestBed.runInInjectionContext(() =>
        service.projectTicketPages(() =>
          wanted() ? { tenant: 'acme', project: 'VKO', pages: 1 } : undefined,
        ),
      );
      await settle();

      expect(list.status()).toBe('idle');
      http.expectNone((r) => r.url === projectUrl);

      wanted.set(true);
      await settle();
      await answer(projectUrl);
      expect(list.status()).toBe('resolved');
    });

    it('is reloaded a moment after an event, like every list on screen', async () => {
      pagesList();
      await settle();
      await answer(projectUrl, listOf([ticket('acme/VKO-1')]));

      stream.next(changed('ticket.changed', 'acme/VKO-1', 1));
      await wait(listReloadDelay);

      expect(take(projectUrl)).toHaveLength(1);
    });

    it('fails with the HTTP error of any page, and leaves the keys of the others out', async () => {
      const list = pagesList(signal(2));
      await settle();
      await answer(projectUrl, listOf([ticket('acme/VKO-1')], 'c1'));

      http
        .expectOne((r) => r.url === projectUrl)
        .flush(problem(500), { status: 500, statusText: 'Internal Server Error' });
      await settle();

      expect(list.status()).toBe('error');
      expect(list.error()).toBeInstanceOf(HttpErrorResponse);
    });
  });

  describe('tenantTickets', () => {
    it('asks for the tickets across the projects of the tenant', async () => {
      const list = TestBed.runInInjectionContext(() =>
        service.tenantTickets(() => ({ tenant: 'acme', project: ['VKO', '!COW'] })),
      );
      await settle();

      const request = http.expectOne((r) => r.url === tenantUrl);

      expect(request.request.params.getAll('project')).toEqual(['VKO', '!COW']);
      request.flush(listOf([]));
      await settle();
      expect(list.status()).toBe('resolved');
    });

    it('keeps the keys in the order of the server and every ticket in the cache', async () => {
      const list = tenantList();
      await settle();

      await answer(tenantUrl, listOf([ticket('acme/COW-2'), ticket('acme/VKO-9', 3)], null, 2));

      expect(list.value()).toEqual<TicketPage>({
        keys: ['acme/COW-2', 'acme/VKO-9'],
        total: 2,
        nextCursor: null,
      });
      expect(service.cache.etag('acme/VKO-9')).toBe('"3"');
    });

    it('asks for nothing while the parameters say there is nothing to ask for', async () => {
      const list = TestBed.runInInjectionContext(() => service.tenantTickets(() => undefined));
      await settle();

      expect(list.status()).toBe('idle');
    });

    it('fails with the HTTP error', async () => {
      const list = tenantList();
      await settle();

      http.expectOne(tenantUrl).flush(problem(403), { status: 403, statusText: 'Forbidden' });
      await settle();

      expect(list.status()).toBe('error');
    });
  });

  describe('a list that loads again and fails (docs/adr/0054 D7)', () => {
    const lists = [
      ['projectTickets', projectUrl, () => projectList()],
      [
        'projectTicketPages',
        projectUrl,
        () =>
          TestBed.runInInjectionContext(() =>
            service.projectTicketPages(() => ({ tenant: 'acme', project: 'VKO', pages: 1 })),
          ),
      ],
      ['tenantTickets', tenantUrl, () => tenantList()],
    ] as const;

    describe.each(lists)('made by %s', (_, url, open) => {
      /** The list, loaded once, and asked to load again by the fallback's poll. */
      async function reloading() {
        const list = open();
        await settle();
        await answer(url, listOf([ticket('acme/VKO-1')]));
        stream.next({ name: 'poll' });
        await wait(listReloadDelay);
        return list;
      }

      it.each([0, 500, 503])(
        'keeps the keys it shows when the load fails with a status of %i',
        async (status) => {
          const list = await reloading();

          fail(
            http.expectOne((request) => request.url === url),
            status,
          );
          await settle();

          expect(list.status()).toBe('resolved');
          expect(list.value()?.keys).toEqual(['acme/VKO-1']);
        },
      );

      it.each([401, 403, 404])(
        'shows nothing when the load answers %i: the list is gone for the person',
        async (status) => {
          const list = await reloading();

          fail(
            http.expectOne((request) => request.url === url),
            status,
          );
          await settle();

          expect(list.status()).toBe('error');
          expect(list.hasValue()).toBe(false);
        },
      );
    });
  });

  describe('a poll that finds a list unchanged (docs/adr/0054 D7)', () => {
    const notModified = { status: 304, statusText: 'Not Modified' };

    describe.each([
      ['projectTickets', projectUrl, () => projectList()],
      [
        'projectTicketPages',
        projectUrl,
        () =>
          TestBed.runInInjectionContext(() =>
            service.projectTicketPages(() => ({ tenant: 'acme', project: 'VKO', pages: 1 })),
          ),
      ],
      ['tenantTickets', tenantUrl, () => tenantList()],
    ] as const)('made by %s', (_, url, open) => {
      it("sends the list's weak ETag and keeps the keys on a 304", async () => {
        const list = open();
        await settle();
        http
          .expectOne((request) => request.url === url)
          .flush(listOf([ticket('acme/VKO-2'), ticket('acme/VKO-1')]), {
            headers: { ETag: 'W/"one"' },
          });
        await settle();

        stream.next({ name: 'poll' });
        await wait(listReloadDelay);
        const again = http.expectOne((request) => request.url === url);
        expect(again.request.headers.get('If-None-Match')).toBe('W/"one"');
        again.flush(null, notModified);
        await settle();

        expect(list.status()).toBe('resolved');
        expect(list.value()?.keys).toEqual(['acme/VKO-2', 'acme/VKO-1']);
        expect(service.cache.value('acme/VKO-1')).toBeDefined();
      });
    });

    it('sends the tag of each page a list follows, cursor by cursor', async () => {
      const list = TestBed.runInInjectionContext(() =>
        service.projectTicketPages(() => ({ tenant: 'acme', project: 'VKO', pages: 2 })),
      );
      await settle();
      http
        .expectOne((r) => r.url === projectUrl && !r.params.has('cursor'))
        .flush(listOf([ticket('acme/VKO-1')], 'c1'), { headers: { ETag: 'W/"one"' } });
      await settle();
      http
        .expectOne((r) => r.url === projectUrl && r.params.get('cursor') === 'c1')
        .flush(listOf([ticket('acme/VKO-2')]), { headers: { ETag: 'W/"two"' } });
      await settle();

      stream.next({ name: 'poll' });
      await wait(listReloadDelay);
      const first = http.expectOne((r) => r.url === projectUrl && !r.params.has('cursor'));
      expect(first.request.headers.get('If-None-Match')).toBe('W/"one"');
      first.flush(null, notModified);
      await settle();
      const second = http.expectOne((r) => r.url === projectUrl && r.params.get('cursor') === 'c1');
      expect(second.request.headers.get('If-None-Match')).toBe('W/"two"');
      second.flush(null, notModified);
      await settle();

      expect(list.value()?.keys).toEqual(['acme/VKO-1', 'acme/VKO-2']);
      expect(list.value()?.nextCursor).toBeNull();
    });
  });

  describe('refresh', () => {
    it('fetches the ticket by its canonical key into the cache and hands it back', async () => {
      const done = service.refresh('acme/VKO-12');

      const request = http.expectOne(ticketUrl('acme/VKO-12'));
      expect(request.request.method).toBe('GET');
      request.flush(ticket('acme/VKO-12', 7));

      expect((await done).version).toBe(7);
      expect(service.cache.value('acme/VKO-12')?.version).toBe(7);
      expect(service.cache.etag('acme/VKO-12')).toBe('"7"');
    });

    it('rejects with the HTTP error and leaves the cache alone', async () => {
      service.cache.put('acme/VKO-12', ticket('acme/VKO-12', 2));
      const done = service.refresh('acme/VKO-12');
      const outcome = done.then(
        () => null,
        (error: unknown) => error,
      );

      http
        .expectOne(ticketUrl('acme/VKO-12'))
        .flush(problem(404), { status: 404, statusText: 'Not Found' });

      expect(await outcome).toBeInstanceOf(HttpErrorResponse);
      expect(service.cache.value('acme/VKO-12')?.version).toBe(2);
    });
  });

  describe('openTickets', () => {
    const url = '/api/v1/tenants/acme/projects/VKO/tickets';

    it('reads every page of the open tickets of a project, in the rank, into the cache', async () => {
      const done = service.openTickets('acme', 'VKO');

      const first = http.expectOne((request) => request.url === url);
      expect(first.request.params.get('limit')).toBe('200');
      expect(first.request.params.has('cursor')).toBe(false);
      expect(first.request.params.has('include_terminal')).toBe(false);
      first.flush({ ...listOf([ticket('acme/VKO-3', 2)]), next_cursor: 'more' });
      await settle();
      const second = http.expectOne((request) => request.url === url);
      expect(second.request.params.get('cursor')).toBe('more');
      second.flush(listOf([ticket('acme/VKO-1', 1)]));

      expect((await done).map((each) => each.key)).toEqual(['acme/VKO-3', 'acme/VKO-1']);
      expect(service.cache.value('acme/VKO-1')?.version).toBe(1);
    });

    it('is no open list: an event reloads nothing', async () => {
      const done = service.openTickets('acme', 'VKO');
      http.expectOne((request) => request.url === url).flush(listOf([]));
      await done;

      stream.next({ name: 'poll' });
      await wait(200);

      http.expectNone((request) => request.url === url);
    });
  });

  describe('ticket', () => {
    it('loads the ticket into the cache and hands out its key', async () => {
      const { ref } = await show('acme/VKO-12', 4);

      expect(ref.status()).toBe('resolved');
      expect(ref.value()).toBe('acme/VKO-12');
      expect(service.cache.value('acme/VKO-12')?.version).toBe(4);
      expect(service.cache.etag('acme/VKO-12')).toBe('"4"');
    });

    it('asks for nothing while there is no key', async () => {
      const ref = service.ticket(() => undefined, view());
      await settle();

      expect(ref.status()).toBe('idle');
    });

    it('loads the next ticket when the key changes', async () => {
      const { shown, ref } = await show('acme/VKO-1');

      shown.set('acme/VKO-2');
      await settle();
      await answer(ticketUrl('acme/VKO-2'), ticket('acme/VKO-2', 3));

      expect(ref.value()).toBe('acme/VKO-2');
      expect(service.cache.ids().sort()).toEqual(['acme/VKO-1', 'acme/VKO-2']);
    });

    it('fails with the HTTP error and leaves the cache alone when the ticket cannot be read', async () => {
      const ref: ResourceRef<string | undefined> = service.ticket(() => 'acme/VKO-404', view());
      await settle();

      http
        .expectOne(ticketUrl('acme/VKO-404'))
        .flush(problem(404), { status: 404, statusText: 'Not Found' });
      await settle();

      expect(ref.status()).toBe('error');
      expect((ref.error() as HttpErrorResponse).status).toBe(404);
      expect(service.cache.ids()).toEqual([]);
    });
  });

  describe('an event about a ticket', () => {
    const key = 'acme/VKO-12';

    beforeEach(() => service.cache.put(key, ticket(key, 5)));

    it('refetches a cached ticket when the version it names is newer', async () => {
      stream.next(changed('ticket.changed', key, 6));

      const request = http.expectOne(ticketUrl(key));
      expect(request.request.method).toBe('GET');
      request.flush(ticket(key, 6, { title: 'Renamed' }));
      await settle();

      expect(service.cache.value(key)?.title).toBe('Renamed');
      expect(service.cache.etag(key)).toBe('"6"');
    });

    it.each([
      ['an equal', 5],
      ['an older', 4],
    ])(
      'does not refetch for %s version, which the cache already holds',
      async (_which, version) => {
        stream.next(changed('ticket.changed', key, version));

        http.expectNone(ticketUrl(key));
      },
    );

    it('does not fetch a ticket that no view has cached', () => {
      stream.next(changed('ticket.changed', 'acme/VKO-99', 3));

      http.expectNone(ticketUrl('acme/VKO-99'));
    });

    it('refetches a cached ticket on link.changed even at the version it holds, because its open prerequisites change', async () => {
      stream.next(changed('link.changed', key, 5));

      http.expectOne(ticketUrl(key)).flush(ticket(key, 5, { open_prerequisites: 1 }));
      await settle();

      expect(service.cache.value(key)?.open_prerequisites).toBe(1);
    });

    it('does not fetch a ticket that is not cached on link.changed', () => {
      stream.next(changed('link.changed', 'acme/VKO-99', 1));

      http.expectNone(ticketUrl('acme/VKO-99'));
    });

    it.each(['question.changed', 'comment.changed', 'interest.changed'] as const)(
      'never refetches the ticket on %s, whatever version it names, because they change nothing it shows',
      async (name) => {
        stream.next(changed(name, key, 99));
        await wait(10 * listReloadDelay);

        http.expectNone(ticketUrl(key));
        expect(service.cache.value(key)?.version).toBe(5);
      },
    );

    it.each([
      [404, 'the ticket is not there'],
      [403, 'the person may not see it any more'],
    ])(
      'removes the entry when the refetch answers %i, because %s, so that no view shows what is not there',
      async (status) => {
        service.cache.put('acme/VKO-1', ticket('acme/VKO-1'));
        const entry = service.cache.entry(key);

        stream.next(changed('ticket.changed', key, 6));
        fail(http.expectOne(ticketUrl(key)), status);
        await settle();

        expect(entry()).toBeUndefined();
        expect(service.cache.value(key)).toBeUndefined();
        expect(service.cache.ids()).toEqual(['acme/VKO-1']);
      },
    );

    it.each([0, 400, 401, 409, 412, 429, 500, 502, 503, 504])(
      'keeps the last known ticket when the refetch fails with status %i, an outage being what the polling fallback is for',
      async (status) => {
        const entry = service.cache.entry(key);

        stream.next(changed('ticket.changed', key, 6));
        fail(http.expectOne(ticketUrl(key)), status);
        await settle();

        expect(entry()?.value.version).toBe(5);
        expect(service.cache.value(key)?.title).toBe(`Ticket ${key}`);
        expect(service.cache.etag(key)).toBe('"5"');
      },
    );

    it('keeps the ticket as well when the refetch after a link.changed event fails with a 500', async () => {
      stream.next(changed('link.changed', key, 5));

      fail(http.expectOne(ticketUrl(key)), 500);
      await settle();

      expect(service.cache.value(key)?.version).toBe(5);
    });

    it('keeps the entry when the refetch fails with something that is not an HTTP error', async () => {
      interceptorFailure = new Error('an interceptor broke');

      stream.next(changed('ticket.changed', key, 6));
      await settle();

      http.expectNone(ticketUrl(key));
      expect(service.cache.value(key)?.version).toBe(5);
    });

    it('refetches again at the next event after a failure that left the entry', async () => {
      stream.next(changed('ticket.changed', key, 6));
      fail(http.expectOne(ticketUrl(key)), 503);
      await settle();

      stream.next(changed('ticket.changed', key, 6));
      http.expectOne(ticketUrl(key)).flush(ticket(key, 6, { title: 'Back again' }));
      await settle();

      expect(service.cache.value(key)?.title).toBe('Back again');
    });

    it('lets the entry come back when a ticket that was gone is fetched again', async () => {
      stream.next(changed('ticket.changed', key, 6));
      fail(http.expectOne(ticketUrl(key)), 404);
      await settle();
      expect(service.cache.value(key)).toBeUndefined();

      const again = service.refresh(key);
      http.expectOne(ticketUrl(key)).flush(ticket(key, 2));
      await again;

      expect(service.cache.value(key)?.version).toBe(2);
    });
  });

  describe('the lists open on screen', () => {
    let projects: ResourceRef<TicketPage | undefined>;
    let tenant: ResourceRef<TicketPage | undefined>;

    beforeEach(async () => {
      projects = projectList();
      tenant = tenantList();
      await settle();
      await answer(projectUrl);
      await answer(tenantUrl);
    });

    it.each(['ticket.changed', 'question.changed', 'link.changed'] as const)(
      'reload once, a moment after a %s event',
      async (name) => {
        stream.next(changed(name, 'acme/VKO-1', 1));

        await wait(listReloadDelay - 1);
        http.expectNone(projectUrl);
        http.expectNone(tenantUrl);

        await wait(1);
        expect(take(projectUrl)).toHaveLength(1);
        expect(take(tenantUrl)).toHaveLength(1);
      },
    );

    it("are not reloaded by an event of another of the person's tenants, which the person-level stream carries (docs/adr/0054 D1)", async () => {
      stream.next(changed('question.changed', 'globex/OPS-1', 1));
      await wait(10 * listReloadDelay);

      http.expectNone(projectUrl);
      http.expectNone(tenantUrl);
    });

    it('reload once for a whole burst, counted from its first event', async () => {
      stream.next(changed('ticket.changed', 'acme/VKO-1', 2));
      await wait(100);
      stream.next(changed('ticket.changed', 'acme/VKO-2', 2));
      stream.next(changed('link.changed', 'acme/VKO-3', 1));
      stream.next(changed('question.changed', 'acme/VKO-4', 1));
      await wait(listReloadDelay - 100 - 1);
      http.expectNone(projectUrl);

      await wait(1);

      const reloads = take(projectUrl);
      const tenantReloads = take(tenantUrl);
      expect(reloads).toHaveLength(1);
      expect(tenantReloads).toHaveLength(1);
      reloads.forEach((request) => request.flush(listOf([])));
      tenantReloads.forEach((request) => request.flush(listOf([])));
      await settle();

      // The later events of the burst did not set a reload of their own going.
      await wait(10 * listReloadDelay);
      http.expectNone(projectUrl);
      http.expectNone(tenantUrl);
    });

    it('reload again for the next burst', async () => {
      stream.next(changed('ticket.changed', 'acme/VKO-1', 2));
      await wait(listReloadDelay);
      take(projectUrl).forEach((request) => request.flush(listOf([])));
      take(tenantUrl).forEach((request) => request.flush(listOf([])));
      await settle();

      stream.next(changed('ticket.changed', 'acme/VKO-1', 3));
      await wait(listReloadDelay);

      expect(take(projectUrl)).toHaveLength(1);
      expect(take(tenantUrl)).toHaveLength(1);
    });

    it.each(['comment.changed', 'interest.changed'] as const)(
      'do not reload on %s, which no list shows',
      async (name) => {
        stream.next(changed(name, 'acme/VKO-1', 2));

        await wait(10 * listReloadDelay);

        http.expectNone(projectUrl);
        http.expectNone(tenantUrl);
      },
    );

    describe('when the event comes during their own load', () => {
      /** A list that is loading, in a view of its own. */
      const loading = async () => {
        const owner = view();
        const list = TestBed.runInInjectionContext(() =>
          service.projectTickets(() => ({ tenant: 'acme', project: 'COW' }), owner),
        );
        await settle();
        return { owner, list, load: http.expectOne((request) => request.url === otherProjectUrl) };
      };
      /**
       * Answers what the two lists of the surrounding test ask for, until they ask no more: each of
       * them had loaded when the event came, so each reloads at once and, if another event came
       * during that reload, once more.
       */
      const answerTheOthers = async () => {
        for (let round = 0; round < 5; round++) {
          const open = [...take(projectUrl), ...take(tenantUrl)];
          if (open.length === 0) {
            return;
          }
          open.forEach((request) => request.flush(listOf([])));
          await settle();
        }
        throw new Error('the lists keep asking');
      };

      it('reload once more when the load ends, because the answer on its way may predate the change', async () => {
        projects.reload();
        await settle();
        const inFlight = http.expectOne((request) => request.url === projectUrl);

        stream.next(changed('ticket.changed', 'acme/VKO-1', 2));
        await wait(listReloadDelay);
        // The list that was loading cannot reload meanwhile; the other one is not loading.
        expect(take(tenantUrl)).toHaveLength(1);
        http.expectNone((request) => request.url === projectUrl);
        inFlight.flush(listOf([]));
        await settle();

        http.expectOne((request) => request.url === projectUrl).flush(listOf([]));
        await settle();
        expect(projects.status()).toBe('resolved');
      });

      it('reload once more after their first load as well', async () => {
        const { list, load } = await loading();
        expect(list.status()).toBe('loading');

        stream.next(changed('ticket.changed', 'acme/VKO-1', 2));
        await wait(listReloadDelay);
        await answerTheOthers();
        http.expectNone((request) => request.url === otherProjectUrl);
        load.flush(listOf([ticket('acme/COW-1')]));
        await settle();

        expect(list.status()).toBe('reloading');
        http
          .expectOne((request) => request.url === otherProjectUrl)
          .flush(listOf([ticket('acme/COW-1', 2), ticket('acme/COW-2')]));
        await settle();
        expect(list.value()?.keys).toEqual(['acme/COW-1', 'acme/COW-2']);
        expect(list.status()).toBe('resolved');
      });

      it('reload once more only once, however many bursts come during one load', async () => {
        const { load } = await loading();

        stream.next(changed('ticket.changed', 'acme/VKO-1', 2));
        await wait(listReloadDelay);
        stream.next(changed('link.changed', 'acme/VKO-2', 1));
        await wait(listReloadDelay);
        stream.next({ name: 'resync' });
        await wait(listReloadDelay);
        await answerTheOthers();
        load.flush(listOf([]));
        await settle();

        http.expectOne((request) => request.url === otherProjectUrl).flush(listOf([]));
        await settle();
        await wait(10 * listReloadDelay);
        http.expectNone((request) => request.url === otherProjectUrl);
      });

      it.each(['resync', 'poll'] as const)(
        'reload once more after a %s as well, which reloads everything shown',
        async (name) => {
          const { load } = await loading();

          stream.next({ name });
          await wait(listReloadDelay);
          await answerTheOthers();
          load.flush(listOf([]));
          await settle();

          http.expectOne((request) => request.url === otherProjectUrl).flush(listOf([]));
        },
      );

      it('do not reload once more when no event came', async () => {
        const { load } = await loading();

        load.flush(listOf([]));
        await settle();
        await wait(10 * listReloadDelay);

        http.expectNone((request) => request.url === otherProjectUrl);
      });

      it('do not reload once more a comment or a stake of a ticket, which no list shows', async () => {
        const { load } = await loading();

        stream.next(changed('comment.changed', 'acme/VKO-1', 2));
        stream.next(changed('interest.changed', 'acme/VKO-1', 2));
        await wait(10 * listReloadDelay);
        load.flush(listOf([]));
        await settle();

        http.expectNone((request) => request.url === otherProjectUrl);
      });

      it('are not reloaded once more when their view is gone by the time the load ends', async () => {
        const { owner, list, load } = await loading();

        stream.next(changed('ticket.changed', 'acme/VKO-1', 2));
        await wait(listReloadDelay);
        await answerTheOthers();
        owner.destroy();
        await settle();
        load.flush(listOf([]));
        await settle();
        await wait(10 * listReloadDelay);

        http.expectNone((request) => request.url === otherProjectUrl);
        expect(list.status()).toBe('idle');
      });
    });

    it('reload a list that failed, so that it recovers when something changes', async () => {
      const owner = view();
      const failing = TestBed.runInInjectionContext(() =>
        service.projectTickets(() => ({ tenant: 'acme', project: 'COW' }), owner),
      );
      await settle();
      fail(
        http.expectOne((request) => request.url === otherProjectUrl),
        500,
      );
      await settle();
      expect(failing.status()).toBe('error');

      stream.next(changed('ticket.changed', 'acme/VKO-1', 2));
      await wait(listReloadDelay);
      await Promise.all([
        ...take(projectUrl).map((request) => request.flush(listOf([]))),
        ...take(tenantUrl).map((request) => request.flush(listOf([]))),
      ]);
      http
        .expectOne((request) => request.url === otherProjectUrl)
        .flush(listOf([ticket('acme/COW-1')]));
      await settle();

      expect(failing.status()).toBe('resolved');
      expect(failing.value()?.keys).toEqual(['acme/COW-1']);
    });

    it('show the refetched ticket because they read it through the cache', async () => {
      service.cache.put('acme/VKO-1', ticket('acme/VKO-1', 1, { state: 'filed' }));
      const seen = service.cache.entry('acme/VKO-1');

      stream.next(changed('ticket.changed', 'acme/VKO-1', 2));
      http.expectOne(ticketUrl('acme/VKO-1')).flush(ticket('acme/VKO-1', 2, { state: 'analysed' }));
      await settle();

      expect(seen()?.value.state).toBe('analysed');
      expect(projects.status()).toBe('resolved');
      expect(tenant.status()).toBe('resolved');
    });

    it('are no longer reloaded once the view that created them is gone', async () => {
      const owner = view();
      const gone = projectList(owner);
      await settle();
      await answer(projectUrl);
      const reload = vi.spyOn(gone, 'reload');

      owner.destroy();
      stream.next({ name: 'resync' });
      await wait(listReloadDelay);

      expect(reload).not.toHaveBeenCalled();
      // Only the two lists of the surrounding test remain.
      expect(take(projectUrl)).toHaveLength(1);
      expect(take(tenantUrl)).toHaveLength(1);
    });
  });

  describe('a list that a view creates in its own injection context', () => {
    /** The view, with its list loading. */
    const shown = async () => {
      const fixture = TestBed.createComponent(ListHost);
      await settle();
      const load = http.expectOne((request) => request.url === otherProjectUrl);
      return { fixture, list: fixture.componentInstance.list, load };
    };

    it('loads the tickets and keeps them in the cache like any other list', async () => {
      const { list, load } = await shown();

      load.flush(listOf([ticket('acme/COW-1'), ticket('acme/COW-2')], null, 2));
      await settle();

      expect(list.value()).toEqual<TicketPage>({
        keys: ['acme/COW-1', 'acme/COW-2'],
        total: 2,
        nextCursor: null,
      });
      expect(service.cache.ids().sort()).toEqual(['acme/COW-1', 'acme/COW-2']);
    });

    it('is reloaded once, a moment after an event that names a change', async () => {
      const { list, load } = await shown();
      load.flush(listOf([ticket('acme/COW-1')]));
      await settle();

      // A ticket that no view has cached, so that no refetch of it answers the event as well.
      stream.next(changed('ticket.changed', 'acme/COW-9', 2));
      await wait(listReloadDelay - 1);
      http.expectNone((request) => request.url === otherProjectUrl);
      await wait(1);
      http
        .expectOne((request) => request.url === otherProjectUrl)
        .flush(listOf([ticket('acme/COW-1', 2), ticket('acme/COW-2')]));
      await settle();

      expect(list.value()?.keys).toEqual(['acme/COW-1', 'acme/COW-2']);
    });

    it('is reloaded once more when the event came during its first load', async () => {
      const { list, load } = await shown();

      stream.next(changed('ticket.changed', 'acme/COW-1', 2));
      await wait(listReloadDelay);
      http.expectNone((request) => request.url === otherProjectUrl);
      load.flush(listOf([ticket('acme/COW-1')]));
      await settle();

      expect(list.status()).toBe('reloading');
      http
        .expectOne((request) => request.url === otherProjectUrl)
        .flush(listOf([ticket('acme/COW-1', 2)]));
      await settle();
      expect(list.status()).toBe('resolved');
    });

    it('is no longer reloaded once the view is destroyed', async () => {
      const { fixture, list, load } = await shown();
      load.flush(listOf([]));
      await settle();
      const reload = vi.spyOn(list, 'reload');

      fixture.destroy();
      stream.next({ name: 'resync' });
      await wait(listReloadDelay);

      expect(reload).not.toHaveBeenCalled();
      http.expectNone((request) => request.url === otherProjectUrl);
    });

    it('is not reloaded once more when the view is destroyed while the load it waits for runs', async () => {
      const { fixture, list, load } = await shown();

      stream.next(changed('ticket.changed', 'acme/COW-1', 2));
      await wait(listReloadDelay);
      fixture.destroy();
      await settle();
      load.flush(listOf([]));
      await settle();
      await wait(10 * listReloadDelay);

      http.expectNone((request) => request.url === otherProjectUrl);
      expect(list.status()).toBe('idle');
    });
  });

  describe('a list that shows nothing', () => {
    it('is not asked for anything when the lists are reloaded', async () => {
      TestBed.runInInjectionContext(() => service.tenantTickets(() => undefined));
      await settle();

      stream.next(changed('ticket.changed', 'acme/VKO-1', 2));
      await wait(listReloadDelay);

      http.expectNone(tenantUrl);
    });
  });

  describe('resync and poll', () => {
    it.each(['resync', 'poll'] as const)(
      'refetch the tickets that a view shows on %s, and only those',
      async (name) => {
        await show('acme/VKO-1', 2);
        await show('acme/VKO-2', 3);
        service.cache.put('acme/VKO-3', ticket('acme/VKO-3', 1));

        stream.next({ name });

        http.expectOne(ticketUrl('acme/VKO-1')).flush(ticket('acme/VKO-1', 4));
        http.expectOne(ticketUrl('acme/VKO-2')).flush(ticket('acme/VKO-2', 5));
        http.expectNone(ticketUrl('acme/VKO-3'));
        await settle();
        expect(service.cache.value('acme/VKO-1')?.version).toBe(4);
        expect(service.cache.value('acme/VKO-2')?.version).toBe(5);
        expect(service.cache.value('acme/VKO-3')?.version).toBe(1);
      },
    );

    it.each(['resync', 'poll'] as const)('reload every open list once on %s', async (name) => {
      projectList();
      tenantList();
      await settle();
      await answer(projectUrl);
      await answer(tenantUrl);

      stream.next({ name });
      stream.next({ name });
      await wait(listReloadDelay);

      expect(take(projectUrl)).toHaveLength(1);
      expect(take(tenantUrl)).toHaveLength(1);
    });

    it('refetch nothing when no view shows a ticket', async () => {
      service.cache.put('acme/VKO-3', ticket('acme/VKO-3', 1));

      stream.next({ name: 'resync' });

      http.expectNone(ticketUrl('acme/VKO-3'));
    });

    it('remove the entry of a shown ticket whose refetch fails', async () => {
      await show('acme/VKO-1', 2);

      stream.next({ name: 'poll' });
      http
        .expectOne(ticketUrl('acme/VKO-1'))
        .flush(problem(404), { status: 404, statusText: 'Not Found' });
      await settle();

      expect(service.cache.value('acme/VKO-1')).toBeUndefined();
    });

    it.each([0, 502, 503])(
      'keep the entry of a shown ticket while the backend is down (status %i), to show what was last known',
      async (status) => {
        await show('acme/VKO-1', 2);

        stream.next({ name: 'poll' });
        fail(http.expectOne(ticketUrl('acme/VKO-1')), status);
        await settle();

        expect(service.cache.value('acme/VKO-1')?.version).toBe(2);
      },
    );

    it('remove the entry of a shown ticket on a resync that finds it forbidden', async () => {
      await show('acme/VKO-1', 2);

      stream.next({ name: 'resync' });
      fail(http.expectOne(ticketUrl('acme/VKO-1')), 403);
      await settle();

      expect(service.cache.value('acme/VKO-1')).toBeUndefined();
    });
  });

  describe('a membership event (docs/adr/0034 D3)', () => {
    it.each<[string, StreamEvent]>([
      ['a restriction set or lifted', { name: 'membership.changed', id: 'e1', projectId: 'j1' }],
      [
        'an access entry',
        { name: 'membership.changed', id: 'e1', personId: 'p2', projectId: 'j1' },
      ],
      ["the person's own role", { name: 'membership.changed', id: 'e1', personId: 'p1' }],
    ])(
      'refetches what a view shows and reloads the open lists on %s, which may hide a project',
      async (_what, event) => {
        await show('acme/VKO-1', 2);
        projectList();
        await settle();
        await answer(projectUrl);

        stream.next(event);
        http
          .expectOne(ticketUrl('acme/VKO-1'))
          .flush(problem(404), { status: 404, statusText: 'Not Found' });
        await wait(listReloadDelay);

        expect(service.cache.value('acme/VKO-1')).toBeUndefined();
        expect(take(projectUrl)).toHaveLength(1);
      },
    );

    it.each<[string, StreamEvent]>([
      ["somebody else's membership", { name: 'membership.changed', id: 'e1', personId: 'p2' }],
      ['a group mapping', { name: 'membership.changed', id: 'e1', mappingId: 'm1' }],
      [
        "a restriction in another of the person's tenants, which the person-level stream carries (docs/adr/0054 D1)",
        { name: 'membership.changed', id: 'e1', tenant: 'beta', projectId: 'j1' },
      ],
      [
        "the person's own role in another of their tenants",
        { name: 'membership.changed', id: 'e1', tenant: 'beta', personId: 'p1' },
      ],
    ])('leaves the tickets and the lists alone on %s', async (_what, event) => {
      await show('acme/VKO-1', 2);
      projectList();
      await settle();
      await answer(projectUrl);

      stream.next(event);
      await wait(10 * listReloadDelay);

      http.expectNone(ticketUrl('acme/VKO-1'));
      http.expectNone(projectUrl);
    });
  });

  describe('a view that shows a ticket', () => {
    it('is refetched while the view shows it', async () => {
      await show('acme/VKO-1', 2);

      stream.next({ name: 'resync' });

      http.expectOne(ticketUrl('acme/VKO-1')).flush(ticket('acme/VKO-1', 3));
      await settle();
    });

    it('is no longer refetched once the view is gone', async () => {
      const { injector } = await show('acme/VKO-1', 2);

      injector.destroy();
      stream.next({ name: 'resync' });

      http.expectNone(ticketUrl('acme/VKO-1'));
    });

    it('moves the watch to the next ticket when the key changes', async () => {
      const { shown } = await show('acme/VKO-1', 2);
      shown.set('acme/VKO-2');
      await settle();
      await answer(ticketUrl('acme/VKO-2'), ticket('acme/VKO-2', 2));

      stream.next({ name: 'resync' });

      http.expectNone(ticketUrl('acme/VKO-1'));
      http.expectOne(ticketUrl('acme/VKO-2')).flush(ticket('acme/VKO-2', 3));
      await settle();
    });

    it('is not watched while the key is undefined', async () => {
      const { shown } = await show('acme/VKO-1', 2);

      shown.set(undefined);
      await settle();
      stream.next({ name: 'resync' });

      http.expectNone(ticketUrl('acme/VKO-1'));
    });

    it('is watched for as long as any of the views that show it is there', async () => {
      const first = await show('acme/VKO-1', 2);
      const second = await show('acme/VKO-1', 2);
      const third = await show('acme/VKO-1', 2);

      stream.next({ name: 'resync' });
      http.expectOne(ticketUrl('acme/VKO-1')).flush(ticket('acme/VKO-1', 3));
      await settle();

      first.injector.destroy();
      stream.next({ name: 'resync' });
      http.expectOne(ticketUrl('acme/VKO-1')).flush(ticket('acme/VKO-1', 4));
      await settle();

      second.injector.destroy();
      stream.next({ name: 'resync' });
      http.expectOne(ticketUrl('acme/VKO-1')).flush(ticket('acme/VKO-1', 5));
      await settle();

      third.injector.destroy();
      stream.next({ name: 'resync' });
      http.expectNone(ticketUrl('acme/VKO-1'));
    });

    it('is refetched once per resync, however many views show it', async () => {
      await show('acme/VKO-1', 2);
      await show('acme/VKO-1', 2);

      stream.next({ name: 'resync' });

      http.expectOne(ticketUrl('acme/VKO-1')).flush(ticket('acme/VKO-1', 3));
      await settle();
    });

    it('is watched again when a view shows it after the last one was gone', async () => {
      const first = await show('acme/VKO-1', 2);
      first.injector.destroy();
      stream.next({ name: 'resync' });
      http.expectNone(ticketUrl('acme/VKO-1'));

      await show('acme/VKO-1', 2);
      stream.next({ name: 'resync' });

      http.expectOne(ticketUrl('acme/VKO-1')).flush(ticket('acme/VKO-1', 3));
      await settle();
    });
  });

  describe('when the tenant changes', () => {
    it('empties the cache, so that no card of one tenant shows in another (docs/adr/0053 D4)', async () => {
      service.cache.put('acme/VKO-1', ticket('acme/VKO-1'));
      service.cache.put('acme/VKO-2', ticket('acme/VKO-2'));
      const entry = service.cache.entry('acme/VKO-1');

      session.enter('globex');
      await settle();

      expect(service.cache.ids()).toEqual([]);
      expect(entry()).toBeUndefined();
    });

    it('keeps the cache while the tenant stays the same', async () => {
      service.cache.put('acme/VKO-1', ticket('acme/VKO-1'));

      session.enter('acme');
      await settle();

      expect(service.cache.ids()).toEqual(['acme/VKO-1']);
    });

    it('empties the cache on a move to a person-level page as well', async () => {
      service.cache.put('acme/VKO-1', ticket('acme/VKO-1'));

      session.enter(null);
      await settle();

      expect(service.cache.ids()).toEqual([]);
    });
  });

  describe('when it is destroyed', () => {
    it('stops listening to the event stream', () => {
      expect(stream.observed).toBe(true);

      TestBed.resetTestingModule();

      expect(stream.observed).toBe(false);
    });
  });
});

/** An EventSource that a test opens, fails and feeds by hand. */
class FakeEventSource implements EventSourceLike {
  readyState = 0;
  onopen: ((event: Event) => unknown) | null = null;
  onerror: ((event: Event) => unknown) | null = null;
  private readonly listeners = new Map<string, ((event: MessageEvent<string>) => void)[]>();

  constructor(readonly url: string) {}

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }

  close(): void {
    this.readyState = 2;
  }

  open(): void {
    this.readyState = 1;
    this.onopen?.(new Event('open'));
  }

  fail(): void {
    this.onerror?.(new Event('error'));
  }

  /** An event as the backend writes it: a name, an id and a JSON payload (docs/adr/0054 D2). */
  send(name: string, data: unknown, lastEventId = ''): void {
    const text = typeof data === 'string' ? data : JSON.stringify(data);
    for (const listener of this.listeners.get(name) ?? []) {
      listener(new MessageEvent<string>(name, { data: text, lastEventId }));
    }
  }
}

describe('TicketsService on the event stream of the backend', () => {
  const key = 'acme/VKO-12';
  let service: TicketsService;
  let http: HttpTestingController;
  let events: EventStreamService;
  let sources: FakeEventSource[];

  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  const wait = async (ms: number) => {
    await vi.advanceTimersByTimeAsync(ms);
    await settle();
  };
  const take = (url: string): TestRequest[] => http.match((request) => request.url === url);
  /** Answers every request that is open, the lists with nothing and the tickets with what they were. */
  const answerAll = async () => {
    for (const request of http.match(() => true)) {
      if (request.request.url.startsWith('/api/v1/tickets/')) {
        request.flush(ticket(request.request.url.replace('/api/v1/tickets/', ''), 9));
      } else {
        request.flush(listOf([]));
      }
    }
    await settle();
  };
  /** Shows the ticket the way a detail view does, and answers its load. */
  const show = async () => {
    const injector = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));
    service.ticket(() => key, injector);
    await settle();
    http.expectOne(ticketUrl(key)).flush(ticket(key, 5));
    await settle();
  };

  beforeEach(async () => {
    vi.useFakeTimers();
    sources = [];
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        {
          provide: EVENT_SOURCE,
          useValue: (url: string) => {
            const source = new FakeEventSource(url);
            sources.push(source);
            return source;
          },
        },
      ],
    });
    service = TestBed.inject(TicketsService);
    events = TestBed.inject(EventStreamService);
    http = TestBed.inject(HttpTestingController);
    await settle();
    http.expectOne('/api/v1/me').flush({ id: 'p1', display_name: 'Hans', memberships: [] });
    TestBed.inject(SessionService).enter('acme');
    await settle();
    events.connect('acme');
    sources[0].open();
  });

  afterEach(() => {
    try {
      // The session asks who is working again on a resync, which its own spec covers.
      http.match('/api/v1/me');
      http.verify();
    } finally {
      vi.useRealTimers();
      TestBed.resetTestingModule();
    }
  });

  it('refetches a cached ticket when an event names a newer version of it', async () => {
    service.cache.put(key, ticket(key, 5));

    sources[0].send('ticket.changed', { key, version: 6, kind: 'edited' }, 'e1');
    http.expectOne(ticketUrl(key)).flush(ticket(key, 6, { title: 'Renamed' }));
    await settle();

    expect(service.cache.value(key)?.title).toBe('Renamed');
    expect(service.cache.etag(key)).toBe('"6"');
  });

  it('does not refetch for the version that the cache holds', async () => {
    service.cache.put(key, ticket(key, 5));

    sources[0].send('ticket.changed', { key, version: 5, kind: 'edited' }, 'e1');
    await wait(listReloadDelay);

    http.expectNone(ticketUrl(key));
  });

  it('reloads the open lists a moment after an event, and not after a comment', async () => {
    TestBed.runInInjectionContext(() =>
      service.projectTickets(
        () => ({ tenant: 'acme', project: 'VKO' }),
        createEnvironmentInjector([], TestBed.inject(EnvironmentInjector)),
      ),
    );
    await settle();
    await answerAll();

    sources[0].send('comment.changed', { key, version: 2, kind: 'commented' }, 'e1');
    await wait(10 * listReloadDelay);
    http.expectNone(projectUrl);

    sources[0].send('ticket.changed', { key, version: 2, kind: 'edited' }, 'e2');
    await wait(listReloadDelay);
    expect(take(projectUrl)).toHaveLength(1);
  });

  it('does nothing about an event whose payload is not the documented one', async () => {
    service.cache.put(key, ticket(key, 5));
    TestBed.runInInjectionContext(() =>
      service.tenantTickets(
        () => ({ tenant: 'acme' }),
        createEnvironmentInjector([], TestBed.inject(EnvironmentInjector)),
      ),
    );
    await settle();
    await answerAll();

    sources[0].send('ticket.changed', '{}', 'e1');
    sources[0].send('ticket.changed', 'not json', 'e2');
    sources[0].send('ticket.changed', { key, version: '6', kind: 'edited' }, 'e3');
    await wait(10 * listReloadDelay);

    http.expectNone(ticketUrl(key));
    http.expectNone(tenantUrl);
  });

  it('refetches what a view shows, and reloads the lists, when a resync comes', async () => {
    await show();
    TestBed.runInInjectionContext(() =>
      service.projectTickets(
        () => ({ tenant: 'acme', project: 'VKO' }),
        createEnvironmentInjector([], TestBed.inject(EnvironmentInjector)),
      ),
    );
    await settle();
    await answerAll();

    sources[0].send('resync', {});
    await wait(listReloadDelay);

    http.expectOne(ticketUrl(key)).flush(ticket(key, 7));
    expect(take(projectUrl)).toHaveLength(1);
  });

  it('refetches and reloads once more when a stream that was down comes back', async () => {
    await show();
    TestBed.runInInjectionContext(() =>
      service.projectTickets(
        () => ({ tenant: 'acme', project: 'VKO' }),
        createEnvironmentInjector([], TestBed.inject(EnvironmentInjector)),
      ),
    );
    await settle();
    await answerAll();

    // The stream fails three times: the fallback begins, asks for a resync and polls.
    for (let i = 0; i < fallback.failures; i++) sources[0].fail();
    await wait(fallback.retryEvery - 1);
    await answerAll();
    // The minute is up: a poll and the retry are due together. The poll is answered...
    await wait(1);
    await answerAll();
    expect(sources).toHaveLength(2);

    // ...and the new stream, with no Last-Event-ID to replay from, opens: that is one more resync.
    sources[1].open();
    await wait(listReloadDelay);

    http.expectOne(ticketUrl(key)).flush(ticket(key, 8));
    expect(take(projectUrl)).toHaveLength(1);
  });

  it('does not refetch or reload for the first stream that opens', async () => {
    await show();
    TestBed.runInInjectionContext(() =>
      service.projectTickets(
        () => ({ tenant: 'acme', project: 'VKO' }),
        createEnvironmentInjector([], TestBed.inject(EnvironmentInjector)),
      ),
    );
    await settle();
    await answerAll();

    events.connect('globex');
    sources[1].open();
    await wait(10 * listReloadDelay);

    http.expectNone(ticketUrl(key));
    http.expectNone(projectUrl);
  });
});
