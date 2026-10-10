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
  linkedSignal,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ConfirmationService } from 'primeng/api';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { SelectButton } from 'primeng/selectbutton';
import { Slider } from 'primeng/slider';
import { Textarea } from 'primeng/textarea';
import { Tooltip } from 'primeng/tooltip';
import {
  Effort,
  Horizon,
  SecurityClass,
  Severity,
  Ticket,
  TicketPatch,
  TicketType,
} from '../../api/models';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { splitKey } from '../../core/tickets.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { AgentMark } from '../../shared/agent-mark';
import { StageBar } from '../../shared/stage-bar';
import {
  currentStage,
  effectOfStages,
  Stage,
  stageFields,
  stageNames,
  stages,
  Stages,
  stagesOf,
} from '../../shared/stages';
import { ago, Clock, dateTime } from '../../shared/time';
import { meanings } from '../../shared/vocabulary';
import { ConfidentialDialog } from './confidential-dialog';
import { MoveDialog, MoveRequest, StagePatch } from './move-dialog';
import { ParentPicker } from './parent-picker';

/** A field's value as the merge prompt shows it. */
export function shown(value: unknown): string {
  if (value === null || value === undefined || value === '') {
    return 'nothing';
  }
  return typeof value === 'object' ? JSON.stringify(value) : String(value);
}

/** The horizons in their order (docs/adr/0010 D3). */
const horizons: Horizon[] = ['now', 'release', 'next', 'later', 'icebox'];

/**
 * The editable frontmatter of the detail page (docs/adr/0018 D2): each change is one `PATCH`
 * with the cached `ETag` (docs/adr/0050 D3). A `412` asks the person: the server's value, theirs,
 * and whether to write theirs over it (D5) — the server never merges. The three progress stages
 * (docs/adr/0017 D2) each have a slider; a parent shows its stages, which its children make, as
 * bars. A slider move that would fill the last stage is the done act and asks for the verification
 * note first, and one that lowers a stage of a ticket done by its stages reopens it and asks for
 * the reason first (docs/adr/0009 D5), both in {@link MoveDialog}. The horizon is set here as on
 * the backlog, with the reason a person may add afterwards (docs/adr/0010 D3); the parent is chosen
 * from the project's open tickets (docs/adr/0008 D2); a tenant administrator sets and lifts the
 * confidential flag (docs/adr/0065). Every editor here — the threat, the stage dialog, the
 * horizon's reason, the confidential dialog — belongs to the ticket it was opened on and closes when
 * the page turns to another, without writing.
 */
@Component({
  selector: 'app-ticket-fields',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    AgentMark,
    ConfidentialDialog,
    FormsModule,
    InputText,
    MoveDialog,
    ParentPicker,
    Select,
    SelectButton,
    Slider,
    StageBar,
    Textarea,
    Tooltip,
  ],
  templateUrl: './ticket-fields.html',
  styleUrl: './ticket-fields.scss',
})
export class TicketFields {
  readonly ticket = input.required<Ticket>();

  private readonly actions = inject(TicketActions);
  private readonly problems = inject(ProblemService);
  private readonly confirm = inject(ConfirmationService);
  private readonly clock = inject(Clock);
  private readonly session = inject(SessionService);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly injector = inject(Injector);
  protected readonly members = inject(MembersService);
  protected readonly meanings = meanings;
  protected readonly horizons = horizons;

  protected readonly types: TicketType[] = ['task', 'bug', 'feature', 'decision', 'question'];
  protected readonly severities: Severity[] = ['critical', 'high', 'medium', 'low', 'cosmetic'];
  protected readonly securities: SecurityClass[] = ['none', 'hardening', 'boundary', 'live'];
  protected readonly efforts: Effort[] = ['XS', 'S', 'M', 'L'];
  protected readonly people = computed(() =>
    this.members
      .list()
      .map((member) => ({ id: member.person.id, name: member.person.display_name })),
  );

  private readonly key = computed(() => this.ticket().key);
  /** The tenant and the project the ticket is in, for the parent's choice. */
  protected readonly place = computed(() => {
    const { team, key } = splitKey(this.key());
    return { tenant: team, project: key.slice(0, key.lastIndexOf('-')) };
  });
  /** A tenant administrator sets and lifts the confidential flag (docs/adr/0065 D6). */
  protected readonly administers = computed(() => this.session.membership()?.role === 'admin');

