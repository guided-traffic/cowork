import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import {
  HttpTestingController,
  provideHttpClientTesting,
  TestRequest,
} from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Subject } from 'rxjs';
import { Project, ProjectList } from '../api/models';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { ProjectsService } from './projects.service';
import { SessionService } from './session.service';

const project = (key: string, overrides: Partial<Project> = {}): Project => ({
  id: `0199aaaa-0000-7000-8000-${key.padStart(12, '0')}`,
  key,
  name: `Project ${key}`,
  description: '',
  restricted: false,
  created_at: '2026-10-01T10:00:00Z',
  updated_at: '2026-10-01T10:00:00Z',
  version: 1,
  wip_limits: {},
  ...overrides,
});

const pageOf = (keys: string[], next: string | null): ProjectList => ({
  items: keys.map((key) => project(key)),
  next_cursor: next,
});

/** Fails a request: without an answer at all (status 0), or with a problem of the status. */
const fail = (request: TestRequest, status: number) =>
  status === 0
    ? request.error(new ProgressEvent('error'))
    : request.flush(
        { type: 'about:blank', title: 'Refused', status, code: 'internal' },
        { status, statusText: `Status ${status}` },
      );

/** The key a form holds for its content (docs/adr/0045 D3). */
const formKey = '0199aaaa-0000-7000-8000-00000000f0f0';
const listUrl = '/api/v1/tenants/acme/projects';

const rejection = (promise: Promise<unknown>) =>
  promise.then(
    () => null,
    (error: unknown) => error,
  );

