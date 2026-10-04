import {
  afterNextRender,
  afterRenderEffect,
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  ElementRef,
  inject,
  Injector,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { ButtonDirective } from 'primeng/button';
import { Message } from 'primeng/message';
import { Textarea } from 'primeng/textarea';
import { Tooltip } from 'primeng/tooltip';
import { CallState, ChatService } from '../core/chat.service';

/** What a call's card says of where it stands. */
const stateTexts: Record<CallState, string> = {
  running: 'running',
  ok: 'ok',
  failed: 'failed',
  waiting: 'waits for you',
  skipped: 'skipped',
  unanswered: 'no result',
};

/**
 * The assistant at the right edge of the shell (docs/adr/0076): the conversation of the tenant's
 * chat, the input, and Stop. Everything the model or a tool wrote is shown as text — never as
 * markup and never as rendered Markdown, because ticket text a tool reads can steer what the
 * model writes. A call is a card with its tool's name, its arguments as JSON (folded away until
 * opened), what it answered, and Run and Skip where it waits for the person.
 */
@Component({
  selector: 'app-chat-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Message, Textarea, Tooltip],
  templateUrl: './chat-panel.html',
  styleUrl: './chat-panel.scss',
  host: {
    id: 'chat-panel',
    role: 'complementary',
    'aria-label': 'Assistant',
    'data-testid': 'chat-panel',
  },
})
export class ChatPanel {
  protected readonly chat = inject(ChatService);
  private readonly host: HTMLElement = inject(ElementRef).nativeElement;
  private readonly injector = inject(Injector);
  private readonly log = viewChild.required<ElementRef<HTMLElement>>('log');
  private readonly input = viewChild.required<ElementRef<HTMLTextAreaElement>>('input');

  protected readonly draft = signal('');
  protected readonly stateTexts = stateTexts;
  protected readonly model = computed(() =>
    this.chat.availability.hasValue() ? this.chat.availability.value().model : null,
  );
  protected readonly inside = computed(
    () => this.chat.availability.hasValue() && this.chat.availability.value().inside,
  );
  /** Whether the log follows the newest message: until the person scrolls up to read. */
  private follow = true;

  constructor() {
    afterRenderEffect({
      write: () => {
        this.chat.entries();
        // A closed panel has nothing to scroll: opened again, it shows what came meanwhile.
        this.chat.open();
        const log = this.log().nativeElement;
        if (this.follow) {
          log.scrollTop = log.scrollHeight;
        }
      },
    });
    // The input is disabled while a turn runs, which takes the keyboard from it; it gets it back
    // when the turn ends, unless the person went on elsewhere.
    let running = false;
    effect(() => {
      const busy = this.chat.busy();
      if (running && !busy) {
        untracked(() => this.refocus());
      }
      running = busy;
    });
  }

  protected scrolled(): void {
    const log = this.log().nativeElement;
    this.follow = log.scrollHeight - log.scrollTop - log.clientHeight < 32;
  }

  /**
   * Enter sends, Shift+Enter is a new line; the Enter that ends a composition (an IME) is neither —
   * Safari ends the composition before that keydown, which only its key code 229 then tells.
   */
  protected keydown(event: KeyboardEvent): void {
    const composing = event.isComposing || event.keyCode === 229;
    if (event.key === 'Enter' && !event.shiftKey && !composing) {
      event.preventDefault();
      void this.send();
    }
  }

  protected submit(event: Event): void {
    event.preventDefault();
    void this.send();
  }

  /**
   * The message goes into the conversation, and comes back into the input when the turn did not
   * begin — unless the person typed something new meanwhile.
   */
  protected async send(): Promise<void> {
    const text = this.draft();
    if (this.chat.busy() || text.trim() === '') {
      return;
    }
    this.write('');
    this.follow = true;
    if (!(await this.chat.send(text)) && this.draft() === '') {
      this.write(text);
    }
  }

  protected decide(callId: string, run: boolean): void {
    this.follow = true;
    void this.chat.decide(callId, run);
  }

  /** A new conversation from the notice, which goes with the old one: the keyboard goes to the input. */
  protected restart(): void {
    this.chat.restart();
    this.input().nativeElement.focus();
  }

  protected json(value: unknown): string {
    return JSON.stringify(value, null, 2);
  }

  /** The input is the person's: the page writes into it only to empty it, or to give a message back. */
  private write(text: string): void {
    this.input().nativeElement.value = text;
    this.draft.set(text);
  }

  private refocus(): void {
    afterNextRender(
      () => {
        const active = this.host.ownerDocument.activeElement;
        if (!active || active === this.host.ownerDocument.body || this.host.contains(active)) {
          if (!this.host.hidden) {
            this.input().nativeElement.focus();
          }
        }
      },
      { injector: this.injector },
    );
  }
}
