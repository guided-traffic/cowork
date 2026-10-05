import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ConfirmationService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Textarea } from 'primeng/textarea';
import { Tooltip } from 'primeng/tooltip';
import { Attachment, Comment, CommentRevision, Ticket } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { TicketRecords } from '../../core/ticket-records.service';
import { AgentMark } from '../../shared/agent-mark';
import { ConflictNote } from '../../shared/conflict-note';
import { MentionList } from '../../shared/mention-list';
import { Mentionable, mentionCandidates, mentionsIn } from '../../shared/mentions';
import { RenderedText } from '../../shared/rendered-text';
import { ago, Clock, dateTime } from '../../shared/time';
import { FilePreview } from './file-preview';
import { fileIcon } from './records-cards';
import { UploadKey } from '../../shared/upload-key';

/**
 * One comment of the thread (docs/adr/0015): its author, the agent or the token it came through,
 * its text as the server rendered it (docs/adr/0011 D6), as text where the answer has no rendering —
 * or that it was withdrawn — and its files (docs/adr/0016 D1), a raster image with its
 * preview. Its author edits it, over the version the editing began with (docs/adr/0050 D3), and
 * attaches files to it; its author or a tenant administrator withdraws it, which hides the text
 * from everybody and keeps the entry (D3); an edited comment shows its earlier texts on request.
 * The page offers what the person may do and the server still decides. The component belongs to
 * one comment: the thread of another ticket makes its own.
 */
