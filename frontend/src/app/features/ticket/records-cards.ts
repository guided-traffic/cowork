import { HttpErrorResponse } from '@angular/common/http';
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
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { Tooltip } from 'primeng/tooltip';
import { Attachment, TimeEntry, TimeEntryPatch, TimeEntryRevision } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TicketRecords } from '../../core/ticket-records.service';
import { AgentMark } from '../../shared/agent-mark';
import { ConflictNote } from '../../shared/conflict-note';
import { ago, Clock, duration, parseDuration, size, today } from '../../shared/time';
import { FilePreview } from './file-preview';
import { TicketRelations } from './ticket-relations';
import { UploadKey } from '../../shared/upload-key';

/** The icon of a file by its content type. */
export function fileIcon(contentType: string): string {
  if (contentType.startsWith('image/')) {
    return 'pi pi-image';
  }
  return contentType === 'application/pdf' ? 'pi pi-file-pdf' : 'pi pi-file';
}

/**
 * The ticket's files (docs/adr/0016), those of its comments among them: each one downloads through
 * the backend, a raster image shows a preview (D5), and a new one is uploaded from here; the
 * server's refusals — size, count, type — come back as toasts.
 */
@Component({
  selector: 'app-attachments-card',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [AgentMark, ButtonDirective, FilePreview, Tooltip],
  template: `
    <h2>Attachments</h2>
    @if (relations.attachments.hasValue()) {
      <ul class="files">
        @for (file of files(); track file.id) {
          <li [attr.data-testid]="'attachment-' + file.id">
            <i [class]="icon(file)"></i>
            <a
              [href]="file.content_url"
              target="_blank"
              rel="noopener"
              [pTooltip]="file.sha256"
              [showDelay]="600"
              >{{ file.file_name }}</a
            >
            <span class="muted small"
              >{{ bytes(file) }} · {{ file.uploaded_by.display_name }}
              @if (file.agent || file.token) {
                <app-agent-mark [mark]="file.agent" [token]="file.token" />
              }
              · {{ when(file.created_at) }}{{ file.comment ? ' · on a comment' : '' }}</span
            >
            <app-file-preview class="shown" [file]="file" />
          </li>
        } @empty {
          <li class="muted">No files.</li>
        }
      </ul>
    }
    <input
      #picker
      type="file"
      class="picker"
      (change)="upload(picker)"
      data-testid="attach-input"
    />
    <button
      pButton
      type="button"
      [text]="true"
      size="small"
      data-testid="attach"
      (click)="picker.click()"
      [disabled]="busy()"
    >
      @if (busy()) {
        <i class="pi pi-spinner pi-spin"></i>
      } @else {
        <i class="pi pi-paperclip"></i>
      }
      Attach a file
    </button>
  `,
  styles: `
    .files {
      margin: 0 0 0.5rem;
      padding: 0;
      list-style: none;
      li {
        display: grid;
        grid-template-columns: auto 1fr;
        column-gap: 0.5rem;
        padding: 0.375rem 0;
        font-size: 0.8125rem;
      }
      .pi {
        grid-row: span 2;
        margin-top: 0.125rem;
        color: var(--p-text-muted-color);
      }
      a {
        overflow: hidden;
        white-space: nowrap;
        text-overflow: ellipsis;
      }
      .shown {
        grid-column: 2;
      }
    }
    .small {
      font-size: 0.75rem;
    }
    .picker {
      display: none;
    }
  `,
})
export class AttachmentsCard {
  readonly ticketKey = input.required<string>();
  protected readonly relations = inject(TicketRelations);
  private readonly records = inject(TicketRecords);
  private readonly problems = inject(ProblemService);
  private readonly clock = inject(Clock);
  protected readonly busy = signal(false);
  protected readonly files = computed<Attachment[]>(() =>
    this.relations.attachments.hasValue() ? this.relations.attachments.value().items : [],
  );

  protected icon(file: Attachment): string {
    return fileIcon(file.content_type);
  }

  protected bytes(file: Attachment): string {
    return size(file.size);
  }

  protected when(iso: string): string {
    return ago(iso, this.clock.now());
  }

  /** The key of an upload whose answer did not come, for the same file picked again. */
  private readonly uploadKey = new UploadKey();

  protected async upload(picker: HTMLInputElement): Promise<void> {
    const file = picker.files?.[0];
    picker.value = '';
    if (!file) {
      return;
    }
    const ticket = this.ticketKey();
    this.busy.set(true);
    try {
      await this.records.attach(ticket, file, this.uploadKey.for(ticket, file));
      this.uploadKey.answered();
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.busy.set(false);
    }
  }
}

