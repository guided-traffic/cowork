import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  linkedSignal,
  signal,
  Signal,
  untracked,
} from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { IconField } from 'primeng/iconfield';
import { InputIcon } from 'primeng/inputicon';
import { InputText } from 'primeng/inputtext';
import { Paginator } from 'primeng/paginator';
import { Select } from 'primeng/select';
import { Skeleton } from 'primeng/skeleton';
import { Tooltip } from 'primeng/tooltip';
import type { PaginatorState } from 'primeng/types/paginator';
import { ListTenantTickets$Params } from '../../api/fn/tickets/list-tenant-tickets';
import { SavedFilter, SavedFilterParameters, Ticket } from '../../api/models';
import { EFFORT } from '../../api/models/effort-array';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TicketPage, TicketsService } from '../../core/tickets.service';
import { SecurityBadge, SeverityBadge, StateBadge, TypeIcon } from '../../shared/badges';
import { SizeIcon } from '../../shared/size';
import { ago, Clock, count } from '../../shared/time';
import { meanings } from '../../shared/vocabulary';
import { describe } from '../project/saved-filter-model';
import { SavedFilters } from '../project/saved-filters';
import {
  BarName,
  beyondBar,
  Choice,
  choosing,
  filterOf,
  plain,
  queryOf,
  sameList,
  withChosen,
} from './tenant-tickets-model';

/** The sizes of a numbered page (docs/adr/0048 D2). */
export type PerPage = NonNullable<ListTenantTickets$Params['per_page']>;

/** A page holds as many tickets as the API gives when it is not told (docs/adr/0048 D2). */
export const defaultPerPage: PerPage = 50;

/** How long the text waits after the last keystroke before the list asks for it. */
export const textDelay = 250;

/** A select of the bar: the parameter it writes, its label, its options and what it shows chosen. */
interface Field {
  name: BarName;
  label: string;
  /** What the select says while it holds nothing. */
  placeholder: string;
  options: Signal<Choice[]>;
  chosen: Signal<string[]>;
}

const vocabulary = (values: readonly string[]): Choice[] =>
  values.map((value) => ({ value, label: value }));

/**
 * The tenant's tickets across its projects (docs/adr/0018 D5, docs/adr/0023 D4): a table over
 * `GET …/tickets`, newest first, the project beside each key. The filters are the address's — every
 * filter of the ticket lists (docs/adr/0049 D1), the bar's selects writing theirs —, so that a
 * filtered list can be linked and comes back as it was; a saved filter applies to it as it does to
 * the backlog, `project` included. The table shows numbered pages with a choice of their size
 * (docs/adr/0048 D2, D4), and another filter or size starts at the first. The list is the tickets
 * service's, so the event stream of the tenant reloads it and refetches the tickets it shows, and a
 * poll that finds it unchanged costs a `304` (docs/adr/0054). A global administrator without a role
 * here sees none of the tenant's work (docs/adr/0034 D2).
 */
@Component({
  selector: 'app-tenant-tickets',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    FormsModule,
    IconField,
    InputIcon,
    InputText,
    Paginator,
    RouterLink,
    SavedFilters,
    SecurityBadge,
    Select,
    SeverityBadge,
    SizeIcon,
    Skeleton,
    StateBadge,
    Tooltip,
    TypeIcon,
  ],
  templateUrl: './tenant-tickets.html',
  styleUrl: './tenant-tickets.scss',
})
export class TenantTickets {
  protected readonly session = inject(SessionService);
  private readonly projects = inject(ProjectsService);
  private readonly members = inject(MembersService);
  private readonly tickets = inject(TicketsService);
  private readonly problems = inject(ProblemService);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly clock = inject(Clock);

  protected readonly meanings = meanings;
  protected readonly perPageOptions: PerPage[] = [25, 50, 100];

  private readonly address = toSignal(this.route.queryParamMap, { requireSync: true });
  /** The filter of the address; the same conditions in another address are the same filter. */
  protected readonly filter = computed(() => filterOf(this.address()), {
    equal: (a, b) => JSON.stringify(a) === JSON.stringify(b),
  });

  /** The members by name, with the username where there is one: two of one name stay apart. */
  private readonly people = computed<Choice[]>(() =>
    this.members.list().map(({ person }) => ({
      value: person.id,
      label: person.username ? `${person.display_name} (${person.username})` : person.display_name,
    })),
  );
  protected readonly fields: Field[] = [
    this.field('project', 'Project', 'Every project', () =>
      this.projects.list().map(({ key, name }) => ({ value: key, label: `${key} · ${name}` })),
    ),
    this.field('state', 'State', 'Every open state', () => vocabulary(Object.keys(meanings.state))),
    this.field('type', 'Type', 'Any type', () => vocabulary(Object.keys(meanings.type))),
    this.field('severity', 'Severity', 'Any severity', () =>
      vocabulary(Object.keys(meanings.severity)),
    ),
    this.field('security', 'Security', 'Any class', () =>
      vocabulary(Object.keys(meanings.security)),
    ),
    this.field('urgency', 'Horizon', 'Any horizon', () =>
      vocabulary(Object.keys(meanings.urgency)),
    ),
    this.field('effort', 'Effort', 'Any effort', () => vocabulary(EFFORT)),
    this.field('assignee', 'Assignee', 'Anyone', () => [
      { value: 'me', label: 'Me' },
      { value: 'none', label: 'Unassigned' },
      ...this.people(),
    ]),
    this.field('reporter', 'Reporter', 'Anyone', () => [
      { value: 'me', label: 'Me' },
      ...this.people(),
    ]),
  ];

