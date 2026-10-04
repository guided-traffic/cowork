import { HttpErrorResponse } from '@angular/common/http';
import { isSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import { Account, Problem } from '../../api/models';
import { AccountsService } from '../../core/accounts.service';
import { AuthService } from '../../core/auth.service';
import { accountUsername, NewAccountDialog } from './new-account-dialog';
import { roleMeanings } from './roles';
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

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

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
      providers: [
        MessageService,
        { provide: AccountsService, useValue: { create } },
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

  afterEach(() => {
    // Restore first, so that an assertion that fails does not leave the spy behind. A field that
    // cannot register with the form it sits in is a warning of development builds (NG01354); the
    // dialog must not cause one.
    const warnings = warn.mock.calls.filter((call) => String(call[0]).includes('NG01354'));
    warn.mockRestore();
    expect(warnings).toEqual([]);
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

  const cancelButton = (fixture: ComponentFixture<NewAccountDialog>) =>
    el(fixture, 'account-cancel') as HTMLButtonElement | null;

  /** A key press as the browser makes one: aimed at the focused element, on its way up to the document. */
  const press = (key: string, target: EventTarget = document.body) =>
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }));

  const mask = () => document.querySelector('.p-dialog-mask') as HTMLElement | null;

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
        'choose their own password at the first sign-in',
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

    it('closes with Escape, with the cross and with a click beside it while nothing is running', async () => {
      const fixture = await render();
      expect(document.querySelector('.p-dialog-close-button')).not.toBeNull();

      press('Escape');
      await settle(fixture);
      expect(fixture.componentInstance.visible()).toBe(false);

      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      mask()?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      await settle(fixture);
      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });

  describe('the fields', () => {
    it('gives the username a neutral name and tells password managers to leave it alone, so that the form does not read as a sign-up', async () => {
      const fixture = await render();

      const field = el(fixture, 'account-username') as HTMLInputElement;
      expect(field.name).toBe('account-username');
      expect(field.getAttribute('autocomplete')).toBe('off');
      expect(field.getAttribute('spellcheck')).toBe('false');
      expect(field.hasAttribute('data-1p-ignore')).toBe(true);
      expect(field.getAttribute('data-lpignore')).toBe('true');
      expect(field.hasAttribute('data-bwignore')).toBe(true);
      expect(field.getAttribute('data-form-type')).toBe('other');
    });

    it('has no field that a browser could take for a password, next to the username', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('input[type="password"]')).toBeNull();
      expect(host(fixture).querySelector('input[name="username"]')).toBeNull();
      expect(host(fixture).querySelector('input[autocomplete="new-password"]')).toBeNull();
    });

    it('names each text field by a label of its own, whose hint and error are not part of the name', async () => {
      const fixture = await render();

      const label = (text: string) =>
        [...host(fixture).querySelectorAll('label')].find((each) => each.textContent === text);
      expect((label('Username') as HTMLLabelElement).htmlFor).toBe('account-username-input');
      expect((label('Name') as HTMLLabelElement).htmlFor).toBe('account-name-input');
      expect((el(fixture, 'account-username') as HTMLInputElement).id).toBe(
        'account-username-input',
      );
      expect((el(fixture, 'account-name') as HTMLInputElement).id).toBe('account-name-input');
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
      expect(generatedLength).toBe(24);
      expect([...sent].every((character) => passwordAlphabet.includes(character))).toBe(true);
    });
  });

  describe('creating', () => {
    it('creates a member with the username, the name and the password that were typed, trimmed', async () => {
      const fixture = await render();
      fill(fixture, '  ada  ', '  Ada Lovelace  ', ' a temporary one ');

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith(
        {
          username: 'ada',
          display_name: 'Ada Lovelace',
          role: 'member',
          temporary_password: ' a temporary one ',
        },
        expect.stringMatching(uuid),
      );
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
      expect(el(fixture, 'account-role-error')?.textContent).toBe('is not a role');
      expect(el(fixture, 'account-password-error')?.textContent).toBe(
        'must be at least 12 characters',
      );
    });

    describe('for assistive technology', () => {
      const refused = async () => {
        create.mockRejectedValue(
          refusal(422, 'validation_failed', [
            { pointer: '/username', message: 'is taken' },
            { pointer: '/display_name', message: 'must not be blank' },
            { pointer: '/role', message: 'is not a role' },
            { pointer: '/temporary_password', message: 'must be at least 12 characters' },
          ]),
        );
        const fixture = await render();
        fill(fixture);
        submit(fixture);
        await settle(fixture);
        return fixture;
      };
      const combobox = (fixture: ComponentFixture<NewAccountDialog>) =>
        el(fixture, 'account-role')?.querySelector('[role="combobox"]');

      it('marks each refused field as invalid, with the text that says why, which is an alert', async () => {
        const fixture = await refused();

        for (const [field, error] of [
          ['account-username', 'account-username-error'],
          ['account-name', 'account-name-error'],
          ['account-password', 'account-password-error'],
        ]) {
          const input = el(fixture, field) as HTMLInputElement;
          expect(input.getAttribute('aria-invalid'), field).toBe('true');
          expect(input.getAttribute('aria-describedby')?.split(' '), field).toContain(error);
          expect(el(fixture, error)?.id, error).toBe(error);
          expect(el(fixture, error)?.getAttribute('role'), error).toBe('alert');
        }
        expect(combobox(fixture)?.getAttribute('aria-invalid')).toBe('true');
        expect(combobox(fixture)?.getAttribute('aria-describedby')?.split(' ')).toContain(
          'account-role-error',
        );
        expect(el(fixture, 'account-role-error')?.getAttribute('role')).toBe('alert');
      });

      it('keeps the hint of the username in the description beside the error, and claims no validity while an error shows', async () => {
        const fixture = await refused();

        const input = el(fixture, 'account-username') as HTMLInputElement;
        expect(input.getAttribute('aria-describedby')).toBe(
          'account-username-hint account-username-error',
        );
        expect(input.getAttribute('aria-invalid')).not.toBe('false');
        for (const id of input.getAttribute('aria-describedby')?.split(' ') ?? []) {
          expect(host(fixture).querySelector(`#${id}`), id).not.toBeNull();
        }
      });

      it('marks the select invalid in its style as well', async () => {
        const fixture = await refused();

        const select = fixture.debugElement.query(By.css('[data-testid="account-role"]'))
          .componentInstance as Select;
        expect(select.invalid()).toBe(true);
      });

      it('takes it all away again with the next attempt, and leaves no field claiming to be invalid', async () => {
        const fixture = await refused();
        let finish: (account: Account) => void = () => undefined;
        create.mockReturnValue(new Promise<Account>((resolve) => (finish = resolve)));

        submit(fixture);
        await settle(fixture);

        for (const field of ['account-username', 'account-name', 'account-password']) {
          const input = el(fixture, field) as HTMLInputElement;
          expect(input.getAttribute('aria-invalid'), field).toBe('false');
          expect(input.getAttribute('aria-describedby') ?? '', field).not.toContain('-error');
        }
        expect(combobox(fixture)?.hasAttribute('aria-invalid')).toBe(false);
        finish(made);
        await settle(fixture);
      });
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

  describe('while the request is out', () => {
    let finish: (account: Account) => void;
    let fail: (error: unknown) => void;

    async function sending() {
      create.mockReturnValue(
        new Promise<Account>((resolve, reject) => {
          finish = resolve;
          fail = reject;
        }),
      );
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      return fixture;
    }

    it('cannot be closed with Cancel, with the cross, with Escape or with a click beside it', async () => {
      const fixture = await sending();

      expect(cancelButton(fixture)?.disabled).toBe(true);
      cancelButton(fixture)?.click();
      expect(document.querySelector('.p-dialog-close-button')).toBeNull();
      press('Escape');
      press('Escape', el(fixture, 'account-username') as HTMLElement);
      mask()?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(true);
      expect((el(fixture, 'account-username') as HTMLInputElement).value).toBe('ada');
      finish(made);
      await settle(fixture);
    });

    it('shows a refusal in the form that sent it, because the form is still there', async () => {
      const fixture = await sending();
      press('Escape');

      fail(refusal(409, 'username_taken', [{ pointer: '/username', message: 'is taken' }]));
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(true);
      expect(el(fixture, 'account-username-error')?.textContent).toBe('is taken');
      expect((el(fixture, 'account-username') as HTMLInputElement).value).toBe('ada');
    });

    it('can be closed again once the refusal is there, with Cancel, the cross and Escape', async () => {
      const fixture = await sending();
      fail(refusal(409, 'username_taken', [{ pointer: '/username', message: 'is taken' }]));
      await settle(fixture);

      expect(cancelButton(fixture)?.disabled).toBe(false);
      expect(document.querySelector('.p-dialog-close-button')).not.toBeNull();
      press('Escape');
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
    });

    it('closes by itself when the account is made, and the page gets the password', async () => {
      const issued: IssuedPassword[] = [];
      const fixture = await sending();
      fixture.componentInstance.created.subscribe((each) => issued.push(each));

      finish(made);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(issued).toEqual([{ account: made, password: 'a-temporary-one' }]);
    });

    it('toasts a refusal that arrives after the page has closed the dialog, and keeps no error for the next form', async () => {
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await sending();

      // The page leaves the tenant and closes the dialog it opened, whatever the request is doing.
      fixture.componentInstance.visible.set(false);
      await settle(fixture);
      fail(refusal(409, 'username_taken', [{ pointer: '/username', message: 'is taken' }]));
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ summary: 'The account is not valid' }),
      );
      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      expect(el(fixture, 'account-username-error')).toBeNull();
      expect(
        (el(fixture, 'account-username') as HTMLInputElement).getAttribute('aria-invalid'),
      ).toBe('false');
    });
  });

  describe('the key of the act (docs/adr/0045)', () => {
    const keyOf = (call: number) => create.mock.calls[call][1];
    const lost = () => new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' });

    it('is a UUID of its own, and not the same one for two forms', async () => {
      const first = await render();
      fill(first);
      submit(first);
      await settle(first);
      const second = await render();
      fill(second);
      submit(second);
      await settle(second);

      expect(keyOf(0)).toMatch(uuid);
      expect(keyOf(1)).toMatch(uuid);
      expect(keyOf(1)).not.toBe(keyOf(0));
    });

    it('is the same for a retry of the same content after a network failure, so that a lost answer is answered again', async () => {
      create.mockRejectedValueOnce(lost());
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);
      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledTimes(2);
      expect(keyOf(1)).toBe(keyOf(0));
      expect(create.mock.calls[1][0]).toEqual(create.mock.calls[0][0]);
    });

    it.each([
      ['the username', 'account-username', 'ada2'],
      ['the name', 'account-name', 'Ada L. Lovelace'],
      ['the password', 'account-password', 'another-temporary-one'],
    ])(
      'is another one as soon as %s is changed, because the body is another',
      async (_what, field, value) => {
        create.mockRejectedValueOnce(lost());
        const fixture = await render();
        fill(fixture);
        submit(fixture);
        await settle(fixture);

        typeInto(fixture, field, value);
        submit(fixture);
        await settle(fixture);

        expect(create).toHaveBeenCalledTimes(2);
        expect(keyOf(1)).not.toBe(keyOf(0));
      },
    );

    it('is another one as soon as the role is changed', async () => {
      create.mockRejectedValueOnce(lost());
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);

      choose(fixture, 'admin');
      submit(fixture);
      await settle(fixture);

      expect(keyOf(1)).not.toBe(keyOf(0));
    });

    it('is another one once the content was changed at all, even when it is changed back: the key follows changes', async () => {
      create.mockRejectedValueOnce(lost());
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      typeInto(fixture, 'account-name', 'Somebody else');
      typeInto(fixture, 'account-name', 'Ada Lovelace');
      submit(fixture);
      await settle(fixture);

      expect(keyOf(1)).not.toBe(keyOf(0));
    });

    it('is reset by closing the form: the same content typed again into a new form is a new act', async () => {
      create.mockRejectedValueOnce(lost());
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);

      cancelButton(fixture)?.click();
      await settle(fixture);
      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      fill(fixture);
      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledTimes(2);
      expect(keyOf(1)).not.toBe(keyOf(0));
    });

    it('does not change while the person only looks at the form', async () => {
      create.mockRejectedValueOnce(lost());
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      await settle(fixture);
      fixture.detectChanges();

      submit(fixture);
      await settle(fixture);

      expect(keyOf(1)).toBe(keyOf(0));
    });
  });
});
