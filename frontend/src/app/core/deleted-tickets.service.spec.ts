import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { computed, signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';
import { provideApiConfiguration } from '../api/api-configuration';
import { DeletedTicket, DeletedTicketList, Ticket } from '../api/models';
import { DeletedTicketsService } from './deleted-tickets.service';
import { EntityCache } from './entity-cache';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { SessionService } from './session.service';
import { TicketsService } from './tickets.service';

const entry = (number: number): DeletedTicket => ({
  key: `acme/COW-${number}`,
  project: 'COW',
  number,
  type: 'task',
  title: `Ticket ${number}`,
  state: 'filed',
  confidential: false,
  deleted_at: '2026-10-01T10:00:00Z',
  deleted_by: { id: 'p-ada', display_name: 'Ada', username: 'ada' },
  purge_at: '2026-10-31T10:00:00Z',
});

const pageOf = (numbers: number[], next: string | null): DeletedTicketList => ({
  items: numbers.map(entry),
  next_cursor: next,
});

describe('DeletedTicketsService', () => {
  let service: DeletedTicketsService;
  let http: HttpTestingController;
  let tenant: WritableSignal<string | null>;
  let role: WritableSignal<'admin' | 'member'>;
  let events: Subject<StreamEvent>;
  let cache: EntityCache<Ticket>;

  const settle = async () => {
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
  };
  const bin = (cursor: string | null = null) =>
    http.expectOne(
      (request) =>
        request.method === 'GET' &&
        request.url === '/api/v1/teams/acme/deleted-tickets' &&
        request.params.get('cursor') === cursor,
    );
  const ticketEvent = (kind: string, key = 'acme/COW-1'): StreamEvent => ({
    name: 'ticket.changed',
    id: 'e1',
    key,
    version: 2,
    kind,
  });

  beforeEach(() => {
    vi.useFakeTimers();
    tenant = signal<string | null>('acme');
    role = signal<'admin' | 'member'>('admin');
    events = new Subject<StreamEvent>();
    cache = new EntityCache<Ticket>();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        {
          provide: SessionService,
          useValue: { tenant, membership: computed(() => ({ role: role() })) },
        },
        { provide: EventStreamService, useValue: { events } },
        { provide: TicketsService, useValue: { cache } },
      ],
    });
    service = TestBed.inject(DeletedTicketsService);
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

  it('reads every page of the bin for a tenant administrator', async () => {
    TestBed.tick();
    bin().flush(pageOf([3, 2], 'c1'));
    await settle();
    bin('c1').flush(pageOf([1], null));
    await settle();

    expect(service.list().map((e) => e.number)).toEqual([3, 2, 1]);
  });

  it('asks for nothing for anybody else, which would only be a 403', async () => {
    role.set('member');
    await settle();

    expect(service.bin.status()).toBe('idle');
  });

  it('loads again on a deletion, a restoration and a purge of its tenant, and on nothing else', async () => {
    TestBed.tick();
    bin().flush(pageOf([], null));
    await settle();

    for (const kind of ['deleted', 'restored', 'purged']) {
      events.next(ticketEvent(kind));
      await settle();
      bin().flush(pageOf([1], null));
      await settle();
    }
    events.next(ticketEvent('transitioned'));
    events.next(ticketEvent('deleted', 'globex/COW-1'));
    await settle();
    http.expectNone('/api/v1/teams/acme/deleted-tickets');
  });

  it('restores a ticket into the cache and purges one by its short key', async () => {
    TestBed.tick();
    bin().flush(pageOf([1, 2], null));
    await settle();

    const restored = service.restore(entry(1));
    http
      .expectOne({ method: 'PUT', url: '/api/v1/teams/acme/deleted-tickets/COW-1/restore' })
      .flush({ key: 'acme/COW-1', version: 5 } as Ticket);
    await restored;
    expect(cache.value('acme/COW-1')?.version).toBe(5);
    await settle();
    bin().flush(pageOf([2], null));
    await settle();

    const purged = service.purge(entry(2));
    http
      .expectOne({ method: 'DELETE', url: '/api/v1/teams/acme/deleted-tickets/COW-2' })
      .flush(null);
    await purged;
    await settle();
    bin().flush(pageOf([], null));
    await settle();
    expect(service.list()).toEqual([]);
  });
});
