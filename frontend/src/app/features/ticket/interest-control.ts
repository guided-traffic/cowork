import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { SelectButton } from 'primeng/selectbutton';
import { Textarea } from 'primeng/textarea';
import { Tooltip } from 'primeng/tooltip';
import { Interest, InterestWeight } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { ProblemService } from '../../core/problem.service';

const weights: { value: InterestWeight; meaning: string }[] = [
  { value: 'watch', meaning: 'I want to see where it goes' },
  { value: 'need', meaning: 'My work depends on it; say why' },
  { value: 'urgent', meaning: 'My work is stuck until it is done; say why' },
];

/**
 * Who holds a stake in the ticket, and the person's own (docs/adr/0013): a weight, and a reason
 * for `need` and `urgent`, which the score weighs (docs/adr/0014 D3).
 */
@Component({
  selector: 'app-interest-control',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, SelectButton, Textarea, Tooltip],
  template: `
    <div class="mine">
      <p-selectbutton
        [options]="weights"
        optionLabel="value"
        optionValue="value"
        [ngModel]="weight()"
        (ngModelChange)="choose($event)"
        size="small"
        ariaLabel="Your stake"
        data-testid="interest-weight"
      />
      @if (mine()) {
        <button
          pButton
          type="button"
          [text]="true"
          [rounded]="true"
          size="small"
          severity="secondary"
          pTooltip="Remove your stake"
          data-testid="interest-remove"
          [iconOnly]="true"
          (click)="remove()"
          aria-label="Remove your stake"
          [disabled]="busy()"
        >
          <i class="pi pi-times"></i>
        </button>
      }
    </div>
    @if (needsReason()) {
      <form class="reason" (ngSubmit)="save()">
        <textarea
          pTextarea
          name="note"
          rows="2"
          placeholder="Why your work depends on it"
          [ngModel]="note()"
          (ngModelChange)="note.set($event)"
          aria-label="Reason"
          data-testid="interest-note"
        ></textarea>
        <button pButton type="submit" size="small" [disabled]="!note().trim() || busy()">
          @if (busy()) {
            <i class="pi pi-spinner pi-spin"></i>
          }
          Save
        </button>
      </form>
    }
    <ul class="holders">
      @for (interest of interests(); track interest.person.id) {
        <li>
          <span class="who">{{ interest.person.display_name }}</span>
          <span class="weight" [attr.data-weight]="interest.weight">{{ interest.weight }}</span>
          @if (interest.note) {
            <span class="muted note">{{ interest.note }}</span>
          }
        </li>
      } @empty {
        <li class="muted">Nobody holds a stake yet.</li>
      }
    </ul>
  `,
  styles: `
    .mine {
      display: flex;
      align-items: center;
      gap: 0.25rem;
    }
    .reason {
      display: flex;
      flex-direction: column;
      align-items: flex-end;
      gap: 0.5rem;
      margin-top: 0.5rem;
      textarea {
        width: 100%;
      }
    }
    .holders {
      margin: 0.75rem 0 0;
      padding: 0;
      list-style: none;
      font-size: 0.8125rem;
      li {
        display: flex;
        flex-wrap: wrap;
        gap: 0.25rem 0.5rem;
        padding: 0.25rem 0;
      }
    }
    .who {
      font-weight: 550;
    }
    .weight[data-weight='urgent'] {
      color: var(--p-severity-critical);
    }
    .weight[data-weight='need'] {
      color: var(--p-severity-high);
    }
    .note {
      flex-basis: 100%;
    }
  `,
})
export class InterestControl {
  readonly ticketKey = input.required<string>();
  readonly interests = input.required<Interest[]>();
  readonly me = input<string | undefined>();

  private readonly conversation = inject(Conversation);
  private readonly problems = inject(ProblemService);

  protected readonly weights = weights;
  protected readonly mine = computed(() =>
    this.interests().find((interest) => interest.person.id === this.me()),
  );
  /** The weight chosen and waiting for its reason. */
  private readonly pending = signal<InterestWeight | null>(null);
  protected readonly weight = computed(() => this.pending() ?? this.mine()?.weight ?? null);
  protected readonly needsReason = computed(
    () => this.pending() === 'need' || this.pending() === 'urgent',
  );
  protected readonly note = signal('');
  protected readonly busy = signal(false);

  protected choose(weight: InterestWeight | null): void {
    if (!weight) {
      return;
    }
    if (weight === 'watch') {
      this.pending.set(null);
      void this.write(() => this.conversation.setInterest(this.ticketKey(), 'watch', ''));
    } else {
      this.note.set(this.mine()?.note ?? '');
      this.pending.set(weight);
    }
  }

  protected save(): void {
    const weight = this.pending();
    if (weight) {
      void this.write(() =>
        this.conversation.setInterest(this.ticketKey(), weight, this.note()),
      ).then((ok) => {
        if (ok) {
          this.pending.set(null);
        }
      });
    }
  }

  protected remove(): void {
    this.pending.set(null);
    void this.write(() => this.conversation.removeInterest(this.ticketKey()));
  }

  private async write(call: () => Promise<unknown>): Promise<boolean> {
    this.busy.set(true);
    try {
      await call();
      return true;
    } catch (error) {
      this.problems.report(error);
      return false;
    } finally {
      this.busy.set(false);
    }
  }
}
