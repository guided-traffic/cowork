import { HttpErrorResponse } from '@angular/common/http';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Problem, Project } from '../../api/models';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { NewProjectDialog, projectKey } from './new-project-dialog';

/** Any Idempotency-Key a form makes: a UUID (docs/adr/0045 D3). */
const formKey = expect.stringMatching(
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
);

const created = { key: 'COW', name: 'cowork' } as Project;

function refusal(status: number, errors: { pointer: string; message: string }[] = []) {
  const body: Problem = {
    type: 'about:blank',
    title: 'The project is not valid',
    status,
    detail: 'Check the key.',
    code: errors.length > 0 ? 'validation_failed' : 'project_key_taken',
    errors,
  };
  return new HttpErrorResponse({ status, statusText: body.title, error: body });
}

describe('projectKey', () => {
  it.each(['AB', 'COW', 'A1', 'B2B', 'VKO', 'ABCDEFGHIJ', 'A123456789'])(
    'accepts the key %s',
    (key) => {
      expect(projectKey.test(key)).toBe(true);
    },
  );

  it.each([
    ['', 'nothing'],
    ['A', 'a single character'],
    ['ABCDEFGHIJK', 'eleven characters'],
    ['cow', 'lower case'],
    ['Cow', 'mixed case'],
    ['1AB', 'a start with a digit'],
    ['AB-C', 'a hyphen, which would end the key'],
    ['AB C', 'a space'],
    ['AB_C', 'an underscore'],
    ['ÄB', 'a letter outside A to Z'],
    [' AB', 'a leading space'],
    ['AB ', 'a trailing space'],
    ['AB\n', 'a trailing newline'],
  ])('rejects the key %j: %s', (key) => {
    expect(projectKey.test(key)).toBe(false);
  });
});

