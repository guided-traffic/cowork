import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import {
  HttpTestingController,
  provideHttpClientTesting,
  TestRequest,
} from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../api/api-configuration';
import { GroupMapping, GroupMappingList, Me, MemberList } from '../api/models';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { GroupMappingsService } from './group-mappings.service';
import { MembersService } from './members.service';
import { SessionService } from './session.service';

const mapping = (group: string, overrides: Partial<GroupMapping> = {}): GroupMapping => ({
  id: `id-${group}`,
  group,
  role: 'member',
  includes_caller: false,
  version: 1,
  created_at: '2026-10-04T08:00:00Z',
  updated_at: '2026-10-04T08:00:00Z',
  ...overrides,
});

const pageOf = (groups: string[], next: string | null): GroupMappingList => ({
  items: groups.map((group) => mapping(group)),
  next_cursor: next,
});

const noMembers: MemberList = { items: [], next_cursor: null };

const person = (admin = true): Me => ({
  id: 'p1',
  display_name: 'Hans',
  username: 'hans',
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships: [
    {
      tenant: { slug: 'acme', name: 'Acme' },
      role: admin ? 'admin' : 'member',
      origins: [{ source: 'grant', role: admin ? 'admin' : 'member' }],
    },
    {
      tenant: { slug: 'globex', name: 'Globex' },
      role: 'admin',
      origins: [{ source: 'mapping', role: 'admin' }],
    },
    {
      tenant: { slug: 'initech', name: 'Initech' },
      role: 'viewer',
      origins: [{ source: 'grant', role: 'viewer' }],
    },
  ],
});

const listUrl = '/api/v1/tenants/acme/group-mappings';
const membersUrl = '/api/v1/tenants/acme/members';

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

