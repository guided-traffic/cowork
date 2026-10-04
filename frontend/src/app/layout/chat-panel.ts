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
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { Message } from 'primeng/message';
import { Select } from 'primeng/select';
import { Textarea } from 'primeng/textarea';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { Tooltip } from 'primeng/tooltip';
import { Capability } from '../api/models';
import { CAPABILITY } from '../api/models/capability-array';
import { CallState, ChatService } from '../core/chat.service';
import { assisted, capabilityMeanings } from '../shared/capabilities';

/** What a call's card says of where it stands. */
const stateTexts: Record<CallState, string> = {
  running: 'running',
  ok: 'ok',
  failed: 'failed',
  unanswered: 'no result',
};

/**
 * The assistant at the right edge of the shell (docs/adr/0076): the conversation of the tenant's
 * chat, the input, Stop, the provider the person picks where the installation configures more than
 * one, and the capabilities the person gives the chat (docs/adr/0043 D5). Everything the model or a
 * tool wrote is shown as text — never as markup and never as rendered Markdown, because ticket text
 * a tool reads can steer what the model writes. A call is a card with its tool's name, its
 * arguments as JSON (folded away until opened) and what it answered; it runs at once, nothing waits
 * for the person.
 */
@Component({
  selector: 'app-chat-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, Message, Select, Textarea, ToggleSwitch, Tooltip],
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
  /** Whether the person's choice of the chat's capabilities is shown. */
  protected readonly settings = signal(false);
  /** A choice of the capabilities is on its way to the backend. */
  protected readonly saving = signal(false);
  protected readonly catalogue = CAPABILITY;
  protected readonly meanings = capabilityMeanings;
  /** The capabilities the chat holds, once read. */
  protected readonly held = computed(() =>
    this.chat.capabilities.hasValue() ? this.chat.capabilities.value() : undefined,
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

  /** One switch of the chat's capabilities: the set with it on or off, in the catalogue's order. */
  protected toggle(held: Capability[], capability: Capability, on: boolean): void {
    void this.choose(CAPABILITY.filter((each) => (each === capability ? on : held.includes(each))));
  }

  /** Full or assisted, the shortcuts of the token page (docs/adr/0043 D4). */
  protected shortcut(which: 'full' | 'assisted'): void {
    void this.choose(which === 'full' ? [...CAPABILITY] : assisted);
  }

  private async choose(capabilities: Capability[]): Promise<void> {
    this.saving.set(true);
    try {
      await this.chat.setCapabilities(capabilities);
    } finally {
      this.saving.set(false);
    }
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
