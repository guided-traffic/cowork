import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { ConfirmationService, MessageService } from 'primeng/api';
import { Subject } from 'rxjs';
import type { MockInstance } from 'vitest';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Member, Problem, Project } from '../../api/models';
import { EventStreamService } from '../../core/event-stream.service';
import { MembersService } from '../../core/members.service';
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

/** The limits by their key, in the order of the board's columns. */
const columns = ['analysed', 'decided', 'in-progress', 'blocked', 'review'];

describe('ProjectSettings', () => {
  let list: WritableSignal<Project[]>;
  let tenant: WritableSignal<string | null>;
  let isAdmin: WritableSignal<boolean>;
  let update: MockInstance<ProjectsService['update']>;
  let archive: MockInstance<ProjectsService['archive']>;
  let restrict: MockInstance<ProjectsService['restrict']>;
  let navigate: MockInstance<Router['navigate']>;

  beforeEach(() => {
    list = signal<Project[]>([project()]);
    tenant = signal<string | null>('acme');
    isAdmin = signal(true);
    update = vi.fn<ProjectsService['update']>().mockResolvedValue(project());
    archive = vi.fn<ProjectsService['archive']>().mockResolvedValue(project());
    restrict = vi.fn<ProjectsService['restrict']>().mockResolvedValue(project());
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
            restrict,
          },
        },
        // The access section has a spec of its own: here the session names no membership, so
        // its list asks for nothing.
        {
          provide: SessionService,
          useValue: { tenant, membership: signal(undefined) },
        },
        { provide: TenantService, useValue: { isAdmin } },
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        { provide: EventStreamService, useValue: { events: new Subject() } },
        { provide: MembersService, useValue: { list: signal<Member[]>([]) } },
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
      expect(columns.map((column) => limitShown(fixture, column))).toEqual(['', '', '3', '2', '']);
    });

    it('offers a limit for each column of the board, named as the board names it, with the states it counts', async () => {
      const fixture = await render();

      const limits = [...host(fixture).querySelectorAll('.limit')].map((limit) => [
        limit.querySelector('.column')?.textContent,
        limit.querySelector('.counts')?.textContent,
      ]);
      expect(limits).toEqual([
        ['Refinement', 'filed and analysed'],
        ['Ready', 'decided'],
        ['In Progress', 'in-progress'],
        ['Blocked', 'blocked'],
        ['Review', 'review'],
      ]);
      expect(el(fixture, 'wip-analysed')?.querySelector('input')?.placeholder).toBe('none');
    });

    it('names each limit field by its column and the states it counts', async () => {
      const fixture = await render();

      const field = fixture.debugElement.query(By.css('[data-testid="wip-analysed"]'));
      expect((field.componentInstance as { ariaLabelledBy(): string }).ariaLabelledBy()).toBe(
        'wip-analysed wip-counts-analysed',
      );
      expect(host(fixture).querySelector('#wip-analysed')?.textContent).toBe('Refinement');
      expect(host(fixture).querySelector('#wip-counts-analysed')?.textContent).toBe(
        'filed and analysed',
      );
    });

    it('follows the project when it is replaced by a newer version', async () => {
      const fixture = await render();

      list.set([
        project({ name: 'Cowork 2', description: '', wip_limits: { analysed: 5 }, version: 5 }),
      ]);
      await settle(fixture);

      expect((el(fixture, 'settings-name') as HTMLInputElement).value).toBe('Cowork 2');
      expect((el(fixture, 'settings-description') as HTMLTextAreaElement).value).toBe('');
      expect(columns.map((column) => limitShown(fixture, column))).toEqual(['5', '', '', '', '']);
    });

    it('keeps what is typed when the list brings the project again at the same version', async () => {
      const fixture = await render();
      typeInto(fixture, 'settings-name', 'Cowork, the tool');

      // An event or a poll loads the list again (docs/adr/0054): a new object, the same version.
      list.set([project()]);
      await settle(fixture);

      expect((el(fixture, 'settings-name') as HTMLInputElement).value).toBe('Cowork, the tool');
    });

    it('keeps what is typed when only the restriction changed, and writes over the newest version', async () => {
      const fixture = await render();
      typeInto(fixture, 'settings-name', 'Cowork, the tool');

      const restricted = project({ restricted: true, version: 5 });
      list.set([restricted]);
      await settle(fixture);
      expect((el(fixture, 'settings-name') as HTMLInputElement).value).toBe('Cowork, the tool');

      submit(fixture);
      await settle(fixture);

      expect(update).toHaveBeenCalledExactlyOnceWith(
        restricted,
        expect.objectContaining({ name: 'Cowork, the tool' }),
      );
    });

    it.each<[string, Partial<Project>]>([
      ['the description', { description: 'Something else' }],
      ['a limit', { wip_limits: { 'in-progress': 3, blocked: 4 } }],
    ])('starts again from the project when %s changed', async (_what, changed) => {
      const fixture = await render();
      typeInto(fixture, 'settings-name', 'Cowork, the tool');

      list.set([project({ ...changed, version: 5 })]);
      await settle(fixture);

      expect((el(fixture, 'settings-name') as HTMLInputElement).value).toBe('Cowork');
      expect((el(fixture, 'settings-description') as HTMLTextAreaElement).value).toBe(
        changed.description ?? 'The tool itself',
      );
      expect(limitShown(fixture, 'blocked')).toBe(String(changed.wip_limits?.blocked ?? 2));
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
      setLimit(fixture, 'review', 2);

      submit(fixture);
      await settle(fixture);

      expect(update.mock.calls[0][1].wip_limits).toStrictEqual({
        analysed: 4,
        'in-progress': 5,
        review: 2,
      });
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

  describe('who sees the project', () => {
    const restricted = (fixture: ComponentFixture<ProjectSettings>) =>
      el(fixture, 'project-restricted')?.querySelector('input')?.checked;

    it('is offered to an administrator, for the project shown, between its settings and its archive', async () => {
      const fixture = await render();

      expect(el(fixture, 'project-access')).not.toBeNull();
      expect(restricted(fixture)).toBe(false);
      expect(
        [...host(fixture).querySelectorAll('.page > *')].map((each) => each.tagName.toLowerCase()),
      ).toEqual(['header', 'form', 'app-project-access', 'section']);
    });

    it('follows the project when it is replaced by a newer version', async () => {
      const fixture = await render();

      list.set([project({ restricted: true, version: 5 })]);
      await settle(fixture);

      expect(restricted(fixture)).toBe(true);
    });

    it('is not offered to anyone else (docs/adr/0034 D8)', async () => {
      isAdmin.set(false);

      const fixture = await render();

      expect(host(fixture).querySelector('app-project-access')).toBeNull();
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

    it("shows a confirmation's message as text, never as markup", async () => {
      const fixture = await render();

      fixture.debugElement.injector
        .get(ConfirmationService)
        .confirm({ header: 'Archive it?', message: '<a href="x">y</a>' });
      await settle(fixture);

      expect(dialog()?.querySelector('.p-confirmdialog-message')?.textContent).toBe(
        '<a href="x">y</a>',
      );
      expect(dialog()?.querySelector('a')).toBeNull();
    });

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

    // The page is reused when only the tenant of the path changes: confirmed now, the question
    // would archive the project of the same key in the tenant shown.
    it('drops the question when the page turns to another tenant before it is answered', async () => {
      const fixture = await render();
      await ask(fixture);
      expect(dialog()?.textContent).toContain('Archive COW?');

      tenant.set('globex');
      await settle(fixture);

      expect(dialog()).toBeNull();
      press('Archive');
      await settle(fixture);
      expect(archive).not.toHaveBeenCalled();
    });

    it('drops the question when the page turns to another project before it is answered', async () => {
      list.set([project(), project({ id: 'id-OPS', key: 'OPS', name: 'Operations' })]);
      const fixture = await render();
      await ask(fixture);

      fixture.componentRef.setInput('project', 'OPS');
      await settle(fixture);

      expect(dialog()).toBeNull();
      press('Archive');
      await settle(fixture);
      expect(archive).not.toHaveBeenCalled();
    });
  });

  describe('a question of the access section and another tenant', () => {
    // The section's own question (docs/adr/0034 D3) goes with the section: the projects of
    // another tenant are not there while they load, as ProjectsService has it, and the page
    // shows no project and no section meanwhile.
    it('is gone with the section, and restricts nothing in the tenant shown now', async () => {
      const fixture = await render();
      fixture.debugElement
        .query(By.css('[data-testid="project-restricted"]'))
        .triggerEventHandler('ngModelChange', true);
      await settle(fixture);
      expect(document.body.querySelector('.p-confirmdialog')?.textContent).toContain(
        'Restrict COW?',
      );

      tenant.set('globex');
      list.set([]);
      await settle(fixture);
      list.set([project({ id: 'id-COW-globex' })]);
      await settle(fixture);

      expect(document.body.querySelector('.p-confirmdialog')).toBeNull();
      expect(el(fixture, 'project-access')).not.toBeNull();
      expect(restrict).not.toHaveBeenCalled();
    });
  });
});
