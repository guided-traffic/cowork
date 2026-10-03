import {
  ChangeDetectionStrategy,
  Component,
  computed,
  ElementRef,
  input,
  linkedSignal,
  model,
  viewChild,
} from '@angular/core';
import { ButtonDirective } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { InputText } from 'primeng/inputtext';

/**
 * Shows a secret that cowork shows exactly once — a new token's plaintext, a temporary password —
 * with a button that copies it. The secret is the parent's signal and it is the only place the page
 * holds it: the one way out is the button that says it is stored, which sets it to `null`, and
 * nothing here writes it anywhere else (no storage, no URL, no log) — except the copy button,
 * which puts the secret on the system clipboard, where cowork cannot reach it again and where a
 * clipboard history may keep it. The projected content is the warning that says what the secret is
 * and what holding it means. Escape, the cross and a click beside the dialog do not close it,
 * because what it shows cannot be shown again.
 */
@Component({
  selector: 'app-secret-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Dialog, InputText],
  template: `
    <p-dialog
      [visible]="open()"
      [modal]="true"
      [draggable]="false"
      [closable]="false"
      [closeOnEscape]="false"
      [dismissableMask]="false"
      [style]="{ width: '36rem' }"
      [header]="header()"
      data-testid="secret-dialog"
    >
      <div class="body">
        <p class="warning" role="alert">
          <i class="pi pi-exclamation-triangle" aria-hidden="true"></i>
          <span><ng-content /></span>
        </p>
        <div class="field">
          <span class="label">{{ label() }}</span>
          <span class="secret">
            <input
              #field
              pInputText
              readonly
              autocomplete="off"
              spellcheck="false"
              [attr.aria-label]="label()"
              [value]="secret() ?? ''"
              (focus)="selectAll($event)"
              data-testid="secret-value"
            />
            <button
              pButton
              type="button"
              severity="secondary"
              (click)="copy()"
              data-testid="secret-copy"
            >
              <i [class]="copyState() === 'copied' ? 'pi pi-check' : 'pi pi-copy'"></i>
              {{ copyState() === 'copied' ? 'Copied' : 'Copy' }}
            </button>
          </span>
        </div>
        @if (copyState() === 'failed') {
          <small class="error" role="status" data-testid="secret-copy-failed">
            Copying is not available here. Select the text and copy it yourself.
          </small>
        }
        <div class="actions">
          <button pButton type="button" (click)="secret.set(null)" data-testid="secret-done">
            I have stored it
          </button>
        </div>
      </div>
    </p-dialog>
  `,
  styles: `
    .body {
      display: flex;
      flex-direction: column;
      gap: 1rem;
    }
    .warning {
      display: flex;
      gap: 0.75rem;
      align-items: flex-start;
      margin: 0;
      padding: 0.75rem 0.875rem;
      border: 1px solid color-mix(in srgb, var(--p-severity-high) 45%, transparent);
      border-radius: var(--p-border-radius-lg);
      background: color-mix(in srgb, var(--p-severity-high) 10%, transparent);
      font-size: 0.875rem;
      > i {
        margin-top: 0.1875rem;
        color: var(--p-severity-high);
      }
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
    }
    .label {
      font-size: 0.8125rem;
      font-weight: 550;
    }
    .secret {
      display: flex;
      gap: 0.5rem;
      input {
        flex: 1;
        min-width: 0;
        font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      }
    }
    .error {
      color: var(--p-severity-critical);
    }
    .actions {
      display: flex;
      justify-content: flex-end;
    }
  `,
})
export class SecretDialog {
  /** The secret to show, or `null` while there is none; the dialog is open exactly while it is set. */
  readonly secret = model<string | null>(null);
  readonly header = input.required<string>();
  /** What the secret is, as the field says it. */
  readonly label = input.required<string>();

  protected readonly open = computed(() => this.secret() !== null);
  /** A new secret starts with nothing copied. */
  protected readonly copyState = linkedSignal<string | null, 'idle' | 'copied' | 'failed'>({
    source: this.secret,
    computation: () => 'idle',
  });
  private readonly field = viewChild<ElementRef<HTMLInputElement>>('field');

  protected selectAll(event: Event): void {
    (event.target as HTMLInputElement).select();
  }

  /**
   * Copies the secret. Where the browser offers no clipboard — a page served over plain HTTP has
   * none, and `navigator.clipboard` is then undefined — or refuses it, the text is selected for the
   * person to copy by hand.
   */
  protected async copy(): Promise<void> {
    const secret = this.secret();
    if (secret === null) {
      return;
    }
    try {
      await navigator.clipboard.writeText(secret);
      this.copyState.set('copied');
    } catch {
      this.copyState.set('failed');
      this.field()?.nativeElement.select();
    }
  }
}
