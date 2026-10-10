import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Account, AccountList, Me, MemberList } from '../api/models';
import { AccountsService } from './accounts.service';
import { MembersService } from './members.service';
import { SessionService } from './session.service';

const account = (username: string, overrides: Partial<Account> = {}): Account => ({
  id: `0199aaaa-0000-7000-8000-${username.padStart(12, '0')}`,
  username,
  display_name: `Person ${username}`,
  role: 'member',
  locked: false,
  password_change_required: true,
  deactivated_at: null,
  created_at: '2026-10-01T10:00:00Z',
  ...overrides,
});

const pageOf = (usernames: string[], next: string | null): AccountList => ({
  items: usernames.map((username) => account(username)),
  next_cursor: next,
});

const person = (admin = true): Me => ({
  id: 'p1',
  display_name: 'Hans',
  username: 'hans',
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
    {
      team: { slug: 'globex', name: 'Globex' },
      tenant: { slug: 'globex', name: 'Globex' },
      role: 'admin',
      origins: [{ source: 'grant', role: 'admin' }],
      can_create_projects: true,
    },
    {
      team: { slug: 'initech', name: 'Initech' },
      tenant: { slug: 'initech', name: 'Initech' },
      role: 'viewer',
      origins: [{ source: 'grant', role: 'viewer' }],
      can_create_projects: false,
    },
  ],
});

const listUrl = '/api/v1/teams/acme/accounts';
const membersUrl = /^\/api\/v1\/teams\/[^/]+\/members$/;

const rejection = (promise: Promise<unknown>) =>
  promise.then(
    () => null,
    (error: unknown) => error,
  );

