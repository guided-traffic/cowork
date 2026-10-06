import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import {
  HttpTestingController,
  provideHttpClientTesting,
  TestRequest,
} from '@angular/common/http/testing';
import { effect } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Subject } from 'rxjs';
import type { MockInstance } from 'vitest';
import { Me, Membership } from '../api/models';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { RELOAD } from './hard-navigation';
import { SESSION_CHANNEL, SessionChannel, SessionService } from './session.service';

const asAdmin: Membership = {
  role: 'admin',
  tenant: { slug: 'acme', name: 'Acme Corp' },
  origins: [{ source: 'grant', role: 'admin' }],
};
const asMember: Membership = {
  role: 'member',
  tenant: { slug: 'globex', name: 'Globex' },
  origins: [{ source: 'grant', role: 'member' }],
};

/** Fails a request: without an answer at all (status 0), or with a problem of the status. */
const fail = (request: TestRequest, status: number) =>
  status === 0
    ? request.error(new ProgressEvent('error'))
    : request.flush(
        { type: 'about:blank', title: 'Refused', status, code: 'internal' },
        { status, statusText: `Status ${status}` },
      );

/** The other tabs as a test plays them: what this one told them, and a way to tell it something. */
class FakeChannel implements SessionChannel {
  readonly told: unknown[] = [];
  private listener: ((event: MessageEvent) => void) | undefined;

  postMessage(message: unknown): void {
    this.told.push(message);
  }

  addEventListener(_type: 'message', listener: (event: MessageEvent) => void): void {
    this.listener = listener;
  }

  /** Another tab says something. */
  say(data: unknown): void {
    this.listener?.(new MessageEvent('message', { data }));
  }
}

const hansId = '0199aaaa-0000-7000-8000-000000000001';

const person = (memberships: Membership[]): Me => ({
  id: hansId,
  display_name: 'Hans',
  username: 'local:hans',
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships,
});

