import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../api/api-configuration';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { followPages, InboxService } from './inbox.service';

describe('followPages', () => {
  it('follows the cursor for as many pages as it is asked for, and says whether there are more', async () => {
    const page = vi.fn(async (cursor: string | undefined) =>
      cursor === undefined
        ? { items: [1, 2], next_cursor: 'c1' }
        : cursor === 'c1'
          ? { items: [3], next_cursor: 'c2' }
          : { items: [4], next_cursor: null },
    );

    expect(await followPages(2, page)).toEqual({ items: [1, 2, 3], nextCursor: 'c2' });
    expect(page.mock.calls.map(([cursor]) => cursor)).toEqual([undefined, 'c1']);
    expect(await followPages(5, page)).toEqual({ items: [1, 2, 3, 4], nextCursor: null });
  });
});

describe('InboxService', () => {
  let service: InboxService;
  let http: HttpTestingController;
  let stream: Subject<StreamEvent>;

  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  const countRequest = () =>
    http.expectOne(
      (request) =>
        request.method === 'GET' &&
        request.url === '/api/v1/me/inbox' &&
        request.params.get('limit') === '1',
    );

  beforeEach(async () => {
    vi.useFakeTimers();
    stream = new Subject<StreamEvent>();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: EventStreamService, useValue: { events: stream.asObservable() } },
      ],
    });
    service = TestBed.inject(InboxService);
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

  it('reads the unread count once the person is known, from one entry of the inbox', async () => {
    expect(service.count()).toBe(0);

    countRequest().flush({ items: [], next_cursor: null, unread: 4 });
    await settle();

    expect(service.count()).toBe(4);
  });

  it('takes the count the person-level stream tells, without asking (docs/adr/0054 D2)', async () => {
    countRequest().flush({ items: [], next_cursor: null, unread: 4 });
    await settle();

    stream.next({ name: 'inbox.changed', unread: 7 });

    expect(service.count()).toBe(7);
  });

  it.each(['resync', 'poll'] as const)('reads the count again on %s', async (name) => {
    countRequest().flush({ items: [], next_cursor: null, unread: 4 });
    await settle();

    stream.next({ name });
    await settle();
    // The session reads the person again on the same events; the count is the inbox's own.
    http.expectOne('/api/v1/me').flush({ id: 'p1', display_name: 'Hans', memberships: [] });
    countRequest().flush({ items: [], next_cursor: null, unread: 2 });
    await settle();

    expect(service.count()).toBe(2);
  });

  it.each<[string, StreamEvent]>([
    [
      'a tenant the person left, in any of their tenants',
      { name: 'membership.changed', id: 'e1', tenant: 'beta', personId: 'p1' },
    ],
    [
      'a project restricted in any of their tenants',
      { name: 'membership.changed', id: 'e1', tenant: 'beta', projectId: 'j1' },
    ],
  ])(
    'reads the count again on %s, which tells the inbox nothing (docs/adr/0054 D1)',
    async (_what, event) => {
      countRequest().flush({ items: [], next_cursor: null, unread: 4 });
      await settle();

      stream.next(event);
      await settle();
      // The session reads the person again on an act that names them.
      for (const request of http.match('/api/v1/me')) {
        request.flush({ id: 'p1', display_name: 'Hans', memberships: [] });
      }
      countRequest().flush({ items: [], next_cursor: null, unread: 1 });
      await settle();

      expect(service.count()).toBe(1);
    },
  );

  it("leaves the count alone on somebody else's membership", async () => {
    countRequest().flush({ items: [], next_cursor: null, unread: 4 });
    await settle();

    stream.next({ name: 'membership.changed', id: 'e1', tenant: 'beta', personId: 'p2' });
    await settle();

    http.expectNone('/api/v1/me/inbox');
  });

  it("sends the count's weak ETag on a poll and keeps the count on a 304 (docs/adr/0054 D7)", async () => {
    countRequest().flush(
      { items: [], next_cursor: null, unread: 4 },
      { headers: { ETag: 'W/"four"' } },
    );
    await settle();

    stream.next({ name: 'poll' });
    await settle();
    http.expectOne('/api/v1/me').flush({ id: 'p1', display_name: 'Hans', memberships: [] });
    const again = countRequest();
    expect(again.request.headers.get('If-None-Match')).toBe('W/"four"');
    again.flush(null, { status: 304, statusText: 'Not Modified' });
    await settle();

    expect(service.unread.status()).toBe('resolved');
    expect(service.count()).toBe(4);
  });

  it('marks one notification read and takes the count of the answer', async () => {
    countRequest().flush({ items: [], next_cursor: null, unread: 4 });
    await settle();

    const done = service.markRead('n1');
    const put = http.expectOne('/api/v1/me/inbox/n1/read');
    expect(put.request.method).toBe('PUT');
    put.flush({ unread: 3 });
    await done;

    expect(service.count()).toBe(3);
  });

  it('marks every notification up to the newest seen read (docs/adr/0020 D6)', async () => {
    countRequest().flush({ items: [], next_cursor: null, unread: 4 });
    await settle();

    const done = service.markAllRead('n9');
    const put = http.expectOne('/api/v1/me/inbox/read');
    expect(put.request.method).toBe('PUT');
    expect(put.request.body).toEqual({ through: 'n9' });
    put.flush({ unread: 1 });
    await done;

    expect(service.count()).toBe(1);
  });
});
