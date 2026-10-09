import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { computed, signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../api/api-configuration';
import { AttachmentConsistency } from '../api/models';
import { AttachmentConsistencyService } from './attachment-consistency.service';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { SessionService } from './session.service';

const result = (overrides: Partial<AttachmentConsistency> = {}): AttachmentConsistency => ({
  check_id: '0199a7c2-1d2e-7f00-8000-0000000000c1',
  checked_at: '2026-10-06T03:12:00Z',
  dangling: 1,
  accepted: 0,
  orphans: 2,
  orphan_bytes: 2048,
  dangling_attachments: [],
  orphaned_objects: [],
  orphan_removal: null,
  last_exported_at: null,
  ...overrides,
});

describe('AttachmentConsistencyService', () => {
  let service: AttachmentConsistencyService;
  let http: HttpTestingController;
  let role: WritableSignal<'admin' | 'member'>;
  let events: Subject<StreamEvent>;

  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  const latest = () =>
    http.expectOne(
      (request) =>
        request.method === 'GET' && request.url === '/api/v1/tenants/acme/attachment-consistency',
    );

  beforeEach(() => {
    vi.useFakeTimers();
    role = signal<'admin' | 'member'>('admin');
    events = new Subject<StreamEvent>();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        {
          provide: SessionService,
          useValue: { tenant: signal('acme'), membership: computed(() => ({ role: role() })) },
        },
        { provide: EventStreamService, useValue: { events } },
      ],
    });
    service = TestBed.inject(AttachmentConsistencyService);
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

  it('reads the latest check for a tenant administrator', async () => {
    TestBed.tick();
    latest().flush(result(), { headers: { ETag: 'W/"c1"' } });
    await settle();

    expect(service.latest.value()?.orphans).toBe(2);
  });

  it('asks for nothing for anybody else, which would only be a 403', async () => {
    role.set('member');
    await settle();

    expect(service.latest.status()).toBe('idle');
  });

  it('asks again on a gap in the stream and a poll, with the tag it holds', async () => {
    TestBed.tick();
    latest().flush(result(), { headers: { ETag: 'W/"c1"' } });
    await settle();

    events.next({ name: 'poll' });
    await settle();
    const again = latest();
    expect(again.request.headers.get('If-None-Match')).toBe('W/"c1"');
    again.flush(null, { status: 304, statusText: 'Not Modified' });
    await settle();
    // A 304 keeps what is shown.
    expect(service.latest.value()?.orphans).toBe(2);

    events.next({ name: 'resync' });
    await settle();
    latest().flush(result({ orphans: 0 }), { headers: { ETag: 'W/"c2"' } });
    await settle();
    expect(service.latest.value()?.orphans).toBe(0);
  });

  it('accepts the loss and removes the objects of the check it names, and reads the check again', async () => {
    TestBed.tick();
    latest().flush(result());
    await settle();

    const accepted = service.acceptLoss('c1');
    const acceptance = http.expectOne({
      method: 'POST',
      url: '/api/v1/tenants/acme/attachment-consistency/dangling-acceptance',
    });
    expect(acceptance.request.body).toEqual({ check_id: 'c1' });
    acceptance.flush({ accepted: 1 });
    expect(await accepted).toEqual({ accepted: 1 });
    await settle();
    latest().flush(result({ dangling: 0, accepted: 1 }));
    await settle();

    const removed = service.removeOrphans('c1');
    const removal = http.expectOne({
      method: 'POST',
      url: '/api/v1/tenants/acme/attachment-consistency/orphan-removal',
    });
    expect(removal.request.body).toEqual({ check_id: 'c1' });
    removal.flush({ removed: 2, kept: 0, failed: 0 });
    expect(await removed).toEqual({ removed: 2, kept: 0, failed: 0 });
    await settle();
    latest().flush(result({ dangling: 0, accepted: 1, orphans: 0 }));
    await settle();
    expect(service.latest.value()?.orphans).toBe(0);
  });

  it('reads the check again after an export, which moves the time of the last one', async () => {
    TestBed.tick();
    latest().flush(result(), { headers: { ETag: 'W/"c1"' } });
    await settle();

    service.exported();
    await settle();
    const again = latest();
    expect(again.request.headers.get('If-None-Match')).toBe('W/"c1"');
    again.flush(result({ last_exported_at: '2026-10-06T10:00:00Z' }), {
      headers: { ETag: 'W/"c2"' },
    });
    await settle();
    expect(service.latest.value()?.last_exported_at).toBe('2026-10-06T10:00:00Z');
  });
});
