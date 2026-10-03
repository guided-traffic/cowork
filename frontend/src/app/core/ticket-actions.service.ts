import { HttpErrorResponse } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import { createTicket, transitionTicket, updateTicket } from '../api/functions';
import { Ticket, TicketCreate, TicketPatch, Transition } from '../api/models';
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

  async create(tenant: string, project: string, body: TicketCreate): Promise<Ticket> {
    const ticket = await this.api.invoke(createTicket, {
      tenant,
      project,
      'Idempotency-Key': crypto.randomUUID(),
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
