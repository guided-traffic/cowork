import { computed, inject, Injectable, Injector, resource, ResourceRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import { listDeletedTickets, purgeTicket, restoreTicket } from '../api/functions';
import { DeletedTicket, Ticket } from '../api/models';
import { EventStreamService, StreamEvent } from './event-stream.service';
import { keepShown, refresh } from './refresh';
import { SessionService } from './session.service';
import { TicketsService } from './tickets.service';

/** The acts that put a ticket into the bin, take it out again, or remove it for good. */
const binKinds = new Set(['deleted', 'restored', 'purged']);

/**
 * The tenant's bin of deleted tickets (docs/adr/0024 D1, D2), every page of it, and its
 * administrators' acts on it: a restoration and a purge. Only a tenant administrator reads the bin,
 * so it is not asked for anybody else. A deletion, a restoration and a purge are on the event
 * stream — the purge job's as well —, and the bin loads again on them, on `resync` and on `poll`.
 */
@Injectable({ providedIn: 'root' })
export class DeletedTicketsService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly tickets = inject(TicketsService);
  private readonly injector = inject(Injector);

  /** The tenant of the page while the person administers it; a computed, so `me` loaded again leaves the bin alone. */
  private readonly administered = computed(() => {
    const tenant = this.session.tenant();
    return tenant !== null && this.session.membership()?.role === 'admin' ? tenant : undefined;
  });

  readonly bin: ResourceRef<DeletedTicket[] | undefined> = resource({
    params: () => this.administered(),
    loader: ({ params: tenant }) =>
      keepShown(this.bin, async () => {
        const items: DeletedTicket[] = [];
        let cursor: string | undefined;
        do {
          const page = await this.api.invoke(listDeletedTickets, { tenant, cursor, limit: 200 });
          items.push(...page.items);
          cursor = page.next_cursor ?? undefined;
        } while (cursor);
        return items;
      }),
  });

  readonly list = computed<DeletedTicket[]>(() => (this.bin.hasValue() ? this.bin.value() : []));

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => this.react(event));
  }

  /** Brings a ticket back as it was; its answer goes into the ticket cache. */
  async restore(entry: DeletedTicket): Promise<Ticket> {
    const ticket = await this.api.invoke(restoreTicket, {
      tenant: this.session.tenant() as string,
      key: `${entry.project}-${entry.number}`,
    });
    this.tickets.cache.put(ticket.key, ticket);
    refresh(this.bin, this.injector);
    return ticket;
  }

  /** Removes a deleted ticket for good (docs/adr/0024 D2); nothing brings it back. */
  async purge(entry: DeletedTicket): Promise<void> {
    await this.api.invoke(purgeTicket, {
      tenant: this.session.tenant() as string,
      key: `${entry.project}-${entry.number}`,
    });
    refresh(this.bin, this.injector);
  }

  private react(event: StreamEvent): void {
    const binEvent =
      event.name === 'ticket.changed' &&
      binKinds.has(event.kind) &&
      event.key.startsWith(`${this.session.tenant()}/`);
    if (binEvent || event.name === 'resync' || event.name === 'poll') {
      refresh(this.bin, this.injector);
    }
  }
}
