import { computed, inject, Injectable, Injector, resource } from '@angular/core';
import { Api } from '../api/api';
import {
  createAccount,
  deactivateAccount,
  endAccountSessions,
  listAccounts,
  resetAccountPassword,
  unlockAccount,
} from '../api/functions';
import { Account, AccountCreate } from '../api/models';
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
        const page = await this.api.invoke(listAccounts, { tenant, cursor, limit: 200 });
        accounts.push(...page.items);
        cursor = page.next_cursor ?? undefined;
      } while (cursor);
      return accounts;
    },
  });

  readonly list = computed<Account[]>(() =>
    this.accounts.hasValue() ? this.accounts.value() : [],
  );

  /** Creates the account with a temporary password the person changes at the first login. */
  async create(body: AccountCreate): Promise<Account> {
    const account = await this.api.invoke(createAccount, {
      tenant: this.session.tenant() as string,
      'Idempotency-Key': crypto.randomUUID(),
      body,
    });
    refresh(this.accounts, this.injector);
    return account;
  }

  /** Sets a new temporary password; every session of the account ends (docs/adr/0033 D5). */
  async reset(username: string, temporaryPassword: string): Promise<void> {
    await this.api.invoke(resetAccountPassword, {
      tenant: this.session.tenant() as string,
      username,
      body: { temporary_password: temporaryPassword },
    });
    refresh(this.accounts, this.injector);
  }

  /** Forgets the failed logins and the lock of the account (docs/adr/0033 D6). */
  async unlock(username: string): Promise<void> {
    await this.api.invoke(unlockAccount, { tenant: this.session.tenant() as string, username });
    refresh(this.accounts, this.injector);
  }

  /** Deactivates the person for good: no login, no token, no session (docs/adr/0024 D5). */
  async deactivate(username: string): Promise<void> {
    await this.api.invoke(deactivateAccount, {
      tenant: this.session.tenant() as string,
      username,
    });
    refresh(this.accounts, this.injector);
  }

  /** Signs the account out everywhere, at once. */
  async endSessions(username: string): Promise<void> {
    await this.api.invoke(endAccountSessions, {
      tenant: this.session.tenant() as string,
      username,
    });
    refresh(this.accounts, this.injector);
  }
}
