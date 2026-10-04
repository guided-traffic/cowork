import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Me, Tenant } from '../api/models';
import { SessionService } from './session.service';
import { TenantService } from './tenant.service';

const person: Me = {
  id: '0199aaaa-0000-7000-8000-000000000001',
  display_name: 'Hans',
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships: [
    {
      role: 'admin',
      tenant: { slug: 'acme', name: 'Acme Corp' },
      origins: [{ source: 'grant', role: 'admin' }],
    },
    {
      role: 'member',
      tenant: { slug: 'globex', name: 'Globex' },
      origins: [{ source: 'grant', role: 'member' }],
    },
    {
      role: 'viewer',
      tenant: { slug: 'initech', name: 'Initech' },
      origins: [{ source: 'grant', role: 'viewer' }],
    },
  ],
};

function tenant(slug: string, overrides: Partial<Tenant> = {}): Tenant {
  return {
    slug,
    name: `Tenant ${slug}`,
    members_create_projects: false,
    chat_external_allowed: false,
    time_visible_to_members: false,
    time_locked_until: null,
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-01T10:00:00Z',
    version: 4,
    ...overrides,
  };
}

const forbidden = {
  type: 'about:blank',
  title: 'Forbidden',
  status: 403,
  code: 'forbidden',
};

const rejection = (promise: Promise<unknown>) =>
  promise.then(
    () => null,
    (error: unknown) => error,
  );