describe('ProjectsService', () => {
  let service: ProjectsService;
  let session: SessionService;
  let http: HttpTestingController;
  let stream: Subject<StreamEvent>;

  /**
   * Starts the loads that are due, lets the promise chains of answered requests finish, and runs the
   * effects they feed. Fake timers yield the macrotask for that, so this never waits for a request the
   * test has not answered yet, which ApplicationRef.whenStable() would.
   */
  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  const keys = () => service.list().map((entry) => entry.key);

  /** One page of the projects of a tenant, with the cursor that asked for it. */
  const page = (tenant: string, cursor: string | null = null) =>
    http.expectOne(
      (request) =>
        request.url === `/api/v1/tenants/${tenant}/projects` &&
        request.params.get('cursor') === cursor,
    );
  /** The page that the loader asks for once the previous one was taken, which is a promise away. */
  const nextPage = async (tenant: string, cursor: string) => {
    await settle();
    return page(tenant, cursor);
  };

  beforeEach(async () => {
    vi.useFakeTimers();
    stream = new Subject<StreamEvent>();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
      ],
    });
    service = TestBed.inject(ProjectsService);
    session = TestBed.inject(SessionService);
    http = TestBed.inject(HttpTestingController);
    TestBed.tick();
    http.expectOne('/api/v1/me').flush({ id: 'p1', display_name: 'Hans', memberships: [] });
    await settle();
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      vi.useRealTimers();
      TestBed.resetTestingModule();
    }
  });

  it('asks for nothing while no tenant is entered', () => {
    TestBed.tick();

    expect(service.projects.status()).toBe('idle');
    expect(service.list()).toEqual([]);
  });

  describe('in a tenant', () => {
    beforeEach(() => {
      session.enter('acme');
      TestBed.tick();
    });

    it('asks for the first page, 200 at a time, at /api/v1 without a doubled slash', async () => {
      const first = page('acme');

      expect(first.request.method).toBe('GET');
      expect(first.request.url).toBe('/api/v1/tenants/acme/projects');
      expect(first.request.params.get('limit')).toBe('200');
      expect(first.request.params.has('include_archived')).toBe(false);
      first.flush(pageOf([], null));
      await settle();
    });

    it('is loading, and lists nothing yet, until the first page arrives', async () => {
      const first = page('acme');

      expect(service.projects.status()).toBe('loading');
      expect(service.list()).toEqual([]);

      first.flush(pageOf(['VKO'], null));
      await settle();
    });

    it('lists the projects of a single page', async () => {
      page('acme').flush(pageOf(['VKO', 'COW'], null));
      await settle();

      expect(service.projects.status()).toBe('resolved');
      expect(keys()).toEqual(['VKO', 'COW']);
    });

    it('follows the cursor to every page and lists the projects in the order of the pages', async () => {
      page('acme').flush(pageOf(['A', 'B'], 'c1'));
      (await nextPage('acme', 'c1')).flush(pageOf(['C'], 'c2'));
      const last = await nextPage('acme', 'c2');
      expect(last.request.params.get('limit')).toBe('200');
      last.flush(pageOf(['D', 'E'], null));
      await settle();

      expect(keys()).toEqual(['A', 'B', 'C', 'D', 'E']);
    });

    it('lists nothing until the last page has arrived', async () => {
      page('acme').flush(pageOf(['A'], 'c1'));
      const second = await nextPage('acme', 'c1');

      expect(service.projects.status()).toBe('loading');
      expect(service.list()).toEqual([]);

      second.flush(pageOf(['B'], null));
      await settle();
      expect(keys()).toEqual(['A', 'B']);
    });

    it('stops at a page that names no cursor at all', async () => {
      page('acme').flush({ items: [project('A')] });
      await settle();

      expect(keys()).toEqual(['A']);
    });

    it('finds a project by its key', async () => {
      page('acme').flush(pageOf(['VKO', 'COW'], null));
      await settle();

      expect(service.byKey('COW')?.name).toBe('Project COW');
      expect(service.byKey('cow')).toBeUndefined();
      expect(service.byKey('NOPE')).toBeUndefined();
    });

    it('finds no project by key before they are loaded', async () => {
      const first = page('acme');

      expect(service.byKey('VKO')).toBeUndefined();

      first.flush(pageOf(['VKO'], null));
      await settle();
    });

    it('lists nothing when a page fails, and says why', async () => {
      page('acme').flush(
        { type: 'about:blank', title: 'Internal', status: 500, code: 'internal' },
        { status: 500, statusText: 'Internal Server Error' },
      );
      await settle();

      expect(service.projects.status()).toBe('error');
      expect(service.list()).toEqual([]);
      expect(service.byKey('VKO')).toBeUndefined();
    });

    it('never lists a page of the old tenant that arrives after the tenant changed', async () => {
      page('acme').flush(pageOf(['A'], 'c1'));
      const late = await nextPage('acme', 'c1');

      session.enter('globex');
      TestBed.tick();
      page('globex').flush(pageOf(['G'], null));
      await settle();
      expect(keys()).toEqual(['G']);

      late.flush(pageOf(['B'], null));
      await settle();

      expect(keys()).toEqual(['G']);
    });

    it('keeps listing the projects while it loads them again', async () => {
      page('acme').flush(pageOf(['A'], null));
      await settle();

      expect(service.projects.reload()).toBe(true);
      TestBed.tick();
      const again = page('acme');

      expect(service.projects.status()).toBe('reloading');
      expect(keys()).toEqual(['A']);

      again.flush(pageOf(['A', 'B'], null));
      await settle();
      expect(keys()).toEqual(['A', 'B']);
    });
  });

  describe('the writes', () => {
    /** What a write that fails answers, shaped as the API's problem details. */
    const refusal = (status: number, code: string) => ({
      body: { type: 'about:blank', title: 'Refused', status, code },
      init: { status, statusText: `Status ${status}` },
    });
    /** The write that is on its way, taken by its method and address. */
    const write = (method: string, url: string) =>
      http.expectOne((request) => request.method === method && request.url === url);
    /** The reload of the list that follows a write, once the write's answer is taken. */
    const reload = async () => {
      await settle();
      return page('acme');
    };

    beforeEach(async () => {
      session.enter('acme');
      TestBed.tick();
      page('acme').flush(pageOf(['VKO'], null));
      await settle();
    });

    describe('create', () => {
      const body = {
        key: 'COW',
        name: 'Cow',
        description: 'The cow project',
        wip_limits: { 'in-progress': 3 },
      };

      it('posts the project to the projects of the tenant and hands back the one that was made', async () => {
        const done = service.create(body, formKey);

        const sent = write('POST', listUrl);
        expect(sent.request.body).toEqual(body);
        expect(sent.request.headers.has('If-Match')).toBe(false);
        const made = project('COW', { name: 'Cow', description: 'The cow project', version: 1 });
        sent.flush(made);

        expect(await done).toEqual(made);
        (await reload()).flush(pageOf(['VKO', 'COW'], null));
      });

      it("sends the form's Idempotency-Key, one for each content it holds (docs/adr/0045 D3)", async () => {
        const done = service.create(body, formKey);
        const sent = write('POST', listUrl);
        expect(sent.request.headers.get('Idempotency-Key')).toBe(formKey);
        sent.flush(project('COW'));
        await done;
        (await reload()).flush(pageOf(['VKO', 'COW'], null));
      });

      it('loads the list again, so that the new project shows in the navigation', async () => {
        const done = service.create(body, formKey);
        write('POST', listUrl).flush(project('COW'));
        await done;

        const again = await reload();
        expect(again.request.params.get('limit')).toBe('200');
        expect(service.projects.status()).toBe('reloading');
        expect(keys()).toEqual(['VKO']);
        again.flush(pageOf(['VKO', 'COW'], null));
        await settle();

        expect(keys()).toEqual(['VKO', 'COW']);
        expect(service.byKey('COW')?.name).toBe('Project COW');
      });

      it('loads the list again only after the backend has answered', async () => {
        const done = service.create(body, formKey);
        const sent = write('POST', listUrl);
        await settle();

        http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
        sent.flush(project('COW'));
        await done;
        (await reload()).flush(pageOf(['VKO', 'COW'], null));
      });

      it('writes to the tenant that is entered', async () => {
        session.enter('globex');
        await settle();
        page('globex').flush(pageOf(['G'], null));
        await settle();

        const done = service.create(body, formKey);
        write('POST', '/api/v1/tenants/globex/projects').flush(project('COW'));
        await done;

        await settle();
        page('globex').flush(pageOf(['G', 'COW'], null));
      });

      it.each([
        [409, 'project_key_taken'],
        [422, 'validation_failed'],
        [403, 'forbidden'],
        [500, 'internal'],
      ])(
        'rejects with the HTTP error of a %i and does not load the list again',
        async (status, code) => {
          const outcome = rejection(service.create(body, formKey));

          write('POST', listUrl).flush(refusal(status, code).body, refusal(status, code).init);
          const error = await outcome;
          await settle();

          expect(error).toBeInstanceOf(HttpErrorResponse);
          expect((error as HttpErrorResponse).status).toBe(status);
          http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
          expect(keys()).toEqual(['VKO']);
        },
      );
    });

    describe('update', () => {
      it('patches the project over the version that was read, as If-Match (docs/adr/0050 D3)', async () => {
        const done = service.update(project('VKO', { version: 7 }), { name: 'Renamed' });

        const sent = write('PATCH', `${listUrl}/VKO`);
        expect(sent.request.headers.get('If-Match')).toBe('"7"');
        expect(sent.request.body).toEqual({ name: 'Renamed' });
        sent.flush(project('VKO', { name: 'Renamed', version: 8 }));

        const changed = await done;
        expect(changed.name).toBe('Renamed');
        expect(changed.version).toBe(8);
        (await reload()).flush({ items: [changed], next_cursor: null });
      });

      it('takes the version from the project it is given, not from the list that is held', async () => {
        const done = service.update(project('VKO', { version: 41 }), { description: 'New' });

        const sent = write('PATCH', `${listUrl}/VKO`);
        expect(sent.request.headers.get('If-Match')).toBe('"41"');
        sent.flush(project('VKO', { version: 42 }));
        await done;
        (await reload()).flush(pageOf(['VKO'], null));
      });

      it('addresses the project by its key', async () => {
        const done = service.update(project('COW'), { name: 'Cow' });

        write('PATCH', `${listUrl}/COW`).flush(project('COW'));
        await done;
        (await reload()).flush(pageOf(['VKO', 'COW'], null));
      });

      it('sends the patch as it is, the limits it names and none it does not', async () => {
        const done = service.update(project('VKO'), {
          wip_limits: { 'in-progress': 2, blocked: 1 },
        });

        const sent = write('PATCH', `${listUrl}/VKO`);
        expect(sent.request.body).toEqual({ wip_limits: { 'in-progress': 2, blocked: 1 } });
        sent.flush(project('VKO', { wip_limits: { 'in-progress': 2, blocked: 1 }, version: 2 }));
        await done;
        (await reload()).flush(pageOf(['VKO'], null));
      });

      it('needs no key, being a patch of a project that exists', async () => {
        const done = service.update(project('VKO'), { name: 'Renamed' });

        const sent = write('PATCH', `${listUrl}/VKO`);
        expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
        sent.flush(project('VKO', { version: 2 }));
        await done;
        (await reload()).flush(pageOf(['VKO'], null));
      });

      it('loads the list again, so that the change shows in the navigation', async () => {
        const done = service.update(project('VKO'), { name: 'Renamed' });
        write('PATCH', `${listUrl}/VKO`).flush(project('VKO', { name: 'Renamed', version: 2 }));
        await done;

        (await reload()).flush({
          items: [project('VKO', { name: 'Renamed', version: 2 })],
          next_cursor: null,
        });
        await settle();

        expect(service.byKey('VKO')?.name).toBe('Renamed');
      });

      it.each([
        [412, 'precondition_failed'],
        [428, 'precondition_required'],
        [409, 'project_archived'],
        [403, 'forbidden'],
      ])(
        'rejects with the HTTP error of a %i and does not load the list again',
        async (status, code) => {
          const outcome = rejection(service.update(project('VKO'), { name: 'Renamed' }));

          write('PATCH', `${listUrl}/VKO`).flush(
            refusal(status, code).body,
            refusal(status, code).init,
          );
          const error = await outcome;
          await settle();

          expect((error as HttpErrorResponse).status).toBe(status);
          http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
        },
      );
    });

    describe('archive', () => {
      it('puts the archiving of the project, which overwrites nothing and so needs no version', async () => {
        const done = service.archive(project('VKO', { version: 7 }));

        const sent = write('PUT', `${listUrl}/VKO/archive`);
        expect(sent.request.body).toBeNull();
        expect(sent.request.headers.has('If-Match')).toBe(false);
        expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
        sent.flush(project('VKO', { archived_at: '2026-10-03T11:00:00Z', version: 8 }));

        const archived = await done;
        expect(archived.archived_at).toBe('2026-10-03T11:00:00Z');
        (await reload()).flush(pageOf([], null));
      });

      it('addresses the project by its key', async () => {
        const done = service.archive(project('COW'));

        write('PUT', `${listUrl}/COW/archive`).flush(project('COW'));
        await done;
        (await reload()).flush(pageOf(['VKO'], null));
      });

      it('loads the list again, so that the project leaves the navigation', async () => {
        expect(service.byKey('VKO')).toBeDefined();
        const done = service.archive(project('VKO'));
        write('PUT', `${listUrl}/VKO/archive`).flush(project('VKO'));
        await done;

        (await reload()).flush(pageOf([], null));
        await settle();

        expect(keys()).toEqual([]);
        expect(service.byKey('VKO')).toBeUndefined();
      });

      it.each([
        [409, 'project_archived'],
        [403, 'forbidden'],
        [404, 'not_found'],
      ])(
        'rejects with the HTTP error of a %i and does not load the list again',
        async (status, code) => {
          const outcome = rejection(service.archive(project('VKO')));

          write('PUT', `${listUrl}/VKO/archive`).flush(
            refusal(status, code).body,
            refusal(status, code).init,
          );
          const error = await outcome;
          await settle();

          expect((error as HttpErrorResponse).status).toBe(status);
          http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
          expect(keys()).toEqual(['VKO']);
        },
      );
    });

    describe('restrict', () => {
      it('puts the restriction over the version that was read, as If-Match (docs/adr/0050 D3)', async () => {
        const done = service.restrict(project('VKO', { version: 7 }), true);

        const sent = write('PUT', `${listUrl}/VKO/restriction`);
        expect(sent.request.body).toEqual({ restricted: true });
        expect(sent.request.headers.get('If-Match')).toBe('"7"');
        sent.flush(project('VKO', { restricted: true, version: 8 }));

        expect((await done).restricted).toBe(true);
        (await reload()).flush(pageOf(['VKO'], null));
      });

      it('opens a project with false', async () => {
        const done = service.restrict(project('VKO', { restricted: true }), false);

        const sent = write('PUT', `${listUrl}/VKO/restriction`);
        expect(sent.request.body).toEqual({ restricted: false });
        sent.flush(project('VKO', { version: 2 }));
        await done;
        (await reload()).flush(pageOf(['VKO'], null));
      });

      it('puts the project as the answer has it into the list at once, and loads the list again', async () => {
        const done = service.restrict(project('VKO'), true);
        write('PUT', `${listUrl}/VKO/restriction`).flush(
          project('VKO', { restricted: true, version: 2 }),
        );
        await done;

        expect(service.byKey('VKO')?.restricted).toBe(true);
        expect(service.byKey('VKO')?.version).toBe(2);
        (await reload()).flush({
          items: [project('VKO', { restricted: true, version: 2 })],
          next_cursor: null,
        });
        await settle();
        expect(service.byKey('VKO')?.restricted).toBe(true);
      });

      it('leaves the other projects of the list as they are', async () => {
        service.projects.reload();
        await settle();
        page('acme').flush(pageOf(['VKO', 'COW'], null));
        await settle();
        const cow = service.byKey('COW');

        const done = service.restrict(project('VKO'), true);
        write('PUT', `${listUrl}/VKO/restriction`).flush(
          project('VKO', { restricted: true, version: 2 }),
        );
        await done;

        expect(service.byKey('COW')).toBe(cow);
        expect(service.byKey('VKO')?.restricted).toBe(true);
        (await reload()).flush(pageOf(['VKO', 'COW'], null));
      });

      it('loads a list that failed again, instead of putting the answer into it', async () => {
        service.projects.reload();
        await settle();
        // Only a failure that says the list is gone empties it; an outage would keep it shown.
        page('acme').flush(
          { type: 'about:blank', title: 'Forbidden', status: 403, code: 'forbidden' },
          { status: 403, statusText: 'Forbidden' },
        );
        await settle();
        expect(service.projects.status()).toBe('error');

        const done = service.restrict(project('VKO'), true);
        write('PUT', `${listUrl}/VKO/restriction`).flush(
          project('VKO', { restricted: true, version: 2 }),
        );
        await done;

        expect(service.list()).toEqual([]);
        (await reload()).flush({
          items: [project('VKO', { restricted: true, version: 2 })],
          next_cursor: null,
        });
        await settle();
        expect(service.byKey('VKO')?.restricted).toBe(true);
      });

      it('does not put an answer into the list of a tenant the pages turned to meanwhile', async () => {
        const done = service.restrict(project('VKO'), true);
        const sent = write('PUT', `${listUrl}/VKO/restriction`);
        session.enter('globex');
        TestBed.tick();
        page('globex').flush(pageOf(['VKO'], null));
        await settle();

        sent.flush(project('VKO', { restricted: true, version: 2 }));
        await done;
        await settle();

        expect(service.byKey('VKO')?.restricted).toBe(false);
        page('globex').flush(pageOf(['VKO'], null));
      });

      it('loads the list once more when it ends during a load, whose answer may predate it', async () => {
        service.projects.reload();
        await settle();
        const inFlight = page('acme');

        const done = service.restrict(project('VKO'), true);
        write('PUT', `${listUrl}/VKO/restriction`).flush(
          project('VKO', { restricted: true, version: 2 }),
        );
        await done;
        await settle();
        inFlight.flush(pageOf(['VKO'], null));
        await settle();

        page('acme').flush({
          items: [project('VKO', { restricted: true, version: 2 })],
          next_cursor: null,
        });
        await settle();
        expect(service.byKey('VKO')?.restricted).toBe(true);
      });

      it.each([
        [412, 'precondition_failed'],
        [428, 'precondition_required'],
        [403, 'forbidden'],
      ])('rejects with the HTTP error of a %i and changes nothing', async (status, code) => {
        const outcome = rejection(service.restrict(project('VKO'), true));

        write('PUT', `${listUrl}/VKO/restriction`).flush(
          refusal(status, code).body,
          refusal(status, code).init,
        );
        const error = await outcome;
        await settle();

        expect((error as HttpErrorResponse).status).toBe(status);
        expect(service.byKey('VKO')?.restricted).toBe(false);
        http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
      });
    });

    // A write that ends during a load of the list — a second write in quick succession, or a slow
    // first load — must show itself: the answer on its way was asked for before the write, so the
    // list loads once more when that load ends (core/refresh.ts).
    describe.each([
      [
        'creating',
        'POST',
        listUrl,
        (s: ProjectsService) => s.create({ key: 'COW', name: 'Cow' }, formKey),
      ],
      [
        'updating',
        'PATCH',
        `${listUrl}/VKO`,
        (s: ProjectsService) => s.update(project('VKO'), { name: 'Cow' }),
      ],
      [
        'archiving',
        'PUT',
        `${listUrl}/VKO/archive`,
        (s: ProjectsService) => s.archive(project('VKO')),
      ],
    ])('%s a project, when it ends while the list is loading', (_act, method, url, run) => {
      it('loads the list once more when that load ends, because its answer may predate the write', async () => {
        service.projects.reload();
        await settle();
        const inFlight = page('acme');

        const done = run(service);
        write(method, url).flush(project('VKO'));
        await done;
        await settle();
        inFlight.flush(pageOf(['VKO'], null));
        await settle();

        // The answer that just arrived was asked for before the write: the list loads once more.
        page('acme').flush(pageOf(['VKO', 'COW'], null));
      });
    });
  });

  describe('when the tenant changes', () => {
    beforeEach(async () => {
      session.enter('acme');
      TestBed.tick();
      page('acme').flush(pageOf(['A'], null));
      await settle();
    });

    it('drops the projects of the old tenant at once and lists those of the new one when they arrive', async () => {
      session.enter('globex');
      TestBed.tick();
      const globex = page('globex');

      expect(service.list()).toEqual([]);
      expect(service.byKey('A')).toBeUndefined();

      globex.flush(pageOf(['G'], null));
      await settle();
      expect(keys()).toEqual(['G']);
    });

    it('lists nothing on a person-level page', async () => {
      session.enter(null);
      TestBed.tick();

      expect(service.projects.status()).toBe('idle');
      expect(service.list()).toEqual([]);
    });
  });

  describe('when the stream says who sees which project (docs/adr/0034 D3, docs/adr/0054)', () => {
    beforeEach(async () => {
      session.enter('acme');
      TestBed.tick();
      page('acme').flush(pageOf(['VKO'], null));
      await settle();
    });

    /** The session asks who is working again on every membership event; this answers it. */
    const answerMe = () =>
      http.expectOne('/api/v1/me').flush({ id: 'p1', display_name: 'Hans', memberships: [] });

    it.each<[string, StreamEvent]>([
      ['a restriction set or lifted', { name: 'membership.changed', id: 'e1', projectId: 'j1' }],
      [
        'an access entry',
        { name: 'membership.changed', id: 'e1', personId: 'p2', projectId: 'j1' },
      ],
      ["the person's own role", { name: 'membership.changed', id: 'e1', personId: 'p1' }],
      ['a resync', { name: 'resync' }],
      ["the fallback's poll", { name: 'poll' }],
    ])('loads the projects again on %s', async (_what, event) => {
      stream.next(event);
      await settle();

      page('acme').flush(pageOf(['VKO', 'SEC'], null));
      answerMe();
      await settle();

      expect(keys()).toEqual(['VKO', 'SEC']);
    });

    it.each<[string, StreamEvent]>([
      ["somebody else's membership", { name: 'membership.changed', id: 'e1', personId: 'p2' }],
      ['a group mapping', { name: 'membership.changed', id: 'e1', mappingId: 'm1' }],
    ])('leaves the projects alone on %s', async (_what, event) => {
      stream.next(event);
      await settle();
      answerMe();
      await settle();

      http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
    });

    it.each<[string, StreamEvent]>([
      [
        "a restriction in another of the person's tenants, which the person-level stream carries (docs/adr/0054 D1)",
        { name: 'membership.changed', id: 'e1', tenant: 'beta', projectId: 'j1' },
      ],
      [
        "the person's own role in another of their tenants",
        { name: 'membership.changed', id: 'e1', tenant: 'beta', personId: 'p1' },
      ],
    ])('leaves the projects alone on %s', async (_what, event) => {
      stream.next(event);
      await settle();
      for (const request of http.match('/api/v1/me')) {
        request.flush({ id: 'p1', display_name: 'Hans', memberships: [] });
      }
      await settle();

      http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
    });

    it('leaves the projects alone on an event that names a ticket', async () => {
      stream.next({ name: 'ticket.changed', id: 'e1', key: 'acme/VKO-1', version: 2, kind: 'x' });
      await settle();

      http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
    });

    it.each([0, 500, 503])(
      'keeps listing the projects when loading them again fails with a status of %i',
      async (status) => {
        stream.next({ name: 'poll' });
        await settle();

        fail(page('acme'), status);
        answerMe();
        await settle();

        expect(service.projects.status()).toBe('resolved');
        expect(keys()).toEqual(['VKO']);
      },
    );

    it('keeps listing the projects when a later page of the load again fails', async () => {
      stream.next({ name: 'poll' });
      await settle();

      page('acme').flush(pageOf(['VKO'], 'c1'));
      answerMe();
      fail(await nextPage('acme', 'c1'), 503);
      await settle();

      expect(keys()).toEqual(['VKO']);
    });

    it("sends each page's weak ETag on a poll and keeps the projects on a 304 (docs/adr/0054 D7)", async () => {
      // The projects as they were loaded with their tags: two pages.
      stream.next({ name: 'resync' });
      await settle();
      page('acme').flush(pageOf(['VKO'], 'c1'), { headers: { ETag: 'W/"one"' } });
      answerMe();
      (await nextPage('acme', 'c1')).flush(pageOf(['SEC'], null), {
        headers: { ETag: 'W/"two"' },
      });
      await settle();
      expect(keys()).toEqual(['VKO', 'SEC']);

      stream.next({ name: 'poll' });
      await settle();
      const first = page('acme');
      expect(first.request.headers.get('If-None-Match')).toBe('W/"one"');
      first.flush(null, { status: 304, statusText: 'Not Modified' });
      answerMe();
      const second = await nextPage('acme', 'c1');
      expect(second.request.headers.get('If-None-Match')).toBe('W/"two"');
      second.flush(null, { status: 304, statusText: 'Not Modified' });
      await settle();

      expect(service.projects.status()).toBe('resolved');
      expect(keys()).toEqual(['VKO', 'SEC']);
    });

    it.each([401, 403, 404])(
      'lists nothing when loading them again answers %i: they are gone for the person',
      async (status) => {
        stream.next({ name: 'poll' });
        await settle();

        fail(page('acme'), status);
        answerMe();
        await settle();

        expect(service.projects.status()).toBe('error');
        expect(service.list()).toEqual([]);
      },
    );
  });
});
