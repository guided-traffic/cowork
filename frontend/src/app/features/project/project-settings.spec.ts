import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import type { MockInstance } from 'vitest';
import { Problem, Project } from '../../api/models';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { ProjectSettings } from './project-settings';

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'id-COW',
    key: 'COW',
    name: 'Cowork',
    description: 'The tool itself',
    restricted: false,
    wip_limits: { 'in-progress': 3, blocked: 2 },
    version: 4,
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-01T09:00:00Z',
    ...overrides,
  };
}

function refusal(status: number, title: string, detail: string) {
  const body: Problem = { type: 'about:blank', title, status, detail, code: 'precondition_failed' };
  return new HttpErrorResponse({ status, statusText: title, error: body });
}

const columns = ['analysed', 'decided', 'in-progress', 'blocked'];

describe('ProjectSettings', () => {
  let list: WritableSignal<Project[]>;
  let isAdmin: WritableSignal<boolean>;
  let update: MockInstance<ProjectsService['update']>;
  let archive: MockInstance<ProjectsService['archive']>;
  let navigate: MockInstance<Router['navigate']>;

  beforeEach(() => {
    list = signal<Project[]>([project()]);
    isAdmin = signal(true);
    update = vi.fn<ProjectsService['update']>().mockResolvedValue(project());
    archive = vi.fn<ProjectsService['archive']>().mockResolvedValue(project());
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        {
          provide: ProjectsService,
          useValue: {
            byKey: (key: string) => list().find((each) => each.key === key),
            update,
            archive,
          },
        },
        { provide: SessionService, useValue: { tenant: signal<string | null>('acme') } },
        { provide: TenantService, useValue: { isAdmin } },
      ],
    });
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
  });

  async function render(key = 'COW') {
    const fixture = TestBed.createComponent(ProjectSettings);
    fixture.componentRef.setInput('project', key);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields take their model a moment after they appear. */
  async function settle(fixture: ComponentFixture<ProjectSettings>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<ProjectSettings>) => fixture.nativeElement as HTMLElement;

  const el = (fixture: ComponentFixture<ProjectSettings>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  function typeInto(fixture: ComponentFixture<ProjectSettings>, testId: string, value: string) {
    const field = el(fixture, testId) as HTMLInputElement | HTMLTextAreaElement;
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  /** What typing a number into a limit, or emptying it (which gives null), tells its model. */
  function setLimit(
    fixture: ComponentFixture<ProjectSettings>,
    column: string,
    value: number | null,
  ) {
    fixture.debugElement
      .query(By.css(`[data-testid="wip-${column}"]`))
      .triggerEventHandler('ngModelChange', value);
    fixture.detectChanges();
  }

  const limitShown = (fixture: ComponentFixture<ProjectSettings>, column: string) =>
    el(fixture, `wip-${column}`)?.querySelector('input')?.value;

  const submit = (fixture: ComponentFixture<ProjectSettings>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));

  const saveButton = (fixture: ComponentFixture<ProjectSettings>) =>
    el(fixture, 'settings-save') as HTMLButtonElement | null;

  describe('what it shows', () => {
    it('says so when there is no such project', async () => {
      const fixture = await render('NOPE');

      expect(host(fixture).querySelector('.page > p.muted')?.textContent).toBe('No such project.');
      expect(host(fixture).querySelector('form')).toBeNull();
      expect(el(fixture, 'settings-archive')).toBeNull();
    });

    it('shows the key of the project under the heading Settings', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('.title .key')?.textContent).toBe('COW');
      expect(host(fixture).querySelector('.title h1')?.textContent).toBe('Settings');
    });

    it('starts the form from the name, the description and the limits of the project', async () => {
      const fixture = await render();

      expect((el(fixture, 'settings-name') as HTMLInputElement).value).toBe('Cowork');
      expect((el(fixture, 'settings-description') as HTMLTextAreaElement).value).toBe(
        'The tool itself',
      );
      expect(columns.map((column) => limitShown(fixture, column))).toEqual(['', '', '3', '2']);
    });

    it('offers a limit for the four columns that can hold one, in board order', async () => {
      const fixture = await render();

      const labels = [...host(fixture).querySelectorAll('.limit > span.muted')].map(
        (label) => label.textContent,
      );
      expect(labels).toEqual(columns);
      expect(el(fixture, 'wip-analysed')?.querySelector('input')?.placeholder).toBe('none');
    });

    it('follows the project when it is replaced by a newer version', async () => {
      const fixture = await render();

      list.set([
        project({ name: 'Cowork 2', description: '', wip_limits: { analysed: 5 }, version: 5 }),
      ]);
      await settle(fixture);

      expect((el(fixture, 'settings-name') as HTMLInputElement).value).toBe('Cowork 2');
      expect((el(fixture, 'settings-description') as HTMLTextAreaElement).value).toBe('');
      expect(columns.map((column) => limitShown(fixture, column))).toEqual(['5', '', '', '']);
    });

    it('follows the project of the path', async () => {
      list.set([project(), project({ key: 'OPS', name: 'Operations', wip_limits: {} })]);
      const fixture = await render();

      fixture.componentRef.setInput('project', 'OPS');
      await settle(fixture);

      expect(host(fixture).querySelector('.title .key')?.textContent).toBe('OPS');
      expect((el(fixture, 'settings-name') as HTMLInputElement).value).toBe('Operations');
    });
  });

  describe('saving', () => {
    it('writes the name, the description and the limits that are shown, over the version that was read', async () => {
      const current = project();
      list.set([current]);
      const fixture = await render();

      submit(fixture);
      await settle(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith(current, {
        name: 'Cowork',
        description: 'The tool itself',
        wip_limits: { 'in-progress': 3, blocked: 2 },
      });
    });

    it('writes what was changed, without the spaces around the name and the description', async () => {
      const fixture = await render();
      typeInto(fixture, 'settings-name', '  Cowork, the tool  ');
      typeInto(fixture, 'settings-description', '  Backlog and board  ');

      submit(fixture);
      await settle(fixture);

      expect(update.mock.calls[0][1]).toMatchObject({
        name: 'Cowork, the tool',
        description: 'Backlog and board',
      });
    });

    it('writes a limit that was set, changed or emptied', async () => {
      const fixture = await render();
      setLimit(fixture, 'analysed', 4);
      setLimit(fixture, 'in-progress', 5);
      setLimit(fixture, 'blocked', null);

      submit(fixture);
      await settle(fixture);

      expect(update.mock.calls[0][1].wip_limits).toStrictEqual({ analysed: 4, 'in-progress': 5 });
    });

    it('writes no limits when every limit is emptied', async () => {
      const fixture = await render();
      setLimit(fixture, 'in-progress', null);
      setLimit(fixture, 'blocked', null);

      submit(fixture);
      await settle(fixture);

      expect(update.mock.calls[0][1].wip_limits).toStrictEqual({});
    });

    it('does not keep a limit whose value is not there at all', async () => {
      const fixture = await render();
      fixture.debugElement
        .query(By.css('[data-testid="wip-blocked"]'))
        .triggerEventHandler('ngModelChange', undefined);

      submit(fixture);
      await settle(fixture);

      expect(update.mock.calls[0][1].wip_limits).toStrictEqual({ 'in-progress': 3 });
    });

    it('does not change the project it was read from while the limits are edited', async () => {
      const current = project();
      list.set([current]);
      const fixture = await render();

      setLimit(fixture, 'blocked', null);
      setLimit(fixture, 'analysed', 9);

      expect(current.wip_limits).toStrictEqual({ 'in-progress': 3, blocked: 2 });
    });

    it('toasts the problem and keeps what was typed when the write is refused', async () => {
      update.mockRejectedValue(
        refusal(412, 'The project changed', 'Somebody saved it while you edited it.'),
      );
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      typeInto(fixture, 'settings-name', 'Cowork, the tool');

      submit(fixture);
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          summary: 'The project changed',
          detail: 'Somebody saved it while you edited it.',
        }),
      );
      expect((el(fixture, 'settings-name') as HTMLInputElement).value).toBe('Cowork, the tool');
      expect(saveButton(fixture)?.disabled).toBe(false);
    });

    it('shows its button as busy until the write is done', async () => {
      let finish: (project: Project) => void = () => undefined;
      update.mockReturnValue(
        new Promise<Project>((resolve) => {
          finish = resolve;
        }),
      );
      const fixture = await render();

      submit(fixture);
      await settle(fixture);
      expect(saveButton(fixture)?.disabled).toBe(true);
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).not.toBeNull();
      finish(project());
      await settle(fixture);

      expect(saveButton(fixture)?.disabled).toBe(false);
      expect(saveButton(fixture)?.querySelector('i.pi-spinner')).toBeNull();
    });

    it('writes nothing for a project that is not there', async () => {
      const fixture = await render('NOPE');

      await fixture.componentInstance['save']();

      expect(update).not.toHaveBeenCalled();
    });
  });

  describe('archiving', () => {
    const dialog = () => document.body.querySelector('.p-confirmdialog');

    const press = (label: string) =>
      [...(dialog()?.querySelectorAll('button') ?? [])]
        .find((button) => button.textContent?.trim() === label)
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

    async function ask(fixture: ComponentFixture<ProjectSettings>) {
      el(fixture, 'settings-archive')?.click();
      await settle(fixture);
    }

    it('is offered to an administrator', async () => {
      const fixture = await render();

      expect(el(fixture, 'settings-archive')?.textContent?.trim()).toBe('Archive');
      expect(host(fixture).querySelector('.danger h2')?.textContent).toBe('Archive this project');
    });

    it('is not offered to anyone else', async () => {
      isAdmin.set(false);

      const fixture = await render();

      expect(el(fixture, 'settings-archive')).toBeNull();
      expect(host(fixture).querySelector('.danger')).toBeNull();
    });

    it('is not offered for a project that is archived already', async () => {
      list.set([project({ archived_at: '2026-10-02T09:00:00Z' })]);

      const fixture = await render();

      expect(el(fixture, 'settings-archive')).toBeNull();
    });

    it('asks first, and says what stays', async () => {
      const fixture = await render();

      await ask(fixture);

      expect(dialog()?.textContent).toContain('Archive COW?');
      expect(dialog()?.textContent).toContain(
        'Its tickets and keys stay; it leaves the lists and takes no new ticket.',
      );
      expect(archive).not.toHaveBeenCalled();
    });

    it('archives the project and goes to the overview of the tenant when it is confirmed', async () => {
      const current = project();
      list.set([current]);
      const fixture = await render();
      await ask(fixture);

      press('Archive');
      await settle(fixture);

      expect(archive).toHaveBeenCalledExactlyOnceWith(current);
      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme']);
    });

    it('does nothing when the person keeps the project', async () => {
      const fixture = await render();
      await ask(fixture);

      press('Keep it');
      await settle(fixture);

      expect(archive).not.toHaveBeenCalled();
      expect(navigate).not.toHaveBeenCalled();
    });

    it('toasts the problem and stays when the project cannot be archived', async () => {
      archive.mockRejectedValue(refusal(403, 'Forbidden', 'Only an administrator archives.'));
      const add = vi.spyOn(TestBed.inject(MessageService), 'add');
      const fixture = await render();
      await ask(fixture);

      press('Archive');
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'Only an administrator archives.' }),
      );
      expect(navigate).not.toHaveBeenCalled();
    });

    it('asks about nothing for a project that is not there', async () => {
      const fixture = await render('NOPE');

      fixture.componentInstance['archive']();
      await settle(fixture);

      expect(dialog()).toBeNull();
    });
  });
});
