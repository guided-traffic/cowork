import {
  DestroyRef,
  effect,
  inject,
  Injectable,
  Injector,
  resource,
  ResourceRef,
  untracked,
} from '@angular/core';
import { HttpErrorResponse } from '@angular/common/http';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import {
  listProjectTickets,
  ListProjectTickets$Params,
  listTenantTickets,
  ListTenantTickets$Params,
  resolveTicket,
} from '../api/functions';
import { Ticket, TicketList } from '../api/models';
import { EntityCache } from './entity-cache';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { refresh } from './refresh';
import { SessionService } from './session.service';

/** A page of a list: the keys in the server's order; the tickets themselves are in the cache. */
export interface TicketPage {
  keys: string[];
  total?: number;
  nextCursor: string | null;
}

/** `acme/VKO-12` → `{ tenant: 'acme', key: 'VKO-12' }`. */
export function splitKey(key: string): { tenant: string; key: string } {
  const slash = key.indexOf('/');
  return { tenant: key.slice(0, slash), key: key.slice(slash + 1) };
}

/** How long list reloads wait for more events of the same burst. */
export const listReloadDelay = 150;

/**
 * The tickets of the tenant the pages show (docs/adr/0053 D1, D2). Lists hold keys and read the
 * tickets through one cache, so a refetch shows on every view at once. An event about a cached
 * ticket refetches it, and every open list reloads once per burst, so a new ticket or a state
 * change appears without a reload (docs/adr/0054); `resync` and the fallback's `poll` reload
 * everything shown.
 */
@Injectable({ providedIn: 'root' })
export class TicketsService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  /** The open lists, each with the injector of the view that shows it. */
  private readonly lists = new Map<ResourceRef<TicketPage | undefined>, Injector>();
  /** The tickets a detail view shows, with how many views show each. */
  private readonly watched = new Map<string, number>();
  private reloadTimer: ReturnType<typeof setTimeout> | null = null;

  readonly cache = new EntityCache<Ticket>();

  constructor() {
    effect(() => {
      this.session.tenant();
      untracked(() => this.cache.clear());
    });
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => this.react(event));
  }

  /** A list of a project's tickets, for the component that creates it in its injection context. */
  projectTickets(
    params: () => ListProjectTickets$Params | undefined,
    injector = inject(Injector),
  ): ResourceRef<TicketPage | undefined> {
    return this.track(
      resource({
        params,
        loader: ({ params }) =>
          this.api.invoke(listProjectTickets, params).then((list) => this.keep(list)),
        injector,
      }),
      injector,
    );
  }

  /** A list across the tenant's projects. */
  tenantTickets(
    params: () => ListTenantTickets$Params | undefined,
    injector = inject(Injector),
  ): ResourceRef<TicketPage | undefined> {
    return this.track(
      resource({
        params,
        loader: ({ params }) =>
          this.api.invoke(listTenantTickets, params).then((list) => this.keep(list)),
        injector,
      }),
      injector,
    );
  }

  /**
   * One ticket by its canonical key, for a detail view: loaded into the cache, which the view
   * reads, and refetched on `resync` and `poll` for as long as the view shows it.
   */
  ticket(
    key: () => string | undefined,
    injector = inject(Injector),
  ): ResourceRef<string | undefined> {
    effect(
      (onCleanup) => {
        const shown = key();
        if (shown) {
          this.watched.set(shown, (this.watched.get(shown) ?? 0) + 1);
          onCleanup(() => {
            const count = this.watched.get(shown);
            if (count && count > 1) {
              this.watched.set(shown, count - 1);
            } else {
              this.watched.delete(shown);
            }
          });
        }
      },
      { injector },
    );
    return resource({
      params: key,
      loader: ({ params }) => this.refresh(params).then((ticket) => ticket.key),
      injector,
    });
  }

  /** Fetches one ticket by its canonical key into the cache. */
  async refresh(key: string): Promise<Ticket> {
    const ticket = await this.api.invoke(resolveTicket, splitKey(key));
    this.cache.put(ticket.key, ticket);
    return ticket;
  }

  private keep(list: TicketList): TicketPage {
    for (const ticket of list.items) {
      this.cache.put(ticket.key, ticket);
    }
    return {
      keys: list.items.map((ticket) => ticket.key),
      total: list.total,
      nextCursor: list.next_cursor,
    };
  }

  private track(
    ref: ResourceRef<TicketPage | undefined>,
    injector: Injector,
  ): ResourceRef<TicketPage | undefined> {
    this.lists.set(ref, injector);
    injector.get(DestroyRef).onDestroy(() => this.lists.delete(ref));
    return ref;
  }

  private react(event: StreamEvent): void {
    if (event.name === 'resync' || event.name === 'poll') {
      for (const key of this.watched.keys()) {
        this.refetch(key);
      }
      this.reloadLists();
      return;
    }
    const held = this.cache.value(event.key);
    // A ticket's version counts its own fields only (docs/adr/0050 D1): a newer one is a change;
    // a question or a link re-derives its urgency without one; a comment or a stake changes
    // nothing the ticket itself shows.
    const stale =
      held !== undefined &&
      (event.name === 'ticket.changed'
        ? held.version < event.version
        : event.name === 'question.changed' || event.name === 'link.changed');
    if (stale) {
      this.refetch(event.key);
    }
    if (event.name !== 'comment.changed' && event.name !== 'interest.changed') {
      this.reloadLists();
    }
  }

  /**
   * Refetches a ticket the page shows. Only an answer that the ticket is gone for the person —
   * `404`, or `403` — empties its entry; an outage keeps the last known value on screen, which is
   * what the polling fallback is for.
   */
  private refetch(key: string): void {
    this.refresh(key).catch((error: unknown) => {
      if (error instanceof HttpErrorResponse && (error.status === 404 || error.status === 403)) {
        this.cache.delete(key);
      }
    });
  }

  private reloadLists(): void {
    if (this.reloadTimer) {
      return;
    }
    this.reloadTimer = setTimeout(() => {
      this.reloadTimer = null;
      for (const [list, injector] of this.lists) {
        refresh(list, injector);
      }
    }, listReloadDelay);
  }
}