@Component({
  selector: 'app-comment',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    AgentMark,
    ButtonDirective,
    ConflictNote,
    FilePreview,
    FormsModule,
    MentionList,
    RenderedText,
    Textarea,
    Tooltip,
  ],
  template: `
    <p class="meta muted">
      <strong>{{ comment().author.display_name }}</strong>
      @if (comment().agent || comment().token) {
        <app-agent-mark [mark]="comment().agent" [token]="comment().token" />
      }
      · <span [pTooltip]="dateTime(comment().created_at)">{{ ago(comment().created_at) }}</span>
      @if (comment().edited && !comment().withdrawn) {
        ·
        <button
          type="button"
          class="link"
          [attr.aria-expanded]="revisions() !== null"
          [attr.data-testid]="'comment-revisions-' + comment().id"
          (click)="toggleRevisions()"
        >
          edited
        </button>
      }
    </p>
    @if (comment().withdrawn) {
      <p class="muted">withdrawn</p>
    } @else if (since()) {
      <form class="edit" ngNoForm novalidate (submit)="$event.preventDefault(); save()">
        <textarea
          #box
          pTextarea
          name="comment"
          rows="4"
          maxlength="100000"
          aria-label="Comment, in Markdown"
          [attr.data-testid]="'comment-input-' + comment().id"
          [ngModel]="draft()"
          (ngModelChange)="draft.set($event)"
        ></textarea>
        <app-mention-list [for]="box" [candidates]="candidates()" (picked)="pick($event)" />
        @if (conflict()) {
          <app-conflict-note
            what="The comment"
            [busy]="busy()"
            (overwrite)="overwrite()"
            (takeTheirs)="takeTheirs()"
          />
        }
        <div class="actions">
          <button
            pButton
            type="button"
            size="small"
            severity="secondary"
            [text]="true"
            [disabled]="busy()"
            (click)="since.set(null)"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            size="small"
            [attr.data-testid]="'comment-save-' + comment().id"
            [disabled]="!draft().trim() || busy() || conflict()"
          >
            @if (busy()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Save
          </button>
        </div>
      </form>
    } @else if (comment().body_html; as html) {
      <app-rendered-text
        class="text"
        [attr.data-testid]="'comment-text-' + comment().id"
        [html]="html"
      />
    } @else {
      <p class="text plain">{{ comment().body }}</p>
    }
    @if (files().length > 0) {
      <ul class="files">
        @for (file of files(); track file.id) {
          <li [attr.data-testid]="'comment-file-' + file.id">
            <i [class]="icon(file)"></i>
            <a [href]="file.content_url" target="_blank" rel="noopener">{{ file.file_name }}</a>
            <app-file-preview class="shown" [file]="file" />
          </li>
        }
      </ul>
    }
    @if (revisions(); as earlier) {
      <ol class="revisions" [attr.data-testid]="'comment-revision-list-' + comment().id">
        @for (revision of earlier; track $index) {
          <li>
            <p class="meta muted">
              replaced by {{ revision.edited_by.display_name }}
              @if (revision.agent || revision.token) {
                <app-agent-mark [mark]="revision.agent" [token]="revision.token" />
              }
              · <span [pTooltip]="dateTime(revision.at)">{{ ago(revision.at) }}</span>
            </p>
            <p class="text muted">{{ revision.body }}</p>
          </li>
        } @empty {
          <li class="muted small">No earlier text.</li>
        }
      </ol>
    }
    @if (!comment().withdrawn && !since() && (mine() || administers())) {
      <div class="row-actions">
        @if (mine()) {
          <button
            pButton
            type="button"
            size="small"
            severity="secondary"
            [text]="true"
            [attr.data-testid]="'comment-edit-' + comment().id"
            (click)="edit()"
          >
            <i class="pi pi-pencil"></i>Edit
          </button>
          <input
            #picker
            type="file"
            class="picker"
            [attr.data-testid]="'comment-attach-input-' + comment().id"
            (change)="attach(picker)"
          />
          <button
            pButton
            type="button"
            size="small"
            severity="secondary"
            [text]="true"
            [disabled]="busy()"
            [attr.data-testid]="'comment-attach-' + comment().id"
            (click)="picker.click()"
          >
            <i class="pi pi-paperclip"></i>Attach a file
          </button>
        }
        <button
          pButton
          type="button"
          size="small"
          severity="secondary"
          [text]="true"
          [disabled]="busy()"
          [attr.data-testid]="'comment-withdraw-' + comment().id"
          (click)="withdraw()"
        >
          <i class="pi pi-eye-slash"></i>Withdraw
        </button>
      </div>
    }
  `,
  styles: `
    p {
      margin: 0.125rem 0;
    }
    .meta {
      font-size: 0.75rem;
    }
    p.text {
      white-space: pre-wrap;
    }
    .link {
      padding: 0;
      border: 0;
      background: none;
      color: var(--p-primary-color);
      font: inherit;
      cursor: pointer;
    }
    .edit {
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
      margin-top: 0.25rem;
      textarea {
        width: 100%;
      }
    }
    .actions,
    .row-actions {
      display: flex;
      gap: 0.25rem;
    }
    .actions {
      justify-content: flex-end;
    }
    .files {
      margin: 0.375rem 0 0;
      padding: 0;
      list-style: none;
      font-size: 0.8125rem;
      li {
        display: grid;
        grid-template-columns: auto 1fr;
        column-gap: 0.5rem;
        padding: 0.125rem 0;
      }
      .pi {
        color: var(--p-text-muted-color);
        margin-top: 0.125rem;
      }
      .shown {
        grid-column: 2;
      }
    }
    .revisions {
      margin: 0.5rem 0 0;
      padding-left: 0.875rem;
      border-left: 2px solid var(--p-app-border);
      list-style: none;
    }
    .picker {
      display: none;
    }
    .small {
      font-size: 0.75rem;
    }
  `,
})
export class CommentItem {
  readonly ticketKey = input.required<string>();
  readonly comment = input.required<Comment>();
  /** The comment's own files, among the ticket's. */
  readonly files = input<Attachment[]>([]);
  /** The person who reads the page, who may change their own comments. */
  readonly me = input<string | undefined>();
  /** The person is an administrator of the tenant, who withdraws any comment. */
  readonly administers = input(false);
  /** The ticket, which decides who can be mentioned: who sees it. */
  readonly ticket = input<Ticket | undefined>();

  private readonly conversation = inject(Conversation);
  private readonly records = inject(TicketRecords);
  private readonly problems = inject(ProblemService);
  /** The key of an upload whose answer did not come, for the same file picked again. */
  private readonly uploadKey = new UploadKey();
  private readonly confirm = inject(ConfirmationService);
  private readonly clock = inject(Clock);
  private readonly members = inject(MembersService);

  protected readonly mine = computed(() => this.comment().author.id === this.me());
  /** The comment as the editing began; null while it is not edited. */
  protected readonly since = signal<Comment | null>(null);
  protected readonly conflict = signal(false);
  protected readonly draft = signal('');
  protected readonly busy = signal(false);
  /** The earlier texts while they are shown; null while they are not. */
  protected readonly revisions = signal<CommentRevision[] | null>(null);
  /** The persons the edit picked to mention, beside those the comment mentions already. */
  protected readonly picked = signal<Mentionable[]>([]);
  protected readonly candidates = computed(() =>
    mentionCandidates(this.members.list(), this.ticket(), this.me()),
  );

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  protected dateTime(iso: string): string {
    return dateTime(iso);
  }

