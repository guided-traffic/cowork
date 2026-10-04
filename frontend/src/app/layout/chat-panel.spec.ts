import { signal, untracked } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ChatAvailability } from '../api/models';
import { CallEntry, ChatEntry, ChatService, NoticeEntry } from '../core/chat.service';
import { ChatPanel } from './chat-panel';

const available: ChatAvailability = {
  available: true,
  provider: 'openai',
  model: 'qwen/qwen3.6-35b-a3b',
  inside: true,
  reason: null,
};

/** The part of the chat the panel reads, with what a test sets and what the panel asks of it. */
class FakeChat {
  readonly availabilityValue = signal<ChatAvailability | undefined>(available);
  readonly availability = {
    hasValue: () => this.availabilityValue() !== undefined,
    value: () => this.availabilityValue(),
  };
  readonly entries = signal<ChatEntry[]>([]);
  readonly busy = signal(false);
  readonly open = signal(true);
  readonly send = vi.fn<(text: string) => Promise<boolean>>().mockResolvedValue(true);
  readonly decide = vi.fn<(id: string, run: boolean) => Promise<boolean>>().mockResolvedValue(true);
  readonly stop = vi.fn<() => void>();
  readonly restart = vi.fn<() => void>();
}

const call = (overrides: Partial<CallEntry> = {}): CallEntry => ({
  id: 2,
  kind: 'call',
  call: { id: 'c1', name: 'file_ticket', arguments: { title: 'Login fails', severity: 'high' } },
  state: 'running',
  ...overrides,
});

