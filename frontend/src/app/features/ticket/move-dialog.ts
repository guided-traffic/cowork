import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  output,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { Checkbox } from 'primeng/checkbox';
import { Dialog } from 'primeng/dialog';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { Textarea } from 'primeng/textarea';
import { BlockKind, Ticket, TicketPatch, Transition } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { keepOpenWhile } from '../../shared/keep-open';
import { Stage, stageFields, stageNames, stages, staysDoneByStages } from '../../shared/stages';
import { count } from '../../shared/time';
import { Move, MoveInput } from '../../shared/transitions';

const blockKinds: BlockKind[] = ['decision', 'human', 'product', 'release', 'external', 'ticket'];

/** A write of the progress stages (docs/adr/0017 D2). */
export type StagePatch = Pick<TicketPatch, 'progress_refinement' | 'progress' | 'progress_review'>;

/**
 * What the dialog asks for and then sends: a transition of docs/adr/0009 that needs a reason, a
 * verification note or a block; the write of the stages that fills the last of them, which is the
 * done act and needs the note; or the write that lowers a stage of a ticket done by its stages,
 * which reopens it and needs a reason (docs/adr/0009 D5).
 */
export type MoveRequest =
  | { kind: 'move'; move: Move }
  | { kind: 'complete'; patch: StagePatch }
  | { kind: 'reopen'; patch: StagePatch };

/** What each stage of a patch is set to: `Review to 75%`. */
function changes(patch: StagePatch): string {
  return stages
    .filter((stage: Stage) => patch[stageFields[stage]] !== undefined)
    .map((stage: Stage) => `${stageNames[stage]} to ${patch[stageFields[stage]]}%`)
    .join(', ');
}

/**
 * The dialog of a move that needs input, for the detail page and the board alike: a reason, the
 * verification note of done, a block — and, for done, the person's override of open prerequisites
 * with its reason (docs/adr/0012 D7), offered when the ticket says it has open prerequisites or
 * when the server refused over them. It stays open while its request runs and shows a refusal in
 * the form; `closed` says `true` once the write went through, whose answer is in the ticket cache
 * then, and `false` when the person cancelled.
 */
@Component({
  selector: 'app-move-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Checkbox, Dialog, FormsModule, InputText, Select, Textarea],
  templateUrl: './move-dialog.html',
  styleUrl: './move-dialog.scss',
})
export class MoveDialog {
  /** The ticket the request is for, as it is now. */
  readonly ticket = input.required<Ticket>();
  /** The request the dialog is open for; `null` keeps it closed. */
  readonly request = input<MoveRequest | null>(null);
  readonly closed = output<boolean>();

  private readonly actions = inject(TicketActions);
  private readonly problems = inject(ProblemService);

  protected readonly blockKinds = blockKinds;

  // Every request starts with an empty form.
  protected readonly text = linkedSignal({ source: this.request, computation: () => '' });
  protected readonly blockKind = linkedSignal<MoveRequest | null, BlockKind>({
    source: this.request,
    computation: () => 'decision',
  });
  protected readonly blockTicket = linkedSignal({ source: this.request, computation: () => '' });
  /** The server refused over open prerequisites the ticket did not show yet. */
  private readonly refusedOverPrerequisites = linkedSignal({
    source: this.request,
    computation: () => false,
  });
  protected readonly override = linkedSignal({ source: this.request, computation: () => false });
  protected readonly overrideReason = linkedSignal({ source: this.request, computation: () => '' });
  protected readonly error = linkedSignal({ source: this.request, computation: () => '' });
  protected readonly busy = signal(false);

  /** What the person types: a block's text and every reason are a reason; done takes the note. */
  protected readonly input = computed<MoveInput>(() => {
    const request = this.request();
    switch (request?.kind) {
      case 'move':
        return request.move.input;
      case 'complete':
        return 'note';
      default:
        return 'reason';
    }
  });

  /** Done, by hand or by the stages: the act the open prerequisites refuse. */
  private readonly closes = computed(() => this.input() === 'note');

  protected readonly offersOverride = computed(
    () =>
      this.closes() && (this.ticket().open_prerequisites > 0 || this.refusedOverPrerequisites()),
  );