describe('SessionService', () => {
  let service: SessionService;
  let http: HttpTestingController;
  let stream: Subject<StreamEvent>;
  let channel: FakeChannel;
  let reload: MockInstance<() => void>;

  /** Runs the effects, which starts the load, and hands out the request of GET /api/v1/me. */
  const meRequest = () => {
    TestBed.tick();
    return http.expectOne('/api/v1/me');
  };
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

  const load = async (body: Me) => {
    meRequest().flush(body);
    await settle();
  };

  beforeEach(() => {
    vi.useFakeTimers();
    stream = new Subject<StreamEvent>();
    channel = new FakeChannel();
    reload = vi.fn<() => void>();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
        { provide: SESSION_CHANNEL, useValue: channel },
        { provide: RELOAD, useValue: reload },
      ],
    });
    service = TestBed.inject(SessionService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      vi.useRealTimers();
      TestBed.resetTestingModule();
    }
  });

  // The generated paths start with /api/v1. The default root URL is '/', which would make the
  // request go to //api/v1/me; the application provides an empty one (app.config.ts), as this bed does.
  it('asks for the person at /api/v1/me, not at //api/v1/me', async () => {
    const request = meRequest();

    expect(request.request.method).toBe('GET');
    expect(request.request.url).toBe('/api/v1/me');
    expect(request.request.url.startsWith('//')).toBe(false);
    request.flush(person([]));
    await settle();
  });

  describe('before the answer', () => {
    it('knows nobody and no tenant', async () => {
      const request = meRequest();

      expect(service.me.status()).toBe('loading');
      expect(service.person()).toBeUndefined();
      expect(service.memberships()).toEqual([]);
      expect(service.tenant()).toBeNull();
      expect(service.membership()).toBeUndefined();
      expect(service.soleTenant()).toBeNull();

      request.flush(person([asAdmin]));
      await settle();
    });
  });

  describe('once the person is loaded', () => {
    beforeEach(() => load(person([asAdmin, asMember])));

    it('shows the person and every membership', () => {
      expect(service.person()?.display_name).toBe('Hans');
      expect(service.memberships()).toEqual([asAdmin, asMember]);
    });

    it('has no membership while no tenant is entered', () => {
      expect(service.tenant()).toBeNull();
      expect(service.membership()).toBeUndefined();
    });

    it('picks the membership of the tenant that is entered', () => {
      service.enter('acme');
      expect(service.membership()).toBe(asAdmin);

      service.enter('globex');
      expect(service.membership()).toBe(asMember);
    });

    it('has no membership in a tenant the person does not belong to', () => {
      service.enter('initech');

      expect(service.tenant()).toBe('initech');
      expect(service.membership()).toBeUndefined();
    });

    it('leaves the tenant on a person-level page', () => {
      service.enter('acme');

      service.enter(null);

      expect(service.tenant()).toBeNull();
      expect(service.membership()).toBeUndefined();
    });

    it('does not name a sole tenant when there are two', () => {
      expect(service.soleTenant()).toBeNull();
    });

    it('does not tell its dependents about entering the tenant it is in', () => {
      const seen: (string | null)[] = [];
      TestBed.runInInjectionContext(() => effect(() => seen.push(service.tenant())));
      TestBed.tick();

      service.enter('acme');
      TestBed.tick();
      service.enter('acme');
      TestBed.tick();

      expect(seen).toEqual([null, 'acme']);
    });

    it('keeps showing the person while it asks again, so that a login does not flash', async () => {
      expect(service.me.reload()).toBe(true);
      TestBed.tick();
      const again = http.expectOne('/api/v1/me');

      expect(service.me.status()).toBe('reloading');
      expect(service.person()?.display_name).toBe('Hans');

      again.flush({ ...person([asMember]), display_name: 'Hans F.' });
      await settle();

      expect(service.person()?.display_name).toBe('Hans F.');
      expect(service.memberships()).toEqual([asMember]);
    });
  });

  describe('a person who is no global administrator', () => {
    beforeEach(() => load(person([asAdmin])));

    it("never asks for the installation's tenants, which would be a 403", () => {
      http.expectNone((request) => request.url === '/api/v1/tenants');
      expect(service.tenants()).toEqual([{ slug: 'acme', name: 'Acme Corp', role: 'admin' }]);
    });

    it('works in the tenant the pages show and oversees none, not even one they are not in', () => {
      service.enter('acme');
      expect(service.oversight()).toBe(false);
      expect(service.mayGrantSelf()).toBe(false);
      expect(service.workTenant()).toBe('acme');

      service.enter('initech');
      expect(service.oversight()).toBe(false);
      expect(service.mayGrantSelf()).toBe(false);
      expect(service.workTenant()).toBe('initech');
    });
  });

  describe('a global administrator (docs/adr/0034 D2)', () => {
    const administrator = (memberships: Membership[]): Me => ({
      ...person(memberships),
      global_admin: true,
    });
    const tenantsRequest = () =>
      http.expectOne((request) => request.method === 'GET' && request.url === '/api/v1/tenants');

    it("names no sole tenant before the installation's tenants are known", async () => {
      await load(administrator([asAdmin]));

      expect(service.soleTenant()).toBeNull();
      tenantsRequest().flush({
        items: [
          { slug: 'acme', name: 'Acme Corp', role: 'admin' },
          { slug: 'initech', name: 'Initech', role: null },
        ],
        next_cursor: null,
      });
      await settle();

      expect(service.soleTenant()).toBeNull();
    });

    it('lists every tenant, every page, the ones without a role among them, by slug', async () => {
      await load(administrator([asMember]));
      const first = tenantsRequest();
      expect(first.request.params.get('limit')).toBe('200');
      first.flush({
        items: [
          { slug: 'acme', name: 'Acme Corp', role: null },
          { slug: 'globex', name: 'Globex', role: 'member' },
        ],
        next_cursor: 'c1',
      });
      await settle();
      const second = tenantsRequest();
      expect(second.request.params.get('cursor')).toBe('c1');
      second.flush({
        items: [{ slug: 'initech', name: 'Initech', role: null }],
        next_cursor: null,
      });
      await settle();

      expect(service.tenants()).toEqual([
        { slug: 'acme', name: 'Acme Corp', role: null },
        { slug: 'globex', name: 'Globex', role: 'member' },
        { slug: 'initech', name: 'Initech', role: null },
      ]);
    });

    it('takes the roles from the memberships, which follow the grants, not from the list', async () => {
      await load(administrator([]));
      tenantsRequest().flush({
        items: [{ slug: 'acme', name: 'Acme Corp', role: null }],
        next_cursor: null,
      });
      await settle();
      expect(service.soleTenant()).toBe('acme');

      service.me.reload();
      await settle();
      http.expectOne('/api/v1/me').flush(administrator([asAdmin]));
      await settle();

      http.expectNone((request) => request.url === '/api/v1/tenants');
      expect(service.tenants()).toEqual([{ slug: 'acme', name: 'Acme Corp', role: 'admin' }]);
    });

    it('oversees a tenant without a role in it, where the work of the tenant is not followed', async () => {
      await load(administrator([asMember]));
      tenantsRequest().flush({
        items: [
          { slug: 'acme', name: 'Acme Corp', role: null },
          { slug: 'globex', name: 'Globex', role: 'member' },
        ],
        next_cursor: null,
      });
      await settle();

      service.enter('acme');
      expect(service.oversight()).toBe(true);
      expect(service.workTenant()).toBeNull();
      expect(service.shown()).toEqual({ slug: 'acme', name: 'Acme Corp', role: null });

      service.enter('globex');
      expect(service.oversight()).toBe(false);
      expect(service.workTenant()).toBe('globex');

      service.enter(null);
      expect(service.oversight()).toBe(false);
      expect(service.workTenant()).toBeNull();
    });

    it('may set their own grant where they do not hold admin, and nowhere else', async () => {
      await load(administrator([asAdmin, asMember]));
      tenantsRequest().flush({
        items: [
          { slug: 'acme', name: 'Acme Corp', role: 'admin' },
          { slug: 'globex', name: 'Globex', role: 'member' },
          { slug: 'initech', name: 'Initech', role: null },
        ],
        next_cursor: null,
      });
      await settle();

      expect(service.mayGrantSelf()).toBe(false);
      service.enter('acme');
      expect(service.mayGrantSelf()).toBe(false);
      service.enter('globex');
      expect(service.mayGrantSelf()).toBe(true);
      expect(service.oversight()).toBe(false);
      service.enter('initech');
      expect(service.mayGrantSelf()).toBe(true);
      expect(service.oversight()).toBe(true);
    });

    it('works in the tenant once a grant to themselves is in the memberships', async () => {
      await load(administrator([]));
      tenantsRequest().flush({
        items: [{ slug: 'acme', name: 'Acme Corp', role: null }],
        next_cursor: null,
      });
      await settle();
      service.enter('acme');
      expect(service.oversight()).toBe(true);

      service.me.reload();
      await settle();
      http.expectOne('/api/v1/me').flush(administrator([asAdmin]));
      await settle();

      expect(service.oversight()).toBe(false);
      expect(service.workTenant()).toBe('acme');
    });

    it('oversees nothing before the person has answered', async () => {
      service.enter('acme');
      const request = meRequest();

      expect(service.oversight()).toBe(false);
      request.flush(administrator([]));
      await settle();
      tenantsRequest().flush({ items: [], next_cursor: null });
      await settle();
      expect(service.oversight()).toBe(true);
    });
  });

  describe('when the memberships change (docs/adr/0054 D2)', () => {
    beforeEach(() => load(person([asAdmin, asMember])));

    it.each<StreamEvent>([
      { name: 'membership.changed', id: 'e1', personId: 'p1' },
      { name: 'membership.changed', id: 'e1', mappingId: 'm1' },
      { name: 'membership.changed', id: 'e1', projectId: 'j1' },
      { name: 'resync' },
      { name: 'poll' },
    ])(
      'asks who is working again on %j, so that a new role shows without a reload',
      async (event) => {
        stream.next(event);
        await settle();

        http.expectOne('/api/v1/me').flush(person([{ ...asAdmin, role: 'member' }]));
        await settle();

        expect(service.memberships()[0].role).toBe('member');
      },
    );

    it('asks once more after a burst of events, not once for each', async () => {
      stream.next({ name: 'membership.changed', id: 'e1', personId: 'p1' });
      await settle();
      const first = http.expectOne('/api/v1/me');
      stream.next({ name: 'membership.changed', id: 'e2', personId: 'p2' });
      stream.next({ name: 'membership.changed', id: 'e3', personId: 'p3' });
      await settle();

      first.flush(person([asAdmin]));
      await settle();
      http.expectOne('/api/v1/me').flush(person([asAdmin]));
      await settle();

      http.expectNone('/api/v1/me');
    });

    it('leaves the person alone on an event that names a ticket', async () => {
      stream.next({ name: 'ticket.changed', id: 'e1', key: 'acme/VKO-1', version: 2, kind: 'x' });
      await settle();

      http.expectNone('/api/v1/me');
    });

    it('asks who is working again on an act that names the person in another of their tenants, one they join or leave among them (docs/adr/0054 D1)', async () => {
      stream.next({ name: 'membership.changed', id: 'e1', tenant: 'gamma', personId: hansId });
      await settle();

      http.expectOne('/api/v1/me').flush(person([asAdmin, asMember]));
      await settle();
    });

    it.each<StreamEvent>([
      { name: 'membership.changed', id: 'e1', tenant: 'gamma', personId: 'p2' },
      { name: 'membership.changed', id: 'e1', tenant: 'gamma', mappingId: 'm1' },
      { name: 'membership.changed', id: 'e1', tenant: 'gamma', projectId: 'j1' },
    ])(
      'leaves the person alone on %j, an act of another tenant that names somebody else',
      async (event) => {
        stream.next(event);
        await settle();

        http.expectNone('/api/v1/me');
      },
    );
  });

  describe('when asking again fails', () => {
    beforeEach(() => load(person([asAdmin, asMember])));

    it.each([0, 500, 503])(
      'keeps the person shown on a status of %i, which says nothing about them',
      async (status) => {
        stream.next({ name: 'poll' });
        await settle();

        fail(http.expectOne('/api/v1/me'), status);
        await settle();

        expect(service.me.status()).toBe('resolved');
        expect(service.person()?.display_name).toBe('Hans');
        expect(service.memberships()).toEqual([asAdmin, asMember]);
      },
    );

    it.each([401, 403, 404])('lets the person go on a %i', async (status) => {
      stream.next({ name: 'poll' });
      await settle();

      fail(http.expectOne('/api/v1/me'), status);
      await settle();

      expect(service.me.status()).toBe('error');
      expect(service.person()).toBeUndefined();
      expect(service.memberships()).toEqual([]);
    });

    it('takes the answer of the next attempt', async () => {
      stream.next({ name: 'poll' });
      await settle();
      fail(http.expectOne('/api/v1/me'), 503);
      await settle();

      stream.next({ name: 'poll' });
      await settle();
      http.expectOne('/api/v1/me').flush(person([asMember]));
      await settle();

      expect(service.memberships()).toEqual([asMember]);
    });
  });

  describe('with a single membership', () => {
    beforeEach(() => load(person([asMember])));

    it('names the sole tenant, which gets no switcher (docs/adr/0023 D4)', () => {
      expect(service.soleTenant()).toBe('globex');
    });
  });

  describe('with no membership at all', () => {
    beforeEach(() => load(person([])));

    it('names no sole tenant', () => {
      expect(service.memberships()).toEqual([]);
      expect(service.soleTenant()).toBeNull();
    });
  });

  describe('when the API answers 401', () => {
    beforeEach(async () => {
      meRequest().flush(
        { type: 'about:blank', title: 'Unauthenticated', status: 401, code: 'unauthenticated' },
        { status: 401, statusText: 'Unauthorized' },
      );
      await settle();
    });

    it('holds the error and nobody', () => {
      expect(service.me.status()).toBe('error');
      expect(service.me.error()).toBeInstanceOf(HttpErrorResponse);
      expect(service.person()).toBeUndefined();
      expect(service.memberships()).toEqual([]);
      expect(service.soleTenant()).toBeNull();
    });

    it('has no membership even in the tenant that is entered', () => {
      service.enter('acme');

      expect(service.membership()).toBeUndefined();
    });

    it('can ask again, as the login does once it has a session', async () => {
      expect(service.me.reload()).toBe(true);
      TestBed.tick();
      http.expectOne('/api/v1/me').flush(person([asAdmin]));
      await settle();

      expect(service.person()?.display_name).toBe('Hans');
    });
  });

  describe("the login page's own sign-in (docs/adr/0029 D6)", () => {
    beforeEach(() => sessionStorage.setItem('cowork.sign-in.attempt', '1'));
    afterEach(() => sessionStorage.clear());

    it('lets the tab try once more when its session ends, once it has a session again', async () => {
      await load(person([asAdmin]));

      expect(sessionStorage.getItem('cowork.sign-in.attempt')).toBeNull();
    });

    it('keeps the attempt noted while the tab has no session', async () => {
      meRequest().flush(
        { type: 'about:blank', title: 'Unauthenticated', status: 401, code: 'unauthenticated' },
        { status: 401, statusText: 'Unauthorized' },
      );
      await settle();

      expect(sessionStorage.getItem('cowork.sign-in.attempt')).toBe('1');
    });
  });

  describe("the application's other tabs in the browser", () => {
    it('hear whose session this tab has once it knows, and not again for the same person', async () => {
      TestBed.tick();
      expect(channel.told).toEqual([]);

      await load(person([asAdmin]));
      expect(channel.told).toEqual([{ person: hansId }]);

      stream.next({ name: 'poll' });
      await settle();
      http.expectOne('/api/v1/me').flush(person([asMember]));
      await settle();

      expect(channel.told).toEqual([{ person: hansId }]);
    });

    it('hear that this tab signed out', () => {
      service.signedOut();

      expect(channel.told).toEqual([{ signedOut: true }]);
    });

    it('make this tab load again when another person signs in in one of them', async () => {
      await load(person([asAdmin]));

      channel.say({ person: '0199aaaa-0000-7000-8000-000000000002' });

      expect(reload).toHaveBeenCalledOnce();
    });

    it('make this tab load again when one of them signs out', async () => {
      await load(person([asAdmin]));

      channel.say({ signedOut: true });

      expect(reload).toHaveBeenCalledOnce();
    });

    it('leave this tab as it is when they have the person it shows', async () => {
      await load(person([asAdmin]));

      channel.say({ person: hansId });

      expect(reload).not.toHaveBeenCalled();
    });

    it("leave a tab alone that has shown nobody yet, which holds nobody's state", () => {
      channel.say({ person: '0199aaaa-0000-7000-8000-000000000002' });
      channel.say({ signedOut: true });

      expect(reload).not.toHaveBeenCalled();
    });

    it("make a tab load again that has lost its session since, and still holds its person's state", async () => {
      await load(person([asAdmin]));
      stream.next({ name: 'poll' });
      await settle();
      fail(http.expectOne('/api/v1/me'), 401);
      await settle();
      expect(service.person()).toBeUndefined();

      channel.say({ person: '0199aaaa-0000-7000-8000-000000000002' });

      expect(reload).toHaveBeenCalledOnce();
    });

    it.each([null, undefined, 'signed out', 42, {}, { person: 7 }, { signedOut: 'yes' }])(
      'say nothing to this tab with %j',
      async (data) => {
        await load(person([asAdmin]));

        channel.say(data);

        expect(reload).not.toHaveBeenCalled();
      },
    );
  });
});

