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
 * `membership.changed` (docs/adr/0054 D2): who belongs to the tenant, or who sees a project,
 * changed — by an administrator's act or by the identity provider's groups (docs/adr/0030). The
 * payload carries keys only, each where it applies.
 */
export interface MembershipEvent {
  name: 'membership.changed';
  /** The audit row's id, which is also the stream's event id (D5). */
  id: string;
  /** The person whose membership or project access changed. */
  personId?: string;
  /** The project whose restriction or access list changed. */
  projectId?: string;
  /** The group mapping that was made, changed or removed. */
  mappingId?: string;
}

/**
 * `project.changed` (docs/adr/0054 D2): a project's rank was set as a whole — sorted by the score
 * (docs/adr/0014 D3). The payload carries the project's key, `<tenant>/<PROJECT>`, and the act's
 * kind; no version, since no ticket's changed.
 */
export interface ProjectEvent {
  name: 'project.changed';
  /** The audit row's id, which is also the stream's event id (D5). */
  id: string;
  /** The project's key, `<tenant>/<PROJECT>`. */
  key: string;
  kind: string;
}

/**
 * `inbox.changed` (docs/adr/0054 D2) on the person-level stream: how many of the person's
 * notifications are unread, in every tenant they belong to — when the stream opens and whenever the
 * inbox changes. It carries no id: it says how things stand.
 */
export interface InboxEvent {
  name: 'inbox.changed';
  unread: number;
}

/**
 * What the services react to: a change of a ticket or of the memberships, the person's unread
 * count; `resync`, after which everything shown is fetched again; or `poll`, the fallback's tick
 * (D7). A ticket event may name a ticket of another tenant than the page's: the person-level stream
 * carries the questions asked of the person in all their tenants (D1).
 */
export type StreamEvent =
  TicketEvent | MembershipEvent | ProjectEvent | InboxEvent | { name: 'resync' } | { name: 'poll' };

/** Whether an event may have changed who belongs to the tenant: its own event, or a gap in the stream. */
export function changesMemberships(event: StreamEvent): boolean {
  return event.name === 'membership.changed' || event.name === 'resync' || event.name === 'poll';
}

/**
 * Whether a membership event may have changed which projects the person sees: a project's
 * restriction or access list, or the person's own role — an administrator sees every project,
 * anybody else the open ones and those whose list names them (docs/adr/0034 D3).
 */
export function changesVisibility(event: MembershipEvent, person: string | undefined): boolean {
  return event.projectId !== undefined || (person !== undefined && event.personId === person);
}

/**
 * `connecting` until the stream is open, `live` while it is, `polling` while the fallback runs,
 * `idle` while the person belongs to no tenant.
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

/** An event's data as JSON, or undefined where it is none. */
function parsed(message: MessageEvent<string>): unknown {
  try {
    return JSON.parse(message.data);
  } catch {
    return undefined;
  }
}

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

/** The payload of `project.changed`: the project's key and the act's kind. */
function isProjectPayload(data: unknown): data is { key: string; kind: string } {
  const payload = data as Record<string, unknown> | null;
  return (
    typeof payload === 'object' &&
    payload !== null &&
    typeof payload['key'] === 'string' &&
    typeof payload['kind'] === 'string'
  );
}

/** The payload of `inbox.changed`: the unread count, a whole number. */
function unreadOf(data: unknown): number | undefined {
  const unread = (data as Record<string, unknown> | null)?.['unread'];
  return typeof unread === 'number' && Number.isInteger(unread) && unread >= 0 ? unread : undefined;
}

/** The keys a `membership.changed` payload may carry, and the event's names for them. */
const membershipKeys = {
  person_id: 'personId',
  project_id: 'projectId',
  mapping_id: 'mappingId',
} as const;

/**
 * The payload of a `membership.changed`: an object with at least one of its keys, each a string;
 * anything else it carries is left out. Undefined for any other payload.
 */
function membershipPayload(
  data: unknown,
): Pick<MembershipEvent, 'personId' | 'projectId' | 'mappingId'> | undefined {
  if (typeof data !== 'object' || data === null || Array.isArray(data)) {
    return undefined;
  }
  const payload = data as Record<string, unknown>;
  const keys: Pick<MembershipEvent, 'personId' | 'projectId' | 'mappingId'> = {};
  for (const [key, field] of Object.entries(membershipKeys)) {
    const value = payload[key];
    if (typeof value === 'string') {
      keys[field] = value;
    } else if (value !== undefined) {
      return undefined;
    }
  }
  return Object.keys(keys).length > 0 ? keys : undefined;
}

