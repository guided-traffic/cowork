import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Component, computed, input, Signal, signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Me, Membership, Problem, Team } from '../../api/models';
import { OpenableTenant, SessionService } from '../../core/session.service';
import { TenantsService } from '../../core/tenants.service';
import { MyTickets } from '../me/my-tickets';
import { Home } from './home';

/** "Next for me" stands in as itself, without the API it reads: its own spec is lists.spec.ts. */
@Component({ selector: 'app-my-tickets', template: '', host: { '[attr.data-list]': 'list()' } })
class MyTicketsStub {
  readonly list = input<string>();
}

function stubMyTickets(): void {
  TestBed.overrideComponent(Home, {
    remove: { imports: [MyTickets] },
    add: { imports: [MyTicketsStub] },
  });
}

const acme: Membership = {
  role: 'admin',
  team: { name: 'Acme Corp', slug: 'acme' },
  tenant: { name: 'Acme Corp', slug: 'acme' },
  origins: [{ source: 'grant', role: 'admin' }],
  can_create_projects: true,
};
const globex: Membership = {
  role: 'member',
  team: { name: 'Globex', slug: 'globex' },
  tenant: { name: 'Globex', slug: 'globex' },
  origins: [{ source: 'grant', role: 'member' }],
  can_create_projects: true,
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
      tenants: Signal<OpenableTenant[]>;
      installation: { isLoading: WritableSignal<boolean>; hasValue: WritableSignal<boolean> };
    };
    /** The tenants of the installation a global administrator holds no role in. */
    let roleless: WritableSignal<OpenableTenant[]>;

    beforeEach(() => {
      roleless = signal<OpenableTenant[]>([]);
      const memberships = signal<Membership[]>([]);
      session = {
        me: { isLoading: signal(false), error: signal<unknown>(undefined) },
        person: signal<Me | undefined>(undefined),
        memberships,
        tenants: computed(() => [
          ...memberships().map(({ team, role }) => ({ ...team, role })),
          ...roleless(),
        ]),
        installation: { isLoading: signal(false), hasValue: signal(true) },
      };
      TestBed.configureTestingModule({
        providers: [
          provideRouter([]),
          MessageService,
          { provide: SessionService, useValue: session },
          { provide: TenantsService, useValue: { create: vi.fn() } },
        ],
      });
      stubMyTickets();
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

    // docs/adr/0018 D3, docs/adr/0023 D4 as amended 2026-10-05: the start page is "next for me".
    it('shows "next for me" to a person with one tenant, and goes nowhere', async () => {
      session.memberships.set([acme]);

      const page = await render();

      expect(page.querySelector('app-my-tickets')?.getAttribute('data-list')).toBe('next');
      expect(page.querySelector('.tenants')).toBeNull();
      expect(navigate).not.toHaveBeenCalled();
    });

    it('shows "next for me" to a person with several tenants, across all of them', async () => {
      session.memberships.set([acme, globex]);

      const page = await render();

      expect(page.querySelector('app-my-tickets')).not.toBeNull();
      expect(page.querySelector('[data-testid="tenant-acme"]')).toBeNull();
      expect(navigate).not.toHaveBeenCalled();
    });

    it('says so when the person is a member of no tenant', async () => {
      const page = await render();

      expect(page.querySelector('.tenant')).toBeNull();
      expect(page.textContent).toContain('You are not a member of any team yet.');
    });

    describe('a global administrator who is in no tenant', () => {
      it('is offered to create the first tenant, instead of being told that there is none', async () => {
        session.person.set(person(true));

        const page = await render();

        expect(page.querySelector('app-first-tenant')).not.toBeNull();
        expect(page.querySelector('[data-testid="first-tenant"] h1')?.textContent).toBe(
          'Create the first team',
        );
        expect(page.textContent).not.toContain('You are not a member of any team yet.');
        expect(page.querySelector('h1')?.textContent).toBe('Create the first team');
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

      it('sees "next for me" when they hold a role somewhere, as anybody does', async () => {
        session.person.set(person(true, [acme, globex]));
        session.memberships.set([acme, globex]);

        const page = await render();

        expect(page.querySelector('app-first-tenant')).toBeNull();
        expect(page.querySelector('app-my-tickets')).not.toBeNull();
      });

      // docs/adr/0034 D2: a global administrator finds the tenants they hold no role in.
      it('lists every tenant of the installation, without a role marked so, while they hold none', async () => {
        session.person.set(person(true));
        roleless.set([
          { slug: 'acme', name: 'Acme Corp', role: null },
          { slug: 'initech', name: 'Initech', role: null },
        ]);

        const page = await render();

        expect(page.querySelector('app-first-tenant')).toBeNull();
        expect(page.querySelector('app-my-tickets')).toBeNull();
        expect(page.querySelector('h1')?.textContent).toBe('Your teams');
        const first = page.querySelector('[data-testid="tenant-acme"]');
        expect(first?.querySelector('.name')?.textContent).toBe('Acme Corp');
        const other = page.querySelector('[data-testid="tenant-initech"]');
        expect(other?.getAttribute('href')).toBe('/t/initech');
        expect(other?.querySelector('.muted')?.textContent).toBe('initech · no role');
      });

      it('is offered no first tenant where tenants exist that they hold no role in', async () => {
        session.person.set(person(true));
        roleless.set([{ slug: 'initech', name: 'Initech', role: null }]);

        const page = await render();

        expect(page.querySelector('app-first-tenant')).toBeNull();
        expect(page.querySelector('[data-testid="tenant-initech"]')).not.toBeNull();
      });

      it("waits for the installation's tenants before it offers or lists anything", async () => {
        session.person.set(person(true));
        session.installation.isLoading.set(true);
        session.installation.hasValue.set(false);

        const page = await render();

        expect(page.querySelector('app-first-tenant')).toBeNull();
        expect(page.querySelector('p-skeleton')).not.toBeNull();
        expect(page.textContent).not.toContain('You are not a member of any team yet.');
      });
    });

    it('does not offer anybody else a tenant to create, and says they are in none', async () => {
      session.person.set(person(false));

      const page = await render();

      expect(page.querySelector('app-first-tenant')).toBeNull();
      expect(page.textContent).toContain('You are not a member of any team yet.');
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
      stubMyTickets();
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
      stubMyTickets();
      const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
      const http = TestBed.inject(HttpTestingController);
      const fixture = TestBed.createComponent(Home);
      fixture.detectChanges();
      http.expectOne('/api/v1/me').flush(person(true));
      // A global administrator's session lists the installation's tenants: there are none.
      const listed = (request: { method: string; url: string }) =>
        request.method === 'GET' && request.url === '/api/v1/teams';
      await new Promise((resolve) => setTimeout(resolve));
      TestBed.tick();
      http.expectOne(listed).flush({ items: [], next_cursor: null });
      await fixture.whenStable();
      fixture.detectChanges();
      const page = fixture.nativeElement as HTMLElement;
      expect(page.querySelector('[data-testid="first-tenant"] h1')?.textContent).toBe(
        'Create the first team',
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
      expect(created.request.url).toBe('/api/v1/teams');
      expect(created.request.body).toEqual({ slug: 'acme', name: 'Acme Corp' });
      const tenant: Team = {
        slug: 'acme',
        name: 'Acme Corp',
        version: 1,
        time_visible_to_members: false,
        members_create_projects: true,
        created_at: '2026-10-03T10:00:00Z',
        updated_at: '2026-10-03T10:00:00Z',
      };
      created.flush(tenant);
      // The request for the person is not answered yet, which keeps the fixture from being stable.
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();

      // The person is loaded again, because the new membership is what the tenant's pages read, and
      // the installation's tenants, which the new one joins.
      http.expectOne('/api/v1/me').flush(person(true, [acme]));
      http
        .expectOne(listed)
        .flush({ items: [{ slug: 'acme', name: 'Acme Corp', role: 'admin' }], next_cursor: null });
      await fixture.whenStable();
      expect(navigate).toHaveBeenCalledWith(['/t', 'acme']);
      http.verify();
    });
  });
});