describe('TenantService', () => {
  let service: TenantService;
  let session: SessionService;
  let http: HttpTestingController;

  /**
   * Starts the loads that are due, lets the promise chains of answered requests finish, and runs the
   * effects they feed. Fake timers yield the macrotask for that, so this never waits for a request
   * the test has not answered yet.
   */
  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  const request = (url: string) => http.expectOne((r) => r.url === url);
  /** Enters the tenant and answers the load of its settings. */
  const enter = async (slug: string, settings: Partial<Tenant> = {}) => {
    session.enter(slug);
    await settle();
    request(`/api/v1/tenants/${slug}`).flush(tenant(slug, settings));
    await settle();
  };

  beforeEach(async () => {
    vi.useFakeTimers();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    service = TestBed.inject(TenantService);
    session = TestBed.inject(SessionService);
    http = TestBed.inject(HttpTestingController);
    await settle();
    request('/api/v1/me').flush(person);
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

  describe('outside a tenant', () => {
    it('asks for nothing and knows nothing', () => {
      expect(service.tenant.status()).toBe('idle');
      expect(service.value()).toBeUndefined();
      expect(service.isAdmin()).toBe(false);
      expect(service.canCreateProjects()).toBe(false);
    });
  });

  describe('the settings of the tenant', () => {
    it('are asked for at /api/v1/tenants/<slug>, without a doubled slash', async () => {
      session.enter('acme');
      await settle();

      const sent = request('/api/v1/tenants/acme');

      expect(sent.request.method).toBe('GET');
      expect(sent.request.url).toBe('/api/v1/tenants/acme');
      expect(service.tenant.status()).toBe('loading');
      sent.flush(tenant('acme'));
      await settle();
    });

    it('are not there until they arrive, and are the answer once they have', async () => {
      session.enter('acme');
      await settle();
      const sent = request('/api/v1/tenants/acme');
      expect(service.value()).toBeUndefined();

      sent.flush(tenant('acme', { members_create_projects: true, version: 7 }));
      await settle();

      expect(service.tenant.status()).toBe('resolved');
      expect(service.value()).toEqual(
        tenant('acme', { members_create_projects: true, version: 7 }),
      );
    });

    it('are not there when the load fails, and say why', async () => {
      session.enter('acme');
      await settle();

      request('/api/v1/tenants/acme').flush(forbidden, { status: 403, statusText: 'Forbidden' });
      await settle();

      expect(service.tenant.status()).toBe('error');
      expect((service.tenant.error() as HttpErrorResponse).status).toBe(403);
      expect(service.value()).toBeUndefined();
    });

    it('are those of the new tenant after a switch, and not the old ones while they load', async () => {
      await enter('acme', { name: 'Acme Corp' });
      expect(service.value()?.name).toBe('Acme Corp');

      session.enter('globex');
      await settle();
      const sent = request('/api/v1/tenants/globex');
      expect(service.value()).toBeUndefined();

      sent.flush(tenant('globex', { name: 'Globex' }));
      await settle();
      expect(service.value()?.name).toBe('Globex');
    });

    it('are asked for no more on a person-level page, and are gone', async () => {
      await enter('acme');

      session.enter(null);
      await settle();

      expect(service.tenant.status()).toBe('idle');
      expect(service.value()).toBeUndefined();
    });

    it('stay on screen while they are loaded again', async () => {
      await enter('acme', { name: 'Acme Corp' });

      expect(service.tenant.reload()).toBe(true);
      await settle();
      const again = request('/api/v1/tenants/acme');

      expect(service.tenant.status()).toBe('reloading');
      expect(service.value()?.name).toBe('Acme Corp');
      again.flush(tenant('acme', { name: 'Acme Inc' }));
      await settle();
      expect(service.value()?.name).toBe('Acme Inc');
    });
  });

  describe('isAdmin', () => {
    it.each([
      ['acme', 'an administrator', true],
      ['globex', 'a member', false],
      ['initech', 'a viewer', false],
      ['nowhere', 'nothing, being no member of the tenant', false],
    ])('in %s, where the person is %s, is %s', async (slug, _role, expected) => {
      session.enter(slug);
      await settle();

      expect(service.isAdmin()).toBe(expected);

      request(`/api/v1/tenants/${slug}`).flush(tenant(slug));
      await settle();
      expect(service.isAdmin()).toBe(expected);
    });

    it('is the role of the tenant that is entered, and changes with it', async () => {
      await enter('acme');
      expect(service.isAdmin()).toBe(true);

      await enter('globex');
      expect(service.isAdmin()).toBe(false);

      session.enter(null);
      await settle();
      expect(service.isAdmin()).toBe(false);
    });
  });

  describe('canCreateProjects', () => {
    it.each([
      ['acme', 'an administrator', true, true],
      ['acme', 'an administrator', false, true],
      ['globex', 'a member', true, true],
      ['globex', 'a member', false, false],
      ['initech', 'a viewer', true, false],
      ['initech', 'a viewer', false, false],
    ])(
      'in %s, where the person is %s and the tenant lets members create projects: %s, is %s',
      async (slug, _role, allowed, expected) => {
        await enter(slug, { members_create_projects: allowed });

        expect(service.canCreateProjects()).toBe(expected);
      },
    );

    it('is true for an administrator before the settings are loaded, who needs no setting', async () => {
      session.enter('acme');
      await settle();
      const sent = request('/api/v1/tenants/acme');

      expect(service.value()).toBeUndefined();
      expect(service.canCreateProjects()).toBe(true);

      sent.flush(tenant('acme'));
      await settle();
    });

    it.each([
      ['globex', 'a member'],
      ['initech', 'a viewer'],
    ])('is false for %s, %s, until the settings say otherwise', async (slug) => {
      session.enter(slug);
      await settle();
      const sent = request(`/api/v1/tenants/${slug}`);

      expect(service.canCreateProjects()).toBe(false);

      sent.flush(tenant(slug, { members_create_projects: true }));
      await settle();
    });

    it('is false for a member when the settings failed to load', async () => {
      session.enter('globex');
      await settle();

      request('/api/v1/tenants/globex').flush(forbidden, { status: 403, statusText: 'Forbidden' });
      await settle();

      expect(service.canCreateProjects()).toBe(false);
    });

    it('is false where the person is no member at all, whatever the settings say', async () => {
      await enter('nowhere', { members_create_projects: true });

      expect(service.canCreateProjects()).toBe(false);
    });

    it('is false outside a tenant', () => {
      expect(service.canCreateProjects()).toBe(false);
    });

    it('follows the setting when the settings are changed', async () => {
      await enter('globex', { members_create_projects: false });
      expect(service.canCreateProjects()).toBe(false);

      const done = service.update({ members_create_projects: true });
      request('/api/v1/tenants/globex').flush(
        tenant('globex', { members_create_projects: true, version: 5 }),
      );
      await done;
      expect(service.canCreateProjects()).toBe(true);

      const back = service.update({ members_create_projects: false });
      request('/api/v1/tenants/globex').flush(
        tenant('globex', { members_create_projects: false, version: 6 }),
      );
      await back;
      expect(service.canCreateProjects()).toBe(false);
    });

    it('follows the tenant that is entered', async () => {
      await enter('globex', { members_create_projects: true });
      expect(service.canCreateProjects()).toBe(true);

      await enter('initech', { members_create_projects: true });
      expect(service.canCreateProjects()).toBe(false);

      await enter('acme', { members_create_projects: false });
      expect(service.canCreateProjects()).toBe(true);
    });
  });

  describe('update', () => {
    beforeEach(() => enter('acme', { version: 4 }));

    it('patches the tenant over the version that is held, as If-Match (docs/adr/0050 D3)', async () => {
      const done = service.update({ name: 'Acme Inc' });

      const sent = request('/api/v1/tenants/acme');
      expect(sent.request.method).toBe('PATCH');
      expect(sent.request.headers.get('If-Match')).toBe('"4"');
      expect(sent.request.body).toEqual({ name: 'Acme Inc' });
      const answer = tenant('acme', {
        name: 'Acme Inc',
        version: 5,
        updated_at: '2026-10-03T11:00:00Z',
      });
      sent.flush(answer);

      expect(await done).toEqual(answer);
    });

    it('shows the answer at once, as the settings, without asking again', async () => {
      const done = service.update({ time_visible_to_members: true });
      request('/api/v1/tenants/acme').flush(
        tenant('acme', { time_visible_to_members: true, version: 5 }),
      );
      await done;
      await settle();

      http.expectNone((r) => r.url === '/api/v1/tenants/acme');
      expect(service.value()?.time_visible_to_members).toBe(true);
      expect(service.value()?.version).toBe(5);
    });

    it('goes on over the version of the answer, so that a second change does not meet a stale one', async () => {
      const first = service.update({ name: 'One' });
      request('/api/v1/tenants/acme').flush(tenant('acme', { name: 'One', version: 5 }));
      await first;

      const second = service.update({ name: 'Two' });
      const sent = request('/api/v1/tenants/acme');
      expect(sent.request.headers.get('If-Match')).toBe('"5"');
      sent.flush(tenant('acme', { name: 'Two', version: 6 }));
      await second;

      expect(service.value()?.name).toBe('Two');
    });

    it('sends the patch as it is, a member that is null included', async () => {
      const done = service.update({ time_locked_until: null, members_create_projects: true });

      const sent = request('/api/v1/tenants/acme');
      expect(sent.request.body).toEqual({ time_locked_until: null, members_create_projects: true });
      sent.flush(tenant('acme', { members_create_projects: true, version: 5 }));
      await done;
    });

    it('needs no key, being a patch of settings that exist', async () => {
      const done = service.update({ name: 'Acme Inc' });

      const sent = request('/api/v1/tenants/acme');
      expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
      sent.flush(tenant('acme', { version: 5 }));
      await done;
    });

    it('rejects with the HTTP error of a 412 and keeps the settings and the version it had', async () => {
      const outcome = rejection(service.update({ name: 'Acme Inc' }));

      request('/api/v1/tenants/acme').flush(
        { ...forbidden, status: 412, code: 'precondition_failed' },
        { status: 412, statusText: 'Precondition Failed' },
      );
      const error = await outcome;
      await settle();

      expect((error as HttpErrorResponse).status).toBe(412);
      expect(service.value()?.version).toBe(4);
      expect(service.value()?.name).toBe('Tenant acme');
    });

    it('rejects with the HTTP error of a 403 for a person who may not change the settings', async () => {
      const outcome = rejection(service.update({ name: 'Acme Inc' }));

      request('/api/v1/tenants/acme').flush(forbidden, { status: 403, statusText: 'Forbidden' });

      expect(((await outcome) as HttpErrorResponse).status).toBe(403);
      expect(service.value()?.name).toBe('Tenant acme');
    });

    it('asks again with the version of the answer after a refusal was resolved by a reload', async () => {
      const outcome = rejection(service.update({ name: 'Acme Inc' }));
      request('/api/v1/tenants/acme').flush(
        { ...forbidden, status: 412, code: 'precondition_failed' },
        { status: 412, statusText: 'Precondition Failed' },
      );
      await outcome;

      service.tenant.reload();
      await settle();
      request('/api/v1/tenants/acme').flush(tenant('acme', { name: 'Theirs', version: 9 }));
      await settle();
      const done = service.update({ name: 'Acme Inc' });

      const sent = request('/api/v1/tenants/acme');
      expect(sent.request.headers.get('If-Match')).toBe('"9"');
      sent.flush(tenant('acme', { name: 'Acme Inc', version: 10 }));
      await done;
    });

    it('writes the tenant that was entered when it was called', async () => {
      await enter('globex');

      const done = service.update({ name: 'Globex Inc' });

      const sent = request('/api/v1/tenants/globex');
      expect(sent.request.headers.get('If-Match')).toBe('"4"');
      sent.flush(tenant('globex', { name: 'Globex Inc', version: 5 }));
      await done;
    });

    // A switch while the write is on its way must not show the settings of the old tenant on the
    // pages of the new one, nor abort the new one's load (docs/adr/0053 D4).
    it('does not show the answer of one tenant as the settings of the next', async () => {
      const done = service.update({ name: 'Acme Inc' });
      const sent = request('/api/v1/tenants/acme');

      session.enter('globex');
      await settle();
      const globex = request('/api/v1/tenants/globex');
      sent.flush(tenant('acme', { name: 'Acme Inc', version: 5 }));
      await done;
      globex.flush(tenant('globex', { name: 'Globex' }));
      await settle();

      expect(service.value()?.slug).toBe('globex');
    });
  });

  describe('update before the settings are loaded', () => {
    it('sends no version, there being none held, and passes the refusal of the backend on', async () => {
      session.enter('acme');
      await settle();
      const loading = request('/api/v1/tenants/acme');

      const outcome = rejection(service.update({ name: 'Acme Inc' }));
      const sent = http.expectOne((r) => r.method === 'PATCH');
      expect(sent.request.headers.has('If-Match')).toBe(false);
      sent.flush(
        { ...forbidden, status: 428, code: 'precondition_required' },
        { status: 428, statusText: 'Precondition Required' },
      );
      const error = await outcome;

      expect((error as HttpErrorResponse).status).toBe(428);
      expect(service.value()).toBeUndefined();
      loading.flush(tenant('acme'));
      await settle();
    });
  });
});
