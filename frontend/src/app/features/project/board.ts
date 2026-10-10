import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  Injector,
  input,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { Tooltip } from 'primeng/tooltip';
import { ProblemService } from '../../core/problem.service';
import { ProjectsService } from '../../core/projects.service';
import { refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { TicketsService } from '../../core/tickets.service';
import { Clock, count } from '../../shared/time';
import { MoveDialog } from '../ticket/move-dialog';
import { BoardColumns } from './board-columns';
import { boardList } from './board-list';
import { arrange, placeOf } from './board-model';
import { BoardMoves } from './board-moves';
import { ProjectHeader } from './project-header';

/** How far back the header counts the tickets done, in days (docs/adr/0018 D1). */
const doneDays = 14;

const hour = 3_600_000;

/**
 * A project's board (docs/adr/0018 D1): the current work. The open leaves of the horizon `now` and
 * `release` stand in the columns Refinement, Ready, In Progress, Blocked and Review — a view over
 * their states, each in the project's rank and counted against its WIP limit (docs/adr/0019 D3) —
 * and the leaves of the horizon `next` in the column `next` on the left, whatever their state, each
 * with the button that makes it `now` ({@link BoardColumns}). A card dragged into another column is
 * that transition of docs/adr/0009, made by {@link BoardMoves}, whose dialog and live region the
 * page shows. The header counts the tickets done in the last fourteen days and leads to them. The
 * event stream moves the cards, and holds back while one is dragged.
 */
@Component({
  selector: 'app-board',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [BoardColumns, MoveDialog, ProjectHeader, RouterLink, Tooltip],
  providers: [BoardMoves],
  templateUrl: './board.html',
  styleUrl: './board.scss',
})
export class Board {
  readonly project = input.required<string>();

  protected readonly session = inject(SessionService);
  private readonly tickets = inject(TicketsService);
  private readonly projects = inject(ProjectsService);
  private readonly clock = inject(Clock);
  private readonly problems = inject(ProblemService);
  private readonly injector = inject(Injector);
  protected readonly moves = inject(BoardMoves);

  /** The open tickets of the board's horizons, every page of them, in the project's rank. */
  private readonly cards = boardList(this.tickets, this.project, this.session.tenant);

  /**
   * Since when a done ticket counts: fourteen days back, to the hour, so that the count asks again
   * once an hour and not with every tick of the clock. The count's link hands it to the backlog.
   */
  protected readonly doneSince = computed(() =>
    new Date(Math.floor((this.clock.now() - doneDays * 24 * hour) / hour) * hour).toISOString(),
  );
  /** The tickets done since then, numbered so that the answer carries the total (docs/adr/0048 D2). */
  private readonly doneList = this.tickets.projectTickets(() => {
    const tenant = this.session.tenant();
    return tenant
      ? {
          team: tenant,
          project: this.project(),
          state: ['done'],
          done_after: this.doneSince(),
          page: 1,
          per_page: 25,
        }
      : undefined;
  });
  protected readonly doneCount = computed(() =>
    this.doneList.hasValue() ? this.doneList.value()?.total : undefined,
  );
  protected readonly doneText = computed(() => {
    const done = this.doneCount();
    return done === undefined ? '' : `${count(done, 'ticket')} done in the last ${doneDays} days`;
  });

  private readonly limits = computed(() => this.projects.byKey(this.project())?.wip_limits ?? {});
  protected readonly board = computed(() =>
    arrange(this.moves.read(this.cards.keys() ?? []), placeOf, this.limits()),
  );
  protected readonly cardCount = computed(() => {
    const board = this.board();
    return board.next.length + board.columns.reduce((sum, column) => sum + column.cards.length, 0);
  });

  protected readonly loading = computed(() => this.cards.list.isLoading());
  protected readonly failure = computed(() => {
    const error = this.cards.list.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The board could not be loaded: ${problem.detail || problem.title}`;
  });

  /** A failed transition loads the list again. */
  protected readonly reload = () => refresh(this.cards.list, this.injector);
}
