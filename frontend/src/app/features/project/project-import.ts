import {
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  effect,
  ElementRef,
  inject,
  Injector,
  input,
  linkedSignal,
  resource,
  ResourceRef,
  signal,
  untracked,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { ConfirmationService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Checkbox } from 'primeng/checkbox';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { SelectButton } from 'primeng/selectbutton';
import { Skeleton } from 'primeng/skeleton';
import { Tooltip } from 'primeng/tooltip';
import {
  BlockKind,
  ImportCorrection,
  ImportFile,
  ImportJob,
  ImportOutcome,
  TicketState,
  TicketType,
} from '../../api/models';
import { ImportsService } from '../../core/imports.service';
import { MembersService } from '../../core/members.service';
import { ProblemService, ProblemView } from '../../core/problem.service';
import { refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { StateBadge, TypeIcon } from '../../shared/badges';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { refocus } from '../../shared/refocus';
import { ago, Clock, count, dateTime, size } from '../../shared/time';
import { meanings } from '../../shared/vocabulary';
import {
  assigneeOf,
  blocking,
  blockLacks,
  blockReasonLimit,
  blockableStates,
  changeable,
  correctable,
  correctionBlockKinds,
  correctionsOf,
  defaultBlockFrom,
  Draft,
  importing,
  linkText,
  outcomeMeanings,
  placeOf,
  refusedFiles,
  shortKey,
  stateOf,
} from './import-model';

/** One line under a file of the report: what the import read, decided or refuses there. */
interface Note {
  tone: 'error' | 'warn' | 'info';
  /** What the line is about: a part of the file, `line 12 · severity`, or the execution. */
  place: string;
  text: string;
}

/** A file of the report as the table shows it. */
interface Row {
  file: ImportFile;
  draft: Draft | undefined;
  notes: Note[];
  /** The person may correct it beyond its exclusion. */
  changeable: boolean;
  /** The block the person gives a file they set `blocked`, while the state is `blocked`. */
  editsBlock: boolean;
  /** The block kind the source's `blocked-by` names beside another state, offered to confirm. */
  candidate: BlockKind | null;
  /** What the block of the file still lacks. */
  lacks: string | null;
}

/** An execution's refusal, as the page shows it above the report. */
interface Refusal {
  title: string;
  detail: string;
  /** The messages per file, by path. */
  files: Map<string, string[]>;
}

const types: TicketType[] = ['task', 'bug', 'feature', 'decision', 'question'];
const states: TicketState[] = [
  'filed',
  'analysed',
  'decided',
  'in-progress',
  'review',
  'blocked',
  'done',
  'dropped',
];
const closedStates: ReadonlySet<string> = new Set(['done', 'dropped']);

/** The accent of each outcome, a token of the preset (docs/adr/0052 D5). */
const outcomeAccents: Record<ImportOutcome, string> = {
  create: 'var(--p-primary-color)',
  created: 'var(--p-state-done)',
  conflict: 'var(--p-severity-high)',
  error: 'var(--p-severity-critical)',
  skip: 'var(--p-text-muted-color)',
  exclude: 'var(--p-text-muted-color)',
};

/** The lines under a file: its refusal by the execution, its errors and conflict, its warnings, then what the import read. */
export function notesOf(file: ImportFile, refused: readonly string[] = []): Note[] {
  const notes: Note[] = refused.map((text) => ({ tone: 'error', place: 'execution', text }));
  for (const error of file.errors) {
    notes.push({ tone: 'error', place: placeOf(error), text: error.message });
  }
  if (file.conflict) {
    notes.push({
      tone: 'error',
      place: 'number',
      text: `${shortKey(file.conflict)} holds this number in the project, or held it until it was purged`,
    });
  }
  for (const warning of file.warnings) {
    notes.push({ tone: 'warn', place: placeOf(warning), text: warning.message });
  }
  if (file.block) {
    const waits = file.block.ticket ? `, waiting on ${shortKey(file.block.ticket)}` : '';
    notes.push({
      tone: 'info',
      place: 'block',
      text: `${file.block.kind}: ${file.block.reason} (from ${file.block.from}${waits})`,
    });
  }
  if (file.confidential_reason) {
    notes.push({
      tone: 'info',
      place: file.confidential ? 'confidential' : 'not confidential',
      text: file.confidential_reason,
    });
  }
  if (file.note) {
    notes.push({ tone: 'info', place: file.state === 'dropped' ? 'reason' : 'note', text: file.note });
  }
  if (file.parent) {
    notes.push({ tone: 'info', place: 'parent', text: shortKey(file.parent) });
  }
  if (file.links.length > 0) {
    notes.push({ tone: 'info', place: 'links', text: file.links.map(linkText).join(', ') });
  }
  if (file.reason) {
    notes.push({ tone: 'info', place: file.outcome, text: file.reason });
  }
  return notes;
}

/**
 * The import into a project (docs/adr/0051, docs/adr/0063), a tenant administrator's act: at
 * `…/imports` the files are chosen and the dry run is made; at `…/imports/<id>` — the job's own
 * address, which a reload keeps for as long as the dry run lives — its report is read: the summary,
 * what blocks the execution, and every file with its outcome, its title, type and state, its
 * confidential flag, its links, questions, warnings and errors. Each ticket file takes a correction
 * — left out, or another type, state, block or assignee (docs/adr/0063 D1) —, and the execution,
 * after a question that says it cannot be undone as a whole, sends them; its refusals land on the
 * files they name. An executed job shows what it created and leads to the backlog.
 */
@Component({
  selector: 'app-project-import',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    Checkbox,
    ConfirmDialog,
    FormsModule,
    InputText,
    RouterLink,
    Select,
    SelectButton,
    Skeleton,
    StateBadge,
    Tooltip,
    TypeIcon,
  ],
  providers: [ConfirmationService],
  templateUrl: './project-import.html',
  styleUrl: './project-import.scss',
})
export class ProjectImport {
  /** The project's key. */
  readonly project = input.required<string>();
  /** The job's id, the last segment of its address; none on the page that makes a dry run. */
  readonly job = input<string>();

  protected readonly session = inject(SessionService);
  protected readonly tenantInfo = inject(TenantService);
  private readonly imports = inject(ImportsService);
  private readonly members = inject(MembersService);
  private readonly problems = inject(ProblemService);
  private readonly confirm = inject(ConfirmationService);
  private readonly router = inject(Router);
  private readonly injector = inject(Injector);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly clock = inject(Clock);

  protected readonly types = types;
  protected readonly states = states;
  protected readonly blockKinds = correctionBlockKinds;
  protected readonly blockFroms = blockableStates;
  protected readonly blockReasonLimit = blockReasonLimit;
  protected readonly outcomeMeanings = outcomeMeanings;
  protected readonly outcomeAccents = outcomeAccents;
  protected readonly meanings = meanings;
  protected readonly count = count;
  protected readonly dateTime = dateTime;
  protected readonly shortKey = shortKey;
  protected readonly size = size;

  private destroyed = false;

  // The upload.
  protected readonly chosen = signal<readonly File[]>([]);
  protected readonly dragging = signal(false);
  protected readonly uploading = signal(false);
  protected readonly uploadRefusal = signal<ProblemView | null>(null);
  protected readonly chosenBytes = computed(() =>
    this.chosen().reduce((sum, file) => sum + file.size, 0),
  );

  // The job.
  /** `<tenant>/<project>/<id>` while an administrator's page shows a job: a primitive, see frontend.md. */
  private readonly address = computed(() => {
    const tenant = this.session.tenant();
    const id = this.job();
    return tenant && id && this.tenantInfo.isAdmin() ? `${tenant}/${this.project()}/${id}` : undefined;
  });
  protected readonly loaded: ResourceRef<ImportJob | undefined> = resource({
    params: () => this.address(),
    loader: ({ params }) => {
      const [tenant, project, id] = params.split('/');
      return this.imports.job(tenant, project, id);
    },
  });
  protected readonly report = computed(() =>
    this.loaded.hasValue() ? this.loaded.value() : undefined,
  );
  /** The execution answered `404`: the dry run expired meanwhile. */
  private readonly expired = linkedSignal({ source: this.address, computation: () => false });
  protected readonly gone = computed(() => {
    const error = this.loaded.error();
    return this.expired() || (error !== undefined && this.problems.read(error).status === 404);
  });
  protected readonly loadFailure = computed(() => {
    const error = this.loaded.error();
    const problem = error ? this.problems.read(error) : undefined;
    return problem && problem.status !== 404 ? problem : undefined;
  });
  /** The dry run's day is over by the browser's clock: its execution would be refused. */
  protected readonly lapsed = computed(() => {
    const expires = this.report()?.expires_at;
    return !!expires && this.report()?.status === 'dry_run' && Date.parse(expires) <= this.clock.now();
  });

  /** The person's corrections, by path; another job starts without any. */
  protected readonly drafts = linkedSignal<string | undefined, ReadonlyMap<string, Draft>>({
    source: this.address,
    computation: () => new Map(),
  });
  protected readonly filter = linkedSignal<string | undefined, ImportOutcome | 'all'>({
    source: this.address,
    computation: () => 'all',
  });
  protected readonly executing = signal(false);
  protected readonly refusal = linkedSignal<string | undefined, Refusal | null>({
    source: this.address,
    computation: () => null,
  });
  protected readonly refusedFiles = computed(() =>
    [...(this.refusal()?.files ?? new Map<string, string[]>())].map(([path, messages]) => ({
      path,
      messages,
    })),
  );
  /** The execution found the dry run executed already, by another tab or person. */
  protected readonly executedElsewhere = linkedSignal({
    source: this.address,
    computation: () => false,
  });

  protected readonly dryRun = computed(() => this.report()?.status === 'dry_run');
  protected readonly files = computed(() => this.report()?.files ?? []);
  protected readonly people = computed(() =>
    this.members.list().map((member) => ({ id: member.person.id, name: member.person.display_name })),
  );
  protected readonly blockingFiles = computed(() =>
    this.dryRun() ? blocking(this.files(), this.drafts()) : [],
  );
  protected readonly importingFiles = computed(() =>
    this.dryRun() ? importing(this.files(), this.drafts()) : [],
  );
  protected readonly excludedCount = computed(
    () => this.files().filter((file) => this.drafts().get(file.path)?.exclude).length,
  );
  /** The first file whose block is not complete, with what it lacks. */
  private readonly unfinished = computed(() =>
    this.files().find((file) => blockLacks(file, this.drafts().get(file.path)) !== null),
  );
  protected readonly ready = computed(
    () =>
      this.dryRun() &&
      !this.lapsed() &&
      !this.executing() &&
      this.blockingFiles().length === 0 &&
      this.unfinished() === undefined &&
      this.importingFiles().length > 0,
  );
  /** What the execution would do, or why it cannot, beside its button. */
  protected readonly executeText = computed(() => {
    const blocked = this.blockingFiles().length;
    if (blocked > 0) {
      return `${count(blocked, 'file')} ${blocked === 1 ? 'blocks' : 'block'} the execution.`;
    }
    const unfinished = this.unfinished();
    if (unfinished) {
      return `${unfinished.path}: ${blockLacks(unfinished, this.drafts().get(unfinished.path))}`;
    }
    const files = this.importingFiles().length;
    if (files === 0) {
      return 'Nothing to import: every ticket file is left out.';
    }
    const excluded = this.excludedCount();
    return (
      `${files} of ${count(this.files().length, 'file')} to import` +
      (excluded > 0 ? `, ${excluded} left out.` : '.')
    );
  });

  protected readonly filters = computed(() => {
    const files = this.files();
    const outcomes = [...new Set(files.map((file) => file.outcome))];
    return [
      { label: `All ${files.length}`, value: 'all' },
      ...outcomes.map((outcome) => ({
        label: `${outcome} ${files.filter((file) => file.outcome === outcome).length}`,
        value: outcome,
      })),
    ];
  });

  protected readonly rows = computed<Row[]>(() => {
    const drafts = this.drafts();
    const refused = this.refusal()?.files;
    const dryRun = this.dryRun();
    const shown = this.filter();
    return this.files()
      .filter((file) => shown === 'all' || file.outcome === shown)
      .map((file) => {
        const draft = drafts.get(file.path);
        const editable = dryRun && changeable(file) && !draft?.exclude;
        const state = stateOf(file, draft);
        const candidate =
          file.block_candidate && correctionBlockKinds.includes(file.block_candidate)
            ? file.block_candidate
            : null;
        return {
          file,
          draft,
          notes: notesOf(file, refused?.get(file.path)),
          changeable: editable,
          editsBlock:
            editable && state === 'blocked' && (file.state !== 'blocked' || file.block === null),
          candidate: editable && state !== 'blocked' ? candidate : null,
          lacks: dryRun ? blockLacks(file, draft) : null,
        };
      });
  });

  /** The summary's tiles: the files, then the count of each outcome the job's status knows. */
  protected readonly tiles = computed(() => {
    const job = this.report();
    if (!job) {
      return [];
    }
    const summary = job.summary;
    const outcomes: ImportOutcome[] =
      job.status === 'dry_run'
        ? ['create', 'conflict', 'error', 'skip']
        : ['created', 'exclude', 'skip'];
    return outcomes.map((outcome) => ({ outcome, count: summary[outcome] }));
  });
  protected readonly totals = computed(() => {
    const summary = this.report()?.summary;
    if (!summary) {
      return '';
    }
    const which = this.dryRun() ? 'Of the tickets to create' : 'Of the tickets created';
    const highest =
      summary.highest_number !== null
        ? ` The highest number is ${summary.highest_number}; ${this.project()} numbers its next ticket after it.`
        : '';
    return `${which}, ${summary.open} open and ${summary.confidential} confidential.${highest}`;
  });

  constructor() {
    inject(DestroyRef).onDestroy(() => (this.destroyed = true));
    // A question belongs to the job it was asked about: another tenant, project or job closes it.
    effect(() => {
      this.address();
      untracked(() => this.confirm.close());
    });
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  // The upload.

  protected over(event: DragEvent): void {
    event.preventDefault();
    this.dragging.set(true);
  }

  protected leave(event: DragEvent): void {
    const zone = event.currentTarget as HTMLElement | null;
    if (!zone?.contains(event.relatedTarget as Node | null)) {
      this.dragging.set(false);
    }
  }

  protected drop(event: DragEvent): void {
    event.preventDefault();
    this.dragging.set(false);
    this.add([...(event.dataTransfer?.files ?? [])]);
  }

  protected pick(picker: HTMLInputElement): void {
    this.add([...(picker.files ?? [])]);
    picker.value = '';
  }

  /** Adds files to the upload; one of a name already chosen takes its place, as a path is sent once. */
  private add(files: File[]): void {
    if (files.length === 0) {
      return;
    }
    this.uploadRefusal.set(null);
    this.chosen.update((chosen) => {
      const names = new Set(files.map((file) => file.name));
      return [...chosen.filter((file) => !names.has(file.name)), ...files];
    });
  }

  protected remove(file: File): void {
    this.chosen.update((chosen) => chosen.filter((each) => each !== file));
  }

  /** Makes the dry run and opens its report at the job's own address. */
  protected async startDryRun(): Promise<void> {
    const tenant = this.session.tenant();
    const project = this.project();
    const files = this.chosen();
    if (!tenant || files.length === 0 || this.uploading()) {
      return;
    }
    this.uploading.set(true);
    this.uploadRefusal.set(null);
    try {
      const job = await this.imports.dryRun(tenant, project, files);
      if (!this.destroyed && this.session.tenant() === tenant && this.project() === project) {
        await this.router.navigate(['/t', tenant, 'p', project, 'imports', job.id]);
      }
    } catch (error) {
      this.uploadRefusal.set(this.problems.read(error));
    } finally {
      this.uploading.set(false);
    }
  }

  // The corrections.

  private change(file: ImportFile, change: (draft: Draft) => Draft): void {
    this.drafts.update((drafts) => new Map(drafts).set(file.path, change(drafts.get(file.path) ?? {})));
  }

  protected exclude(file: ImportFile, exclude: boolean): void {
    this.change(file, (draft) => ({ ...draft, exclude }));
  }

  /** Leaves out every file named, the ticket files among them. */
  protected excludeAll(paths: Iterable<string>): void {
    const named = new Set(paths);
    this.drafts.update((drafts) => {
      const next = new Map(drafts);
      for (const file of this.files()) {
        if (named.has(file.path) && correctable(file)) {
          next.set(file.path, { ...next.get(file.path), exclude: true });
        }
      }
      return next;
    });
  }

  protected setType(file: ImportFile, type: TicketType): void {
    this.change(file, (draft) => ({ ...draft, type }));
  }

  /**
   * Sets the state. `blocked` takes a block, which the person gives — the kind the source names
   * offered —, unless the file keeps the one its source gives it; any other state drops it.
   */
  protected setState(file: ImportFile, state: TicketState): void {
    this.change(file, (draft) => {
      const keepsItsOwn = file.state === 'blocked' && file.block !== null;
      const candidate =
        file.block_candidate && correctionBlockKinds.includes(file.block_candidate)
          ? file.block_candidate
          : null;
      return {
        ...draft,
        state,
        block:
          state === 'blocked' && !keepsItsOwn
            ? (draft.block ?? { kind: candidate, reason: '', from: defaultBlockFrom(file) })
            : undefined,
      };
    });
  }

  protected setBlock(file: ImportFile, change: Partial<Draft['block'] & object>): void {
    this.change(file, (draft) => ({
      ...draft,
      block: {
        ...(draft.block ?? { kind: null, reason: '', from: defaultBlockFrom(file) }),
        ...change,
      },
    }));
  }

  protected setAssignee(file: ImportFile, assignee: string | null): void {
    this.change(file, (draft) => ({ ...draft, assignee: assignee ?? null }));
  }

  protected assigneeOf(file: ImportFile, draft: Draft | undefined): string | null {
    return assigneeOf(file, draft);
  }

  protected stateOf(file: ImportFile, draft: Draft | undefined): TicketState | null {
    return stateOf(file, draft);
  }

  // The execution.

  /** Asks first: an import creates every ticket at once and is undone only ticket by ticket. */
  protected askToExecute(): void {
    const job = this.report();
    const tenant = this.session.tenant();
    const project = this.project();
    if (!job || !tenant || !this.ready()) {
      return;
    }
    const drafts = this.drafts();
    const files = this.importingFiles();
    const open = files.filter(
      (file) => !closedStates.has(stateOf(file, drafts.get(file.path)) ?? ''),
    ).length;
    const confidential = files.filter((file) => file.confidential).length;
    const tickets = count(files.length, 'ticket');
    const corrections = correctionsOf(this.files(), drafts);
    this.confirm.confirm({
      header: `Import ${tickets} into ${project}?`,
      message:
        `${tickets} — ${open} open, ${confidential} confidential — ${files.length === 1 ? 'is' : 'are'} ` +
        'created at once, with their questions and the links among them, and you are their ' +
        'reporter. An import cannot be undone as a whole: a ticket it created goes only by deleting ' +
        'it on its own page, one at a time.',
      acceptLabel: `Import ${tickets}`,
      rejectLabel: 'Not yet',
      defaultFocus: 'reject',
      accept: () => void this.execute(job, tenant, project, corrections),
    });
  }

  private async execute(
    job: ImportJob,
    tenant: string,
    project: string,
    corrections: ImportCorrection[],
  ): Promise<void> {
    const at = `${tenant}/${project}/${job.id}`;
    this.executing.set(true);
    this.refusal.set(null);
    try {
      const executed = await this.imports.execute(tenant, project, job.id, corrections);
      if (this.address() === at) {
        this.loaded.set(executed);
        refocus(this.host.nativeElement, this.injector, '#import-done-heading');
      }
    } catch (error) {
      if (this.address() === at) {
        this.refused(error, corrections);
      } else {
        this.problems.report(error);
      }
    } finally {
      this.executing.set(false);
    }
  }

  /** Where an execution's refusal lands: on the files it names, on the page, or in a toast. */
  private refused(error: unknown, sent: ImportCorrection[]): void {
    const problem = this.problems.read(error);
    if (problem.status === 404) {
      this.expired.set(true);
      return;
    }
    if (problem.code === 'import_executed') {
      this.executedElsewhere.set(true);
      refresh(this.loaded, this.injector);
      return;
    }
    const files = refusedFiles(this.problems.entries(error), sent);
    if (problem.code === 'import_conflict' || (problem.status === 400 && files.size > 0)) {
      this.refusal.set({ title: problem.title, detail: problem.detail, files });
      refocus(this.host.nativeElement, this.injector, '#import-refusal-heading');
      return;
    }
    this.problems.report(error);
  }

  protected retry(): void {
    refresh(this.loaded, this.injector);
  }
}
