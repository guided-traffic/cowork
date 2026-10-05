import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  Injector,
  resource,
  ResourceRef,
  signal,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { Skeleton } from 'primeng/skeleton';
import { Api } from '../../api/api';
import { listMyInbox } from '../../api/functions';
import { InboxEntry, TenantRef, TicketRef } from '../../api/models';
import { ConditionalPages } from '../../core/conditional';
import { followPages, InboxService, personPageSize, PersonPages } from '../../core/inbox.service';
import { ProblemService } from '../../core/problem.service';
import { keepShown, refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { AgentMark } from '../../shared/agent-mark';
import { StateBadge } from '../../shared/badges';
import { ago, Clock, count } from '../../shared/time';
import { changesExistence } from '../../core/event-stream.service';
import { reloadOn, shortKey, ticketRoute } from './person-list';

/** The notifications of one ticket, newest first (docs/adr/0020 D1). */
export interface InboxGroup {
  ticket: TicketRef;
  tenant: TenantRef;
  entries: InboxEntry[];
  unread: number;
}

/** The entries grouped by ticket, the groups in the order of their newest entry. */
export function groupByTicket(entries: InboxEntry[]): InboxGroup[] {
  const groups = new Map<string, InboxGroup>();
  for (const entry of entries) {
    let group = groups.get(entry.ticket.key);
    if (!group) {
      group = { ticket: entry.ticket, tenant: entry.tenant, entries: [], unread: 0 };
      groups.set(entry.ticket.key, group);
    }
    group.entries.push(entry);
    if (!entry.read) {
      group.unread++;
    }
  }
  return [...groups.values()];
}

/**
 * What happened, from the act the notification renders (docs/adr/0020 D2, D3); the actor stands
 * before it. An act withheld because it names a ticket the person cannot see says only its kind.
 */
export function happening(entry: InboxEntry): string {
  const state = (entry.act.after as Record<string, unknown> | null)?.['state'];
  switch (entry.reason) {
    case 'assigned':
      return 'assigned it to you';
    case 'asked':
      return 'asked you a question';
    case 'answered':
      return 'answered your question';
    case 'state_changed':
      return typeof state === 'string' ? `moved it to ${state}` : 'changed its state';
    case 'blocker_closed': {
      const blocker = entry.blocker ? shortKey(entry.blocker.key) : 'a ticket';
      return `closed ${blocker}, which blocks it${typeof state === 'string' ? `, as ${state}` : ''}`;
    }
    case 'commented':
      return 'commented';
    case 'urgent':
      return 'registered an urgent need';
  }
}

/** Who made the act: the person, or the system actor. */
export function actor(entry: InboxEntry): string {
  return entry.act.actor?.display_name ?? entry.act.actor_system ?? 'someone';
}

/**
 * The person's inbox (docs/adr/0020): their notifications across every tenant they belong to,
 * grouped by ticket with the tenant beside each, each rendered from its act, newest first — live
 * through the person-level stream (docs/adr/0054 D1, D2). An entry is marked read on its own, all
 * of them up to the newest shown at once, and a group's when its ticket is opened from here.
 */
@Component({
  selector: 'app-inbox',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [AgentMark, ButtonDirective, RouterLink, Skeleton, StateBadge],
  templateUrl: './inbox.html',
  styleUrl: './person-list.scss',
})
export class Inbox {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  protected readonly inbox = inject(InboxService);
  private readonly problems = inject(ProblemService);
  private readonly clock = inject(Clock);
  private readonly injector = inject(Injector);

  protected readonly pages = signal(1);
  protected readonly marking = signal(false);
  protected readonly route = ticketRoute;
  protected readonly shortKey = shortKey;
  protected readonly happening = happening;
  protected readonly actor = actor;

  /** The person's id, a primitive, so that `me` loaded again leaves the list alone. */
  private readonly person = computed(() => this.session.person()?.id);

  /** The weak `ETag`s of the pages the list holds, for a poll that finds them unchanged. */
  private readonly conditional = new ConditionalPages(this.api);

  protected readonly list: ResourceRef<PersonPages<InboxEntry> | undefined> = resource({
    params: () => {
      const person = this.person();
      return person ? { person, pages: this.pages() } : undefined;
    },
    loader: ({ params }) =>
      keepShown(this.list, () =>
        this.conditional.load((page) =>
          followPages(params.pages, (cursor) =>
            page(listMyInbox, { cursor, limit: personPageSize }),
          ),
        ),
      ),
  });

  protected readonly entries = computed(() =>
    this.list.hasValue() ? (this.list.value()?.items ?? []) : [],
  );
  protected readonly groups = computed(() => groupByTicket(this.entries()));
  protected readonly more = computed(() =>
    this.list.hasValue() ? this.list.value()?.nextCursor != null : false,
  );
  protected readonly headline = computed(() => count(this.inbox.count(), 'unread notification'));
  protected readonly failure = computed(() => {
    const error = this.list.error();
    return error ? this.problems.read(error) : undefined;
  });

  constructor() {
    reloadOn(this.list, (event) => event.name === 'inbox.changed' || changesExistence(event));
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  protected async markRead(entry: InboxEntry): Promise<void> {
    try {
      await this.inbox.markRead(entry.id);
      refresh(this.list, this.injector);
    } catch (error) {
      this.problems.report(error);
    }
  }

  /** Marks every notification up to the newest shown read: one that arrives meanwhile stays unread. */
  protected async markAll(): Promise<void> {
    const newest = this.entries()[0];
    if (!newest) {
      return;
    }
    this.marking.set(true);
    try {
      await this.inbox.markAllRead(newest.id);
      refresh(this.list, this.injector);
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.marking.set(false);
    }
  }

  /** Opening a ticket from here reads its notifications. */
  protected opened(group: InboxGroup): void {
    for (const entry of group.entries) {
      if (!entry.read) {
        this.inbox.markRead(entry.id).catch((error: unknown) => this.problems.report(error));
      }
    }
  }
}