describe('NewProjectDialog', () => {
  let create: MockInstance<ProjectsService['create']>;
  let navigate: MockInstance<Router['navigate']>;

  beforeEach(() => {
    create = vi.fn<ProjectsService['create']>().mockResolvedValue(created);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        { provide: ProjectsService, useValue: { create } },
        { provide: SessionService, useValue: { tenant: signal<string | null>('acme') } },
      ],
    });
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
  });

  async function render(visible = true) {
    const fixture = TestBed.createComponent(NewProjectDialog);
    fixture.componentRef.setInput('visible', visible);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a submit started finish, and shows it; fields register with the form a moment later. */
  async function settle(fixture: ComponentFixture<NewProjectDialog>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const el = (fixture: ComponentFixture<NewProjectDialog>, testId: string) =>
    (fixture.nativeElement as HTMLElement).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  function typeInto(fixture: ComponentFixture<NewProjectDialog>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement | HTMLTextAreaElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  const submit = (fixture: ComponentFixture<NewProjectDialog>) =>
    (fixture.nativeElement as HTMLElement)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const saveButton = (fixture: ComponentFixture<NewProjectDialog>) =>
    el(fixture, 'project-save') as HTMLButtonElement | null;

  function fill(fixture: ComponentFixture<NewProjectDialog>, key = 'cow', name = 'cowork') {
    typeInto(fixture, 'project-key', key);
    typeInto(fixture, 'project-name', name);
  }

  describe('the dialog', () => {
    it('is closed until it is asked to open', async () => {
      const fixture = await render(false);

      expect(el(fixture, 'project-key')).toBeNull();
    });

    it('asks for a key, a name and a description under the header of a new project', async () => {
      const fixture = await render();

      const host = fixture.nativeElement as HTMLElement;
      expect(host.querySelector('.p-dialog-title')?.textContent).toBe('New project');
      expect(el(fixture, 'project-key')).not.toBeNull();
      expect(el(fixture, 'project-name')).not.toBeNull();
      expect(el(fixture, 'project-description')).not.toBeNull();
      expect(host.querySelector('.hint')?.textContent).toBe(
        'The key starts every ticket key (COW-12) and never changes.',
      );
    });

    it('limits the key to ten characters and the name to two hundred', async () => {
      const fixture = await render();

      expect(el(fixture, 'project-key')?.getAttribute('maxlength')).toBe('10');
      expect(el(fixture, 'project-name')?.getAttribute('maxlength')).toBe('200');
    });

    it('closes when the dialog asks to be closed', async () => {
      const fixture = await render();

      fixture.debugElement.query(By.css('p-dialog')).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(el(fixture, 'project-key')).toBeNull();
    });

    it('closes with Cancel, without creating anything', async () => {
      const fixture = await render();
      fill(fixture);

      const cancel = [...(fixture.nativeElement as HTMLElement).querySelectorAll('button')].find(
        (button) => button.textContent?.trim() === 'Cancel',
      );
      cancel?.click();
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(create).not.toHaveBeenCalled();
    });
  });

  describe('what a project needs', () => {
    it('cannot be created without a valid key and a name', async () => {
      const fixture = await render();
      expect(saveButton(fixture)?.disabled).toBe(true);

      typeInto(fixture, 'project-key', 'COW');
      expect(saveButton(fixture)?.disabled).toBe(true);
      typeInto(fixture, 'project-name', 'cowork');
      expect(saveButton(fixture)?.disabled).toBe(false);
      typeInto(fixture, 'project-name', '   ');
      expect(saveButton(fixture)?.disabled).toBe(true);
    });

    it.each(['C', 'ABCDEFGHIJK', '1AB', 'CO-W', 'CO W'])(
      'cannot be created with the key %j, which is no project key',
      async (key) => {
        const fixture = await render();

        fill(fixture, key, 'cowork');
        submit(fixture);
        await settle(fixture);

        expect(saveButton(fixture)?.disabled).toBe(true);
        expect(create).not.toHaveBeenCalled();
      },
    );

    it('takes a key typed in lower case as the same key in upper case', async () => {
      const fixture = await render();

      fill(fixture, 'cow', 'cowork');

      expect(saveButton(fixture)?.disabled).toBe(false);
      submit(fixture);
      await settle(fixture);
      expect(create.mock.calls[0][0].key).toBe('COW');
    });
  });

  describe('creating', () => {
    it('creates the project with its key and its name, and leaves out an empty description', async () => {
      const fixture = await render();
      fill(fixture, 'COW', '  cowork  ');

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith({ key: 'COW', name: 'cowork' }, formKey);
      expect(create.mock.calls[0][0]).toStrictEqual({ key: 'COW', name: 'cowork' });
    });

    it('creates the project with its description, trimmed, when there is one', async () => {
      const fixture = await render();
      fill(fixture, 'COW', 'cowork');
      typeInto(fixture, 'project-description', '  The tool itself  ');

      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith(
        {
          key: 'COW',
          name: 'cowork',
          description: 'The tool itself',
        },
        formKey,
      );
    });

    it('leaves out a description of spaces only', async () => {
      const fixture = await render();
      fill(fixture, 'COW', 'cowork');
      typeInto(fixture, 'project-description', '   ');

      submit(fixture);
      await settle(fixture);

      expect(create.mock.calls[0][0]).toStrictEqual({ key: 'COW', name: 'cowork' });
    });

    it('closes the dialog, empties the form and opens the backlog of the new project', async () => {
      const fixture = await render();
      fill(fixture, 'COW', 'cowork');
      typeInto(fixture, 'project-description', 'The tool itself');

      submit(fixture);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme', 'p', 'COW', 'backlog']);
      fixture.componentInstance.visible.set(true);
      await settle(fixture);
      expect((el(fixture, 'project-key') as HTMLInputElement).value).toBe('');
      expect((el(fixture, 'project-name') as HTMLInputElement).value).toBe('');
      expect((el(fixture, 'project-description') as HTMLTextAreaElement).value).toBe('');
    });

    it('opens the backlog of the key the server answered with', async () => {
      create.mockResolvedValue({ key: 'OPS', name: 'operations' } as Project);
      const fixture = await render();
      fill(fixture, 'ops', 'operations');

      submit(fixture);
      await settle(fixture);

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme', 'p', 'OPS', 'backlog']);
    });

    it('creates once while the project is on its way, and shows the button as busy', async () => {
      let finish: (project: Project) => void = () => undefined;
      create.mockReturnValue(
        new Promise<Project>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      submit(fixture);
      await settle(fixture);
      expect(create).toHaveBeenCalledOnce();
      expect(saveButton(fixture)?.disabled).toBe(true);
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();
      finish(created);
      await settle(fixture);

      expect(fixture.componentInstance.visible()).toBe(false);
    });
  });

  describe('a project that the server refuses', () => {
    it('shows the problem of the key beside the key, and keeps the dialog and what was typed', async () => {
      create.mockRejectedValue(
        refusal(409, [{ pointer: '/key', message: 'The key COW is taken in this tenant.' }]),
      );
      const fixture = await render();
      fill(fixture, 'cow', 'cowork');

      submit(fixture);
      await settle(fixture);

      const error = (fixture.nativeElement as HTMLElement).querySelector('small.error');
      expect(error?.textContent).toBe('The key COW is taken in this tenant.');
      expect(fixture.componentInstance.visible()).toBe(true);
      expect((el(fixture, 'project-key') as HTMLInputElement).value).toBe('COW');
      expect((el(fixture, 'project-name') as HTMLInputElement).value).toBe('cowork');
      expect(navigate).not.toHaveBeenCalled();
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('does not toast what the key already says', async () => {
      create.mockRejectedValue(refusal(409, [{ pointer: '/key', message: 'Taken.' }]));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      fill(fixture);

      submit(fixture);
      await settle(fixture);

      expect(add).not.toHaveBeenCalled();
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
      expect((fixture.nativeElement as HTMLElement).querySelector('small.error')).toBeNull();
    });

    it('creates again with the same Idempotency-Key after a lost answer, and with a new one for another content (docs/adr/0045 D3)', async () => {
      create.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      create.mockRejectedValueOnce(
        new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }),
      );
      const fixture = await render();
      fill(fixture);
      submit(fixture);
      await settle(fixture);
      submit(fixture);
      await settle(fixture);
      expect(create.mock.calls[1][1]).toBe(create.mock.calls[0][1]);

      typeInto(fixture, 'project-description', 'The tool itself');
      submit(fixture);
      await settle(fixture);
      expect(create.mock.calls[2][1]).not.toBe(create.mock.calls[0][1]);
    });

    it('shows no old problem when the next attempt is made, and creates the project when it works', async () => {
      create.mockRejectedValueOnce(refusal(409, [{ pointer: '/key', message: 'Taken.' }]));
      const fixture = await render();
      fill(fixture, 'cow', 'cowork');
      submit(fixture);
      await settle(fixture);
      expect((fixture.nativeElement as HTMLElement).querySelector('small.error')).not.toBeNull();

      typeInto(fixture, 'project-key', 'cwk');
      submit(fixture);
      await settle(fixture);

      expect(create).toHaveBeenCalledTimes(2);
      expect(create.mock.calls[1][0].key).toBe('CWK');
      expect(navigate).toHaveBeenCalledOnce();
    });
  });
});
