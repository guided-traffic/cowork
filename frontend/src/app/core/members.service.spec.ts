import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import {
  HttpTestingController,
  provideHttpClientTesting,
  TestRequest,
} from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Subject } from 'rxjs';
import { Me, Member, MemberList } from '../api/models';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { MembersService } from './members.service';
import { SessionService } from './session.service';

const idOf = (name: string) => `0199aaaa-0000-7000-8000-${name.padStart(12, '0')}`;

const member = (name: string, role: Member['role'] = 'member'): Member => ({
  person: { id: idOf(name), display_name: name },
  role,
  origins: [{ source: 'grant', role }],
  local: false,
  email: null,
});

/** Hans, who works here: the person whose own grant may change. */
const hans: Me = {
  id: idOf('Hans'),
  display_name: 'Hans',
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships: [],
};

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

const pageOf = (names: string[], next: string | null): MemberList => ({
  items: names.map((name) => member(name)),
  next_cursor: next,
});

describe('MembersService', () => {
  let service: MembersService;
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
  const names = () => service.list().map((entry) => entry.person.display_name);

  /** One page of the members of a tenant, with the cursor that asked for it. */
  const page = (tenant: string, cursor: string | null = null) =>
    http.expectOne(
      (request) =>
        request.url === `/api/v1/tenants/${tenant}/members` &&
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
    service = TestBed.inject(MembersService);
    session = TestBed.inject(SessionService);
    http = TestBed.inject(HttpTestingController);
    TestBed.tick();
    http.expectOne('/api/v1/me').flush(hans);
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

    expect(service.members.status()).toBe('idle');
    expect(service.list()).toEqual([]);
  });

  describe('in a tenant', () => {
    beforeEach(() => {
      session.enter('acme');
      TestBed.tick();
    });

    it('asks for the first page, 200 at a time', async () => {
      const first = page('acme');

      expect(first.request.method).toBe('GET');
      expect(first.request.url).toBe('/api/v1/tenants/acme/members');
      expect(first.request.params.get('limit')).toBe('200');
      first.flush(pageOf([], null));
      await settle();
    });

    it('lists nobody until the first page arrives', async () => {
      const first = page('acme');

      expect(service.members.status()).toBe('loading');
      expect(service.list()).toEqual([]);

      first.flush(pageOf(['Ada'], null));
      await settle();
      expect(names()).toEqual(['Ada']);
    });

    it('keeps each member with the role in the tenant', async () => {
      page('acme').flush({
        items: [member('Ada', 'admin'), member('Bob', 'viewer')],
        next_cursor: null,
      });
      await settle();

      expect(service.list().map((entry) => [entry.person.display_name, entry.role])).toEqual([
        ['Ada', 'admin'],
        ['Bob', 'viewer'],
      ]);
    });

    it('follows the cursor to every page and lists the members in the order of the pages', async () => {
      page('acme').flush(pageOf(['Ada', 'Bob'], 'c1'));
      (await nextPage('acme', 'c1')).flush(pageOf(['Cy'], 'c2'));
      const last = await nextPage('acme', 'c2');
      expect(last.request.params.get('limit')).toBe('200');
      last.flush(pageOf(['Di'], null));
      await settle();

      expect(names()).toEqual(['Ada', 'Bob', 'Cy', 'Di']);
    });

    it('lists nobody until the last page has arrived', async () => {
      page('acme').flush(pageOf(['Ada'], 'c1'));
      const second = await nextPage('acme', 'c1');

      expect(service.members.status()).toBe('loading');
      expect(service.list()).toEqual([]);

      second.flush(pageOf(['Bob'], null));
      await settle();
      expect(names()).toEqual(['Ada', 'Bob']);
    });

    it('lists nobody when a page fails, and says why', async () => {
      page('acme').flush(
        { type: 'about:blank', title: 'Forbidden', status: 403, code: 'forbidden' },
        { status: 403, statusText: 'Forbidden' },
      );
      await settle();

      expect(service.members.status()).toBe('error');
      expect(service.list()).toEqual([]);
    });

    it('never lists a page of the old tenant that arrives after the tenant changed', async () => {
      page('acme').flush(pageOf(['Ada'], 'c1'));
      const late = await nextPage('acme', 'c1');

      session.enter('globex');
      TestBed.tick();
      page('globex').flush(pageOf(['Gus'], null));
      await settle();
      expect(names()).toEqual(['Gus']);

      late.flush(pageOf(['Bob'], null));
      await settle();

      expect(names()).toEqual(['Gus']);
    });
  });

  describe('when the tenant changes', () => {
    beforeEach(async () => {
      session.enter('acme');
      TestBed.tick();
      page('acme').flush(pageOf(['Ada'], null));
      await settle();
    });

    it('drops the members of the old tenant at once and lists those of the new one when they arrive', async () => {
      session.enter('globex');
      TestBed.tick();
      const globex = page('globex');

      expect(service.list()).toEqual([]);

      globex.flush(pageOf(['Gus'], null));
      await settle();
      expect(names()).toEqual(['Gus']);
    });

    it('lists nobody on a person-level page', () => {
      session.enter(null);
      TestBed.tick();

      expect(service.members.status()).toBe('idle');
      expect(service.list()).toEqual([]);
    });
  });

  describe('when the memberships change (docs/adr/0054 D2)', () => {
    beforeEach(async () => {
      session.enter('acme');
      TestBed.tick();
      page('acme').flush(pageOf(['Ada'], null));
      await settle();
    });

    it.each<StreamEvent>([
      { name: 'membership.changed', id: 'e1', personId: 'p9' },
      { name: 'membership.changed', id: 'e1', mappingId: 'm1' },
      { name: 'membership.changed', id: 'e1', personId: 'p9', projectId: 'j1' },
      { name: 'membership.changed', id: 'e1', tenant: 'acme', personId: 'p9' },
      { name: 'resync' },
      { name: 'poll' },
    ])('loads the members again on %j', async (event) => {
      stream.next(event);
      await settle();

      page('acme').flush(pageOf(['Ada', 'Bob'], null));
      // Who is working is asked again too, by the session.
      http.expectOne('/api/v1/me').flush(hans);
      await settle();

      expect(names()).toEqual(['Ada', 'Bob']);
    });

    it("leaves the members alone on an act of another of the person's tenants, which the person-level stream carries (docs/adr/0054 D1)", async () => {
      stream.next({ name: 'membership.changed', id: 'e1', tenant: 'beta', personId: 'p9' });
      await settle();

      http.expectNone((request) => request.url === '/api/v1/tenants/acme/members');
      http.expectNone('/api/v1/me');
    });

    it("sends the list's weak ETag on a poll and keeps the members on a 304 (docs/adr/0054 D7)", async () => {
      stream.next({ name: 'resync' });
      await settle();
      page('acme').flush(pageOf(['Ada', 'Bob'], null), { headers: { ETag: 'W/"one"' } });
      http.expectOne('/api/v1/me').flush(hans);
      await settle();

      stream.next({ name: 'poll' });
      await settle();
      const again = page('acme');
      expect(again.request.headers.get('If-None-Match')).toBe('W/"one"');
      again.flush(null, { status: 304, statusText: 'Not Modified' });
      http.expectOne('/api/v1/me').flush(hans);
      await settle();

      expect(service.members.status()).toBe('resolved');
      expect(names()).toEqual(['Ada', 'Bob']);
    });

    it('leaves the members alone on an event that names a ticket', async () => {
      stream.next({ name: 'ticket.changed', id: 'e1', key: 'acme/VKO-1', version: 2, kind: 'x' });
      await settle();

      http.expectNone((request) => request.url === '/api/v1/tenants/acme/members');
    });

    it.each([0, 500, 503])(
      'keeps listing the members when loading them again fails with a status of %i',
      async (status) => {
        stream.next({ name: 'poll' });
        await settle();

        fail(page('acme'), status);
        http.expectOne('/api/v1/me').flush(hans);
        await settle();

        expect(service.members.status()).toBe('resolved');
        expect(names()).toEqual(['Ada']);
      },
    );

    it.each([401, 403, 404])(
      'lists nobody when loading them again answers %i: they are gone for the person',
      async (status) => {
        stream.next({ name: 'poll' });
        await settle();

        fail(page('acme'), status);
        http.expectOne('/api/v1/me').flush(hans);
        await settle();

        expect(service.members.status()).toBe('error');
        expect(service.list()).toEqual([]);
      },
    );
  });

  describe('the grants (docs/adr/0030 D3)', () => {
    const membersUrl = '/api/v1/tenants/acme/members';
    const grantUrl = (name: string) => `${membersUrl}/${idOf(name)}/grant`;
    const write = (method: string, url: string) =>
      http.expectOne((request) => request.method === method && request.url === url);
    /** The reload of the list that follows a write, once the write's answer is taken. */
    const reload = async () => {
      await settle();
      return page('acme');
    };
    const meAgain = async () => {
      await settle();
      return http.expectOne('/api/v1/me');
    };

    beforeEach(async () => {
      session.enter('acme');
      TestBed.tick();
      page('acme').flush(pageOf(['Ada', 'Bob'], null));
      await settle();
    });

    describe('add', () => {
      it('posts the e-mail address or username and the role, with the key of the form (docs/adr/0045)', async () => {
        const done = service.add('cyd@example.com', 'viewer', 'key-1');

        const sent = write('POST', membersUrl);
        expect(sent.request.body).toEqual({ person: 'cyd@example.com', role: 'viewer' });
        expect(sent.request.headers.get('Idempotency-Key')).toBe('key-1');
        const cyd = member('Cyd', 'viewer');
        sent.flush(cyd, { status: 201, statusText: 'Created' });

        expect(await done).toEqual(cyd);
        (await reload()).flush(pageOf(['Ada', 'Bob', 'Cyd'], null));
      });

      it('lists the new member at once, and loads the list again', async () => {
        const done = service.add('cyd@example.com', 'viewer', 'key-1');
        write('POST', membersUrl).flush(member('Cyd', 'viewer'));
        await done;

        expect(names()).toEqual(['Ada', 'Bob', 'Cyd']);
        const again = await reload();
        again.flush(pageOf(['Ada', 'Bob', 'Cyd'], null));
        await settle();
        expect(names()).toEqual(['Ada', 'Bob', 'Cyd']);
      });

      it('rejects with the HTTP error of a refusal and changes nothing', async () => {
        const outcome = rejection(service.add('nobody@example.com', 'member', 'key-1'));

        write('POST', membersUrl).flush(
          { type: 'about:blank', title: 'Not found', status: 404, code: 'person_not_found' },
          { status: 404, statusText: 'Not Found' },
        );
        const error = await outcome;
        await settle();

        expect((error as HttpErrorResponse).status).toBe(404);
        expect(names()).toEqual(['Ada', 'Bob']);
        http.expectNone((request) => request.url === membersUrl && request.method === 'GET');
      });
    });

    describe('setGrant', () => {
      it('puts the role to the grant of the member, without a precondition (docs/adr/0045 D1)', async () => {
        const done = service.setGrant(idOf('Bob'), 'admin');

        const sent = write('PUT', grantUrl('Bob'));
        expect(sent.request.body).toEqual({ role: 'admin' });
        expect(sent.request.headers.has('If-Match')).toBe(false);
        sent.flush(member('Bob', 'admin'));

        expect((await done).role).toBe('admin');
        (await reload()).flush(pageOf(['Ada', 'Bob'], null));
      });

      it('puts the member as the answer has them into the list at once, in their place', async () => {
        const done = service.setGrant(idOf('Ada'), 'viewer');
        write('PUT', grantUrl('Ada')).flush(member('Ada', 'viewer'));
        await done;

        expect(service.list().map((entry) => [entry.person.display_name, entry.role])).toEqual([
          ['Ada', 'viewer'],
          ['Bob', 'member'],
        ]);
        (await reload()).flush(pageOf(['Ada', 'Bob'], null));
      });

      it('does not ask who is working again for somebody else', async () => {
        const done = service.setGrant(idOf('Bob'), 'admin');
        write('PUT', grantUrl('Bob')).flush(member('Bob', 'admin'));
        await done;
        (await reload()).flush(pageOf(['Ada', 'Bob'], null));
        await settle();

        http.expectNone('/api/v1/me');
      });

      it("asks who is working again when the grant is the person's own, so that the pages follow the new role", async () => {
        const done = service.setGrant(hans.id, 'viewer');
        write('PUT', `${membersUrl}/${hans.id}/grant`).flush(member('Hans', 'viewer'));
        await done;

        (await meAgain()).flush(hans);
        page('acme').flush(pageOf(['Ada', 'Bob', 'Hans'], null));
        await settle();
      });

      it('does not put an answer into the list of a tenant the pages turned to meanwhile', async () => {
        const done = service.setGrant(idOf('Bob'), 'admin');
        const sent = write('PUT', grantUrl('Bob'));
        session.enter('globex');
        TestBed.tick();
        page('globex').flush(pageOf(['Gus'], null));
        await settle();

        sent.flush(member('Bob', 'admin'));
        await done;
        await settle();

        expect(names()).toEqual(['Gus']);
        page('globex').flush(pageOf(['Gus'], null));
      });

      it('rejects with the HTTP error of a refusal and leaves the list as it was', async () => {
        const outcome = rejection(service.setGrant(idOf('Ada'), 'member'));

        write('PUT', grantUrl('Ada')).flush(
          { type: 'about:blank', title: 'Conflict', status: 409, code: 'last_admin' },
          { status: 409, statusText: 'Conflict' },
        );
        const error = await outcome;
        await settle();

        expect((error as HttpErrorResponse).status).toBe(409);
        expect(service.list()[0].role).toBe('member');
        http.expectNone((request) => request.url === membersUrl && request.method === 'GET');
      });
    });

    describe('removeGrant', () => {
      it('deletes the grant of the member and loads the list again', async () => {
        const done = service.removeGrant(idOf('Bob'));

        write('DELETE', grantUrl('Bob')).flush(null, { status: 204, statusText: 'No Content' });
        await done;

        (await reload()).flush(pageOf(['Ada'], null));
        await settle();
        expect(names()).toEqual(['Ada']);
      });

      it("asks who is working again when the grant is the person's own", async () => {
        const done = service.removeGrant(hans.id);
        write('DELETE', `${membersUrl}/${hans.id}/grant`).flush(null, {
          status: 204,
          statusText: 'No Content',
        });
        await done;

        (await meAgain()).flush(hans);
        page('acme').flush(pageOf(['Ada', 'Bob'], null));
        await settle();
      });

      it('rejects with the HTTP error of a refusal and does not load the list again', async () => {
        const outcome = rejection(service.removeGrant(idOf('Ada')));

        write('DELETE', grantUrl('Ada')).flush(
          { type: 'about:blank', title: 'Conflict', status: 409, code: 'last_admin' },
          { status: 409, statusText: 'Conflict' },
        );
        expect(((await outcome) as HttpErrorResponse).status).toBe(409);
        await settle();

        http.expectNone((request) => request.url === membersUrl && request.method === 'GET');
      });
    });
  });
});
