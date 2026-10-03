import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  linkedSignal,
  resource,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { InputText } from 'primeng/inputtext';
import { SelectButton } from 'primeng/selectbutton';
import { Api } from '../../api/api';
import { timeReport } from '../../api/functions';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { Clock, duration, today } from '../../shared/time';

type GroupBy = 'ticket' | 'project' | 'person' | 'tenant';

/** The first day of the month `day` falls in, as the API's day. */
export function monthStart(day: string): string {
  return `${day.slice(0, 8)}01`;
}

/**
 * The time report (docs/adr/0017 D10, docs/adr/0018): the minutes booked in a period, summed per
 * ticket, project or person, or for the whole tenant, under the visibility of time entries the
 * server applies. Days are the browser's dates; the API takes them as days (docs/adr/0055 D3).
 */
@Component({
  selector: 'app-time-report',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, InputText, SelectButton],
  templateUrl: './time-report.html',
  styleUrl: './time-report.scss',
})
export class TimeReport {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly problems = inject(ProblemService);
  private readonly clock = inject(Clock);

  protected readonly groups: GroupBy[] = ['project', 'ticket', 'person', 'tenant'];
  /** Today, moving on at midnight; a period the person chose holds until then. */
  private readonly currentDay = computed(() => today(new Date(this.clock.now())));
  protected readonly from = linkedSignal(() => monthStart(this.currentDay()));
  protected readonly to = linkedSignal(() => this.currentDay());
  protected readonly groupBy = signal<GroupBy>('project');

  protected readonly report = resource({
    params: () => {
      const tenant = this.session.tenant();
      return tenant
        ? { tenant, from: this.from(), to: this.to(), group_by: this.groupBy() }
        : undefined;
    },
    loader: ({ params }) => this.api.invoke(timeReport, params),
  });

  protected readonly rows = computed(() =>
    this.report.hasValue() ? this.report.value().items : [],
  );
  protected readonly total = computed(() =>
    this.report.hasValue() ? this.report.value().total_minutes : 0,
  );
  protected readonly largest = computed(() =>
    Math.max(1, ...this.rows().map((row) => row.minutes)),
  );
  protected readonly failure = computed(() => {
    const error = this.report.error();
    return error ? this.problems.read(error) : undefined;
  });
  protected readonly duration = duration;

  protected share(minutes: number): string {
    return `${(100 * minutes) / this.largest()}%`;
  }
}