  /**
   * What the text field holds: the address's `q`, and what the person types until the list asks
   * for it — kept as typed while it trims to the address's, so a space at its end stays.
   */
  protected readonly text = linkedSignal<string, string>({
    source: () => this.filter().q ?? '',
    computation: (q, previous) => (previous && previous.value.trim() === q ? previous.value : q),
  });

  /** The saved filter applied last; one belongs to its tenant, and another tenant starts without. */
  protected readonly applied = linkedSignal<string | null, SavedFilter | null>({
    source: () => this.session.tenant(),
    computation: () => null,
  });
  /** A filter's conditions the bar has no control for, as a line; null where there are none. */
  protected readonly beyond = computed(() => {
    const rest = beyondBar(this.filter());
    return Object.keys(rest).length > 0 ? describe(rest) : null;
  });
  protected readonly filtered = computed(() => Object.keys(this.filter()).length > 0);

  protected readonly perPage = signal<PerPage>(defaultPerPage);
  /** The page shown; another tenant, filter or size starts at the first again. */
  protected readonly page = linkedSignal<unknown, number>({
    source: () => [this.session.workTenant(), this.filter(), this.perPage()],
    computation: () => 1,
  });

  /** What the list asks for; nothing under a tenant whose work the person does not see. */
  private readonly request = computed<ListTenantTickets$Params | undefined>(() => {
    const tenant = this.session.workTenant();
    return tenant
      ? { ...this.filter(), tenant, page: this.page(), per_page: this.perPage() }
      : undefined;
  });
  protected readonly list = this.tickets.tenantTickets(() => this.request());

  /**
   * The last answer, kept while the next page or another filter loads, so that the rows and the
   * pages — and the page's button, which may hold the focus — stay where they are; a failure, or
   * another tenant, shows none.
   */
  private readonly shown = linkedSignal<
    { tenant: string | null; page: TicketPage | undefined },
    TicketPage | undefined
  >({
    source: () => ({
      tenant: this.session.workTenant(),
      page: this.list.hasValue() ? this.list.value() : undefined,
    }),
    computation: (source, previous) =>
      source.page ??
      (this.list.isLoading() && previous?.source.tenant === source.tenant
        ? previous.value
        : undefined),
  });
  protected readonly rows = computed(() =>
    (this.shown()?.keys ?? [])
      .map((key) => this.tickets.cache.entry(key)()?.value)
      .filter((ticket): ticket is Ticket => ticket !== undefined),
  );
  protected readonly total = computed(() => this.shown()?.total ?? 0);
  /** The total of the answer shown, `120 tickets`; nothing before the first. */
  protected readonly summary = computed(() =>
    this.shown() ? count(this.total(), 'ticket') : null,
  );
  protected readonly failure = computed(() => {
    const error = this.list.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    const fields = Object.entries(problem.fields).map(([name, message]) => `${name}: ${message}`);
    const because = fields.length > 0 ? ` (${fields.join('; ')})` : '';
    return `The tickets could not be loaded: ${problem.detail || problem.title}${because}`;
  });

  constructor() {
    // The text goes into the address once the person stops typing; a text that is what the address
    // says already has nothing to wait for.
    effect((onCleanup) => {
      const typed = this.text().trim();
      if (typed === (untracked(this.filter).q ?? '')) {
        return;
      }
      const timer = setTimeout(
        () => this.go({ ...this.filter(), q: typed || undefined }),
        textDelay,
      );
      onCleanup(() => clearTimeout(timer));
    });
    // A list that shrank while a later page was shown — tickets closed, another filter elsewhere —
    // goes to its last page instead of showing an empty one.
    effect(() => {
      const total = this.shown()?.total;
      if (total === undefined) {
        return;
      }
      const last = Math.max(1, Math.ceil(total / this.perPage()));
      if (untracked(this.page) > last) {
        this.page.set(last);
      }
    });
  }

  /** A select's choice goes into the address, its negated values kept. */
  protected choose(name: BarName, values: string[] | null): void {
    this.go(choosing(this.filter(), name, values ?? []));
  }

  /** A saved filter's conditions replace the address's, `project` among them; none clears them. */
  protected applySaved(filter: SavedFilter | null): void {
    this.applied.set(filter);
    this.go(filter?.parameters ?? {});
  }

  protected clear(): void {
    this.applied.set(null);
    this.go({});
  }

  protected turn(state: PaginatorState): void {
    const rows = (state.rows ?? this.perPage()) as PerPage;
    if (rows !== this.perPage()) {
      this.perPage.set(rows);
      return;
    }
    this.page.set((state.page ?? 0) + 1);
  }

  protected open(ticket: Ticket): void {
    void this.router.navigate(['/t', this.session.tenant(), 'tickets', this.shortKey(ticket.key)]);
  }

  protected shortKey(key: string): string {
    return key.slice(key.indexOf('/') + 1);
  }

  /** A project by its name; one the list of projects does not hold — archived — by its key. */
  protected projectName(key: string): string {
    return this.projects.byKey(key)?.name ?? key;
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  /** The filter goes into the address, so that the list can be linked and comes back as it was. */
  private go(filter: SavedFilterParameters): void {
    void this.router.navigate([], {
      relativeTo: this.route,
      queryParams: queryOf(filter),
      replaceUrl: true,
    });
  }

  /**
   * A select of the bar. Its options and its choice keep their identity while they hold the same:
   * a choice made in an open select goes through the address and comes back, which must not
   * build the select's options anew under the pointer.
   */
  private field(name: BarName, label: string, placeholder: string, known: () => Choice[]): Field {
    const chosen = computed(() => plain(this.filter()[name]), { equal: sameList });
    const options = computed(() => withChosen(known(), chosen()), { equal: sameList });
    return { name, label, placeholder, options, chosen };
  }
}
