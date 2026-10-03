import {
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  inject,
  input,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ConfirmationService } from 'primeng/api';
import { Select } from 'primeng/select';
import { SelectButton } from 'primeng/selectbutton';
import { Slider } from 'primeng/slider';
import { Textarea } from 'primeng/textarea';
import { Tooltip } from 'primeng/tooltip';
import { Effort, SecurityClass, Severity, Ticket, TicketPatch, TicketType } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { StaleWrite, TicketActions } from '../../core/ticket-actions.service';
import { ago, Clock, dateTime } from '../../shared/time';
import { meanings } from '../../shared/vocabulary';

/** A field's value as the merge prompt shows it. */
export function shown(value: unknown): string {
  if (value === null || value === undefined || value === '') {
    return 'nothing';
  }
  return typeof value === 'object' ? JSON.stringify(value) : String(value);
}

/**
 * The editable frontmatter of the detail page (docs/adr/0018 D2): each change is one `PATCH`
 * with the cached `ETag` (docs/adr/0050 D3). A `412` asks the person: the server's value, theirs,
 * and whether to write theirs over it (D5) — the server never merges.
 */
@Component({
  selector: 'app-ticket-fields',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, Select, SelectButton, Slider, Textarea, Tooltip],
  templateUrl: './ticket-fields.html',
  styleUrl: './ticket-fields.scss',
})
export class TicketFields {
  readonly ticket = input.required<Ticket>();

  private readonly actions = inject(TicketActions);
  private readonly problems = inject(ProblemService);
  private readonly confirm = inject(ConfirmationService);
  private readonly clock = inject(Clock);
  protected readonly members = inject(MembersService);
  protected readonly meanings = meanings;

  protected readonly types: TicketType[] = ['task', 'bug', 'feature', 'decision', 'question'];
  protected readonly severities: Severity[] = ['critical', 'high', 'medium', 'low', 'cosmetic'];
  protected readonly securities: SecurityClass[] = ['none', 'hardening', 'boundary', 'live'];
  protected readonly efforts: Effort[] = ['XS', 'S', 'M', 'L'];
  protected readonly people = computed(() =>
    this.members
      .list()
      .map((member) => ({ id: member.person.id, name: member.person.display_name })),
  );

  /** The security class waiting for its threat before it is written. */
  protected readonly pendingSecurity = signal<SecurityClass | null>(null);
  protected readonly savingSecurity = signal(false);
  protected readonly threat = signal('');
  protected readonly progress = signal<number | null>(null);

  protected readonly progressShown = computed(() => this.progress() ?? this.ticket().progress);
  private progressTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    inject(DestroyRef).onDestroy(() => this.flushProgress());
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

  /** A drag or the arrow keys move the slider; the value is written once it rests for 400 ms. */
  protected moveProgress(value: number): void {
    this.progress.set(value);
    if (this.progressTimer) {
      clearTimeout(this.progressTimer);
    }
    this.progressTimer = setTimeout(() => this.flushProgress(), 400);
  }

  private flushProgress(): void {
    if (this.progressTimer) {
      clearTimeout(this.progressTimer);
      this.progressTimer = null;
    }
    const value = this.progress();
    this.progress.set(null);
    if (value !== null && value !== this.ticket().progress) {
      void this.write({ progress: value });
    }
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
