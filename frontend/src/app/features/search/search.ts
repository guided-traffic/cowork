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
import { Api } from '../../api/api';
import { searchMyTenants } from '../../api/fn/search/search-my-tenants';
import { searchTenant } from '../../api/fn/search/search-tenant';
import { SearchHit } from '../../api/models';
import { followPages, personPageSize, PersonPages } from '../../core/inbox.service';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { StateBadge, TypeIcon } from '../../shared/badges';
import { count } from '../../shared/time';
import { shortKey, ticketRoute } from '../me/person-list';

/** Where a search looks: the tenant the pages show, or every tenant of the person. */
export type SearchScope = 'tenant' | 'me';

/** Where a hit was found, as the page says it (docs/adr/0025 D5). */
export function foundIn(hit: SearchHit): string {
  switch (hit.found_in) {
    case 'key':
      return 'by its key';
    case 'comment':
      return 'in a comment';
    case 'question':
      return hit.question === null ? 'in a question' : `in question Q${hit.question}`;
    case 'attachment':
      return 'in a file name';
    default:
      return 'in the ticket';
  }
}

/**
 * The part of the ticket's page a hit links to: the comment or the question it was found in, which
 * the page scrolls to; none for the rest.
 */
export function hitFragment(hit: SearchHit): string | undefined {
  if (hit.found_in === 'comment' && hit.comment) {
    return `comment-${hit.comment}`;
  }
  if (hit.found_in === 'question' && hit.question !== null) {
    return `question-${hit.question}`;
  }
  return undefined;
}

/**
 * The results of a search (docs/adr/0018 D7, docs/adr/0025): `/t/:tenant/search?q=` searches the
 * tenant the pages show — what the search box in the top bar does inside a tenant, that tenant
 * first —, `/me/search?q=` every tenant of the person, each hit beside its tenant's name. A hit is
 * the ticket — type, key, title, state —, where it was found and the snippet, its found words marked;
 * the snippet is text in parts, shown by interpolation, never as markup. A hit in a comment or a
 * question links to it on the ticket's page. The best hits first, fifty at a time, *Load more* for
 * the next; a search is a snapshot and does not follow the event stream.
 */