/** The fallback's numbers (docs/adr/0054 D7). */
export const fallback = { failures: 3, pollEvery: 15_000, retryEvery: 60_000 };

const closed = 2;

/**
 * The event stream (docs/adr/0054): one `EventSource`, opened as the person-level stream
 * (`?me=true`, D1) on the tenant the pages show, or — on the person-level pages and wherever no
 * tenant is shown — on one of the person's tenants ({@link personal}), so that the unread count
 * and the questions asked of the person arrive everywhere; events that carry keys and versions and
 * nothing else, and the polling fallback — after three failures in a row or `event: unavailable`,
 * a `poll` tick every fifteen seconds and a new attempt at the stream every minute. While the tab
 * is hidden the events wait and arrive, merged, when it is visible again (D8). The browser sends
 * `Last-Event-ID` on its own reconnects (D5).
 */
@Injectable({ providedIn: 'root' })
export class EventStreamService {
  private readonly open = inject(EVENT_SOURCE);
  private readonly document = inject(DOCUMENT);
  private readonly subject = new Subject<StreamEvent>();

  private source: EventSourceLike | null = null;
  /** The tenant the stream is open on: the page's, else the person's. */
  private tenant: string | null = null;
  private pageTenant: string | null = null;
  private personTenant: string | null = null;
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

  /** Follows the tenant the pages show; `null` falls back to the person's own ({@link personal}). */
  connect(tenant: string | null): void {
    this.pageTenant = tenant;
    this.follow(this.pageTenant ?? this.personTenant);
  }

  /**
   * The tenant to hold the person-level stream on while no tenant page is open: one of the
   * person's, `null` for a person who belongs to none — then there is no stream.
   */
  personal(tenant: string | null): void {
    this.personTenant = tenant;
    this.follow(this.pageTenant ?? this.personTenant);
  }

  private follow(tenant: string | null): void {
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
    const source = this.open(`/api/v1/tenants/${encodeURIComponent(tenant)}/events?me=true`);
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
    source.addEventListener('membership.changed', (message) => this.membershipEvent(message));
    source.addEventListener('project.changed', (message) => {
      const data = parsed(message);
      if (isProjectPayload(data)) {
        this.emit({
          name: 'project.changed',
          id: message.lastEventId,
          key: data.key,
          kind: data.kind,
        });
      }
    });
    source.addEventListener('inbox.changed', (message) => {
      const unread = unreadOf(parsed(message));
      if (unread !== undefined) {
        this.emit({ name: 'inbox.changed', unread });
      }
    });
    source.addEventListener('resync', () => this.emit({ name: 'resync' }));
    source.addEventListener('unavailable', () => this.fallBack());
  }

  /** A payload that is not the documented one changes nothing a client shows; the next resync or poll catches up. */
  private ticketEvent(name: TicketEventName, message: MessageEvent<string>): void {
    const data = parsed(message);
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

  private membershipEvent(message: MessageEvent<string>): void {
    const keys = membershipPayload(parsed(message));
    if (keys) {
      this.emit({ name: 'membership.changed', id: message.lastEventId, ...keys });
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

  /**
   * What waited while the tab was hidden: one resync if any was due, else each ticket's latest
   * event, then the membership and project events as they came — each says what it touched, and
   * the views they reload load at most once more however many arrive (`refresh`) — and the latest
   * unread count.
   */
  private flush(): void {
    const waiting = this.deferred;
    this.deferred = [];
    if (waiting.some((event) => event.name === 'resync' || event.name === 'poll')) {
      this.subject.next({ name: 'resync' });
      return;
    }
    const latest = new Map<string, TicketEvent>();
    const memberships: (MembershipEvent | ProjectEvent)[] = [];
    let inbox: InboxEvent | undefined;
    for (const event of waiting as (TicketEvent | MembershipEvent | ProjectEvent | InboxEvent)[]) {
      if (event.name === 'membership.changed' || event.name === 'project.changed') {
        memberships.push(event);
        continue;
      }
      if (event.name === 'inbox.changed') {
        inbox = event;
        continue;
      }
      const id = `${event.name} ${event.key}`;
      const held = latest.get(id);
      if (!held || held.version <= event.version) {
        latest.set(id, event);
      }
    }
    for (const event of [...latest.values(), ...memberships, ...(inbox ? [inbox] : [])]) {
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