describe('AccountsService', () => {
  let service: AccountsService;
  let session: SessionService;
  let http: HttpTestingController;

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
  const usernames = () => service.list().map((entry) => entry.username);

  /** One page of the accounts of a tenant, with the cursor that asked for it. */
  const page = (tenant: string, cursor: string | null = null) =>
    http.expectOne(
      (request) =>
        request.method === 'GET' &&
        request.url === `/api/v1/teams/${tenant}/accounts` &&
        request.params.get('cursor') === cursor,
    );
  /** The page that the loader asks for once the previous one was taken, which is a promise away. */
  const nextPage = async (tenant: string, cursor: string) => {
    await settle();
    return page(tenant, cursor);
  };
  const noLoad = (tenant = 'acme') =>
    http.expectNone(
      (request) => request.method === 'GET' && request.url === `/api/v1/teams/${tenant}/accounts`,
    );

  async function start(me: Me = person()) {
    vi.useFakeTimers();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    service = TestBed.inject(AccountsService);
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

      expect(service.accounts.status()).toBe('idle');
      expect(service.list()).toEqual([]);
    });

    it('asks for nothing in a tenant the person does not administer, which would only be a 403', async () => {
      for (const tenant of ['initech', 'nowhere']) {
        session.enter(tenant);
        await settle();

        expect(service.accounts.status()).toBe('idle');
        noLoad(tenant);
      }
      expect(service.list()).toEqual([]);
    });

    describe('in a tenant the person administers', () => {
      beforeEach(() => {
        session.enter('acme');
        TestBed.tick();
      });

      it('asks for the first page, 200 at a time, at /api/v1 without a doubled slash', async () => {
        const first = page('acme');

        expect(first.request.url).toBe('/api/v1/teams/acme/accounts');
        expect(first.request.params.get('limit')).toBe('200');
        first.flush(pageOf([], null));
        await settle();
      });

      it('lists nobody until the first page arrives, and then the accounts of the page', async () => {
        const first = page('acme');

        expect(service.accounts.status()).toBe('loading');
        expect(service.list()).toEqual([]);

        first.flush(pageOf(['ada', 'sam'], null));
        await settle();
        expect(usernames()).toEqual(['ada', 'sam']);
      });

      it('follows the cursor to every page and lists the accounts in the order of the pages', async () => {
        page('acme').flush(pageOf(['a', 'b'], 'c1'));
        (await nextPage('acme', 'c1')).flush(pageOf(['c'], 'c2'));
        const last = await nextPage('acme', 'c2');
        expect(last.request.params.get('limit')).toBe('200');
        last.flush(pageOf(['d'], null));
        await settle();

        expect(usernames()).toEqual(['a', 'b', 'c', 'd']);
      });

      it('lists nobody until the last page has arrived', async () => {
        page('acme').flush(pageOf(['a'], 'c1'));
        const second = await nextPage('acme', 'c1');

        expect(service.accounts.status()).toBe('loading');
        expect(service.list()).toEqual([]);

        second.flush(pageOf(['b'], null));
        await settle();
        expect(usernames()).toEqual(['a', 'b']);
      });

      it('keeps the flags of each account as the API lists them', async () => {
        page('acme').flush({
          items: [
            account('ada', { locked: true, role: 'admin' }),
            account('sam', {
              password_change_required: false,
              deactivated_at: '2026-10-02T10:00:00Z',
            }),
          ],
          next_cursor: null,
        });
        await settle();

        expect(service.list().map((entry) => [entry.locked, entry.role])).toEqual([
          [true, 'admin'],
          [false, 'member'],
        ]);
        expect(service.list()[1].deactivated_at).toBe('2026-10-02T10:00:00Z');
      });

      it('lists nobody when a page fails, and says why', async () => {
        page('acme').flush(
          { type: 'about:blank', title: 'Forbidden', status: 403, code: 'forbidden' },
          { status: 403, statusText: 'Forbidden' },
        );
        await settle();

        expect(service.accounts.status()).toBe('error');
        expect(service.list()).toEqual([]);
      });

      it('keeps listing the accounts while it loads them again', async () => {
        page('acme').flush(pageOf(['a'], null));
        await settle();

        expect(service.accounts.reload()).toBe(true);
        TestBed.tick();
        const again = page('acme');

        expect(service.accounts.status()).toBe('reloading');
        expect(usernames()).toEqual(['a']);
        again.flush(pageOf(['a', 'b'], null));
        await settle();
        expect(usernames()).toEqual(['a', 'b']);
      });

      it('does not load them again when the person is loaded again with the same role', async () => {
        page('acme').flush(pageOf(['a'], null));
        await settle();

        session.me.reload();
        await settle();
        http.expectOne('/api/v1/me').flush(person());
        await settle();

        noLoad();
        expect(usernames()).toEqual(['a']);
      });

      it('stops asking when the person turns out not to administer the tenant any more', async () => {
        page('acme').flush(pageOf(['a'], null));
        await settle();

        session.me.reload();
        await settle();
        http.expectOne('/api/v1/me').flush(person(false));
        await settle();

        expect(service.accounts.status()).toBe('idle');
        expect(service.list()).toEqual([]);
        noLoad();
      });

      it('never lists a page of the old tenant that arrives after the tenant changed', async () => {
        page('acme').flush(pageOf(['a'], 'c1'));
        const late = await nextPage('acme', 'c1');

        session.enter('globex');
        TestBed.tick();
        page('globex').flush(pageOf(['g'], null));
        await settle();
        expect(usernames()).toEqual(['g']);

        late.flush(pageOf(['b'], null));
        await settle();

        expect(usernames()).toEqual(['g']);
      });

      it('drops the accounts of the old tenant at once, and none are listed on a person-level page', async () => {
        page('acme').flush(pageOf(['a'], null));
        await settle();

        session.enter(null);
        TestBed.tick();

        expect(service.accounts.status()).toBe('idle');
        expect(service.list()).toEqual([]);
      });
    });
  });

  describe('the acts', () => {
    /** What an act that fails answers, shaped as the API's problem details. */
    const refusal = (status: number, code: string) => ({
      body: { type: 'about:blank', title: 'Refused', status, code },
      init: { status, statusText: `Status ${status}` },
    });
    /** The act that is on its way, taken by its method and address. */
    const write = (method: string, url: string) =>
      http.expectOne((request) => request.method === method && request.url === url);
    const noContent = { status: 204, statusText: 'No Content' };
    /** The load of the members that a created account sets off: two, when nobody had asked for them before. */
    const flushMembers = async () => {
      for (let round = 0; round < 3; round++) {
        await settle();
        const asked = http.match(
          (request) => request.method === 'GET' && membersUrl.test(request.url),
        );
        if (asked.length === 0) {
          return;
        }
        asked.forEach((request) => request.flush({ items: [], next_cursor: null }));
      }
    };
    /** The reload of the list that follows an act, once the act's answer is taken. */
    const reload = async () => {
      await flushMembers();
      return page('acme');
    };

    beforeEach(async () => {
      await start();
      session.enter('acme');
      TestBed.tick();
      page('acme').flush(pageOf(['ada'], null));
      await settle();
    });

    describe('create', () => {
      const body = {
        username: 'sam',
        display_name: 'Sam Rivera',
        role: 'member' as const,
        temporary_password: 'a-temporary-one',
      };
      const key = '0199aaaa-1111-7000-8000-000000000001';
      const memberPage = (names: string[]): MemberList => ({
        items: names.map((name) => ({
          person: { id: `id-${name}`, display_name: name },
          role: 'member',
          origins: [{ source: 'grant', role: 'member' }],
          local: true,
          email: null,
        })),
        next_cursor: null,
      });
      const membersGet = () =>
        http.expectOne(
          (request) => request.method === 'GET' && request.url === '/api/v1/teams/acme/members',
        );

      it('posts the account to the accounts of the tenant and hands back the one that was made', async () => {
        const done = service.create(body, key);

        const sent = write('POST', listUrl);
        expect(sent.request.body).toEqual(body);
        expect(sent.request.headers.has('If-Match')).toBe(false);
        const made = account('sam');
        sent.flush(made);

        expect(await done).toEqual(made);
        (await reload()).flush(pageOf(['ada', 'sam'], null));
      });

      it('sends the key it is given, not one of its own (docs/adr/0045 D3)', async () => {
        const done = service.create(body, key);

        const sent = write('POST', listUrl);
        expect(sent.request.headers.get('Idempotency-Key')).toBe(key);
        sent.flush(account('sam'));
        await done;
        (await reload()).flush(pageOf(['ada', 'sam'], null));
      });

      it('sends the same key again for a retry of the same content after a network failure, and another one for other content', async () => {
        const lost = rejection(service.create(body, key));
        write('POST', listUrl).error(new ProgressEvent('error'));
        expect(((await lost) as HttpErrorResponse).status).toBe(0);
        await settle();
        noLoad();

        const retry = service.create(body, key);
        const again = write('POST', listUrl);
        expect(again.request.headers.get('Idempotency-Key')).toBe(key);
        again.flush(account('sam'));
        await retry;
        (await reload()).flush(pageOf(['ada', 'sam'], null));
        await settle();

        const other = '0199aaaa-1111-7000-8000-000000000002';
        const changed = service.create({ ...body, username: 'kim' }, other);
        const third = write('POST', listUrl);
        expect(third.request.headers.get('Idempotency-Key')).toBe(other);
        third.flush(account('kim'));
        await changed;
        (await reload()).flush(pageOf(['ada', 'sam', 'kim'], null));
      });

      it('loads the list again, so that the new account shows', async () => {
        const done = service.create(body, key);
        await settle();
        noLoad();
        write('POST', listUrl).flush(account('sam'));
        await done;

        const again = await reload();
        expect(service.accounts.status()).toBe('reloading');
        again.flush(pageOf(['ada', 'sam'], null));
        await settle();

        expect(usernames()).toEqual(['ada', 'sam']);
      });

      it('writes to the tenant that is entered', async () => {
        session.enter('globex');
        await settle();
        page('globex').flush(pageOf(['g'], null));
        await settle();

        const done = service.create(body, key);
        write('POST', '/api/v1/teams/globex/accounts').flush(account('sam'));
        await done;

        await flushMembers();
        page('globex').flush(pageOf(['g', 'sam'], null));
      });

      it.each([
        [409, 'username_taken'],
        [422, 'validation_failed'],
        [403, 'forbidden'],
        [500, 'internal'],
      ])(
        'rejects with the HTTP error of a %i and loads neither the list nor the members again',
        async (status, code) => {
          const outcome = rejection(service.create(body, key));

          write('POST', listUrl).flush(refusal(status, code).body, refusal(status, code).init);
          const error = await outcome;
          await settle();

          expect(error).toBeInstanceOf(HttpErrorResponse);
          expect((error as HttpErrorResponse).status).toBe(status);
          noLoad();
          http.expectNone((request) => membersUrl.test(request.url));
          expect(usernames()).toEqual(['ada']);
        },
      );

      describe('the members of the tenant', () => {
        const members = () => {
          const service = TestBed.inject(MembersService);
          TestBed.tick();
          return service;
        };
        const names = (service: MembersService) =>
          service.list().map((member) => member.person.display_name);

        it('are not asked for while the accounts are listed, so the page of the accounts starts no load of them', async () => {
          await settle();

          http.expectNone((request) => membersUrl.test(request.url));
        });

        it('are loaded again once the account is made, because the account is a member from then on', async () => {
          const held = members();
          membersGet().flush(memberPage(['ada']));
          await settle();
          expect(names(held)).toEqual(['ada']);

          const done = service.create(body, key);
          write('POST', listUrl).flush(account('sam'));
          await done;
          await settle();

          const reloaded = membersGet();
          expect(held.members.status()).toBe('reloading');
          expect(names(held)).toEqual(['ada']);
          reloaded.flush(memberPage(['ada', 'sam']));
          await settle();
          expect(names(held)).toEqual(['ada', 'sam']);
          page('acme').flush(pageOf(['ada', 'sam'], null));
        });

        it('are loaded again only after the answer, not before it', async () => {
          const held = members();
          membersGet().flush(memberPage(['ada']));
          await settle();

          const done = service.create(body, key);
          await settle();
          http.expectNone((request) => membersUrl.test(request.url));
          write('POST', listUrl).flush(account('sam'));
          await done;
          await settle();

          membersGet().flush(memberPage(['ada', 'sam']));
          await settle();
          expect(names(held)).toEqual(['ada', 'sam']);
          page('acme').flush(pageOf(['ada', 'sam'], null));
        });

        it('are loaded once more when the load that was under way ends, because its answer may predate the account', async () => {
          const held = members();
          const inFlight = membersGet();

          const done = service.create(body, key);
          write('POST', listUrl).flush(account('sam'));
          await done;
          await settle();
          inFlight.flush(memberPage(['ada']));
          await settle();

          membersGet().flush(memberPage(['ada', 'sam']));
          await settle();
          expect(names(held)).toEqual(['ada', 'sam']);
          page('acme').flush(pageOf(['ada', 'sam'], null));
        });
      });
    });

    describe('reset', () => {
      it('puts the temporary password to the account, and nothing else', async () => {
        const done = service.reset('sam', 'a-new-temporary-one');

        const sent = write('PUT', `${listUrl}/sam/password`);
        expect(sent.request.body).toEqual({ temporary_password: 'a-new-temporary-one' });
        expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
        expect(sent.request.headers.has('If-Match')).toBe(false);
        sent.flush(null, noContent);

        await done;
        (await reload()).flush(pageOf(['ada'], null));
      });

      it('loads no members, because nobody joined', async () => {
        const done = service.reset('sam', 'a-new-temporary-one');
        write('PUT', `${listUrl}/sam/password`).flush(null, noContent);
        await done;
        await settle();

        http.expectNone((request) => membersUrl.test(request.url));
        page('acme').flush(pageOf(['ada'], null));
      });

      it('loads the list again, so that the account shows as having to change its password', async () => {
        const done = service.reset('ada', 'a-new-temporary-one');
        write('PUT', `${listUrl}/ada/password`).flush(null, noContent);
        await done;

        (await reload()).flush({
          items: [account('ada', { password_change_required: true })],
          next_cursor: null,
        });
        await settle();

        expect(service.list()[0].password_change_required).toBe(true);
      });

      it.each([
        [422, 'validation_failed'],
        [404, 'not_found'],
        [403, 'forbidden'],
      ])(
        'rejects with the HTTP error of a %i and does not load the list again',
        async (status, code) => {
          const outcome = rejection(service.reset('sam', 'short'));

          write('PUT', `${listUrl}/sam/password`).flush(
            refusal(status, code).body,
            refusal(status, code).init,
          );
          const error = await outcome;
          await settle();

          expect((error as HttpErrorResponse).status).toBe(status);
          noLoad();
        },
      );
    });

    describe.each([
      ['unlock', 'DELETE', 'lockout', (s: AccountsService) => s.unlock('sam')],
      ['deactivate', 'PUT', 'deactivation', (s: AccountsService) => s.deactivate('sam')],
      ['endSessions', 'DELETE', 'sessions', (s: AccountsService) => s.endSessions('sam')],
    ] as const)('%s', (_act, method, path, run) => {
      it(`sends ${method} ${path} for the account by its username, with no body and no key`, async () => {
        const done = run(service);

        const sent = write(method, `${listUrl}/sam/${path}`);
        expect(sent.request.body).toBeNull();
        expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
        expect(sent.request.headers.has('If-Match')).toBe(false);
        sent.flush(null, noContent);

        await done;
        (await reload()).flush(pageOf(['ada'], null));
      });

      it('loads no members, because nobody joined', async () => {
        const done = run(service);
        write(method, `${listUrl}/sam/${path}`).flush(null, noContent);
        await done;
        await settle();

        http.expectNone((request) => membersUrl.test(request.url));
        page('acme').flush(pageOf(['ada'], null));
      });

      it('writes to the tenant that is entered', async () => {
        session.enter('globex');
        await settle();
        page('globex').flush(pageOf(['g'], null));
        await settle();

        const done = run(service);
        write(method, `/api/v1/teams/globex/accounts/sam/${path}`).flush(null, noContent);
        await done;

        await settle();
        page('globex').flush(pageOf(['g'], null));
      });

      it('loads the list again after the answer, so that the account shows what it did', async () => {
        const done = run(service);
        await settle();
        noLoad();
        write(method, `${listUrl}/sam/${path}`).flush(null, noContent);
        await done;

        const again = await reload();
        expect(service.accounts.status()).toBe('reloading');
        again.flush(pageOf(['ada', 'sam'], null));
        await settle();
        expect(usernames()).toEqual(['ada', 'sam']);
      });

      it.each([
        [404, 'not_found'],
        [403, 'forbidden'],
      ])(
        'rejects with the HTTP error of a %i and does not load the list again',
        async (status, code) => {
          const outcome = rejection(run(service));

          write(method, `${listUrl}/sam/${path}`).flush(
            refusal(status, code).body,
            refusal(status, code).init,
          );
          const error = await outcome;
          await settle();

          expect((error as HttpErrorResponse).status).toBe(status);
          noLoad();
        },
      );
    });

    // An act that ends during a load of the list — a second act in quick succession, or a slow first
    // load — must show itself: the answer on its way was asked for before the act, so the list loads
    // once more when that load ends (core/refresh.ts).
    describe.each([
      [
        'creating',
        'POST',
        listUrl,
        (s: AccountsService) =>
          s.create(
            {
              username: 'sam',
              display_name: 'Sam',
              role: 'member',
              temporary_password: 'x'.repeat(12),
            },
            '0199aaaa-1111-7000-8000-000000000003',
          ),
      ],
      [
        'resetting',
        'PUT',
        `${listUrl}/sam/password`,
        (s: AccountsService) => s.reset('sam', 'x'.repeat(12)),
      ],
      ['unlocking', 'DELETE', `${listUrl}/sam/lockout`, (s: AccountsService) => s.unlock('sam')],
      [
        'deactivating',
        'PUT',
        `${listUrl}/sam/deactivation`,
        (s: AccountsService) => s.deactivate('sam'),
      ],
      [
        'ending the sessions of',
        'DELETE',
        `${listUrl}/sam/sessions`,
        (s: AccountsService) => s.endSessions('sam'),
      ],
    ])('%s an account, when it ends while the list is loading', (_act, method, url, run) => {
      it('loads the list once more when that load ends, because its answer may predate the act', async () => {
        service.accounts.reload();
        await settle();
        const inFlight = page('acme');

        const done = run(service);
        write(method, url).flush(account('sam'));
        await done;
        await flushMembers();
        inFlight.flush(pageOf(['ada'], null));
        await settle();

        page('acme').flush(pageOf(['ada', 'sam'], null));
      });
    });
  });
});
