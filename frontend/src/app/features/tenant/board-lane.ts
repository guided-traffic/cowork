import { ScrollDispatcher } from '@angular/cdk/scrolling';
import {
  afterNextRender,
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  ElementRef,
  inject,
  Injector,
  input,
  output,
  signal,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { Project, Ticket } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { refresh } from '../../core/refresh';
import { TicketsService } from '../../core/tickets.service';
import { BoardColumns, Point } from '../project/board-columns';
import { boardList } from '../project/board-list';
import { arrange, placeOf } from '../project/board-model';
import { BoardMoves } from '../project/board-moves';
import { BoardExtra } from '../project/saved-filter-model';

/**
 * How near the window a swimlane has to come to load: a screen's height above or below the area the
 * page scrolls in.
 */
export const nearMargin = '100% 0px';

/**
 * A swimlane of the tenant's board (docs/adr/0018 D4): a project's name, linked to its board, and
 * the project board's columns over the same tickets ({@link BoardColumns}, {@link boardList}),
 * counted against the project's WIP limits, and narrowed by the conditions of the saved filter the
 * page applies (`filter`). It loads lazily: its list is asked for only while the swimlane is in
 * view or near it, and follows the event stream only then; out of view it keeps what it showed,
 * and asks again — with the page's `ETag`, so an unchanged list costs a `304` — when it comes back.
 * While a card is dragged anywhere on the page it holds still, and while a card of another
 * swimlane is over it, it says no.
 */
@Component({
  selector: 'app-board-lane',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [BoardColumns, RouterLink],
  templateUrl: './board-lane.html',
  styleUrl: './board-lane.scss',
  host: {
    role: 'region',
    '[attr.aria-labelledby]': "'lane-' + project().key",
    '[attr.data-lane]': 'project().key',
    '[attr.data-testid]': "'lane-' + project().key",
    '[class.elsewhere]': 'dragFrom() !== null && dragFrom() !== project().key',
    '[class.refusing]': 'refusing()',
  },
})
export class BoardLane {
  readonly project = input.required<Project>();
  readonly tenant = input.required<string>();
  /** The conditions of the saved filter the page applies beyond the projects; none, every card. */
  readonly filter = input<BoardExtra>({});
  /** The key of the swimlane a card is dragged from, while one is. */
  readonly dragFrom = input<string | null>(null);
  /** A card of another swimlane is over this one, which refuses it. */
  readonly refusing = input(false);

  readonly dragStart = output<Ticket>();
  readonly dragMove = output<Point>();
  readonly dragEnd = output<Point | undefined>();

  private readonly tickets = inject(TicketsService);
  private readonly moves = inject(BoardMoves);
  private readonly problems = inject(ProblemService);
  private readonly injector = inject(Injector);

  /** In view or near it: only then is the list asked for. Without an observer, always. */
  protected readonly near = signal(typeof IntersectionObserver === 'undefined');

  private readonly cards = boardList(
    this.tickets,
    () => this.project().key,
    () => (this.near() ? this.tenant() : null),
    this.filter,
  );
  protected readonly loaded = computed(() => this.cards.keys() !== undefined);
  protected readonly view = computed(() =>
    arrange(this.moves.read(this.cards.keys() ?? []), placeOf, this.project().wip_limits),
  );
  protected readonly countText = computed(() => {
    if (!this.loaded()) {
      return '';
    }
    const view = this.view();
    const cards =
      view.next.length + view.columns.reduce((sum, column) => sum + column.cards.length, 0);
    return `${cards} on the board`;
  });
  protected readonly loading = computed(() => this.cards.list.isLoading());
  protected readonly failure = computed(() => {
    const error = this.cards.list.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The board of ${this.project().key} could not be loaded: ${problem.detail || problem.title}`;
  });

  /** A failed transition loads the swimlane's list again. */
  protected readonly reload = () => refresh(this.cards.list, this.injector);

  constructor() {
    const host = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
    const scrolling = inject(ScrollDispatcher);
    const destroyed = inject(DestroyRef);
    afterNextRender(() => {
      if (typeof IntersectionObserver === 'undefined') {
        return;
      }
      // The page scrolls in the shell's content area, not in the window: that area is the root,
      // so that the margin reaches past what it clips.
      const root =
        scrolling.getAncestorScrollContainers(host)[0]?.getElementRef().nativeElement ?? null;
      const observer = new IntersectionObserver(
        (entries) => this.near.set(entries.at(-1)?.isIntersecting ?? false),
        { root, rootMargin: nearMargin },
      );
      observer.observe(host);
      destroyed.onDestroy(() => observer.disconnect());
    });
  }
}
