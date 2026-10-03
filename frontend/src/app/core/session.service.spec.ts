import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { effect } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Me, Membership } from '../api/models';
import { SessionService } from './session.service';

const asAdmin: Membership = { role: 'admin', tenant: { slug: 'acme', name: 'Acme Corp' } };
const asMember: Membership = { role: 'member', tenant: { slug: 'globex', name: 'Globex' } };

const person = (memberships: Membership[]): Me => ({
  id: '0199aaaa-0000-7000-8000-000000000001',
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
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
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
});