  protected icon(file: Attachment): string {
    return fileIcon(file.content_type);
  }

  protected edit(): void {
    this.draft.set(this.comment().body ?? '');
    this.conflict.set(false);
    this.picked.set([]);
    this.since.set(this.comment());
  }

  protected pick(person: Mentionable): void {
    if (!this.picked().some((each) => each.id === person.id)) {
      this.picked.update((held) => [...held, person]);
    }
  }

  /**
   * The mentions the edit sends (docs/adr/0015 D5): those the comment holds, but one whose `@<name>`
   * the text held and the edit took out, and the persons picked whose name the text holds. A
   * mention the text never named by the member's name — one an agent wrote — stays.
   */
  private mentionsAfter(since: Comment, body: string): string[] {
    const before = since.body ?? '';
    const names = new Map(this.members.list().map((m) => [m.person.id, m.person.display_name]));
    const kept = (since.mentions ?? []).filter((id) => {
      const name = names.get(id);
      return !name || !before.includes(`@${name}`) || body.includes(`@${name}`);
    });
    return [...new Set([...kept, ...mentionsIn(body, this.picked())])];
  }

  protected async save(): Promise<void> {
    const since = this.since();
    const body = this.draft().trim();
    if (!since || !body || this.busy() || this.conflict()) {
      return;
    }
    if (body === since.body) {
      this.since.set(null);
      return;
    }
    this.busy.set(true);
    try {
      await this.conversation.editComment(
        this.ticketKey(),
        since,
        body,
        this.mentionsAfter(since, body),
      );
      if (this.since() === since) {
        this.since.set(null);
        this.revisions.set(null);
      }
    } catch (error) {
      if (error instanceof HttpErrorResponse && error.status === 412) {
        this.conflict.set(true);
      } else {
        this.problems.report(error);
      }
    } finally {
      this.busy.set(false);
    }
  }

  /** The person's text goes over the comment as it is now, which its event has brought. */
  protected overwrite(): void {
    this.conflict.set(false);
    this.since.set(this.comment());
    void this.save();
  }

  /** The editor goes on from the comment as it is now. */
  protected takeTheirs(): void {
    this.conflict.set(false);
    this.draft.set(this.comment().body ?? '');
    this.since.set(this.comment());
  }

  protected async toggleRevisions(): Promise<void> {
    if (this.revisions() !== null) {
      this.revisions.set(null);
      return;
    }
    const comment = this.comment();
    try {
      this.revisions.set(await this.conversation.commentRevisions(this.ticketKey(), comment));
    } catch (error) {
      this.problems.report(error);
    }
  }

  /** Asks first: a withdrawn comment's text is gone for everybody, and there is no way back. */
  protected withdraw(): void {
    const key = this.ticketKey();
    const comment = this.comment();
    this.confirm.confirm({
      header: 'Withdraw the comment',
      message: this.mine()
        ? 'Its text is hidden from everybody, here, in its history and in the activity; the entry stays, marked withdrawn. This cannot be undone.'
        : `The comment of ${comment.author.display_name} is hidden from everybody, here, in its history and in the activity; the entry stays, marked withdrawn, and the activity names you. This cannot be undone.`,
      acceptLabel: 'Withdraw',
      rejectLabel: 'Keep it',
      accept: () => void this.guard(() => this.conversation.withdrawComment(key, comment)),
    });
  }

  /**
   * Uploads a file picked to the comment, with the key of an upload of the same file to the same
   * comment whose answer did not come (`UploadKey`).
   */
  protected async attach(picker: HTMLInputElement): Promise<void> {
    const file = picker.files?.[0];
    picker.value = '';
    if (!file) {
      return;
    }
    const ticket = this.ticketKey();
    const comment = this.comment().id;
    const key = this.uploadKey.for(`${ticket}\n${comment}`, file);
    if (await this.guard(() => this.records.attach(ticket, file, key, comment))) {
      this.uploadKey.answered();
    }
  }

  /** Runs a write, reports its problem, and says whether it went through. */
  private async guard(write: () => Promise<unknown>): Promise<boolean> {
    this.busy.set(true);
    try {
      await write();
      return true;
    } catch (error) {
      this.problems.report(error);
      return false;
    } finally {
      this.busy.set(false);
    }
  }
}
