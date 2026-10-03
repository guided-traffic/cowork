import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Skeleton } from 'primeng/skeleton';
import { Tooltip } from 'primeng/tooltip';
import { Ticket, TicketState } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TicketsService } from '../../core/tickets.service';
import { StateBadge, TypeIcon } from '../../shared/badges';
import { ago, Clock, count } from '../../shared/time';

/** The interim front page reads one page of open tickets; the dashboard counts them all. */
export const pageSize = 100;

/** The open states in the order of docs/adr/0009 D1. */
export const openStates: TicketState[] = [
  'filed',
  'analysed',
  'decided',
  'in-progress',
  'review',
  'blocked',
];

export interface ProjectSummary {
  key: string;
  name: string;
  description: string;
  open: number;
  byState: { state: TicketState; count: number }[];
}

/** Open tickets per project and state, in board order; projects without open tickets count zero. */
export function summarise(
  projects: { key: string; name: string; description: string }[],
  tickets: Ticket[],
): ProjectSummary[] {
  return projects.map((project) => {
    const mine = tickets.filter((ticket) => ticket.project === project.key);
    return {
      key: project.key,
      name: project.name,
      description: project.description,
      open: mine.length,
      byState: openStates
        .map((state) => ({ state, count: mine.filter((ticket) => ticket.state === state).length }))
        .filter((part) => part.count > 0),
    };
  });
}

/**
 * The tenant's front page: each project with its open tickets by state, and what changed last.
 * One list request feeds both, and the event stream keeps it current.
 */
@Component({
  selector: 'app-tenant-overview',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, Skeleton, StateBadge, Tooltip, TypeIcon],
  templateUrl: './overview.html',
  styleUrl: './overview.scss',
})
export class TenantOverview {
  protected readonly session = inject(SessionService);
  protected readonly projects = inject(ProjectsService);
  private readonly tickets = inject(TicketsService);
  private readonly clock = inject(Clock);
  private readonly problems = inject(ProblemService);

  /** The newest open tickets, numbered so the answer carries the total (docs/adr/0048 D2). */
  private readonly open = this.tickets.tenantTickets(() => {
    const tenant = this.session.tenant();
    return tenant ? { tenant, page: 1, per_page: pageSize } : undefined;
  });

  protected readonly loaded = computed(() => this.open.hasValue());
  protected readonly total = computed(() =>
    this.open.hasValue() ? (this.open.value()?.total ?? 0) : 0,
  );
  protected readonly headline = computed(
    () =>
      `${count(this.total(), 'open ticket')} across ${count(this.projects.list().length, 'project')}`,
  );
  /** More open tickets than one page: the cards count the newest page only, and say so. */
  protected readonly partial = computed(() => this.total() > this.openTickets().length);
  protected readonly failure = computed(() => {
    const error = this.open.error();
    return error ? this.problems.read(error) : undefined;
  });
  protected readonly openTickets = computed(() =>
    (this.open.hasValue() ? (this.open.value()?.keys ?? []) : [])
      .map((key) => this.tickets.cache.entry(key)()?.value)
      .filter((ticket): ticket is Ticket => ticket !== undefined),
  );
  protected readonly summaries = computed(() =>
    summarise(this.projects.list(), this.openTickets()),
  );
  protected readonly recent = computed(() =>
    [...this.openTickets()].sort((a, b) => b.updated_at.localeCompare(a.updated_at)).slice(0, 8),
  );

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  protected width(count: number, total: number): string {
    return `${(100 * count) / Math.max(total, 1)}%`;
  }
}