  /** The security class waiting for its threat before it is written; another ticket drops it. */
  protected readonly pendingSecurity = linkedSignal<string, SecurityClass | null>({
    source: this.key,
    computation: () => null,
  });
  protected readonly savingSecurity = signal(false);
  protected readonly threat = linkedSignal({ source: this.key, computation: () => '' });

  /** The horizon just set, waiting for the reason the person may add; another ticket drops it. */
  protected readonly horizonAsked = linkedSignal<string, Horizon | null>({
    source: this.key,
    computation: () => null,
  });
  protected readonly horizonReason = linkedSignal({ source: this.key, computation: () => '' });
  protected readonly sendingReason = signal(false);

  /** The confidential dialog is open; another ticket closes it. */
  protected readonly confidentialOpen = linkedSignal({
    source: this.key,
    computation: () => false,
  });

  protected readonly stages = stages;
  protected readonly stageNames = stageNames;
  /**
   * The stages a slider moved and that are not written yet: they wait for the slider to rest, for
   * their write, or for the dialog that asks what the write needs. They belong to the ticket they
   * were moved on; another ticket starts without them, and a newer version of the same keeps them.
   */
  private readonly moved = linkedSignal<string, Partial<Stages>>({
    source: this.key,
    computation: () => ({}),
  });
  protected readonly stagesShown = computed<Stages>(() => ({
    ...stagesOf(this.ticket()),
    ...this.moved(),
  }));
  protected readonly current = computed(() => currentStage(this.ticket()));
  /** The write of the stages that waits for the verification note or the reason; another ticket drops it. */
  protected readonly stageRequest = linkedSignal<string, MoveRequest | null>({
    source: this.key,
    computation: () => null,
  });
  private stageTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    inject(DestroyRef).onDestroy(() => this.flushStages(true));
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  protected dateTime(iso: string): string {
    return dateTime(iso);
  }

  protected setSecurity(security: SecurityClass): void {
    if (security === 'none') {
      this.pendingSecurity.set(null);
      void this.write({ security, threat: null });
    } else {
      this.threat.set(this.ticket().threat ?? '');
      this.pendingSecurity.set(security);
    }
  }

  /**
   * The threat editor closes when the write went through — also when the person writes over a
   * newer version from the prompt — and keeps what was typed when not. While a save is on its way
   * the editor takes no second one, which would only meet its own new version as a `412`.
   */
  protected saveSecurity(): void {
    const security = this.pendingSecurity();
    if (!security || !this.threat().trim() || this.savingSecurity()) {
      return;
    }
    this.savingSecurity.set(true);
    void this.write({ security, threat: this.threat().trim() }, () =>
      this.pendingSecurity.set(null),
    ).finally(() => this.savingSecurity.set(false));
  }

  /**
   * Sets the horizon (docs/adr/0010 D3); `later`, where a ticket nobody placed stands, clears the
   * horizon set. Another horizon then offers the reason, which the person may leave out. A horizon
   * somebody else set meanwhile is asked about, as a field is.
   */
  protected async setHorizon(value: Horizon | null): Promise<void> {
    const ticket = this.ticket();
    if (!value || value === ticket.horizon) {
      return;
    }
    this.horizonAsked.set(null);
    try {
      await this.actions.setHorizon(ticket.key, value);
      if (value !== 'later' && this.key() === ticket.key) {
        this.askReason(value);
      }
    } catch (error) {
      if (error instanceof StaleWrite) {
        this.confirm.confirm({
          header: 'Changed meanwhile',
          message: `Someone set the horizon of this ticket while you chose: now ${error.current.horizon}, yours ${value}. Set yours over it?`,
          acceptLabel: 'Set mine',
          rejectLabel: 'Keep theirs',
          accept: () => void this.setHorizon(value),
        });
        return;
      }
      this.problems.report(error);
    }
  }

  protected horizonMeaning(value: Horizon): string {
    return meanings.horizon[value];
  }