/** An entry being corrected: the entry as the correction began, and what the form holds. */
interface Correction {
  since: TimeEntry;
  day: string;
  text: string;
  note: string;
  /** The entry changed meanwhile, and the person decides. */
  conflict: boolean;
}

/**
 * The time booked on the ticket (docs/adr/0017): the sum, each entry — marked when a token booked
 * it (docs/adr/0036 D6) — and the person's own booking, a day and a duration as people type it
 * (`1:30`, `1h 30m`, `90`). The author corrects an entry in its row — over the version the
 * correction began with, `If-Match` (docs/adr/0050 D3) — or voids it; an entry that was corrected
 * shows its previous values on request (D7). What the forms hold belongs to the ticket they were
 * typed on.
 */
@Component({
  selector: 'app-time-card',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [AgentMark, ButtonDirective, ConflictNote, FormsModule, InputText, Tooltip],
  template: `
    <h2>
      Time
      @if (relations.time.hasValue()) {
        <span class="muted total" data-testid="time-total">{{ total() }}</span>
      }
    </h2>
    @if (relations.time.hasValue()) {
      <ul class="entries">
        @for (entry of entries(); track entry.id) {
          <li [attr.data-testid]="'time-' + entry.id">
            @let correcting = correction()?.since?.id === entry.id ? correction() : null;
            @if (correcting) {
              <form
                class="correct"
                ngNoForm
                novalidate
                (submit)="$event.preventDefault(); saveCorrection()"
              >
                <input
                  pInputText
                  type="date"
                  name="day"
                  aria-label="Day"
                  [attr.data-testid]="'correct-day-' + entry.id"
                  [ngModel]="correcting.day"
                  (ngModelChange)="correct({ day: $event })"
                />
                <input
                  pInputText
                  name="duration"
                  aria-label="Duration"
                  [attr.data-testid]="'correct-duration-' + entry.id"
                  [ngModel]="correcting.text"
                  (ngModelChange)="correct({ text: $event })"
                />
                <input
                  pInputText
                  name="note"
                  aria-label="Note"
                  class="note-input"
                  [attr.data-testid]="'correct-note-' + entry.id"
                  [ngModel]="correcting.note"
                  (ngModelChange)="correct({ note: $event })"
                />
                @if (correcting.conflict) {
                  <app-conflict-note
                    class="note-input"
                    what="The entry"
                    [busy]="busy()"
                    (overwrite)="overwrite()"
                    (takeTheirs)="takeTheirs()"
                  />
                }
                <span class="note-input actions">
                  <button
                    pButton
                    type="button"
                    size="small"
                    severity="secondary"
                    [text]="true"
                    [disabled]="busy()"
                    (click)="correction.set(null)"
                  >
                    Cancel
                  </button>
                  <button
                    pButton
                    type="submit"
                    size="small"
                    [attr.data-testid]="'correct-save-' + entry.id"
                    [disabled]="parse(correcting.text) === null || busy() || correcting.conflict"
                  >
                    Save
                  </button>
                </span>
              </form>
            } @else {
              <span class="tabular">{{ duration(entry.minutes) }}</span>
              <span class="muted small"
                >{{ entry.person.display_name }}
                @if (entry.token) {
                  <app-agent-mark [token]="entry.token" />
                }
                · {{ entry.day }}
                @if (entry.edited) {
                  ·
                  <button
                    type="button"
                    class="link"
                    [attr.aria-expanded]="revisionsOf() === entry.id"
                    [attr.data-testid]="'time-revisions-' + entry.id"
                    (click)="toggleRevisions(entry)"
                  >
                    corrected
                  </button>
                }
              </span>
              @if (entry.person.id === me()) {
                <span class="row-actions">
                  <button
                    pButton
                    type="button"
                    [text]="true"
                    [rounded]="true"
                    size="small"
                    severity="secondary"
                    pTooltip="Correct this entry"
                    [attr.data-testid]="'correct-' + entry.id"
                    [iconOnly]="true"
                    [disabled]="busy()"
                    (click)="startCorrection(entry)"
                    aria-label="Correct this entry"
                  >
                    <i class="pi pi-pencil"></i>
                  </button>
                  <button
                    pButton
                    type="button"
                    [text]="true"
                    [rounded]="true"
                    size="small"
                    severity="secondary"
                    pTooltip="Void this entry"
                    [attr.data-testid]="'void-' + entry.id"
                    [iconOnly]="true"
                    [disabled]="busy()"
                    (click)="voidEntry(entry)"
                    aria-label="Void this entry"
                  >
                    <i class="pi pi-times"></i>
                  </button>
                </span>
              }
              @if (entry.note) {
                <span class="note small">{{ entry.note }}</span>
              }
              @if (revisionsOf() === entry.id) {
                <ol class="revisions note" [attr.data-testid]="'time-revision-list-' + entry.id">
                  @for (revision of revisions(); track $index) {
                    <li class="small">
                      <span class="tabular">{{ duration(revision.minutes) }}</span> on
                      {{ revision.day }}{{ revision.note ? ' — ' + revision.note : '' }}
                      <span class="muted"
                        >· changed by {{ revision.edited_by.display_name }}
                        @if (revision.token) {
                          <app-agent-mark [token]="revision.token" />
                        }
                        · {{ when(revision.at) }}</span
                      >
                    </li>
                  } @empty {
                    <li class="muted small">Loading the earlier values…</li>
                  }
                </ol>
              }
            }
          </li>
        } @empty {
          <li class="muted">No time booked.</li>
        }
      </ul>
    }
    <form class="book" (ngSubmit)="book()">
      <input
        pInputText
        type="date"
        name="day"
        [ngModel]="day()"
        (ngModelChange)="day.set($event)"
        aria-label="Day"
        data-testid="time-day"
      />
      <input
        pInputText
        name="duration"
        placeholder="1:30"
        [ngModel]="text()"
        (ngModelChange)="text.set($event)"
        aria-label="Duration"
        data-testid="time-duration"
      />
      <input
        pInputText
        name="note"
        placeholder="What for"
        [ngModel]="note()"
        (ngModelChange)="note.set($event)"
        aria-label="Note"
        class="note-input"
        data-testid="time-note"
      />
      <button
        pButton
        type="submit"
        size="small"
        data-testid="time-book"
        [disabled]="minutes() === null || busy()"
      >
        @if (busy()) {
          <i class="pi pi-spinner pi-spin"></i>
        }
        Book
      </button>
    </form>
  `,
  styles: `
    h2 .total {
      font-weight: 400;
      margin-left: 0.25rem;
    }
    .entries {
      margin: 0 0 0.75rem;
      padding: 0;
      list-style: none;
      > li {
        display: grid;
        grid-template-columns: 5rem 1fr auto;
        align-items: center;
        column-gap: 0.5rem;
        padding: 0.25rem 0;
        font-size: 0.8125rem;
      }
      .note {
        grid-column: 2 / 4;
      }
    }
    .row-actions {
      display: flex;
    }
    .revisions {
      margin: 0.25rem 0 0;
      padding-left: 1rem;
    }
    .correct {
      grid-column: 1 / 4;
      display: grid;
      grid-template-columns: 1fr 5rem;
      gap: 0.5rem;
      input {
        min-width: 0;
      }
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.25rem;
    }
    .link {
      padding: 0;
      border: 0;
      background: none;
      color: var(--p-primary-color);
      font: inherit;
      cursor: pointer;
    }
    .small {
      font-size: 0.75rem;
    }
    .book {
      display: grid;
      grid-template-columns: 1fr 5rem;
      gap: 0.5rem;
      p-button {
        grid-column: 2;
        justify-self: end;
      }
      input {
        min-width: 0;
      }
    }
    .note-input {
      grid-column: 1 / 3;
    }
  `,
})
export class TimeCard {
  readonly ticketKey = input.required<string>();
  private readonly clock = inject(Clock);
  protected readonly relations = inject(TicketRelations);
  private readonly records = inject(TicketRecords);
  private readonly problems = inject(ProblemService);
  private readonly session = inject(SessionService);