describe('GroupMappingsService', () => {
  let service: GroupMappingsService;
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
  const groups = () => service.list().map((entry) => entry.group);

  /** One page of the mappings of a tenant, with the cursor that asked for it. */
  const page = (tenant: string, cursor: string | null = null) =>
    http.expectOne(
      (request) =>
        request.method === 'GET' &&
        request.url === `/api/v1/tenants/${tenant}/group-mappings` &&
        request.params.get('cursor') === cursor,
    );
  /** The page that the loader asks for once the previous one was taken, which is a promise away. */
  const nextPage = async (tenant: string, cursor: string) => {
    await settle();
    return page(tenant, cursor);
  };
  const noLoad = (tenant = 'acme') =>
    http.expectNone(
      (request) =>
        request.method === 'GET' && request.url === `/api/v1/tenants/${tenant}/group-mappings`,
    );

  async function start(me: Me = person()) {
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
    service = TestBed.inject(GroupMappingsService);
    session = TestBed.inject(SessionService);
    http = TestBed.inject(HttpTestingController);
    TestBed.tick();
    http.expectOne('/api/v1/me').flush(me);
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
    beforeEach(() => start());

    it('asks for nothing while no tenant is entered', () => {
      TestBed.tick();

      expect(service.mappings.status()).toBe('idle');
      expect(service.list()).toEqual([]);
    });

    it('asks for nothing in a tenant the person does not administer, which would only be a 403', async () => {
      for (const tenant of ['initech', 'nowhere']) {
        session.enter(tenant);
        await settle();

        expect(service.mappings.status()).toBe('idle');
        noLoad(tenant);
      }
      expect(service.list()).toEqual([]);
    });

    describe('in a tenant the person administers', () => {
      beforeEach(() => {
        session.enter('acme');
        TestBed.tick();
      });

      it('asks for the first page, 200 at a time', async () => {
        const first = page('acme');

        expect(first.request.url).toBe(listUrl);
        expect(first.request.params.get('limit')).toBe('200');
        first.flush(pageOf([], null));
        await settle();
      });

      it('follows the cursor to every page and lists the mappings in the order of the pages', async () => {
        page('acme').flush(pageOf(['a', 'b'], 'c1'));
        (await nextPage('acme', 'c1')).flush(pageOf(['c'], null));
        await settle();

        expect(groups()).toEqual(['a', 'b', 'c']);
      });

      it('lists nothing until the last page has arrived', async () => {
        page('acme').flush(pageOf(['a'], 'c1'));
        const second = await nextPage('acme', 'c1');

        expect(service.mappings.status()).toBe('loading');
        expect(service.list()).toEqual([]);

        second.flush(pageOf(['b'], null));
        await settle();
        expect(groups()).toEqual(['a', 'b']);
      });

      it('lists nothing when a page fails, and says why', async () => {
        page('acme').flush(
          { type: 'about:blank', title: 'Forbidden', status: 403, code: 'forbidden' },
          { status: 403, statusText: 'Forbidden' },
        );
        await settle();

        expect(service.mappings.status()).toBe('error');
        expect(service.list()).toEqual([]);
      });

      it('does not load them again when the person is loaded again with the same role', async () => {
        page('acme').flush(pageOf(['a'], null));
        await settle();

        session.me.reload();
        await settle();
        http.expectOne('/api/v1/me').flush(person());
        await settle();

        noLoad();
        expect(groups()).toEqual(['a']);
      });

      it('stops asking when the person turns out not to administer the tenant any more', async () => {
        page('acme').flush(pageOf(['a'], null));
        await settle();

        session.me.reload();
        await settle();
        http.expectOne('/api/v1/me').flush(person(false));
        await settle();

        expect(service.mappings.status()).toBe('idle');
        expect(service.list()).toEqual([]);
      });

      it('lists those of the next tenant the person administers when the tenant changes', async () => {
        page('acme').flush(pageOf(['a'], null));
        await settle();

        session.enter('globex');
        TestBed.tick();
        page('globex').flush(pageOf(['g'], null));
        await settle();

        expect(groups()).toEqual(['g']);
      });
    });
  });

  describe('when the memberships change (docs/adr/0054 D2)', () => {
    beforeEach(async () => {
      await start();
      session.enter('acme');
      TestBed.tick();
      page('acme').flush(pageOf(['a'], null));
      await settle();
    });

    it.each<StreamEvent>([
      { name: 'membership.changed', id: 'e1', mappingId: 'm1' },
      { name: 'membership.changed', id: 'e1', personId: 'p2' },
      { name: 'resync' },
      { name: 'poll' },
    ])('loads the mappings again on %j', async (event) => {
      stream.next(event);
      await settle();

      page('acme').flush(pageOf(['a', 'b'], null));
      http.expectOne('/api/v1/me').flush(person());
      await settle();

      expect(groups()).toEqual(['a', 'b']);
    });

    it('leaves the mappings alone on an event that names a ticket', async () => {
      stream.next({ name: 'ticket.changed', id: 'e1', key: 'acme/VKO-1', version: 2, kind: 'x' });
      await settle();

      noLoad();
    });

    it.each([0, 500, 503])(
      'keeps listing the mappings when loading them again fails with a status of %i',
      async (status) => {
        stream.next({ name: 'poll' });
        await settle();

        fail(page('acme'), status);
        http.expectOne('/api/v1/me').flush(person());
        await settle();

        expect(service.mappings.status()).toBe('resolved');
        expect(groups()).toEqual(['a']);
      },
    );

    it.each([401, 403, 404])(
      'lists nothing when loading them again answers %i: they are gone for the person',
      async (status) => {
        stream.next({ name: 'poll' });
        await settle();

        fail(page('acme'), status);
        http.expectOne('/api/v1/me').flush(person());
        await settle();

        expect(service.mappings.status()).toBe('error');
        expect(service.list()).toEqual([]);
      },
    );
  });

  describe('the writes', () => {
    const write = (method: string, url: string) =>
      http.expectOne((request) => request.method === method && request.url === url);
    /** What follows every write: the mappings and the members the mapping derives, loaded again. */
    const reloads = async (groupsNow: string[]) => {
      await settle();
      page('acme').flush(pageOf(groupsNow, null));
      http.expectOne((request) => request.url === membersUrl).flush(noMembers);
      await settle();
    };

    beforeEach(async () => {
      await start();
      session.enter('acme');
      TestBed.tick();
      page('acme').flush(pageOf(['team-red', 'cowork-admins'], null));
      // The members are loaded already, as the tenant's pages load them.
      TestBed.inject(MembersService);
      await settle();
      http.expectOne((request) => request.url === membersUrl).flush(noMembers);
      await settle();
    });

    describe('create', () => {
      it('posts the group and the role with the key of the form (docs/adr/0045)', async () => {
        const done = service.create('team-blue', 'viewer', 'key-1');

        const sent = write('POST', listUrl);
        expect(sent.request.body).toEqual({ group: 'team-blue', role: 'viewer' });
        expect(sent.request.headers.get('Idempotency-Key')).toBe('key-1');
        const made = mapping('team-blue', { role: 'viewer' });
        sent.flush(made, { status: 201, statusText: 'Created' });

        expect(await done).toEqual(made);
        await reloads(['team-red', 'cowork-admins', 'team-blue']);
      });

      it('lists the new mapping at once, and loads the mappings and the members again', async () => {
        const done = service.create('team-blue', 'viewer', 'key-1');
        write('POST', listUrl).flush(mapping('team-blue', { role: 'viewer' }));
        await done;

        expect(groups()).toEqual(['team-red', 'cowork-admins', 'team-blue']);
        await reloads(['team-red', 'cowork-admins', 'team-blue']);
        http.expectNone('/api/v1/me');
      });

      it("asks who is working again when the group is the person's own", async () => {
        const done = service.create('team-blue', 'admin', 'key-1');
        write('POST', listUrl).flush(mapping('team-blue', { includes_caller: true }));
        await done;

        await reloads(['team-red', 'cowork-admins', 'team-blue']);
        http.expectOne('/api/v1/me').flush(person());
        await settle();
      });

      it('rejects with the HTTP error of a refusal and loads nothing again', async () => {
        const outcome = rejection(service.create('team-red', 'member', 'key-1'));

        write('POST', listUrl).flush(
          { type: 'about:blank', title: 'Conflict', status: 409, code: 'mapping_exists' },
          { status: 409, statusText: 'Conflict' },
        );
        expect(((await outcome) as HttpErrorResponse).status).toBe(409);
        await settle();

        noLoad();
        expect(groups()).toEqual(['team-red', 'cowork-admins']);
      });
    });

    describe('changeRole', () => {
      it('patches the role over the version that was read, as If-Match (docs/adr/0050 D3)', async () => {
        const done = service.changeRole(mapping('team-red', { version: 7 }), 'viewer');

        const sent = write('PATCH', `${listUrl}/id-team-red`);
        expect(sent.request.headers.get('If-Match')).toBe('"7"');
        expect(sent.request.body).toEqual({ role: 'viewer' });
        sent.flush(mapping('team-red', { role: 'viewer', version: 8 }));

        expect((await done).version).toBe(8);
        await reloads(['team-red', 'cowork-admins']);
      });

      it('puts the mapping as the answer has it into the list at once, in its place', async () => {
        const done = service.changeRole(mapping('team-red'), 'admin');
        write('PATCH', `${listUrl}/id-team-red`).flush(mapping('team-red', { role: 'admin' }));
        await done;

        expect(service.list().map((each) => [each.group, each.role])).toEqual([
          ['team-red', 'admin'],
          ['cowork-admins', 'member'],
        ]);
        await reloads(['team-red', 'cowork-admins']);
      });

      it("asks who is working again when the group is the person's own", async () => {
        const done = service.changeRole(mapping('team-red', { includes_caller: true }), 'viewer');
        write('PATCH', `${listUrl}/id-team-red`).flush(
          mapping('team-red', { role: 'viewer', includes_caller: true }),
        );
        await done;

        await reloads(['team-red', 'cowork-admins']);
        http.expectOne('/api/v1/me').flush(person());
        await settle();
      });

      it('does not put an answer into the list of a tenant the pages turned to meanwhile', async () => {
        const done = service.changeRole(mapping('team-red'), 'admin');
        const sent = write('PATCH', `${listUrl}/id-team-red`);
        session.enter('globex');
        TestBed.tick();
        page('globex').flush(pageOf(['team-red'], null));
        await settle();
        http
          .expectOne((request) => request.url === '/api/v1/tenants/globex/members')
          .flush(noMembers);
        await settle();

        sent.flush(mapping('team-red', { role: 'admin' }));
        await done;
        await settle();

        expect(service.list()[0].role).toBe('member');
        page('globex').flush(pageOf(['team-red'], null));
        http
          .expectOne((request) => request.url === '/api/v1/tenants/globex/members')
          .flush(noMembers);
      });

      it.each([
        [412, 'precondition_failed'],
        [409, 'last_admin'],
      ])('rejects with the HTTP error of a %i and loads nothing again', async (status, code) => {
        const outcome = rejection(service.changeRole(mapping('team-red'), 'viewer'));

        write('PATCH', `${listUrl}/id-team-red`).flush(
          { type: 'about:blank', title: 'Refused', status, code },
          { status, statusText: 'Refused' },
        );
        expect(((await outcome) as HttpErrorResponse).status).toBe(status);
        await settle();

        noLoad();
      });
    });

    describe('remove', () => {
      it('deletes the mapping, and loads the mappings and the members again', async () => {
        const done = service.remove(mapping('team-red'));

        write('DELETE', `${listUrl}/id-team-red`).flush(null, {
          status: 204,
          statusText: 'No Content',
        });
        await done;

        await reloads(['cowork-admins']);
        expect(groups()).toEqual(['cowork-admins']);
        http.expectNone('/api/v1/me');
      });

      it("asks who is working again when the group is the person's own", async () => {
        const done = service.remove(mapping('cowork-admins', { includes_caller: true }));
        write('DELETE', `${listUrl}/id-cowork-admins`).flush(null, {
          status: 204,
          statusText: 'No Content',
        });
        await done;

        await reloads(['team-red']);
        http.expectOne('/api/v1/me').flush(person());
        await settle();
      });

      it('rejects with the HTTP error of a refusal and loads nothing again', async () => {
        const outcome = rejection(service.remove(mapping('cowork-admins')));

        write('DELETE', `${listUrl}/id-cowork-admins`).flush(
          { type: 'about:blank', title: 'Conflict', status: 409, code: 'last_admin' },
          { status: 409, statusText: 'Conflict' },
        );
        expect(((await outcome) as HttpErrorResponse).status).toBe(409);
        await settle();

        noLoad();
      });
    });
  });
});
