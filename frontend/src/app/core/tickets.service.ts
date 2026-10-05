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
import { changesVisibility, EventStreamService, StreamEvent } from './event-stream.service';
import { keepShown, refresh } from './refresh';
import { SessionService } from './session.service';

/** A page of a list: the keys in the server's order; the tickets themselves are in the cache. */
export interface TicketPage {
  keys: string[];
  total?: number;
  nextCursor: string | null;
  /**
   * The version each ticket had in this answer, which the cache does not keep where it holds a
   * newer one: a page tells by it whether an answer shows a write of its own yet. Only
   * {@link TicketsService.projectTicketPages} says.
   */
  versions?: ReadonlyMap<string, number>;
}

/** `acme/VKO-12` → `{ tenant: 'acme', key: 'VKO-12' }`. */
export function splitKey(key: string): { tenant: string; key: string } {
  const slash = key.indexOf('/');
  return { tenant: key.slice(0, slash), key: key.slice(slash + 1) };
}

/** How long list reloads wait for more events of the same burst. */
export const listReloadDelay = 150;

/**
 * The page size of a list that is followed cursor by cursor. The server clamps what it is asked
 * for to `COWORK_MAX_PAGE_SIZE`, 200 unless an operator changed it (docs/adr/0039 D2), and the
 * projects list asks for 200 as well; a longer list takes more pages.
 */
export const pageSize = 200;

/**
 * The request of {@link TicketsService.projectTicketPages}: the filters of a project's list and how
 * many pages of it to hold. Cursor, limit and the numbered pages are the service's business.
 */
export type ProjectTicketPagesParams = Omit<
  ListProjectTickets$Params,
  'cursor' | 'limit' | 'page' | 'per_page' | 'If-None-Match'
> & { pages: number };

/**
 * The tickets of the tenant the pages show (docs/adr/0053 D1, D2). Lists hold keys and read the
 * tickets through one cache, so a refetch shows on every view at once. An event about a cached
 * ticket refetches it, and every open list reloads once per burst, so a new ticket or a state
 * change appears without a reload (docs/adr/0054); `resync` and the fallback's `poll` reload
 * everything shown. A list that loads again and fails keeps what it shows ({@link keepShown}).
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
    const tickets: ResourceRef<TicketPage | undefined> = resource({
      params,
      loader: ({ params }) =>
        keepShown(tickets, () =>
          this.api.invoke(listProjectTickets, params).then((list) => this.keep(list)),
        ),
      injector,
    });
    return this.track(tickets, injector);
  }

  /**
   * A project's tickets in the server's order, followed cursor by cursor (docs/adr/0048 D4): the
   * first `pages` pages, each as large as the server allows, and `nextCursor` says whether there
   * is more. A reload asks for the same number of pages again, so that what a person loaded stays
   * and the order is the current one; a ticket that moved between two answers shows once, where
   * the later answer has it.
   */
  projectTicketPages(
    params: () => ProjectTicketPagesParams | undefined,
    injector = inject(Injector),
  ): ResourceRef<TicketPage | undefined> {
    const tickets: ResourceRef<TicketPage | undefined> = resource({
      params,
      loader: ({ params }) => keepShown(tickets, () => this.followPages(params)),
      injector,
    });
    return this.track(tickets, injector);
  }

  /** A list across the tenant's projects. */
  tenantTickets(
    params: () => ListTenantTickets$Params | undefined,
    injector = inject(Injector),
  ): ResourceRef<TicketPage | undefined> {
    const tickets: ResourceRef<TicketPage | undefined> = resource({
      params,
      loader: ({ params }) =>
        keepShown(tickets, () =>
          this.api.invoke(listTenantTickets, params).then((list) => this.keep(list)),
        ),
      injector,
    });
    return this.track(tickets, injector);
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

  /**
   * Every open ticket of a project, in its rank, every page of it, into the cache — what a person
   * picks a parent from (docs/adr/0008 D2). A one-off read, not an open list: nothing reloads it.
   */
  async openTickets(tenant: string, project: string): Promise<Ticket[]> {
    const tickets: Ticket[] = [];
    let cursor: string | undefined;
    do {
      const list = await this.api.invoke(listProjectTickets, {
        tenant,
        project,
        cursor,
        limit: pageSize,
      });
      this.keep(list);
      tickets.push(...list.items);
      cursor = list.next_cursor ?? undefined;
    } while (cursor);
    return tickets;
  }

  /** Fetches one ticket by its canonical key into the cache. */
  async refresh(key: string): Promise<Ticket> {
    const ticket = await this.api.invoke(resolveTicket, splitKey(key));
    this.cache.put(ticket.key, ticket);
    return ticket;
  }

  private async followPages({ pages, ...query }: ProjectTicketPagesParams): Promise<TicketPage> {
    // The version of each key; a ticket seen twice keeps the place and the version of the later
    // answer, so it is deleted first.
    const versions = new Map<string, number>();
    let cursor: string | undefined;
    for (let page = 0; page < pages; page++) {
      const list = await this.api.invoke(listProjectTickets, { ...query, cursor, limit: pageSize });
      this.keep(list);
      for (const ticket of list.items) {
        versions.delete(ticket.key);
        versions.set(ticket.key, ticket.version);
      }
      if (list.next_cursor === null) {
        return { keys: [...versions.keys()], nextCursor: null, versions };
      }
      cursor = list.next_cursor;
    }
    return { keys: [...versions.keys()], nextCursor: cursor ?? null, versions };
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
    // A project's restriction, its access list or the person's own role may have hidden a project
    // or shown one: what is shown is fetched again, and a ticket the person no longer sees goes.
    // Any other change of a membership leaves the tickets as they are.
    const visibility =
      event.name === 'membership.changed' && changesVisibility(event, this.session.person()?.id);
    if (event.name === 'resync' || event.name === 'poll' || visibility) {
      for (const key of this.watched.keys()) {
        this.refetch(key);
      }
      this.reloadLists();
      return;
    }
    // The person-level stream also names tickets of the person's other tenants (docs/adr/0054 D1);
    // this service holds the tickets of the tenant the pages show.
    if (
      event.name === 'membership.changed' ||
      event.name === 'inbox.changed' ||
      splitKey(event.key).tenant !== this.session.tenant()
    ) {
      return;
    }
    // A project's rank set as a whole — the sort by the score — moves no ticket's version: the
    // lists, which hold the order, load again (docs/adr/0014 D3).
    if (event.name === 'project.changed') {
      this.reloadLists();
      return;
    }
    const held = this.cache.value(event.key);
    // A ticket's version counts its own fields only (docs/adr/0050 D1): a newer one is a change;
    // a link changes its open prerequisites without one, and a stake its score (docs/adr/0013
    // D3); a question or a comment changes nothing the ticket itself shows.
    const stale =
      held !== undefined &&
      (event.name === 'ticket.changed'
        ? held.version < event.version
        : event.name === 'link.changed' || event.name === 'interest.changed');
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

  /** Loads every open list again, once per burst ({@link listReloadDelay}). */
  reloadLists(): void {
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
