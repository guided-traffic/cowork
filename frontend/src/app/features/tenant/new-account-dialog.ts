import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  linkedSignal,
  model,
  output,
  signal,
  untracked,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ButtonDirective } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { Role } from '../../api/models';
import { AccountsService } from '../../core/accounts.service';
import { ProblemService } from '../../core/problem.service';
import { describedBy, selectAria } from '../../shared/field-aria';
import { keepOpenWhile } from '../../shared/keep-open';
import { roleMeanings } from './roles';
import { IssuedPassword, TemporaryPassword } from './temporary-password';

/** A username: lower case, one to 63 characters, a letter or a digit first (docs/adr/0033 D2). */
export const accountUsername = /^[a-z0-9][a-z0-9._-]{0,62}$/;

/**
 * Creates a local account in the tenant (docs/adr/0033 D1): its username, its name, its role and a
 * temporary password the person changes at the first sign-in. The page shows the password once,
 * from the event; the dialog forgets it as soon as it closes, however it closes. While the request
 * is out nothing closes it, so that a refusal always lands in the form that was sent. The username
 * field has a neutral name and the attributes password managers read as "leave this alone", so
 * that neither this form nor the password field in it looks like the sign-up of a credential.
 */
@Component({
  selector: 'app-new-account-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Dialog, FormsModule, InputText, Select, TemporaryPassword],
  template: `
    <p-dialog
      [visible]="visible()"
      (visibleChange)="visible.set($event)"
      [modal]="true"
      [draggable]="false"
      [closable]="!saving()"
      [dismissableMask]="!saving()"
      appendTo="body"
      [style]="{ width: '34rem' }"
      header="New account"
      data-testid="new-account-dialog"
    >
      <form class="form" (ngSubmit)="save()">
        <p class="muted lead">
          For a person who has no identity elsewhere. They sign in with this username and the
          temporary password, and choose their own password at the first sign-in.
        </p>
        <div class="field">
          <label for="account-username-input">Username</label>
          <input
            pInputText
            id="account-username-input"
            name="account-username"
            maxlength="63"
            autocomplete="off"
            spellcheck="false"
            data-1p-ignore
            data-lpignore="true"
            data-bwignore
            data-form-type="other"
            placeholder="ada"
            [ngModel]="username()"
            (ngModelChange)="username.set($event.toLowerCase())"
            [attr.aria-invalid]="usernameInvalid() || !!errors()['username']"
            [attr.aria-describedby]="
              describedBy('account-username-hint', errors()['username'] && 'account-username-error')
            "
            data-testid="account-username"
          />
          <small
            id="account-username-hint"
            [class.error]="usernameInvalid()"
            [class.muted]="!usernameInvalid()"
            data-testid="account-username-hint"
          >
            Lower-case letters, digits, dots, hyphens and underscores, starting with a letter or a
            digit, 63 at most. A username exists once in the whole installation.
          </small>
          @if (errors()['username']; as error) {
            <small
              class="error"
              id="account-username-error"
              role="alert"
              data-testid="account-username-error"
              >{{ error }}</small
            >
          }
        </div>
        <div class="field">
          <label for="account-name-input">Name</label>
          <input
            pInputText
            id="account-name-input"
            name="displayName"
            maxlength="200"
            autocomplete="off"
            placeholder="Ada Lovelace"
            [ngModel]="displayName()"
            (ngModelChange)="displayName.set($event)"
            [attr.aria-invalid]="!!errors()['display_name']"
            [attr.aria-describedby]="describedBy(errors()['display_name'] && 'account-name-error')"
            data-testid="account-name"
          />
          @if (errors()['display_name']; as error) {
            <small
              class="error"
              id="account-name-error"
              role="alert"
              data-testid="account-name-error"
              >{{ error }}</small
            >
          }
        </div>
        <div class="field">
          <span id="account-role-label">Role</span>
          <p-select
            [options]="roles"
            [ngModel]="role()"
            (ngModelChange)="role.set($event)"
            name="role"
            size="small"
            [invalid]="!!errors()['role']"
            [pt]="rolePt()"
            ariaLabelledBy="account-role-label"
            data-testid="account-role"
          />
          <small class="muted" id="account-role-meaning" data-testid="account-role-meaning">{{
            roleMeanings[role()]
          }}</small>
          @if (errors()['role']; as error) {
            <small
              class="error"
              id="account-role-error"
              role="alert"
              data-testid="account-role-error"
              >{{ error }}</small
            >
          }
        </div>
        <app-temporary-password
          [(value)]="password"
          [error]="errors()['temporary_password']"
          testId="account-password"
        />
        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            [disabled]="saving()"
            (click)="visible.set(false)"
            data-testid="account-cancel"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            data-testid="account-save"
            [disabled]="!canSave() || saving()"
          >
            @if (saving()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Create account
          </button>
        </div>
      </form>
    </p-dialog>
  `,
  styles: `
    .form {
      display: flex;
      flex-direction: column;
      gap: 0.875rem;
    }
    .lead {
      margin: 0;
      font-size: 0.8125rem;
    }
    .field {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
      > label,
      > span:first-child {
        font-size: 0.8125rem;
        font-weight: 550;
      }
    }
    small {
      font-size: 0.75rem;
    }
    .error {
      color: var(--p-severity-critical);
    }
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
  `,
})
export class NewAccountDialog {
  readonly visible = model(false);
  /** The account that was made and the password that was set, for the page to show once. */
  readonly created = output<IssuedPassword>();

