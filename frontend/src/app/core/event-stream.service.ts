import { DOCUMENT } from '@angular/common';
import { DestroyRef, inject, Injectable, InjectionToken, signal } from '@angular/core';
import { Observable, Subject } from 'rxjs';

/** The events of docs/adr/0054 D2 that name a ticket. */
export const ticketEventNames = [
  'ticket.changed',
  'comment.changed',
  'question.changed',
  'link.changed',
  'interest.changed',
] as const;
export type TicketEventName = (typeof ticketEventNames)[number];

export interface TicketEvent {
  name: TicketEventName;
  /** The audit row's id, which is also the stream's event id (D5). */
  id: string;
  /** The ticket's canonical key, `<tenant>/<PROJECT>-<number>`. */
  key: string;
  version: number;
  /** The act's action, such as `transitioned` or `commented`. */
  kind: string;
}

/**
 * What the services react to: a change of a ticket; `resync`, after which everything shown is
 * fetched again; or `poll`, the fallback's tick (D7).
 */
export type StreamEvent = TicketEvent | { name: 'resync' } | { name: 'poll' };

/**
 * `connecting` until the stream is open, `live` while it is, `polling` while the fallback runs,
 * `idle` outside a tenant.
 */
export type StreamStatus = 'idle' | 'connecting' | 'live' | 'polling';

/** The part of `EventSource` the service uses, so a test can hand in its own. */
export interface EventSourceLike {
  readonly readyState: number;
  onopen: ((event: Event) => unknown) | null;
  onerror: ((event: Event) => unknown) | null;
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void;
  close(): void;
}

export const EVENT_SOURCE = new InjectionToken<(url: string) => EventSourceLike>('EVENT_SOURCE', {
  providedIn: 'root',
  factory: () => (url: string) => new EventSource(url),
});

/** The payload of D2: a key, a version and the act's kind, nothing else. */
function isTicketPayload(data: unknown): data is { key: string; version: number; kind: string } {
  const payload = data as Record<string, unknown> | null;
  return (
    typeof payload === 'object' &&
    payload !== null &&
    typeof payload['key'] === 'string' &&
    typeof payload['version'] === 'number' &&
    typeof payload['kind'] === 'string'
  );
}

/** The fallback's numbers (docs/adr/0054 D7). */
export const fallback = { failures: 3, pollEvery: 15_000, retryEvery: 60_000 };

const closed = 2;

/**
 * The tenant's event stream (docs/adr/0054): one `EventSource` per tenant page, events that
 * carry a key and a version and nothing else, and the polling fallback — after three failures in
 * a row or `event: unavailable`, a `poll` tick every fifteen seconds and a new attempt at the
 * stream every minute. While the tab is hidden the events wait and arrive, merged, when it is
 * visible again (D8). The browser sends `Last-Event-ID` on its own reconnects (D5).
 */
@Injectable({ providedIn: 'root' })
export class EventStreamService {
  private readonly open = inject(EVENT_SOURCE);
  private readonly document = inject(DOCUMENT);
  private readonly subject = new Subject<StreamEvent>();

  private source: EventSourceLike | null = null;
  private tenant: string | null = null;
  private failures = 0;
  private pollTimer: ReturnType<typeof setInterval> | null = null;
  private retryTimer: ReturnType<typeof setInterval> | null = null;
  private deferred: StreamEvent[] = [];

  readonly status = signal<StreamStatus>('idle');
  readonly events: Observable<StreamEvent> = this.subject.asObservable();

  constructor() {
    const visibility = () => {
      if (this.document.visibilityState !== 'hidden') {
        this.flush();
      }
    };
    this.document.addEventListener('visibilitychange', visibility);
    inject(DestroyRef).onDestroy(() => {
      this.document.removeEventListener('visibilitychange', visibility);
      this.connect(null);
    });
  }

  /** Follows the tenant the pages show; `null` closes the stream. */
  connect(tenant: string | null): void {
    if (tenant === this.tenant) {
      return;
    }
    this.closeSource();
    this.stopPolling();
    this.tenant = tenant;
    this.failures = 0;
    this.deferred = [];
    if (tenant) {
      this.status.set('connecting');
      this.openStream(tenant);
    } else {
      this.status.set('idle');
    }
  }

  private openStream(tenant: string): void {
    const source = this.open(`/api/v1/tenants/${encodeURIComponent(tenant)}/events`);
    this.source = source;
    source.onopen = () => {
      // A stream opened after polling is a new EventSource without Last-Event-ID, so the hub
      // neither replays nor says resync: what changed since the last poll is fetched once.
      const recovered = this.status() === 'polling';
      this.failures = 0;
      this.stopPolling();
      this.status.set('live');
      if (recovered) {
        this.emit({ name: 'resync' });
      }
    };
    source.onerror = () => {
      this.failures++;
      if (source.readyState === closed || this.failures >= fallback.failures) {
        this.fallBack();
      }
    };
    for (const name of ticketEventNames) {
      source.addEventListener(name, (message) => this.ticketEvent(name, message));
    }
    source.addEventListener('resync', () => this.emit({ name: 'resync' }));
    source.addEventListener('unavailable', () => this.fallBack());
  }

  /** A payload that is not the documented one changes nothing a client shows; the next resync or poll catches up. */
  private ticketEvent(name: TicketEventName, message: MessageEvent<string>): void {
    let data: unknown;
    try {
      data = JSON.parse(message.data);
    } catch {
      return;
    }
    if (isTicketPayload(data)) {
      this.emit({
        name,
        id: message.lastEventId,
        key: data.key,
        version: data.version,
        kind: data.kind,
      });
    }
  }

  private fallBack(): void {
    this.closeSource();
    if (this.status() === 'polling') {
      return;
    }
    this.status.set('polling');
    this.emit({ name: 'resync' });
    this.pollTimer = setInterval(() => this.emit({ name: 'poll' }), fallback.pollEvery);
    this.retryTimer = setInterval(() => {
      if (this.tenant && !this.source) {
        this.openStream(this.tenant);
      }
    }, fallback.retryEvery);
  }

  private emit(event: StreamEvent): void {
    if (this.document.visibilityState === 'hidden') {
      this.deferred.push(event);
    } else {
      this.subject.next(event);
    }
  }

  /** What waited while the tab was hidden: one resync if any was due, else each ticket's latest event. */
  private flush(): void {
    const waiting = this.deferred;
    this.deferred = [];
    if (waiting.some((event) => event.name === 'resync' || event.name === 'poll')) {
      this.subject.next({ name: 'resync' });
      return;
    }
    const latest = new Map<string, TicketEvent>();
    for (const event of waiting as TicketEvent[]) {
      const id = `${event.name} ${event.key}`;
      const held = latest.get(id);
      if (!held || held.version <= event.version) {
        latest.set(id, event);
      }
    }
    for (const event of latest.values()) {
      this.subject.next(event);
    }
  }

  private closeSource(): void {
    this.source?.close();
    this.source = null;
  }

  private stopPolling(): void {
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
    }
    if (this.retryTimer) {
      clearInterval(this.retryTimer);
    }
    this.pollTimer = null;
    this.retryTimer = null;
  }
}
