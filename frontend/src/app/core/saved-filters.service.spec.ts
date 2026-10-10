import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../api/api-configuration';
import { SavedFilter, SavedFilterList } from '../api/models';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { SavedFiltersService } from './saved-filters.service';
import { SessionService } from './session.service';

const filter = (id: string, overrides: Partial<SavedFilter> = {}): SavedFilter => ({
  id,
  name: `Filter ${id}`,
  owner: { id: 'p-ada', display_name: 'Ada', username: 'ada' },
  shared: false,
  parameters: { severity: ['high'] },
  redacted: false,
  warnings: [],
  version: 3,
  created_at: '2026-10-01T10:00:00Z',
  updated_at: '2026-10-01T10:00:00Z',
  ...overrides,
});

const pageOf = (ids: string[], next: string | null): SavedFilterList => ({
  items: ids.map((id) => filter(id)),
  next_cursor: next,
});

describe('SavedFiltersService', () => {
  let service: SavedFiltersService;
  let http: HttpTestingController;
  let workTenant: WritableSignal<string | null>;
  let events: Subject<StreamEvent>;

  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  const list = (cursor: string | null = null) =>
    http.expectOne(
      (request) =>
        request.method === 'GET' &&
        request.url === '/api/v1/teams/acme/filters' &&
        request.params.get('cursor') === cursor,
    );

  beforeEach(() => {
    vi.useFakeTimers();
    workTenant = signal<string | null>('acme');
    events = new Subject<StreamEvent>();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: SessionService, useValue: { tenant: workTenant, workTenant } },
        { provide: EventStreamService, useValue: { events } },
      ],
    });
    service = TestBed.inject(SavedFiltersService);
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

  it('lists every page of the tenant the pages work in, 200 at a time', async () => {
    TestBed.tick();
    const first = list();
    expect(first.request.params.get('limit')).toBe('200');
    first.flush(pageOf(['a', 'b'], 'c1'));
    await settle();
    list('c1').flush(pageOf(['c'], null));
    await settle();

    expect(service.list().map((f) => f.id)).toEqual(['a', 'b', 'c']);
  });

  it('asks for nothing where the pages work in no tenant, as under oversight', async () => {
    workTenant.set(null);
    await settle();

    expect(service.filters.status()).toBe('idle');
    expect(service.list()).toEqual([]);
  });

  it('loads again on resync and poll, which may hide a filter shared meanwhile', async () => {
    TestBed.tick();
    list().flush(pageOf(['a'], null));
    await settle();

    events.next({ name: 'poll' });
    await settle();
    list().flush(pageOf(['a', 'b'], null));
    await settle();

    expect(service.list().map((f) => f.id)).toEqual(['a', 'b']);
  });

  it('saves a filter with a key of its own and loads the list again', async () => {
    TestBed.tick();
    list().flush(pageOf([], null));
    await settle();

    const saved = service.create('Bugs', { type: ['bug'] }, true, 'key-of-the-form');
    const post = http.expectOne({ method: 'POST', url: '/api/v1/teams/acme/filters' });
    expect(post.request.headers.get('Idempotency-Key')).toBe('key-of-the-form');
    expect(post.request.body).toEqual({
      name: 'Bugs',
      parameters: { type: ['bug'] },
      shared: true,
    });
    post.flush(filter('n', { name: 'Bugs', shared: true }));
    expect((await saved).name).toBe('Bugs');
    await settle();
    list().flush(pageOf(['n'], null));
    await settle();
  });

  it('changes a filter over the version it was read in, and deletes one', async () => {
    TestBed.tick();
    list().flush(pageOf(['a'], null));
    await settle();

    const changed = service.update(filter('a'), { shared: true });
    const patch = http.expectOne({ method: 'PATCH', url: '/api/v1/teams/acme/filters/a' });
    expect(patch.request.headers.get('If-Match')).toBe('"3"');
    expect(patch.request.body).toEqual({ shared: true });
    patch.flush(filter('a', { shared: true, version: 4 }));
    expect((await changed).version).toBe(4);
    await settle();
    list().flush(pageOf(['a'], null));
    await settle();

    const removed = service.remove(filter('a'));
    http.expectOne({ method: 'DELETE', url: '/api/v1/teams/acme/filters/a' }).flush(null);
    await removed;
    await settle();
    list().flush(pageOf([], null));
    await settle();
    expect(service.list()).toEqual([]);
  });
});
