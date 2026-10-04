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
import { Attachment, TimeEntry } from '../../api/models';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TicketRecords } from '../../core/ticket-records.service';
import { AgentMark } from '../../shared/agent-mark';
import { ago, Clock, duration, parseDuration, size, today } from '../../shared/time';
import { TicketRelations } from './ticket-relations';

/** The icon of a file by its content type. */
export function fileIcon(contentType: string): string {
  if (contentType.startsWith('image/')) {
    return 'pi pi-image';
  }
  return contentType === 'application/pdf' ? 'pi pi-file-pdf' : 'pi pi-file';
}

/**
 * The ticket's files (docs/adr/0016): each one downloads through the backend, and a new one is
 * uploaded from here; the server's refusals — size, count, type — come back as toasts.
 */
@Component({
  selector: 'app-attachments-card',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [AgentMark, ButtonDirective, Tooltip],
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
              · {{ when(file.created_at) }}</span
            >
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

  protected async upload(picker: HTMLInputElement): Promise<void> {
    const file = picker.files?.[0];
    picker.value = '';
    if (!file) {
      return;
    }
    this.busy.set(true);
    try {
      await this.records.attach(this.ticketKey(), file);
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.busy.set(false);
    }
  }
}

/**
 * The time booked on the ticket (docs/adr/0017): the sum, each entry — marked when a token booked
 * it (docs/adr/0036 D6) — and the person's own booking, a day and a duration as people type it
 * (`1:30`, `1h 30m`, `90`).
 */
@Component({
  selector: 'app-time-card',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [AgentMark, ButtonDirective, FormsModule, InputText, Tooltip],
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
            <span class="tabular">{{ duration(entry.minutes) }}</span>
            <span class="muted small"
              >{{ entry.person.display_name }}
              @if (entry.token) {
                <app-agent-mark [token]="entry.token" />
              }
              · {{ entry.day }}</span
            >
            @if (entry.note) {
              <span class="note small">{{ entry.note }}</span>
            }
            @if (entry.person.id === me()) {
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
                (click)="voidEntry(entry)"
                aria-label="Void this entry"
              >
                <i class="pi pi-times"></i>
              </button>
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
      li {
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
    .small {
      font-size: 0.75rem;
    }
    .book {
      display: grid;
      grid-template-columns: 1fr 5rem;
      gap: 0.5rem;
      .note-input {
        grid-column: 1 / 3;
      }
      p-button {
        grid-column: 2;
        justify-self: end;
      }
      input {
        min-width: 0;
      }
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
  protected readonly text = signal('');
  protected readonly note = signal('');
  protected readonly minutes = computed(() => parseDuration(this.text()));
  protected readonly busy = signal(false);
  protected readonly duration = duration;

  protected async book(): Promise<void> {
    const minutes = this.minutes();
    if (minutes === null) {
      return;
    }
    if (
      await this.write(() => this.records.book(this.ticketKey(), this.day(), minutes, this.note()))
    ) {
      this.text.set('');
      this.note.set('');
    }
  }

  protected async voidEntry(entry: TimeEntry): Promise<void> {
    await this.write(() => this.records.void(this.ticketKey(), entry));
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
