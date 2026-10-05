import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  input,
  output,
  signal,
} from '@angular/core';
import { insertMention, matching, Mentionable, MentionQuery, mentionQuery } from './mentions';

let lists = 0;

/**
 * The picker of a comment's mentions (docs/adr/0015 D5): beside a textarea, it opens when an `@` is
 * typed at the start of a word, lists the candidates whose name or username starts as typed, and a
 * pick — a click, Enter or Tab — writes `@<name> ` into the text and hands the person to the form,
 * which sends their id in the comment's `mentions`. ArrowUp and ArrowDown move through the list,
 * Escape closes it; the textarea keeps the keyboard throughout and names the active option through
 * `aria-activedescendant`. A name typed without the picker is text and mentions nobody.
 */
@Component({
  selector: 'app-mention-list',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (query()) {
      <ul
        class="mentions"
        role="listbox"
        [id]="listId"
        aria-label="People to mention"
        data-testid="mention-list"
      >
        @for (person of shown(); track person.id; let i = $index) {
          <li
            role="option"
            [id]="listId + '-' + i"
            [attr.aria-selected]="i === active()"
            [class.active]="i === active()"
            [attr.data-testid]="'mention-option-' + person.id"
            (mousedown)="$event.preventDefault(); pick(person)"
          >
            {{ person.name }}
            @if (person.username) {
              <span class="muted">{{ person.username }}</span>
            }
          </li>
        } @empty {
          <li class="muted empty" data-testid="mention-none">
            Nobody who sees the ticket by that name.
          </li>
        }
      </ul>
    }
  `,
  styles: `
    :host {
      position: relative;
      display: block;
    }
    .mentions {
      position: absolute;
      z-index: 5;
      top: 0.25rem;
      left: 0;
      min-width: 16rem;
      max-width: 100%;
      margin: 0;
      padding: 0.25rem;
      list-style: none;
      border: 1px solid var(--p-app-border);
      border-radius: var(--p-border-radius-md);
      background: var(--p-app-raised);
    }
    li {
      display: flex;
      gap: 0.5rem;
      padding: 0.375rem 0.5rem;
      border-radius: var(--p-border-radius-sm);
      font-size: 0.875rem;
      cursor: pointer;
    }
    li.active {
      background: var(--p-app-hover);
    }
    .empty {
      cursor: default;
    }
  `,
})
export class MentionList {
  /** The textarea the mentions are typed into. */
  readonly for = input.required<HTMLTextAreaElement>();
  /** Whom the comment can mention. */
  readonly candidates = input.required<Mentionable[]>();
  /** The person picked, whose name is in the text now. */
  readonly picked = output<Mentionable>();

  protected readonly listId = `mention-list-${++lists}`;
  protected readonly query = signal<MentionQuery | null>(null);
  protected readonly active = signal(0);
  protected readonly shown = computed(() => {
    const q = this.query();
    return q ? matching(this.candidates(), q.query).slice(0, 8) : [];
  });

  constructor() {
    effect((onCleanup) => {
      const box = this.for();
      const follow = () => this.follow(box);
      const close = () => this.close(box);
      const keys = (event: KeyboardEvent) => this.key(box, event);
      box.addEventListener('input', follow);
      box.addEventListener('click', follow);
      box.addEventListener('keydown', keys);
      box.addEventListener('blur', close);
      box.setAttribute('aria-autocomplete', 'list');
      onCleanup(() => {
        box.removeEventListener('input', follow);
        box.removeEventListener('click', follow);
        box.removeEventListener('keydown', keys);
        box.removeEventListener('blur', close);
      });
    });
    effect(() => {
      const box = this.for();
      const open = this.query() !== null;
      const active = this.shown().length > 0 ? `${this.listId}-${this.active()}` : null;
      if (open) {
        box.setAttribute('aria-controls', this.listId);
      } else {
        box.removeAttribute('aria-controls');
      }
      if (open && active) {
        box.setAttribute('aria-activedescendant', active);
      } else {
        box.removeAttribute('aria-activedescendant');
      }
    });
  }

  /** Writes the person's name over the `@` being typed, and hands the person on. */
  protected pick(person: Mentionable): void {
    const box = this.for();
    const q = this.query();
    if (!q) {
      return;
    }
    const next = insertMention(
      box.value,
      q.start,
      box.selectionStart ?? box.value.length,
      person.name,
    );
    box.value = next.text;
    box.setSelectionRange(next.caret, next.caret);
    this.query.set(null);
    box.dispatchEvent(new Event('input', { bubbles: true }));
    box.focus();
    this.picked.emit(person);
  }

  private follow(box: HTMLTextAreaElement): void {
    const q = mentionQuery(box.value, box.selectionStart ?? box.value.length);
    if (q?.query !== this.query()?.query) {
      this.active.set(0);
    }
    this.query.set(q);
  }

  private close(box: HTMLTextAreaElement): void {
    this.query.set(null);
    box.removeAttribute('aria-activedescendant');
  }

  private key(box: HTMLTextAreaElement, event: KeyboardEvent): void {
    if (!this.query()) {
      return;
    }
    const shown = this.shown();
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        this.active.set(shown.length === 0 ? 0 : (this.active() + 1) % shown.length);
        return;
      case 'ArrowUp':
        event.preventDefault();
        this.active.set(shown.length === 0 ? 0 : (this.active() - 1 + shown.length) % shown.length);
        return;
      case 'Enter':
      case 'Tab':
        if (shown.length > 0) {
          event.preventDefault();
          this.pick(shown[Math.min(this.active(), shown.length - 1)]);
        }
        return;
      case 'Escape':
        event.preventDefault();
        event.stopPropagation();
        this.close(box);
        return;
    }
  }
}
