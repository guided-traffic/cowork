import { HttpErrorResponse } from '@angular/common/http';
import { isSignal, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Tooltip } from 'primeng/tooltip';
import type { MockInstance } from 'vitest';
import { Account, Me, Problem } from '../../api/models';
import { AccountsService } from '../../core/accounts.service';
import { AuthService } from '../../core/auth.service';
import { HARD_NAVIGATION, HardNavigation } from '../../core/hard-navigation';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { dateTime } from '../../shared/time';
import { Accounts, flagMeanings } from './accounts';
import { NewAccountDialog, roleMeanings } from './new-account-dialog';
import { ResetPasswordDialog } from './reset-password-dialog';

function account(username: string, overrides: Partial<Account> = {}): Account {
  return {
    id: `id-${username}`,
    username,
    display_name: `${username[0].toUpperCase()}${username.slice(1)} Person`,
    role: 'member',
    locked: false,
    password_change_required: false,
    deactivated_at: null,
    created_at: '2026-10-01T10:00:00Z',
    ...overrides,
  };
}

const ada = account('ada', { display_name: 'Ada Lovelace', role: 'admin' });
const sam = account('sam', {
  display_name: 'Sam Rivera',
  locked: true,
  password_change_required: true,
});
const kim = account('kim', {
  display_name: 'Kim Park',
  role: 'viewer',
  locked: true,
  deactivated_at: '2026-10-02T09:30:00Z',
});
const lee = account('lee', { display_name: 'Lee Wong', role: null });

const me = (username: string | null = 'ada'): Me => ({
  id: 'p1',
  display_name: 'Ada Lovelace',
  username,
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships: [{ role: 'admin', tenant: { slug: 'acme', name: 'Acme Corp' } }],
});

