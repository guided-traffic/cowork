import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  resource,
  ResourceRef,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { Skeleton } from 'primeng/skeleton';
import { Tooltip } from 'primeng/tooltip';
import { Api } from '../../api/api';
import { listMyAssigned, listMyNext } from '../../api/functions';
import { MyTicket } from '../../api/models';
import { followPages, personPageSize, PersonPages } from '../../core/inbox.service';
import { ProblemService } from '../../core/problem.service';
import { keepShown } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { SeverityBadge, StateBadge, TypeIcon } from '../../shared/badges';
import { ago, Clock, count } from '../../shared/time';
import { reloadOn, shortKey, ticketRoute } from './person-list';

/** Which of the person's lists of tickets a page shows (docs/adr/0018 D3). */
export type MyList = 'next' | 'assigned';

/** What each list says of itself. */
const lists: Record<MyList, { title: string; lead: string; empty: string; op: typeof listMyNext }> =
  {
    next: {
      title: 'Next for me',
      lead: 'Your open tickets and the unassigned ones of the projects you see, by score',
      empty: 'Nothing is open for you, and nothing is unassigned.',
      op: listMyNext,
    },
    assigned: {
      title: 'Assigned to me',
      lead: 'Your open tickets, by score',
      empty: 'No open ticket is assigned to you.',
      op: listMyAssigned,
    },
  };

/**
 * The person's lists of tickets across every tenant they belong to (docs/adr/0018 D3), each item
 * beside its tenant: "next for me" — their open tickets and the unassigned open tickets of the
 * projects they see, the start page (docs/adr/0023 D4) — and "assigned to me". Both are in the
 * score's order, the place in the project's backlog beside it (docs/adr/0014 D5). A list loads
 * again when the person-level stream says it may have changed: the person's inbox, a ticket, a
 * stake — which moves a score — or a project's rank, and on `resync` and `poll`.
 */
@Component({
  selector: 'app-my-tickets',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, RouterLink, SeverityBadge, Skeleton, StateBadge, Tooltip, TypeIcon],
  template: `
    <section class="page">
      <header class="head">
        <div>
          <h1 data-testid="my-title">{{ text().title }}</h1>
          <p class="muted" data-testid="my-lead">{{ text().lead }}</p>
          @if (pages.hasValue()) {
            <p class="muted" [attr.data-testid]="list() + '-count'">{{ headline() }}</p>
          }
        </div>
      </header>
      @if (failure(); as problem) {
        <p class="muted" [attr.data-testid]="list() + '-failed'">
          The tickets could not be loaded: {{ problem.detail || problem.title }}
        </p>
      }
      <div class="panel">
        @for (item of items(); track item.ticket.key) {
          <div class="row" [attr.data-testid]="list() + '-' + item.ticket.key">
            <span class="tenant" data-testid="tenant">{{ item.tenant.name }}</span>
            <a class="ticket" [routerLink]="route(item.ticket.key)">
              <app-type [value]="item.ticket.type" />
              <span class="ticket-key tabular">{{ shortKey(item.ticket.key) }}</span>
              <span class="ticket-title">{{ item.ticket.title }}</span>
            </a>
            <span
              class="place tabular"
              tabindex="0"
              [pTooltip]="placeText(item)"
              tooltipEvent="both"
              [showDelay]="200"
              data-testid="place"
              >{{ item.ticket.urgency }} #{{ item.place
              }}<span class="sr-only">: {{ placeText(item) }}</span></span
            >
            <app-severity [value]="item.ticket.severity" />
            <app-state [value]="item.ticket.state" />
            @if (list() === 'next') {
              <span class="whose muted" data-testid="whose">{{
                item.ticket.assignee ? 'yours' : 'unassigned'
              }}</span>
            }
            <span class="when muted">{{ ago(item.ticket.updated_at) }}</span>
          </div>
        } @empty {
          @if (pages.isLoading()) {
            <p-skeleton height="6rem" />
          } @else if (!failure()) {
            <p class="row muted" [attr.data-testid]="list() + '-empty'">{{ text().empty }}</p>
          }
        }
      </div>
      @if (more()) {
        <button
          pButton
          type="button"
          severity="secondary"
          [text]="true"
          (click)="loaded.set(loaded() + 1)"
          data-testid="load-more"
        >
          Load more
        </button>
      }
    </section>
  `,
  styleUrl: './person-list.scss',
  styles: `
    .place {
      flex: none;
      padding: 0.125rem 0.5rem;
      border-radius: 999px;
      font-size: 0.75rem;
      color: var(--p-text-muted-color);
      border: 1px solid var(--p-app-border);
      cursor: help;
    }
    .whose {
      flex: none;
      width: 6rem;
      font-size: 0.8125rem;
    }
  `,
})
export class MyTickets {
  /** The list the route names; "next for me" where none is named, as on the start page. */
  readonly list = input<MyList>('next');

  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly problems = inject(ProblemService);
  private readonly clock = inject(Clock);

  /** How many pages are loaded: one again when the page shows the other list. */
  protected readonly loaded = linkedSignal({ source: () => this.list(), computation: () => 1 });
  protected readonly route = ticketRoute;
  protected readonly shortKey = shortKey;
  protected readonly text = computed(() => lists[this.list()]);

  /** The person's id, a primitive, so that `me` loaded again leaves the list alone. */
  private readonly person = computed(() => this.session.person()?.id);

  protected readonly pages: ResourceRef<PersonPages<MyTicket> | undefined> = resource({
    params: () => {
      const person = this.person();
      return person ? { person, list: this.list(), pages: this.loaded() } : undefined;
    },
    loader: ({ params }) =>
      keepShown(this.pages, () =>
        followPages(params.pages, (cursor) =>
          this.api.invoke(lists[params.list].op, { cursor, limit: personPageSize }),
        ),
      ),
  });

  protected readonly items = computed(() =>
    this.pages.hasValue() ? (this.pages.value()?.items ?? []) : [],
  );
  protected readonly more = computed(() =>
    this.pages.hasValue() ? this.pages.value()?.nextCursor != null : false,
  );
  protected readonly headline = computed(() => {
    const shown = count(this.items().length, 'open ticket');
    return this.more() ? `${shown} shown, more to load` : shown;
  });
  protected readonly failure = computed(() => {
    const error = this.pages.error();
    return error ? this.problems.read(error) : undefined;
  });

  constructor() {
    reloadOn(
      this.pages,
      (event) =>
        event.name === 'inbox.changed' ||
        event.name === 'ticket.changed' ||
        event.name === 'interest.changed' ||
        event.name === 'project.changed',
    );
  }

  /** The place in the backlog and the score, which orders the list. */
  protected placeText(item: MyTicket): string {
    const project = shortKey(item.ticket.key).split('-')[0];
    const score =
      item.ticket.score === null ? 'no score yet' : `score ${item.ticket.score.toFixed(1)}`;
    return `#${item.place} of ${item.ticket.urgency} in the backlog of ${project}; ${score}`;
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }
}
