import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MenuItem } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Checkbox } from 'primeng/checkbox';
import { Dialog } from 'primeng/dialog';
import { InputText } from 'primeng/inputtext';
import { Menu } from 'primeng/menu';
import { Select } from 'primeng/select';
import { Textarea } from 'primeng/textarea';
import { BlockKind, Ticket } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { TicketActions } from '../../core/ticket-actions.service';
import { Move, movesFrom } from '../../shared/transitions';

const blockKinds: BlockKind[] = ['decision', 'human', 'product', 'release', 'external', 'ticket'];

/**
 * The state machine on the detail page (docs/adr/0009): the main move as a button, the others in a
 * menu, and a dialog for what a move requires — a reason, the verification note of `done`, a
 * block — and for the person's override of open prerequisites (docs/adr/0012 D7).
 */
@Component({
  selector: 'app-ticket-moves',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Checkbox, Dialog, FormsModule, InputText, Menu, Select, Textarea],
  templateUrl: './ticket-moves.html',
  styleUrl: './ticket-moves.scss',
})
export class TicketMoves {
  readonly ticket = input.required<Ticket>();

  private readonly actions = inject(TicketActions);
  private readonly problems = inject(ProblemService);

  protected readonly blockKinds = blockKinds;
  protected readonly moves = computed(() =>
    movesFrom(this.ticket().state, this.ticket().block?.from),
  );
  /** The forward step, `done`, the unblock or the reopen — whichever the state has. */
  protected readonly main = computed(() =>
    this.moves().find((move) => ['forward', 'done', 'unblock', 'reopen'].includes(move.kind)),
  );
  protected readonly others = computed<MenuItem[]>(() =>
    this.moves()
      .filter((move) => move !== this.main())
      .map((move) => ({ label: move.label, command: () => this.start(move) })),
  );

  protected readonly pending = signal<Move | null>(null);
  protected readonly text = signal('');
  protected readonly blockKind = signal<BlockKind>('decision');
  protected readonly blockTicket = signal('');
  protected readonly prerequisitesOpen = signal(false);
  protected readonly override = signal(false);
  protected readonly overrideReason = signal('');
  protected readonly busy = signal(false);
  protected readonly error = signal('');

  protected readonly dialogTitle = computed(() => {
    const move = this.pending();
    if (!move) {
      return '';
    }
    return move.kind === 'done' ? 'Done: how was it verified?' : move.label;
  });
  protected readonly canSend = computed(() => {
    const move = this.pending();
    if (!move || this.busy()) {
      return false;
    }
    if (
      move.input === 'block' &&
      this.blockKind() === 'ticket' &&
      this.blockTicket().trim() === ''
    ) {
      return false;
    }
    if (this.override() && this.overrideReason().trim() === '') {
      return false;
    }
    return move.input === 'none' || this.text().trim() !== '';
  });

  protected start(move: Move): void {
    if (move.input === 'none') {
      void this.send(move);
      return;
    }
    this.text.set('');
    this.blockKind.set('decision');
    this.blockTicket.set('');
    this.prerequisitesOpen.set(false);
    this.override.set(false);
    this.overrideReason.set('');
    this.error.set('');
    this.pending.set(move);
  }

  protected close(): void {
    this.pending.set(null);
  }

  protected async send(move = this.pending()): Promise<void> {
    if (!move) {
      return;
    }
    const text = this.text().trim();
    this.busy.set(true);
    this.error.set('');
    try {
      await this.actions.transition(this.ticket().key, {
        to: move.to,
        ...(move.input === 'reason' ? { reason: text } : {}),
        ...(move.input === 'note' ? { note: text } : {}),
        ...(move.input === 'block'
          ? {
              reason: text,
              block: {
                kind: this.blockKind(),
                ...(this.blockKind() === 'ticket' ? { ticket: this.blockTicket().trim() } : {}),
              },
            }
          : {}),
        ...(this.override()
          ? { override_prerequisites: true, reason: this.overrideReason().trim() }
          : {}),
      });
      this.pending.set(null);
    } catch (error) {
      const problem = this.problems.read(error);
      if (error instanceof HttpErrorResponse && problem.code === 'open_prerequisites') {
        this.prerequisitesOpen.set(true);
        this.error.set(problem.detail);
      } else if (this.pending()) {
        this.error.set(problem.detail || problem.title);
      } else {
        this.problems.report(error);
      }
    } finally {
      this.busy.set(false);
    }
  }
}
