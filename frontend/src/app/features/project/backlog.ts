import { NgTemplateOutlet } from '@angular/common';
import {
  CdkDrag,
  CdkDragDrop,
  CdkDragHandle,
  CdkDragPreview,
  CdkDropList,
  CdkDropListGroup,
} from '@angular/cdk/drag-drop';
import {
  afterNextRender,
  ChangeDetectionStrategy,
  ChangeDetectorRef,
  Component,
  computed,
  effect,
  ElementRef,
  inject,
  Injector,
  input,
  linkedSignal,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { ConfirmationService, MenuItem, MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { IconField } from 'primeng/iconfield';
import { InputIcon } from 'primeng/inputicon';
import { InputText } from 'primeng/inputtext';
import { Menu } from 'primeng/menu';
import { Select } from 'primeng/select';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { Tooltip } from 'primeng/tooltip';
import { Horizon, SavedFilter, SavedFilterParameters, Ticket, TicketState } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { TicketPage, TicketsService } from '../../core/tickets.service';
import { SecurityBadge, SeverityBadge, StateBadge, TypeIcon } from '../../shared/badges';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { SizeIcon } from '../../shared/size';
import { StageBar } from '../../shared/stage-bar';
import { currentStage, Stage, stagesOf } from '../../shared/stages';
import { ago, Clock, count, dateTime } from '../../shared/time';
import { meanings } from '../../shared/vocabulary';
import {
  alwaysShown,
  arrange,
  Group,
  Move,
  Plan,
  planDrop,
  planGroup,
  planStep,
  position,
  rankBody,
  Row,
  ScoreMark,
  scoreMarks,
  unanswered,
  horizons,
  withMoves,
} from './backlog-model';
import { ProjectHeader } from './project-header';
import { backlogLeftOut, fromBacklog, listParameters, toBacklog } from './saved-filter-model';
import { SavedFilters } from './saved-filters';

/** The states of the open tickets, in the order of the vocabulary: the filter is for the groups. */
const openStates = (Object.keys(meanings.state) as TicketState[]).filter(
  (state) => state !== 'done' && state !== 'dropped',
);

/** A time the address gives, as the list's filter takes it, or nothing where it is no time. */
function timeOf(value: string | undefined): string | undefined {
  const time = value ? Date.parse(value) : Number.NaN;
  return Number.isNaN(time) ? undefined : new Date(time).toISOString();
}

/**
 * A project's backlog (docs/adr/0018 D1): its open tickets in one table, grouped by horizon in the
 * order now, release, next, later, icebox, each group in the project's rank (docs/adr/0014). A
 * row is dragged by its handle within its group, which moves the rank, and into another group,
 * which sets the horizon and then places the row there; a menu on every row does the same for
 * those who cannot drag. The list loads with the cursor and shows more on request (docs/adr/0048
 * D4); the closed tickets are in no group and load when asked for. The event stream keeps the
 * rows current, and holds back while a row is dragged, so that it is never taken from under the
 * pointer; a move shows at once and stays until an answer of the list shows it.
 */
@Component({
  selector: 'app-backlog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    CdkDrag,
    CdkDragHandle,
    CdkDragPreview,
    CdkDropList,
    CdkDropListGroup,
    ConfirmDialog,
    FormsModule,
    IconField,
    InputIcon,
    InputText,
    Menu,
    NgTemplateOutlet,
    ProjectHeader,
    RouterLink,
    SavedFilters,
    SecurityBadge,
    Select,
    SeverityBadge,
    SizeIcon,
    StageBar,
    StateBadge,
    ToggleSwitch,
    Tooltip,
    TypeIcon,
  ],
  templateUrl: './backlog.html',
  styleUrl: './backlog.scss',
  providers: [ConfirmationService],
})
export class Backlog {
  readonly project = input.required<string>();
  /** `?closed=true` opens the page with the closed tickets shown, as the board's count links it. */
  readonly closed = input<string>();
  /**
   * `?done_after=` with a time narrows the closed tickets to those done after it: the board's count
   * of the tickets done in the last fourteen days leads to them (docs/adr/0018 D1). Named as the
   * filter of the list it is handed on to.
   */
  readonly done_after = input<string>();

  protected readonly session = inject(SessionService);
  private readonly tickets = inject(TicketsService);
  private readonly actions = inject(TicketActions);
  private readonly router = inject(Router);
  private readonly clock = inject(Clock);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly confirm = inject(ConfirmationService);
  private readonly injector = inject(Injector);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly view = inject(ChangeDetectorRef);

  protected readonly meanings = meanings;
  protected readonly stateOptions = openStates.map((state) => ({
    state,
    meaning: meanings.state[state],
  }));
  protected readonly selectedStates = signal<TicketState[]>([]);
  protected readonly query = signal('');
  private readonly debouncedQuery = signal('');
  /**
   * The saved filter applied, and its conditions beyond the search and the states, which the lists
   * take as they are (docs/adr/0018 D5). A saved filter is the tenant's: another tenant's page
   * starts without one.
   */
  protected readonly applied = linkedSignal<string | null, SavedFilter | null>({
    source: () => this.session.tenant(),
    computation: () => null,
  });
  private readonly extra = linkedSignal<string | null, Omit<SavedFilterParameters, 'project'>>({
    source: () => this.session.tenant(),
    computation: () => ({}),
  });
  /** What the backlog applies now, as a saved filter keeps it. */
  protected readonly currentFilter = computed(() =>
    fromBacklog(this.query(), this.selectedStates(), this.extra()),
  );
  /** A saved filter's `project` is the tenant's ticket list's: the bar says so under it. */
  protected readonly leftOut = backlogLeftOut;
  protected readonly showClosed = linkedSignal(() => this.closed() === 'true');
  /** The time the closed tickets are narrowed to, from the address, until the person drops it. */
  protected readonly doneAfter = linkedSignal(() => timeOf(this.done_after()));
  /** How many pages of the list are loaded; the router keeps this page alive across projects. */
  private readonly pages = linkedSignal({ source: () => this.project(), computation: () => 1 });
  private readonly closedPages = linkedSignal({
    source: () => this.project(),
    computation: () => 1,
  });

  private readonly openList = this.tickets.projectTicketPages(() => {
    const tenant = this.session.tenant();
    if (!tenant) {
      return undefined;
    }
    const q = this.debouncedQuery().trim();
    return {
      ...listParameters(this.extra(), this.selectedStates()),
      team: tenant,
      project: this.project(),
      pages: this.pages(),
      q: q === '' ? undefined : q,
    };
  });
  /** Done and dropped tickets, or those done after a time: asked for only while the toggle is on. */
  private readonly closedList = this.tickets.projectTicketPages(() => {
    const tenant = this.session.tenant();
    if (!tenant || !this.showClosed()) {
      return undefined;
    }
    const q = this.debouncedQuery().trim();
    const doneAfter = this.doneAfter();
    const extra = { ...this.extra() };
    delete extra.state;
    return {
      ...extra,
      team: tenant,
      project: this.project(),
      pages: this.closedPages(),
      ...(doneAfter
        ? { state: ['done'] as TicketState[], done_after: doneAfter }
        : { state: ['done', 'dropped'] as TicketState[] }),
      q: q === '' ? undefined : q,
    };
  });

  /** What the list of the open tickets answered last for this project, kept while it loads again. */
  private readonly answer = linkedSignal<
    { project: string; page: TicketPage | undefined },
    TicketPage | undefined
  >({
    source: () => ({
      project: this.project(),
      page: this.openList.hasValue() ? this.openList.value() : undefined,
    }),
    computation: (source, previous) =>
      source.page ?? (previous?.source.project === source.project ? previous.value : undefined),
  });
  /**
   * The person's moves in the rank that the list does not show yet. A move shows at once and stays
   * on top of every answer until one has the version its write gave the ticket: an answer that was
   * on its way when the person dropped cannot take the row back, and the answer that follows the
   * write's own event changes nothing.
   */
  private readonly moves = linkedSignal<
    { project: string; answer: TicketPage | undefined },
    Move[]
  >({
    source: () => ({ project: this.project(), answer: this.answer() }),
    computation: (source, previous) =>
      previous?.source.project === source.project
        ? unanswered(previous.value, source.answer?.versions)
        : [],
  });
  /** The keys of the open tickets in the project's rank, as the page shows them. */
  private readonly order = computed(() => withMoves(this.answer()?.keys ?? [], this.moves()));
  private readonly closedKeys = linkedSignal<
    { project: string; keys: string[] | undefined },
    string[]
  >({
    source: () => ({
      project: this.project(),
      keys: this.closedList.hasValue() ? this.closedList.value()?.keys : undefined,
    }),
    computation: (source, previous) =>
      source.keys ?? (previous?.source.project === source.project ? previous.value : []),
  });
  /** The groups tickets that were dragged and are not yet written are shown in. */
  private readonly shownIn = signal<ReadonlyMap<string, Horizon>>(new Map());

  private readonly openTickets = computed(() => this.read(this.order()));
  /** Closed tickets stand in no group, so none is indented and each names its parent. */
  protected readonly closedRows = computed<Row[]>(() =>
    this.read(this.closedKeys()).map((ticket) => ({
      ticket,
      depth: 0,
      under: null,
      elsewhere: ticket.parent,
    })),
  );
  private readonly groups = computed(() =>
    arrange(this.openTickets(), (ticket) => this.shownIn().get(ticket.key) ?? ticket.horizon),
  );
  /** The groups as they were when a row was picked up: the table does not move under the pointer. */
  private readonly held = signal<Group[] | undefined>(undefined);
  protected readonly dragging = computed(() => this.held() !== undefined);
  /** The groups the table shows: now, next and later always, release and icebox while they hold a ticket. */
  protected readonly visibleGroups = computed(() =>
    (this.held() ?? this.groups()).filter(
      (group) => group.rows.length > 0 || alwaysShown.has(group.horizon),
    ),
  );
  /**
   * While a row is dragged, the groups that are empty and not in the table — release and icebox —
   * are offered as drop zones in a bar at the bottom of the window. A group put into the table
   * above the row that was picked up would push it away from the pointer, and the CDK measures its
   * lists as the drag starts, so nothing may be inserted above the rows that move; the bar is out
   * of the flow of the page and in view wherever the page is scrolled to.
   */
  protected readonly zones = computed(
    () =>
      this.held()?.filter((group) => group.rows.length === 0 && !alwaysShown.has(group.horizon)) ??
      [],
  );

  protected readonly loading = computed(() => this.openList.isLoading());
  protected readonly loadingClosed = computed(() => this.closedList.isLoading());
  protected readonly failure = computed(() => {
    const error = this.openList.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The tickets could not be loaded: ${problem.detail || problem.title}`;
  });
  protected readonly more = computed(
    () => this.openList.hasValue() && !!this.openList.value()?.nextCursor,
  );
  protected readonly moreClosed = computed(
    () => this.closedList.hasValue() && !!this.closedList.value()?.nextCursor,
  );
  protected readonly summary = computed(
    () => `${count(this.openTickets().length, 'open ticket')}${this.more() ? ' loaded' : ''}`,
  );
  /**
   * Where the score disagrees with the rank (docs/adr/0014 D3), a marker, not a figure: among the
   * siblings of each group the page shows, over the rows it has loaded.
   */
  protected readonly marks = computed(() => scoreMarks(this.held() ?? this.groups()));
  /** A viewer does not rank; whoever may is offered the sort by the score. */
  protected readonly mayRank = computed(() => this.session.membership()?.role !== 'viewer');
  protected readonly sorting = signal(false);

  /** What a row's menu offers, for the row that opened it. */
  protected readonly menuItems = signal<MenuItem[]>([]);
  private readonly menu = viewChild.required<Menu>('rowMenu');
  /** The ticket asked for the reason of its horizon, and what the person has typed. */
  protected readonly asking = signal<{ key: string; value: Horizon } | null>(null);
  protected readonly reason = signal('');
  protected readonly sendingReason = signal(false);
  /** What a screen reader is told about a move, which no one sees. */
  protected readonly status = signal('');

  /** The writes of the drops, one after the other: a move never meets its predecessor half done. */
  private writes: Promise<void> = Promise.resolve();

  constructor() {
    // A text that is what the list already asks for has nothing to wait for — also the first run,
    // which would otherwise send the list back to its first page.
    effect((onCleanup) => {
      const query = this.query();
      if (query === untracked(this.debouncedQuery)) {
        return;
      }
      const timer = setTimeout(() => {
        this.debouncedQuery.set(query);
        this.pages.set(1);
        this.closedPages.set(1);
      }, 250);
      onCleanup(() => clearTimeout(timer));
    });
  }

  protected onStates(states: TicketState[]): void {
    this.selectedStates.set(states);
    this.pages.set(1);
  }

  /**
   * Applies a saved filter, or none: its search and the open states it names go into the bar,
   * everything else to the lists as it is; none clears all of it. The lists start at one page.
   */
  protected applyFilter(filter: SavedFilter | null): void {
    const b = filter
      ? toBacklog(filter.parameters, openStates)
      : { q: '', states: [] as TicketState[], extra: {} };
    this.applied.set(filter);
    this.extra.set(b.extra);
    this.query.set(b.q);
    this.debouncedQuery.set(b.q);
    this.selectedStates.set(b.states);
    this.pages.set(1);
    this.closedPages.set(1);
  }

  protected loadMore(): void {
    this.pages.update((pages) => pages + 1);
  }

  protected loadMoreClosed(): void {
    this.closedPages.update((pages) => pages + 1);
  }

  /** From the tickets done after a time to every closed one. */
  protected allClosed(): void {
    this.doneAfter.set(undefined);
    this.closedPages.set(1);
  }

  protected dateTime(iso: string): string {
    return dateTime(iso);
  }

  /** A ticket filed here shows at once; the event that follows changes nothing more. */
  protected filed(): void {
    refresh(this.openList, this.injector);
  }

  /** What the marker of a row says, for its tooltip and for a screen reader. */
  protected markText(mark: ScoreMark, ticket: Ticket): string {
    return `The score, ${ticket.score?.toFixed(1)}, says ${mark} than the rank puts it`;
  }

  /**
   * "Sort by score" (docs/adr/0014 D3): never automatic, and it replaces the order set by hand, so
   * the page asks first. The open tickets the person sees take their places in the score's order —
   * within each horizon and among each parent's children, as the marks read them.
   */
  protected askSort(): void {
    const project = this.project();
    this.confirm.confirm({
      header: `Sort ${project} by score?`,
      message:
        `Every open ticket of ${project} you can see takes its place by its score, highest first, ` +
        'within its horizon and among its siblings. The order set by hand is replaced, and the sort ' +
        'is recorded as one act of the project.',
      acceptLabel: 'Sort by score',
      rejectLabel: 'Keep the order',
      defaultFocus: 'reject',
      accept: () => void this.sortByScore(project),
    });
  }

  private async sortByScore(project: string): Promise<void> {
    const tenant = this.session.tenant();
    if (!tenant || this.sorting()) {
      return;
    }
    this.sorting.set(true);
    try {
      const moved = await this.actions.sortByScore(tenant, project);
      const said =
        moved > 0
          ? `Sorted by score: ${count(moved, 'ticket')} moved`
          : 'The backlog follows the score already';
      this.messages.add({ severity: moved > 0 ? 'success' : 'info', summary: said });
      this.status.set(said);
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.sorting.set(false);
    }
  }

  protected open(ticket: Ticket): void {
    void this.router.navigate(['/t', this.session.tenant(), 'tickets', this.shortKey(ticket.key)]);
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  protected shortKey(key: string): string {
    return key.slice(key.indexOf('/') + 1);
  }

  /** The progress column shows the stage the ticket works on now (docs/adr/0018 D1). */
  protected stageOf(ticket: Ticket): Stage | null {
    return currentStage(ticket);
  }

  protected stageValue(ticket: Ticket, stage: Stage): number {
    return stagesOf(ticket)[stage];
  }

  private read(keys: readonly string[]): Ticket[] {
    return keys
      .map((key) => this.tickets.cache.entry(key)()?.value)
      .filter((ticket): ticket is Ticket => ticket !== undefined);
  }

  // The drag. The CDK measures its lists the moment a drag starts, right after it announces it, so
  // the groups that show only while a row is dragged have to be in the page by then.

  protected dragStarted(): void {
    this.held.set(this.groups());
    this.view.detectChanges();
  }

  protected dragEnded(): void {
    this.held.set(undefined);
  }

  protected dropped(event: CdkDragDrop<Group, Group, Ticket>): void {
    this.held.set(undefined);
    // Dropped beside the table: the row goes back where it was.
    if (!event.isPointerOverContainer) {
      return;
    }
    const from = event.previousContainer.data;
    const to = event.container.data;
    const plan = planDrop(event.item.data, from.horizon, to.horizon, to.rows, event.currentIndex);
    if (plan) {
      void this.move(plan);
    }
  }

  // The menu of a row does what a drag does, for those who cannot drag.

  protected openMenu(event: Event, group: Group, row: Row): void {
    const ticket = row.ticket;
    // The row is one of the group's rows, so it has a place among them.
    const place = position(group.rows, ticket)!;
    const first = place.index === 0;
    const last = place.index === place.count;
    const step = (index: number) => () => this.menuMove(planStep(group.rows, ticket, index));
    this.menuItems.set([
      { label: 'Move up', icon: 'pi pi-arrow-up', disabled: first, command: step(place.index - 1) },
      {
        label: 'Move down',
        icon: 'pi pi-arrow-down',
        disabled: last,
        command: step(place.index + 1),
      },
      { label: 'Move to top', icon: 'pi pi-angle-double-up', disabled: first, command: step(0) },
      {
        label: 'Move to bottom',
        icon: 'pi pi-angle-double-down',
        disabled: last,
        command: step(place.count),
      },
      { separator: true },
      ...horizons
        .filter((horizon) => horizon !== group.horizon)
        .map((horizon) => ({
          label: `Move to ${horizon}`,
          icon: 'pi pi-arrow-right-arrow-left',
          command: () => {
            // Every horizon has a group, empty or not.
            const target = this.groups().find((each) => each.horizon === horizon)!;
            this.menuMove(planGroup(target.rows, ticket, horizon));
          },
        })),
    ]);
    this.menu().toggle(event);
  }

  /** A menu move, after which the keyboard finds the row's menu button again where the row went. */
  private menuMove(plan: Plan | null): void {
    if (plan) {
      void this.move(plan);
      afterNextRender(() => this.focusMenuOf(plan.ticket.key), { injector: this.injector });
    }
  }

  private focusMenuOf(key: string): void {
    this.host.nativeElement.querySelector<HTMLElement>(`[data-testid="row-menu-${key}"]`)?.focus();
  }

  // The move. It shows at once; the writes follow, one after the other. A failure puts the row back
  // and tells the person why.

  private move(plan: Plan): Promise<void> {
    const key = plan.ticket.key;
    if (plan.horizon) {
      const horizon = plan.horizon;
      this.shownIn.update((shown) => new Map(shown).set(key, horizon));
    }
    const move: Move | null = plan.placement ? { key, placement: plan.placement } : null;
    if (move) {
      this.moves.update((moves) => [...moves, move]);
    }
    this.announce(key);
    const done = this.writes.then(() => this.write(plan, move));
    this.writes = done;
    return done;
  }

  /** Tells a screen reader where a ticket went: its group and its place among the siblings. */
  private announce(key: string): void {
    const group = this.groups().find((each) => each.rows.some((row) => row.ticket.key === key));
    const place =
      group && position(group.rows, group.rows.find((row) => row.ticket.key === key)!.ticket);
    this.status.set(
      group && place
        ? `${this.shortKey(key)} moved to ${group.horizon}, place ${place.index + 1} of ${place.count + 1}`
        : `${this.shortKey(key)} moved`,
    );
  }

  /**
   * The writes of one move; they take care of their own failure, so the next move can follow. A
   * failure takes the move back, which puts the row where the list has it.
   */
  private async write(plan: Plan, move: Move | null): Promise<void> {
    const ticket = plan.ticket;
    const key = ticket.key;
    try {
      if (plan.horizon) {
        await this.actions.setHorizon(key, plan.horizon);
        // The cache holds the ticket with its new horizon now.
        this.shownIn.update((shown) => {
          const next = new Map(shown);
          next.delete(key);
          return next;
        });
      }
      if (move) {
        const { version } = await this.actions.rank(key, rankBody(move.placement));
        this.moves.update((moves) =>
          moves.map((each) => (each === move ? { ...each, version } : each)),
        );
      }
      // later is where a ticket nobody placed stands: it keeps no reason (docs/adr/0010 D3).
      if (plan.horizon && plan.horizon !== 'later') {
        this.ask(key, plan.horizon);
      }
    } catch (error) {
      this.shownIn.update((shown) => {
        const next = new Map(shown);
        next.delete(key);
        return next;
      });
      this.moves.update((moves) => moves.filter((each) => each !== move));
      this.report(error, key);
      refresh(this.openList, this.injector);
    }
  }

  private report(error: unknown, key: string): void {
    if (error instanceof StaleWrite) {
      this.messages.add({
        severity: 'warn',
        summary: 'Changed meanwhile',
        detail: `${this.shortKey(key)} was changed by someone else: its horizon is ${error.current.horizon} now.`,
        life: 6000,
      });
    } else {
      this.problems.report(error);
    }
  }

  // The reason of a horizon a person has just set, which they may add (docs/adr/0010 D3).

  private ask(key: string, value: Horizon): void {
    this.reason.set('');
    this.asking.set({ key, value });
    afterNextRender(
      () =>
        this.host.nativeElement.querySelector<HTMLInputElement>('[data-testid="reason"]')?.focus(),
      { injector: this.injector },
    );
  }

  /** The field goes; where a key sent it away, the keyboard goes back to the menu button of the row. */
  protected dismissReason(refocus = false): void {
    const key = this.asking()?.key;
    this.asking.set(null);
    if (refocus && key) {
      afterNextRender(() => this.focusMenuOf(key), { injector: this.injector });
    }
  }

  protected async sendReason(): Promise<void> {
    const asked = this.asking();
    const text = this.reason().trim();
    if (!asked || this.sendingReason()) {
      return;
    }
    if (text === '') {
      this.dismissReason(true);
      return;
    }
    this.sendingReason.set(true);
    try {
      await this.actions.setHorizon(asked.key, asked.value, text);
    } catch (error) {
      this.report(error, asked.key);
    } finally {
      this.sendingReason.set(false);
      if (this.asking()?.key === asked.key) {
        this.dismissReason(true);
      }
    }
  }
}
