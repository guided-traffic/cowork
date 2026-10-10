import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Dialog } from 'primeng/dialog';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import { Member, Problem } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { AddMemberDialog, addRefusals } from './add-member-dialog';
import { roleMeanings } from './roles';

const bob: Member = {
  role: 'member',
  person: { id: 'p3', display_name: 'Bob Example', username: null },
  origins: [{ source: 'grant', role: 'member' }],
  local: false,
  email: 'bob@example.com',
};

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

function refusal(
  status: number,
  code: Problem['code'],
  errors: { pointer: string; message: string }[] = [],
) {
  const body: Problem = {
    type: 'about:blank',
    title: 'Refused',
    status,
    detail: 'The server says no.',
    code,
    ...(errors.length ? { errors } : {}),
  };
  return new HttpErrorResponse({ status, statusText: body.title, error: body });
}

describe('AddMemberDialog', () => {
  let add: MockInstance<MembersService['add']>;

  beforeEach(() => {
    add = vi.fn<MembersService['add']>().mockResolvedValue(bob);
    TestBed.configureTestingModule({
      providers: [MessageService, { provide: MembersService, useValue: { add } }],
    });
  });

  // Whatever a test spied on goes back, also when an assertion of the test has failed.
  afterEach(() => vi.restoreAllMocks());

  async function render(visible = true) {
    const fixture = TestBed.createComponent(AddMemberDialog);
    fixture.componentRef.setInput('visible', visible);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a submit started finish, and shows it; fields register with the form a moment later. */
  async function settle(fixture: ComponentFixture<AddMemberDialog>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  // The dialog lies in the document's body (appendTo), beside its host, over a dialog it is opened
  // from: its own window and the mask around it, while it is open.
  const host = (fixture: ComponentFixture<AddMemberDialog>) =>
    (fixture.debugElement.query(By.directive(Dialog))?.componentInstance as Dialog | undefined)
      ?.container()?.parentElement ?? (fixture.nativeElement as HTMLElement);
  const el = (fixture: ComponentFixture<AddMemberDialog>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const person = (fixture: ComponentFixture<AddMemberDialog>) =>
    el(fixture, 'member-person') as HTMLInputElement;

  function typeInto(fixture: ComponentFixture<AddMemberDialog>, value: string) {
    person(fixture).value = value;
    person(fixture).dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  const choose = (fixture: ComponentFixture<AddMemberDialog>, role: string) => {
    fixture.debugElement
      .query(By.css('[data-testid="member-role"]'))
      .triggerEventHandler('ngModelChange', role);
    fixture.detectChanges();
  };

  const submit = (fixture: ComponentFixture<AddMemberDialog>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const saveButton = (fixture: ComponentFixture<AddMemberDialog>) =>
    el(fixture, 'member-save') as HTMLButtonElement | null;
  const cancelButton = (fixture: ComponentFixture<AddMemberDialog>) =>
    el(fixture, 'member-cancel') as HTMLButtonElement | null;

  describe('the dialog', () => {
    it('is closed until it is asked to open', async () => {
      const fixture = await render(false);

      expect(el(fixture, 'member-person')).toBeNull();
    });

    it('asks for an e-mail address or a username and a role, and says who can be found', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('.p-dialog-title')?.textContent).toBe('Add a member');
      expect(host(fixture).querySelector('label[for="member-person-input"]')?.textContent).toBe(
        'E-mail address or username',
      );
      expect(person(fixture).id).toBe('member-person-input');
      expect(person(fixture).getAttribute('maxlength')).toBe('320');
      expect(person(fixture).getAttribute('autocomplete')).toBe('off');
      expect(host(fixture).querySelector('.lead')?.textContent).toContain(
        'once they have signed in',
      );
    });

    it('offers the three roles, a member chosen, and says what the chosen one may do', async () => {
      const fixture = await render();

      const select = fixture.debugElement.query(By.css('[data-testid="member-role"]'))
        .componentInstance as Select;
      expect(select.options()).toEqual(['viewer', 'member', 'admin']);
      expect(select.ariaLabelledBy()).toBe('member-role-label');
      expect(el(fixture, 'member-role-meaning')?.textContent?.trim()).toBe(roleMeanings.member);

      choose(fixture, 'admin');

      expect(el(fixture, 'member-role-meaning')?.textContent?.trim()).toBe(roleMeanings.admin);
    });

    it('closes with Cancel, without adding anybody', async () => {
      const fixture = await render();
      typeInto(fixture, 'bob@example.com');

      cancelButton(fixture)?.click();
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(add).not.toHaveBeenCalled();
    });

    it('closes when the dialog asks to be closed', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });

  describe('adding', () => {
    it('cannot add without an e-mail address or a username, or with spaces only', async () => {
      const fixture = await render();
      expect(saveButton(fixture)?.disabled).toBe(true);

      typeInto(fixture, '   ');
      expect(saveButton(fixture)?.disabled).toBe(true);
      submit(fixture);
      await settle(fixture);
      expect(add).not.toHaveBeenCalled();

      typeInto(fixture, 'bob@example.com');
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('adds the person typed, without the spaces around, with the role chosen and a key of its own', async () => {
      const fixture = await render();
      typeInto(fixture, '  bob@example.com  ');
      choose(fixture, 'viewer');

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        'bob@example.com',
        'viewer',
        expect.stringMatching(uuid),
      );
    });

    it('hands the member on, for the page to say so, and closes', async () => {
      const added: Member[] = [];
      const fixture = await render();
      fixture.componentInstance.added.subscribe((each) => added.push(each));
      typeInto(fixture, 'bob@example.com');

      submit(fixture);
      await settle(fixture);

      expect(added).toEqual([bob]);
      expect(fixture.componentInstance.visible()).toBe(false);
    });

    it('forgets what was typed once it is closed', async () => {
      const fixture = await render();
      typeInto(fixture, 'bob@example.com');
      choose(fixture, 'admin');
      submit(fixture);
      await settle(fixture);

      fixture.componentInstance.visible.set(true);
      await settle(fixture);

      expect(person(fixture).value).toBe('');
      expect(
        el(fixture, 'member-role')?.querySelector('.p-select-label')?.textContent?.trim(),
      ).toBe('member');
    });

    it('cannot be closed with Escape while the request is out, and can again once it is refused', async () => {
      let fail: (error: unknown) => void = () => undefined;
      add.mockReturnValue(new Promise<Member>((_resolve, reject) => (fail = reject)));
      const fixture = await render();
      typeInto(fixture, 'bob@example.com');
      submit(fixture);
      await settle(fixture);

      document.body.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
      );
      await settle(fixture);
      expect(fixture.componentInstance.visible()).toBe(true);

      fail(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));
      await settle(fixture);
      document.body.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
      );
      await settle(fixture);
      expect(fixture.componentInstance.visible()).toBe(false);
    });

    it('adds once while the request is out, shows its button as busy, and cannot be closed meanwhile', async () => {
      let finish: (member: Member) => void = () => undefined;
      add.mockReturnValue(new Promise<Member>((resolve) => (finish = resolve)));
      const fixture = await render();
      typeInto(fixture, 'bob@example.com');

      submit(fixture);
      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledOnce();
      expect(saveButton(fixture)?.disabled).toBe(true);
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();
      expect(cancelButton(fixture)?.disabled).toBe(true);
      expect(document.querySelector('.p-dialog-close-button')).toBeNull();
      finish(bob);
      await settle(fixture);
      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });

  describe('a refusal', () => {
    it.each([
      ['person_not_found', 404],
      ['person_ambiguous', 409],
      ['grant_exists', 409],
    ] as const)('says what %s means under the field, and keeps the form', async (code, status) => {
      add.mockRejectedValue(refusal(status, code));
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      typeInto(fixture, 'bob@example.com');

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'member-person-error')?.textContent).toBe(addRefusals[code]);
      expect(el(fixture, 'member-person-error')?.getAttribute('role')).toBe('alert');
      expect(person(fixture).getAttribute('aria-invalid')).toBe('true');
      expect(person(fixture).getAttribute('aria-describedby')).toBe('member-person-error');
      expect(person(fixture).value).toBe('bob@example.com');
      expect(fixture.componentInstance.visible()).toBe(true);
      expect(toasts).not.toHaveBeenCalled();
    });

    it('explains a person nobody has seen yet in words', () => {
      expect(addRefusals.person_not_found).toContain('after their first sign-in');
    });

    it('shows the problems of the fields beside them', async () => {
      add.mockRejectedValue(
        refusal(422, 'validation_failed', [
          { pointer: '/person', message: 'is too long' },
          { pointer: '/role', message: 'is not a role' },
        ]),
      );
      const fixture = await render();
      typeInto(fixture, 'bob@example.com');

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'member-person-error')?.textContent).toBe('is too long');
      expect(el(fixture, 'member-role-error')?.textContent).toBe('is not a role');
      const combobox = el(fixture, 'member-role')?.querySelector('[role="combobox"]');
      expect(combobox?.getAttribute('aria-invalid')).toBe('true');
      expect(combobox?.getAttribute('aria-describedby')).toBe(
        'member-role-meaning member-role-error',
      );
    });

    it('toasts a problem that names no field', async () => {
      add.mockRejectedValue(refusal(403, 'forbidden'));
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      typeInto(fixture, 'bob@example.com');

      submit(fixture);
      await settle(fixture);

      expect(toasts).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'The server says no.' }),
      );
      expect(host(fixture).querySelector('small.error')).toBeNull();
    });

    it('toasts a refusal that arrives after the page closed the dialog, and keeps no error for the next form', async () => {
      let fail: (error: unknown) => void = () => undefined;
      add.mockReturnValue(new Promise<Member>((_resolve, reject) => (fail = reject)));
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      typeInto(fixture, 'bob@example.com');
      submit(fixture);
      await settle(fixture);

      fixture.componentInstance.visible.set(false);
      await settle(fixture);
      fail(refusal(404, 'person_not_found'));
      await settle(fixture);

      expect(toasts).toHaveBeenCalledOnce();
      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      expect(el(fixture, 'member-person-error')).toBeNull();
    });
  });

  describe('the key of the act (docs/adr/0045)', () => {
    const keyOf = (call: number) => add.mock.calls[call][2];
    const lost = () => new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' });

    it('is the same for a retry of the same content, so that a lost answer is answered again', async () => {
      add.mockRejectedValueOnce(lost());
      const fixture = await render();
      typeInto(fixture, 'bob@example.com');
      submit(fixture);
      await settle(fixture);

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledTimes(2);
      expect(keyOf(1)).toBe(keyOf(0));
    });

    it('is another one once the person or the role changed', async () => {
      add.mockRejectedValueOnce(lost()).mockRejectedValueOnce(lost());
      const fixture = await render();
      typeInto(fixture, 'bob@example.com');
      submit(fixture);
      await settle(fixture);

      typeInto(fixture, 'cyd@example.com');
      submit(fixture);
      await settle(fixture);
      choose(fixture, 'admin');
      submit(fixture);
      await settle(fixture);

      expect(new Set([keyOf(0), keyOf(1), keyOf(2)]).size).toBe(3);
    });
  });
});
