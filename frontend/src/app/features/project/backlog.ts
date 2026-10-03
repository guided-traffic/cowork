import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  linkedSignal,
  signal,
  untracked,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { IconField } from 'primeng/iconfield';
import { InputIcon } from 'primeng/inputicon';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { TableLazyLoadEvent, TableModule } from 'primeng/table';
import { Tooltip } from 'primeng/tooltip';
import { Ticket, TicketState } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TicketsService } from '../../core/tickets.service';
import { SecurityBadge, SeverityBadge, StateBadge, TypeIcon } from '../../shared/badges';
import { ago, Clock } from '../../shared/time';
import { meanings } from '../../shared/vocabulary';
import { NewTicketDialog } from '../ticket/new-ticket-dialog';

const states: TicketState[] = [
  'filed',
  'analysed',
  'decided',
  'in-progress',
  'blocked',
  'done',
  'dropped',
];
export const pageSizes = [25, 50, 100] as const;
type PageSize = (typeof pageSizes)[number];

/**
 * A project's tickets as a table with numbered pages (docs/adr/0048 D2), filtered by state and
 * by full text (docs/adr/0049, 0025). The event stream keeps it current. Rank and its drag order
 * (docs/adr/0014) replace the number order when they are built.
 */
@Component({
  selector: 'app-backlog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    FormsModule,
    IconField,
    InputIcon,
    InputText,
    Select,
    NewTicketDialog,
    RouterLink,
    SecurityBadge,
    SeverityBadge,
    StateBadge,
    TableModule,
    Tooltip,
    TypeIcon,
  ],
  templateUrl: './backlog.html',
  styleUrl: './backlog.scss',
})
export class Backlog {
  readonly project = input.required<string>();

  protected readonly session = inject(SessionService);
  private readonly projects = inject(ProjectsService);
  private readonly tickets = inject(TicketsService);
  private readonly router = inject(Router);
  private readonly clock = inject(Clock);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);

  protected readonly stateOptions = states.map((state) => ({
    state,
    meaning: meanings.state[state],
  }));
  protected readonly pageSizes = [...pageSizes];
  protected readonly selectedStates = signal<TicketState[]>([]);
  protected readonly query = signal('');
  private readonly debouncedQuery = signal('');
  /** Back to the first page whenever the project changes: the router keeps this page alive. */
  protected readonly page = linkedSignal({ source: () => this.project(), computation: () => 1 });
  protected readonly creating = signal(false);
  protected readonly perPage = signal<PageSize>(25);

  protected readonly details = computed(() => this.projects.byKey(this.project()));

  private readonly list = this.tickets.projectTickets(() => {
    const tenant = this.session.tenant();
    if (!tenant) {
      return undefined;
    }
    const states = this.selectedStates();
    const q = this.debouncedQuery().trim();
    return {
      tenant,
      project: this.project(),
      page: this.page(),
      per_page: this.perPage(),
      include_terminal: states.length === 0 ? true : undefined,
      state: states.length > 0 ? states : undefined,
      q: q === '' ? undefined : q,
    };
  });

  protected readonly loading = computed(() => this.list.isLoading());
  protected readonly failure = computed(() => {
    const error = this.list.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The tickets could not be loaded: ${problem.detail || problem.title}`;
  });
  protected readonly total = computed(() =>
    this.list.hasValue() ? (this.list.value()?.total ?? 0) : 0,
  );
  protected readonly rows = computed<Ticket[]>(() =>
    (this.list.hasValue() ? (this.list.value()?.keys ?? []) : [])
      .map((key) => this.tickets.cache.entry(key)()?.value)
      .filter((ticket): ticket is Ticket => ticket !== undefined),
  );

  constructor() {
    // A text that is what the list already asks for has nothing to wait for — also the first run,
    // which would otherwise send a page chosen in the first 250 ms back to the first one.
    effect((onCleanup) => {
      const query = this.query();
      if (query === untracked(this.debouncedQuery)) {
        return;
      }
      const timer = setTimeout(() => {
        this.debouncedQuery.set(query);
        this.page.set(1);
      }, 250);
      onCleanup(() => clearTimeout(timer));
    });
  }

  protected onStates(states: TicketState[]): void {
    this.selectedStates.set(states);
    this.page.set(1);
  }

  protected onLazyLoad(event: TableLazyLoadEvent): void {
    const rows = (event.rows ?? this.perPage()) as PageSize;
    this.perPage.set(pageSizes.includes(rows) ? rows : 25);
    this.page.set(Math.floor((event.first ?? 0) / this.perPage()) + 1);
  }

  /** A ticket filed here shows at once; the event that follows changes nothing more. */
  protected filed(ticket: Ticket): void {
    this.messages.add({
      severity: 'success',
      summary: `${ticket.project}-${ticket.number} filed`,
      detail: ticket.title,
      life: 4000,
    });
    this.list.reload();
  }

  protected open(ticket: Ticket): void {
    void this.router.navigate([
      '/t',
      this.session.tenant(),
      'tickets',
      `${ticket.project}-${ticket.number}`,
    ]);
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  protected urgencyMeaning(ticket: Ticket): string {
    return meanings.urgency[ticket.urgency];
  }
}
