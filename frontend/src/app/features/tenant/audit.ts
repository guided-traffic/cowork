import { DOCUMENT } from '@angular/common';
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
import { RouterLink } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { Paginator } from 'primeng/paginator';
import { Select } from 'primeng/select';
import { Skeleton } from 'primeng/skeleton';
import { Tooltip } from 'primeng/tooltip';
import type { PaginatorState } from 'primeng/types/paginator';
import { AuditAction, AuditEvent, AuditList, TokenMark } from '../../api/models';
import { AUDIT_ACTION } from '../../api/models/audit-action-array';
import { AuditQuery, AuditService, csvRowLimit, PerPage } from '../../core/audit.service';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { AgentMark } from '../../shared/agent-mark';
import { dateTime } from '../../shared/time';

/** The entities the record names most, offered beside the field, which takes any other too. */
export const entityTypes = [
  'ticket',
  'comment',
  'question',
  'link',
  'interest',
  'attachment',
  'time_entry',
  'project',
  'repository',
  'project_access',
  'membership',
  'group_mapping',
  'tenant',
  'token',
  'user',
];

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Whether the token field holds a token's id, as an act shows it; empty is no filter. */
export function tokenIdOrEmpty(text: string): boolean {
  const trimmed = text.trim();
  return trimmed === '' || uuidPattern.test(trimmed);
}

/**
 * The period of two days of the browser's calendar as the API's instants (docs/adr/0055 D3): from
 * the start of the first day, up to the start of the day after the last, so that the last day
 * counts whole. An empty day is no bound.
 */
export function period(fromDay: string, untilDay: string): { from?: string; to?: string } {
  const start = (day: string) => new Date(`${day}T00:00:00`);
  const out: { from?: string; to?: string } = {};
  if (fromDay) {
    out.from = start(fromDay).toISOString();
  }
  if (untilDay) {
    const next = start(untilDay);
    next.setDate(next.getDate() + 1);
    out.to = next.toISOString();
  }
  return out;
}

/** The fields an act changed, each as `field: before → after`, the values as JSON. */
export function changes(event: AuditEvent): string[] {
  const before = (event.before ?? {}) as Record<string, unknown>;
  const after = (event.after ?? {}) as Record<string, unknown>;
  if (typeof before !== 'object' || typeof after !== 'object') {
    return [];
  }
  const fields = [...new Set([...Object.keys(before), ...Object.keys(after)])];
  const shown = (value: unknown) => (value === undefined ? '—' : JSON.stringify(value));
  return fields.map((field) =>
    field in before
      ? `${field}: ${shown(before[field])} → ${shown(after[field])}`
      : `${field}: ${shown(after[field])}`,
  );
}

/** `acme/VKO-12` → `VKO-12`, the key a ticket's page is addressed by. */
function shortKey(key: string): string {
  return key.slice(key.indexOf('/') + 1);
}

/**
 * The tenant's audit record, for its administrators (docs/adr/0026 D6): every act, newest first —
 * who made it, a person or a system actor, the agent and the token it came through
 * (docs/adr/0036 D6), what it did to which entity, and the fields it changed. Filtered by actor,
 * token, action, entity and period, read in numbered pages with a total (docs/adr/0048 D2), and
 * downloaded as CSV. A change of a filter goes back to the first page. The record shows what the
 * server answers; a member who is no administrator is told so and asks nothing.
 */
@Component({
  selector: 'app-audit',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    AgentMark,
    ButtonDirective,
    FormsModule,
    InputText,
    Paginator,
    RouterLink,
    Select,
    Skeleton,
    Tooltip,
  ],
  templateUrl: './audit.html',
  styleUrl: './audit.scss',
})
export class Audit {
  private readonly audit = inject(AuditService);
  private readonly members = inject(MembersService);
  private readonly problems = inject(ProblemService);
  private readonly document = inject(DOCUMENT);
  protected readonly session = inject(SessionService);
  protected readonly tenantInfo = inject(TenantService);

  protected readonly actions = AUDIT_ACTION;
  protected readonly entityTypes = entityTypes;
  protected readonly perPageOptions: PerPage[] = [25, 50, 100];
  protected readonly dateTime = dateTime;
  protected readonly changes = changes;
  protected readonly shortKey = shortKey;

  protected readonly actor = signal<string | null>(null);
  protected readonly token = signal('');
  protected readonly action = signal<AuditAction[]>([]);
  protected readonly entity = signal('');
  protected readonly fromDay = signal('');
  protected readonly untilDay = signal('');