@Component({
  selector: 'app-search-results',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, RouterLink, Skeleton, StateBadge, TypeIcon],
  template: `
    <section class="page">
      <header class="head">
        <div>
          <h1>Search</h1>
          @if (query()) {
            <p class="muted" data-testid="search-headline">
              {{ headline() }} for “{{ query() }}” {{ where() }}
            </p>
          } @else {
            <p class="muted" data-testid="search-empty-query">
              Type what to find in the search box at the top.
            </p>
          }
        </div>
        @if (query() && widens()) {
          <a
            pButton
            severity="secondary"
            [text]="true"
            routerLink="/me/search"
            [queryParams]="{ q: query() }"
            data-testid="search-everywhere"
            >Search all your tenants</a
          >
        }
      </header>
      @if (failure(); as problem) {
        <p class="muted" data-testid="search-failed">
          The search failed: {{ problem.detail || problem.title }}
        </p>
      }
      @if (query()) {
        <div class="panel">
          @for (hit of items(); track hit.key) {
            <a
              class="hit"
              [routerLink]="route(hit.key)"
              [fragment]="fragment(hit)"
              [attr.data-testid]="'hit-' + hit.key"
            >
              <span class="line">
                @if (scope() === 'me') {
                  <span class="tenant" data-testid="tenant">{{ hit.tenant.name }}</span>
                }
                <app-type [value]="hit.type" />
                <span class="ticket-key tabular">{{ shortKey(hit.key) }}</span>
                <span class="ticket-title">{{ hit.title }}</span>
                <app-state [value]="hit.state" />
              </span>
              <span class="found muted" data-testid="found-in">{{ foundIn(hit) }}</span>
              @if (hit.snippet.length > 0) {
                <!-- Every part in an element: the parts carry their own spaces, the template adds none. -->
                <span class="snippet" data-testid="snippet">
                  @for (part of hit.snippet; track $index) {
                    @if (part.match) {
                      <mark>{{ part.text }}</mark>
                    } @else {
                      <span>{{ part.text }}</span>
                    }
                  }
                </span>
              }
            </a>
          } @empty {
            @if (list.isLoading()) {
              <p-skeleton height="6rem" />
            } @else if (!failure()) {
              <p class="hit muted" data-testid="search-nothing">Nothing found.</p>
            }
          }
        </div>
      }
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
  styleUrl: '../me/person-list.scss',
  styles: `
    .hit {
      display: flex;
      flex-direction: column;
      gap: 0.25rem;
      padding: 0.625rem 1rem;
      color: var(--p-text-color);
      border-bottom: 1px solid var(--p-app-border);
      &:last-child {
        border-bottom: 0;
      }
      &:hover {
        text-decoration: none;
        background: var(--p-app-hover);
        .ticket-title {
          text-decoration: underline;
        }
      }
    }
    .line {
      display: flex;
      align-items: center;
      gap: 0.75rem;
      min-width: 0;
    }
    .found {
      font-size: 0.75rem;
    }
    .snippet {
      font-size: 0.8125rem;
      color: var(--p-text-muted-color);
      overflow-wrap: anywhere;
    }
    mark {
      padding: 0 0.125rem;
      border-radius: var(--p-border-radius-sm);
      color: var(--p-text-color);
      background: color-mix(in srgb, var(--p-primary-color) 24%, transparent);
    }
  `,
})
export class SearchResults {
  /** `q` of the address: the words to find. */
  readonly q = input<string>();
  /** The route's: the tenant's search, or the person's across their tenants. */
  readonly scope = input<SearchScope>('me');

  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly problems = inject(ProblemService);

  protected readonly route = ticketRoute;
  protected readonly shortKey = shortKey;
  protected readonly foundIn = foundIn;
  protected readonly fragment = hitFragment;

  protected readonly query = computed(() => (this.q() ?? '').trim());
  /** The tenant searched, for the tenant's search. */
  private readonly tenant = computed(() =>
    this.scope() === 'tenant' ? this.session.tenant() : null,
  );
  /** How many pages are shown; another query or another tenant starts at one again. */
  protected readonly pages = linkedSignal({
    source: () => `${this.scope()}|${this.tenant()}|${this.query()}`,
    computation: () => 1,
  });

  protected readonly list: ResourceRef<PersonPages<SearchHit> | undefined> = resource({
    params: () => {
      const q = this.query();
      const tenant = this.tenant();
      if (!q || (this.scope() === 'tenant' && !tenant)) {
        return undefined;
      }
      return { q, tenant, pages: this.pages() };
    },
    loader: ({ params: { q, tenant, pages } }) =>
      followPages(pages, (cursor) =>
        tenant
          ? this.api.invoke(searchTenant, { tenant, q, cursor, limit: personPageSize })
          : this.api.invoke(searchMyTenants, { q, cursor, limit: personPageSize }),
      ),
  });

  protected readonly items = computed(() =>
    this.list.hasValue() ? (this.list.value()?.items ?? []) : [],
  );
  protected readonly more = computed(() =>
    this.list.hasValue() ? this.list.value()?.nextCursor != null : false,
  );
  protected readonly headline = computed(() => {
    const shown = count(this.items().length, 'ticket');
    return this.more() ? `${shown} shown, more to load,` : shown;
  });
  protected readonly where = computed(() => {
    if (this.scope() === 'me') {
      return 'in all your tenants';
    }
    return `in ${this.session.shown()?.name ?? this.tenant() ?? 'this tenant'}`;
  });
  /** The tenant's search offers the person's other tenants where they have some. */
  protected readonly widens = computed(
    () => this.scope() === 'tenant' && this.session.memberships().length > 1,
  );
  protected readonly failure = computed(() => {
    const error = this.list.error();
    return error ? this.problems.read(error) : undefined;
  });
}
