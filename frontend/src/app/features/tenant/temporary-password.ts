import { ChangeDetectionStrategy, Component, computed, inject, input, model } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { Account } from '../../api/models';
import { AuthService } from '../../core/auth.service';

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

/**
 * A generated password is at least this long: 24 of these characters are about 139 bits, and the
 * installation's minimum (`COWORK_PASSWORD_MIN_LENGTH`, 12 by default) is longer only where an
 * operator has raised it, which `TemporaryPassword` follows.
 */
export const generatedLength = 24;

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
 * The field for a temporary password (docs/adr/0033 D4), filled with a generated one by a button.
 * It is a plain text field and never a password field: an administrator reads the password out and
 * hands it over, and a field that looks like a credential makes a password manager offer to save
 * it under the administrator's own login for this site, or to fill that login into it. For the
 * same reason it carries `autocomplete="off"` and the attributes that the common managers read as
 * "leave this alone". The server holds the password to its policy and its message comes in as
 * `error`. The value travels through `value`, not through the form the field sits in, so its
 * `ngModel` is standalone.
 */
@Component({
  selector: 'app-temporary-password',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, FormsModule, InputText],
  template: `
    <div class="row">
      <div class="field">
        <label [attr.for]="inputId()">Temporary password</label>
        <input
          pInputText
          type="text"
          [attr.id]="inputId()"
          autocomplete="off"
          spellcheck="false"
          data-1p-ignore
          data-lpignore="true"
          data-bwignore
          data-form-type="other"
          [ngModel]="value()"
          [ngModelOptions]="{ standalone: true }"
          (ngModelChange)="value.set($event)"
          [attr.aria-invalid]="!!error()"
          [attr.aria-describedby]="error() ? errorId() : null"
          [attr.data-testid]="testId()"
        />
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
      <small class="error" role="alert" [attr.id]="errorId()" [attr.data-testid]="errorId()">{{
        message
      }}</small>
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
    label {
      font-size: 0.8125rem;
      font-weight: 550;
    }
    input {
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    }
    .error {
      font-size: 0.75rem;
      color: var(--p-severity-critical);
    }
  `,
})
export class TemporaryPassword {
  readonly value = model('');
  readonly error = input<string>();
  /** The `data-testid` of the input; its button and its error add `-generate` and `-error`. */
  readonly testId = input('temporary-password');

  private readonly auth = inject(AuthService);

  protected readonly inputId = computed(() => `${this.testId()}-input`);
  protected readonly errorId = computed(() => `${this.testId()}-error`);
  /** As long as the installation's minimum asks, and never shorter than `generatedLength`. */
  private readonly length = computed(() =>
    Math.max(
      generatedLength,
      this.auth.options.hasValue() ? this.auth.options.value().password_min_length : 0,
    ),
  );

  protected generate(): void {
    this.value.set(generatePassword(this.length()));
  }
}
