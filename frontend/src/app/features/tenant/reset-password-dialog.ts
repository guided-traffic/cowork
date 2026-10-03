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
import { Account } from '../../api/models';
import { AccountsService } from '../../core/accounts.service';
import { ProblemService } from '../../core/problem.service';
import { keepOpenWhile } from '../../shared/keep-open';
import { IssuedPassword, TemporaryPassword } from './temporary-password';

/**
 * Sets a new temporary password on a local account (docs/adr/0033 D5). The dialog is open exactly
 * while it holds the account; every session of the account ends, and the person changes the
 * password at the next login. The page shows the password once, from the event, and the dialog
 * forgets it as soon as it closes, however it closes. While the request is out nothing closes it,
 * so that a refusal always lands in the form that was sent.
 */
@Component({
  selector: 'app-reset-password-dialog',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Dialog, FormsModule, TemporaryPassword],
  template: `
    <p-dialog
      [visible]="open()"
      (visibleChange)="closed($event)"
      [modal]="true"
      [draggable]="false"
      [closable]="!saving()"
      [dismissableMask]="!saving()"
      [style]="{ width: '32rem' }"
      [header]="'Reset the password of ' + (account()?.username ?? '')"
      data-testid="reset-password-dialog"
    >
      <form class="form" (ngSubmit)="save()">
        <p class="muted lead">
          Every session of the account ends and its person chooses a new password at the next login.
          The account stays locked if it is, and its tokens stay as they are.
        </p>
        <app-temporary-password
          [(value)]="password"
          [error]="errors()['temporary_password']"
          testId="reset-password"
        />
        <div class="actions">
          <button
            pButton
            type="button"
            [text]="true"
            severity="secondary"
            [disabled]="saving()"
            (click)="account.set(null)"
            data-testid="reset-cancel"
          >
            Cancel
          </button>
          <button
            pButton
            type="submit"
            data-testid="reset-save"
            [disabled]="!canSave() || saving()"
          >
            @if (saving()) {
              <i class="pi pi-spinner pi-spin"></i>
            }
            Reset password
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
    .actions {
      display: flex;
      justify-content: flex-end;
      gap: 0.5rem;
    }
  `,
})
export class ResetPasswordDialog {
  /** The account to reset; `null` while the dialog is closed. */
  readonly account = model<Account | null>(null);
  /** The account and the password that was set, for the page to show once. */
  readonly passwordSet = output<IssuedPassword>();

  private readonly accounts = inject(AccountsService);
  private readonly problems = inject(ProblemService);

  protected readonly open = computed(() => this.account() !== null);
  protected readonly password = signal('');
  protected readonly saving = signal(false);
  protected readonly errors = signal<Record<string, string>>({});
  protected readonly canSave = computed(() => this.password() !== '' && !this.saving());

  constructor() {
    keepOpenWhile(() => this.saving());
    // However the dialog closes, what was typed goes with it.
    effect(() => {
      if (this.account() === null) {
        untracked(() => {
          this.password.set('');
          this.errors.set({});
        });
      }
    });
  }

  protected closed(visible: boolean): void {
    if (!visible) {
      this.account.set(null);
    }
  }

  protected async save(): Promise<void> {
    const account = this.account();
    if (account === null || !this.canSave()) {
      return;
    }
    this.saving.set(true);
    this.errors.set({});
    try {
      const password = this.password();
      await this.accounts.reset(account.username, password);
      this.passwordSet.emit({ account, password });
      this.account.set(null);
    } catch (error) {
      if (this.account() === account) {
        this.errors.set(this.problems.report(error, { fields: true }).fields);
      } else {
        // The page closed the dialog, or opened it for another account, while the request was out:
        // a field error would sit under nothing, or under somebody else. A toast says it.
        this.problems.report(error);
      }
    } finally {
      this.saving.set(false);
    }
  }
}