  protected readonly overrideLabel = computed(() => {
    const open = this.ticket().open_prerequisites;
    return `Close it over its ${open > 0 ? count(open, 'open prerequisite') : 'open prerequisites'}`;
  });

  protected readonly title = computed(() => {
    const request = this.request();
    switch (request?.kind) {
      case undefined:
        return '';
      case 'complete':
        return 'Done: how was it verified?';
      case 'reopen':
        return `Reopen ${this.shortKey()}: why?`;
      case 'move':
        return request.move.kind === 'done'
          ? 'Done by hand: how was it verified?'
          : request.move.label;
    }
  });

  /** What the request does beyond what its title says, where that is not plain. */
  protected readonly explanation = computed(() => {
    const request = this.request();
    const ticket = this.ticket();
    if (!request) {
      return '';
    }
    if (request.kind === 'complete') {
      return `${changes(request.patch)} fills the last progress stage: the ticket is done.`;
    }
    if (request.kind === 'reopen') {
      return `${changes(request.patch)}: the ticket was done by its stages and goes back to ${ticket.done_from ?? 'the state it was done from'}.`;
    }
    switch (request.move.kind) {
      case 'done':
        return 'The progress stages stay as they are.';
      case 'withdraw':
        return staysDoneByStages(ticket)
          ? 'Its three stages are full: it stays done, by its stages.'
          : `It goes back to ${request.move.to}.`;
      default:
        return '';
    }
  });

  protected readonly sendLabel = computed(() => {
    const request = this.request();
    switch (request?.kind) {
      case undefined:
        return '';
      case 'complete':
        return 'Done';
      case 'reopen':
        return 'Reopen';
      case 'move':
        return request.move.label;
    }
  });

  protected readonly canSend = computed(() => {
    if (!this.request() || this.busy()) {
      return false;
    }
    if (this.input() === 'block' && this.blockKind() === 'ticket' && !this.blockTicket().trim()) {
      return false;
    }
    if (this.override() && this.offersOverride() && this.overrideReason().trim() === '') {
      return false;
    }
    return this.input() === 'none' || this.text().trim() !== '';
  });

  constructor() {
    keepOpenWhile(() => this.busy());
  }

  protected cancel(): void {
    if (!this.busy()) {
      this.closed.emit(false);
    }
  }

  protected async send(): Promise<void> {
    const request = this.request();
    if (!request || !this.canSend()) {
      return;
    }
    const key = this.ticket().key;
    this.busy.set(true);
    this.error.set('');
    try {
      if (request.kind === 'move') {
        await this.actions.transition(key, this.transition(request.move));
      } else {
        await this.actions.update(key, this.stageWrite(request));
      }
      this.closed.emit(true);
    } catch (error) {
      this.refused(error);
    } finally {
      this.busy.set(false);
    }
  }

  private transition(move: Move): Omit<Transition, 'from'> {
    const text = this.text().trim();
    return {
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
      ...this.overriding(),
    };
  }

  private stageWrite(request: Exclude<MoveRequest, { kind: 'move' }>): TicketPatch {
    const text = this.text().trim();
    return {
      ...request.patch,
      ...(request.kind === 'complete' ? { note: text } : { reason: text }),
      ...this.overriding(),
    };
  }

  /** The override of open prerequisites, with its own reason, where the person chose it. */
  private overriding(): Pick<Transition, 'override_prerequisites' | 'reason'> {
    return this.override() && this.offersOverride()
      ? { override_prerequisites: true, reason: this.overrideReason().trim() }
      : {};
  }

  private refused(error: unknown): void {
    if (error instanceof StaleWrite) {
      this.error.set(
        `${this.shortKey()} was changed by someone else meanwhile. Check what it shows now, and send again to write yours over it.`,
      );
      return;
    }
    const problem = this.problems.read(error);
    if (problem.code === 'open_prerequisites') {
      this.refusedOverPrerequisites.set(true);
    }
    this.error.set(problem.detail || problem.title);
  }

  private shortKey(): string {
    const key = this.ticket().key;
    return key.slice(key.indexOf('/') + 1);
  }
}
