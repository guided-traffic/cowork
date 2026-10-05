import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  resource,
  ResourceRef,
  signal,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { Skeleton } from 'primeng/skeleton';
import { Api } from '../../api/api';
import { listMyAssigned } from '../../api/functions';
import { MyTicket } from '../../api/models';
import { ConditionalPages } from '../../core/conditional';
import { followPages, personPageSize, PersonPages } from '../../core/inbox.service';
import { ProblemService } from '../../core/problem.service';
import { keepShown } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { SeverityBadge, StateBadge, TypeIcon } from '../../shared/badges';
import { ago, Clock, count } from '../../shared/time';
import { reloadOn, shortKey, ticketRoute } from './person-list';

/**
 * "Assigned to me" (docs/adr/0018 D3): the person's open tickets across every tenant they belong
 * to, the tenant beside each key, in the order of the tenant, the project and the project's rank
 * until the score exists (docs/adr/0014 D5). It loads again when a ticket of any of the person's
 * tenants changes, and on what {@link reloadOn} follows for every person-level page.
 */
@Component({
  selector: 'app-assigned',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, RouterLink, SeverityBadge, Skeleton, StateBadge, TypeIcon],
  template: `
    <section class="page">
      <header class="head">
        <div>
          <h1>Assigned to me</h1>
          @if (list.hasValue()) {
            <p class="muted" data-testid="assigned-count">{{ headline() }}</p>
          }
        </div>
      </header>
      @if (failure(); as problem) {
        <p class="muted" data-testid="assigned-failed">
          The tickets could not be loaded: {{ problem.detail || problem.title }}
        </p>
      }
      <div class="panel">
        @for (item of items(); track item.ticket.key) {
          <div class="row" [attr.data-testid]="'assigned-' + item.ticket.key">
            <span class="tenant" data-testid="tenant">{{ item.tenant.name }}</span>
            <a class="ticket" [routerLink]="route(item.ticket.key)">
              <app-type [value]="item.ticket.type" />
              <span class="ticket-key tabular">{{ shortKey(item.ticket.key) }}</span>
              <span class="ticket-title">{{ item.ticket.title }}</span>
            </a>
            <app-severity [value]="item.ticket.severity" />
            <app-state [value]="item.ticket.state" />
            <span class="when muted">{{ ago(item.ticket.updated_at) }}</span>
          </div>
        } @empty {
          @if (list.isLoading()) {
            <p-skeleton height="6rem" />
          } @else if (!failure()) {
            <p class="row muted" data-testid="assigned-empty">No open ticket is assigned to you.</p>
          }
        }
      </div>
      @if (more()) {
        <button
          pButton
          type="button"
          severity="secondary"
          [text]="true"
          (click)="pages.set(pages() + 1)"
          data-testid="load-more"
        >
          Load more
        </button>
      }
    </section>
  `,
  styleUrl: './person-list.scss',
})
export class Assigned {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly problems = inject(ProblemService);
  private readonly clock = inject(Clock);

  protected readonly pages = signal(1);
  protected readonly route = ticketRoute;
  protected readonly shortKey = shortKey;

  /** The person's id, a primitive, so that `me` loaded again leaves the list alone. */
  private readonly person = computed(() => this.session.person()?.id);

  /** The weak `ETag`s of the pages the list holds, for a poll that finds them unchanged. */
  private readonly conditional = new ConditionalPages(this.api);

  protected readonly list: ResourceRef<PersonPages<MyTicket> | undefined> = resource({
    params: () => {
      const person = this.person();
      return person ? { person, pages: this.pages() } : undefined;
    },
    loader: ({ params }) =>
      keepShown(this.list, () =>
        this.conditional.load((page) =>
          followPages(params.pages, (cursor) =>
            page(listMyAssigned, { cursor, limit: personPageSize }),
          ),
        ),
      ),
  });

  protected readonly items = computed(() =>
    this.list.hasValue() ? (this.list.value()?.items ?? []) : [],
  );
  protected readonly more = computed(() =>
    this.list.hasValue() ? this.list.value()?.nextCursor != null : false,
  );
  protected readonly headline = computed(() => {
    const shown = count(this.items().length, 'open ticket');
    return this.more() ? `${shown} shown, more to load` : shown;
  });
  protected readonly failure = computed(() => {
    const error = this.list.error();
    return error ? this.problems.read(error) : undefined;
  });

  constructor() {
    reloadOn(this.list, (event) => event.name === 'ticket.changed');
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }
}