  protected readonly me = computed(() => this.session.person()?.id);
  protected readonly entries = computed<TimeEntry[]>(() =>
    this.relations.time.hasValue()
      ? this.relations.time.value().items.filter((entry) => !entry.voided)
      : [],
  );
  protected readonly total = computed(() =>
    duration(this.relations.time.hasValue() ? (this.relations.time.value().total_minutes ?? 0) : 0),
  );
  /** Today, moving on at midnight; the person's own choice of day holds until then. */
  private readonly currentDay = computed(() => today(new Date(this.clock.now())));
  protected readonly day = linkedSignal(() => this.currentDay());
  protected readonly text = linkedSignal({ source: this.ticketKey, computation: () => '' });
  protected readonly note = linkedSignal({ source: this.ticketKey, computation: () => '' });
  protected readonly minutes = computed(() => parseDuration(this.text()));
  protected readonly busy = signal(false);
  protected readonly duration = duration;
  protected readonly parse = parseDuration;

  /** The entry being corrected; another ticket drops it. */
  protected readonly correction = linkedSignal<string, Correction | null>({
    source: this.ticketKey,
    computation: () => null,
  });
  /** The entry whose earlier values are shown, and those values. */
  protected readonly revisionsOf = linkedSignal<string, string | null>({
    source: this.ticketKey,
    computation: () => null,
  });
  protected readonly revisions = signal<TimeEntryRevision[]>([]);

