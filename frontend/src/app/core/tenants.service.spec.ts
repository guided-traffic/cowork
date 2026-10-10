import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Me, Team } from '../api/models';
import { SessionService } from './session.service';
import { TenantsService } from './tenants.service';

const administrator = (memberships: Me['memberships'] = []): Me => ({
  id: 'p1',
  display_name: 'Hans',
  username: 'hans',
  global_admin: true,
  local: true,
  password_change_required: false,
  memberships,
});

const acme: Team = {
  slug: 'acme',
  name: 'Acme Corp',
  version: 1,
  time_visible_to_members: false,
  members_create_projects: true,
  created_at: '2026-10-03T10:00:00Z',
  updated_at: '2026-10-03T10:00:00Z',
};

const rejection = (promise: Promise<unknown>) =>
  promise.then(
    () => null,
    (error: unknown) => error,
  );

describe('TenantsService', () => {
  let service: TenantsService;
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
  const write = () =>
    http.expectOne((request) => request.method === 'POST' && request.url === '/api/v1/teams');
  /** The list of the installation's tenants a global administrator's session loads (docs/adr/0034 D2). */
  const installation = () =>
    http.expectOne((request) => request.method === 'GET' && request.url === '/api/v1/teams');
  const noTenants = { items: [], next_cursor: null };
  const body = { slug: 'acme', name: 'Acme Corp' };
  const key = '0199aaaa-2222-7000-8000-000000000001';

  beforeEach(async () => {
    vi.useFakeTimers();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    service = TestBed.inject(TenantsService);
    session = TestBed.inject(SessionService);
    http = TestBed.inject(HttpTestingController);
    TestBed.tick();
    http.expectOne('/api/v1/me').flush(administrator());
    await settle();
    installation().flush(noTenants);
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

  it('posts the slug and the name to /api/v1/teams and hands back the tenant that was made', async () => {
    const done = service.create(body, key);

    const sent = write();
    expect(sent.request.url).toBe('/api/v1/teams');
    expect(sent.request.body).toEqual(body);
    expect(sent.request.headers.has('If-Match')).toBe(false);
    sent.flush(acme);

    expect(await done).toEqual(acme);
    await settle();
    http.expectOne('/api/v1/me').flush(administrator());
    installation().flush(noTenants);
  });

  it('sends the key it is given, not one of its own (docs/adr/0045 D3)', async () => {
    const done = service.create(body, key);

    const sent = write();
    expect(sent.request.headers.get('Idempotency-Key')).toBe(key);
    sent.flush(acme);
    await done;
    await settle();
    http.expectOne('/api/v1/me').flush(administrator());
    installation().flush(noTenants);
  });

  it('sends the same key again for a retry of the same content after a network failure, and another one for other content', async () => {
    const lost = rejection(service.create(body, key));
    write().error(new ProgressEvent('error'));
    expect(((await lost) as HttpErrorResponse).status).toBe(0);
    await settle();
    http.expectNone('/api/v1/me');
    http.expectNone((request) => request.method === 'GET' && request.url === '/api/v1/teams');

    const retry = service.create(body, key);
    const again = write();
    expect(again.request.headers.get('Idempotency-Key')).toBe(key);
    again.flush(acme);
    await retry;
    await settle();
    http.expectOne('/api/v1/me').flush(administrator());
    installation().flush(noTenants);
    await settle();

    const other = '0199aaaa-2222-7000-8000-000000000002';
    const changed = service.create({ slug: 'globex', name: 'Globex' }, other);
    const third = write();
    expect(third.request.headers.get('Idempotency-Key')).toBe(other);
    third.flush({ ...acme, slug: 'globex' });
    await changed;
    await settle();
    http.expectOne('/api/v1/me').flush(administrator());
    installation().flush(noTenants);
  });

  it('loads the person again, so that the new membership is there for the pages of the tenant', async () => {
    expect(session.memberships()).toEqual([]);
    const done = service.create(body, key);
    await settle();
    http.expectNone('/api/v1/me');
    write().flush(acme);
    await done;

    await settle();
    expect(session.me.status()).toBe('reloading');
    http.expectOne('/api/v1/me').flush(
      administrator([
        {
          role: 'admin',
          team: { slug: 'acme', name: 'Acme Corp' },
          tenant: { slug: 'acme', name: 'Acme Corp' },
          origins: [{ source: 'grant', role: 'admin' }],
        },
      ]),
    );
    installation().flush({ items: [{ slug: 'acme', name: 'Acme Corp', role: 'admin' }], next_cursor: null });
    await settle();

    expect(session.memberships().map((membership) => membership.team.slug)).toEqual(['acme']);
    expect(session.installation.value()?.map((tenant) => tenant.slug)).toEqual(['acme']);
    expect(session.soleTenant()).toBe('acme');
  });

  it('loads the person once more when it ends while the person is loading, because that answer may predate the tenant', async () => {
    session.me.reload();
    await settle();
    const inFlight = http.expectOne('/api/v1/me');

    const done = service.create(body, key);
    write().flush(acme);
    await done;
    await settle();
    inFlight.flush(administrator());
    await settle();

    http.expectOne('/api/v1/me').flush(
      administrator([
        {
          role: 'admin',
          team: { slug: 'acme', name: 'Acme Corp' },
          tenant: { slug: 'acme', name: 'Acme Corp' },
          origins: [{ source: 'grant', role: 'admin' }],
        },
      ]),
    );
    installation().flush(noTenants);
  });

  it.each([
    [409, 'tenant_slug_taken'],
    [403, 'session_required'],
    [403, 'forbidden'],
    [422, 'validation_failed'],
    [500, 'internal'],
  ])(
    'rejects with the HTTP error of a %i (%s) and does not load the person again',
    async (status, code) => {
      const outcome = rejection(service.create(body, key));

      write().flush(
        { type: 'about:blank', title: 'Refused', status, code },
        { status, statusText: `Status ${status}` },
      );
      const error = await outcome;
      await settle();

      expect(error).toBeInstanceOf(HttpErrorResponse);
      expect((error as HttpErrorResponse).status).toBe(status);
      http.expectNone('/api/v1/me');
      http.expectNone((request) => request.method === 'GET' && request.url === '/api/v1/teams');
    },
  );
});
