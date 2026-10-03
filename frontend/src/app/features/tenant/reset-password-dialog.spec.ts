import { HttpErrorResponse } from '@angular/common/http';
import { isSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Account, Problem } from '../../api/models';
import { AccountsService } from '../../core/accounts.service';
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
      providers: [MessageService, { provide: AccountsService, useValue: { reset } }],
    });
  });

  afterEach(() => {
    // A field that cannot register with the form it sits in is a warning of development builds
    // (NG01354); the dialog must not cause one.
    expect(warn.mock.calls.filter((call) => String(call[0]).includes('NG01354'))).toEqual([]);
    warn.mockRestore();
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
      expect(lead).toContain('chooses a new password at the next login');
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

      expect(el(fixture, 'temporary-password-error')?.textContent).toBe(
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
      expect(el(fixture, 'temporary-password-error')).toBeNull();
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
      expect(el(fixture, 'temporary-password-error')).not.toBeNull();

      typePassword(fixture, 'a-long-enough-one');
      submit(fixture);
      await settle(fixture);

      expect(reset).toHaveBeenCalledTimes(2);
      expect(fixture.componentInstance.account()).toBeNull();
    });
  });
});
