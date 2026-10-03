import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Member, MemberList } from '../api/models';
import { MembersService } from './members.service';
import { SessionService } from './session.service';

const member = (name: string, role: Member['role'] = 'member'): Member => ({
  person: { id: `0199aaaa-0000-7000-8000-${name.padStart(12, '0')}`, display_name: name },
  role,
});

const pageOf = (names: string[], next: string | null): MemberList => ({
  items: names.map((name) => member(name)),
  next_cursor: next,
});

describe('MembersService', () => {
  let service: MembersService;
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
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    service = TestBed.inject(MembersService);
    session = TestBed.inject(SessionService);
    http = TestBed.inject(HttpTestingController);
    TestBed.tick();
    http.expectOne('/api/v1/me').flush({ id: 'p1', display_name: 'Hans', memberships: [] });
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
});
