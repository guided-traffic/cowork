import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, Type, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Comment, Member, Problem, Question, Ticket } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { MembersService } from '../../core/members.service';
import { SessionService } from '../../core/session.service';
import {
  AnswerQuestion,
  AskQuestion,
  CommentComposer,
  EditQuestion,
  LinkAdder,
} from './conversation-forms';

/** Any Idempotency-Key a form makes: a UUID (docs/adr/0045 D3). */
const formKey = expect.stringMatching(
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
);

const ada: Member = {
  role: 'admin',
  person: { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' },
  origins: [{ source: 'grant', role: 'admin' }],
  local: true,
  email: null,
};
const sam: Member = {
  role: 'member',
  person: { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' },
  origins: [{ source: 'grant', role: 'member' }],
  local: true,
  email: null,
};

const key = 'acme/COW-12';

function question(overrides: Partial<Question> = {}): Question {
  return {
    id: 'q-2',
    number: 2,
    question: 'Which flicker is it?',
    options: '',
    options_html: '',
    recommendation: '',
    status: 'open',
    answer: null,
    answer_html: null,
    answered_at: null,
    answered_by: null,
    asked_by: ada.person,
    asked_by_agent: null,
    asked_by_token: null,
    asked_of: null,
    recorded_by_agent: false,
    answered_by_token: null,
    withdrawn_at: null,
    created_at: '2026-10-03T11:00:00Z',
    updated_at: '2026-10-03T11:00:00Z',
    version: 4,
    ...overrides,
  };
}

function refusal(status: number, title: string, detail: string) {
  const body: Problem = { type: 'about:blank', title, status, detail, code: 'validation_failed' };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

/** A write that ends when the test says so. */
function deferred<T>() {
  let resolve: (value: T) => void = () => undefined;
  let reject: (error: unknown) => void = () => undefined;
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

describe('conversation forms', () => {
  let conversation: {
    comment: MockInstance<Conversation['comment']>;
    ask: MockInstance<Conversation['ask']>;
    answer: MockInstance<Conversation['answer']>;
    withdraw: MockInstance<Conversation['withdraw']>;
    link: MockInstance<Conversation['link']>;
    editQuestion: MockInstance<Conversation['editQuestion']>;
  };
  let people: WritableSignal<Member[]>;

  beforeEach(() => {
    conversation = {
      comment: vi.fn<Conversation['comment']>().mockResolvedValue({} as Comment),
      ask: vi.fn<Conversation['ask']>().mockResolvedValue({} as Question),
      answer: vi.fn<Conversation['answer']>().mockResolvedValue({} as Question),
      withdraw: vi.fn<Conversation['withdraw']>().mockResolvedValue({} as Question),
      link: vi.fn<Conversation['link']>().mockResolvedValue(undefined),
      editQuestion: vi.fn<Conversation['editQuestion']>().mockResolvedValue({} as Question),
    };
    people = signal<Member[]>([ada, sam]);
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: Conversation, useValue: conversation },
        { provide: MembersService, useValue: { list: people } },
        { provide: SessionService, useValue: { person: signal({ id: 'p1' }) } },
      ],
    });
  });

  async function render<T>(component: Type<T>, inputs: Record<string, unknown>) {
    const fixture = TestBed.createComponent(component);
    for (const [name, value] of Object.entries(inputs)) {
      fixture.componentRef.setInput(name, value);
    }
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields that appear register a moment later. */
  async function settle(fixture: ComponentFixture<unknown>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<unknown>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<unknown>, selector: string) =>
    host(fixture).querySelector<HTMLElement>(selector);

  const byTestId = (testId: string) => `[data-testid="${testId}"]`;

  function typeInto(fixture: ComponentFixture<unknown>, selector: string, value: string) {
    const field = el(fixture, selector) as HTMLInputElement | HTMLTextAreaElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  const submit = (fixture: ComponentFixture<unknown>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  /** A button of PrimeNG's directive carries its test id itself. */
  const button = (fixture: ComponentFixture<unknown>, selector: string) =>
    el(fixture, selector) as HTMLButtonElement | null;

  const buttonLabelled = (fixture: ComponentFixture<unknown>, label: string) =>
    [...host(fixture).querySelectorAll('button')].find(
      (candidate) => candidate.textContent?.trim() === label,
    ) ?? null;

  describe('CommentComposer', () => {
    const compose = () => render(CommentComposer, { ticketKey: key });

    describe('mentions (docs/adr/0015 D5)', () => {
      const cyd: Member = {
        role: 'admin',
        person: { id: 'p3', display_name: 'Cyd Charisse', username: null },
        origins: [{ source: 'grant', role: 'admin' }],
        local: false,
        email: null,
      };
      const ticket = (overrides: Partial<Ticket> = {}) =>
        ({
          key,
          confidential: false,
          assignee: null,
          reporter: ada.person,
          ...overrides,
        }) as Ticket;

      /** Types into the comment with the caret at the end, as a person does. */
      function typeAtEnd(fixture: ComponentFixture<unknown>, value: string) {
        const box = el(fixture, byTestId('comment-text')) as HTMLTextAreaElement;
        box.value = value;
        box.setSelectionRange(value.length, value.length);
        box.dispatchEvent(new Event('input'));
        fixture.detectChanges();
        return box;
      }
      const options = (fixture: ComponentFixture<unknown>) =>
        [...host(fixture).querySelectorAll('[role="option"]')].map((o) => o.textContent?.trim());

      it('opens the members who see the ticket on an @, never the writer, and filters them as typed', async () => {
        people.set([ada, sam, cyd]);
        const fixture = await render(CommentComposer, { ticketKey: key, ticket: ticket() });

        const box = typeAtEnd(fixture, 'Look @');
        expect(options(fixture)).toEqual(['Sam Rivera local:sam', 'Cyd Charisse']);
        expect(box.getAttribute('aria-controls')).toBe(
          el(fixture, byTestId('mention-list'))?.getAttribute('id'),
        );
        typeAtEnd(fixture, 'Look @ri');
        expect(options(fixture)).toEqual(['Sam Rivera local:sam']);
        typeAtEnd(fixture, 'mail@example');
        expect(el(fixture, byTestId('mention-list'))).toBeNull();
      });

      it('writes the name picked into the text and mentions the person by id', async () => {
        const fixture = await render(CommentComposer, { ticketKey: key, ticket: ticket() });
        const box = typeAtEnd(fixture, 'Look @sa');

        box.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', cancelable: true }));
        fixture.detectChanges();
        expect(box.value).toBe('Look @Sam Rivera ');
        expect(el(fixture, byTestId('mention-list'))).toBeNull();
        typeAtEnd(fixture, 'Look @Sam Rivera, please check.');
        submit(fixture);
        await settle(fixture);

        expect(conversation.comment).toHaveBeenCalledExactlyOnceWith(
          key,
          'Look @Sam Rivera, please check.',
          formKey,
          ['p2'],
        );
      });

      it('picks with a click as well, and moves through the list with the arrows', async () => {
        people.set([ada, sam, cyd]);
        const fixture = await render(CommentComposer, { ticketKey: key, ticket: ticket() });
        const box = typeAtEnd(fixture, '@');

        box.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', cancelable: true }));
        fixture.detectChanges();
        expect(box.getAttribute('aria-activedescendant')).toMatch(/-1$/);
        el(fixture, byTestId('mention-option-p2'))?.dispatchEvent(
          new MouseEvent('mousedown', { bubbles: true, cancelable: true }),
        );
        fixture.detectChanges();

        expect(box.value).toBe('@Sam Rivera ');
      });

      it('mentions nobody whose name the text no longer holds, and closes on Escape', async () => {
        const fixture = await render(CommentComposer, { ticketKey: key, ticket: ticket() });
        const box = typeAtEnd(fixture, '@sa');
        box.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', cancelable: true }));
        typeAtEnd(fixture, 'Never mind @');
        box.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }));
        fixture.detectChanges();
        expect(el(fixture, byTestId('mention-list'))).toBeNull();

        submit(fixture);
        await settle(fixture);

        expect(conversation.comment).toHaveBeenCalledExactlyOnceWith(
          key,
          'Never mind @',
          formKey,
          [],
        );
      });

      it('offers of a confidential ticket only the administrators, the assignee and the reporter', async () => {
        people.set([ada, sam, cyd]);
        const fixture = await render(CommentComposer, {
          ticketKey: key,
          ticket: ticket({ confidential: true, reporter: sam.person }),
        });

        typeAtEnd(fixture, '@');
        expect(options(fixture)).toEqual(['Sam Rivera local:sam', 'Cyd Charisse']);
        typeAtEnd(fixture, '@zz');
        expect(el(fixture, byTestId('mention-none'))?.textContent?.trim()).toBe(
          'Nobody who sees the ticket by that name.',
        );
      });
    });

    it('cannot send nothing, or spaces only', async () => {
      const fixture = await compose();
      expect(button(fixture, byTestId('comment-send'))?.disabled).toBe(true);

      typeInto(fixture, byTestId('comment-text'), '   ');
      expect(button(fixture, byTestId('comment-send'))?.disabled).toBe(true);
      submit(fixture);
      await settle(fixture);

      expect(conversation.comment).not.toHaveBeenCalled();
    });

    it('sends the text, trimmed, to the ticket and empties the field', async () => {
      const fixture = await compose();
      typeInto(fixture, byTestId('comment-text'), '  Reproduced on the second board.  ');
      expect(button(fixture, byTestId('comment-send'))?.disabled).toBe(false);

      submit(fixture);
      await settle(fixture);

      expect(conversation.comment).toHaveBeenCalledExactlyOnceWith(
        key,
        'Reproduced on the second board.',
        formKey,
        [],
      );
      expect((el(fixture, byTestId('comment-text')) as HTMLTextAreaElement).value).toBe('');
      expect(button(fixture, byTestId('comment-send'))?.disabled).toBe(true);
    });

    it('keeps the text and toasts the problem when the comment is refused', async () => {
      conversation.comment.mockRejectedValue(
        refusal(422, 'Validation failed', 'The comment is too long.'),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await compose();
      typeInto(fixture, byTestId('comment-text'), 'A very long comment');

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          severity: 'warn',
          summary: 'Validation failed',
          detail: 'The comment is too long.',
        }),
      );
      expect((el(fixture, byTestId('comment-text')) as HTMLTextAreaElement).value).toBe(
        'A very long comment',
      );
      expect(button(fixture, byTestId('comment-send'))?.disabled).toBe(false);
    });

    it('sends the same Idempotency-Key again after a lost answer, and a new one for another text or the next comment (docs/adr/0045 D3)', async () => {
      conversation.comment.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      conversation.comment.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      const fixture = await compose();
      typeInto(fixture, byTestId('comment-text'), 'Reproduced.');
      submit(fixture);
      await settle(fixture);
      submit(fixture);
      await settle(fixture);
      const keys = () => conversation.comment.mock.calls.map((call) => call[2]);
      expect(keys()[1]).toBe(keys()[0]);

      typeInto(fixture, byTestId('comment-text'), 'Reproduced twice.');
      submit(fixture);
      await settle(fixture);
      expect(keys()[2]).not.toBe(keys()[0]);

      // Written: the same text again is another comment.
      typeInto(fixture, byTestId('comment-text'), 'Reproduced twice.');
      submit(fixture);
      await settle(fixture);
      expect(keys()[3]).not.toBe(keys()[2]);
    });

    it('sends nothing twice while a comment is on its way', async () => {
      const write = deferred<Comment>();
      conversation.comment.mockReturnValue(write.promise);
      const fixture = await compose();
      typeInto(fixture, byTestId('comment-text'), 'Once');

      submit(fixture);
      await settle(fixture);
      expect(button(fixture, byTestId('comment-send'))?.disabled).toBe(true);
      expect(
        button(fixture, byTestId('comment-send'))?.querySelector('i.pi-spinner'),
      ).not.toBeNull();
      typeInto(fixture, byTestId('comment-text'), 'Once more');
      expect(button(fixture, byTestId('comment-send'))?.disabled).toBe(true);
      write.resolve({} as Comment);
      await settle(fixture);

      expect(conversation.comment).toHaveBeenCalledOnce();
    });
  });

  describe('AskQuestion', () => {
    const ask = () => render(AskQuestion, { ticketKey: key });

    async function open(fixture: ComponentFixture<AskQuestion>) {
      button(fixture, byTestId('ask-open'))?.click();
      await settle(fixture);
    }

    it('shows only a button until the person wants to ask', async () => {
      const fixture = await ask();

      expect(button(fixture, byTestId('ask-open'))?.textContent?.trim()).toBe('Ask a question');
      expect(host(fixture).querySelector('form')).toBeNull();
    });

    it('opens the form from the button and closes it again with Cancel', async () => {
      const fixture = await ask();
      await open(fixture);
      expect(host(fixture).querySelector('form')).not.toBeNull();
      expect(button(fixture, byTestId('ask-open'))).toBeNull();

      buttonLabelled(fixture, 'Cancel')?.click();
      await settle(fixture);

      expect(host(fixture).querySelector('form')).toBeNull();
      expect(button(fixture, byTestId('ask-open'))).not.toBeNull();
    });

    it('cannot ask without a question', async () => {
      const fixture = await ask();
      await open(fixture);
      expect(button(fixture, byTestId('ask-send'))?.disabled).toBe(true);

      typeInto(fixture, byTestId('ask-question'), '   ');
      expect(button(fixture, byTestId('ask-send'))?.disabled).toBe(true);
      typeInto(fixture, byTestId('ask-question'), 'Which flicker is it?');
      expect(button(fixture, byTestId('ask-send'))?.disabled).toBe(false);
    });

    it('asks just the question when that is all there is', async () => {
      const fixture = await ask();
      await open(fixture);
      typeInto(fixture, byTestId('ask-question'), '  Which flicker is it?  ');

      submit(fixture);
      await settle(fixture);

      expect(conversation.ask).toHaveBeenCalledExactlyOnceWith(
        key,
        {
          question: 'Which flicker is it?',
        },
        formKey,
      );
      expect(conversation.ask.mock.calls[0][1]).toStrictEqual({ question: 'Which flicker is it?' });
    });

    it('asks with the options, the recommendation and the person asked, when there are any', async () => {
      const fixture = await ask();
      await open(fixture);
      typeInto(fixture, byTestId('ask-question'), 'Which flicker is it?');
      typeInto(fixture, 'textarea[aria-label="Options"]', ' Repaint, or reflow ');
      typeInto(fixture, 'textarea[aria-label="Recommendation"]', ' Repaint ');
      fixture.debugElement.query(By.directive(Select)).triggerEventHandler('ngModelChange', 'p2');
      await settle(fixture);

      submit(fixture);
      await settle(fixture);

      expect(conversation.ask).toHaveBeenCalledExactlyOnceWith(
        key,
        {
          question: 'Which flicker is it?',
          options: 'Repaint, or reflow',
          recommendation: 'Repaint',
          asked_of: 'p2',
        },
        formKey,
      );
    });

    it('asks again with the same Idempotency-Key after a lost answer, and with a new one for another content (docs/adr/0045 D3)', async () => {
      conversation.ask.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      conversation.ask.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      const fixture = await ask();
      await open(fixture);
      typeInto(fixture, byTestId('ask-question'), 'Which flicker is it?');
      submit(fixture);
      await settle(fixture);
      submit(fixture);
      await settle(fixture);
      const keys = () => conversation.ask.mock.calls.map((call) => call[2]);
      expect(keys()[1]).toBe(keys()[0]);

      typeInto(fixture, 'textarea[aria-label="Options"]', 'Repaint, or reflow');
      submit(fixture);
      await settle(fixture);
      expect(keys()[2]).not.toBe(keys()[0]);
    });

    it('offers the members of the tenant, by name, to ask', async () => {
      const fixture = await ask();
      await open(fixture);

      const select = fixture.debugElement.query(By.directive(Select)).componentInstance as Select;
      expect(select.options()).toEqual([
        { id: 'p1', name: 'Ada Lovelace' },
        { id: 'p2', name: 'Sam Rivera' },
      ]);
    });

    it('asks nobody in particular when the person asked is cleared again', async () => {
      const fixture = await ask();
      await open(fixture);
      typeInto(fixture, byTestId('ask-question'), 'Which flicker is it?');
      const select = fixture.debugElement.query(By.directive(Select));
      select.triggerEventHandler('ngModelChange', 'p2');
      select.triggerEventHandler('ngModelChange', null);
      await settle(fixture);

      submit(fixture);
      await settle(fixture);

      expect(conversation.ask.mock.calls[0][1]).toStrictEqual({ question: 'Which flicker is it?' });
    });

    it('closes the form and empties it once the question is asked', async () => {
      const fixture = await ask();
      await open(fixture);
      typeInto(fixture, byTestId('ask-question'), 'Which flicker is it?');
      typeInto(fixture, 'textarea[aria-label="Options"]', 'Repaint, or reflow');
      typeInto(fixture, 'textarea[aria-label="Recommendation"]', 'Repaint');
      fixture.debugElement.query(By.directive(Select)).triggerEventHandler('ngModelChange', 'p1');
      await settle(fixture);
      submit(fixture);
      await settle(fixture);
      expect(host(fixture).querySelector('form')).toBeNull();

      await open(fixture);

      expect((el(fixture, byTestId('ask-question')) as HTMLTextAreaElement).value).toBe('');
      expect((el(fixture, 'textarea[aria-label="Options"]') as HTMLTextAreaElement).value).toBe('');
      expect(
        (el(fixture, 'textarea[aria-label="Recommendation"]') as HTMLTextAreaElement).value,
      ).toBe('');
      expect(el(fixture, 'p-select .p-select-label')?.textContent?.trim()).toBe(
        'Asked of nobody in particular',
      );
    });

    it('keeps the form open with what was typed, and toasts the problem, when it is refused', async () => {
      conversation.ask.mockRejectedValue(
        refusal(422, 'Validation failed', 'The person cannot see the ticket.'),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await ask();
      await open(fixture);
      typeInto(fixture, byTestId('ask-question'), 'Which flicker is it?');

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'The person cannot see the ticket.' }),
      );
      expect(host(fixture).querySelector('form')).not.toBeNull();
      expect((el(fixture, byTestId('ask-question')) as HTMLTextAreaElement).value).toBe(
        'Which flicker is it?',
      );
    });

    it('asks once while the question is on its way', async () => {
      const write = deferred<Question>();
      conversation.ask.mockReturnValue(write.promise);
      const fixture = await ask();
      await open(fixture);
      typeInto(fixture, byTestId('ask-question'), 'Which flicker is it?');

      submit(fixture);
      await settle(fixture);
      expect(button(fixture, byTestId('ask-send'))?.disabled).toBe(true);
      write.resolve({} as Question);
      await settle(fixture);

      expect(conversation.ask).toHaveBeenCalledOnce();
    });
  });

  describe('AnswerQuestion', () => {
    const answerTo = (current: Question = question()) =>
      render(AnswerQuestion, { ticketKey: key, question: current });

    async function edit(fixture: ComponentFixture<AnswerQuestion>, number = 2) {
      button(fixture, byTestId(`answer-open-${number}`))?.click();
      await settle(fixture);
    }

    describe('an open question', () => {
      it('offers to answer it or to withdraw it', async () => {
        const fixture = await answerTo();

        expect(button(fixture, byTestId('answer-open-2'))?.textContent?.trim()).toBe('Answer');
        expect(buttonLabelled(fixture, 'Withdraw')).not.toBeNull();
        expect(host(fixture).querySelector('form')).toBeNull();
      });

      it('sends the answer, trimmed, with the question, and closes the form', async () => {
        const current = question();
        const fixture = await answerTo(current);
        await edit(fixture);
        expect((el(fixture, byTestId('answer-text-2')) as HTMLTextAreaElement).value).toBe('');
        expect(button(fixture, byTestId('answer-send-2'))?.disabled).toBe(true);

        typeInto(fixture, byTestId('answer-text-2'), '  Reflow.  ');
        submit(fixture);
        await settle(fixture);

        expect(conversation.answer).toHaveBeenCalledExactlyOnceWith(key, current, 'Reflow.');
        expect(host(fixture).querySelector('form')).toBeNull();
      });

      it('cannot be answered with spaces only', async () => {
        const fixture = await answerTo();
        await edit(fixture);

        typeInto(fixture, byTestId('answer-text-2'), '   ');

        expect(button(fixture, byTestId('answer-send-2'))?.disabled).toBe(true);
      });

      it('goes back to the buttons on Cancel without sending anything', async () => {
        const fixture = await answerTo();
        await edit(fixture);
        typeInto(fixture, byTestId('answer-text-2'), 'Half an answer');

        buttonLabelled(fixture, 'Cancel')?.click();
        await settle(fixture);

        expect(host(fixture).querySelector('form')).toBeNull();
        expect(conversation.answer).not.toHaveBeenCalled();
      });

      it('withdraws the question', async () => {
        const current = question();
        const fixture = await answerTo(current);

        buttonLabelled(fixture, 'Withdraw')?.click();
        await settle(fixture);

        expect(conversation.withdraw).toHaveBeenCalledExactlyOnceWith(key, current);
      });

      it('toasts the problem when the question cannot be withdrawn', async () => {
        conversation.withdraw.mockRejectedValue(
          refusal(409, 'The question was answered', 'It was answered a moment ago.'),
        );
        const add = vi.spyOn(TestBed.inject(MessageService), 'add');
        const fixture = await answerTo();

        buttonLabelled(fixture, 'Withdraw')?.click();
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({ detail: 'It was answered a moment ago.' }),
        );
      });

      it('keeps the form open with what was typed when the answer is refused', async () => {
        conversation.answer.mockRejectedValue(refusal(422, 'Validation failed', 'Too long.'));
        const fixture = await answerTo();
        await edit(fixture);
        typeInto(fixture, byTestId('answer-text-2'), 'A long answer');

        submit(fixture);
        await settle(fixture);

        expect(host(fixture).querySelector('form')).not.toBeNull();
        expect((el(fixture, byTestId('answer-text-2')) as HTMLTextAreaElement).value).toBe(
          'A long answer',
        );
      });

      it('does not offer to withdraw while the answer is on its way', async () => {
        const write = deferred<Question>();
        conversation.answer.mockReturnValue(write.promise);
        const fixture = await answerTo();
        await edit(fixture);
        typeInto(fixture, byTestId('answer-text-2'), 'Reflow.');

        submit(fixture);
        await settle(fixture);
        expect(button(fixture, byTestId('answer-send-2'))?.disabled).toBe(true);
        write.resolve({} as Question);
        await settle(fixture);

        expect(host(fixture).querySelector('form')).toBeNull();
        expect(buttonLabelled(fixture, 'Withdraw')?.disabled).toBe(false);
      });
    });

    describe('an answered question', () => {
      const answered = () =>
        question({ status: 'answered', answer: 'Repaint.', answered_by: sam.person, version: 7 });

      it('offers to change the answer, and no longer to withdraw the question', async () => {
        const fixture = await answerTo(answered());

        expect(button(fixture, byTestId('answer-open-2'))?.textContent?.trim()).toBe(
          'Change the answer',
        );
        expect(buttonLabelled(fixture, 'Withdraw')).toBeNull();
      });

      it('starts the form from the answer that is there', async () => {
        const fixture = await answerTo(answered());

        await edit(fixture);

        expect((el(fixture, byTestId('answer-text-2')) as HTMLTextAreaElement).value).toBe(
          'Repaint.',
        );
      });

      it('sends the changed answer with the question as it was read', async () => {
        const current = answered();
        const fixture = await answerTo(current);
        await edit(fixture);

        typeInto(fixture, byTestId('answer-text-2'), 'Reflow after all.');
        submit(fixture);
        await settle(fixture);

        expect(conversation.answer).toHaveBeenCalledExactlyOnceWith(
          key,
          current,
          'Reflow after all.',
        );
      });

      it('follows the question when it changes', async () => {
        const fixture = await answerTo(answered());

        fixture.componentRef.setInput('question', question({ number: 3 }));
        await settle(fixture);

        expect(button(fixture, byTestId('answer-open-3'))?.textContent?.trim()).toBe('Answer');
      });
    });

    describe('with the real conversation service', () => {
      let http: HttpTestingController;

      beforeEach(() => {
        TestBed.resetTestingModule();
        TestBed.configureTestingModule({
          providers: [
            MessageService,
            provideHttpClient(),
            provideHttpClientTesting(),
            provideApiConfiguration(''),
          ],
        });
        http = TestBed.inject(HttpTestingController);
      });

      const url = '/api/v1/teams/acme/projects/COW/tickets/12/questions/2/answer';

      it('sends the version of an answered question as If-Match, so that a change overwrites only what was read', async () => {
        const fixture = await answerTo(
          question({ status: 'answered', answer: 'Repaint.', version: 7 }),
        );
        await edit(fixture);
        typeInto(fixture, byTestId('answer-text-2'), 'Reflow after all.');

        submit(fixture);
        const sent = http.expectOne(url);
        expect(sent.request.method).toBe('PUT');
        expect(sent.request.headers.get('If-Match')).toBe('"7"');
        expect(sent.request.body).toEqual({ answer: 'Reflow after all.' });
        sent.flush({});
        await settle(fixture);

        expect(host(fixture).querySelector('form')).toBeNull();
      });

      it('sends no If-Match for the first answer of an open question', async () => {
        const fixture = await answerTo(question({ version: 4 }));
        await edit(fixture);
        typeInto(fixture, byTestId('answer-text-2'), 'Reflow.');

        submit(fixture);
        const sent = http.expectOne(url);
        expect(sent.request.headers.has('If-Match')).toBe(false);
        sent.flush({});
        await settle(fixture);
      });
    });
  });

  describe('LinkAdder', () => {
    const adder = () => render(LinkAdder, { ticketKey: key });

    it.each([
      ['COW-12', true],
      ['cow-12', true],
      ['  cow-12  ', true],
      ['AB-1', true],
      ['A1-100', true],
      ['ABCDEFGHIJ-5', true],
      ['', false],
      ['   ', false],
      ['COW', false],
      ['COW-', false],
      ['COW-0', false],
      ['COW-012', false],
      ['C-1', false],
      ['1COW-2', false],
      ['COW-12-3', false],
      ['COW 12', false],
      ['acme/COW-12', false],
      ['ABCDEFGHIJK-1', false],
    ])('accepts the other ticket %j: %s', async (other, valid) => {
      const fixture = await adder();

      typeInto(fixture, byTestId('link-other'), other);

      expect(button(fixture, byTestId('link-add'))?.disabled).toBe(!valid);
    });

    it('links as relates-to unless another type is chosen, and offers the four types', async () => {
      const fixture = await adder();

      const select = fixture.debugElement.query(By.directive(Select)).componentInstance as Select;
      expect(select.options()).toEqual(['blocks', 'relates-to', 'duplicates', 'found-in']);
      expect(el(fixture, 'p-select .p-select-label')?.textContent?.trim()).toBe('relates-to');
    });

    it('links to the other ticket by its short key, upper-cased and trimmed, and empties the field', async () => {
      const fixture = await adder();
      typeInto(fixture, byTestId('link-other'), '  ops-3 ');

      submit(fixture);
      await settle(fixture);

      expect(conversation.link).toHaveBeenCalledExactlyOnceWith(key, 'relates-to', 'OPS-3');
      expect((el(fixture, byTestId('link-other')) as HTMLInputElement).value).toBe('');
      expect(button(fixture, byTestId('link-add'))?.disabled).toBe(true);
    });

    it('links with the type that is chosen', async () => {
      const fixture = await adder();
      fixture.debugElement
        .query(By.directive(Select))
        .triggerEventHandler('ngModelChange', 'blocks');
      typeInto(fixture, byTestId('link-other'), 'COW-3');
      await settle(fixture);

      submit(fixture);
      await settle(fixture);

      expect(conversation.link).toHaveBeenCalledExactlyOnceWith(key, 'blocks', 'COW-3');
    });

    it('does not link to something that is not a ticket key', async () => {
      const fixture = await adder();
      typeInto(fixture, byTestId('link-other'), 'not a key');

      submit(fixture);
      await settle(fixture);

      expect(button(fixture, byTestId('link-add'))?.disabled).toBe(true);
    });

    it('keeps the key and toasts the problem when the link is refused', async () => {
      conversation.link.mockRejectedValue(
        refusal(409, 'Link cycle', 'COW-3 already waits on this ticket.'),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await adder();
      typeInto(fixture, byTestId('link-other'), 'COW-3');

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'COW-3 already waits on this ticket.' }),
      );
      expect((el(fixture, byTestId('link-other')) as HTMLInputElement).value).toBe('COW-3');
      expect(button(fixture, byTestId('link-add'))?.disabled).toBe(false);
    });

    it('links once while the link is on its way', async () => {
      const write = deferred<unknown>();
      conversation.link.mockReturnValue(write.promise);
      const fixture = await adder();
      typeInto(fixture, byTestId('link-other'), 'COW-3');

      submit(fixture);
      await settle(fixture);
      expect(button(fixture, byTestId('link-add'))?.disabled).toBe(true);
      write.resolve(undefined);
      await settle(fixture);

      expect(conversation.link).toHaveBeenCalledOnce();
    });
  });

  describe('EditQuestion (docs/adr/0011 D2)', () => {
    const editing = (current: Question = question()) =>
      render(EditQuestion, { ticketKey: key, question: current });

    async function open(fixture: ComponentFixture<EditQuestion>) {
      button(fixture, byTestId('edit-question-2'))?.click();
      await settle(fixture);
    }

    const save = async (fixture: ComponentFixture<EditQuestion>) => {
      button(fixture, byTestId('edit-question-save-2'))?.click();
      await settle(fixture);
    };

    it('is offered to the asker of an open question', async () => {
      const fixture = await editing();

      expect(button(fixture, byTestId('edit-question-2'))?.textContent?.trim()).toBe(
        'Edit the question',
      );
    });

    it.each([
      ['somebody who did not ask it', question({ asked_by: sam.person })],
      ['an answered question', question({ status: 'answered', answer: 'This one' })],
      ['a withdrawn question', question({ status: 'withdrawn' })],
    ])('is not offered for %s', async (_, current) => {
      const fixture = await editing(current);

      expect(button(fixture, byTestId('edit-question-2'))).toBeNull();
    });

    it('starts from the text of the question, its options and its recommendation', async () => {
      const fixture = await editing(
        question({ options: 'A or B', recommendation: 'A, it is cheaper' }),
      );

      await open(fixture);

      expect((el(fixture, byTestId('edit-question-text-2')) as HTMLTextAreaElement).value).toBe(
        'Which flicker is it?',
      );
      expect((el(fixture, byTestId('edit-question-options-2')) as HTMLTextAreaElement).value).toBe(
        'A or B',
      );
      expect(
        (el(fixture, byTestId('edit-question-recommendation-2')) as HTMLTextAreaElement).value,
      ).toBe('A, it is cheaper');
    });

    it('writes what changed over the version the editing began with, and closes', async () => {
      const fixture = await editing();
      await open(fixture);
      typeInto(
        fixture,
        byTestId('edit-question-text-2'),
        '  Which flicker, the first or the second? ',
      );
      typeInto(fixture, byTestId('edit-question-options-2'), 'The first, the second');
      fixture.componentRef.setInput('question', question({ version: 5 }));
      await settle(fixture);

      await save(fixture);

      expect(conversation.editQuestion).toHaveBeenCalledExactlyOnceWith(key, question(), {
        question: 'Which flicker, the first or the second?',
        options: 'The first, the second',
      });
      expect(el(fixture, byTestId('edit-question-text-2'))).toBeNull();
    });

    it('closes without a write when nothing changed, and on Cancel', async () => {
      const fixture = await editing();
      await open(fixture);
      await save(fixture);
      expect(el(fixture, byTestId('edit-question-text-2'))).toBeNull();

      await open(fixture);
      typeInto(fixture, byTestId('edit-question-text-2'), 'Something else');
      buttonLabelled(fixture, 'Cancel')?.click();
      await settle(fixture);

      expect(el(fixture, byTestId('edit-question-text-2'))).toBeNull();
      expect(conversation.editQuestion).not.toHaveBeenCalled();
    });

    describe('that met a change made meanwhile', () => {
      async function conflicted() {
        conversation.editQuestion.mockRejectedValueOnce(
          refusal(412, 'Precondition failed', 'The question changed.'),
        );
        const fixture = await editing();
        await open(fixture);
        typeInto(fixture, byTestId('edit-question-text-2'), 'Mine');
        await save(fixture);
        fixture.componentRef.setInput('question', question({ version: 5, question: 'Theirs' }));
        await settle(fixture);
        return fixture;
      }

      it('says so, keeps what was typed and saves nothing until the person decides', async () => {
        const fixture = await conflicted();

        expect(el(fixture, byTestId('conflict'))?.textContent).toContain(
          'The question changed while you edited it.',
        );
        expect((el(fixture, byTestId('edit-question-text-2')) as HTMLTextAreaElement).value).toBe(
          'Mine',
        );
        expect(button(fixture, byTestId('edit-question-save-2'))?.disabled).toBe(true);
      });

      it('writes the person own text over the question as it is now on request', async () => {
        const fixture = await conflicted();

        button(fixture, byTestId('conflict-overwrite'))?.click();
        await settle(fixture);

        expect(conversation.editQuestion).toHaveBeenLastCalledWith(
          key,
          question({ version: 5, question: 'Theirs' }),
          { question: 'Mine' },
        );
      });

      it('goes on from the question as it is now on request', async () => {
        const fixture = await conflicted();

        button(fixture, byTestId('conflict-take-theirs'))?.click();
        await settle(fixture);

        expect((el(fixture, byTestId('edit-question-text-2')) as HTMLTextAreaElement).value).toBe(
          'Theirs',
        );
        expect(conversation.editQuestion).toHaveBeenCalledOnce();
      });
    });

    it('toasts any other refusal and keeps the form', async () => {
      conversation.editQuestion.mockRejectedValueOnce(
        refusal(403, 'Forbidden', 'Only the asker edits a question.'),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await editing();
      await open(fixture);
      typeInto(fixture, byTestId('edit-question-text-2'), 'Mine');

      await save(fixture);

      expect(add).toHaveBeenCalledOnce();
      expect(el(fixture, byTestId('edit-question-text-2'))).not.toBeNull();
    });
  });

  describe('a turn of the page to another ticket', () => {
    const next = 'acme/COW-13';

    it('drops the comment typed for the ticket before, which nothing is sent to', async () => {
      const fixture = await render(CommentComposer, { ticketKey: key });
      typeInto(fixture, byTestId('comment-text'), 'Meant for COW-12');

      fixture.componentRef.setInput('ticketKey', next);
      await settle(fixture);

      expect((el(fixture, byTestId('comment-text')) as HTMLTextAreaElement).value).toBe('');
      expect(button(fixture, byTestId('comment-send'))?.disabled).toBe(true);
      expect(conversation.comment).not.toHaveBeenCalled();
    });

    it('lands a comment on its way on the ticket it was written for, and keeps what is typed for the next', async () => {
      const write = deferred<Comment>();
      conversation.comment.mockReturnValue(write.promise);
      const fixture = await render(CommentComposer, { ticketKey: key });
      typeInto(fixture, byTestId('comment-text'), 'For COW-12');
      submit(fixture);
      await settle(fixture);

      fixture.componentRef.setInput('ticketKey', next);
      await settle(fixture);
      typeInto(fixture, byTestId('comment-text'), 'For COW-13');
      write.resolve({} as Comment);
      await settle(fixture);

      expect(conversation.comment).toHaveBeenCalledExactlyOnceWith(key, 'For COW-12', formKey, []);
      expect((el(fixture, byTestId('comment-text')) as HTMLTextAreaElement).value).toBe(
        'For COW-13',
      );
    });

    it('closes the question form, empty, and asks nothing', async () => {
      const fixture = await render(AskQuestion, { ticketKey: key });
      button(fixture, byTestId('ask-open'))?.click();
      await settle(fixture);
      typeInto(fixture, byTestId('ask-question'), 'Meant for COW-12?');

      fixture.componentRef.setInput('ticketKey', next);
      await settle(fixture);

      expect(host(fixture).querySelector('form')).toBeNull();
      button(fixture, byTestId('ask-open'))?.click();
      await settle(fixture);
      expect((el(fixture, byTestId('ask-question')) as HTMLTextAreaElement).value).toBe('');
      expect(conversation.ask).not.toHaveBeenCalled();
    });

    it('drops the other ticket typed for a link, and the type chosen', async () => {
      const fixture = await render(LinkAdder, { ticketKey: key });
      fixture.debugElement
        .query(By.directive(Select))
        .triggerEventHandler('ngModelChange', 'blocks');
      typeInto(fixture, byTestId('link-other'), 'OPS-3');

      fixture.componentRef.setInput('ticketKey', next);
      await settle(fixture);

      expect((el(fixture, byTestId('link-other')) as HTMLInputElement).value).toBe('');
      expect(el(fixture, 'p-select .p-select-label')?.textContent?.trim()).toBe('relates-to');
      expect(conversation.link).not.toHaveBeenCalled();
    });
  });
});
