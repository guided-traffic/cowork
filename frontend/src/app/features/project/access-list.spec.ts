import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import {
  HttpTestingController,
  provideHttpClientTesting,
  TestRequest,
} from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Me, ProjectAccessEntry, ProjectAccessList } from '../../api/models';
import { EventStreamService, StreamEvent } from '../../core/event-stream.service';
import { SessionService } from '../../core/session.service';
import { AccessList } from './access-list';

const entry = (name: string, role: ProjectAccessEntry['role'] = 'member'): ProjectAccessEntry => ({
  person: { id: `id-${name}`, display_name: name },
  role,
  email: `${name.toLowerCase()}@example.com`,
  created_at: '2026-10-04T08:00:00Z',
});

const pageOf = (names: string[], next: string | null): ProjectAccessList => ({
  items: names.map((name) => entry(name)),
  next_cursor: next,
});

const person = (admin = true): Me => ({
  id: 'p1',
  display_name: 'Hans',
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships: [
    {
      team: { slug: 'acme', name: 'Acme' },
      tenant: { slug: 'acme', name: 'Acme' },
      role: admin ? 'admin' : 'member',
      origins: [{ source: 'grant', role: admin ? 'admin' : 'member' }],
      can_create_projects: true,
    },
  ],
});

const url = (project = 'VKO') => `/api/v1/teams/acme/projects/${project}/access`;

/** Fails a request: without an answer at all (status 0), or with a problem of the status. */
const fail = (request: TestRequest, status: number) =>
  status === 0
    ? request.error(new ProgressEvent('error'))
    : request.flush(
        { type: 'about:blank', title: 'Refused', status, code: 'internal' },
        { status, statusText: `Status ${status}` },
      );

const rejection = (promise: Promise<unknown>) =>
  promise.then(
    () => null,
    (error: unknown) => error,
  );