  private readonly accounts = inject(AccountsService);
  private readonly problems = inject(ProblemService);

  protected readonly roles: Role[] = ['viewer', 'member', 'admin'];
  protected readonly roleMeanings = roleMeanings;
  protected readonly describedBy = describedBy;
  protected readonly username = signal('');
  protected readonly displayName = signal('');
  protected readonly role = signal<Role>('member');
  protected readonly password = signal('');
  protected readonly saving = signal(false);
  protected readonly errors = signal<Record<string, string>>({});
  protected readonly usernameInvalid = computed(
    () => this.username().trim() !== '' && !accountUsername.test(this.username().trim()),
  );
  protected readonly rolePt = computed(() =>
    selectAria(
      !!this.errors()['role'],
      describedBy('account-role-meaning', this.errors()['role'] && 'account-role-error'),
    ),
  );
  protected readonly canSave = computed(
    () =>
      accountUsername.test(this.username().trim()) &&
      this.displayName().trim() !== '' &&
      this.password() !== '' &&
      !this.saving(),
  );

  /**
   * The Idempotency-Key of the act this form is making: one for each content it holds. A retry of
   * the same content — after a network failure, say — sends the same key, so a lost answer is
   * answered again instead of being refused as taken; any change of the content, and closing the
   * form, make a new one (docs/adr/0045).
   */
  private readonly key = linkedSignal(() => {
    this.username();
    this.displayName();
    this.role();
    this.password();
    return crypto.randomUUID();
  });

  constructor() {
    keepOpenWhile(() => this.saving());
    // However the dialog closes, what was typed goes with it: the password above all.
    effect(() => {
      if (!this.visible()) {
        untracked(() => this.reset());
      }
    });
  }

  protected async save(): Promise<void> {
    if (!this.canSave()) {
      return;
    }
    this.saving.set(true);
    this.errors.set({});
    try {
      const password = this.password();
      const account = await this.accounts.create(
        {
          username: this.username().trim(),
          display_name: this.displayName().trim(),
          role: this.role(),
          temporary_password: password,
        },
        this.key(),
      );
      this.created.emit({ account, password });
      this.visible.set(false);
    } catch (error) {
      if (this.visible()) {
        this.errors.set(this.problems.report(error, { fields: true }).fields);
      } else {
        // The page closed the dialog while the request was out, and the form is empty: a field
        // error would sit under nothing and come back with the next account. A toast says it.
        this.problems.report(error);
      }
    } finally {
      this.saving.set(false);
    }
  }

  private reset(): void {
    this.username.set('');
    this.displayName.set('');
    this.role.set('member');
    this.password.set('');
    this.errors.set({});
  }
}
