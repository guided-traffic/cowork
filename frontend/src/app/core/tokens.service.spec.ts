import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Project, Token, TokenCreated } from '../api/models';
import { TokensService } from './tokens.service';

const token = (id: string, overrides: Partial<Token> = {}): Token => ({
  id,
  name: `Token ${id}`,
  scope: 'write',
  agent: false,
  capabilities: [],
  created_at: '2026-10-01T10:00:00Z',
  expires_at: '2026-12-30T10:00:00Z',
  last_used_on: null,
  revoked_at: null,
  restricted_tenant: null,
  restricted_project_id: null,
  state: 'active',
  ...overrides,
});

const project = (key: string, overrides: Partial<Project> = {}): Project => ({
  id: `id-${key}`,
  key,
  name: `Project ${key}`,
  description: '',
  restricted: false,
  created_at: '2026-10-01T10:00:00Z',
  updated_at: '2026-10-01T10:00:00Z',
  version: 1,
  wip_limits: {},
  ...overrides,
});

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const listUrl = '/api/v1/me/tokens';
const plaintext = `cwk_${'A'.repeat(43)}`;

const rejection = (promise: Promise<unknown>) =>
  promise.then(
    () => null,
    (error: unknown) => error,
  );

describe('TokensService', () => {
  let service: TokensService;
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
  const ids = () => service.list().map((entry) => entry.id);

  /** One page of the person's tokens, with the cursor that asked for it. */
  const page = (cursor: string | null = null) =>
    http.expectOne(
      (request) =>
        request.method === 'GET' &&
        request.url === listUrl &&
        request.params.get('cursor') === cursor,
    );
  /** The page that the loader asks for once the previous one was taken, which is a promise away. */
  const nextPage = async (cursor: string) => {
    await settle();
    return page(cursor);
  };
  const pageOf = (names: string[], next: string | null) => ({
    items: names.map((name) => token(name)),
    next_cursor: next,
  });

  /** One page of the projects of a tenant, with the cursor that asked for it. */
  const projectPage = (tenant: string, cursor: string | null = null) =>
    http.expectOne(
      (request) =>
        request.method === 'GET' &&
        request.url === `/api/v1/tenants/${tenant}/projects` &&
        request.params.get('cursor') === cursor,
    );

  beforeEach(() => {
    vi.useFakeTimers();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideApiConfiguration('')],
    });
    service = TestBed.inject(TokensService);
    http = TestBed.inject(HttpTestingController);
    TestBed.tick();
  });

  afterEach(() => {
    try {
      http.verify();
    } finally {
      vi.useRealTimers();
      TestBed.resetTestingModule();
    }
  });

  describe('the list', () => {
    it('asks for the first page, 200 at a time, at /api/v1 without a doubled slash', async () => {
      const first = page();

      expect(first.request.url).toBe('/api/v1/me/tokens');
      expect(first.request.params.get('limit')).toBe('200');
      first.flush(pageOf([], null));
      await settle();
    });

    it('lists nothing yet while it loads, and the tokens when they arrive', async () => {
      const first = page();

      expect(service.tokens.status()).toBe('loading');
      expect(service.list()).toEqual([]);

      first.flush(pageOf(['a', 'b'], null));
      await settle();
      expect(service.tokens.status()).toBe('resolved');
      expect(ids()).toEqual(['a', 'b']);
    });

    it('follows the cursor to every page and lists the tokens in the order of the pages', async () => {
      page().flush(pageOf(['a', 'b'], 'c1'));
      (await nextPage('c1')).flush(pageOf(['c'], 'c2'));
      const last = await nextPage('c2');
      expect(last.request.params.get('limit')).toBe('200');
      last.flush(pageOf(['d'], null));
      await settle();

      expect(ids()).toEqual(['a', 'b', 'c', 'd']);
    });

    it('lists nothing until the last page has arrived', async () => {
      page().flush(pageOf(['a'], 'c1'));
      const second = await nextPage('c1');

      expect(service.tokens.status()).toBe('loading');
      expect(service.list()).toEqual([]);

      second.flush(pageOf(['b'], null));
      await settle();
      expect(ids()).toEqual(['a', 'b']);
    });

    it('keeps each token as the API lists it, with its state', async () => {
      const dead = token('x', { state: 'revoked', revoked_at: '2026-10-02T10:00:00Z' });
      page().flush({
        items: [token('w', { agent: true, capabilities: ['rank'] }), dead],
        next_cursor: null,
      });
      await settle();

      expect(service.list().map((entry) => entry.state)).toEqual(['active', 'revoked']);
      expect(service.list()[0].capabilities).toEqual(['rank']);
    });

    it('lists nothing when a page fails, and says why', async () => {
      page().flush(
        { type: 'about:blank', title: 'Internal', status: 500, code: 'internal' },
        { status: 500, statusText: 'Internal Server Error' },
      );
      await settle();

      expect(service.tokens.status()).toBe('error');
      expect(service.list()).toEqual([]);
    });

    it('keeps listing the tokens while it loads them again', async () => {
      page().flush(pageOf(['a'], null));
      await settle();

      expect(service.tokens.reload()).toBe(true);
      TestBed.tick();
      const again = page();

      expect(service.tokens.status()).toBe('reloading');
      expect(ids()).toEqual(['a']);
      again.flush(pageOf(['a', 'b'], null));
      await settle();
      expect(ids()).toEqual(['a', 'b']);
    });
  });

  describe('create', () => {
    const body = { name: 'ci', scope: 'write' as const, lifetime_days: 30 };
    const created = (overrides: Partial<TokenCreated> = {}): TokenCreated => ({
      ...token('new', { name: 'ci' }),
      token: plaintext,
      ...overrides,
    });
    const write = () =>
      http.expectOne((request) => request.method === 'POST' && request.url === listUrl);

    beforeEach(async () => {
      page().flush(pageOf(['a'], null));
      await settle();
    });

    it('posts the token to the person, with the body as given, and hands back the answer with its plaintext', async () => {
      const done = service.create(body);

      const sent = write();
      expect(sent.request.body).toEqual(body);
      expect(sent.request.headers.has('If-Match')).toBe(false);
      sent.flush(created());

      expect((await done).token).toBe(plaintext);
      await settle();
      page().flush(pageOf(['a', 'new'], null));
    });

    it('sends an Idempotency-Key of its own for every act (docs/adr/0045 D3)', async () => {
      const first = service.create(body);
      const one = write();
      one.flush(created());
      await first;
      await settle();
      page().flush(pageOf(['a', 'new'], null));
      await settle();

      const second = service.create(body);
      const two = write();
      two.flush(created());
      await second;
      await settle();
      page().flush(pageOf(['a', 'new'], null));

      const keys = [one, two].map((sent) => sent.request.headers.get('Idempotency-Key'));
      expect(keys[0]).toMatch(uuid);
      expect(keys[1]).toMatch(uuid);
      expect(keys[0]).not.toBe(keys[1]);
    });

    it('loads the list again after the answer, so that the new token shows', async () => {
      const done = service.create(body);
      await settle();
      http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
      write().flush(created());
      await done;

      await settle();
      const again = page();
      expect(service.tokens.status()).toBe('reloading');
      again.flush({ items: [token('a'), token('new', { name: 'ci' })], next_cursor: null });
      await settle();

      expect(ids()).toEqual(['a', 'new']);
    });

    it('does not put the answer of the creation into the list, which would show the plaintext until the list is loaded again', async () => {
      const done = service.create(body);
      write().flush(created());
      const answer = await done;
      await settle();

      // The list is asked for again but has not answered: what it holds now is what it held before,
      // and the answer that carries the plaintext is nowhere in it.
      const again = page();
      expect(answer.token).toBe(plaintext);
      expect(ids()).toEqual(['a']);
      expect(JSON.stringify(service.list())).not.toContain(plaintext);
      expect(JSON.stringify(service.tokens.value())).not.toContain(plaintext);
      again.flush({ items: [token('a'), token('new', { name: 'ci' })], next_cursor: null });
      await settle();
      expect(JSON.stringify(service.list())).not.toContain(plaintext);
    });

    it('answers without a plaintext when the answer is a replay, and says nothing more', async () => {
      const done = service.create(body);
      write().flush(created({ token: undefined }));

      expect((await done).token).toBeUndefined();
      await settle();
      page().flush(pageOf(['a'], null));
    });

    it.each([
      [403, 'session_required'],
      [422, 'validation_failed'],
      [500, 'internal'],
    ])(
      'rejects with the HTTP error of a %i and does not load the list again',
      async (status, code) => {
        const outcome = rejection(service.create(body));

        write().flush(
          { type: 'about:blank', title: 'Refused', status, code },
          { status, statusText: `Status ${status}` },
        );
        const error = await outcome;
        await settle();

        expect(error).toBeInstanceOf(HttpErrorResponse);
        expect((error as HttpErrorResponse).status).toBe(status);
        http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
        expect(ids()).toEqual(['a']);
      },
    );

    it('loads the list once more when it ends while the list is loading, because that answer may predate the token', async () => {
      service.tokens.reload();
      await settle();
      const inFlight = page();

      const done = service.create(body);
      write().flush(created());
      await done;
      await settle();
      inFlight.flush(pageOf(['a'], null));
      await settle();

      page().flush(pageOf(['a', 'new'], null));
    });
  });

  describe('revoke', () => {
    const write = (id: string) =>
      http.expectOne(
        (request) => request.method === 'DELETE' && request.url === `${listUrl}/${id}`,
      );

    beforeEach(async () => {
      page().flush(pageOf(['a', 'b'], null));
      await settle();
    });

    it('deletes the token by its id, with no body, no version and no key', async () => {
      const done = service.revoke(token('b'));

      const sent = write('b');
      expect(sent.request.body).toBeNull();
      expect(sent.request.headers.has('If-Match')).toBe(false);
      expect(sent.request.headers.has('Idempotency-Key')).toBe(false);
      sent.flush(null, { status: 204, statusText: 'No Content' });

      await done;
      await settle();
      page().flush({ items: [token('a'), token('b', { state: 'revoked' })], next_cursor: null });
    });

    it('loads the list again, so that the token shows as revoked', async () => {
      const done = service.revoke(token('b'));
      write('b').flush(null, { status: 204, statusText: 'No Content' });
      await done;

      await settle();
      page().flush({
        items: [token('a'), token('b', { state: 'revoked', revoked_at: '2026-10-03T09:00:00Z' })],
        next_cursor: null,
      });
      await settle();

      expect(service.list().map((entry) => entry.state)).toEqual(['active', 'revoked']);
    });

    it.each([
      [404, 'not_found'],
      [403, 'forbidden'],
    ])(
      'rejects with the HTTP error of a %i and does not load the list again',
      async (status, code) => {
        const outcome = rejection(service.revoke(token('b')));

        write('b').flush(
          { type: 'about:blank', title: 'Refused', status, code },
          { status, statusText: `Status ${status}` },
        );
        const error = await outcome;
        await settle();

        expect((error as HttpErrorResponse).status).toBe(status);
        http.expectNone((request) => request.method === 'GET' && request.url === listUrl);
      },
    );
  });

  describe('projectsOf', () => {
    beforeEach(async () => {
      page().flush(pageOf([], null));
      await settle();
    });

    it('lists the projects of the tenant named, every page of them, 200 at a time', async () => {
      const done = service.projectsOf('acme');

      const first = projectPage('acme');
      expect(first.request.params.get('limit')).toBe('200');
      expect(first.request.params.has('include_archived')).toBe(false);
      first.flush({ items: [project('A'), project('B')], next_cursor: 'c1' });
      await settle();
      projectPage('acme', 'c1').flush({ items: [project('C')], next_cursor: null });

      expect((await done).map((each) => each.key)).toEqual(['A', 'B', 'C']);
    });

    it('asks for the archived ones too when it is told to', async () => {
      const done = service.projectsOf('acme', true);

      const first = projectPage('acme');
      expect(first.request.params.get('include_archived')).toBe('true');
      first.flush({ items: [], next_cursor: null });

      expect(await done).toEqual([]);
    });

    it('rejects with the HTTP error when the projects cannot be read', async () => {
      const outcome = rejection(service.projectsOf('acme'));

      projectPage('acme').flush(
        { type: 'about:blank', title: 'Not found', status: 404, code: 'not_found' },
        { status: 404, statusText: 'Not Found' },
      );

      expect(((await outcome) as HttpErrorResponse).status).toBe(404);
    });
  });

  describe('the keys of the projects a token is restricted to', () => {
    const restricted = (id: string, tenant: string, projectId: string) =>
      token(id, { restricted_tenant: tenant, restricted_project_id: projectId });

    it('asks for no project while no token is restricted to one', async () => {
      page().flush({
        items: [token('a'), token('b', { restricted_tenant: 'acme' })],
        next_cursor: null,
      });
      await settle();

      expect(service.projectKeys.status()).toBe('idle');
      expect(service.keyOfProject('id-COW')).toBeUndefined();
    });

    it('finds the key of each project by its id, including an archived one, tenant by tenant', async () => {
      page().flush({
        items: [
          restricted('a', 'acme', 'id-COW'),
          restricted('b', 'acme', 'id-OPS'),
          restricted('c', 'globex', 'id-GLX'),
        ],
        next_cursor: null,
      });
      await settle();

      const acme = projectPage('acme');
      expect(acme.request.params.get('include_archived')).toBe('true');
      acme.flush({ items: [project('COW'), project('OPS'), project('OLD')], next_cursor: null });
      await settle();
      projectPage('globex').flush({ items: [project('GLX')], next_cursor: null });
      await settle();

      expect(service.keyOfProject('id-COW')).toBe('COW');
      expect(service.keyOfProject('id-OPS')).toBe('OPS');
      expect(service.keyOfProject('id-GLX')).toBe('GLX');
      expect(service.keyOfProject('id-NOPE')).toBeUndefined();
    });

    it('asks once for a tenant that several tokens name', async () => {
      page().flush({
        items: [restricted('a', 'acme', 'id-COW'), restricted('b', 'acme', 'id-COW')],
        next_cursor: null,
      });
      await settle();

      projectPage('acme').flush({ items: [project('COW')], next_cursor: null });
      await settle();

      expect(service.keyOfProject('id-COW')).toBe('COW');
    });

    it('leaves out the ids of a tenant that cannot be read, and still finds the others', async () => {
      page().flush({
        items: [restricted('a', 'acme', 'id-COW'), restricted('b', 'globex', 'id-GLX')],
        next_cursor: null,
      });
      await settle();

      projectPage('acme').flush(
        { type: 'about:blank', title: 'Not found', status: 404, code: 'not_found' },
        { status: 404, statusText: 'Not Found' },
      );
      await settle();
      projectPage('globex').flush({ items: [project('GLX')], next_cursor: null });
      await settle();

      expect(service.projectKeys.status()).toBe('resolved');
      expect(service.keyOfProject('id-COW')).toBeUndefined();
      expect(service.keyOfProject('id-GLX')).toBe('GLX');
    });

    it('finds no key before the lookup has answered', async () => {
      page().flush({ items: [restricted('a', 'acme', 'id-COW')], next_cursor: null });
      await settle();

      const lookup = projectPage('acme');
      expect(service.projectKeys.status()).toBe('loading');
      expect(service.keyOfProject('id-COW')).toBeUndefined();

      lookup.flush({ items: [project('COW')], next_cursor: null });
      await settle();
    });

    it('does not ask again when the list reloads with the same restrictions, but does for a new one', async () => {
      page().flush({ items: [restricted('a', 'acme', 'id-COW')], next_cursor: null });
      await settle();
      projectPage('acme').flush({ items: [project('COW'), project('OPS')], next_cursor: null });
      await settle();

      service.tokens.reload();
      await settle();
      page().flush({
        items: [restricted('a', 'acme', 'id-COW'), token('b')],
        next_cursor: null,
      });
      await settle();
      http.expectNone((request) => request.url === '/api/v1/tenants/acme/projects');

      service.tokens.reload();
      await settle();
      page().flush({
        items: [restricted('a', 'acme', 'id-COW'), restricted('c', 'acme', 'id-OPS')],
        next_cursor: null,
      });
      await settle();
      projectPage('acme').flush({ items: [project('COW'), project('OPS')], next_cursor: null });
      await settle();

      expect(service.keyOfProject('id-OPS')).toBe('OPS');
    });
  });
});
