import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Me, Problem } from '../../api/models';
import { AuthService } from '../../core/auth.service';
import { SessionService } from '../../core/session.service';
import { ChangePassword } from './change-password';

const ada: Me = {
  id: 'p1',
  display_name: 'Ada Lovelace',
  username: 'local:ada',
  memberships: [],
  global_admin: false,
  local: true,
  password_change_required: false,
};

describe('ChangePassword', () => {
  let person: WritableSignal<Me | undefined>;
  let changePassword: MockInstance<AuthService['changePassword']>;
  let navigateByUrl: MockInstance<Router['navigateByUrl']>;
  let toasts: MockInstance<MessageService['add']>;

  beforeEach(() => {
    person = signal<Me | undefined>(ada);
    changePassword = vi.fn<AuthService['changePassword']>().mockResolvedValue(undefined);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        { provide: AuthService, useValue: { changePassword } },
        { provide: SessionService, useValue: { person } },
      ],
    });
    navigateByUrl = vi.spyOn(TestBed.inject(Router), 'navigateByUrl').mockResolvedValue(true);
    toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
  });

  async function render(back?: string) {
    const fixture = TestBed.createComponent(ChangePassword);
    if (back !== undefined) {
      fixture.componentRef.setInput('return', back);
    }
    await settle(fixture);
    return fixture;
  }

  /** Lets what a submit started finish, and shows it; fields register with the form a moment later. */
  async function settle(fixture: ComponentFixture<ChangePassword>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<ChangePassword>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<ChangePassword>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  const field = (fixture: ComponentFixture<ChangePassword>, testId: string) =>
    el(fixture, testId) as HTMLInputElement;

  const submitButton = (fixture: ComponentFixture<ChangePassword>) =>
    el(fixture, 'password-submit') as HTMLButtonElement;

  function typeInto(fixture: ComponentFixture<ChangePassword>, testId: string, value: string) {
    const input = field(fixture, testId);
    input.value = value;
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  function fill(
    fixture: ComponentFixture<ChangePassword>,
    current = 'old horse battery',
    next = 'correct horse battery staple',
    again = next,
  ) {
    typeInto(fixture, 'password-current', current);
    typeInto(fixture, 'password-new', next);
    typeInto(fixture, 'password-again', again);
  }

  const submit = (fixture: ComponentFixture<ChangePassword>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  function refusal(status: number, errors: Problem['errors'] = []) {
    const body: Problem = {
      type: 'about:blank',
      title: status === 422 ? 'Invalid request' : 'Unauthenticated',
      status,
      code: status === 422 ? 'validation_failed' : 'unauthenticated',
      detail: 'refused',
      ...(errors.length ? { errors } : {}),
    };
    return new HttpErrorResponse({ status, statusText: body.title, error: body });
  }

  describe('what the page says', () => {
    it('offers a change to a person who chose to make one', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Change your password');
      expect(el(fixture, 'password-required')).toBeNull();
      expect(host(fixture).querySelector('app-wordmark')).not.toBeNull();
    });

    it('asks a person with a temporary password to choose their own first', async () => {
      person.set({ ...ada, password_change_required: true });

      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Choose your password');
      expect(el(fixture, 'password-required')?.textContent).toContain('temporary');
    });

    it('reads a person not loaded yet as one who is not required to change', async () => {
      person.set(undefined);

      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Change your password');
    });

    it('lets the password manager tell the current password from the new ones', async () => {
      const fixture = await render();

      expect(field(fixture, 'password-current').getAttribute('autocomplete')).toBe(
        'current-password',
      );
      expect(field(fixture, 'password-new').getAttribute('autocomplete')).toBe('new-password');
      expect(field(fixture, 'password-again').getAttribute('autocomplete')).toBe('new-password');
    });
  });

  describe('the form', () => {
    it('takes no submit until all three are filled and the new ones match', async () => {
      const fixture = await render();
      expect(submitButton(fixture).disabled).toBe(true);

      typeInto(fixture, 'password-current', 'old horse battery');
      typeInto(fixture, 'password-new', 'correct horse battery staple');
      expect(submitButton(fixture).disabled).toBe(true);

      typeInto(fixture, 'password-again', 'correct horse battery stapler');
      expect(submitButton(fixture).disabled).toBe(true);

      typeInto(fixture, 'password-again', 'correct horse battery staple');
      expect(submitButton(fixture).disabled).toBe(false);
    });

    it('says the two new ones differ only once the second has been typed', async () => {
      const fixture = await render();

      typeInto(fixture, 'password-new', 'correct horse battery staple');
      expect(el(fixture, 'password-mismatch')).toBeNull();

      typeInto(fixture, 'password-again', 'correct');
      expect(el(fixture, 'password-mismatch')?.textContent).toContain('do not match');

      typeInto(fixture, 'password-again', 'correct horse battery staple');
      expect(el(fixture, 'password-mismatch')).toBeNull();
    });

    it('ignores a submit the button would not allow', async () => {
      const fixture = await render();
      fill(fixture, 'old horse battery', 'correct horse battery staple', 'something else');

      submit(fixture);
      await settle(fixture);

      expect(changePassword).not.toHaveBeenCalled();
    });

    it('shows and hides all three passwords together', async () => {
      const fixture = await render();
      const reveal = el(fixture, 'password-reveal') as HTMLButtonElement;
      const types = () =>
        ['password-current', 'password-new', 'password-again'].map(
          (testId) => field(fixture, testId).type,
        );
      expect(types()).toEqual(['password', 'password', 'password']);
      expect(reveal.textContent?.trim()).toBe('Show the passwords');

      reveal.click();
      await settle(fixture);

      expect(types()).toEqual(['text', 'text', 'text']);
      expect(reveal.textContent?.trim()).toBe('Hide the passwords');

      reveal.click();
      await settle(fixture);

      expect(types()).toEqual(['password', 'password', 'password']);
    });
  });

  describe('a change that goes through', () => {
    it('sends the current and the new password, empties the form and goes to the start page', async () => {
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(changePassword).toHaveBeenCalledExactlyOnceWith(
        'old horse battery',
        'correct horse battery staple',
      );
      expect(navigateByUrl).toHaveBeenCalledExactlyOnceWith('/');
      expect(field(fixture, 'password-current').value).toBe('');
      expect(field(fixture, 'password-new').value).toBe('');
      expect(field(fixture, 'password-again').value).toBe('');
      expect(toasts).not.toHaveBeenCalled();
    });

    it('goes back to the page that sent the person here', async () => {
      const fixture = await render('/t/acme/p/COW/backlog');
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(navigateByUrl).toHaveBeenCalledExactlyOnceWith('/t/acme/p/COW/backlog');
    });

    it('never follows a return address that leaves the application', async () => {
      const fixture = await render('//evil.example/t/acme');
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(navigateByUrl).toHaveBeenCalledExactlyOnceWith('/');
    });

    it('takes no second submit while the first is on its way, and shows that it waits', async () => {
      let finish: () => void = () => undefined;
      changePassword.mockReturnValueOnce(
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(submitButton(fixture).disabled).toBe(true);
      expect(submitButton(fixture).querySelector('.pi-spinner')).not.toBeNull();
      submit(fixture);
      await settle(fixture);
      expect(changePassword).toHaveBeenCalledTimes(1);

      finish();
      await settle(fixture);

      expect(submitButton(fixture).querySelector('.pi-spinner')).toBeNull();
      expect(navigateByUrl).toHaveBeenCalledTimes(1);
    });
  });

  describe('a change the server refuses', () => {
    it('puts a wrong current password beside its field, keeps what was typed and stays', async () => {
      changePassword.mockRejectedValueOnce(
        refusal(422, [{ pointer: '/current_password', message: 'the current password is wrong' }]),
      );
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      const errors = [...host(fixture).querySelectorAll('small.error')].map((e) => e.textContent);
      expect(errors).toEqual(['the current password is wrong']);
      expect(field(fixture, 'password-new').value).toBe('correct horse battery staple');
      expect(navigateByUrl).not.toHaveBeenCalled();
      expect(toasts).not.toHaveBeenCalled();
      expect(submitButton(fixture).disabled).toBe(false);
    });

    it('puts the length rule beside the new password', async () => {
      changePassword.mockRejectedValueOnce(
        refusal(422, [{ pointer: '/new_password', message: 'must be at least 12 characters' }]),
      );
      const fixture = await render();
      fill(fixture, 'old horse battery', 'short', 'short');

      submit(fixture);
      await settle(fixture);

      const newField = field(fixture, 'password-new').closest('label');
      expect(newField?.querySelector('small.error')?.textContent).toBe(
        'must be at least 12 characters',
      );
    });

    it('forgets the errors of the last attempt when the next one starts', async () => {
      changePassword.mockRejectedValueOnce(
        refusal(422, [{ pointer: '/current_password', message: 'the current password is wrong' }]),
      );
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      expect(host(fixture).querySelectorAll('small.error')).toHaveLength(1);

      submit(fixture);
      await settle(fixture);

      expect(host(fixture).querySelectorAll('small.error')).toHaveLength(0);
      expect(navigateByUrl).toHaveBeenCalledExactlyOnceWith('/');
    });

    it('reports a refusal without a field as a message and blames no field', async () => {
      changePassword.mockRejectedValueOnce(refusal(401));
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(host(fixture).querySelectorAll('small.error')).toHaveLength(0);
      expect(toasts).toHaveBeenCalledTimes(1);
      expect(navigateByUrl).not.toHaveBeenCalled();
    });
  });
});
