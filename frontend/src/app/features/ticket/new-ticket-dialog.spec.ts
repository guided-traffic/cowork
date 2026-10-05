import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import { SelectButton } from 'primeng/selectbutton';
import type { MockInstance } from 'vitest';
import { Member, Problem, Ticket, TicketCreate } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { TicketActions } from '../../core/ticket-actions.service';
import { TicketsService } from '../../core/tickets.service';
import { NewTicketDialog } from './new-ticket-dialog';
import { ParentPicker } from './parent-picker';

/** Any Idempotency-Key a form makes: a UUID (docs/adr/0045 D3). */
const formKey = expect.stringMatching(
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
);

const ada: Member = {
  role: 'admin',
  person: { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' },
  origins: [{ source: 'grant', role: 'admin' }],
  local: true,
  email: null,
};
const sam: Member = {
  role: 'member',
  person: { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' },
  origins: [{ source: 'grant', role: 'member' }],
  local: true,
  email: null,
};

const filedTicket = {
  key: 'acme/COW-7',
  project: 'COW',
  number: 7,
  title: 'The board flickers',
} as Ticket;

function refusal(status: number, errors: { pointer: string; message: string }[] = []) {
  const body: Problem = {
    type: 'about:blank',
    title: 'Validation failed',
    status,
    detail: 'The ticket is not valid.',
    code: 'validation_failed',
    errors,
  };
  return new HttpErrorResponse({ status, statusText: body.title, error: body });
}

describe('NewTicketDialog', () => {
  let create: MockInstance<TicketActions['create']>;
  let people: WritableSignal<Member[]>;
  let warn: MockInstance<typeof console.warn>;

  beforeEach(() => {
    create = vi.fn<TicketActions['create']>().mockResolvedValue(filedTicket);
    people = signal<Member[]>([ada, sam]);
    warn = vi.spyOn(console, 'warn');
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        { provide: TicketActions, useValue: { create } },
        { provide: MembersService, useValue: { list: people } },
        { provide: TicketsService, useValue: { openTickets: vi.fn().mockResolvedValue([]) } },
      ],
    });
  });

  afterEach(() => {
    // Restore first, so that an assertion that fails does not leave the spy behind. The select
    // buttons of PrimeNG keep an `ngModel` each; inside an `NgForm` of the dialog they would warn
    // that they cannot register with it (NG01354), so the form is none (`ngNoForm`).
    const warnings = warn.mock.calls.filter((call) => String(call[0]).includes('NG01354'));
    warn.mockRestore();
    expect(warnings).toEqual([]);
  });

  async function render(visible = true) {
    const fixture = TestBed.createComponent(NewTicketDialog);
    fixture.componentRef.setInput('tenant', 'acme');
    fixture.componentRef.setInput('project', 'COW');
    fixture.componentRef.setInput('visible', visible);
    await fixture.whenStable();
    return fixture;
  }

  /** Lets the writes that the dialog started finish, and shows what they changed. */
  async function settle(fixture: ComponentFixture<NewTicketDialog>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const el = (fixture: ComponentFixture<NewTicketDialog>, testId: string) =>
    (fixture.nativeElement as HTMLElement).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  function typeInto(fixture: ComponentFixture<NewTicketDialog>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement | HTMLTextAreaElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  /**
   * What choosing an option in a PrimeNG select, select button or the like tells its model. The
   * fields that the choice shows register with the form a moment later, so this waits for them.
   */
  async function choose(
    fixture: ComponentFixture<NewTicketDialog>,
    testId: string,
    value: unknown,
  ) {
    fixture.debugElement
      .query(By.css(`[data-testid="${testId}"]`))
      .triggerEventHandler('ngModelChange', value);
    await settle(fixture);
  }

  const submit = (fixture: ComponentFixture<NewTicketDialog>) =>
    (fixture.nativeElement as HTMLElement)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const saveButton = (fixture: ComponentFixture<NewTicketDialog>) =>
    el(fixture, 'new-save') as HTMLButtonElement | null;

  /** The ticket as filed with nothing but a title typed in. */
  const plain = (title: string): TicketCreate => ({
    title,
    type: 'task',
    severity: 'medium',
    security: 'none',
    effort: 'M',
  });

  describe('the dialog', () => {
    it('is closed until it is asked to open', async () => {
      const fixture = await render(false);

      expect(el(fixture, 'new-title')).toBeNull();
      expect(fixture.componentInstance.visible()).toBe(false);
    });

    it('names the project in its header and offers the fields of a ticket', async () => {
      const fixture = await render();

      const host = fixture.nativeElement as HTMLElement;
      expect(host.querySelector('.p-dialog-title')?.textContent).toBe('New ticket in COW');
      for (const testId of [
        'new-title',
        'new-type',
        'new-severity',
        'new-security',
        'new-effort',
        'new-assignee',
        'new-body',
      ]) {
        expect(el(fixture, testId), testId).not.toBeNull();
      }
      expect(el(fixture, 'new-threat')).toBeNull();
    });

    it('closes when the dialog asks to be closed', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(el(fixture, 'new-title')).toBeNull();
    });
  });

  describe('the choices', () => {
    it('offers the five types, each with its icon, and labels each button with its type', async () => {
      const fixture = await render();

      const types = fixture.debugElement.query(By.css('[data-testid="new-type"]'));
      const options = (types.componentInstance as SelectButton).options() as {
        value: string;
        icon: string;
      }[];
      expect(options.map((option) => [option.value, option.icon])).toEqual([
        ['task', 'pi pi-check-square'],
        ['bug', 'pi pi-exclamation-circle'],
        ['feature', 'pi pi-star'],
        ['decision', 'pi pi-directions'],
        ['question', 'pi pi-question-circle'],
      ]);
      const buttons = [...(types.nativeElement as HTMLElement).querySelectorAll('p-togglebutton')];
      expect(buttons.map((button) => button.getAttribute('aria-label'))).toEqual([
        'task',
        'bug',
        'feature',
        'decision',
        'question',
      ]);
    });

    it('offers the severities from critical to cosmetic, the security classes and the efforts', async () => {
      const fixture = await render();

      const optionsOf = (testId: string) =>
        (
          fixture.debugElement.query(By.css(`[data-testid="${testId}"]`))
            .componentInstance as Select
        )
          .options()
          ?.map((option) => option.value);
      expect(optionsOf('new-severity')).toEqual(['critical', 'high', 'medium', 'low', 'cosmetic']);
      expect(optionsOf('new-security')).toEqual(['none', 'hardening', 'boundary', 'live']);
      const efforts = fixture.debugElement.query(By.css('[data-testid="new-effort"]'))
        .componentInstance as SelectButton;
      expect(efforts.options()).toEqual(['XS', 'S', 'M', 'L']);
    });

    it('offers the members of the tenant as assignees, by name', async () => {
      const fixture = await render();

      const assignee = fixture.debugElement.query(By.css('[data-testid="new-assignee"]'))
        .componentInstance as Select;
      expect(assignee.options()).toEqual([
        { id: 'p1', name: 'Ada Lovelace' },
        { id: 'p2', name: 'Sam Rivera' },
      ]);
    });

    it('follows the members when they arrive', async () => {
      people.set([]);
      const fixture = await render();

      people.set([sam]);
      await settle(fixture);

      const assignee = fixture.debugElement.query(By.css('[data-testid="new-assignee"]'))
        .componentInstance as Select;
      expect(assignee.options()).toEqual([{ id: 'p2', name: 'Sam Rivera' }]);
    });
  });

  describe('filing', () => {
    it('files a task of medium severity and size M, with no security class, from a title alone', async () => {
      const fixture = await render();
      typeInto(fixture, 'new-title', 'The board flickers');

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith(
        'acme',
        'COW',
        plain('The board flickers'),
        formKey,
      );
      expect(create.mock.calls[0][2]).toStrictEqual(plain('The board flickers'));
    });

    it('files what was chosen', async () => {
      const fixture = await render();
      typeInto(fixture, 'new-title', 'Anyone can read another tenant');
      await choose(fixture, 'new-type', 'bug');
      await choose(fixture, 'new-severity', 'critical');
      await choose(fixture, 'new-security', 'boundary');
      await choose(fixture, 'new-effort', 'L');
      await choose(fixture, 'new-assignee', 'p2');
      typeInto(fixture, 'new-threat', 'A member can read a ticket of another tenant');
      typeInto(fixture, 'new-body', '    code first\n\nthen words');

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith(
        'acme',
        'COW',
        {
          title: 'Anyone can read another tenant',
          type: 'bug',
          severity: 'critical',
          security: 'boundary',
          effort: 'L',
          threat: 'A member can read a ticket of another tenant',
          assignee: 'p2',
          body: '    code first\n\nthen words',
        },
        formKey,
      );
    });

    it('files again with the same Idempotency-Key after a lost answer, and with a new one for another content or the next ticket (docs/adr/0045 D3)', async () => {
      create.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      create.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      const fixture = await render();
      typeInto(fixture, 'new-title', 'The board flickers');
      submit(fixture);
      await settle(fixture);
      submit(fixture);
      await settle(fixture);
      expect(create.mock.calls[1][3]).toBe(create.mock.calls[0][3]);

      typeInto(fixture, 'new-title', 'The board flickers at night');
      submit(fixture);
      await settle(fixture);
      expect(create.mock.calls[2][3]).not.toBe(create.mock.calls[0][3]);

      // Filed: the next ticket of the same content is another act.
      fixture.componentRef.setInput('visible', false);
      await settle(fixture);
      fixture.componentRef.setInput('visible', true);
      await settle(fixture);
      typeInto(fixture, 'new-title', 'The board flickers at night');
      submit(fixture);
      await settle(fixture);
      expect(create.mock.calls[3][3]).not.toBe(create.mock.calls[2][3]);
      expect(new Set(create.mock.calls.map((call) => call[3])).size).toBe(3);
    });

    it('trims the title and the threat but sends the description as written', async () => {
      const fixture = await render();
      typeInto(fixture, 'new-title', '  The board flickers  ');
      await choose(fixture, 'new-security', 'live');
      typeInto(fixture, 'new-threat', '  Anyone can do it  ');
      typeInto(fixture, 'new-body', '  indented\n');

      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][2]).toMatchObject({
        title: 'The board flickers',
        threat: 'Anyone can do it',
        body: '  indented\n',
      });
    });

    it('files the ticket under the parent chosen among the open tickets of its project (docs/adr/0008 D2)', async () => {
      const fixture = await render();
      typeInto(fixture, 'new-title', 'Part of the board');
      const picker = fixture.debugElement.query(By.directive(ParentPicker))
        .componentInstance as ParentPicker;
      expect([picker.tenant(), picker.project(), picker.value()]).toEqual(['acme', 'COW', null]);

      picker.picked.emit('acme/COW-3');
      await settle(fixture);
      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][2]).toStrictEqual({
        ...plain('Part of the board'),
        parent: 'acme/COW-3',
      });
    });

    it('files no parent once the choice is cleared', async () => {
      const fixture = await render();
      const picker = fixture.debugElement.query(By.directive(ParentPicker))
        .componentInstance as ParentPicker;
      typeInto(fixture, 'new-title', 'Second');
      picker.picked.emit('acme/COW-3');
      picker.picked.emit(null);
      await settle(fixture);
      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][2]).toStrictEqual(plain('Second'));
    });

    it('leaves out a description that is only spaces and an assignee that was cleared', async () => {
      const fixture = await render();
      typeInto(fixture, 'new-title', 'The board flickers');
      typeInto(fixture, 'new-body', '  \n  ');
      await choose(fixture, 'new-assignee', 'p1');
      await choose(fixture, 'new-assignee', null);

      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][2]).toStrictEqual(plain('The board flickers'));
    });

    it('hands the filed ticket on, closes the dialog and empties the form', async () => {
      const fixture = await render();
      const filed = vi.fn();
      fixture.componentInstance.filed.subscribe(filed);
      typeInto(fixture, 'new-title', 'The board flickers');
      await choose(fixture, 'new-type', 'bug');
      await choose(fixture, 'new-severity', 'low');
      await choose(fixture, 'new-security', 'hardening');
      typeInto(fixture, 'new-threat', 'Someone could');
      typeInto(fixture, 'new-body', 'Details');

      submit(fixture);
      await settle(fixture);

      expect(filed).toHaveBeenCalledExactlyOnceWith(filedTicket);
      expect(fixture.componentInstance.visible()).toBe(false);
      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      expect((el(fixture, 'new-title') as HTMLInputElement).value).toBe('');
      expect((el(fixture, 'new-body') as HTMLTextAreaElement).value).toBe('');
      expect(el(fixture, 'new-threat')).toBeNull();
      const efforts = el(fixture, 'new-effort')?.querySelector('.p-togglebutton-checked');
      expect(efforts?.textContent?.trim()).toBe('M');
      const types = el(fixture, 'new-type')?.querySelector('.p-togglebutton-checked');
      expect(types?.textContent?.trim()).toBe('task');
      expect(
        el(fixture, 'new-severity')?.querySelector('.p-select-label')?.textContent?.trim(),
      ).toBe('medium');
    });
  });

  describe('what a ticket needs', () => {
    it('cannot be filed without a title', async () => {
      const fixture = await render();
      expect(saveButton(fixture)?.disabled).toBe(true);

      submit(fixture);
      await settle(fixture);
      expect(create).not.toHaveBeenCalled();

      typeInto(fixture, 'new-title', '   ');
      expect(saveButton(fixture)?.disabled).toBe(true);
      typeInto(fixture, 'new-title', 'The board flickers');
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('asks for the threat when the security class is not none, and cannot be filed without it', async () => {
      const fixture = await render();
      typeInto(fixture, 'new-title', 'Anyone can read another tenant');
      expect(el(fixture, 'new-threat')).toBeNull();

      await choose(fixture, 'new-security', 'boundary');
      expect(el(fixture, 'new-threat')).not.toBeNull();
      expect(saveButton(fixture)?.disabled).toBe(true);
      submit(fixture);
      await settle(fixture);
      expect(create).not.toHaveBeenCalled();

      typeInto(fixture, 'new-threat', 'A member could');
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('leaves the threat out again when the security class goes back to none', async () => {
      const fixture = await render();
      typeInto(fixture, 'new-title', 'The board flickers');
      await choose(fixture, 'new-security', 'live');
      typeInto(fixture, 'new-threat', 'Someone could');

      await choose(fixture, 'new-security', 'none');
      expect(el(fixture, 'new-threat')).toBeNull();
      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][2]).toStrictEqual(plain('The board flickers'));
    });
  });

  describe('a ticket that the server refuses', () => {
    async function refused(fixture: ComponentFixture<NewTicketDialog>) {
      typeInto(fixture, 'new-title', 'The board flickers');
      await choose(fixture, 'new-security', 'live');
      typeInto(fixture, 'new-threat', 'Someone could');
      submit(fixture);
      await settle(fixture);
    }

    it('shows the problem of each field beside it and keeps the dialog and what was typed', async () => {
      create.mockRejectedValue(
        refusal(422, [
          { pointer: '/title', message: 'A title like this exists already.' },
          { pointer: '/threat', message: 'Name a principal, a verb and a target.' },
        ]),
      );
      const fixture = await render();
      const filed = vi.fn();
      fixture.componentInstance.filed.subscribe(filed);

      await refused(fixture);

      const errors = [...(fixture.nativeElement as HTMLElement).querySelectorAll('small.error')];
      expect(errors.map((error) => error.textContent)).toEqual([
        'A title like this exists already.',
        'Name a principal, a verb and a target.',
      ]);
      expect(errors[0].closest('label')?.querySelector('[data-testid="new-title"]')).not.toBeNull();
      expect(
        errors[1].closest('label')?.querySelector('[data-testid="new-threat"]'),
      ).not.toBeNull();
      expect(fixture.componentInstance.visible()).toBe(true);
      expect((el(fixture, 'new-title') as HTMLInputElement).value).toBe('The board flickers');
      expect(filed).not.toHaveBeenCalled();
    });

    it('does not toast what the fields already say', async () => {
      create.mockRejectedValue(refusal(422, [{ pointer: '/title', message: 'Too long.' }]));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();

      await refused(fixture);

      expect(add).not.toHaveBeenCalled();
    });

    it('toasts a problem that names no field, such as an unreachable backend', async () => {
      create.mockRejectedValue(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();

      await refused(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ severity: 'error', summary: 'The backend cannot be reached' }),
      );
      expect((fixture.nativeElement as HTMLElement).querySelector('small.error')).toBeNull();
      expect(fixture.componentInstance.visible()).toBe(true);
    });

    it('can be filed again once it is corrected, and then shows no old problem', async () => {
      create.mockRejectedValueOnce(refusal(422, [{ pointer: '/title', message: 'Too long.' }]));
      const fixture = await render();
      await refused(fixture);
      expect((fixture.nativeElement as HTMLElement).querySelector('small.error')).not.toBeNull();
      expect(saveButton(fixture)?.disabled).toBe(false);

      typeInto(fixture, 'new-title', 'Shorter');
      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledTimes(2);
      expect(fixture.componentInstance.visible()).toBe(false);
      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      expect((fixture.nativeElement as HTMLElement).querySelector('small.error')).toBeNull();
    });
  });

  describe('while it is being filed', () => {
    it('does not file the ticket twice', async () => {
      let finish: (ticket: Ticket) => void = () => undefined;
      create.mockReturnValue(
        new Promise<Ticket>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = await render();
      typeInto(fixture, 'new-title', 'The board flickers');

      submit(fixture);
      submit(fixture);
      await settle(fixture);
      expect(create).toHaveBeenCalledOnce();
      expect(saveButton(fixture)?.disabled).toBe(true);
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();
      expect(saveButton(fixture)?.querySelector('i.pi-plus')).toBeNull();

      finish(filedTicket);
      await settle(fixture);
      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });

  describe('cancelling', () => {
    it('closes the dialog, empties the form and files nothing', async () => {
      const fixture = await render();
      typeInto(fixture, 'new-title', 'The board flickers');
      typeInto(fixture, 'new-body', 'Details');

      const cancel = [...(fixture.nativeElement as HTMLElement).querySelectorAll('button')].find(
        (button) => button.textContent?.trim() === 'Cancel',
      );
      cancel?.click();
      await settle(fixture);

      expect(create).not.toHaveBeenCalled();
      expect(fixture.componentInstance.visible()).toBe(false);
      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      expect((el(fixture, 'new-title') as HTMLInputElement).value).toBe('');
      expect((el(fixture, 'new-body') as HTMLTextAreaElement).value).toBe('');
    });
  });
});