describe('ChatPanel', () => {
  let chat: FakeChat;
  let fixture: ComponentFixture<ChatPanel>;
  let panel: HTMLElement;

  beforeEach(() => {
    chat = new FakeChat();
    TestBed.configureTestingModule({ providers: [{ provide: ChatService, useValue: chat }] });
  });

  async function render(): Promise<void> {
    fixture = TestBed.createComponent(ChatPanel);
    panel = fixture.nativeElement as HTMLElement;
    await fixture.whenStable();
  }

  /** Lets what a click or a keystroke started finish, and shows it. */
  async function settle(): Promise<void> {
    await new Promise((resolve) => setTimeout(resolve));
    await fixture.whenStable();
  }

  const el = <T extends HTMLElement = HTMLElement>(testId: string) =>
    panel.querySelector<T>(`[data-testid="${testId}"]`);
  const all = (testId: string) => [
    ...panel.querySelectorAll<HTMLElement>(`[data-testid="${testId}"]`),
  ];
  const input = () => el<HTMLTextAreaElement>('chat-input') as HTMLTextAreaElement;
  const log = () => el('chat-log') as HTMLElement;

  function type(text: string): void {
    input().value = text;
    input().dispatchEvent(new Event('input'));
  }

  function press(init: KeyboardEventInit): KeyboardEvent {
    const event = new KeyboardEvent('keydown', { key: 'Enter', cancelable: true, ...init });
    input().dispatchEvent(event);
    return event;
  }

  it('is the region the toggle in the top bar controls', async () => {
    await render();

    expect(panel.id).toBe('chat-panel');
    expect(panel.getAttribute('role')).toBe('complementary');
    expect(panel.getAttribute('aria-label')).toBe('Assistant');
    expect(panel.getAttribute('data-testid')).toBe('chat-panel');
  });

  describe('before the first message', () => {
    it('says what the assistant does and names the model, which runs inside the installation', async () => {
      await render();

      const empty = el('chat-empty')?.textContent?.replace(/\s+/g, ' ');
      expect(empty).toContain('works in this tenant as your agent');
      expect(empty).toContain('waits for you to run or skip it');
      expect(el('chat-model')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
        'Model qwen/qwen3.6-35b-a3b, inside this installation.',
      );
      expect(el('chat-model')?.querySelector('code')?.textContent).toBe('qwen/qwen3.6-35b-a3b');
    });

    it('says that what the assistant reads goes to a provider outside the installation', async () => {
      chat.availabilityValue.set({
        ...available,
        inside: false,
        provider: 'anthropic',
        model: 'm',
      });

      await render();

      expect(el('chat-model')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
        'Model m, outside this installation: what the assistant reads here is sent to its provider.',
      );
    });

    it('names no model while none is known', async () => {
      chat.availabilityValue.set(undefined);

      await render();

      expect(el('chat-empty')).not.toBeNull();
      expect(el('chat-model')).toBeNull();
    });

    it('goes once there is a message', async () => {
      await render();

      chat.entries.set([{ id: 1, kind: 'user', text: 'Hi' }]);
      await fixture.whenStable();

      expect(el('chat-empty')).toBeNull();
    });
  });

  describe('the conversation', () => {
    const markup = '<img src=x onerror="alert(1)"> <b>bold</b> **not bold** [link](javascript:x)';

    it("shows the person's message and the assistant's answer as text, whatever markup they hold", async () => {
      chat.entries.set([
        { id: 1, kind: 'user', text: markup },
        { id: 2, kind: 'assistant', text: `Here: ${markup}` },
      ]);

      await render();

      const [user, assistant] = all('chat-message');
      expect(user.textContent).toBe(markup);
      expect(user.classList).toContain('user');
      expect(assistant.textContent).toBe(`Here: ${markup}`);
      expect(assistant.classList).toContain('assistant');
      expect(log().querySelector('img, b, a')).toBeNull();
      expect(assistant.innerHTML).toContain('&lt;img');
    });

    it('keeps the line breaks and the spaces of a message, which the page shows as written', async () => {
      chat.entries.set([{ id: 1, kind: 'assistant', text: 'one\n  two\n\nthree' }]);

      await render();

      const [message] = all('chat-message');
      expect(message.textContent).toBe('one\n  two\n\nthree');
      expect(message.querySelector('br')).toBeNull();
      expect(getComputedStyle(message).whiteSpace).toBe('pre-wrap');
    });

    it('says beneath an answer that no tool was called for it, and beneath no other', async () => {
      chat.entries.set([
        { id: 1, kind: 'assistant', text: 'WEB-1, WEB-2 and WEB-3.', noTools: true },
        { id: 2, kind: 'assistant', text: 'WEB-1 only.' },
      ]);

      await render();

      const notes = all('chat-no-tools');
      expect(notes).toHaveLength(1);
      expect(notes[0].previousElementSibling).toBe(all('chat-message')[0]);
      expect(notes[0].textContent?.trim()).toBe(
        'No tool was called for this answer: nothing it says about cowork was looked up.',
      );
    });

    it('shows a call as a card: the tool, where it stands, the arguments folded away', async () => {
      chat.entries.set([call()]);

      await render();

      const [card] = all('chat-tool');
      expect(card.getAttribute('data-state')).toBe('running');
      expect(card.querySelector('[data-testid="chat-tool-name"]')?.textContent).toBe('file_ticket');
      expect(card.querySelector('[data-testid="chat-tool-state"]')?.textContent).toBe('running');
      const details = card.querySelector('details') as HTMLDetailsElement;
      expect(details.open).toBe(false);
      expect(details.querySelector('summary')?.textContent).toBe('Arguments');
      expect(card.querySelector('[data-testid="chat-tool-arguments"]')?.textContent).toBe(
        '{\n  "title": "Login fails",\n  "severity": "high"\n}',
      );
      expect(card.querySelector('[data-testid="chat-tool-summary"]')).toBeNull();
      expect(card.querySelector('[data-testid="chat-confirm-run"]')).toBeNull();
    });

    it.each([
      ['ok', 'ok'],
      ['failed', 'failed'],
      ['skipped', 'skipped'],
      ['unanswered', 'no result'],
      ['waiting', 'waits for you'],
    ] as const)('says a call is %s', async (state, text) => {
      chat.entries.set([call({ state })]);

      await render();

      expect(el('chat-tool-state')?.textContent).toBe(text);
      expect(el('chat-tool')?.getAttribute('data-state')).toBe(state);
    });

    it('shows what a call answered as text', async () => {
      chat.entries.set([call({ state: 'ok', summary: '# Filed\n<script>x()</script> COW-12' })]);

      await render();

      expect(el('chat-tool-summary')?.textContent).toBe('# Filed\n<script>x()</script> COW-12');
      expect(log().querySelector('script, h1')).toBeNull();
    });

    it('shows arguments that hold markup as text', async () => {
      chat.entries.set([
        call({ call: { id: 'c1', name: '<i>x</i>', arguments: { title: '<img src=x>' } } }),
      ]);

      await render();

      expect(el('chat-tool-name')?.textContent).toBe('<i>x</i>');
      expect(el('chat-tool-arguments')?.textContent).toContain('"<img src=x>"');
      expect(log().querySelector('img, i:not(.pi)')).toBeNull();
    });

    it('says which page a call asked to open that the panel did not open', async () => {
      chat.entries.set([call({ refused: '/me/tokens' })]);

      await render();

      expect(el('chat-tool-refused')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
        'Not opened: /me/tokens is not a ticket, a backlog or a board of this tenant.',
      );
    });

    it('shows the problem of a failed turn', async () => {
      chat.entries.set([
        {
          id: 1,
          kind: 'problem',
          problem: {
            status: 502,
            code: 'chat_provider_failed',
            title: 'Provider failed',
            detail: 'the model did not answer',
            fields: {},
            current: {},
          },
        },
      ]);

      await render();

      expect(el('chat-problem')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
        'Provider failed — the model did not answer',
      );
    });

    it('shows a problem without a detail by its title', async () => {
      chat.entries.set([
        {
          id: 1,
          kind: 'problem',
          problem: {
            status: 500,
            code: 'internal',
            title: 'The turn failed',
            detail: '',
            fields: {},
            current: {},
          },
        },
      ]);

      await render();

      expect(el('chat-problem')?.textContent?.trim()).toBe('The turn failed');
    });

    it('shows nothing for an entry of a kind it does not know', async () => {
      chat.entries.set([
        { id: 1, kind: 'user', text: 'Hi' },
        { id: 2, kind: 'thought', text: 'hidden' } as unknown as ChatEntry,
      ]);

      await render();

      expect(log().textContent).not.toContain('hidden');
      expect(all('chat-message')).toHaveLength(1);
    });

    it('shows how a turn ended', async () => {
      chat.entries.set([{ id: 1, kind: 'notice', text: 'Stopped.' }]);

      await render();

      expect(el('chat-notice')?.textContent).toBe('Stopped.');
      expect(el('chat-notice-new')).toBeNull();
    });
  });

  describe('a conversation too long for another turn', () => {
    const tooLong: NoticeEntry = {
      id: 1,
      kind: 'notice',
      text: 'This conversation is too long for another turn. A new one starts empty.',
      restart: true,
    };

    it('says so, and offers a new conversation right there, as a button', async () => {
      chat.entries.set([{ id: 0, kind: 'user', text: 'Hi' }, tooLong]);

      await render();

      expect(el('chat-notice')?.textContent).toBe(tooLong.text);
      const button = el<HTMLButtonElement>('chat-notice-new');
      expect(button?.tagName).toBe('BUTTON');
      expect(button?.type).toBe('button');
      expect(button?.textContent?.trim()).toBe('New conversation');
      expect(button?.previousElementSibling).toBe(el('chat-notice'));
    });

    it('begins the new conversation, and the keyboard goes to the input', async () => {
      chat.entries.set([tooLong]);
      await render();

      el('chat-notice-new')?.click();

      expect(chat.restart).toHaveBeenCalledOnce();
      expect(document.activeElement).toBe(input());
    });

    it('offers it only while no turn runs', async () => {
      chat.entries.set([tooLong]);
      chat.busy.set(true);

      await render();

      expect(el<HTMLButtonElement>('chat-notice-new')?.disabled).toBe(true);
    });
  });

  describe('a proposal', () => {
    const waiting = call({
      call: { id: 'c9', name: 'transition', arguments: { key: 'COW-1', to: 'done' } },
      state: 'waiting',
      description: 'Move COW-1 to done',
    });

    it('says the act in words and offers Run and Skip', async () => {
      chat.entries.set([waiting]);

      await render();

      expect(el('chat-proposal')?.textContent).toBe('Move COW-1 to done');
      expect(el('chat-confirm-run')?.textContent?.trim()).toBe('Run');
      expect(el('chat-confirm-skip')?.textContent?.trim()).toBe('Skip');
    });

    it('runs with Run and is skipped with Skip', async () => {
      chat.entries.set([waiting]);
      await render();

      el('chat-confirm-run')?.click();
      expect(chat.decide).toHaveBeenLastCalledWith('c9', true);

      el('chat-confirm-skip')?.click();
      expect(chat.decide).toHaveBeenLastCalledWith('c9', false);
    });

    it('cannot be decided while a turn runs', async () => {
      chat.entries.set([waiting]);
      chat.busy.set(true);

      await render();

      expect(el<HTMLButtonElement>('chat-confirm-run')?.disabled).toBe(true);
      expect(el<HTMLButtonElement>('chat-confirm-skip')?.disabled).toBe(true);
    });

    it.each(['running', 'ok', 'skipped', 'unanswered'] as const)(
      'offers no decision once it is %s, and keeps the act in words',
      async (state) => {
        chat.entries.set([{ ...waiting, state }]);

        await render();

        expect(el('chat-confirm-run')).toBeNull();
        expect(el('chat-confirm-skip')).toBeNull();
        expect(el('chat-proposal')?.textContent).toBe('Move COW-1 to done');
      },
    );
  });

  describe('the input', () => {
    it('sends with Enter, and empties itself', async () => {
      await render();
      type('File a bug');

      const event = press({});
      await settle();

      expect(event.defaultPrevented).toBe(true);
      expect(chat.send).toHaveBeenCalledExactlyOnceWith('File a bug');
      expect(input().value).toBe('');
    });

    it('takes Shift+Enter for a new line and sends nothing', async () => {
      await render();
      type('one');

      const event = press({ shiftKey: true });
      await settle();

      expect(event.defaultPrevented).toBe(false);
      expect(chat.send).not.toHaveBeenCalled();
    });

    it.each([
      ['as composing', { isComposing: true }],
      ['by its key code, as Safari has it', { keyCode: 229 }],
    ])('sends nothing on the Enter that ends a composition, told %s', async (_how, init) => {
      await render();
      type('にほん');

      const event = press(init);
      await settle();

      expect(event.defaultPrevented).toBe(false);
      expect(chat.send).not.toHaveBeenCalled();
    });

    it('reads past the other keys', async () => {
      await render();
      type('x');

      press({ key: 'a' });
      await settle();

      expect(chat.send).not.toHaveBeenCalled();
    });

    it('sends with the Send button, which is off while there is nothing to send', async () => {
      await render();
      const button = () => el<HTMLButtonElement>('chat-send') as HTMLButtonElement;
      expect(button().disabled).toBe(true);

      type('  ');
      await fixture.whenStable();
      expect(button().disabled).toBe(true);

      type('Rank COW-12 to now');
      await fixture.whenStable();
      expect(button().disabled).toBe(false);
      button().click();
      await settle();

      expect(chat.send).toHaveBeenCalledExactlyOnceWith('Rank COW-12 to now');
    });

    it('sends nothing empty with Enter', async () => {
      await render();
      type('   ');

      press({});
      await settle();

      expect(chat.send).not.toHaveBeenCalled();
    });

    it('gets the message back when the turn did not begin', async () => {
      chat.send.mockResolvedValue(false);
      await render();
      type('File a bug');

      press({});
      await settle();

      expect(input().value).toBe('File a bug');
    });

    it('keeps what the person typed meanwhile over a message that comes back', async () => {
      let refuse: (begun: boolean) => void = () => undefined;
      chat.send.mockReturnValue(new Promise<boolean>((resolve) => (refuse = resolve)));
      await render();
      type('First');
      press({});
      await settle();

      type('Second');
      refuse(false);
      await settle();

      expect(input().value).toBe('Second');
    });

    it('is off while a turn runs, which Stop ends instead of Send', async () => {
      await render();
      expect(el('chat-stop')).toBeNull();

      chat.busy.set(true);
      await fixture.whenStable();

      expect(input().disabled).toBe(true);
      expect(el('chat-send')).toBeNull();
      el('chat-stop')?.click();
      expect(chat.stop).toHaveBeenCalledOnce();
    });

    it('sends nothing while a turn runs', async () => {
      chat.busy.set(true);
      await render();
      type('More');

      press({});
      await settle();

      expect(chat.send).not.toHaveBeenCalled();
    });

    it('says the assistant is working while a turn runs, and marks the log busy', async () => {
      await render();
      expect(el('chat-working')).toBeNull();
      expect(log().getAttribute('aria-busy')).toBe('false');

      chat.busy.set(true);
      await fixture.whenStable();

      expect(el('chat-working')?.textContent?.trim()).toBe('The assistant is working');
      expect(log().getAttribute('aria-busy')).toBe('true');
    });

    it('names its keys for the screen reader', async () => {
      await render();

      expect(input().getAttribute('aria-label')).toBe('Message to the assistant');
      expect(panel.querySelector(`#${input().getAttribute('aria-describedby')}`)?.textContent).toBe(
        'Enter sends, Shift+Enter starts a new line',
      );
      expect(input().maxLength).toBe(100000);
    });
  });

  describe('a new conversation', () => {
    it('begins with its button', async () => {
      chat.entries.set([{ id: 1, kind: 'user', text: 'Hi' }]);
      await render();

      el('chat-new')?.click();

      expect(chat.restart).toHaveBeenCalledOnce();
      expect(el('chat-new')?.getAttribute('aria-label')).toBe('New conversation');
    });

    it('cannot begin while nothing was said, or while a turn runs', async () => {
      await render();
      const button = () => el<HTMLButtonElement>('chat-new') as HTMLButtonElement;
      expect(button().disabled).toBe(true);

      chat.entries.set([{ id: 1, kind: 'user', text: 'Hi' }]);
      chat.busy.set(true);
      await fixture.whenStable();

      expect(button().disabled).toBe(true);
    });
  });

  describe('scrolling', () => {
    /** Gives the log the measures jsdom does not have, and records where it is scrolled to. */
    function measure(height: number, client = 100): { top: number } {
      const position = { top: 0 };
      Object.defineProperty(log(), 'scrollHeight', { configurable: true, get: () => height });
      Object.defineProperty(log(), 'clientHeight', { configurable: true, get: () => client });
      Object.defineProperty(log(), 'scrollTop', {
        configurable: true,
        get: () => position.top,
        set: (value: number) => (position.top = value),
      });
      return position;
    }

    it('follows the newest message', async () => {
      await render();
      const position = measure(500);

      chat.entries.set([{ id: 1, kind: 'assistant', text: 'A long answer' }]);
      await fixture.whenStable();

      expect(position.top).toBe(500);
    });

    it('stays where the person scrolled up to, and follows again once they send', async () => {
      await render();
      const position = measure(500);
      position.top = 100;
      log().dispatchEvent(new Event('scroll'));

      chat.entries.set([{ id: 1, kind: 'assistant', text: 'More' }]);
      await fixture.whenStable();
      expect(position.top).toBe(100);

      type('Go on');
      press({});
      chat.entries.set([
        { id: 1, kind: 'assistant', text: 'More' },
        { id: 2, kind: 'user', text: 'Go on' },
      ]);
      await settle();

      expect(position.top).toBe(500);
    });

    it('follows again once the person scrolls back down, or decides a proposal', async () => {
      chat.entries.set([call({ state: 'waiting', description: 'Close it' })]);
      await render();
      const position = measure(500);
      position.top = 0;
      log().dispatchEvent(new Event('scroll'));

      position.top = 390;
      log().dispatchEvent(new Event('scroll'));
      chat.entries.set([
        call({ state: 'waiting', description: 'Close it' }),
        { id: 3, kind: 'notice', text: 'x' },
      ]);
      await fixture.whenStable();
      expect(position.top).toBe(500);

      position.top = 0;
      log().dispatchEvent(new Event('scroll'));
      el('chat-confirm-run')?.click();
      chat.entries.set([call({ state: 'running', description: 'Close it' })]);
      await fixture.whenStable();

      expect(position.top).toBe(500);
    });

    it('shows what came while it was closed once it opens again', async () => {
      await render();
      // A closed panel is not laid out (display: none): no height, and a scroll sets nothing. The
      // measures read whether it is open untracked, as the browser's layout does.
      const open = () => untracked(() => chat.open());
      const position = { top: 0 };
      Object.defineProperty(log(), 'scrollHeight', {
        configurable: true,
        get: () => (open() ? 500 : 0),
      });
      Object.defineProperty(log(), 'scrollTop', {
        configurable: true,
        get: () => position.top,
        set: (value: number) => {
          if (open()) {
            position.top = value;
          }
        },
      });

      chat.open.set(false);
      await fixture.whenStable();
      chat.entries.set([{ id: 1, kind: 'assistant', text: 'An answer that came meanwhile' }]);
      await fixture.whenStable();
      expect(position.top).toBe(0);

      chat.open.set(true);
      await fixture.whenStable();

      expect(position.top).toBe(500);
    });

    it('takes the focus, so that a conversation of text alone scrolls from the keyboard', async () => {
      await render();

      expect(log().getAttribute('tabindex')).toBe('0');
      expect(log().tabIndex).toBe(0);
    });

    it('shows where the focus is with a ring of the primary colour inside it', async () => {
      await render();

      // The focus shows where the keyboard moved it: jsdom tells that from the last key pressed.
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true }));
      log().focus();

      expect(document.activeElement).toBe(log());
      // jsdom keeps a shorthand that holds a variable as it is written.
      expect(getComputedStyle(log()).outline).toBe('2px solid var(--p-primary-color)');
      expect(getComputedStyle(log()).outlineOffset).toBe('-2px');
    });
  });

  describe('the keyboard', () => {
    it('comes back to the input when a turn ends, where the turn took it away', async () => {
      chat.busy.set(true);
      await render();
      (document.activeElement as HTMLElement | null)?.blur();

      chat.busy.set(false);
      await settle();

      expect(document.activeElement).toBe(input());
    });

    it('comes back from a control of the panel that a turn ends', async () => {
      chat.busy.set(true);
      await render();
      el('chat-stop')?.focus();

      chat.busy.set(false);
      await settle();

      expect(document.activeElement).toBe(input());
    });

    it('stays where the person put it outside the panel', async () => {
      const outside = document.createElement('button');
      document.body.appendChild(outside);
      chat.busy.set(true);
      await render();
      outside.focus();

      chat.busy.set(false);
      await settle();

      expect(document.activeElement).toBe(outside);
      outside.remove();
    });

    it('stays put while the panel is closed', async () => {
      chat.busy.set(true);
      await render();
      panel.hidden = true;
      (document.activeElement as HTMLElement | null)?.blur();

      chat.busy.set(false);
      await settle();

      expect(document.activeElement).not.toBe(input());
    });

    it('is not taken when the panel first shows', async () => {
      await render();
      await settle();

      expect(document.activeElement).not.toBe(input());
    });
  });
});
