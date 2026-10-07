import { TestBed } from '@angular/core/testing';
import {
  changesMemberships,
  changesVisibility,
  EVENT_SOURCE,
  EventSourceLike,
  EventStreamService,
  fallback,
  isImport,
  MembershipEvent,
  ofTenant,
  StreamEvent,
  StreamStatus,
  ticketEventNames,
} from './event-stream.service';

const connecting = 0;
const closed = 2;
const pollEvery = 15_000;
const retryEvery = 60_000;

/** An EventSource that records what the service asks of it and fires what a test tells it to. */
class FakeEventSource implements EventSourceLike {
  readyState = connecting;
  onopen: ((event: Event) => unknown) | null = null;
  onerror: ((event: Event) => unknown) | null = null;
  closeCalls = 0;
  private readonly listeners = new Map<string, ((event: MessageEvent<string>) => void)[]>();

  constructor(readonly url: string) {}

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }

  close(): void {
    this.closeCalls++;
    this.readyState = closed;
  }

  get isClosed(): boolean {
    return this.closeCalls > 0;
  }

  get eventNames(): string[] {
    return [...this.listeners.keys()];
  }

  open(): void {
    this.readyState = 1;
    this.onopen?.(new Event('open'));
  }

  /** An error; the browser keeps reconnecting by itself unless it leaves the source CLOSED. */
  fail(readyState = connecting): void {
    this.readyState = readyState;
    this.onerror?.(new Event('error'));
  }

  send(name: string, data: string, lastEventId = ''): void {
    for (const listener of this.listeners.get(name) ?? []) {
      listener(new MessageEvent<string>(name, { data, lastEventId }));
    }
  }

  sendTicket(
    name: string,
    key: string,
    version: number,
    kind = 'transitioned',
    lastEventId = '',
  ): void {
    this.send(name, JSON.stringify({ key, version, kind }), lastEventId);
  }
}

