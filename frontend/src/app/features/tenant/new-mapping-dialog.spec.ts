import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Dialog } from 'primeng/dialog';
import { Select } from 'primeng/select';
import type { MockInstance } from 'vitest';
import { GroupMapping, Problem } from '../../api/models';
import { GroupMappingsService } from '../../core/group-mappings.service';
import { mappingExists, NewMappingDialog } from './new-mapping-dialog';
import { roleMeanings } from './roles';

const made: GroupMapping = {
  id: 'm1',
  group: 'team-red',
  role: 'member',
  includes_caller: false,
  version: 1,
  created_at: '2026-10-04T08:00:00Z',
  updated_at: '2026-10-04T08:00:00Z',
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

describe('NewMappingDialog', () => {
  let create: MockInstance<GroupMappingsService['create']>;

  beforeEach(() => {
    create = vi.fn<GroupMappingsService['create']>().mockResolvedValue(made);
    TestBed.configureTestingModule({
      providers: [MessageService, { provide: GroupMappingsService, useValue: { create } }],
    });
  });

  // Whatever a test spied on goes back, also when an assertion of the test has failed.
  afterEach(() => vi.restoreAllMocks());

  async function render(visible = true) {
    const fixture = TestBed.createComponent(NewMappingDialog);
    fixture.componentRef.setInput('visible', visible);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a submit started finish, and shows it; fields register with the form a moment later. */
  async function settle(fixture: ComponentFixture<NewMappingDialog>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  // The dialog lies in the document's body (appendTo), beside its host, over a dialog it is opened
  // from: its own window and the mask around it, while it is open.
  const host = (fixture: ComponentFixture<NewMappingDialog>) =>
    (fixture.debugElement.query(By.directive(Dialog))?.componentInstance as Dialog | undefined)
      ?.container()?.parentElement ?? (fixture.nativeElement as HTMLElement);
  const el = (fixture: ComponentFixture<NewMappingDialog>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const group = (fixture: ComponentFixture<NewMappingDialog>) =>
    el(fixture, 'mapping-group') as HTMLInputElement;

  function typeInto(fixture: ComponentFixture<NewMappingDialog>, value: string) {
    group(fixture).value = value;
    group(fixture).dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  const choose = (fixture: ComponentFixture<NewMappingDialog>, role: string) => {
    fixture.debugElement
      .query(By.css('[data-testid="mapping-new-role"]'))
      .triggerEventHandler('ngModelChange', role);
    fixture.detectChanges();
  };

  const submit = (fixture: ComponentFixture<NewMappingDialog>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const saveButton = (fixture: ComponentFixture<NewMappingDialog>) =>
    el(fixture, 'mapping-save') as HTMLButtonElement | null;
  const cancelButton = (fixture: ComponentFixture<NewMappingDialog>) =>
    el(fixture, 'mapping-cancel') as HTMLButtonElement | null;

  describe('the dialog', () => {
    it('is closed until it is asked to open', async () => {
      const fixture = await render(false);

      expect(el(fixture, 'mapping-group')).toBeNull();
    });

    it('asks for a group and a role, and says the group is matched exactly', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('.p-dialog-title')?.textContent).toBe('New group mapping');
      expect(host(fixture).querySelector('label[for="mapping-group-input"]')?.textContent).toBe(
        'Group',
      );
      expect(group(fixture).id).toBe('mapping-group-input');
      expect(group(fixture).getAttribute('maxlength')).toBe('256');
      expect(group(fixture).getAttribute('aria-describedby')).toBe('mapping-group-hint');
      expect(host(fixture).querySelector('#mapping-group-hint')?.textContent).toContain(
        'upper and lower case as they are',
      );
    });

    it('offers the three roles, a member chosen, and says what the chosen one may do', async () => {
      const fixture = await render();

      const select = fixture.debugElement.query(By.css('[data-testid="mapping-new-role"]'))
        .componentInstance as Select;
      expect(select.options()).toEqual(['viewer', 'member', 'admin']);
      expect(select.ariaLabelledBy()).toBe('mapping-role-label');
      expect(el(fixture, 'mapping-role-meaning')?.textContent?.trim()).toBe(roleMeanings.member);

      choose(fixture, 'viewer');

      expect(el(fixture, 'mapping-role-meaning')?.textContent?.trim()).toBe(roleMeanings.viewer);
    });

    it('closes with Cancel, without mapping anything', async () => {
      const fixture = await render();
      typeInto(fixture, 'team-red');

      cancelButton(fixture)?.click();
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(create).not.toHaveBeenCalled();
    });

    it('closes when the dialog asks to be closed', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });

  describe('mapping', () => {
    it('cannot map without a group, or with spaces only', async () => {
      const fixture = await render();
      expect(saveButton(fixture)?.disabled).toBe(true);

      typeInto(fixture, '   ');
      submit(fixture);
      await settle(fixture);

      expect(saveButton(fixture)?.disabled).toBe(true);
      expect(create).not.toHaveBeenCalled();
    });

    it('maps the group as typed, case and all but without the spaces around, to the role chosen, with a key of its own', async () => {
      const fixture = await render();
      typeInto(fixture, '  Team-Red  ');
      choose(fixture, 'admin');

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith(
        'Team-Red',
        'admin',
        expect.stringMatching(uuid),
      );
    });

    it('hands the mapping on, for the page to say so, and closes', async () => {
      const created: GroupMapping[] = [];
      const fixture = await render();
      fixture.componentInstance.created.subscribe((each) => created.push(each));
      typeInto(fixture, 'team-red');

      submit(fixture);
      await settle(fixture);

      expect(created).toEqual([made]);
      expect(fixture.componentInstance.visible()).toBe(false);
    });

    it('forgets what was typed once it is closed', async () => {
      const fixture = await render();
      typeInto(fixture, 'team-red');
      choose(fixture, 'viewer');
      submit(fixture);
      await settle(fixture);

      fixture.componentInstance.visible.set(true);
      await settle(fixture);

      expect(group(fixture).value).toBe('');
      expect(
        el(fixture, 'mapping-new-role')?.querySelector('.p-select-label')?.textContent?.trim(),
      ).toBe('member');
    });

    it('cannot be closed with Escape while the request is out, and can again once it is refused', async () => {
      let fail: (error: unknown) => void = () => undefined;
      create.mockReturnValue(new Promise<GroupMapping>((_resolve, reject) => (fail = reject)));
      const fixture = await render();
      typeInto(fixture, 'team-red');
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

    it('maps once while the request is out, shows its button as busy, and cannot be closed meanwhile', async () => {
      let finish: (mapping: GroupMapping) => void = () => undefined;
      create.mockReturnValue(new Promise<GroupMapping>((resolve) => (finish = resolve)));
      const fixture = await render();
      typeInto(fixture, 'team-red');

      submit(fixture);
      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledOnce();
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();
      expect(cancelButton(fixture)?.disabled).toBe(true);
      expect(document.querySelector('.p-dialog-close-button')).toBeNull();
      finish(made);
      await settle(fixture);
      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });

  describe('a refusal', () => {
    it('says under the group that the tenant maps it already, and keeps the form', async () => {
      create.mockRejectedValue(refusal(409, 'mapping_exists'));
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      typeInto(fixture, 'team-red');

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'mapping-group-error')?.textContent).toBe(mappingExists);
      expect(el(fixture, 'mapping-group-error')?.getAttribute('role')).toBe('alert');
      expect(group(fixture).getAttribute('aria-invalid')).toBe('true');
      expect(group(fixture).getAttribute('aria-describedby')).toBe(
        'mapping-group-hint mapping-group-error',
      );
      expect(group(fixture).value).toBe('team-red');
      expect(toasts).not.toHaveBeenCalled();
    });

    it('shows the problems of the fields beside them', async () => {
      create.mockRejectedValue(
        refusal(422, 'validation_failed', [
          { pointer: '/group', message: 'must not start or end with white space' },
          { pointer: '/role', message: 'is not a role' },
        ]),
      );
      const fixture = await render();
      typeInto(fixture, 'team-red');

      submit(fixture);
      await settle(fixture);

      expect(el(fixture, 'mapping-group-error')?.textContent).toBe(
        'must not start or end with white space',
      );
      expect(el(fixture, 'mapping-role-error')?.textContent).toBe('is not a role');
      const combobox = el(fixture, 'mapping-new-role')?.querySelector('[role="combobox"]');
      expect(combobox?.getAttribute('aria-invalid')).toBe('true');
      expect(combobox?.getAttribute('aria-describedby')).toBe(
        'mapping-role-meaning mapping-role-error',
      );
    });

    it('toasts a problem that names no field', async () => {
      create.mockRejectedValue(refusal(403, 'forbidden'));
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      typeInto(fixture, 'team-red');

      submit(fixture);
      await settle(fixture);

      expect(toasts).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'The server says no.' }),
      );
    });

    it('toasts a refusal that arrives after the page closed the dialog', async () => {
      let fail: (error: unknown) => void = () => undefined;
      create.mockReturnValue(new Promise<GroupMapping>((_resolve, reject) => (fail = reject)));
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      typeInto(fixture, 'team-red');
      submit(fixture);
      await settle(fixture);

      fixture.componentInstance.visible.set(false);
      await settle(fixture);
      fail(refusal(409, 'mapping_exists'));
      await settle(fixture);

      expect(toasts).toHaveBeenCalledOnce();
      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      expect(el(fixture, 'mapping-group-error')).toBeNull();
    });
  });

  describe('the key of the act (docs/adr/0045)', () => {
    const keyOf = (call: number) => create.mock.calls[call][2];
    const lost = () => new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' });

    it('is the same for a retry of the same content, and another one once the content changed', async () => {
      create.mockRejectedValueOnce(lost()).mockRejectedValueOnce(lost());
      const fixture = await render();
      typeInto(fixture, 'team-red');
      submit(fixture);
      await settle(fixture);
      submit(fixture);
      await settle(fixture);

      choose(fixture, 'viewer');
      submit(fixture);
      await settle(fixture);

      expect(keyOf(1)).toBe(keyOf(0));
      expect(keyOf(2)).not.toBe(keyOf(0));
    });
  });
});