  protected readonly tokenInvalid = computed(() => !tokenIdOrEmpty(this.token()));
  protected readonly people = computed(() =>
    this.members.list().map((member) => ({
      id: member.person.id,
      label: member.person.username
        ? `${member.person.display_name} (${member.person.username})`
        : member.person.display_name,
    })),
  );

  /** The filters as the API takes them; undefined while the page asks for nothing. */
  protected readonly query = computed<AuditQuery | undefined>(
    () => {
      const tenant = this.session.tenant();
      if (!tenant || !this.tenantInfo.isAdmin() || this.tokenInvalid()) {
        return undefined;
      }
      const token = this.token().trim();
      const entity = this.entity().trim();
      return {
        tenant,
        ...(this.actor() ? { actor: this.actor() as string } : {}),
        ...(token ? { token: token.toLowerCase() } : {}),
        ...(this.action().length > 0 ? { action: this.action() } : {}),
        ...(entity ? { entity_type: entity } : {}),
        ...period(this.fromDay(), this.untilDay()),
      };
    },
    { equal: (a, b) => JSON.stringify(a) === JSON.stringify(b) },
  );

  protected readonly perPage = signal<PerPage>(25);
  /** The page shown; another filter, or another page size, starts at the first again. */
  protected readonly page = linkedSignal<{ query?: AuditQuery; perPage: PerPage }, number>({
    source: () => ({ query: this.query(), perPage: this.perPage() }),
    computation: () => 1,
  });

  protected readonly record = resource({
    params: () => {
      const query = this.query();
      return query ? { query, page: this.page(), perPage: this.perPage() } : undefined;
    },
    loader: ({ params }) => this.audit.page(params.query, params.page, params.perPage),
  });

  protected readonly rows = computed<AuditEvent[]>(() =>
    this.record.hasValue() ? this.record.value().items : [],
  );
  /**
   * The total of the last answer, kept while the next page loads, so that the pages — and the
   * button of the page that was asked for, which may hold the focus — stay where they are.
   */
  protected readonly total = linkedSignal<AuditList | undefined, number>({
    source: () => (this.record.hasValue() ? this.record.value() : undefined),
    computation: (list, previous) =>
      list ? (list.total ?? 0) : this.record.isLoading() ? (previous?.value ?? 0) : 0,
  });
  protected readonly failure = computed(() => {
    const error = this.record.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The record could not be loaded: ${problem.detail || problem.title}`;
  });

  protected readonly downloading = signal(false);
  /** What the last download said beyond the file: that it was cut at the depth of the pages. */
  protected readonly downloadNote = signal<string | null>(null);

  protected readonly filtered = computed(
    () =>
      this.actor() !== null ||
      this.token() !== '' ||
      this.action().length > 0 ||
      this.entity() !== '' ||
      this.fromDay() !== '' ||
      this.untilDay() !== '',
  );

  /** The token an act came through, as the mark shows it; none for a session's act. */
  protected tokenOf(event: AuditEvent): TokenMark | null {
    return event.token_id ? { id: event.token_id, name: event.token_name ?? null } : null;
  }

  /** Shows what a token did: its id becomes the token filter. */
  protected byToken(id: string): void {
    this.token.set(id);
  }

  protected turn(state: PaginatorState): void {
    const rows = (state.rows ?? this.perPage()) as PerPage;
    if (rows !== this.perPage()) {
      this.perPage.set(rows);
      return;
    }
    this.page.set((state.page ?? 0) + 1);
  }

  protected clear(): void {
    this.actor.set(null);
    this.token.set('');
    this.action.set([]);
    this.entity.set('');
    this.fromDay.set('');
    this.untilDay.set('');
  }

  /**
   * Downloads the CSV of what the filters select (docs/adr/0026 D6), up to the moment the download
   * began and at most the newest `csvRowLimit` rows, which the page says when it cut.
   */
  protected async download(): Promise<void> {
    const query = this.query();
    if (!query || this.downloading()) {
      return;
    }
    this.downloading.set(true);
    this.downloadNote.set(null);
    try {
      const csv = await this.audit.csv(query);
      const url = URL.createObjectURL(new Blob([csv.text], { type: 'text/csv' }));
      const link = this.document.createElement('a');
      link.href = url;
      link.download = `audit-${query.tenant}-${csv.until.slice(0, 19).replace(/[:T]/g, '-')}.csv`;
      link.click();
      URL.revokeObjectURL(url);
      if (csv.total > csvRowLimit) {
        this.downloadNote.set(
          `The file holds the newest ${csvRowLimit.toLocaleString()} of ` +
            `${csv.total.toLocaleString()} acts: narrow the period for the rest.`,
        );
      }
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.downloading.set(false);
    }
  }
}