describe('SessionService where the browser has no BroadcastChannel', () => {
  afterEach(() => {
    vi.useRealTimers();
    TestBed.resetTestingModule();
  });

  it('keeps the session to its own tab', async () => {
    vi.useFakeTimers();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: EventStreamService, useValue: { events: new Subject<StreamEvent>() } },
        { provide: SESSION_CHANNEL, useValue: null },
      ],
    });
    const service = TestBed.inject(SessionService);
    const http = TestBed.inject(HttpTestingController);
    TestBed.tick();

    http.expectOne('/api/v1/me').flush(person([asAdmin]));
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
    service.signedOut();

    expect(service.person()?.id).toBe(hansId);
    http.verify();
  });
});

describe('SESSION_CHANNEL', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    TestBed.resetTestingModule();
  });

  it("is the browser's BroadcastChannel cowork.session, closed with the application", () => {
    const close = vi.spyOn(BroadcastChannel.prototype, 'close');

    const channel = TestBed.inject(SESSION_CHANNEL);

    expect(channel).toBeInstanceOf(BroadcastChannel);
    expect((channel as BroadcastChannel).name).toBe('cowork.session');
    expect(close).not.toHaveBeenCalled();
    TestBed.resetTestingModule();
    expect(close).toHaveBeenCalledOnce();
  });

  it('is none where the browser has no BroadcastChannel', () => {
    vi.stubGlobal('BroadcastChannel', undefined);

    expect(TestBed.inject(SESSION_CHANNEL)).toBeNull();
  });
});
