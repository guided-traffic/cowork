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
import { Select } from 'primeng/select';
import { Textarea } from 'primeng/textarea';
import { LinkType, Question, QuestionPatch } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { ConflictNote } from '../../shared/conflict-note';

/**
 * A field of a form that belongs to its ticket: the page is reused when its path names another
 * ticket, and what was typed for one ticket must not be sent to the next. Call it in the field
 * initialiser of a component with a `ticketKey` input.
 */
function draft<T>(ticketKey: () => string, empty: () => T) {
  return linkedSignal<string, T>({ source: ticketKey, computation: empty });
}

/** Runs a write once at a time and reports its problem; returns whether it succeeded. */
async function guarded(
  busy: ReturnType<typeof signal<boolean>>,
  problems: ProblemService,
  write: () => Promise<unknown>,
): Promise<boolean> {
  busy.set(true);
  try {
    await write();
    return true;
  } catch (error) {
    problems.report(error);
    return false;
  } finally {
    busy.set(false);
  }
}

/**
 * Writes a comment on the ticket (docs/adr/0015); the thread reloads through the event stream. The
 * text belongs to the ticket it was typed on.
 */
@Component({
  selector: 'app-comment-composer',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, Textarea],
  template: `
    <form class="composer" (ngSubmit)="send()">
      <textarea
        pTextarea
        name="comment"
        rows="3"
        placeholder="Write a comment (Markdown)"
        [ngModel]="text()"
        (ngModelChange)="text.set($event)"
        aria-label="Comment"
        data-testid="comment-text"
      ></textarea>
      <button
        pButton
        type="submit"
        size="small"
        data-testid="comment-send"
        [disabled]="!text().trim() || busy()"
      >
        @if (busy()) {
          <i class="pi pi-spinner pi-spin"></i>
        }
        Comment
      </button>
    </form>
  `,
  styles: `
    .composer {
      display: flex;
      flex-direction: column;
      align-items: flex-end;
      gap: 0.5rem;
      margin-top: 0.75rem;
      textarea {
        width: 100%;
      }
    }
  `,
})
export class CommentComposer {
  readonly ticketKey = input.required<string>();
  private readonly conversation = inject(Conversation);
  private readonly problems = inject(ProblemService);
  protected readonly text = draft(this.ticketKey, () => '');
  protected readonly busy = signal(false);

  /**
   * The Idempotency-Key of the comment this form is writing: one for each text and ticket, so a
   * retry of a lost answer is answered again instead of commenting twice; any change, and a comment
   * written, make a new one (docs/adr/0045 D3).
   */
  private readonly idempotencyKey = linkedSignal(() => {
    this.ticketKey();
    this.text();
    return crypto.randomUUID();
  });

  protected async send(): Promise<void> {
    const text = this.text().trim();
    const key = this.ticketKey();
    const idempotencyKey = this.idempotencyKey();
    if (
      text &&
      (await guarded(this.busy, this.problems, () =>
        this.conversation.comment(key, text, idempotencyKey),
      )) &&
      this.ticketKey() === key
    ) {
      this.text.set('');
    }
  }
}

/**
 * Asks a question on the ticket, of a person if one is named (docs/adr/0011 D2). The form belongs
 * to the ticket it was opened on and closes when the page turns to another.
 */