describe('AccessList', () => {
  let access: AccessList;
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
  const names = () => access.list().map((each) => each.person.display_name);
  const page = (project = 'VKO', cursor: string | null = null) =>
    http.expectOne(
      (request) =>
        request.method === 'GET' &&
        request.url === url(project) &&
        request.params.get('cursor') === cursor,
    );
  const noLoad = () =>
    http.expectNone((request) => request.method === 'GET' && request.url.endsWith('/access'));

  async function start(me: Me = person()) {
    vi.useFakeTimers();
    stream = new Subject<StreamEvent>();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
        AccessList,
      ],
    });
    access = TestBed.inject(AccessList);
    session = TestBed.inject(SessionService);
    http = TestBed.inject(HttpTestingController);
    TestBed.tick();
    http.expectOne('/api/v1/me').flush(me);
    session.enter('acme');
    await settle();
  }

  afterEach(() => {
    try {
      http.verify();
    } finally {
      vi.useRealTimers();
      TestBed.resetTestingModule();
    }
  });

  describe('the list', () => {
    it('asks for nothing until the page names a project', async () => {
      await start();

      expect(access.entries.status()).toBe('idle');
      noLoad();
    });

    it('asks for nothing for anybody but an administrator, who would only get a 403', async () => {
      await start(person(false));

      access.project.set('VKO');
      await settle();

      expect(access.entries.status()).toBe('idle');
      noLoad();
    });

    it("asks for the project's list, 200 at a time, and follows the cursor to every page", async () => {
      await start();

      access.project.set('VKO');
      await settle();
      const first = page();
      expect(first.request.params.get('limit')).toBe('200');
      first.flush(pageOf(['Ada'], 'c1'));
      await settle();
      page('VKO', 'c1').flush(pageOf(['Bob'], null));
      await settle();

      expect(names()).toEqual(['Ada', 'Bob']);
    });

    it('lists nobody when the list fails, and says why', async () => {
      await start();
      access.project.set('VKO');
      await settle();

      page().flush(
        { type: 'about:blank', title: 'Forbidden', status: 403, code: 'forbidden' },
        { status: 403, statusText: 'Forbidden' },
      );
      await settle();

      expect(access.entries.status()).toBe('error');
      expect(access.list()).toEqual([]);
    });

    it('follows the project of the page', async () => {
      await start();
      access.project.set('VKO');
      await settle();
      page().flush(pageOf(['Ada'], null));
      await settle();

      access.project.set('OPS');
      await settle();
      page('OPS').flush(pageOf(['Ops'], null));
      await settle();

      expect(names()).toEqual(['Ops']);
    });

    it('does not load it again when the person is loaded again with the same role', async () => {
      await start();
      access.project.set('VKO');
      await settle();
      page().flush(pageOf(['Ada'], null));
      await settle();

      session.me.reload();
      await settle();
      http.expectOne('/api/v1/me').flush(person());
      await settle();

      noLoad();
    });
  });

  describe('when the memberships change (docs/adr/0054 D2)', () => {
    beforeEach(async () => {
      await start();
      access.project.set('VKO');
      await settle();
      page().flush(pageOf(['Ada'], null));
      await settle();
    });

    it.each<StreamEvent>([
      { name: 'membership.changed', id: 'e1', personId: 'p2', projectId: 'j1' },
      { name: 'membership.changed', id: 'e1', projectId: 'j1' },
      { name: 'membership.changed', id: 'e1', tenant: 'acme', projectId: 'j1' },
      { name: 'resync' },
      { name: 'poll' },
    ])('loads the list again on %j', async (event) => {
      stream.next(event);
      await settle();

      page().flush(pageOf(['Ada', 'Bob'], null));
      http.expectOne('/api/v1/me').flush(person());
      await settle();

      expect(names()).toEqual(['Ada', 'Bob']);
    });

    it("leaves the list alone on an act of another of the person's tenants, which the person-level stream carries (docs/adr/0054 D1)", async () => {
      stream.next({ name: 'membership.changed', id: 'e1', tenant: 'beta', projectId: 'j1' });
      await settle();

      http.expectNone((request) => request.url === url());
      http.expectNone('/api/v1/me');
    });

    it("sends the list's weak ETag on a poll and keeps the entries on a 304 (docs/adr/0054 D7)", async () => {
      stream.next({ name: 'resync' });
      await settle();
      page().flush(pageOf(['Ada', 'Bob'], null), { headers: { ETag: 'W/"one"' } });
      http.expectOne('/api/v1/me').flush(person());
      await settle();

      stream.next({ name: 'poll' });
      await settle();
      const again = page();
      expect(again.request.headers.get('If-None-Match')).toBe('W/"one"');
      again.flush(null, { status: 304, statusText: 'Not Modified' });
      http.expectOne('/api/v1/me').flush(person());
      await settle();

      expect(access.entries.status()).toBe('resolved');
      expect(names()).toEqual(['Ada', 'Bob']);
    });

    it('leaves the list alone on an event that names a ticket', async () => {
      stream.next({ name: 'ticket.changed', id: 'e1', key: 'acme/VKO-1', version: 2, kind: 'x' });
      await settle();

      noLoad();
    });

    it.each([0, 500, 503])(
      'keeps listing the entries when loading them again fails with a status of %i',
      async (status) => {
        stream.next({ name: 'poll' });
        await settle();

        fail(page(), status);
        http.expectOne('/api/v1/me').flush(person());
        await settle();

        expect(access.entries.status()).toBe('resolved');
        expect(names()).toEqual(['Ada']);
      },
    );

    it.each([401, 403, 404])(
      'lists nobody when loading them again answers %i: the list is gone for the person',
      async (status) => {
        stream.next({ name: 'poll' });
        await settle();

        fail(page(), status);
        http.expectOne('/api/v1/me').flush(person());
        await settle();

        expect(access.entries.status()).toBe('error');
        expect(access.list()).toEqual([]);
      },
    );
  });

  describe('the writes', () => {
    const write = (method: string, address: string) =>
      http.expectOne((request) => request.method === method && request.url === address);

    beforeEach(async () => {
      await start();
      access.project.set('VKO');
      await settle();
      page().flush(pageOf(['Ada', 'Bob'], null));
      await settle();
    });

    it('puts a person on the list with the role, and lists the entry at once', async () => {
      const done = access.set('id-Cyd', 'viewer');

      const sent = write('PUT', `${url()}/id-Cyd`);
      expect(sent.request.body).toEqual({ role: 'viewer' });
      sent.flush(entry('Cyd', 'viewer'));
      await done;

      expect(names()).toEqual(['Ada', 'Bob', 'Cyd']);
      await settle();
      page().flush(pageOf(['Ada', 'Bob', 'Cyd'], null));
      await settle();
    });

    it('changes the entry in its place', async () => {
      const done = access.set('id-Ada', 'viewer');
      write('PUT', `${url()}/id-Ada`).flush(entry('Ada', 'viewer'));
      await done;

      expect(access.list().map((each) => [each.person.display_name, each.role])).toEqual([
        ['Ada', 'viewer'],
        ['Bob', 'member'],
      ]);
      await settle();
      page().flush(pageOf(['Ada', 'Bob'], null));
    });

    it('does not put an answer into the list of a project the page turned to meanwhile', async () => {
      const done = access.set('id-Ada', 'viewer');
      const sent = write('PUT', `${url()}/id-Ada`);
      access.project.set('OPS');
      await settle();
      page('OPS').flush(pageOf(['Ops'], null));
      await settle();

      sent.flush(entry('Ada', 'viewer'));
      await done;
      await settle();

      expect(names()).toEqual(['Ops']);
      page('OPS').flush(pageOf(['Ops'], null));
    });

    it('takes a person off the list and loads it again', async () => {
      const done = access.remove('id-Bob');

      write('DELETE', `${url()}/id-Bob`).flush(null, { status: 204, statusText: 'No Content' });
      await done;
      await settle();
      page().flush(pageOf(['Ada'], null));
      await settle();

      expect(names()).toEqual(['Ada']);
    });

    it('takes the entry out of the list at once, before the list has loaded again', async () => {
      const done = access.remove('id-Bob');

      write('DELETE', `${url()}/id-Bob`).flush(null, { status: 204, statusText: 'No Content' });
      await done;

      expect(names()).toEqual(['Ada']);
      await settle();
      page().flush(pageOf(['Ada'], null));
      await settle();
    });

    it('does not take an entry out of the list of a project the page turned to meanwhile', async () => {
      const done = access.remove('id-Bob');
      const sent = write('DELETE', `${url()}/id-Bob`);
      access.project.set('OPS');
      await settle();
      page('OPS').flush({ items: [entry('Bob')], next_cursor: null });
      await settle();

      sent.flush(null, { status: 204, statusText: 'No Content' });
      await done;

      expect(names()).toEqual(['Bob']);
      await settle();
      page('OPS').flush({ items: [entry('Bob')], next_cursor: null });
    });

    it('rejects with the HTTP error of a refusal and loads nothing again', async () => {
      const outcome = rejection(access.set('id-Gone', 'member'));

      write('PUT', `${url()}/id-Gone`).flush(
        { type: 'about:blank', title: 'Not found', status: 404, code: 'person_not_found' },
        { status: 404, statusText: 'Not Found' },
      );
      expect(((await outcome) as HttpErrorResponse).status).toBe(404);
      await settle();

      noLoad();
    });
  });
});
