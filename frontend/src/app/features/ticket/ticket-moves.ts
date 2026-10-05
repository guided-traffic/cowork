import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  signal,
} from '@angular/core';
import { MenuItem } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Menu } from 'primeng/menu';
import { Ticket } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { TicketActions } from '../../core/ticket-actions.service';
import { Move, MoveKind, movesOf } from '../../shared/transitions';
import { MoveDialog, MoveRequest } from './move-dialog';

/** The moves that may stand on the main button, in the order the matrix lists them. */
const mainKinds: ReadonlySet<MoveKind> = new Set([
  'forward',
  'done',
  'unblock',
  'reopen',
  'withdraw',
]);

/**
 * The state machine on the detail page (docs/adr/0009): the main move as a button, the others in a
 * menu, and {@link MoveDialog} for what a move requires — a reason, the verification note of done
 * by hand, a block — and for the person's override of open prerequisites (docs/adr/0012 D7). A
 * ticket done by its stages has no move here: lowering a stage reopens it.
 */
@Component({
  selector: 'app-ticket-moves',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Menu, MoveDialog],
  templateUrl: './ticket-moves.html',
  styleUrl: './ticket-moves.scss',
})
export class TicketMoves {
  readonly ticket = input.required<Ticket>();

  private readonly actions = inject(TicketActions);
  private readonly problems = inject(ProblemService);

  /** The key alone, so that a newer version of the same ticket keeps what is open. */
  private readonly key = computed(() => this.ticket().key);
  protected readonly moves = computed(() => movesOf(this.ticket()));
  /** The forward step, done by hand, the unblock, the withdrawal or the reopen — whichever comes first. */
  protected readonly main = computed(() => this.moves().find((move) => mainKinds.has(move.kind)));
  protected readonly others = computed<MenuItem[]>(() =>
    this.moves()
      .filter((move) => move !== this.main())
      .map((move) => ({ label: move.label, command: () => this.start(move) })),
  );

  /**
   * The move whose input the dialog asks for. It belongs to the ticket it was chosen on: the page
   * is reused when its path names another ticket, and a dialog confirmed after that would move the
   * ticket shown then.
   */
  protected readonly request = linkedSignal<string, MoveRequest | null>({
    source: this.key,
    computation: () => null,
  });
  /** A move that needs no input is on its way. */
  protected readonly sending = signal(false);

  protected start(move: Move): void {
    if (move.input === 'none') {
      void this.send(move);
    } else {
      this.request.set({ kind: 'move', move });
    }
  }

  private async send(move: Move): Promise<void> {
    this.sending.set(true);
    try {
      await this.actions.transition(this.ticket().key, { to: move.to });
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.sending.set(false);
    }
  }
}