function setVisibility(state: DocumentVisibilityState): void {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('EventStreamService', () => {
  let service: EventStreamService;
  let sources: FakeEventSource[];
  let events: StreamEvent[];
  /** What the status was at the moment each event reached the subscriber. */
  let statusAt: Map<StreamEvent, StreamStatus>;

  const names = () => events.map((event) => event.name);

  beforeEach(() => {
    vi.useFakeTimers();
    sources = [];
    events = [];
    statusAt = new Map();
    TestBed.configureTestingModule({
      providers: [
        {
          provide: EVENT_SOURCE,
          useValue: (url: string) => {
            const source = new FakeEventSource(url);
            sources.push(source);
            return source;
          },
        },
      ],
    });
    service = TestBed.inject(EventStreamService);
    service.events.subscribe((event) => {
      events.push(event);
      statusAt.set(event, service.status());
    });
  });

  afterEach(() => {
    Reflect.deleteProperty(document, 'visibilityState');
    vi.useRealTimers();
  });

  it('uses the numbers of docs/adr/0054 D7: three failures, a poll every 15 seconds, a retry every minute', () => {
    expect(fallback).toEqual({ failures: 3, pollEvery, retryEvery });
  });

  describe('connect', () => {
    it('is idle and holds no stream before it follows a tenant', () => {
      expect(service.status()).toBe('idle');
      expect(sources).toEqual([]);
    });

    it('opens the stream of the tenant and is connecting until it is open', () => {
      service.connect('acme');

      expect(sources.map((source) => source.url)).toEqual(['/api/v1/tenants/acme/events?me=true']);
      expect(service.status()).toBe('connecting');
    });

    it('writes the tenant into the path as one segment', () => {
      service.connect('a b/c');

      expect(sources[0].url).toBe('/api/v1/tenants/a%20b%2Fc/events?me=true');
    });

    it('is live once the stream is open', () => {
      service.connect('acme');

      sources[0].open();

      expect(service.status()).toBe('live');
    });

    it('asks for no resync when the first stream of a tenant opens, because nothing was missed', () => {
      service.connect('acme');

      sources[0].open();

      expect(events).toEqual([]);
    });

    it('asks for no resync when the browser reconnects the stream by itself, which replays from Last-Event-ID', () => {
      service.connect('acme');
      sources[0].open();

      sources[0].fail();
      sources[0].open();

      expect(service.status()).toBe('live');
      expect(events).toEqual([]);
    });

    it('asks for no resync when the stream opens after an error before it was ever open', () => {
      service.connect('acme');

      sources[0].fail();
      sources[0].open();

      expect(service.status()).toBe('live');
      expect(events).toEqual([]);
    });

    it('listens to the events that name a ticket, to membership.changed, to project.changed, to inbox.changed, to resync and to unavailable (docs/adr/0054 D2)', () => {
      service.connect('acme');

      expect([...sources[0].eventNames].sort()).toEqual(
        [
          ...ticketEventNames,
          'membership.changed',
          'project.changed',
          'inbox.changed',
          'resync',
          'unavailable',
        ].sort(),
      );
      expect([...ticketEventNames].sort()).toEqual([
        'comment.changed',
        'interest.changed',
        'link.changed',
        'pull_request.changed',
        'question.changed',
        'ticket.changed',
      ]);
    });

    it('does nothing when it is asked for the tenant it already follows', () => {
      service.connect('acme');
      sources[0].open();

      service.connect('acme');

      expect(sources).toHaveLength(1);
      expect(sources[0].isClosed).toBe(false);
      expect(service.status()).toBe('live');
    });

    it('closes the stream and goes idle for null', () => {
      service.connect('acme');
      sources[0].open();

      service.connect(null);

      expect(sources[0].isClosed).toBe(true);
      expect(service.status()).toBe('idle');
    });

    it('does nothing for null while it follows no tenant', () => {
      service.connect(null);

      expect(service.status()).toBe('idle');
      expect(sources).toEqual([]);
    });

    it('closes the stream of the old tenant and opens the one of the new tenant', () => {
      service.connect('acme');
      sources[0].open();

      service.connect('globex');

      expect(sources[0].isClosed).toBe(true);
      expect(sources.map((source) => source.url)).toEqual([
        '/api/v1/tenants/acme/events?me=true',
        '/api/v1/tenants/globex/events?me=true',
      ]);
      expect(service.status()).toBe('connecting');
    });

    it('does not carry the failures of one tenant over to the next', () => {
      service.connect('acme');
      sources[0].fail();
      sources[0].fail();

      service.connect('globex');
      sources[1].fail();
      sources[1].fail();

      expect(service.status()).toBe('connecting');
      expect(sources[1].isClosed).toBe(false);
    });

    it('stops the fallback when the tenant changes', () => {
      service.connect('acme');
      for (let i = 0; i < fallback.failures; i++) sources[0].fail();
      expect(service.status()).toBe('polling');
      events.length = 0;

      service.connect('globex');
      vi.advanceTimersByTime(2 * retryEvery);

      expect(service.status()).toBe('connecting');
      expect(events).toEqual([]);
      expect(sources.map((source) => source.url)).toEqual([
        '/api/v1/tenants/acme/events?me=true',
        '/api/v1/tenants/globex/events?me=true',
      ]);
    });

    it('stops the fallback when it follows no tenant', () => {
      service.connect('acme');
      sources[0].fail(closed);
      events.length = 0;

      service.connect(null);
      vi.advanceTimersByTime(2 * retryEvery);

      expect(service.status()).toBe('idle');
      expect(events).toEqual([]);
      expect(sources).toHaveLength(1);
    });
  });

  describe('events', () => {
    beforeEach(() => {
      service.connect('acme');
      sources[0].open();
    });

    it.each(ticketEventNames)('turns a %s event into the key and version it names', (name) => {
      sources[0].send(
        name,
        JSON.stringify({ key: 'acme/VKO-12', version: 17, kind: 'transitioned' }),
        '0199aaaa-0000-7000-8000-000000000001',
      );

      expect(events).toEqual([
        {
          name,
          id: '0199aaaa-0000-7000-8000-000000000001',
          key: 'acme/VKO-12',
          version: 17,
          kind: 'transitioned',
        },
      ]);
    });

    it('carries a key and a version and nothing else, however much the payload says (docs/adr/0054 D2)', () => {
      sources[0].send(
        'ticket.changed',
        JSON.stringify({
          key: 'acme/VKO-1',
          version: 2,
          kind: 'commented',
          title: 'Secret',
          project: 'p',
        }),
        'e1',
      );

      expect(Object.keys(events[0]).sort()).toEqual(['id', 'key', 'kind', 'name', 'version']);
    });

    it('passes events on in the order they arrive, each with its own id', () => {
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2, 'edited', 'e1');
      sources[0].sendTicket('comment.changed', 'acme/VKO-1', 2, 'commented', 'e2');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-2', 5, 'transitioned', 'e3');

      expect(events.map((event) => ('id' in event ? event.id : null))).toEqual(['e1', 'e2', 'e3']);
    });

    it('turns a resync event into a resync, whatever its data says', () => {
      sources[0].send('resync', '{}');

      expect(events).toEqual([{ name: 'resync' }]);
    });

    it.each([
      ['text that is not JSON', 'not json'],
      ['an empty payload', ''],
      ['a JSON null', 'null'],
      ['a JSON object without any member', '{}'],
      ['a JSON string', '"text"'],
      ['a JSON number', '42'],
      ['a JSON boolean', 'true'],
      ['a JSON array that holds the payload', '[{"key":"acme/VKO-1","version":2,"kind":"edited"}]'],
      ['a payload with a key only', '{"key":"acme/VKO-1"}'],
      ['a payload without a key', '{"version":2,"kind":"edited"}'],
      ['a payload without a version', '{"key":"acme/VKO-1","kind":"edited"}'],
      ['a payload without a kind', '{"key":"acme/VKO-1","version":2}'],
      ['a key that is a number', '{"key":12,"version":2,"kind":"edited"}'],
      ['a key that is null', '{"key":null,"version":2,"kind":"edited"}'],
      ['a version that is a string', '{"key":"acme/VKO-1","version":"2","kind":"edited"}'],
      ['a version that is null', '{"key":"acme/VKO-1","version":null,"kind":"edited"}'],
      ['a kind that is a number', '{"key":"acme/VKO-1","version":2,"kind":7}'],
      ['a kind that is an object', '{"key":"acme/VKO-1","version":2,"kind":{}}'],
    ])('ignores %s', (_description, data) => {
      sources[0].send('ticket.changed', data);

      expect(events).toEqual([]);
    });

    it.each(ticketEventNames)(
      'ignores a payload that names no ticket in a %s event too',
      (name) => {
        sources[0].send(name, '{}');
        sources[0].send(name, 'not json');

        expect(events).toEqual([]);
      },
    );

    it('keeps listening after a payload it ignored', () => {
      sources[0].send('ticket.changed', 'not json');
      sources[0].send('ticket.changed', '{}');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2);

      expect(events).toHaveLength(1);
    });
  });

  describe('the person-level stream (docs/adr/0054 D1)', () => {
    it("is held on the person's tenant while no tenant page is open", () => {
      service.personal('acme');

      expect(sources.map((source) => source.url)).toEqual(['/api/v1/tenants/acme/events?me=true']);
      expect(service.status()).toBe('connecting');
    });

    it("follows the page's tenant over the person's, and goes back to the person's when the page leaves", () => {
      service.personal('acme');
      service.connect('globex');
      service.connect(null);

      expect(sources.map((source) => source.url)).toEqual([
        '/api/v1/tenants/acme/events?me=true',
        '/api/v1/tenants/globex/events?me=true',
        '/api/v1/tenants/acme/events?me=true',
      ]);
      expect(sources.slice(0, 2).every((source) => source.isClosed)).toBe(true);
    });

    it("keeps the stream open when the page leaves a tenant that is the person's", () => {
      service.personal('acme');
      service.connect('acme');
      sources[0].open();

      service.connect(null);

      expect(sources).toHaveLength(1);
      expect(service.status()).toBe('live');
    });

    it('holds no stream for a person who belongs to no tenant', () => {
      service.personal(null);

      expect(sources).toEqual([]);
      expect(service.status()).toBe('idle');
    });

    it('passes the unread count on', () => {
      service.connect('acme');
      sources[0].open();

      sources[0].send('inbox.changed', '{"unread":3}');

      expect(events).toEqual([{ name: 'inbox.changed', unread: 3 }]);
    });

    it.each([
      ['text that is not JSON', 'not json'],
      ['a JSON object without the count', '{}'],
      ['a count that is text', '{"unread":"3"}'],
      ['a negative count', '{"unread":-1}'],
      ['a fraction', '{"unread":1.5}'],
    ])('ignores an inbox.changed with %s', (_description, data) => {
      service.connect('acme');
      sources[0].open();

      sources[0].send('inbox.changed', data);

      expect(events).toEqual([]);
    });
  });

  describe('membership events', () => {
    beforeEach(() => {
      service.connect('acme');
      sources[0].open();
    });

    it.each<[Record<string, string>, Partial<MembershipEvent>, string]>([
      [{ person_id: 'p1' }, { personId: 'p1' }, 'a membership created, changed or removed'],
      [
        { person_id: 'p1', project_id: 'j1' },
        { personId: 'p1', projectId: 'j1' },
        'an access entry',
      ],
      [{ project_id: 'j1' }, { projectId: 'j1' }, 'a restriction set or lifted'],
      [{ mapping_id: 'm1' }, { mappingId: 'm1' }, 'a group mapping'],
      [
        { tenant: 'beta', person_id: 'p1' },
        { tenant: 'beta', personId: 'p1' },
        'an act of another tenant of the person (docs/adr/0054 D1)',
      ],
    ])('turns the keys of %j into the event, with its id: %s', (data, keys) => {
      sources[0].send('membership.changed', JSON.stringify(data), 'e1');

      expect(events).toEqual([{ name: 'membership.changed', id: 'e1', ...keys }]);
    });

    it('carries the keys and nothing else, however much the payload says (docs/adr/0054 D2)', () => {
      sources[0].send(
        'membership.changed',
        JSON.stringify({ person_id: 'p1', role: 'admin', display_name: 'Ada' }),
        'e1',
      );

      expect(events).toEqual([{ name: 'membership.changed', id: 'e1', personId: 'p1' }]);
    });

    it.each([
      ['text that is not JSON', 'not json'],
      ['an empty payload', ''],
      ['a JSON null', 'null'],
      ['a JSON string', '"p1"'],
      ['a JSON array that holds the payload', '[{"person_id":"p1"}]'],
      ['a JSON object without any of the keys', '{}'],
      ['a JSON object with other keys only', '{"role":"admin"}'],
      ['a person id that is a number', '{"person_id":12}'],
      ['a project id that is null', '{"person_id":"p1","project_id":null}'],
      ['a mapping id that is an object', '{"mapping_id":{}}'],
      ['a tenant that is a number', '{"tenant":3,"person_id":"p1"}'],
    ])('ignores %s', (_description, data) => {
      sources[0].send('membership.changed', data);

      expect(events).toEqual([]);
    });

    it('passes membership and ticket events on in the order they arrive', () => {
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2, 'edited', 'e1');
      sources[0].send('membership.changed', '{"person_id":"p1"}', 'e2');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-2', 3, 'edited', 'e3');

      expect(names()).toEqual(['ticket.changed', 'membership.changed', 'ticket.changed']);
    });

    // docs/adr/0014 D3: the sort by the score is the project's act, with its key and no version.
    it('passes project.changed on with the project and the kind, and drops a payload that is not one', () => {
      sources[0].send('project.changed', '{"key":"acme/VKO","kind":"ranked"}', 'e4');
      sources[0].send('project.changed', '{"key":3}', 'e5');
      sources[0].send('project.changed', 'not json', 'e6');

      expect(events).toEqual([
        { name: 'project.changed', id: 'e4', key: 'acme/VKO', kind: 'ranked' },
      ]);
    });
  });

  describe('ofTenant', () => {
    it.each<[StreamEvent, string | null, boolean]>([
      [
        { name: 'ticket.changed', id: 'e1', key: 'acme/VKO-1', version: 2, kind: 'edited' },
        'acme',
        true,
      ],
      [
        { name: 'ticket.changed', id: 'e1', key: 'beta/VKO-1', version: 2, kind: 'edited' },
        'acme',
        false,
      ],
      [
        { name: 'question.changed', id: 'e1', key: 'acme-two/VKO-1', version: 2, kind: 'x' },
        'acme',
        false,
      ],
      [
        { name: 'ticket.changed', id: 'e1', key: 'acme/VKO-1', version: 2, kind: 'edited' },
        null,
        false,
      ],
      [{ name: 'membership.changed', id: 'e1', tenant: 'acme', personId: 'p1' }, 'acme', true],
      [{ name: 'membership.changed', id: 'e1', tenant: 'beta', personId: 'p1' }, 'acme', false],
      [{ name: 'membership.changed', id: 'e1', personId: 'p1' }, 'acme', true],
      [{ name: 'membership.changed', id: 'e1', personId: 'p1' }, null, true],
      [{ name: 'membership.changed', id: 'e1', tenant: 'acme', personId: 'p1' }, null, false],
      [{ name: 'inbox.changed', unread: 2 }, 'acme', false],
      [{ name: 'resync' }, 'acme', true],
      [{ name: 'poll' }, null, true],
    ])('says whether %j concerns a page of %s: %s', (event, tenant, expected) => {
      expect(ofTenant(event, tenant)).toBe(expected);
    });
  });

  describe('changesMemberships', () => {
    it.each<[StreamEvent, boolean]>([
      [{ name: 'membership.changed', id: 'e1', tenant: 'acme', personId: 'p1' }, true],
      [{ name: 'membership.changed', id: 'e1', tenant: 'beta', personId: 'p1' }, false],
      [{ name: 'resync' }, true],
      [{ name: 'poll' }, true],
      [{ name: 'ticket.changed', id: 'e1', key: 'acme/VKO-1', version: 2, kind: 'edited' }, false],
      [{ name: 'comment.changed', id: 'e1', key: 'acme/VKO-1', version: 2, kind: 'x' }, false],
    ])('says whether %j may have changed who belongs to acme: %s', (event, expected) => {
      expect(changesMemberships(event, 'acme')).toBe(expected);
    });
  });

  describe('changesVisibility', () => {
    const event = (keys: Partial<MembershipEvent>): MembershipEvent => ({
      name: 'membership.changed',
      id: 'e1',
      ...keys,
    });

    it('says a restriction or an access entry may change what the person sees', () => {
      expect(changesVisibility(event({ projectId: 'j1' }), 'p1')).toBe(true);
      expect(changesVisibility(event({ personId: 'p2', projectId: 'j1' }), 'p1')).toBe(true);
    });

    it("says the person's own role may change what they see, an administrator seeing every project", () => {
      expect(changesVisibility(event({ personId: 'p1' }), 'p1')).toBe(true);
    });

    it("leaves somebody else's membership and a mapping alone", () => {
      expect(changesVisibility(event({ personId: 'p2' }), 'p1')).toBe(false);
      expect(changesVisibility(event({ mappingId: 'm1' }), 'p1')).toBe(false);
    });

    it('leaves a membership alone while the person is not known', () => {
      expect(changesVisibility(event({ personId: 'p1' }), undefined)).toBe(false);
    });
  });

  describe('isImport', () => {
    it('says a project.changed of the kind imported is an import executed (docs/adr/0051 D3)', () => {
      expect(
        isImport({ name: 'project.changed', id: 'e1', key: 'acme/VKO', kind: 'imported' }),
      ).toBe(true);
    });

    it('leaves the sort of a rank and every other event alone', () => {
      expect(isImport({ name: 'project.changed', id: 'e1', key: 'acme/VKO', kind: 'ranked' })).toBe(
        false,
      );
      expect(
        isImport({
          name: 'ticket.changed',
          id: 'e1',
          key: 'acme/VKO-1',
          version: 1,
          kind: 'imported',
        }),
      ).toBe(false);
      expect(isImport({ name: 'resync' })).toBe(false);
    });
  });

  describe('the fallback', () => {
    beforeEach(() => {
      service.connect('acme');
    });

    const failThrice = (source: FakeEventSource) => {
      for (let i = 0; i < fallback.failures; i++) source.fail();
    };

    it('leaves the reconnecting to the browser after one or two errors', () => {
      sources[0].fail();
      sources[0].fail();

      expect(service.status()).toBe('connecting');
      expect(sources[0].isClosed).toBe(false);
      expect(events).toEqual([]);
    });

    it('polls after three errors in a row: it closes the stream and asks for a resync', () => {
      failThrice(sources[0]);

      expect(service.status()).toBe('polling');
      expect(sources[0].isClosed).toBe(true);
      expect(events).toEqual([{ name: 'resync' }]);
    });

    it('counts only errors in a row: an open starts the count again', () => {
      sources[0].fail();
      sources[0].fail();
      sources[0].open();
      sources[0].fail();
      sources[0].fail();

      expect(service.status()).toBe('live');
      expect(events).toEqual([]);

      sources[0].fail();

      expect(service.status()).toBe('polling');
    });

    it('polls at once when an error leaves the stream closed, because the browser will not retry', () => {
      sources[0].fail(closed);

      expect(service.status()).toBe('polling');
      expect(events).toEqual([{ name: 'resync' }]);
    });

    it('polls at once when the server says unavailable', () => {
      sources[0].send('unavailable', '{}');

      expect(service.status()).toBe('polling');
      expect(sources[0].isClosed).toBe(true);
      expect(events).toEqual([{ name: 'resync' }]);
    });

    it('sends a poll every 15 seconds and none before', () => {
      failThrice(sources[0]);
      events.length = 0;

      vi.advanceTimersByTime(pollEvery - 1);
      expect(events).toEqual([]);

      vi.advanceTimersByTime(1);
      expect(events).toEqual([{ name: 'poll' }]);

      vi.advanceTimersByTime(2 * pollEvery);
      expect(names()).toEqual(['poll', 'poll', 'poll']);
    });

    it('tries the stream again every minute, at the same address', () => {
      failThrice(sources[0]);

      vi.advanceTimersByTime(retryEvery - 1);
      expect(sources).toHaveLength(1);

      vi.advanceTimersByTime(1);
      expect(sources.map((source) => source.url)).toEqual([
        '/api/v1/tenants/acme/events?me=true',
        '/api/v1/tenants/acme/events?me=true',
      ]);
      expect(service.status()).toBe('polling');
    });

    it('goes back to live and stops polling and retrying when a retry opens', () => {
      failThrice(sources[0]);
      vi.advanceTimersByTime(retryEvery);
      events.length = 0;

      sources[1].open();
      expect(service.status()).toBe('live');
      events.length = 0;
      vi.advanceTimersByTime(5 * retryEvery);

      expect(events).toEqual([]);
      expect(sources).toHaveLength(2);
      expect(sources[1].isClosed).toBe(false);
    });

    describe('when a retry opens, the stream having recovered from polling', () => {
      const recover = () => {
        failThrice(sources[0]);
        vi.advanceTimersByTime(retryEvery);
        events.length = 0;
        sources[1].open();
      };

      it('asks for one resync, because the new EventSource has no Last-Event-ID to replay from (docs/adr/0054 D5)', () => {
        recover();

        expect(events).toEqual([{ name: 'resync' }]);
      });

      it('sends that resync once the status is live, so that what reacts to it sees the stream up', () => {
        recover();

        expect(statusAt.get(events[0])).toBe('live');
      });

      it('asks only once, however often the stream opens afterwards', () => {
        recover();

        sources[1].fail();
        sources[1].open();
        sources[1].open();

        expect(events).toEqual([{ name: 'resync' }]);
      });

      it('asks again when a later retry recovers after another failed one', () => {
        failThrice(sources[0]);
        vi.advanceTimersByTime(retryEvery);
        sources[1].fail();
        vi.advanceTimersByTime(retryEvery);
        events.length = 0;

        sources[2].open();

        expect(service.status()).toBe('live');
        expect(events).toEqual([{ name: 'resync' }]);
      });

      it('asks again each time the stream recovers from a fallback of its own', () => {
        recover();
        failThrice(sources[1]);
        vi.advanceTimersByTime(retryEvery);
        events.length = 0;

        sources[2].open();

        expect(events).toEqual([{ name: 'resync' }]);
      });

      it('holds that resync while the tab is hidden and sends it when the tab is visible again', () => {
        failThrice(sources[0]);
        vi.advanceTimersByTime(retryEvery);
        events.length = 0;
        setVisibility('hidden');

        sources[1].open();
        expect(events).toEqual([]);
        setVisibility('visible');

        expect(events).toEqual([{ name: 'resync' }]);
      });

      it('is not asked for by a stream of another tenant that opens after the old one polled', () => {
        failThrice(sources[0]);
        service.connect('globex');
        events.length = 0;

        sources[1].open();

        expect(service.status()).toBe('live');
        expect(events).toEqual([]);
      });
    });

    it('delivers the events of the new stream after a retry opened', () => {
      failThrice(sources[0]);
      vi.advanceTimersByTime(retryEvery);
      sources[1].open();
      events.length = 0;

      sources[1].sendTicket('ticket.changed', 'acme/VKO-3', 4);

      expect(names()).toEqual(['ticket.changed']);
    });

    it('keeps polling when a retry fails, and gives up on that attempt at its first error', () => {
      failThrice(sources[0]);
      vi.advanceTimersByTime(retryEvery);
      events.length = 0;

      sources[1].fail();

      expect(sources[1].isClosed).toBe(true);
      expect(service.status()).toBe('polling');
      expect(events).toEqual([]);

      vi.advanceTimersByTime(pollEvery);
      expect(names()).toEqual(['poll']);
    });

    it('tries again a minute after a failed retry', () => {
      failThrice(sources[0]);
      vi.advanceTimersByTime(retryEvery);
      sources[1].fail();

      vi.advanceTimersByTime(retryEvery);

      expect(sources).toHaveLength(3);
      sources[2].open();
      expect(service.status()).toBe('live');
    });

    it('does not ask for a resync of its own when a retry fails', () => {
      failThrice(sources[0]);
      vi.advanceTimersByTime(retryEvery);
      events.length = 0;

      sources[1].fail();

      expect(events).toEqual([]);
    });

    it('does not stack a new attempt on a retry that is still connecting', () => {
      failThrice(sources[0]);
      vi.advanceTimersByTime(retryEvery);
      expect(sources).toHaveLength(2);

      vi.advanceTimersByTime(3 * retryEvery);

      expect(sources).toHaveLength(2);
      expect(sources[1].isClosed).toBe(false);
      expect(service.status()).toBe('polling');
      sources[1].open();
      expect(service.status()).toBe('live');
    });

    it('does not ask for a second resync when a retry is told unavailable', () => {
      failThrice(sources[0]);
      vi.advanceTimersByTime(retryEvery);
      events.length = 0;

      sources[1].send('unavailable', '{}');

      expect(sources[1].isClosed).toBe(true);
      expect(service.status()).toBe('polling');
      expect(events.filter((event) => event.name === 'resync')).toEqual([]);
    });

    it('falls back again, with fresh timers, when the stream that came back fails three times', () => {
      failThrice(sources[0]);
      vi.advanceTimersByTime(retryEvery);
      sources[1].open();
      events.length = 0;

      failThrice(sources[1]);

      expect(service.status()).toBe('polling');
      expect(events).toEqual([{ name: 'resync' }]);
      vi.advanceTimersByTime(pollEvery);
      expect(names()).toEqual(['resync', 'poll']);
      vi.advanceTimersByTime(retryEvery);
      expect(sources).toHaveLength(3);
    });
  });

  describe('a hidden tab (docs/adr/0054 D8)', () => {
    beforeEach(() => {
      service.connect('acme');
      sources[0].open();
    });

    it('sends the latest unread count last when the tab is visible again', () => {
      setVisibility('hidden');
      sources[0].send('inbox.changed', '{"unread":1}');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2, 'edited', 'a');
      sources[0].send('inbox.changed', '{"unread":2}');

      setVisibility('visible');

      expect(events).toEqual([
        { name: 'ticket.changed', id: 'a', key: 'acme/VKO-1', version: 2, kind: 'edited' },
        { name: 'inbox.changed', unread: 2 },
      ]);
    });

    it('passes events on at once while the tab is visible', () => {
      setVisibility('visible');

      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2);

      expect(names()).toEqual(['ticket.changed']);
    });

    it('holds events back while the tab is hidden, and also when the tab changes while hidden', () => {
      setVisibility('hidden');

      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2);
      document.dispatchEvent(new Event('visibilitychange'));

      expect(events).toEqual([]);
    });

    it('sends the latest event of each ticket and each event name when the tab is visible again', () => {
      setVisibility('hidden');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2, 'edited', 'a');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 3, 'transitioned', 'b');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-2', 7, 'edited', 'c');
      sources[0].sendTicket('comment.changed', 'acme/VKO-1', 3, 'commented', 'd');
      sources[0].sendTicket('comment.changed', 'acme/VKO-1', 3, 'commented', 'e');

      setVisibility('visible');

      expect(events).toEqual([
        { name: 'ticket.changed', id: 'b', key: 'acme/VKO-1', version: 3, kind: 'transitioned' },
        { name: 'ticket.changed', id: 'c', key: 'acme/VKO-2', version: 7, kind: 'edited' },
        { name: 'comment.changed', id: 'e', key: 'acme/VKO-1', version: 3, kind: 'commented' },
      ]);
    });

    it('keeps the highest version of a ticket even when an older event arrives after it', () => {
      setVisibility('hidden');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 5, 'edited', 'new');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 4, 'edited', 'old');

      setVisibility('visible');

      expect(events).toEqual([
        { name: 'ticket.changed', id: 'new', key: 'acme/VKO-1', version: 5, kind: 'edited' },
      ]);
    });

    it('sends one resync instead of everything when a resync waited', () => {
      setVisibility('hidden');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2);
      sources[0].send('membership.changed', '{"person_id":"p1"}');
      sources[0].send('resync', '{}');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-2', 3);

      setVisibility('visible');

      expect(events).toEqual([{ name: 'resync' }]);
    });

    it('sends every membership event that waited, as they came, after the latest event of each ticket', () => {
      setVisibility('hidden');
      sources[0].send('membership.changed', '{"project_id":"j1"}', 'm1');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2, 'edited', 'a');
      sources[0].send('membership.changed', '{"person_id":"p1"}', 'm2');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 3, 'edited', 'b');

      setVisibility('visible');

      expect(events).toEqual([
        { name: 'ticket.changed', id: 'b', key: 'acme/VKO-1', version: 3, kind: 'edited' },
        { name: 'membership.changed', id: 'm1', projectId: 'j1' },
        { name: 'membership.changed', id: 'm2', personId: 'p1' },
      ]);
    });

    it('sends one resync when a poll waited, because a poll names no ticket', () => {
      for (let i = 0; i < fallback.failures; i++) sources[0].fail();
      expect(events).toEqual([{ name: 'resync' }]);
      events.length = 0;
      setVisibility('hidden');

      vi.advanceTimersByTime(pollEvery);
      expect(events).toEqual([]);
      setVisibility('visible');

      expect(events).toEqual([{ name: 'resync' }]);
    });

    it('sends one resync when the fallback began while the tab was hidden', () => {
      setVisibility('hidden');

      for (let i = 0; i < fallback.failures; i++) sources[0].fail();
      vi.advanceTimersByTime(3 * pollEvery);
      expect(events).toEqual([]);
      setVisibility('visible');

      expect(events).toEqual([{ name: 'resync' }]);
    });

    it('sends what waited once and not again at the next change of the tab', () => {
      setVisibility('hidden');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2);
      setVisibility('visible');
      events.length = 0;

      setVisibility('hidden');
      setVisibility('visible');

      expect(events).toEqual([]);
    });

    it('sends nothing when the tab is visible again and nothing waited', () => {
      setVisibility('hidden');

      setVisibility('visible');

      expect(events).toEqual([]);
    });

    it('drops what waited when it follows another tenant', () => {
      setVisibility('hidden');
      sources[0].sendTicket('ticket.changed', 'acme/VKO-1', 2);

      service.connect('globex');
      setVisibility('visible');

      expect(events).toEqual([]);
    });
  });

  describe('when its injector is destroyed', () => {
    it('closes the stream and stops listening to the tab', () => {
      service.connect('acme');
      sources[0].open();
      const stopListening = vi.spyOn(document, 'removeEventListener');

      TestBed.resetTestingModule();

      expect(sources[0].isClosed).toBe(true);
      expect(stopListening).toHaveBeenCalledWith('visibilitychange', expect.any(Function));
    });

    it('stops the fallback timers', () => {
      const timers = vi.getTimerCount();
      service.connect('acme');
      sources[0].fail(closed);
      expect(vi.getTimerCount()).toBe(timers + 2);

      TestBed.resetTestingModule();

      expect(vi.getTimerCount()).toBe(timers);
    });
  });
});

describe('EVENT_SOURCE', () => {
  class NativeEventSource {
    constructor(readonly url: string) {}
  }

  afterEach(() => vi.unstubAllGlobals());

  it('opens a native EventSource at the address by default', () => {
    vi.stubGlobal('EventSource', NativeEventSource);

    const source = TestBed.inject(EVENT_SOURCE)('/api/v1/tenants/acme/events');

    expect(source).toBeInstanceOf(NativeEventSource);
    expect((source as unknown as NativeEventSource).url).toBe('/api/v1/tenants/acme/events');
  });
});
