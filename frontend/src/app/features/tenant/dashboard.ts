import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { Skeleton } from 'primeng/skeleton';
import { Dashboard } from '../../api/models';
import { DashboardService } from '../../core/dashboard.service';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { SecurityBadge, SeverityBadge, StateBadge, TypeIcon } from '../../shared/badges';
import { ago, Clock, count, duration, today } from '../../shared/time';
import {
  ageLabel,
  leadTime,
  partialWeek,
  periodOf,
  projectCards,
  share,
  shortDay,
  shortKey,
  stateTotals,
} from './dashboard-model';

/**
 * The tenant's front page, its dashboard (docs/adr/0018 D6): the nine tiles over what the person
 * can see — the server counts under the visibility predicate —, filtered by project and period, both
 * held in the page's address (`?project=`, repeatable as the ticket lists take it, `?from=`,
 * `?to=`); beside them each project's open tickets by state and the open tickets updated last. The
 * charts are bars drawn with the preset's tokens, so they follow the scheme without a library. The
 * dashboard loads again when an event may have changed it ({@link DashboardService}). A global
 * administrator without a role in the tenant sees its name only: its work is its members'
 * (docs/adr/0034 D2).
 */
@Component({
  selector: 'app-tenant-dashboard',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    FormsModule,
    InputText,
    RouterLink,
    SecurityBadge,
    Select,
    SeverityBadge,
    Skeleton,
    StateBadge,
    TypeIcon,
  ],
  templateUrl: './dashboard.html',
  styleUrl: './dashboard.scss',
})
export class TenantDashboard {
  protected readonly session = inject(SessionService);
  protected readonly projects = inject(ProjectsService);
  private readonly dashboards = inject(DashboardService);
  private readonly problems = inject(ProblemService);
  private readonly clock = inject(Clock);
  private readonly router = inject(Router);

  /** `?project=` as the ticket lists take it: repeatable, a `!` leaves a project out (docs/adr/0049). */
  readonly project = input<string | string[]>();
  /** `?from=` and `?to=`, the period's first and last day; the thirty days to today without them. */
  readonly from = input<string>();
  readonly to = input<string>();

  protected readonly filter = computed(() => {
    const value = this.project();
    return value === undefined ? [] : Array.isArray(value) ? value : [value];
  });
  /** The projects the select shows as chosen: the plain values of the filter. */
  protected readonly chosen = computed(() =>
    this.filter().filter((value) => !value.startsWith('!')),
  );
  /** Today in the browser's calendar, moving on at midnight. */
  private readonly today = computed(() => today(new Date(this.clock.now())));
  protected readonly period = computed(() =>
    periodOf(this.from() ?? null, this.to() ?? null, this.today()),
  );
  /** A period that ends before it begins is not asked for: the page says so instead. */
  protected readonly backwards = computed(() => this.period().from > this.period().to);
  protected readonly filtered = computed(
    () => this.filter().length > 0 || !!this.from() || !!this.to(),
  );
  protected readonly projectOptions = computed(() =>
    this.projects.list().map((project) => ({
      label: `${project.key} · ${project.name}`,
      value: project.key,
    })),
  );

  protected readonly dashboard = this.dashboards.dashboard(() => {
    const tenant = this.session.workTenant();
    if (!tenant || this.backwards()) {
      return undefined;
    }
    const { from, to } = this.period();
    return { tenant, project: this.filter(), from, to };
  });

  protected readonly tiles = computed<Dashboard | undefined>(() =>
    this.dashboard.hasValue() ? this.dashboard.value() : undefined,
  );
  protected readonly failure = computed(() => {
    const error = this.dashboard.error();
    return error ? this.problems.read(error) : undefined;
  });

  protected readonly states = computed(() => stateTotals(this.tiles()?.open_by_state ?? []));
  protected readonly openTotal = computed(() =>
    this.states().reduce((sum, part) => sum + part.count, 0),
  );
  protected readonly cards = computed(() =>
    projectCards(this.projects.list(), this.tiles()?.open_by_state ?? [], this.filter()),
  );
  protected readonly headline = computed(
    () =>
      `${count(this.openTotal(), 'open ticket')} across ${count(this.cards().length, 'project')}`,
  );
  protected readonly severityMax = computed(() =>
    Math.max(0, ...(this.tiles()?.open_by_severity ?? []).map((part) => part.count)),
  );
  protected readonly ageMax = computed(() =>
    Math.max(0, ...(this.tiles()?.age ?? []).map((bucket) => bucket.count)),
  );
  protected readonly weekMax = computed(() =>
    Math.max(0, ...(this.tiles()?.throughput ?? []).map((week) => week.done)),
  );
  protected readonly timeMax = computed(() =>
    Math.max(0, ...(this.tiles()?.time.projects ?? []).map((part) => part.minutes)),
  );

  protected readonly share = share;
  protected readonly ageLabel = ageLabel;
  protected readonly leadTime = leadTime;
  protected readonly partialWeek = partialWeek;
  protected readonly shortDay = shortDay;
  protected readonly shortKey = shortKey;
  protected readonly duration = duration;

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  /** The projects chosen in the select; the values the filter leaves out stay. */
  protected chooseProjects(keys: string[] | null): void {
    const without = this.filter().filter((value) => value.startsWith('!'));
    this.show({ project: [...(keys ?? []), ...without] });
  }

  protected choosePeriod(edge: 'from' | 'to', day: string | null): void {
    this.show(edge === 'from' ? { from: day || undefined } : { to: day || undefined });
  }

  protected clear(): void {
    void this.router.navigate(['/t', this.session.tenant()]);
  }

  /** The page's address with the filters changed; the others stay as they are. */
  private show(change: { project?: string[]; from?: string; to?: string }): void {
    const next = { project: this.filter(), from: this.from(), to: this.to(), ...change };
    void this.router.navigate(['/t', this.session.tenant()], {
      queryParams: {
        project: next.project && next.project.length > 0 ? next.project : undefined,
        from: next.from || undefined,
        to: next.to || undefined,
      },
    });
  }
}