function refusal(status: number, title: string, detail: string) {
  const body: Problem = { type: 'about:blank', title, status, detail, code: 'forbidden' };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

describe('Accounts', () => {
  let list: WritableSignal<Account[]>;
  let loading: WritableSignal<boolean>;
  let error: WritableSignal<unknown>;
  let tenantSlug: WritableSignal<string | null>;
  let person: WritableSignal<Me | undefined>;
  let personLoading: WritableSignal<boolean>;
  let isAdmin: WritableSignal<boolean>;
  let reload: MockInstance<() => boolean>;
  let create: MockInstance<AccountsService['create']>;
  let reset: MockInstance<AccountsService['reset']>;
  let unlock: MockInstance<AccountsService['unlock']>;
  let deactivate: MockInstance<AccountsService['deactivate']>;
  let endSessions: MockInstance<AccountsService['endSessions']>;
  let navigate: MockInstance<HardNavigation>;

  beforeEach(() => {
    list = signal<Account[]>([ada, sam]);
    loading = signal(false);
    error = signal<unknown>(undefined);
    tenantSlug = signal<string | null>('acme');
    person = signal<Me | undefined>(me());
    personLoading = signal(false);
    isAdmin = signal(true);
    reload = vi.fn<() => boolean>().mockReturnValue(true);
    create = vi.fn<AccountsService['create']>().mockResolvedValue(sam);
    reset = vi.fn<AccountsService['reset']>().mockResolvedValue(undefined);
    unlock = vi.fn<AccountsService['unlock']>().mockResolvedValue(undefined);
    deactivate = vi.fn<AccountsService['deactivate']>().mockResolvedValue(undefined);
    endSessions = vi.fn<AccountsService['endSessions']>().mockResolvedValue(undefined);
    navigate = vi.fn<HardNavigation>();
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        {
          provide: AccountsService,
          useValue: {
            list,
            accounts: { isLoading: loading, error, reload },
            create,
            reset,
            unlock,
            deactivate,
            endSessions,
          },
        },
        {
          provide: SessionService,
          useValue: { tenant: tenantSlug, person, me: { isLoading: personLoading } },
        },
        { provide: TenantService, useValue: { isAdmin } },
        { provide: HARD_NAVIGATION, useValue: navigate },
        {
          provide: AuthService,
          useValue: {
            options: {
              hasValue: () => true,
              value: () => ({ local: true, oidc: false, password_min_length: 12 }),
            },
          },
        },
      ],
    });
  });

  // Whatever a test spied on goes back, also when an assertion of the test has failed.
  afterEach(() => vi.restoreAllMocks());

  async function render() {
    const fixture = TestBed.createComponent(Accounts);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields take their model a moment after they appear. */
  async function settle(fixture: ComponentFixture<Accounts>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<Accounts>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<Accounts>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const row = (fixture: ComponentFixture<Accounts>, username: string) =>
    el(fixture, `account-${username}`);
  const cells = (fixture: ComponentFixture<Accounts>, username: string) => [
    ...(row(fixture, username)?.querySelectorAll('td') ?? []),
  ];
  const testIds = (fixture: ComponentFixture<Accounts>, username: string) =>
    [...(row(fixture, username)?.querySelectorAll('[data-testid]') ?? [])].map((each) =>
      each.getAttribute('data-testid'),
    );
  const tooltipOf = (fixture: ComponentFixture<Accounts>, testId: string) =>
    fixture.debugElement
      .query(By.css(`[data-testid="${testId}"]`))
      .injector.get(Tooltip)
      .content();

  describe('the page', () => {
    it('is headed Accounts, says what the accounts are and offers a new one to an administrator', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Accounts');
      expect(host(fixture).querySelector('.lead')?.textContent).toContain(
        'The local accounts this tenant created',
      );
      expect(el(fixture, 'new-account')?.textContent?.trim()).toBe('New account');
    });

    it('shows the headers of the columns and a row per account', async () => {
      const fixture = await render();

      expect([...host(fixture).querySelectorAll('th')].map((header) => header.textContent)).toEqual(
        ['Username', 'Name', 'Role', 'Status', ''],
      );
      expect(el(fixture, 'accounts')).not.toBeNull();
      expect(row(fixture, 'ada')).not.toBeNull();
      expect(row(fixture, 'sam')).not.toBeNull();
      expect(el(fixture, 'accounts-empty')).toBeNull();
    });

    it('shows each account with its username, its name and its role', async () => {
      const fixture = await render();

      expect(cells(fixture, 'sam').map((cell) => cell.textContent?.trim())).toEqual([
        'sam',
        'Sam Rivera',
        'member',
        expect.stringContaining('locked'),
        '',
      ]);
    });

    it('follows the list when accounts arrive', async () => {
      list.set([]);
      const fixture = await render();
      expect(row(fixture, 'lee')).toBeNull();

      list.set([lee]);
      await settle(fixture);

      expect(row(fixture, 'lee')).not.toBeNull();
    });

    it('explains each role in a tooltip, and says so when an account has none any more', async () => {
      list.set([ada, lee]);
      const fixture = await render();

      const roleTip = (username: string) =>
        fixture.debugElement
          .query(By.css(`[data-testid="account-${username}"] .col-role span`))
          .injector.get(Tooltip)
          .content();
      expect(cells(fixture, 'ada')[2].textContent?.trim()).toBe('admin');
      expect(roleTip('ada')).toBe(roleMeanings.admin);
      expect(cells(fixture, 'lee')[2].textContent?.trim()).toBe('none');
      expect(roleTip('lee')).toBe('The tenant has no grant for this person any more');
    });
  });

  describe('the flags', () => {
    it('marks an account that is locked, and one that has to change its password, with their meaning', async () => {
      const fixture = await render();

      expect(el(fixture, 'flag-locked')?.textContent?.trim()).toBe('locked');
      expect(el(fixture, 'flag-password')?.textContent?.trim()).toBe('must change password');
      expect(tooltipOf(fixture, 'flag-locked')).toBe(flagMeanings.locked);
      expect(tooltipOf(fixture, 'flag-password')).toBe(flagMeanings.passwordChange);
      expect(testIds(fixture, 'ada')).not.toContain('flag-locked');
      expect(testIds(fixture, 'ada')).not.toContain('flag-password');
      expect(testIds(fixture, 'ada')).not.toContain('flag-deactivated');
    });

    it('marks a deactivated account, with the day since when', async () => {
      list.set([kim]);
      const fixture = await render();

      expect(el(fixture, 'flag-deactivated')?.textContent?.trim()).toBe('deactivated');
      expect(tooltipOf(fixture, 'flag-deactivated')).toBe(
        `Since ${dateTime('2026-10-02T09:30:00Z')}. ${flagMeanings.deactivated}`,
      );
    });

    it('takes the colour of each flag from a token of the preset, never a literal', async () => {
      list.set([sam, kim]);
      const fixture = await render();

      const accent = (testId: string) => el(fixture, testId)?.style.getPropertyValue('--accent');
      expect(accent('flag-locked')).toBe('var(--p-severity-high)');
      expect(accent('flag-password')).toBe('var(--p-state-analysed)');
      expect(accent('flag-deactivated')).toBe('var(--p-state-dropped)');
    });
  });

  describe('what a row offers', () => {
    it('offers another administrator a reset, an unlock where it is locked, ending the sessions and a deactivation', async () => {
      const fixture = await render();

      expect(testIds(fixture, 'sam').filter((id) => id?.startsWith('account-'))).toEqual([
        'account-reset-sam',
        'account-unlock-sam',
        'account-sessions-sam',
        'account-deactivate-sam',
      ]);
    });

    it('offers no unlock where the account is not locked', async () => {
      list.set([account('sam')]);
      const fixture = await render();

      expect(el(fixture, 'account-unlock-sam')).toBeNull();
      expect(el(fixture, 'account-reset-sam')).not.toBeNull();
    });

    it('names each button for the account it acts on, for a screen reader', async () => {
      const fixture = await render();

      const label = (testId: string) => el(fixture, testId)?.getAttribute('aria-label');
      expect(label('account-reset-sam')).toBe('Reset the password of sam');
      expect(label('account-unlock-sam')).toBe('Unlock sam');
      expect(label('account-sessions-sam')).toBe('End the sessions of sam');
      expect(label('account-deactivate-sam')).toBe('Deactivate sam');
    });

    it('offers the administrator no more on their own row than ending their sessions', async () => {
      list.set([{ ...ada, locked: true }]);
      const fixture = await render();

      expect(testIds(fixture, 'ada').filter((id) => id?.startsWith('account-'))).toEqual([
        'account-sessions-ada',
      ]);
      expect(cells(fixture, 'ada')[0].textContent).toContain('(you)');
    });

    it('does not mark anybody as the administrator when the person has no username', async () => {
      person.set(me(null));
      const fixture = await render();

      expect(cells(fixture, 'ada')[0].textContent).not.toContain('(you)');
      expect(el(fixture, 'account-deactivate-ada')).not.toBeNull();
    });

    it('does not mark anybody as the administrator while the person is not loaded', async () => {
      person.set(undefined);
      const fixture = await render();

      expect(cells(fixture, 'ada')[0].textContent).not.toContain('(you)');
    });

    it('offers nothing on a deactivated account, which nothing reactivates', async () => {
      list.set([kim]);
      const fixture = await render();

      expect(testIds(fixture, 'kim').filter((id) => id?.startsWith('account-'))).toEqual([]);
      expect(cells(fixture, 'kim')[4].textContent?.trim()).toBe('');
    });
  });

  describe('for anybody but an administrator', () => {
    it('shows a short notice instead of the accounts, and no way to make one', async () => {
      isAdmin.set(false);

      const fixture = await render();

      expect(el(fixture, 'accounts-notice')?.textContent?.trim()).toBe(
        'Only the administrators of this tenant manage its accounts.',
      );
      expect(el(fixture, 'accounts')).toBeNull();
      expect(el(fixture, 'new-account')).toBeNull();
      expect(host(fixture).querySelector('.lead')).toBeNull();
    });

    it('does not say so while the person is still loading, which would be wrong for an administrator', async () => {
      isAdmin.set(false);
      personLoading.set(true);

      const fixture = await render();

      expect(el(fixture, 'accounts-notice')).toBeNull();
      expect(host(fixture).querySelector('p-skeleton')).not.toBeNull();
      personLoading.set(false);
      await settle(fixture);
      expect(el(fixture, 'accounts-notice')).not.toBeNull();
      expect(host(fixture).querySelector('p-skeleton')).toBeNull();
    });

    it('keeps the table while the person of an administrator is loaded again', async () => {
      personLoading.set(true);

      const fixture = await render();

      expect(el(fixture, 'accounts')).not.toBeNull();
      expect(host(fixture).querySelector('p-skeleton')).toBeNull();
    });
  });

  describe('without accounts', () => {
    it('says there are none', async () => {
      list.set([]);

      const fixture = await render();

      expect(el(fixture, 'accounts-empty')?.textContent?.trim()).toBe('No accounts yet.');
    });

    it('does not say so while the accounts load', async () => {
      list.set([]);
      loading.set(true);

      const fixture = await render();
      expect(el(fixture, 'accounts-empty')).toBeNull();

      loading.set(false);
      await settle(fixture);
      expect(el(fixture, 'accounts-empty')?.textContent?.trim()).toBe('No accounts yet.');
    });

    it('says why the accounts could not be loaded, with the detail of the problem', async () => {
      list.set([]);
      error.set(refusal(403, 'Forbidden', 'Only an administrator lists accounts.'));

      const fixture = await render();

      expect(el(fixture, 'accounts-empty')?.textContent?.trim()).toBe(
        'The accounts could not be loaded: Only an administrator lists accounts.',
      );
    });

    it('says why with the title when the problem has no detail', async () => {
      list.set([]);
      error.set(refusal(403, 'Forbidden', ''));

      const fixture = await render();

      expect(el(fixture, 'accounts-empty')?.textContent?.trim()).toBe(
        'The accounts could not be loaded: Forbidden',
      );
    });
  });

  describe('coming to the page', () => {
    it('asks for the accounts again, because what the list holds is as old as the last visit', async () => {
      await render();

      expect(reload).toHaveBeenCalledOnce();
    });
  });

  describe('a new account', () => {
    const dialog = (fixture: ComponentFixture<Accounts>) =>
      fixture.debugElement.query(By.directive(NewAccountDialog));

    it('opens the dialog for it', async () => {
      const fixture = await render();
      expect(dialog(fixture).componentInstance.visible()).toBe(false);

      el(fixture, 'new-account')?.click();
      await settle(fixture);

      expect(dialog(fixture).componentInstance.visible()).toBe(true);
      expect(el(fixture, 'account-username')).not.toBeNull();
    });

    it('closes again when the dialog asks to be closed', async () => {
      const fixture = await render();
      el(fixture, 'new-account')?.click();
      await settle(fixture);

      dialog(fixture).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(dialog(fixture).componentInstance.visible()).toBe(false);
      expect(el(fixture, 'account-username')).toBeNull();
    });

    it('shows the temporary password once, when the account is made, and names the person it is for', async () => {
      const fixture = await render();

      dialog(fixture).triggerEventHandler('created', { account: sam, password: 'Zk7-temporary' });
      await settle(fixture);

      expect((el(fixture, 'secret-value') as HTMLInputElement).value).toBe('Zk7-temporary');
      const host = fixture.nativeElement as HTMLElement;
      expect(host.querySelector('.p-dialog-title')?.textContent).toContain(
        'Temporary password for sam',
      );
      const warning = host.querySelector('[role="alert"]')?.textContent ?? '';
      expect(warning).toContain('Shown once.');
      expect(warning).toContain('Give it to Sam Rivera yourself');
      expect(warning).toContain('They must change it at their first login');
    });

    it('holds the temporary password in one place only, and in none once the dialog is closed', async () => {
      const fixture = await render();
      const held = () =>
        Object.values(fixture.componentInstance)
          .filter((value) => isSignal(value))
          .map((value) => JSON.stringify((value as () => unknown)()) ?? '')
          .filter((json) => json.includes('Zk7-temporary'));
      dialog(fixture).triggerEventHandler('created', { account: sam, password: 'Zk7-temporary' });
      await settle(fixture);
      expect(held()).toEqual([JSON.stringify('Zk7-temporary')]);

      el(fixture, 'secret-done')?.click();
      await settle(fixture);

      expect(held()).toEqual([]);
    });

    it('forgets the temporary password when the person has stored it', async () => {
      const fixture = await render();
      dialog(fixture).triggerEventHandler('created', { account: sam, password: 'Zk7-temporary' });
      await settle(fixture);

      el(fixture, 'secret-done')?.click();
      await settle(fixture);

      expect(el(fixture, 'secret-value')).toBeNull();
      expect(fixture.componentInstance['secret']()).toBeNull();
      expect(document.body.innerHTML).not.toContain('Zk7-temporary');
    });
  });

  describe('resetting a password', () => {
    const dialog = (fixture: ComponentFixture<Accounts>) =>
      fixture.debugElement.query(By.directive(ResetPasswordDialog));

    it('opens the dialog for the account of the row', async () => {
      const fixture = await render();
      expect(dialog(fixture).componentInstance.account()).toBeNull();

      el(fixture, 'account-reset-sam')?.click();
      await settle(fixture);

      expect(dialog(fixture).componentInstance.account()).toBe(sam);
      expect(host(fixture).querySelector('.p-dialog-title')?.textContent).toBe(
        'Reset the password of sam',
      );
    });

    it('closes again without resetting anything when the dialog is cancelled', async () => {
      const fixture = await render();
      el(fixture, 'account-reset-sam')?.click();
      await settle(fixture);

      el(fixture, 'reset-cancel')?.click();
      await settle(fixture);

      expect(dialog(fixture).componentInstance.account()).toBeNull();
      expect(fixture.componentInstance['resetting']()).toBeNull();
      expect(el(fixture, 'reset-password')).toBeNull();
      expect(reset).not.toHaveBeenCalled();
    });

    it('shows the temporary password once, when it is set, and names the person it is for', async () => {
      const fixture = await render();

      dialog(fixture).triggerEventHandler('passwordSet', { account: sam, password: 'Qm4-another' });
      await settle(fixture);

      expect((el(fixture, 'secret-value') as HTMLInputElement).value).toBe('Qm4-another');
      expect(host(fixture).querySelector('[role="alert"]')?.textContent).toContain(
        'Give it to Sam Rivera yourself',
      );
    });
  });

  describe('a dialog and the tenant', () => {
    it('closes the dialogs and forgets a password shown, when the page turns to another tenant', async () => {
      const fixture = await render();
      el(fixture, 'new-account')?.click();
      el(fixture, 'account-reset-sam')?.click();
      fixture.componentInstance['issued']({ account: sam, password: 'Zk7-temporary' });
      await settle(fixture);
      expect(fixture.componentInstance['creating']()).toBe(true);
      expect(fixture.componentInstance['resetting']()).toBe(sam);
      expect(fixture.componentInstance['secret']()).toBe('Zk7-temporary');

      tenantSlug.set('globex');
      await settle(fixture);

      expect(fixture.componentInstance['creating']()).toBe(false);
      expect(fixture.componentInstance['resetting']()).toBeNull();
      expect(fixture.componentInstance['secret']()).toBeNull();
    });
  });

  describe('the acts of a row', () => {
    const toasts = () => vi.spyOn(TestBed.inject(MessageService), 'add');

    describe('unlocking', () => {
      it('unlocks the account of the row and says so', async () => {
        const add = toasts();
        const fixture = await render();

        el(fixture, 'account-unlock-sam')?.click();
        await settle(fixture);

        expect(unlock).toHaveBeenCalledExactlyOnceWith('sam');
        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({
            severity: 'success',
            summary: 'Account unlocked',
            detail: 'sam can try to log in again.',
          }),
        );
      });

      it('toasts the problem and says nothing of success when the server refuses', async () => {
        unlock.mockRejectedValue(refusal(404, 'Not found', 'No such account.'));
        const add = toasts();
        const fixture = await render();

        el(fixture, 'account-unlock-sam')?.click();
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({ summary: 'Not found', detail: 'No such account.' }),
        );
      });
    });

    describe('ending the sessions', () => {
      const dialog = () => document.body.querySelector('.p-confirmdialog');

      const press = (label: string) =>
        [...(dialog()?.querySelectorAll('button') ?? [])]
          .find((button) => button.textContent?.trim() === label)
          ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

      async function ask(fixture: ComponentFixture<Accounts>, username = 'sam') {
        el(fixture, `account-sessions-${username}`)?.click();
        await settle(fixture);
      }

      it('asks first, naming the account, and says that its tokens are not affected', async () => {
        const fixture = await render();

        await ask(fixture);

        expect(dialog()?.textContent).toContain('End the sessions of sam?');
        expect(dialog()?.textContent).toContain('Every session of sam ends');
        expect(dialog()?.textContent).toContain('Their tokens are not affected and keep working');
        expect(dialog()?.textContent).toContain('only deactivating the account revokes them');
        expect(endSessions).not.toHaveBeenCalled();
      });

      it('opens with the focus on the button that keeps the sessions', async () => {
        const fixture = await render();

        await ask(fixture);
        await new Promise((resolve) => setTimeout(resolve, 50));

        const focused = document.activeElement as HTMLElement | null;
        expect(focused?.textContent?.trim()).toBe('Keep them');
        expect(dialog()?.contains(focused)).toBe(true);
      });

      it('ends the sessions of the account of the row when it is confirmed, and says what ended and what did not', async () => {
        const add = toasts();
        const fixture = await render();
        await ask(fixture);

        press('End sessions');
        await settle(fixture);

        expect(endSessions).toHaveBeenCalledExactlyOnceWith('sam');
        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({
            severity: 'success',
            summary: 'Sessions ended',
            detail: 'Every session of sam has ended. Their tokens are not affected.',
          }),
        );
        expect(navigate).not.toHaveBeenCalled();
      });

      it('does not say that anybody is signed out everywhere, which the tokens of the account would contradict', async () => {
        const add = toasts();
        const fixture = await render();
        await ask(fixture);

        press('End sessions');
        await settle(fixture);

        expect(JSON.stringify(add.mock.calls)).not.toContain('everywhere');
      });

      it('does nothing when the person keeps the sessions', async () => {
        const add = toasts();
        const fixture = await render();
        await ask(fixture);

        press('Keep them');
        await settle(fixture);

        expect(endSessions).not.toHaveBeenCalled();
        expect(add).not.toHaveBeenCalled();
        expect(navigate).not.toHaveBeenCalled();
      });

      it('toasts the problem when the server refuses', async () => {
        endSessions.mockRejectedValue(refusal(403, 'Forbidden', 'Not for you.'));
        const add = toasts();
        const fixture = await render();
        await ask(fixture);

        press('End sessions');
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({ summary: 'Forbidden', detail: 'Not for you.' }),
        );
        expect(navigate).not.toHaveBeenCalled();
      });

      describe('on the own row of the administrator', () => {
        it('says that this session ends too, here at once and in every other browser, and that the tokens stay', async () => {
          const fixture = await render();

          await ask(fixture, 'ada');

          expect(dialog()?.textContent).toContain('End the sessions of ada?');
          expect(dialog()?.textContent).toContain('this one too');
          expect(dialog()?.textContent).toContain('signed out here at once');
          expect(dialog()?.textContent).toContain('every other browser and tab');
          expect(dialog()?.textContent).toContain('Your tokens are not affected');
          expect(endSessions).not.toHaveBeenCalled();
        });

        it('opens with the focus on the button that keeps the sessions', async () => {
          const fixture = await render();

          await ask(fixture, 'ada');
          await new Promise((resolve) => setTimeout(resolve, 50));

          expect((document.activeElement as HTMLElement | null)?.textContent?.trim()).toBe(
            'Keep them',
          );
        });

        it('ends the sessions and loads the login page as a new document, instead of leaving the page to find out', async () => {
          const add = toasts();
          const fixture = await render();
          await ask(fixture, 'ada');

          press('End sessions');
          await settle(fixture);

          expect(endSessions).toHaveBeenCalledExactlyOnceWith('ada');
          expect(navigate).toHaveBeenCalledExactlyOnceWith('/login');
          expect(add).not.toHaveBeenCalled();
        });

        it('stays on the page, and says why, when the server refuses', async () => {
          endSessions.mockRejectedValue(refusal(403, 'Forbidden', 'Not for you.'));
          const add = toasts();
          const fixture = await render();
          await ask(fixture, 'ada');

          press('End sessions');
          await settle(fixture);

          expect(navigate).not.toHaveBeenCalled();
          expect(add).toHaveBeenCalledExactlyOnceWith(
            expect.objectContaining({ summary: 'Forbidden' }),
          );
        });

        it('leaves the page only after the backend has ended the sessions', async () => {
          let finish: () => void = () => undefined;
          endSessions.mockReturnValue(new Promise<void>((resolve) => (finish = resolve)));
          const fixture = await render();
          await ask(fixture, 'ada');

          press('End sessions');
          await new Promise((resolve) => setTimeout(resolve));
          expect(navigate).not.toHaveBeenCalled();
          finish();
          await settle(fixture);

          expect(navigate).toHaveBeenCalledExactlyOnceWith('/login');
        });
      });
    });

    describe('deactivating', () => {
      const dialog = () => document.body.querySelector('.p-confirmdialog');

      const press = (label: string) =>
        [...(dialog()?.querySelectorAll('button') ?? [])]
          .find((button) => button.textContent?.trim() === label)
          ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

      async function ask(fixture: ComponentFixture<Accounts>) {
        el(fixture, 'account-deactivate-sam')?.click();
        await settle(fixture);
      }

      it('asks first, and says that it is for good and what stays', async () => {
        const fixture = await render();

        await ask(fixture);

        expect(dialog()?.textContent).toContain('Deactivate sam?');
        expect(dialog()?.textContent).toContain('nothing reactivates the account');
        expect(dialog()?.textContent).toContain('everything they did stay');
        expect(deactivate).not.toHaveBeenCalled();
      });

      it('offers the deactivation as a dangerous act', async () => {
        const fixture = await render();

        await ask(fixture);

        const accept = [...(dialog()?.querySelectorAll('button') ?? [])].find(
          (button) => button.textContent?.trim() === 'Deactivate',
        );
        expect(accept?.className).toContain('p-button-danger');
      });

      it('opens with the focus on the button that keeps the account, so that a second Enter does not deactivate it for good', async () => {
        const fixture = await render();

        await ask(fixture);
        await new Promise((resolve) => setTimeout(resolve, 50));

        const focused = document.activeElement as HTMLElement | null;
        expect(focused?.textContent?.trim()).toBe('Keep it');
        expect(dialog()?.contains(focused)).toBe(true);
      });

      it('deactivates the account and says so when it is confirmed', async () => {
        const add = toasts();
        const fixture = await render();
        await ask(fixture);

        press('Deactivate');
        await settle(fixture);

        expect(deactivate).toHaveBeenCalledExactlyOnceWith('sam');
        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({
            severity: 'success',
            summary: 'Account deactivated',
            detail: 'sam cannot log in any more.',
          }),
        );
      });

      it('does nothing when the person keeps the account', async () => {
        const add = toasts();
        const fixture = await render();
        await ask(fixture);

        press('Keep it');
        await settle(fixture);

        expect(deactivate).not.toHaveBeenCalled();
        expect(add).not.toHaveBeenCalled();
      });

      it('toasts the problem when the account cannot be deactivated', async () => {
        deactivate.mockRejectedValue(refusal(403, 'Forbidden', 'Not for your own account.'));
        const add = toasts();
        const fixture = await render();
        await ask(fixture);

        press('Deactivate');
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({ summary: 'Forbidden', detail: 'Not for your own account.' }),
        );
      });
    });
  });
});
