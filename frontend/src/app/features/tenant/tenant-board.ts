import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Select } from 'primeng/select';
import { Skeleton } from 'primeng/skeleton';
import { SavedFilter, Ticket } from '../../api/models';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { Point } from '../project/board-columns';
import { BoardMoves, shortKey } from '../project/board-moves';
import {
  BoardExtra,
  boardLeftOut,
  describe,
  fromBoard,
  toBoard,
  withoutEmpty,
} from '../project/saved-filter-model';
import { SavedFilters } from '../project/saved-filters';
import { MoveDialog } from '../ticket/move-dialog';
import { BoardLane } from './board-lane';
import { chosenKeys, excludes, laneOf, lanesOf, refusingLane } from './tenant-board-model';
import { TeamHeader } from './team-header';

/** The card a person drags, and the swimlane it belongs to. */
interface Dragged {
  lane: string;
  key: string;
}

/**
 * The tenant's board (docs/adr/0018 D4), a tab of the team's head ({@link TeamHeader}, D6 as
 * amended 2026-10-10): one swimlane per project the person sees that is not
 * archived ({@link BoardLane}), each with the project board's columns over the same tickets, so a
 * card stands in the same column on both boards. Only the swimlanes in view or near it load. The
 * project filter is the address's `?project=`, repeated; a saved filter applies as on the backlog
 * (docs/adr/0018 D5): its projects go into the address, every other condition to the swimlanes'
 * lists, `include_terminal` left out and named under the bar. A card dragged to another column of
 * its swimlane is the transition of docs/adr/0009, with the project board's dialogs
 * ({@link BoardMoves}); one dragged to another swimlane is refused, visibly — the swimlane under it
 * says no while it is there, and a toast says why once it is let go: a ticket never changes
 * project on a board. Rank is not edited here; the backlog owns it.
 */
@Component({
  selector: 'app-tenant-board',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    BoardLane,
    ButtonDirective,
    FormsModule,
    MoveDialog,
    SavedFilters,
    Select,
    Skeleton,
    TeamHeader,
  ],
  providers: [BoardMoves],
  templateUrl: './tenant-board.html',
  styleUrl: './tenant-board.scss',
})
export class TenantBoard {
  /** `?project=COW&project=!OPS`: the swimlanes the filter shows or excludes; none, every one. */
  readonly project = input<string | string[]>();

  protected readonly session = inject(SessionService);
  protected readonly projects = inject(ProjectsService);
  protected readonly moves = inject(BoardMoves);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly messages = inject(MessageService);
  private readonly document = inject(DOCUMENT);

  protected readonly chosen = computed(() => chosenKeys(this.project()));
  /** What the select shows chosen: the projects the filter names, not those it excludes. */
  protected readonly selected = computed(() => this.chosen().filter((key) => !excludes(key)));
  protected readonly lanes = computed(() => lanesOf(this.projects.list(), this.chosen()));
  protected readonly options = computed(() =>
    lanesOf(this.projects.list(), []).map((project) => ({
      key: project.key,
      label: `${project.key} · ${project.name}`,
    })),
  );

  /**
   * The saved filter applied, and its conditions beyond the projects, which every swimlane's list
   * takes as they are (docs/adr/0018 D5). A saved filter is the tenant's: another tenant's page
   * starts without one.
   */
  protected readonly applied = linkedSignal<string | null, SavedFilter | null>({
    source: () => this.session.tenant(),
    computation: () => null,
  });
  protected readonly extra = linkedSignal<string | null, BoardExtra>({
    source: () => this.session.tenant(),
    computation: () => ({}),
  });
  /** What the board applies now, as a saved filter keeps it. */
  protected readonly currentFilter = computed(() => fromBoard(this.chosen(), this.extra()));
  /** A saved filter's `include_terminal` is the tenant's ticket list's: the bar says so. */
  protected readonly leftOut = boardLeftOut;
  /** What the board applies beyond its select, as a line; null where there is nothing. */
  protected readonly beyond = computed(() => {
    const rest = withoutEmpty({ project: this.chosen().filter(excludes), ...this.extra() });
    return Object.keys(rest).length > 0 ? describe(rest) : null;
  });

  /** The card that is dragged, while one is. */
  protected readonly dragged = signal<Dragged | null>(null);
  protected readonly dragFrom = computed(() => this.dragged()?.lane ?? null);
  /** The swimlane under a card of another one, which says no. */
  protected readonly refusing = signal<string | null>(null);

  /** The select's choice goes into the address, the projects the filter excludes kept. */
  protected choose(keys: string[] | null): void {
    this.showProjects([...(keys ?? []), ...this.chosen().filter(excludes)]);
  }

  /**
   * Applies a saved filter, or none: its projects go into the address, every other condition to
   * the swimlanes' lists as it is; none clears all of it.
   */
  protected applyFilter(filter: SavedFilter | null): void {
    const board = filter ? toBoard(filter.parameters) : { projects: [], extra: {} };
    this.applied.set(filter);
    this.extra.set(board.extra);
    this.showProjects(board.projects);
  }

  /** The projects go into the address, so that the board can be linked and comes back as it was. */
  protected showProjects(keys: readonly string[]): void {
    void this.router.navigate([], {
      relativeTo: this.route,
      queryParams: { project: keys.length > 0 ? keys : null },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    });
  }

  protected dragStarted(lane: string, ticket: Ticket): void {
    this.dragged.set({ lane, key: ticket.key });
    this.refusing.set(null);
  }

  protected dragMoved(lane: string, point: Point): void {
    const refusing = refusingLane(lane, this.laneAt(point));
    if (refusing !== this.refusing()) {
      this.refusing.set(refusing);
    }
  }

  /**
   * The card is let go. Over its own swimlane the columns decide what the drop is; over another,
   * the CDK has kept it where it was, and the person is told why.
   */
  protected dragEnded(lane: string, point: Point | undefined): void {
    const dragged = this.dragged();
    const over = point ? refusingLane(lane, this.laneAt(point)) : this.refusing();
    this.dragged.set(null);
    this.refusing.set(null);
    if (dragged && over) {
      this.messages.add({
        severity: 'warn',
        summary: 'Not moved',
        detail: `A ticket never changes project on a board: ${shortKey(dragged.key)} stays in ${lane}.`,
        life: 6000,
      });
    }
  }

  /** The swimlane under a point of the window; the dragged card's preview lets the pointer through. */
  private laneAt(point: Point): string | null {
    return laneOf(this.document.elementFromPoint?.(point.x, point.y));
  }
}
