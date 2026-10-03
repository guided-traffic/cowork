import { ChangeDetectionStrategy, Component, input, model, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { InputPassword } from 'primeng/inputpassword';
import { Account } from '../../api/models';

/** A temporary password an administrator set, and the account it is for: shown once, then forgotten. */
export interface IssuedPassword {
  account: Account;
  password: string;
}

/**
 * The characters of a generated password: letters and digits, without the ones that are read for
 * each other (0 and O, 1 and l and I), because an administrator hands the password to a person who
 * types it in, perhaps from a message or from the other end of a call.
 */
export const passwordAlphabet = 'abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789';

/** Sixteen of these characters are about 92 bits, more than the policy (length only) asks for. */
export const generatedLength = 16;

/**
 * A random password from `crypto.getRandomValues`. A byte is used only below the greatest multiple
 * of the alphabet's size that fits in it, so that no character is likelier than another.
 */
export function generatePassword(length = generatedLength): string {
  const limit = 256 - (256 % passwordAlphabet.length);
  let password = '';
  while (password.length < length) {
    for (const byte of crypto.getRandomValues(new Uint8Array(length))) {
      if (byte < limit && password.length < length) {
        password += passwordAlphabet[byte % passwordAlphabet.length];
      }
    }
  }
  return password;
}

/**
 * The field for a temporary password (docs/adr/0033 D4): hidden until the person shows it, and
 * filled with a generated one by a button, which also shows it, since an administrator has to read
 * out what they set. The server holds the password to its policy; its message comes in as `error`.
 * The value travels through `value`, not through the form the field sits in, so its `ngModel` is
 * standalone.
 */
@Component({
  selector: 'app-temporary-password',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, InputPassword],
  template: `
    <div class="row">
      <div class="field">
        <span class="label">Temporary password</span>
        <span class="password">
          <input
            pInputPassword
            aria-label="Temporary password"
            [(mask)]="masked"
            autocomplete="new-password"
            spellcheck="false"
            [fluid]="true"
            [ngModel]="value()"
            [ngModelOptions]="{ standalone: true }"
            (ngModelChange)="value.set($event)"
            [attr.data-testid]="testId()"
          />
          <button
            type="button"
            class="reveal"
            (click)="masked.set(!masked())"
            [attr.aria-label]="masked() ? 'Show the password' : 'Hide the password'"
            [attr.data-testid]="testId() + '-reveal'"
          >
            <i [class]="masked() ? 'pi pi-eye' : 'pi pi-eye-slash'"></i>
          </button>
        </span>
      </div>
      <button
        pButton
        type="button"
        severity="secondary"
        [outlined]="true"
        (click)="generate()"
        [attr.data-testid]="testId() + '-generate'"
      >
        <i class="pi pi-sync"></i>Generate
      </button>
    </div>
    @if (error(); as message) {
      <small class="error" data-testid="temporary-password-error">{{ message }}</small>
    }
  `,
  styles: `
    :host {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
    }
    .row {
      display: grid;
      grid-template-columns: 1fr auto;
      align-items: end;
      gap: 0.5rem;
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
    .password {
      position: relative;
      display: block;
      input {
        padding-right: 2.5rem;
        font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      }
    }
    .reveal {
      position: absolute;
      top: 50%;
      right: 0.5rem;
      transform: translateY(-50%);
      padding: 0.25rem;
      border: 0;
      background: none;
      color: var(--p-text-muted-color);
      cursor: pointer;
    }
    .error {
      color: var(--p-severity-critical);
    }
  `,
})
export class TemporaryPassword {
  readonly value = model('');
  readonly error = input<string>();
  /** The `data-testid` of the input; the reveal and generate buttons add `-reveal` and `-generate`. */
  readonly testId = input('temporary-password');

  protected readonly masked = signal(true);

  protected generate(): void {
    this.value.set(generatePassword());
    this.masked.set(false);
  }
}