@Component({
  selector: 'app-ask-question',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, Select, Textarea],
  template: `
    @if (open()) {
      <form class="ask" (ngSubmit)="send()">
        <textarea
          pTextarea
          name="question"
          rows="2"
          placeholder="The question"
          [ngModel]="question()"
          (ngModelChange)="question.set($event)"
          aria-label="Question"
          data-testid="ask-question"
        ></textarea>
        <textarea
          pTextarea
          name="options"
          rows="2"
          placeholder="The options, each with its cost"
          [ngModel]="options()"
          (ngModelChange)="options.set($event)"
          aria-label="Options"
        ></textarea>
        <textarea
          pTextarea
          name="recommendation"
          rows="2"
          placeholder="The recommended option and why"
          [ngModel]="recommendation()"
          (ngModelChange)="recommendation.set($event)"
          aria-label="Recommendation"
        ></textarea>
        <p-select
          [options]="people()"
          optionLabel="name"
          optionValue="id"
          [ngModel]="askedOf()"
          (ngModelChange)="askedOf.set($event)"
          name="askedOf"
          placeholder="Asked of nobody in particular"
          [showClear]="true"
          size="small"
          ariaLabel="Asked of"
        />
        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            size="small"
            (click)="open.set(false)"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            size="small"
            data-testid="ask-send"
            [disabled]="!question().trim() || busy()"
          >
            @if (busy()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Ask
          </button>
        </div>
      </form>
    } @else {
      <button
        pButton
        type="button"
        [text]="true"
        size="small"
        data-testid="ask-open"
        (click)="open.set(true)"
      >
        <i class="pi pi-question-circle"></i>Ask a question
      </button>
    }
  `,
  styles: `
    .ask {
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
      margin-top: 0.75rem;
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
  `,
})
export class AskQuestion {
  readonly ticketKey = input.required<string>();
  private readonly conversation = inject(Conversation);
  private readonly problems = inject(ProblemService);
  private readonly members = inject(MembersService);
  protected readonly people = computed(() =>
    this.members.list().map((m) => ({ id: m.person.id, name: m.person.display_name })),
  );
  protected readonly open = draft(this.ticketKey, () => false);
  protected readonly question = draft(this.ticketKey, () => '');
  protected readonly options = draft(this.ticketKey, () => '');
  protected readonly recommendation = draft(this.ticketKey, () => '');
  protected readonly askedOf = draft<string | null>(this.ticketKey, () => null);
  protected readonly busy = signal(false);

  /**
   * The Idempotency-Key of the question this form is asking: one for each content and ticket, so a
   * retry of a lost answer is answered again instead of asking twice; any change, and a question
   * asked, make a new one (docs/adr/0045 D3).
   */
  private readonly idempotencyKey = linkedSignal(() => {
    this.ticketKey();
    this.question();
    this.options();
    this.recommendation();
    this.askedOf();
    return crypto.randomUUID();
  });

  protected async send(): Promise<void> {
    const key = this.ticketKey();
    const idempotencyKey = this.idempotencyKey();
    const ok = await guarded(this.busy, this.problems, () =>
      this.conversation.ask(
        key,
        {
          question: this.question().trim(),
          ...(this.options().trim() ? { options: this.options().trim() } : {}),
          ...(this.recommendation().trim() ? { recommendation: this.recommendation().trim() } : {}),
          ...(this.askedOf() ? { asked_of: this.askedOf() as string } : {}),
        },
        idempotencyKey,
      ),
    );
    if (ok && this.ticketKey() === key) {
      this.question.set('');
      this.options.set('');
      this.recommendation.set('');
      this.askedOf.set(null);
      this.open.set(false);
    }
  }
}

/** Answers an open question, or changes an answer, and withdraws an open one (docs/adr/0011). */
@Component({
  selector: 'app-answer-question',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, Textarea],
  template: `
    @if (editing()) {
      <form class="answer" (ngSubmit)="send()">
        <textarea
          pTextarea
          name="answer"
          rows="2"
          placeholder="The answer"
          [ngModel]="text()"
          (ngModelChange)="text.set($event)"
          aria-label="Answer"
          [attr.data-testid]="'answer-text-' + question().number"
        ></textarea>
        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            size="small"
            (click)="editing.set(false)"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            size="small"
            [attr.data-testid]="'answer-send-' + question().number"
            [disabled]="!text().trim() || busy()"
          >
            @if (busy()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Answer
          </button>
        </div>
      </form>
    } @else {
      <div class="actions start">
        <button
          pButton
          type="button"
          [text]="true"
          size="small"
          [attr.data-testid]="'answer-open-' + question().number"
          (click)="edit()"
        >
          {{ question().status === 'answered' ? 'Change the answer' : 'Answer' }}
        </button>
        @if (question().status === 'open') {
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            size="small"
            (click)="withdraw()"
            [disabled]="busy()"
          >
            Withdraw
          </button>
        }
      </div>
    }
  `,
  styles: `
    .answer {
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
      margin-top: 0.5rem;
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
    .start {
      justify-content: flex-start;
    }
  `,
})
export class AnswerQuestion {
  readonly ticketKey = input.required<string>();
  readonly question = input.required<Question>();
  private readonly conversation = inject(Conversation);
  private readonly problems = inject(ProblemService);
  protected readonly editing = signal(false);
  protected readonly text = signal('');
  protected readonly busy = signal(false);

