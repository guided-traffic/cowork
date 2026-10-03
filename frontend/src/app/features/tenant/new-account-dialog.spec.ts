import { HttpErrorResponse } from '@angular/common/http';
import { isSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import { Account, Problem } from '../../api/models';
import { AccountsService } from '../../core/accounts.service';
import { accountUsername, NewAccountDialog, roleMeanings } from './new-account-dialog';
import { generatedLength, IssuedPassword, passwordAlphabet } from './temporary-password';

const made: Account = {
  id: 'id-ada',
  username: 'ada',
  display_name: 'Ada Lovelace',
  role: 'member',
  locked: false,
  password_change_required: true,
  deactivated_at: null,
  created_at: '2026-10-03T10:00:00Z',
};

function refusal(
  status: number,
  code: Problem['code'],
  errors: { pointer: string; message: string }[],
) {
  const body: Problem = {
    type: 'about:blank',
    title: 'The account is not valid',
    status,
    detail: 'Check the fields.',
    code,
    errors,
  };
  return new HttpErrorResponse({ status, statusText: body.title, error: body });
}

describe('accountUsername', () => {
  it.each(['a', 'ada', 'a1', '0day', 'a.b', 'a-b', 'a_b', 'ada.lovelace-2', 'a'.repeat(63)])(
    'accepts the username %s',
    (username) => {
      expect(accountUsername.test(username)).toBe(true);
    },
  );

  it.each([
    ['', 'nothing'],
    ['a'.repeat(64), '64 characters'],
    ['-ada', 'a start with a hyphen'],
    ['.ada', 'a start with a dot'],
    ['_ada', 'a start with an underscore'],
    ['Ada', 'an upper-case letter'],
    ['ada lovelace', 'a space'],
    ['ada@acme', 'an at sign'],
    ['ädä', 'a letter outside a to z'],
    [' ada', 'a leading space'],
    ['ada ', 'a trailing space'],
    ['ada\n', 'a trailing newline'],
  ])('rejects the username %j: %s', (username) => {
    expect(accountUsername.test(username)).toBe(false);
  });
});

describe('roleMeanings', () => {
  it('says something of each of the three roles', () => {
    expect(Object.keys(roleMeanings)).toEqual(['viewer', 'member', 'admin']);
    expect(Object.values(roleMeanings).every((meaning) => meaning.length > 0)).toBe(true);
  });
});

describe('NewAccountDialog', () => {
  let create: MockInstance<AccountsService['create']>;
  let warn: MockInstance<typeof console.warn>;

  beforeEach(() => {
    create = vi.fn<AccountsService['create']>().mockResolvedValue(made);
    warn = vi.spyOn(console, 'warn');
    TestBed.configureTestingModule({
      providers: [MessageService, { provide: AccountsService, useValue: { create } }],
    });
  });

  afterEach(() => {
    // A field that cannot register with the form it sits in is a warning of development builds
    // (NG01354); the dialog must not cause one.
    expect(warn.mock.calls.filter((call) => String(call[0]).includes('NG01354'))).toEqual([]);
    warn.mockRestore();
  });

  async function render(visible = true) {
    const fixture = TestBed.createComponent(NewAccountDialog);
    fixture.componentRef.setInput('visible', visible);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a submit started finish, and shows it; fields register with the form a moment later. */
  async function settle(fixture: ComponentFixture<NewAccountDialog>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<NewAccountDialog>) =>
    fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<NewAccountDialog>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  function typeInto(fixture: ComponentFixture<NewAccountDialog>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  const choose = (fixture: ComponentFixture<NewAccountDialog>, role: string) => {
    fixture.debugElement
      .query(By.css('[data-testid="account-role"]'))
      .triggerEventHandler('ngModelChange', role);
    fixture.detectChanges();
  };

  const submit = (fixture: ComponentFixture<NewAccountDialog>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const saveButton = (fixture: ComponentFixture<NewAccountDialog>) =>
    el(fixture, 'account-save') as HTMLButtonElement | null;

  function fill(
    fixture: ComponentFixture<NewAccountDialog>,
    username = 'ada',
    name = 'Ada Lovelace',
    password = 'a-temporary-one',
  ) {
    typeInto(fixture, 'account-username', username);
    typeInto(fixture, 'account-name', name);
    typeInto(fixture, 'account-password', password);
  }

  describe('the dialog', () => {
    it('is closed until it is asked to open', async () => {
      const fixture = await render(false);

      expect(el(fixture, 'account-username')).toBeNull();
    });

    it('asks for a username, a name, a role and a temporary password under the header of a new account', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('.p-dialog-title')?.textContent).toBe('New account');
      expect(el(fixture, 'account-username')).not.toBeNull();
      expect(el(fixture, 'account-name')).not.toBeNull();
      expect(el(fixture, 'account-role')).not.toBeNull();
      expect(el(fixture, 'account-password')).not.toBeNull();
      expect(el(fixture, 'account-password-generate')).not.toBeNull();
      expect(host(fixture).querySelector('.lead')?.textContent).toContain(
        'choose their own password at the first login',
      );
    });

    it('limits the username to 63 characters and the name to two hundred', async () => {
      const fixture = await render();

      expect(el(fixture, 'account-username')?.getAttribute('maxlength')).toBe('63');
      expect(el(fixture, 'account-name')?.getAttribute('maxlength')).toBe('200');
    });

    it('closes when the dialog asks to be closed', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(el(fixture, 'account-username')).toBeNull();
    });

    it('closes with Cancel, without creating anything', async () => {
      const fixture = await render();
      fill(fixture);

      el(fixture, 'account-cancel')?.click();
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(create).not.toHaveBeenCalled();
    });
  });

  describe('the role', () => {
    it('offers the three roles, and a member is the one that is chosen', async () => {
      const fixture = await render();

      const select = fixture.debugElement.query(By.css('[data-testid="account-role"]'))
        .componentInstance as Select;
      expect(select.options()).toEqual(['viewer', 'member', 'admin']);
      expect(host(fixture).querySelector('.p-select-label')?.textContent?.trim()).toBe('member');
    });

    it('says what the chosen role may do, and follows the choice', async () => {
      const fixture = await render();
      expect(el(fixture, 'account-role-meaning')?.textContent?.trim()).toBe(roleMeanings.member);

      choose(fixture, 'admin');
      expect(el(fixture, 'account-role-meaning')?.textContent?.trim()).toBe(roleMeanings.admin);

      choose(fixture, 'viewer');
      expect(el(fixture, 'account-role-meaning')?.textContent?.trim()).toBe(roleMeanings.viewer);
    });

    it('names the select for a screen reader', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('#account-role-label')?.textContent).toBe('Role');
      const select = fixture.debugElement.query(By.css('[data-testid="account-role"]'))
        .componentInstance as Select;
      expect(select.ariaLabelledBy()).toBe('account-role-label');
    });
  });

  describe('what an account needs', () => {
    it('cannot be created without a valid username, a name and a password', async () => {
      const fixture = await render();
      expect(saveButton(fixture)?.disabled).toBe(true);

      typeInto(fixture, 'account-username', 'ada');
      expect(saveButton(fixture)?.disabled).toBe(true);
      typeInto(fixture, 'account-name', 'Ada Lovelace');
      expect(saveButton(fixture)?.disabled).toBe(true);
      typeInto(fixture, 'account-password', 'a-temporary-one');
      expect(saveButton(fixture)?.disabled).toBe(false);
      typeInto(fixture, 'account-name', '   ');
      expect(saveButton(fixture)?.disabled).toBe(true);
    });

    it.each(['-ada', 'ada lovelace', 'ada@acme', 'a'.repeat(64)])(
      'cannot be created with the username %j, which is no username',
      async (username) => {
        const fixture = await render();

        fill(fixture, username);
        submit(fixture);
        await settle(fixture);

        expect(saveButton(fixture)?.disabled).toBe(true);
        expect(create).not.toHaveBeenCalled();
      },
    );

    it('takes a username typed in upper case as the same username in lower case', async () => {
      const fixture = await render();

      fill(fixture, 'Ada.Lovelace');
      await settle(fixture);

      expect((el(fixture, 'account-username') as HTMLInputElement).value).toBe('ada.lovelace');
      submit(fixture);
      await settle(fixture);
      expect(create.mock.calls[0][0].username).toBe('ada.lovelace');
    });

    it('does not need a password of any length: that is the server rule, and the server says it', async () => {
      const fixture = await render();

      fill(fixture, 'ada', 'Ada', 'x');

      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('cannot be created while another one is on its way', async () => {
      let finish: (account: Account) => void = () => undefined;
      create.mockReturnValue(new Promise<Account>((resolve) => (finish = resolve)));
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      submit(fixture);
      await settle(fixture);
      expect(create).toHaveBeenCalledOnce();
      expect(saveButton(fixture)?.disabled).toBe(true);
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();
      finish(made);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });

  describe('the username while it is typed', () => {
    const hint = (fixture: ComponentFixture<NewAccountDialog>) =>
      el(fixture, 'account-username-hint');
    const field = (fixture: ComponentFixture<NewAccountDialog>) => el(fixture, 'account-username');

    it('says what a username is, quietly, before anything is typed and while it is valid', async () => {
      const fixture = await render();

      expect(hint(fixture)?.textContent).toContain('Lower-case letters, digits, dots, hyphens');
      expect(hint(fixture)?.classList).toContain('muted');
      expect(hint(fixture)?.classList).not.toContain('error');
      expect(field(fixture)?.getAttribute('aria-invalid')).toBe('false');

      typeInto(fixture, 'account-username', 'ada');

      expect(hint(fixture)?.classList).toContain('muted');
      expect(field(fixture)?.getAttribute('aria-invalid')).toBe('false');
    });

    it('turns the same sentence into an error as soon as what is typed cannot be a username', async () => {
      const fixture = await render();

      typeInto(fixture, 'account-username', 'ada lovelace');

      expect(hint(fixture)?.classList).toContain('error');
      expect(hint(fixture)?.classList).not.toContain('muted');
      expect(field(fixture)?.getAttribute('aria-invalid')).toBe('true');
      expect(field(fixture)?.getAttribute('aria-describedby')).toBe('account-username-hint');

      typeInto(fixture, 'account-username', 'ada-lovelace');

      expect(hint(fixture)?.classList).toContain('muted');
      expect(field(fixture)?.getAttribute('aria-invalid')).toBe('false');
    });

    it('does not call spaces around a username an error, because they are left out', async () => {
      const fixture = await render();

      typeInto(fixture, 'account-username', '  ada  ');

      expect(field(fixture)?.getAttribute('aria-invalid')).toBe('false');
    });
  });

  describe('the password', () => {
    it('is filled with a generated one by the button, which is enough to create the account', async () => {
      const fixture = await render();
      typeInto(fixture, 'account-username', 'ada');
      typeInto(fixture, 'account-name', 'Ada Lovelace');

      el(fixture, 'account-password-generate')?.click();
      await settle(fixture);
      expect(saveButton(fixture)?.disabled).toBe(false);
      submit(fixture);
      await settle(fixture);

      const sent = create.mock.calls[0][0].temporary_password;
      expect(sent).toHaveLength(generatedLength);
      expect([...sent].every((character) => passwordAlphabet.includes(character))).toBe(true);
    });
  });

  describe('creating', () => {
    it('creates a member with the username, the name and the password that were typed, trimmed', async () => {
      const fixture = await render();
      fill(fixture, '  ada  ', '  Ada Lovelace  ', ' a temporary one ');

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith({
        username: 'ada',
        display_name: 'Ada Lovelace',
        role: 'member',
        temporary_password: ' a temporary one ',
      });
    });

    it('creates the account with the role that was chosen', async () => {
      const fixture = await render();
      fill(fixture);
      choose(fixture, 'admin');

      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][0].role).toBe('admin');
    });

    it('hands the account and the password on, for the page to show once, and closes', async () => {
      const issued: IssuedPassword[] = [];
      const fixture = await render();
      fixture.componentInstance.created.subscribe((each) => issued.push(each));
      fill(fixture, 'ada', 'Ada Lovelace', 'a-temporary-one');

      submit(fixture);
      await settle(fixture);

      expect(issued).toEqual([{ account: made, password: 'a-temporary-one' }]);
      expect(fixture.componentInstance.visible()).toBe(false);
    });

    it('keeps the password in no signal of the dialog once the account is made', async () => {
      const fixture = await render();
      fill(fixture, 'ada', 'Ada Lovelace', 'a-temporary-one');
      submit(fixture);
      await settle(fixture);

      const held = Object.values(fixture.componentInstance)
        .filter((value) => isSignal(value))
        .map((value) => JSON.stringify((value as () => unknown)()) ?? '')
        .filter((json) => json.includes('a-temporary-one'));
      expect(held).toEqual([]);
    });

    it('forgets what was typed once it is closed, the password above all', async () => {
      const fixture = await render();
      fill(fixture, 'ada', 'Ada Lovelace', 'a-temporary-one');
      choose(fixture, 'admin');
      submit(fixture);
      await settle(fixture);

      fixture.componentInstance.visible.set(true);
      await settle(fixture);

      expect((el(fixture, 'account-username') as HTMLInputElement).value).toBe('');
      expect((el(fixture, 'account-name') as HTMLInputElement).value).toBe('');
      expect((el(fixture, 'account-password') as HTMLInputElement).value).toBe('');
      expect(host(fixture).querySelector('.p-select-label')?.textContent?.trim()).toBe('member');
    });

    it('forgets what was typed when the dialog is cancelled, however it is closed', async () => {
      const fixture = await render();
      fill(fixture, 'ada', 'Ada Lovelace', 'a-temporary-one');

      el(fixture, 'account-cancel')?.click();
      await settle(fixture);
      fixture.componentInstance.visible.set(true);
      await settle(fixture);

      expect((el(fixture, 'account-username') as HTMLInputElement).value).toBe('');
      expect((el(fixture, 'account-password') as HTMLInputElement).value).toBe('');
    });
  });

  describe('an account that the server refuses', () => {
    it('shows the problem of the username beside the username, and keeps the dialog and what was typed', async () => {
      create.mockRejectedValue(
        refusal(409, 'username_taken', [{ pointer: '/username', message: 'is taken' }]),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'account-username-error')?.textContent).toBe('is taken');
      expect(fixture.componentInstance.visible()).toBe(true);
      expect((el(fixture, 'account-username') as HTMLInputElement).value).toBe('ada');
      expect((el(fixture, 'account-name') as HTMLInputElement).value).toBe('Ada Lovelace');
      expect((el(fixture, 'account-password') as HTMLInputElement).value).toBe('a-temporary-one');
      expect(add).not.toHaveBeenCalled();
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('shows the problem of the name, the role and the password beside their fields', async () => {
      create.mockRejectedValue(
        refusal(422, 'validation_failed', [
          { pointer: '/display_name', message: 'must not be blank' },
          { pointer: '/role', message: 'is not a role' },
          { pointer: '/temporary_password', message: 'must be at least 12 characters' },
        ]),
      );
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'account-name-error')?.textContent).toBe('must not be blank');
      expect(
        host(fixture).querySelector('.field small.error:not([data-testid])')?.textContent,
      ).toBe('is not a role');
      expect(el(fixture, 'temporary-password-error')?.textContent).toBe(
        'must be at least 12 characters',
      );
    });

    it('toasts a problem that names no field, such as an unreachable backend', async () => {
      create.mockRejectedValue(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ severity: 'error', summary: 'The backend cannot be reached' }),
      );
      expect(host(fixture).querySelector('small.error')).toBeNull();
    });

    it('shows no old problem when the next attempt is made, and creates the account when it works', async () => {
      create.mockRejectedValueOnce(
        refusal(409, 'username_taken', [{ pointer: '/username', message: 'is taken' }]),
      );
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      expect(el(fixture, 'account-username-error')).not.toBeNull();

      typeInto(fixture, 'account-username', 'ada2');
      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledTimes(2);
      expect(create.mock.calls[1][0].username).toBe('ada2');
      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });
});