  private askReason(value: Horizon): void {
    this.horizonReason.set('');
    this.horizonAsked.set(value);
    afterNextRender(
      () =>
        this.host.nativeElement
          .querySelector<HTMLInputElement>('[data-testid="field-horizon-reason"]')
          ?.focus(),
      { injector: this.injector },
    );
  }

  /** The reason goes with the same horizon once more; an empty one leaves the horizon as set. */
  protected async sendHorizonReason(): Promise<void> {
    const value = this.horizonAsked();
    const reason = this.horizonReason().trim();
    if (!value || this.sendingReason()) {
      return;
    }
    if (!reason) {
      this.horizonAsked.set(null);
      return;
    }
    const key = this.key();
    this.sendingReason.set(true);
    try {
      await this.actions.setHorizon(key, value, reason);
      if (this.key() === key) {
        this.horizonAsked.set(null);
      }
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.sendingReason.set(false);
    }
  }

  protected dismissHorizonReason(): void {
    if (!this.sendingReason()) {
      this.horizonAsked.set(null);
    }
  }

  /**
   * A drag or the arrow keys move a stage's slider; what the sliders moved is written once they
   * rest for 400 ms, in one `PATCH`.
   */
  protected moveStage(stage: Stage, value: number): void {
    if (this.stageRequest()) {
      return;
    }
    this.moved.update((moved) => ({ ...moved, [stage]: value }));
    if (this.stageTimer) {
      clearTimeout(this.stageTimer);
    }
    this.stageTimer = setTimeout(() => this.flushStages(), 400);
  }

  /**
   * Writes the stages that moved. A write that changes the stages only goes at once; one that
   * closes or reopens the ticket waits for the dialog, and the sliders show it meanwhile. When the
   * page goes away (`leaving`), nobody is left to ask, and such a write is not made.
   */
  private flushStages(leaving = false): void {
    if (this.stageTimer) {
      clearTimeout(this.stageTimer);
      this.stageTimer = null;
    }
    const ticket = this.ticket();
    const before = stagesOf(ticket);
    const moved = this.moved();
    const changed = stages.filter(
      (stage) => moved[stage] !== undefined && moved[stage] !== before[stage],
    );
    const pending: Partial<Stages> = {};
    const patch: StagePatch = {};
    for (const stage of changed) {
      pending[stage] = moved[stage];
      patch[stageFields[stage]] = moved[stage];
    }
    this.moved.set(pending);
    if (changed.length === 0) {
      return;
    }
    const effect = effectOfStages(ticket, { ...before, ...moved });
    if (effect === 'keep') {
      void this.write(patch).finally(() => this.settle(patch));
    } else if (leaving) {
      this.moved.set({});
    } else {
      this.stageRequest.set({ kind: effect, patch });
    }
  }

  /** A written stage follows the ticket again, unless its slider moved on meanwhile. */
  private settle(patch: StagePatch): void {
    this.moved.update((moved) => {
      const rest = { ...moved };
      for (const stage of stages) {
        if (rest[stage] !== undefined && rest[stage] === patch[stageFields[stage]]) {
          delete rest[stage];
        }
      }
      return rest;
    });
  }

  /** The dialog is over: the cache holds what it wrote, or the sliders go back to the ticket. */
  protected stageClosed(): void {
    this.stageRequest.set(null);
    this.moved.set({});
  }

  /**
   * Writes a change and says whether it went through; `done` runs once it has — at once, or after
   * the person chose to write over a newer version at the `412` prompt, which is a write of its own.
   */
  async write(patch: TicketPatch, done?: () => void): Promise<boolean> {
    try {
      await this.actions.update(this.ticket().key, patch);
      done?.();
      return true;
    } catch (error) {
      if (error instanceof StaleWrite) {
        const lines = Object.keys(patch)
          .filter((field) => field !== 'comment')
          .map(
            (field) =>
              `${field}: now ${shown(error.current[field as keyof Ticket])}, yours ${shown(patch[field as keyof TicketPatch])}`,
          );
        this.confirm.confirm({
          header: 'Changed meanwhile',
          message: `Someone changed this ticket while you edited it. ${lines.join('; ')}. Write yours over it?`,
          acceptLabel: 'Write mine',
          rejectLabel: 'Keep theirs',
          accept: () => void this.write(patch, done),
        });
        return false;
      }
      this.problems.report(error);
      return false;
    }
  }
}
