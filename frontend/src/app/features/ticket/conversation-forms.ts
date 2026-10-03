import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { Textarea } from 'primeng/textarea';
import { LinkType, Question } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { MembersService } from '../../core/members.service';
import { ProblemService } from '../../core/problem.service';

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

/** Writes a comment on the ticket (docs/adr/0015); the thread reloads through the event stream. */
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
  protected readonly text = signal('');
  protected readonly busy = signal(false);

  protected async send(): Promise<void> {
    const text = this.text().trim();
    if (
      text &&
      (await guarded(this.busy, this.problems, () =>
        this.conversation.comment(this.ticketKey(), text),
      ))
    ) {
      this.text.set('');
    }
  }
}

/** Asks a question on the ticket, of a person if one is named (docs/adr/0011 D2). */
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
  protected readonly open = signal(false);
  protected readonly question = signal('');
  protected readonly options = signal('');
  protected readonly recommendation = signal('');
  protected readonly askedOf = signal<string | null>(null);
  protected readonly busy = signal(false);

  protected async send(): Promise<void> {
    const ok = await guarded(this.busy, this.problems, () =>
      this.conversation.ask(this.ticketKey(), {
        question: this.question().trim(),
        ...(this.options().trim() ? { options: this.options().trim() } : {}),
        ...(this.recommendation().trim() ? { recommendation: this.recommendation().trim() } : {}),
        ...(this.askedOf() ? { asked_of: this.askedOf() as string } : {}),
      }),
    );
    if (ok) {
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

/** Adds a link from this ticket to another of the tenant (docs/adr/0012). */
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
  protected readonly type = signal<LinkType>('relates-to');
  protected readonly other = signal('');
  protected readonly busy = signal(false);
  protected readonly valid = computed(() =>
    /^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]*$/.test(this.other().trim().toUpperCase()),
  );

  protected async send(): Promise<void> {
    if (
      await guarded(this.busy, this.problems, () =>
        this.conversation.link(this.ticketKey(), this.type(), this.other().trim().toUpperCase()),
      )
    ) {
      this.other.set('');
    }
  }
}