  protected edit(): void {
    this.text.set(this.question().answer ?? '');
    this.editing.set(true);
  }

  protected async send(): Promise<void> {
    if (
      await guarded(this.busy, this.problems, () =>
        this.conversation.answer(this.ticketKey(), this.question(), this.text().trim()),
      )
    ) {
      this.editing.set(false);
    }
  }

  protected async withdraw(): Promise<void> {
    await guarded(this.busy, this.problems, () =>
      this.conversation.withdraw(this.ticketKey(), this.question()),
    );
  }
}

/**
 * Adds a link from this ticket to another of the tenant (docs/adr/0012). The other ticket typed
 * belongs to the ticket it was typed on.
 */
@Component({
  selector: 'app-link-adder',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, InputText, Select],
  template: `
    <form class="adder" (ngSubmit)="send()">
      <p-select
        [options]="types"
        [ngModel]="type()"
        (ngModelChange)="type.set($event)"
        name="type"
        size="small"
        ariaLabel="Link type"
      />
      <input
        pInputText
        name="other"
        placeholder="COW-12"
        [ngModel]="other()"
        (ngModelChange)="other.set($event)"
        aria-label="The other ticket"
        data-testid="link-other"
      />
      <button
        pButton
        type="submit"
        size="small"
        data-testid="link-add"
        [iconOnly]="true"
        aria-label="Add the link"
        [disabled]="!valid() || busy()"
      >
        @if (busy()) {
          <i class="pi pi-spinner pi-spin"></i>
        } @else {
          <i class="pi pi-plus"></i>
        }
      </button>
    </form>
  `,
  styles: `
    .adder {
      display: grid;
      grid-template-columns: 1fr 1fr auto;
      gap: 0.5rem;
      margin-top: 0.75rem;
      input {
        min-width: 0;
      }
    }
  `,
})
export class LinkAdder {
  readonly ticketKey = input.required<string>();
  private readonly conversation = inject(Conversation);
  private readonly problems = inject(ProblemService);
  protected readonly types: LinkType[] = ['blocks', 'relates-to', 'duplicates', 'found-in'];
  protected readonly type = draft<LinkType>(this.ticketKey, () => 'relates-to');
  protected readonly other = draft(this.ticketKey, () => '');
  protected readonly busy = signal(false);
  protected readonly valid = computed(() =>
    /^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]*$/.test(this.other().trim().toUpperCase()),
  );

  protected async send(): Promise<void> {
    const key = this.ticketKey();
    if (
      (await guarded(this.busy, this.problems, () =>
        this.conversation.link(key, this.type(), this.other().trim().toUpperCase()),
      )) &&
      this.ticketKey() === key
    ) {
      this.other.set('');
    }
  }
}

/**
 * The asker changes an open question's text — the question, the options, the recommendation —
 * over the version the editing began with (docs/adr/0011 D2, docs/adr/0050 D3). A change made
 * meanwhile is shown in the form, which asks whether to write over it or to take the new text.
 * Offered to the asker of an open question only; the server decides.
 */
