import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
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
import { IssuedPassword, TemporaryPassword } from './temporary-password';

/** A username: lower case, one to 63 characters, a letter or a digit first (docs/adr/0033 D2). */
export const accountUsername = /^[a-z0-9][a-z0-9._-]{0,62}$/;

/** What each role may do, in a line (docs/adr/0034 D1), shown beside the choice. */
export const roleMeanings: Record<Role, string> = {
  viewer: 'Reads what the tenant shares and watches tickets; changes nothing.',
  member: 'Does the daily work: files and moves tickets, comments, books time.',
  admin: 'Does what a member does, and administers the tenant: accounts, settings, projects.',
};

/**
 * Creates a local account in the tenant (docs/adr/0033 D1): its username, its name, its role and a
 * temporary password the person changes at the first login. The page shows the password once, from
 * the event; the dialog forgets it as soon as it closes, however it closes.
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
      [dismissableMask]="true"
      [style]="{ width: '34rem' }"
      header="New account"
      data-testid="new-account-dialog"
    >
      <form class="form" (ngSubmit)="save()">
        <p class="muted lead">
          For a person who has no identity elsewhere. They sign in with this username and the
          temporary password, and choose their own password at the first login.
        </p>
        <label class="field">
          <span>Username</span>
          <input
            pInputText
            name="username"
            maxlength="63"
            autocomplete="off"
            spellcheck="false"
            placeholder="ada"
            [ngModel]="username()"
            (ngModelChange)="username.set($event.toLowerCase())"
            [attr.aria-invalid]="usernameInvalid()"
            aria-describedby="account-username-hint"
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
            <small class="error" data-testid="account-username-error">{{ error }}</small>
          }
        </label>
        <label class="field">
          <span>Name</span>
          <input
            pInputText
            name="displayName"
            maxlength="200"
            autocomplete="off"
            placeholder="Ada Lovelace"
            [ngModel]="displayName()"
            (ngModelChange)="displayName.set($event)"
            data-testid="account-name"
          />
          @if (errors()['display_name']; as error) {
            <small class="error" data-testid="account-name-error">{{ error }}</small>
          }
        </label>
        <div class="field">
          <span id="account-role-label">Role</span>
          <p-select
            [options]="roles"
            [ngModel]="role()"
            (ngModelChange)="role.set($event)"
            name="role"
            size="small"
            ariaLabelledBy="account-role-label"
            data-testid="account-role"
          />
          <small class="muted" data-testid="account-role-meaning">{{ roleMeanings[role()] }}</small>
          @if (errors()['role']; as error) {
            <small class="error">{{ error }}</small>
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
  protected readonly username = signal('');
  protected readonly displayName = signal('');
  protected readonly role = signal<Role>('member');
  protected readonly password = signal('');
  protected readonly saving = signal(false);
  protected readonly errors = signal<Record<string, string>>({});
  protected readonly usernameInvalid = computed(
    () => this.username().trim() !== '' && !accountUsername.test(this.username().trim()),
  );
  protected readonly canSave = computed(
    () =>
      accountUsername.test(this.username().trim()) &&
      this.displayName().trim() !== '' &&
      this.password() !== '' &&
      !this.saving(),
  );

  constructor() {
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
      const account = await this.accounts.create({
        username: this.username().trim(),
        display_name: this.displayName().trim(),
        role: this.role(),
        temporary_password: password,
      });
      this.created.emit({ account, password });
      this.visible.set(false);
    } catch (error) {
      this.errors.set(this.problems.report(error, { fields: true }).fields);
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
