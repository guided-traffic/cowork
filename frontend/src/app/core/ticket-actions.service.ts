import { HttpErrorResponse } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import {
  createTicket,
  moveTicketRank,
  overrideUrgency,
  replaceTicketBody,
  setConfidential,
  transitionTicket,
  updateTicket,
  withdrawUrgencyOverride,
} from '../api/functions';
import {
  ConfidentialSet,
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

/** The fields of a patch that it changes; what only explains or qualifies the act is left out. */
const qualifiers = new Set(['comment', 'note', 'reason', 'override_prerequisites']);

/** Whether two values of a field are the same, an object by its content. */
function same(a: unknown, b: unknown): boolean {
  return JSON.stringify(a ?? null) === JSON.stringify(b ?? null);
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

  /**
   * Changes fields with the cached version as `If-Match`; a `412` becomes a {@link StaleWrite}.
   *
   * An editor that was open for a while passes `since`, the ticket as it was when the editing
   * began: the write is then made over that version, never over a newer one the cache took from an
   * event meanwhile, which would write over a change the person never saw (docs/adr/0050 D3). Its
   * `412` is written once more over the current version while each field the patch changes still
   * has the value it had then — what changed was another field —, and is a {@link StaleWrite}
   * otherwise.
   */
  async update(key: string, patch: TicketPatch, since?: Ticket): Promise<Ticket> {
    if (since) {
      const fields = Object.keys(patch).filter((field) => !qualifiers.has(field));
      return this.writeOver(
        key,
        (etag) => this.api.invoke(updateTicket, { ...routeOf(key), 'If-Match': etag, body: patch }),
        (current) =>
          fields.every((field) =>
            same(current[field as keyof Ticket], since[field as keyof Ticket]),
          ),
        etagOf(since.version),
      );
    }
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
   * Replaces the body as a whole (docs/adr/0011 D1) over `since`, the ticket as the editor began
   * with it; a `412` is written once more while the body is still the one it began with, and is a
   * {@link StaleWrite} once somebody else changed the body.
   */
  replaceBody(key: string, body: string, since: Ticket): Promise<Ticket> {
    return this.writeOver(
      key,
      (etag) =>
        this.api.invoke(replaceTicketBody, { ...routeOf(key), 'If-Match': etag, body: { body } }),
      (current) => current.body === since.body,
      etagOf(since.version),
    );
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
   * Sets the horizon, the API's urgency override (docs/adr/0010 D3). `reason` goes into the
   * request only when the person gave one: it is optional for a person and never made up.
   */
  async overrideUrgency(key: string, value: Urgency, reason?: string): Promise<Ticket> {
    const body: UrgencyOverrideSet = { value, ...(reason ? { reason } : {}) };
    const held = this.tickets.cache.value(key) ?? (await this.tickets.refresh(key));
    return this.writeOver(
      key,
      (etag) => this.api.invoke(overrideUrgency, { ...routeOf(key), 'If-Match': etag, body }),
      (current) => current.urgency === held.urgency,
    );
  }

  /** Withdraws the horizon set on the ticket, which returns it to `later`; it needs no reason. */
  async withdrawUrgency(key: string): Promise<Ticket> {
    const held = this.tickets.cache.value(key) ?? (await this.tickets.refresh(key));
    return this.writeOver(
      key,
      (etag) => this.api.invoke(withdrawUrgencyOverride, { ...routeOf(key), 'If-Match': etag }),
      (current) => current.urgency === held.urgency,
    );
  }

  /**
   * Sets or lifts the confidential flag (docs/adr/0065 D2, D3): a tenant administrator's act,
   * lifting with a reason.
   */
  async setConfidential(key: string, confidential: boolean, reason?: string): Promise<Ticket> {
    const body: ConfidentialSet = { confidential, ...(reason ? { reason } : {}) };
    const held = this.tickets.cache.value(key) ?? (await this.tickets.refresh(key));
    return this.writeOver(
      key,
      (etag) => this.api.invoke(setConfidential, { ...routeOf(key), 'If-Match': etag, body }),
      (current) => current.confidential === held.confidential,
    );
  }

  /**
   * Writes with `etag` as `If-Match` — the cached `ETag` where none is given. A `412` refetches the
   * ticket: while `unchanged` says that what the write changes is still as it was read, the write
   * is repeated once over the new version, because what changed was something else; otherwise
   * someone else decided it, and the person is told with a {@link StaleWrite} (docs/adr/0050 D5).
   */
  private async writeOver(
    key: string,
    write: (etag: string) => Promise<Ticket>,
    unchanged: (current: Ticket) => boolean,
    etag?: string,
    retried = false,
  ): Promise<Ticket> {
    let sent = etag;
    if (sent === undefined) {
      const held = this.tickets.cache.value(key) ?? (await this.tickets.refresh(key));
      sent = this.tickets.cache.etag(key) ?? etagOf(held.version);
    }
    try {
      const ticket = await write(sent);
      this.tickets.cache.put(ticket.key, ticket);
      return ticket;
    } catch (error) {
      if (!(error instanceof HttpErrorResponse && error.status === 412)) {
        throw error;
      }
      const current = await this.tickets.refresh(key);
      if (retried || !unchanged(current)) {
        throw new StaleWrite(this.problems.read(error), current);
      }
      return this.writeOver(key, write, unchanged, etagOf(current.version), true);
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