@Component({
  selector: 'app-edit-question',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, ConflictNote, FormsModule, Textarea],
  template: `
    @if (since(); as editing) {
      <form class="edit" ngNoForm novalidate (submit)="$event.preventDefault(); save()">
        <textarea
          pTextarea
          name="question"
          rows="2"
          maxlength="2000"
          aria-label="Question"
          [attr.data-testid]="'edit-question-text-' + editing.number"
          [ngModel]="text()"
          (ngModelChange)="text.set($event)"
        ></textarea>
        <textarea
          pTextarea
          name="options"
          rows="3"
          maxlength="100000"
          placeholder="The options, each with its cost"
          aria-label="Options"
          [attr.data-testid]="'edit-question-options-' + editing.number"
          [ngModel]="options()"
          (ngModelChange)="options.set($event)"
        ></textarea>
        <textarea
          pTextarea
          name="recommendation"
          rows="2"
          maxlength="10000"
          placeholder="The recommended option and why"
          aria-label="Recommendation"
          [attr.data-testid]="'edit-question-recommendation-' + editing.number"
          [ngModel]="recommendation()"
          (ngModelChange)="recommendation.set($event)"
        ></textarea>
        @if (conflict()) {
          <app-conflict-note
            what="The question"
            [busy]="busy()"
            (overwrite)="overwrite()"
            (takeTheirs)="takeTheirs()"
          />
        }
        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            size="small"
            [disabled]="busy()"
            (click)="since.set(null)"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            size="small"
            [attr.data-testid]="'edit-question-save-' + editing.number"
            [disabled]="!text().trim() || busy() || conflict()"
          >
            @if (busy()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Save
          </button>
        </div>
      </form>
    } @else if (mine()) {
      <button
        pButton
        type="button"
        [text]="true"
        size="small"
        severity="secondary"
        [attr.data-testid]="'edit-question-' + question().number"
        (click)="edit()"
      >
        <i class="pi pi-pencil"></i>Edit the question
      </button>
    }
  `,
  styles: `
    .edit {
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
      margin-top: 0.5rem;
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
  `,
})
export class EditQuestion {
  readonly ticketKey = input.required<string>();
  readonly question = input.required<Question>();
  private readonly conversation = inject(Conversation);
  private readonly problems = inject(ProblemService);
  private readonly session = inject(SessionService);

  /** The asker of an open question edits it. */
  protected readonly mine = computed(
    () =>
      this.question().status === 'open' &&
      this.question().asked_by.id === this.session.person()?.id,
  );
  /** The question as the editing began; null while it is not edited. */
  protected readonly since = signal<Question | null>(null);
  protected readonly conflict = signal(false);
  protected readonly text = signal('');
  protected readonly options = signal('');
  protected readonly recommendation = signal('');
  protected readonly busy = signal(false);

  protected edit(): void {
    this.fill(this.question());
    this.conflict.set(false);
    this.since.set(this.question());
  }

  private fill(question: Question): void {
    this.text.set(question.question);
    this.options.set(question.options);
    this.recommendation.set(question.recommendation);
  }

  protected async save(): Promise<void> {
    const since = this.since();
    const text = this.text().trim();
    if (!since || !text || this.busy() || this.conflict()) {
      return;
    }
    const patch: QuestionPatch = {
      ...(text !== since.question ? { question: text } : {}),
      ...(this.options() !== since.options ? { options: this.options() } : {}),
      ...(this.recommendation() !== since.recommendation
        ? { recommendation: this.recommendation() }
        : {}),
    };
    if (Object.keys(patch).length === 0) {
      this.since.set(null);
      return;
    }
    this.busy.set(true);
    try {
      await this.conversation.editQuestion(this.ticketKey(), since, patch);
      if (this.since() === since) {
        this.since.set(null);
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

  /** The person's text goes over the question as it is now, which its event has brought. */
  protected overwrite(): void {
    this.conflict.set(false);
    this.since.set(this.question());
    void this.save();
  }

  /** The form goes on from the question as it is now. */
  protected takeTheirs(): void {
    this.conflict.set(false);
    this.fill(this.question());
    this.since.set(this.question());
  }
}
