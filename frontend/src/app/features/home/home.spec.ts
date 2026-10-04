import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Me, Membership, Problem, Tenant } from '../../api/models';
import { SessionService } from '../../core/session.service';
import { TenantsService } from '../../core/tenants.service';
import { Home } from './home';

const acme: Membership = {
  role: 'admin',
  tenant: { name: 'Acme Corp', slug: 'acme' },
  origins: [{ source: 'grant', role: 'admin' }],
};
const globex: Membership = {
  role: 'member',
  tenant: { name: 'Globex', slug: 'globex' },
  origins: [{ source: 'grant', role: 'member' }],
};

function person(globalAdmin: boolean, memberships: Membership[] = []): Me {
  return {
    id: 'p1',
    display_name: 'Hans',
    username: 'hans',
    global_admin: globalAdmin,
    local: true,
    password_change_required: false,
    memberships,
  };
}

function problem(status: number, code: Problem['code'], title: string, detail: string): Problem {
  return { type: 'about:blank', title, status, detail, code };
}

function failure(body: Problem): HttpErrorResponse {
  return new HttpErrorResponse({ status: body.status, statusText: body.title, error: body });
}

describe('Home', () => {
  describe('against a mocked session', () => {
    let navigate: MockInstance<Router['navigate']>;
    let session: {
      me: { isLoading: WritableSignal<boolean>; error: WritableSignal<unknown> };
      person: WritableSignal<Me | undefined>;
      memberships: WritableSignal<Membership[]>;
      soleTenant: WritableSignal<string | null>;
    };

    beforeEach(() => {
      session = {
        me: { isLoading: signal(false), error: signal<unknown>(undefined) },
        person: signal<Me | undefined>(undefined),
        memberships: signal<Membership[]>([]),
        soleTenant: signal<string | null>(null),
      };
      TestBed.configureTestingModule({
        providers: [
          provideRouter([]),
          MessageService,
          { provide: SessionService, useValue: session },
          { provide: TenantsService, useValue: { create: vi.fn() } },
        ],
      });
      navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    });

    async function render(): Promise<HTMLElement> {
      const fixture = TestBed.createComponent(Home);
      await fixture.whenStable();
      return fixture.nativeElement as HTMLElement;
    }

    it('shows a skeleton while the person is loading', async () => {
      session.me.isLoading.set(true);

      const page = await render();

      expect(page.querySelector('p-skeleton')).not.toBeNull();
      expect(page.querySelector('h1')).toBeNull();
    });

    it('goes straight to the only tenant and replaces the start page in the history', async () => {
      session.memberships.set([acme]);
      session.soleTenant.set('acme');

      await render();

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme'], { replaceUrl: true });
    });

    it('goes to the tenant when it turns out to be the only one after the page opened', async () => {
      const fixture = TestBed.createComponent(Home);
      await fixture.whenStable();
      expect(navigate).not.toHaveBeenCalled();

      session.memberships.set([globex]);
      session.soleTenant.set('globex');
      await fixture.whenStable();

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'globex'], { replaceUrl: true });
    });

    it('lists the tenants to choose from when there are several and stays on the page', async () => {
      session.memberships.set([acme, globex]);

      const page = await render();

      expect(navigate).not.toHaveBeenCalled();
      expect(page.querySelector('h1')?.textContent).toBe('Your tenants');
      const first = page.querySelector('[data-testid="tenant-acme"]');
      expect(first?.getAttribute('href')).toBe('/t/acme');
      expect(first?.querySelector('.name')?.textContent).toBe('Acme Corp');
      expect(first?.querySelector('.muted')?.textContent).toBe('acme · admin');
      const second = page.querySelector('[data-testid="tenant-globex"]');
      expect(second?.getAttribute('href')).toBe('/t/globex');
      expect(second?.querySelector('.muted')?.textContent).toBe('globex · member');
    });

    it('says so when the person is a member of no tenant', async () => {
      const page = await render();

      expect(page.querySelector('.tenant')).toBeNull();
      expect(page.textContent).toContain('You are not a member of any tenant yet.');
    });

    describe('a global administrator who is in no tenant', () => {
      it('is offered to create the first tenant, instead of being told that there is none', async () => {
        session.person.set(person(true));

        const page = await render();

        expect(page.querySelector('app-first-tenant')).not.toBeNull();
        expect(page.querySelector('[data-testid="first-tenant"] h1')?.textContent).toBe(
          'Create the first tenant',
        );
        expect(page.textContent).not.toContain('You are not a member of any tenant yet.');
        expect(page.querySelector('h1')?.textContent).toBe('Create the first tenant');
        expect(page.querySelector('.tenants')).toBeNull();
        expect(navigate).not.toHaveBeenCalled();
      });

      it('is offered nothing to create while the person is loading', async () => {
        session.person.set(person(true));
        session.me.isLoading.set(true);

        const page = await render();

        expect(page.querySelector('app-first-tenant')).toBeNull();
        expect(page.querySelector('p-skeleton')).not.toBeNull();
      });

      it('is told what went wrong, and offered nothing to create, when the person could not be loaded', async () => {
        session.person.set(person(true));
        session.me.error.set(
          failure(
            problem(503, 'not_ready', 'The service is not ready', 'The database is starting.'),
          ),
        );

        const page = await render();

        expect(page.querySelector('app-first-tenant')).toBeNull();
        expect(page.querySelector('[data-testid="signed-out"]')).not.toBeNull();
      });

      it('chooses between the tenants of the person when there are some, as anybody does', async () => {
        session.person.set(person(true, [acme, globex]));
        session.memberships.set([acme, globex]);

        const page = await render();

        expect(page.querySelector('app-first-tenant')).toBeNull();
        expect(page.querySelector('h1')?.textContent).toBe('Your tenants');
        expect(page.querySelector('[data-testid="tenant-acme"]')).not.toBeNull();
      });

      it('goes straight to the only tenant, as anybody does', async () => {
        session.person.set(person(true, [acme]));
        session.memberships.set([acme]);
        session.soleTenant.set('acme');

        await render();

        expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme'], { replaceUrl: true });
      });
    });

    it('does not offer anybody else a tenant to create, and says they are in none', async () => {
      session.person.set(person(false));

      const page = await render();

      expect(page.querySelector('app-first-tenant')).toBeNull();
      expect(page.textContent).toContain('You are not a member of any tenant yet.');
    });

    it('shows the signed-out notice with the way to the login when the API answers 401', async () => {
      session.me.error.set(
        failure(
          problem(401, 'unauthenticated', 'Authentication required', 'Sign in to use the API.'),
        ),
      );

      const page = await render();

      const notice = page.querySelector('[data-testid="signed-out"]');
      expect(notice?.querySelector('h1')?.textContent).toBe('Authentication required');
      expect(notice?.querySelector('p.muted')?.textContent).toBe('Sign in to use the API.');
      expect(notice?.querySelector('[data-testid="sign-in"]')?.getAttribute('href')).toBe('/login');
      expect(page.querySelector('.tenants')).toBeNull();
      expect(navigate).not.toHaveBeenCalled();
    });

    it('shows a failure other than a 401 without the way to the login', async () => {
      session.me.error.set(
        failure(problem(503, 'not_ready', 'The service is not ready', 'The database is starting.')),
      );

      const page = await render();

      const notice = page.querySelector('[data-testid="signed-out"]');
      expect(notice?.querySelector('h1')?.textContent).toBe('The service is not ready');
      expect(notice?.querySelector('p.muted')?.textContent).toBe('The database is starting.');
      expect(notice?.querySelector('[data-testid="sign-in"]')).toBeNull();
    });

    it('shows an unreachable backend as the problem it is', async () => {
      session.me.error.set(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));

      const page = await render();

      const notice = page.querySelector('[data-testid="signed-out"]');
      expect(notice?.querySelector('h1')?.textContent).toBe('The backend cannot be reached');
      expect(notice?.querySelector('[data-testid="sign-in"]')).toBeNull();
    });
  });

  describe('against the real session', () => {
    it('shows the signed-out notice when GET /api/v1/me answers 401 with a problem body', async () => {
      TestBed.configureTestingModule({
        providers: [
          provideRouter([]),
          provideHttpClient(),
          provideHttpClientTesting(),
          provideApiConfiguration(''),
          MessageService,
        ],
      });
      const http = TestBed.inject(HttpTestingController);
      const fixture = TestBed.createComponent(Home);
      fixture.detectChanges();

      http
        .expectOne('/api/v1/me')
        .flush(problem(401, 'unauthenticated', 'Authentication required', 'Sign in first.'), {
          status: 401,
          statusText: 'Unauthorized',
        });
      await fixture.whenStable();
      fixture.detectChanges();

      const notice = (fixture.nativeElement as HTMLElement).querySelector(
        '[data-testid="signed-out"]',
      );
      expect(notice?.querySelector('h1')?.textContent).toBe('Authentication required');
      expect(notice?.querySelector('[data-testid="sign-in"]')?.getAttribute('href')).toBe('/login');
      http.verify();
    });

    it('lets a global administrator without a tenant create the first one, load the person again and go into it', async () => {
      TestBed.configureTestingModule({
        providers: [
          provideRouter([]),
          provideHttpClient(),
          provideHttpClientTesting(),
          provideApiConfiguration(''),
          MessageService,
        ],
      });
      const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
      const http = TestBed.inject(HttpTestingController);
      const fixture = TestBed.createComponent(Home);
      fixture.detectChanges();
      http.expectOne('/api/v1/me').flush(person(true));
      await fixture.whenStable();
      fixture.detectChanges();
      const page = fixture.nativeElement as HTMLElement;
      expect(page.querySelector('[data-testid="first-tenant"] h1')?.textContent).toBe(
        'Create the first tenant',
      );

      const type = (testId: string, value: string) => {
        const field = page.querySelector<HTMLInputElement>(`[data-testid="${testId}"]`);
        field!.value = value;
        field!.dispatchEvent(new Event('input'));
        fixture.detectChanges();
      };
      type('first-tenant-slug', 'acme');
      type('first-tenant-name', 'Acme Corp');
      page.querySelector('form')?.dispatchEvent(new Event('submit', { cancelable: true }));
      const created = http.expectOne((request) => request.method === 'POST');
      expect(created.request.url).toBe('/api/v1/tenants');
      expect(created.request.body).toEqual({ slug: 'acme', name: 'Acme Corp' });
      const tenant: Tenant = {
        slug: 'acme',
        name: 'Acme Corp',
        version: 1,
        time_visible_to_members: false,
        members_create_projects: true,
        chat_external_allowed: false,
        created_at: '2026-10-03T10:00:00Z',
        updated_at: '2026-10-03T10:00:00Z',
      };
      created.flush(tenant);
      // The request for the person is not answered yet, which keeps the fixture from being stable.
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();

      // The person is loaded again, because the new membership is what the tenant's pages read.
      http.expectOne('/api/v1/me').flush(person(true, [acme]));
      await fixture.whenStable();
      expect(navigate).toHaveBeenCalledWith(['/t', 'acme']);
      http.verify();
    });
  });
});
