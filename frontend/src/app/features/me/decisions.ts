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
import { listMyDecisions } from '../../api/fn/me/list-my-decisions';
import { Decision } from '../../api/models';
import { ConditionalPages } from '../../core/conditional';
import { followPages, personPageSize, PersonPages } from '../../core/inbox.service';
import { ProblemService } from '../../core/problem.service';
import { keepShown } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { StateBadge } from '../../shared/badges';
import { ago, Clock, count } from '../../shared/time';
import { changesExistence, isImport } from '../../core/event-stream.service';
import { reloadOn, shortKey, ticketRoute } from './person-list';

/** Whom a decision waits for: the person, or anybody of the tenant (docs/adr/0011 D2). */
export function askedOf(decision: Decision, person: string | undefined): string {
  return decision.question.asked_of?.id === person && person !== undefined
    ? 'asked of you'
    : 'open in the team';
}

/**
 * "Open decisions" (docs/adr/0018 D3): the open questions asked of the person and those open in
 * their tenants, across every tenant they belong to, each beside its tenant and its ticket, in the
 * order of the tenant, the project and the ticket's place in its rank until the score exists
 * (docs/adr/0014 D5). It loads again when a question of any of the person's tenants changes, and
 * on what {@link reloadOn} follows for every person-level page (docs/adr/0054 D1).
 */
@Component({
  selector: 'app-decisions',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, RouterLink, Skeleton, StateBadge],
  template: `
    <section class="page">
      <header class="head">
        <div>
          <h1>Open decisions</h1>
          @if (list.hasValue()) {
            <p class="muted" data-testid="decisions-count">{{ headline() }}</p>
          }
        </div>
      </header>
      @if (failure(); as problem) {
        <p class="muted" data-testid="decisions-failed">
          The decisions could not be loaded: {{ problem.detail || problem.title }}
        </p>
      }
      <div class="panel">
        @for (item of items(); track item.question.id) {
          <div class="row" [attr.data-testid]="'decision-' + item.question.id">
            <span class="tenant" data-testid="tenant">{{ item.team.name }}</span>
            <a class="ticket" [routerLink]="route(item.ticket.key)">
              <span class="ticket-key tabular">{{ shortKey(item.ticket.key) }}</span>
              <span class="ticket-title">{{ item.ticket.title }}</span>
            </a>
            <app-state [value]="item.ticket.state" />
            <span class="question">
              <strong>Q{{ item.question.number }}</strong> {{ item.question.question }}
              <span class="muted" data-testid="asked-of"
                >— {{ askedOf(item, person()) }}, by {{ item.question.asked_by.display_name }}</span
              >
            </span>
            <span class="when muted">{{ ago(item.question.created_at) }}</span>
          </div>
        } @empty {
          @if (list.isLoading()) {
            <p-skeleton height="6rem" />
          } @else if (!failure()) {
            <p class="row muted" data-testid="decisions-empty">No decision waits for you.</p>
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
export class Decisions {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly problems = inject(ProblemService);
  private readonly clock = inject(Clock);

  protected readonly pages = signal(1);
  protected readonly route = ticketRoute;
  protected readonly shortKey = shortKey;
  protected readonly askedOf = askedOf;
  protected readonly person = computed(() => this.session.person()?.id);

  /** The weak `ETag`s of the pages the list holds, for a poll that finds them unchanged. */
  private readonly conditional = new ConditionalPages(this.api);

  protected readonly list: ResourceRef<PersonPages<Decision> | undefined> = resource({
    params: () => {
      const person = this.person();
      return person ? { person, pages: this.pages() } : undefined;
    },
    loader: ({ params }) =>
      keepShown(this.list, () =>
        this.conditional.load((page) =>
          followPages(params.pages, (cursor) =>
            page(listMyDecisions, { cursor, limit: personPageSize }),
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
    const shown = count(this.items().length, 'open decision');
    return this.more() ? `${shown} shown, more to load` : shown;
  });
  protected readonly failure = computed(() => {
    const error = this.list.error();
    return error ? this.problems.read(error) : undefined;
  });

  constructor() {
    reloadOn(
      this.list,
      (event) => event.name === 'question.changed' || changesExistence(event) || isImport(event),
    );
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }
}
