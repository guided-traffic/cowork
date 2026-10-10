import { computed, inject, Injectable, Injector, resource } from '@angular/core';
import { Api } from '../api/api';
import { createAccount } from '../api/fn/accounts/create-account';
import { deactivateAccount } from '../api/fn/accounts/deactivate-account';
import { endAccountSessions } from '../api/fn/accounts/end-account-sessions';
import { listAccounts } from '../api/fn/accounts/list-accounts';
import { resetAccountPassword } from '../api/fn/accounts/reset-account-password';
import { unlockAccount } from '../api/fn/accounts/unlock-account';
import { Account, AccountCreate } from '../api/models';
import { MembersService } from './members.service';
import { refresh } from './refresh';
import { SessionService } from './session.service';

/**
 * The local accounts the current tenant manages (docs/adr/0033), every page of them, and the acts
 * of its administrators on them. Only an administrator of the tenant may read them, so the list
 * is not asked for anybody else: it would be a `403` for every tenant they enter. None of the acts
 * is on the event stream, so each reloads the list itself.
 */
@Injectable({ providedIn: 'root' })
export class AccountsService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

  /**
   * The tenant of the page while the person administers it, and nothing else: only an administrator
   * may read the accounts, so for anybody else the list would be a `403` in every tenant they
   * enter. A resource loads again whenever its params function runs, so this is a computed: a
   * person who is loaded again with the same role leaves the accounts alone.
   */
  private readonly administered = computed(() => {
    const tenant = this.session.tenant();
    return tenant !== null && this.session.membership()?.role === 'admin' ? tenant : undefined;
  });

  readonly accounts = resource({
    params: () => this.administered(),
    loader: async ({ params: tenant }) => {
      const accounts: Account[] = [];
      let cursor: string | undefined;
      do {
        const page = await this.api.invoke(listAccounts, { team: tenant, cursor, limit: 200 });
        accounts.push(...page.items);
        cursor = page.next_cursor ?? undefined;
      } while (cursor);
      return accounts;
    },
  });

  readonly list = computed<Account[]>(() =>
    this.accounts.hasValue() ? this.accounts.value() : [],
  );

  /**
   * Creates the account with a temporary password the person changes at the first login. The key
   * is the form's, one for each content it holds: a retry of the same content sends the same key,
   * so an answer that was lost is answered again instead of being refused as taken (docs/adr/0045).
   * The account is a member of the tenant from this moment, so the members and the pickers that
   * read them are loaded again; `MembersService` is asked for only now, so that the page of the
   * accounts does not start a load of members by being open.
   */
  async create(body: AccountCreate, idempotencyKey: string): Promise<Account> {
    const account = await this.api.invoke(createAccount, {
      team: this.session.tenant() as string,
      'Idempotency-Key': idempotencyKey,
      body,
    });
    refresh(this.accounts, this.injector);
    refresh(this.injector.get(MembersService).members, this.injector);
    return account;
  }

  /** Sets a new temporary password; every session of the account ends (docs/adr/0033 D5). */
  async reset(username: string, temporaryPassword: string): Promise<void> {
    await this.api.invoke(resetAccountPassword, {
      team: this.session.tenant() as string,
      username,
      body: { temporary_password: temporaryPassword },
    });
    refresh(this.accounts, this.injector);
  }

  /** Forgets the failed logins and the lock of the account (docs/adr/0033 D6). */
  async unlock(username: string): Promise<void> {
    await this.api.invoke(unlockAccount, { team: this.session.tenant() as string, username });
    refresh(this.accounts, this.injector);
  }

  /** Deactivates the person for good: no login, no token, no session (docs/adr/0024 D5). */
  async deactivate(username: string): Promise<void> {
    await this.api.invoke(deactivateAccount, {
      team: this.session.tenant() as string,
      username,
    });
    refresh(this.accounts, this.injector);
  }

  /** Signs the account out everywhere, at once. */
  async endSessions(username: string): Promise<void> {
    await this.api.invoke(endAccountSessions, {
      team: this.session.tenant() as string,
      username,
    });
    refresh(this.accounts, this.injector);
  }
}
