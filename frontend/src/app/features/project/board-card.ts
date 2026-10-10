import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { Tooltip } from 'primeng/tooltip';
import { Ticket } from '../../api/models';
import { SecurityBadge, SeverityBadge, StateBadge, TypeIcon } from '../../shared/badges';
import { SizeIcon } from '../../shared/size';
import { StageBar } from '../../shared/stage-bar';
import { currentStage, stagesOf } from '../../shared/stages';
import { parentChip } from '../../shared/ticket-head';
import { count } from '../../shared/time';
import { Move } from '../../shared/transitions';
import { meanings } from '../../shared/vocabulary';

/**
 * A card of the board (docs/adr/0018 D1). In a state column: the type and the key, the title,
 * the parent by its head — of any project or team, linked where the reader may open it, the
 * placeholder `<team> [Confidential]` where they may not see it (docs/adr/0005 D3, docs/adr/0065
 * D5) —, `release` where the horizon is that, the state in Refinement (which holds two), severity and
 * security, the bar of the stage the ticket works on, the block on a blocked card, the open
 * prerequisites, the effort as its size and the assignee — and the button of the card's action and
 * the menu of its moves. In the column `next` it is compact: key, title, size, state and the button
 * that makes the ticket `now`. A click on the card itself is the board's, which opens the ticket.
 */
@Component({
  selector: 'app-board-card',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    RouterLink,
    SecurityBadge,
    SeverityBadge,
    SizeIcon,
    StageBar,
    StateBadge,
    Tooltip,
    TypeIcon,
  ],
  templateUrl: './board-card.html',
  styleUrl: './board-card.scss',
  host: { '[class.compact]': 'compact()', '[attr.data-key]': 'ticket().key' },
})
export class BoardCard {
  readonly ticket = input.required<Ticket>();
  readonly tenant = input.required<string>();
  /** The card of the column `next`. */
  readonly compact = input(false);
  /** The card's action in its own column, `filed → analysed`. */
  readonly action = input<Move | null>(null);
  /** The card has moves to offer in its menu. */
  readonly movable = input(false);

  readonly act = output<Move>();
  readonly menu = output<Event>();
  readonly now = output<void>();

  protected readonly meanings = meanings;
  protected readonly shortKey = computed(() => {
    const key = this.ticket().key;
    return key.slice(key.indexOf('/') + 1);
  });
  protected readonly stage = computed(() => currentStage(this.ticket()));
  protected readonly stageValue = computed(() => {
    const stage = this.stage();
    return stage ? stagesOf(this.ticket())[stage] : 0;
  });
  /** Refinement holds two states; the card says which. */
  protected readonly showsState = computed(
    () => this.compact() || ['filed', 'analysed'].includes(this.ticket().state),
  );
  /** The chip that names the parent, where the ticket has one. */
  protected readonly parent = computed(() => {
    const head = this.ticket().parent_head;
    return head ? parentChip(head, this.tenant()) : null;
  });
  protected readonly prerequisites = computed(() =>
    count(this.ticket().open_prerequisites, 'open prerequisite'),
  );
}
