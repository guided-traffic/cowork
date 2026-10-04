import { HttpErrorResponse } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import {
  createTicket,
  moveTicketRank,
  overrideUrgency,
  transitionTicket,
  updateTicket,
  withdrawUrgencyOverride,
} from '../api/functions';
import {
  Ticket,
  TicketCreate,
  TicketPatch,
  Transition,
  Urgency,
  UrgencyOverrideSet,
} from '../api/models';
import { etagOf } from './entity-cache';
import { ProblemService, ProblemView } from './problem.service';
import { splitKey, TicketsService } from './tickets.service';

/**
 * A `412`: the ticket changed since it was read (docs/adr/0050 D5). It names the fields the write
 * tried to change with the server's current values and carries the fresh ticket, so the person
 * can decide: keep theirs, or write again over the new version.
 */
export class StaleWrite extends Error {
  constructor(
    readonly problem: ProblemView,
    readonly current: Ticket,
  ) {
    super(problem.detail || 'The ticket changed since it was read');
  }
}

/** Where a move puts a ticket: directly after, or directly before, another ticket of its project. */
export type RankPlacement = { after: number } | { before: number };

/** `acme/VKO-12` → the address a ticket's own routes take. */
export function routeOf(key: string): { tenant: string; project: string; number: number } {
  const { tenant, key: short } = splitKey(key);
  const dash = short.lastIndexOf('-');
  return { tenant, project: short.slice(0, dash), number: Number(short.slice(dash + 1)) };
}

/**
 * The writes on a ticket (docs/adr/0018 D1, D2). Every answer goes into the ticket cache at once,
 * so every view shows it before the event returns — and the event of one's own write then
 * refetches nothing, because the cache holds that version already.
 */
@Injectable({ providedIn: 'root' })
export class TicketActions {
  private readonly api = inject(Api);
  private readonly tickets = inject(TicketsService);
  private readonly problems = inject(ProblemService);

  /**
   * Files a ticket. The key is the form's, one for each content it holds, so a retry of a lost
   * answer is answered again instead of filing the ticket twice (docs/adr/0045 D3).
   */
  async create(
    tenant: string,
    project: string,
    body: TicketCreate,
    idempotencyKey: string,
  ): Promise<Ticket> {
    const ticket = await this.api.invoke(createTicket, {
      tenant,
      project,
      'Idempotency-Key': idempotencyKey,
      body,
    });
    this.tickets.cache.put(ticket.key, ticket);
    return ticket;
  }

  /** Changes fields with the cached version as `If-Match`; a `412` becomes a {@link StaleWrite}. */
  async update(key: string, patch: TicketPatch): Promise<Ticket> {
    const held = this.tickets.cache.value(key) ?? (await this.tickets.refresh(key));
    try {
      const ticket = await this.api.invoke(updateTicket, {
        ...routeOf(key),
        'If-Match': this.tickets.cache.etag(key) ?? etagOf(held.version),
        body: patch,
      });
      this.tickets.cache.put(ticket.key, ticket);
      return ticket;
    } catch (error) {
      if (error instanceof HttpErrorResponse && error.status === 412) {
        const current = await this.tickets.refresh(key);
        throw new StaleWrite(this.problems.read(error), current);
      }
      throw error;
    }
  }

  /**
   * Places the ticket directly after or before another open ticket of its project (docs/adr/0014
   * D2). A move overwrites nothing, so it takes no `If-Match` and never meets a `412`
   * (docs/adr/0050 D4); it raises the ticket's version, and the answer replaces the cache entry.
   */
  async rank(key: string, placement: RankPlacement): Promise<Ticket> {
    const ticket = await this.api.invoke(moveTicketRank, { ...routeOf(key), body: placement });
    this.tickets.cache.put(ticket.key, ticket);
    return ticket;
  }

  /**
   * Sets the urgency override (docs/adr/0010 D3). `reason` goes into the request only when the
   * person gave one: it is optional for a person and never made up.
   */
  overrideUrgency(key: string, value: Urgency, reason?: string): Promise<Ticket> {
    const body: UrgencyOverrideSet = { value, ...(reason ? { reason } : {}) };
    return this.writeUrgency(key, (etag) =>
      this.api.invoke(overrideUrgency, { ...routeOf(key), 'If-Match': etag, body }),
    );
  }

  /** Withdraws the override, so that the derived urgency holds again; it needs no reason. */
  withdrawUrgency(key: string): Promise<Ticket> {
    return this.writeUrgency(key, (etag) =>
      this.api.invoke(withdrawUrgencyOverride, { ...routeOf(key), 'If-Match': etag }),
    );
  }

  /**
   * Writes the urgency with the cached `ETag`. A `412` refetches the ticket: while its urgency is
   * still the one the cache held, the write is repeated once over the new version, because what
   * changed was another field; otherwise someone else decided the urgency, and the person is told
   * with a {@link StaleWrite} (docs/adr/0050 D5).
   */
  private async writeUrgency(
    key: string,
    write: (etag: string) => Promise<Ticket>,
    retried = false,
  ): Promise<Ticket> {
    const held = this.tickets.cache.value(key) ?? (await this.tickets.refresh(key));
    try {
      const ticket = await write(this.tickets.cache.etag(key) ?? etagOf(held.version));
      this.tickets.cache.put(ticket.key, ticket);
      return ticket;
    } catch (error) {
      if (!(error instanceof HttpErrorResponse && error.status === 412)) {
        throw error;
      }
      const current = await this.tickets.refresh(key);
      if (retried || current.urgency !== held.urgency) {
        throw new StaleWrite(this.problems.read(error), current);
      }
      return this.writeUrgency(key, write, true);
    }
  }

  /** Moves the ticket from the state the cache holds (docs/adr/0045 D2). */
  async transition(key: string, move: Omit<Transition, 'from'>): Promise<Ticket> {
    const held = this.tickets.cache.value(key) ?? (await this.tickets.refresh(key));
    const ticket = await this.api.invoke(transitionTicket, {
      ...routeOf(key),
      body: { ...move, from: held.state },
    });
    this.tickets.cache.put(ticket.key, ticket);
    return ticket;
  }
}
