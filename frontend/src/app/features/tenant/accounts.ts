import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  signal,
  untracked,
} from '@angular/core';
import { ConfirmationService, MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { ConfirmDialog } from 'primeng/confirmdialog';
import { Skeleton } from 'primeng/skeleton';
import { TableModule } from 'primeng/table';
import { Tooltip } from 'primeng/tooltip';
import { Account, Role } from '../../api/models';
import { AccountsService } from '../../core/accounts.service';
import { HARD_NAVIGATION } from '../../core/hard-navigation';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { SecretDialog } from '../../shared/secret-dialog';
import { dateTime } from '../../shared/time';
import { NewAccountDialog, roleMeanings } from './new-account-dialog';
import { ResetPasswordDialog } from './reset-password-dialog';
import { IssuedPassword } from './temporary-password';

/** What each flag of an account means, for the tooltip beside it. */
export const flagMeanings = {
  locked:
    'Failed logins locked the account. Unlock it, or wait: a lock ends by itself when the installation lets it.',
  passwordChange: 'Its person has not changed the temporary password yet.',
  deactivated: 'No login, no token and no session; nothing reactivates it.',
};

/**
 * The local accounts the tenant manages, for its administrators (docs/adr/0033): creating one with
 * a temporary password, resetting a password, unlocking, ending the sessions and deactivating. A
 * temporary password is shown once, in a dialog, and held nowhere else. The server decides who may
 * do what; the page offers only what it will accept: nothing on the administrator's own row but
 * ending their sessions, and nothing on a deactivated one. Ending sessions and deactivating ask
 * first, with the focus on the button that does nothing. Ending the sessions of the
 * administrator's own account ends the one this page runs in, so it is followed by a new document
 * at the login page.
 */
@Component({
  selector: 'app-accounts',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    ConfirmDialog,
    NewAccountDialog,
    ResetPasswordDialog,
    SecretDialog,
    Skeleton,
    TableModule,
    Tooltip,
  ],
  providers: [ConfirmationService],
  templateUrl: './accounts.html',
  styleUrl: './accounts.scss',
})
export class Accounts {
  protected readonly accounts = inject(AccountsService);
  protected readonly session = inject(SessionService);
  protected readonly tenant = inject(TenantService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly confirm = inject(ConfirmationService);
  private readonly navigate = inject(HARD_NAVIGATION);

  protected readonly flags = flagMeanings;
  protected readonly dateTime = dateTime;

  protected readonly creating = signal(false);
  protected readonly resetting = signal<Account | null>(null);
  /** The temporary password being shown, and nothing else of it is kept. */
  protected readonly secret = signal<string | null>(null);
  protected readonly issuedTo = signal<Account | null>(null);

  protected readonly failure = computed(() => {
    const error = this.accounts.accounts.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The accounts could not be loaded: ${problem.detail || problem.title}`;
  });

  constructor() {
    // What the list holds is as old as the last visit: ask again on the way in. A load that is
    // under way already is a fresh one, and `reload` leaves it alone.
    this.accounts.accounts.reload();
    // A dialog belongs to the tenant it was opened in; another tenant's page starts clean.
    effect(() => {
      this.session.tenant();
      untracked(() => {
        this.creating.set(false);
        this.resetting.set(null);
        this.secret.set(null);
      });
    });
  }

  protected roleMeaning(role: Role): string {
    return roleMeanings[role];
  }

  /** The administrator's own row: a reset, an unlock and a deactivation are refused for it. */
  protected isOwn(account: Account): boolean {
    return account.username === this.session.person()?.username;
  }

  protected issued({ account, password }: IssuedPassword): void {
    this.issuedTo.set(account);
    this.secret.set(password);
  }

  protected unlock(account: Account): void {
    void this.run(
      () => this.accounts.unlock(account.username),
      this.said('Account unlocked', `${account.username} can try to log in again.`),
    );
  }

  /**
   * Ends every session of the account, which leaves its tokens as they are: a token made from a
   * stolen session keeps working, and only deactivating the account revokes it. The text says so.
   */
  protected endSessions(account: Account): void {
    const own = this.isOwn(account);
    this.confirm.confirm({
      header: `End the sessions of ${account.username}?`,
      message: own
        ? 'Every session of your account ends, this one too: you are signed out here at once, ' +
          'and in every other browser and tab. Your tokens are not affected.'
        : `Every session of ${account.username} ends, and they sign in again. Their tokens are not ` +
          'affected and keep working; only deactivating the account revokes them.',
      acceptLabel: 'End sessions',
      rejectLabel: 'Keep them',
      defaultFocus: 'reject',
      accept: () =>
        this.run(
          () => this.accounts.endSessions(account.username),
          own
            ? () => this.navigate('/login')
            : this.said(
                'Sessions ended',
                `Every session of ${account.username} has ended. Their tokens are not affected.`,
              ),
        ),
    });
  }

  protected deactivate(account: Account): void {
    this.confirm.confirm({
      header: `Deactivate ${account.username}?`,
      message:
        'No login, no token and no session from now on, and nothing reactivates the account. ' +
        'The person, their grants and everything they did stay.',
      acceptLabel: 'Deactivate',
      rejectLabel: 'Keep it',
      acceptButtonProps: { severity: 'danger' },
      // Nothing reactivates it: an Enter that comes a moment late must not confirm it.
      defaultFocus: 'reject',
      accept: () =>
        this.run(
          () => this.accounts.deactivate(account.username),
          this.said('Account deactivated', `${account.username} cannot log in any more.`),
        ),
    });
  }

  /** The toast that says an act is done. */
  private said(summary: string, detail: string): () => void {
    return () => this.messages.add({ severity: 'success', summary, detail, life: 4000 });
  }

  private async run(act: () => Promise<void>, done: () => void): Promise<void> {
    try {
      await act();
      done();
    } catch (error) {
      this.problems.report(error);
    }
  }
}
