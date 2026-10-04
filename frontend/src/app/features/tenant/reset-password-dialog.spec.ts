import { HttpErrorResponse } from '@angular/common/http';
import { isSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Account, Problem } from '../../api/models';
import { AccountsService } from '../../core/accounts.service';
import { AuthService } from '../../core/auth.service';
import { ResetPasswordDialog } from './reset-password-dialog';
import { generatedLength, IssuedPassword, passwordAlphabet } from './temporary-password';

const sam: Account = {
  id: 'id-sam',
  username: 'sam',
  display_name: 'Sam Rivera',
  role: 'member',
  locked: false,
  password_change_required: false,
  deactivated_at: null,
  created_at: '2026-10-01T10:00:00Z',
};

function refusal(status: number, errors: { pointer: string; message: string }[] = []) {
  const body: Problem = {
    type: 'about:blank',
    title: 'The password is not valid',
    status,
    detail: 'Check the password.',
    code: errors.length > 0 ? 'validation_failed' : 'not_found',
    errors,
  };
  return new HttpErrorResponse({ status, statusText: body.title, error: body });
}

describe('ResetPasswordDialog', () => {
  let reset: MockInstance<AccountsService['reset']>;
  let warn: MockInstance<typeof console.warn>;

  beforeEach(() => {
    reset = vi.fn<AccountsService['reset']>().mockResolvedValue(undefined);
    warn = vi.spyOn(console, 'warn');
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: AccountsService, useValue: { reset } },
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

  async function render(account: Account | null = sam) {
    const fixture = TestBed.createComponent(ResetPasswordDialog);
    fixture.componentRef.setInput('account', account);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a submit started finish, and shows it; fields register with the form a moment later. */
  async function settle(fixture: ComponentFixture<ResetPasswordDialog>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<ResetPasswordDialog>) =>
    fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<ResetPasswordDialog>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  function typePassword(fixture: ComponentFixture<ResetPasswordDialog>, value: string) {
    const field = el(fixture, 'reset-password') as HTMLInputElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  const submit = (fixture: ComponentFixture<ResetPasswordDialog>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const saveButton = (fixture: ComponentFixture<ResetPasswordDialog>) =>
    el(fixture, 'reset-save') as HTMLButtonElement | null;

  const cancelButton = (fixture: ComponentFixture<ResetPasswordDialog>) =>
    el(fixture, 'reset-cancel') as HTMLButtonElement | null;

  /** A key press as the browser makes one: aimed at the focused element, on its way up to the document. */
  const press = (key: string, target: EventTarget = document.body) =>
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }));

  const mask = () => document.querySelector('.p-dialog-mask') as HTMLElement | null;

  describe('the dialog', () => {
    it('is closed while it holds no account', async () => {
      const fixture = await render(null);

      expect(el(fixture, 'reset-password')).toBeNull();
    });

    it('opens for an account, under a header that names it, and says what a reset does', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('.p-dialog-title')?.textContent).toBe(
        'Reset the password of sam',
      );
      const lead = host(fixture).querySelector('.lead')?.textContent ?? '';
      expect(lead).toContain('Every session of the account ends');
      expect(lead).toContain('chooses a new password at the next sign-in');
      expect(lead).toContain('stays locked if it is');
      expect(lead).toContain('its tokens stay as they are');
      expect(el(fixture, 'reset-password')).not.toBeNull();
      expect(el(fixture, 'reset-password-generate')).not.toBeNull();
    });

    it('closes when the dialog asks to be closed, such as with Escape or the cross', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(fixture.componentInstance.account()).toBeNull();
      expect(el(fixture, 'reset-password')).toBeNull();
    });

    it('stays open when the dialog tells that it is visible', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', true);
      await settle(fixture);

      expect(fixture.componentInstance.account()).toBe(sam);
    });

    it('closes with Cancel, without resetting anything', async () => {
      const fixture = await render();
      typePassword(fixture, 'a-temporary-one');

      el(fixture, 'reset-cancel')?.click();
      await settle(fixture);

      expect(fixture.componentInstance.account()).toBeNull();
      expect(reset).not.toHaveBeenCalled();
    });

    it('closes with Escape, with the cross and with a click beside it while nothing is running', async () => {
      const fixture = await render();
      expect(document.querySelector('.p-dialog-close-button')).not.toBeNull();

      press('Escape');
      await settle(fixture);
      expect(fixture.componentInstance.account()).toBeNull();

      fixture.componentInstance.account.set(sam);
      await settle(fixture);
      mask()?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      await settle(fixture);
      expect(fixture.componentInstance.account()).toBeNull();
    });

    it('follows the account it is given when it is opened for another one', async () => {
      const fixture = await render();

      fixture.componentRef.setInput('account', { ...sam, username: 'kim' });
      await settle(fixture);

      expect(host(fixture).querySelector('.p-dialog-title')?.textContent).toBe(
        'Reset the password of kim',
      );
    });
  });

  describe('what a reset needs', () => {
    it('needs a password, and nothing more of it than the server will say', async () => {
      const fixture = await render();
      expect(saveButton(fixture)?.disabled).toBe(true);

      typePassword(fixture, 'x');
      expect(saveButton(fixture)?.disabled).toBe(false);
      typePassword(fixture, '');
      expect(saveButton(fixture)?.disabled).toBe(true);
    });

    it('is filled with a generated password by the button, which is enough to reset', async () => {
      const fixture = await render();

      el(fixture, 'reset-password-generate')?.click();
      await settle(fixture);
      expect(saveButton(fixture)?.disabled).toBe(false);
      submit(fixture);
      await settle(fixture);

      const sent = reset.mock.calls[0][1];
      expect(generatedLength).toBe(24);
      expect(sent).toHaveLength(generatedLength);
      expect([...sent].every((character) => passwordAlphabet.includes(character))).toBe(true);
    });

    it('resets once while the reset is on its way, and shows the button as busy', async () => {
      let finish: () => void = () => undefined;
      reset.mockReturnValue(new Promise<void>((resolve) => (finish = resolve)));
      const fixture = await render();
      typePassword(fixture, 'a-temporary-one');

      submit(fixture);
      submit(fixture);
      await settle(fixture);
      expect(reset).toHaveBeenCalledOnce();
      expect(saveButton(fixture)?.disabled).toBe(true);
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();
      finish();
      await settle(fixture);

      expect(fixture.componentInstance.account()).toBeNull();
    });

    it('resets nothing without a password', async () => {
      const fixture = await render();

      submit(fixture);
      await settle(fixture);

      expect(reset).not.toHaveBeenCalled();
    });

    it('resets nothing for an account that is not there', async () => {
      const fixture = await render(null);

      await fixture.componentInstance['save']();

      expect(reset).not.toHaveBeenCalled();
    });
  });

  describe('resetting', () => {
    it('sets the password that was typed, as it was typed, on the account of the dialog', async () => {
      const fixture = await render();
      typePassword(fixture, ' a temporary one ');

      submit(fixture);
      await settle(fixture);

      expect(reset).toHaveBeenCalledExactlyOnceWith('sam', ' a temporary one ');
    });

    it('hands the account and the password on, for the page to show once, and closes', async () => {
      const issued: IssuedPassword[] = [];
      const fixture = await render();
      fixture.componentInstance.passwordSet.subscribe((each) => issued.push(each));
      typePassword(fixture, 'a-temporary-one');

      submit(fixture);
      await settle(fixture);

      expect(issued).toEqual([{ account: sam, password: 'a-temporary-one' }]);
      expect(fixture.componentInstance.account()).toBeNull();
    });

    it('keeps the password in no signal of the dialog once it is set', async () => {
      const fixture = await render();
      typePassword(fixture, 'a-temporary-one');
      submit(fixture);
      await settle(fixture);

      const held = Object.values(fixture.componentInstance)
        .filter((value) => isSignal(value))
        .map((value) => JSON.stringify((value as () => unknown)()) ?? '')
        .filter((json) => json.includes('a-temporary-one'));
      expect(held).toEqual([]);
    });

    it('forgets the password once it is closed, however it closes', async () => {
      const fixture = await render();
      typePassword(fixture, 'a-temporary-one');
      el(fixture, 'reset-cancel')?.click();
      await settle(fixture);

      fixture.componentInstance.account.set(sam);
      await settle(fixture);

      expect((el(fixture, 'reset-password') as HTMLInputElement).value).toBe('');
      expect(saveButton(fixture)?.disabled).toBe(true);
    });
  });

  describe('a password that the server refuses', () => {
    it('shows its message beside the field, and keeps the dialog and what was typed', async () => {
      reset.mockRejectedValue(
        refusal(422, [
          { pointer: '/temporary_password', message: 'must be at least 12 characters' },
        ]),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      typePassword(fixture, 'short');

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'reset-password-error')?.textContent).toBe(
        'must be at least 12 characters',
      );
      expect(fixture.componentInstance.account()).toBe(sam);
      expect((el(fixture, 'reset-password') as HTMLInputElement).value).toBe('short');
      expect(add).not.toHaveBeenCalled();
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('toasts a problem that names no field, such as an account that is gone', async () => {
      reset.mockRejectedValue(refusal(404));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      typePassword(fixture, 'a-temporary-one');

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ summary: 'The password is not valid' }),
      );
      expect(el(fixture, 'reset-password-error')).toBeNull();
      expect(fixture.componentInstance.account()).toBe(sam);
    });

    it('shows no old problem when the next attempt is made, and resets when it works', async () => {
      reset.mockRejectedValueOnce(
        refusal(422, [{ pointer: '/temporary_password', message: 'too short' }]),
      );
      const fixture = await render();
      typePassword(fixture, 'short');
      submit(fixture);
      await settle(fixture);
      expect(el(fixture, 'reset-password-error')).not.toBeNull();

      typePassword(fixture, 'a-long-enough-one');
      submit(fixture);
      await settle(fixture);

      expect(reset).toHaveBeenCalledTimes(2);
      expect(fixture.componentInstance.account()).toBeNull();
    });
  });

  describe('for assistive technology', () => {
    it('marks the password as invalid after a refusal, with the text that says why, which is an alert', async () => {
      reset.mockRejectedValue(
        refusal(422, [
          { pointer: '/temporary_password', message: 'must be at least 12 characters' },
        ]),
      );
      const fixture = await render();
      typePassword(fixture, 'short');
      const field = el(fixture, 'reset-password') as HTMLInputElement;
      expect(field.getAttribute('aria-invalid')).toBe('false');

      submit(fixture);
      await settle(fixture);

      expect(field.getAttribute('aria-invalid')).toBe('true');
      expect(field.getAttribute('aria-describedby')).toBe('reset-password-error');
      expect(el(fixture, 'reset-password-error')?.id).toBe('reset-password-error');
      expect(el(fixture, 'reset-password-error')?.getAttribute('role')).toBe('alert');
    });

    it('takes it away again with the next attempt', async () => {
      reset.mockRejectedValueOnce(
        refusal(422, [{ pointer: '/temporary_password', message: 'too short' }]),
      );
      const fixture = await render();
      typePassword(fixture, 'short');
      submit(fixture);
      await settle(fixture);
      let finish: () => void = () => undefined;
      reset.mockReturnValue(new Promise<void>((resolve) => (finish = resolve)));

      typePassword(fixture, 'a-long-enough-one');
      submit(fixture);
      await settle(fixture);

      const field = el(fixture, 'reset-password') as HTMLInputElement;
      expect(field.getAttribute('aria-invalid')).toBe('false');
      expect(field.hasAttribute('aria-describedby')).toBe(false);
      finish();
      await settle(fixture);
    });
  });

  describe('while the request is out', () => {
    let finish: () => void;
    let fail: (error: unknown) => void;

    async function sending() {
      reset.mockReturnValue(
        new Promise<void>((resolve, reject) => {
          finish = resolve;
          fail = reject;
        }),
      );
      const fixture = await render();
      typePassword(fixture, 'a-temporary-one');
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
      press('Escape', el(fixture, 'reset-password') as HTMLElement);
      mask()?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      await settle(fixture);

      expect(fixture.componentInstance.account()).toBe(sam);
      expect((el(fixture, 'reset-password') as HTMLInputElement).value).toBe('a-temporary-one');
      finish();
      await settle(fixture);
    });

    it('shows a refusal in the form that sent it, because the form is still there', async () => {
      const fixture = await sending();
      press('Escape');

      fail(
        refusal(422, [
          { pointer: '/temporary_password', message: 'must be at least 12 characters' },
        ]),
      );
      await settle(fixture);

      expect(fixture.componentInstance.account()).toBe(sam);
      expect(el(fixture, 'reset-password-error')?.textContent).toBe(
        'must be at least 12 characters',
      );
    });

    it('can be closed again once the refusal is there, with Cancel, the cross and Escape', async () => {
      const fixture = await sending();
      fail(refusal(422, [{ pointer: '/temporary_password', message: 'too short' }]));
      await settle(fixture);

      expect(cancelButton(fixture)?.disabled).toBe(false);
      expect(document.querySelector('.p-dialog-close-button')).not.toBeNull();
      press('Escape');
      await settle(fixture);

      expect(fixture.componentInstance.account()).toBeNull();
    });

    it('closes by itself when the password is set, and the page gets it', async () => {
      const issued: IssuedPassword[] = [];
      const fixture = await sending();
      fixture.componentInstance.passwordSet.subscribe((each) => issued.push(each));

      finish();
      await settle(fixture);

      expect(fixture.componentInstance.account()).toBeNull();
      expect(issued).toEqual([{ account: sam, password: 'a-temporary-one' }]);
    });

    it('toasts a refusal that arrives after the page has closed the dialog, and keeps no error for the next account', async () => {
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await sending();

      // The page leaves the tenant and closes the dialog it opened, whatever the request is doing.
      fixture.componentInstance.account.set(null);
      await settle(fixture);
      fail(refusal(422, [{ pointer: '/temporary_password', message: 'too short' }]));
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ summary: 'The password is not valid' }),
      );
      fixture.componentInstance.account.set({ ...sam, username: 'kim' });
      await settle(fixture);
      expect(el(fixture, 'reset-password-error')).toBeNull();
    });

    it('toasts a refusal that arrives after the dialog was opened for another account, and shows it under nobody', async () => {
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await sending();

      fixture.componentInstance.account.set({ ...sam, username: 'kim' });
      await settle(fixture);
      fail(refusal(422, [{ pointer: '/temporary_password', message: 'too short' }]));
      await settle(fixture);

      expect(add).toHaveBeenCalledOnce();
      expect(el(fixture, 'reset-password-error')).toBeNull();
    });
  });
});