  protected when(iso: string): string {
    return ago(iso, this.clock.now());
  }

  /**
   * The Idempotency-Key of the booking this form is making: one for each ticket, day, duration and
   * note, so a retry of a lost answer is answered again instead of booking the time twice; any
   * change, and a booking made, make a new one (docs/adr/0045 D3).
   */
  private readonly key = linkedSignal(() => {
    this.ticketKey();
    this.day();
    this.minutes();
    this.note();
    return crypto.randomUUID();
  });

  protected async book(): Promise<void> {
    const minutes = this.minutes();
    const key = this.ticketKey();
    const idempotencyKey = this.key();
    if (minutes === null) {
      return;
    }
    if (
      (await this.write(() =>
        this.records.book(key, this.day(), minutes, this.note(), idempotencyKey),
      )) &&
      this.ticketKey() === key
    ) {
      this.text.set('');
      this.note.set('');
    }
  }

  protected async voidEntry(entry: TimeEntry): Promise<void> {
    await this.write(() => this.records.void(this.ticketKey(), entry));
  }

  protected startCorrection(entry: TimeEntry): void {
    this.correction.set({
      since: entry,
      day: entry.day,
      text: duration(entry.minutes),
      note: entry.note,
      conflict: false,
    });
  }

  protected correct(change: Partial<Pick<Correction, 'day' | 'text' | 'note'>>): void {
    this.correction.update((correction) => (correction ? { ...correction, ...change } : null));
  }

  /** Writes what changed over the version the correction began with. */
  protected async saveCorrection(): Promise<void> {
    const correction = this.correction();
    const minutes = correction ? parseDuration(correction.text) : null;
    if (!correction || minutes === null || this.busy() || correction.conflict) {
      return;
    }
    const { since } = correction;
    const patch: TimeEntryPatch = {
      ...(correction.day !== since.day ? { day: correction.day } : {}),
      ...(minutes !== since.minutes ? { minutes } : {}),
      ...(correction.note.trim() !== since.note ? { note: correction.note.trim() } : {}),
    };
    if (Object.keys(patch).length === 0) {
      this.correction.set(null);
      return;
    }
    const key = this.ticketKey();
    this.busy.set(true);
    try {
      await this.records.edit(key, since, patch);
      this.relations.reloadTime();
      if (this.correction() === correction) {
        this.correction.set(null);
      }
    } catch (error) {
      if (error instanceof HttpErrorResponse && error.status === 412) {
        // Time entries are not published (docs/adr/0054 D4): the list is loaded again here.
        this.relations.reloadTime();
        if (this.correction() === correction) {
          this.correction.set({ ...correction, conflict: true });
        }
      } else {
        this.problems.report(error);
      }
    } finally {
      this.busy.set(false);
    }
  }

  /** The entry as the list holds it now, after the reload a conflict started. */
  private latest(id: string): TimeEntry | undefined {
    return this.relations.time.hasValue()
      ? this.relations.time.value().items.find((entry) => entry.id === id)
      : undefined;
  }

  /** The person's values go over the entry as it is now. */
  protected overwrite(): void {
    const correction = this.correction();
    const latest = correction && this.latest(correction.since.id);
    if (correction && latest) {
      this.correction.set({ ...correction, since: latest, conflict: false });
      void this.saveCorrection();
    }
  }

  /** The form goes on from the entry as it is now. */
  protected takeTheirs(): void {
    const correction = this.correction();
    const latest = correction && this.latest(correction.since.id);
    if (latest) {
      this.startCorrection(latest);
    }
  }

  protected async toggleRevisions(entry: TimeEntry): Promise<void> {
    if (this.revisionsOf() === entry.id) {
      this.revisionsOf.set(null);
      return;
    }
    const key = this.ticketKey();
    this.revisions.set([]);
    this.revisionsOf.set(entry.id);
    try {
      const revisions = await this.records.revisions(key, entry);
      if (this.revisionsOf() === entry.id && this.ticketKey() === key) {
        this.revisions.set(revisions);
      }
    } catch (error) {
      this.revisionsOf.set(null);
      this.problems.report(error);
    }
  }

  private async write(call: () => Promise<unknown>): Promise<boolean> {
    this.busy.set(true);
    try {
      await call();
      this.relations.reloadTime();
      return true;
    } catch (error) {
      this.problems.report(error);
      return false;
    } finally {
      this.busy.set(false);
    }
  }
}
