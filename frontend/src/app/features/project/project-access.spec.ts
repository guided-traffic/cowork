import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import {
  HttpTestingController,
  provideHttpClientTesting,
  TestRequest,
} from '@angular/common/http/testing';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Select } from 'primeng/select';
import { Subject } from 'rxjs';
import type { MockInstance } from 'vitest';
import { provideApiConfiguration } from '../../api/api-configuration';
import {
  Member,
  Membership,
  Problem,
  ProblemCode,
  Project,
  ProjectAccessEntry,
  ProjectAccessRole,
} from '../../api/models';
import { EventStreamService } from '../../core/event-stream.service';
import { MembersService } from '../../core/members.service';
import { ProjectsService } from '../../core/projects.service';
import { SessionService } from '../../core/session.service';
import { AccessList } from './access-list';
import { accessMeanings, ProjectAccess, restrictionNotice } from './project-access';

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'id-SEC',
    key: 'SEC',
    name: 'Security',
    description: '',
    restricted: false,
    wip_limits: {},
    version: 4,
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-01T09:00:00Z',
    ...overrides,
  };
}

const member = (id: string, name: string, email: string | null = null): Member => ({
  person: { id, display_name: name },
  role: 'member',
  origins: [{ source: 'grant', role: 'member' }],
  local: false,
  email,
});

const entry = (
  id: string,
  name: string,
  role: ProjectAccessRole = 'member',
  email: string | null = null,
): ProjectAccessEntry => ({
  person: { id, display_name: name },
  role,
  email,
  created_at: '2026-10-04T08:00:00Z',
});

function refusal(status: number, code: ProblemCode, detail = 'The server says no.') {
  const body: Problem = { type: 'about:blank', title: 'Refused', status, detail, code };
  return new HttpErrorResponse({ status, statusText: 'Refused', error: body });
}

describe('ProjectAccess', () => {
  let http: HttpTestingController;
  let members: WritableSignal<Member[]>;
  let membersReload: MockInstance<() => boolean>;
  let restrict: MockInstance<ProjectsService['restrict']>;
  let projectsReload: MockInstance<() => boolean>;
  /** The page's own access list, the real one, whose acts a test may stand in for. */
  let access: AccessList;
  let set: MockInstance<AccessList['set']>;
  let remove: MockInstance<AccessList['remove']>;
  let entriesReload: MockInstance<() => boolean>;
  /** The load of the list that `render` left open. */
  let openLoad: TestRequest;

  const administrator: Membership = {
    role: 'admin',
    team: { slug: 'acme', name: 'Acme Corp' },
    tenant: { slug: 'acme', name: 'Acme Corp' },
    origins: [{ source: 'grant', role: 'admin' }],
    can_create_projects: true,
  };

  beforeEach(() => {
    members = signal<Member[]>([
      member('p1', 'Ada Lovelace'),
      member('p2', 'Sam Rivera'),
      member('p3', 'Bob Example'),
    ]);
    membersReload = vi.fn<() => boolean>().mockReturnValue(true);
    restrict = vi
      .fn<ProjectsService['restrict']>()
      .mockImplementation(async (each, restricted) => ({
        ...each,
        restricted,
        version: each.version + 1,
      }));
    projectsReload = vi.fn<() => boolean>().mockReturnValue(true);
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration(''),
        MessageService,
        { provide: EventStreamService, useValue: { events: new Subject() } },
        {
          provide: SessionService,
          useValue: { tenant: signal('acme'), membership: signal(administrator) },
        },
        {
          provide: ProjectsService,
          useValue: { restrict, projects: { reload: projectsReload, isLoading: () => false } },
        },
        {
          provide: MembersService,
          useValue: { list: members, members: { reload: membersReload, isLoading: () => false } },
        },
      ],
    });
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  // Whatever a test spied on goes back, also when an assertion of the test has failed.
  afterEach(() => vi.restoreAllMocks());

  /** The load of a project's access list, which is open until a test answers it. */
  const listLoad = (key = 'SEC') =>
    http.expectOne((request) => request.url === `/api/v1/teams/acme/projects/${key}/access`);

  /**
   * Shows the section for the project, and answers the load of its access list with the entries —
   * or leaves the load open, or fails it, where a test says so.
   */
  async function render(
    shown: Project = project(),
    answer: ProjectAccessEntry[] | HttpErrorResponse | 'open' = [
      entry('p2', 'Sam Rivera', 'viewer'),
    ],
  ) {
    const fixture = TestBed.createComponent(ProjectAccess);
    fixture.componentRef.setInput('project', shown);
    await tick(fixture);
    const load = listLoad(shown.key);
    if (answer instanceof HttpErrorResponse) {
      load.flush(answer.error, { status: answer.status, statusText: answer.statusText });
    } else if (answer !== 'open') {
      load.flush({ items: answer, next_cursor: null });
    }
    if (answer === 'open') {
      openLoad = load;
      await tick(fixture);
    } else {
      await settle(fixture);
    }
    access = fixture.debugElement.injector.get(AccessList);
    set = vi.spyOn(access, 'set').mockImplementation(async (id, role) => {
      const name = members().find((each) => each.person.id === id)?.person.display_name ?? id;
      return entry(id, name, role);
    });
    remove = vi.spyOn(access, 'remove').mockResolvedValue(undefined);
    entriesReload = vi.spyOn(access.entries, 'reload').mockReturnValue(true);
    return fixture;
  }

  /**
   * Runs what is due and shows it, without waiting for the requests that are open, which a test
   * answers itself: in a zoneless test bed whenStable would wait for them.
   */
  async function tick(fixture: ComponentFixture<ProjectAccess>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  /** Lets what a click started finish, and shows it; fields take their model a moment after they appear. */
  async function settle(fixture: ComponentFixture<ProjectAccess>) {
    await tick(fixture);
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<ProjectAccess>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<ProjectAccess>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const switchOn = (fixture: ComponentFixture<ProjectAccess>) =>
    el(fixture, 'project-restricted')?.querySelector('input')?.checked;
  const toggle = (fixture: ComponentFixture<ProjectAccess>, value: boolean) =>
    fixture.debugElement
      .query(By.css('[data-testid="project-restricted"]'))
      .triggerEventHandler('ngModelChange', value);
  const shownRole = (fixture: ComponentFixture<ProjectAccess>, id: string) =>
    el(fixture, `access-role-${id}`)?.querySelector('.p-select-label')?.textContent?.trim();
  function choose(fixture: ComponentFixture<ProjectAccess>, testId: string, value: string) {
    fixture.debugElement
      .query(By.css(`[data-testid="${testId}"]`))
      .triggerEventHandler('ngModelChange', value);
    fixture.detectChanges();
  }
  const submit = (fixture: ComponentFixture<ProjectAccess>) =>
    host(fixture)
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { cancelable: true }));
  const toasts = () => vi.spyOn(TestBed.inject(MessageService), 'add');

  const dialog = () => document.body.querySelector('.p-confirmdialog');
  const press = (label: string) =>
    [...(dialog()?.querySelectorAll('button') ?? [])]
      .find((button) => button.textContent?.trim() === label)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  /** Turns the switch on and says yes to the question it asks. */
  async function restrictConfirmed(fixture: ComponentFixture<ProjectAccess>) {
    toggle(fixture, true);
    await settle(fixture);
    press('Restrict');
    await settle(fixture);
  }

  describe('the section', () => {
    it('is headed Access, and loads the access list of the project of the page', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('h2')?.textContent).toBe('Access');
      expect(access.project()).toBe('SEC');
      expect(access.list().map((each) => each.person.display_name)).toEqual(['Sam Rivera']);
    });

    it('follows the project of the page, and drops what was chosen for the one before', async () => {
      const fixture = await render();
      choose(fixture, 'access-person', 'p3');

      fixture.componentRef.setInput('project', project({ key: 'OPS' }));
      await tick(fixture);
      listLoad('OPS').flush({ items: [], next_cursor: null });
      await settle(fixture);

      expect(access.project()).toBe('OPS');
      expect(fixture.componentInstance['candidate']()).toBeNull();
    });

    it('keeps what was chosen and what it said when the projects load again and hand in the same project', async () => {
      const fixture = await render();
      set.mockRejectedValueOnce(refusal(404, 'person_not_found'));
      choose(fixture, 'access-role-p2', 'member');
      await settle(fixture);
      choose(fixture, 'access-person', 'p3');

      fixture.componentRef.setInput('project', project({ version: 5 }));
      await settle(fixture);

      expect(fixture.componentInstance['candidate']()).toBe('p3');
      expect(el(fixture, 'access-notice')?.textContent?.trim()).toBe(
        'Sam Rivera is no longer a member of this team.',
      );
      http.expectNone((request) => request.url.endsWith('/access'));
    });
  });

  describe('the restriction (docs/adr/0034 D3)', () => {
    it('shows an open project as open, and says that the list counts only once it is restricted', async () => {
      const fixture = await render();

      expect(switchOn(fixture)).toBe(false);
      expect(el(fixture, 'access-hint')?.textContent).toContain(
        'Every member of the team sees this project',
      );
      expect(el(fixture, 'access-hint')?.textContent).toContain(
        'fill it before restricting, so that nobody on it loses the project in between',
      );
    });

    it('shows a restricted project as restricted, and says who sees it', async () => {
      const fixture = await render(project({ restricted: true }));

      expect(switchOn(fixture)).toBe(true);
      expect(el(fixture, 'access-hint')?.textContent).toContain(
        'Nobody else but the administrators sees it at all',
      );
    });

    it('asks first, and says how many people are on the access list and who sees it then', async () => {
      const fixture = await render();

      toggle(fixture, true);
      await settle(fixture);

      expect(dialog()?.querySelector('.p-dialog-title')?.textContent).toBe('Restrict SEC?');
      expect(dialog()?.querySelector('.p-confirmdialog-message')?.textContent).toBe(
        "1 person is on its access list. Once it is restricted, only they and the team's administrators see it.",
      );
      expect(restrict).not.toHaveBeenCalled();
      expect(switchOn(fixture)).toBe(true);
      expect(el(fixture, 'project-restricted')?.querySelector('input')?.disabled).toBe(true);
      press('Keep it open');
      await settle(fixture);
    });

    it('says that only the administrators will see it when nobody is on the access list', async () => {
      const fixture = await render(project(), []);

      toggle(fixture, true);
      await settle(fixture);

      expect(dialog()?.querySelector('.p-confirmdialog-message')?.textContent).toBe(
        "Nobody is on its access list: once it is restricted, only the team's administrators see it.",
      );
      press('Keep it open');
      await settle(fixture);
    });

    it('names no number while the access list is not loaded', async () => {
      const fixture = await render(project(), 'open');

      toggle(fixture, true);
      await tick(fixture);

      expect(dialog()?.querySelector('.p-confirmdialog-message')?.textContent).toBe(
        restrictionNotice(undefined),
      );
      press('Keep it open');
      openLoad.flush({ items: [], next_cursor: null });
      await settle(fixture);
    });

    it('opens with the focus on the button that keeps the project open', async () => {
      const fixture = await render();

      toggle(fixture, true);
      await settle(fixture);
      await new Promise((resolve) => setTimeout(resolve, 50));

      expect((document.activeElement as HTMLElement | null)?.textContent?.trim()).toBe(
        'Keep it open',
      );
      press('Keep it open');
      await settle(fixture);
    });

    it('restricts the project over the version that was read once it is confirmed', async () => {
      const shown = project();
      const fixture = await render(shown);

      await restrictConfirmed(fixture);

      expect(restrict).toHaveBeenCalledExactlyOnceWith(shown, true);
    });

    it('leaves the project open, and the switch off, when the administrator keeps it open', async () => {
      const fixture = await render();
      toggle(fixture, true);
      await settle(fixture);

      press('Keep it open');
      await settle(fixture);

      expect(restrict).not.toHaveBeenCalled();
      expect(switchOn(fixture)).toBe(false);
      expect(el(fixture, 'project-restricted')?.querySelector('input')?.disabled).toBe(false);
    });

    it('drops the question when the page turns to another project before it is answered', async () => {
      const fixture = await render();
      toggle(fixture, true);
      await settle(fixture);
      expect(dialog()).not.toBeNull();

      fixture.componentRef.setInput('project', project({ id: 'id-OPS', key: 'OPS' }));
      await tick(fixture);
      listLoad('OPS').flush({ items: [], next_cursor: null });
      await settle(fixture);

      expect(dialog()).toBeNull();
      expect(switchOn(fixture)).toBe(false);
      expect(el(fixture, 'project-restricted')?.querySelector('input')?.disabled).toBe(false);
      expect(restrict).not.toHaveBeenCalled();
    });

    describe('opening a restricted project', () => {
      it('asks first, and says that every member will see it and its tickets', async () => {
        const fixture = await render(project({ restricted: true }));

        toggle(fixture, false);
        await settle(fixture);

        expect(dialog()?.querySelector('.p-dialog-title')?.textContent).toBe(
          'Open SEC to every member?',
        );
        expect(dialog()?.querySelector('.p-confirmdialog-message')?.textContent).toBe(
          'SEC and its tickets become visible to every member of the team. A confidential ' +
            'ticket stays visible only to the administrators, its assignee and its reporter.',
        );
        expect(restrict).not.toHaveBeenCalled();
        expect(switchOn(fixture)).toBe(false);
        expect(el(fixture, 'project-restricted')?.querySelector('input')?.disabled).toBe(true);
        press('Keep it restricted');
        await settle(fixture);
      });

      it('opens with the focus on the button that keeps the project restricted', async () => {
        const fixture = await render(project({ restricted: true }));

        toggle(fixture, false);
        await settle(fixture);
        await new Promise((resolve) => setTimeout(resolve, 50));

        expect((document.activeElement as HTMLElement | null)?.textContent?.trim()).toBe(
          'Keep it restricted',
        );
        press('Keep it restricted');
        await settle(fixture);
      });

      it('opens the project over the version that was read once it is confirmed', async () => {
        const shown = project({ restricted: true });
        const fixture = await render(shown);
        toggle(fixture, false);
        await settle(fixture);

        press('Open it');
        await settle(fixture);

        expect(restrict).toHaveBeenCalledExactlyOnceWith(shown, false);
      });

      it('leaves the project restricted, and the switch on, when the administrator keeps it so', async () => {
        const fixture = await render(project({ restricted: true }));
        toggle(fixture, false);
        await settle(fixture);

        press('Keep it restricted');
        await settle(fixture);

        expect(restrict).not.toHaveBeenCalled();
        expect(switchOn(fixture)).toBe(true);
        expect(el(fixture, 'project-restricted')?.querySelector('input')?.disabled).toBe(false);
      });
    });

    it('shows the switch as set, and holds it, until the answer is in', async () => {
      let finish: (changed: Project) => void = () => undefined;
      restrict.mockReturnValue(new Promise<Project>((resolve) => (finish = resolve)));
      const fixture = await render();

      await restrictConfirmed(fixture);
      expect(switchOn(fixture)).toBe(true);
      expect(el(fixture, 'project-restricted')?.querySelector('input')?.disabled).toBe(true);

      // The service puts the answer into the list, which the page hands in.
      const changed = project({ restricted: true, version: 5 });
      fixture.componentRef.setInput('project', changed);
      finish(changed);
      await settle(fixture);

      expect(switchOn(fixture)).toBe(true);
      expect(el(fixture, 'project-restricted')?.querySelector('input')?.disabled).toBe(false);
    });

    it('goes back and says so when somebody changed the project meanwhile, and loads the projects again', async () => {
      restrict.mockRejectedValue(refusal(412, 'precondition_failed'));
      const add = toasts();
      const fixture = await render();

      await restrictConfirmed(fixture);

      expect(el(fixture, 'access-notice')?.textContent?.trim()).toBe(
        'SEC changed meanwhile: the page shows it as it is now. Set it again if you still want it.',
      );
      expect(projectsReload).toHaveBeenCalled();
      expect(switchOn(fixture)).toBe(false);
      expect(add).not.toHaveBeenCalled();
    });

    it('still says so once the projects have loaded again and hand in the project as it is now', async () => {
      restrict.mockRejectedValue(refusal(412, 'precondition_failed'));
      const fixture = await render();
      await restrictConfirmed(fixture);

      fixture.componentRef.setInput('project', project({ version: 6, description: 'Changed' }));
      await settle(fixture);

      expect(el(fixture, 'access-notice')?.textContent?.trim()).toBe(
        'SEC changed meanwhile: the page shows it as it is now. Set it again if you still want it.',
      );
    });

    it('goes back and toasts any other refusal', async () => {
      restrict.mockRejectedValue(refusal(403, 'forbidden', 'Only an administrator restricts.'));
      const add = toasts();
      const fixture = await render();

      await restrictConfirmed(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'Only an administrator restricts.' }),
      );
      expect(switchOn(fixture)).toBe(false);
      expect(el(fixture, 'access-notice')).toBeNull();
    });
  });

  describe('the access list', () => {
    it('shows a row per entry with the name, the access and a way to take it off', async () => {
      const fixture = await render();

      expect([...host(fixture).querySelectorAll('th')].map((each) => each.textContent)).toEqual([
        'Name',
        'Access',
        '',
      ]);
      expect(el(fixture, 'access-p2')?.querySelector('td')?.textContent?.trim()).toBe('Sam Rivera');
      expect(shownRole(fixture, 'p2')).toBe('viewer');
      const select = fixture.debugElement.query(By.css('[data-testid="access-role-p2"]'))
        .componentInstance as Select;
      expect(select.options()).toEqual(['viewer', 'member']);
      expect(select.ariaLabel()).toBe('Access of Sam Rivera');
      expect(el(fixture, 'access-remove-p2')?.getAttribute('aria-label')).toBe(
        'Take Sam Rivera off the list',
      );
    });

    it('shows the e-mail address under the name, which tells two persons of one name apart', async () => {
      const fixture = await render(project(), [
        entry('p2', 'Sam Rivera', 'viewer', 'sam@example.com'),
        entry('p5', 'Sam Rivera', 'member', 'sam.rivera@globex.example'),
        entry('p1', 'Ada Lovelace', 'member', null),
      ]);

      const email = (id: string) =>
        el(fixture, `access-${id}`)?.querySelector('.email')?.textContent?.trim();
      expect(email('p2')).toBe('sam@example.com');
      expect(email('p5')).toBe('sam.rivera@globex.example');
      // A person without an address, such as a local account, shows the name alone.
      expect(el(fixture, 'access-p1')?.querySelector('.email')).toBeNull();
      expect(el(fixture, 'access-p1')?.querySelector('td')?.textContent?.trim()).toBe(
        'Ada Lovelace',
      );
    });

    it('says that nobody is on it, and not while it loads', async () => {
      const fixture = await render(project(), 'open');
      expect(el(fixture, 'access-empty')).toBeNull();

      openLoad.flush({ items: [], next_cursor: null });
      await settle(fixture);

      expect(el(fixture, 'access-empty')?.textContent?.trim()).toBe('Nobody is on the list.');
    });

    it('says why it could not be loaded', async () => {
      const fixture = await render(
        project(),
        refusal(503, 'not_ready', 'The database is starting.'),
      );

      expect(el(fixture, 'access-empty')?.textContent?.trim()).toBe(
        'The access list could not be loaded: The database is starting.',
      );
    });

    it('says why with the title when the problem has no detail', async () => {
      const fixture = await render(project(), refusal(403, 'forbidden', ''));

      expect(el(fixture, 'access-empty')?.textContent?.trim()).toBe(
        'The access list could not be loaded: Refused',
      );
    });

    describe('changing an entry', () => {
      it('gives the person the access chosen in the row', async () => {
        const fixture = await render();

        choose(fixture, 'access-role-p2', 'member');
        await settle(fixture);

        expect(set).toHaveBeenCalledExactlyOnceWith('p2', 'member');
      });

      it('changes nothing when the access chosen is the one there is', async () => {
        const fixture = await render();

        choose(fixture, 'access-role-p2', 'viewer');
        await settle(fixture);

        expect(set).not.toHaveBeenCalled();
      });

      it('shows the access chosen, and holds the select, until the answer is in', async () => {
        let finish: (changed: ProjectAccessEntry) => void = () => undefined;
        const fixture = await render();
        set.mockReturnValue(new Promise<ProjectAccessEntry>((resolve) => (finish = resolve)));

        choose(fixture, 'access-role-p2', 'member');
        await settle(fixture);
        expect(shownRole(fixture, 'p2')).toBe('member');
        expect(el(fixture, 'access-role-p2')?.classList).toContain('p-disabled');

        // The list puts the answer in before the write resolves.
        const changed = entry('p2', 'Sam Rivera', 'member');
        access.entries.set([changed]);
        finish(changed);
        await settle(fixture);

        expect(shownRole(fixture, 'p2')).toBe('member');
        expect(el(fixture, 'access-role-p2')?.classList).not.toContain('p-disabled');
      });

      it('goes back, says so and loads again when the person is no longer a member of the tenant', async () => {
        const fixture = await render();
        set.mockRejectedValue(refusal(404, 'person_not_found'));

        choose(fixture, 'access-role-p2', 'member');
        await settle(fixture);

        expect(el(fixture, 'access-notice')?.textContent?.trim()).toBe(
          'Sam Rivera is no longer a member of this team.',
        );
        expect(membersReload).toHaveBeenCalled();
        expect(entriesReload).toHaveBeenCalled();
        expect(shownRole(fixture, 'p2')).toBe('viewer');
      });

      it('goes back and toasts any other refusal', async () => {
        const add = toasts();
        const fixture = await render();
        set.mockRejectedValue(refusal(403, 'forbidden', 'Not for you.'));

        choose(fixture, 'access-role-p2', 'member');
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({ detail: 'Not for you.' }),
        );
        expect(shownRole(fixture, 'p2')).toBe('viewer');
      });

      it('lets the message be closed', async () => {
        const fixture = await render();
        set.mockRejectedValue(refusal(404, 'person_not_found'));
        choose(fixture, 'access-role-p2', 'member');
        await settle(fixture);

        el(fixture, 'access-notice')?.querySelector('button')?.click();
        await settle(fixture);

        expect(el(fixture, 'access-notice')).toBeNull();
      });
    });

    describe('taking a person off', () => {
      it('takes the person of the row off the list', async () => {
        const fixture = await render();

        el(fixture, 'access-remove-p2')?.click();
        await settle(fixture);

        expect(remove).toHaveBeenCalledExactlyOnceWith('p2');
      });

      it('toasts a refusal', async () => {
        const add = toasts();
        const fixture = await render();
        remove.mockRejectedValue(refusal(403, 'forbidden', 'Not for you.'));

        el(fixture, 'access-remove-p2')?.click();
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({ detail: 'Not for you.' }),
        );
      });

      it('holds the row while the removal is out: no second removal, no change', async () => {
        let finish: () => void = () => undefined;
        const fixture = await render();
        remove.mockReturnValue(new Promise<void>((resolve) => (finish = resolve)));

        el(fixture, 'access-remove-p2')?.click();
        await settle(fixture);
        expect((el(fixture, 'access-remove-p2') as HTMLButtonElement).disabled).toBe(true);
        expect(el(fixture, 'access-role-p2')?.classList).toContain('p-disabled');

        await fixture.componentInstance['remove'](access.list()[0]);
        choose(fixture, 'access-role-p2', 'member');
        await settle(fixture);
        expect(remove).toHaveBeenCalledOnce();
        expect(set).not.toHaveBeenCalled();

        finish();
        await settle(fixture);
        expect((el(fixture, 'access-remove-p2') as HTMLButtonElement).disabled).toBe(false);
        expect(el(fixture, 'access-role-p2')?.classList).not.toContain('p-disabled');
      });

      it('cannot take a person off while the change of their entry is out', async () => {
        let finish: (changed: ProjectAccessEntry) => void = () => undefined;
        const fixture = await render();
        set.mockReturnValue(new Promise<ProjectAccessEntry>((resolve) => (finish = resolve)));

        choose(fixture, 'access-role-p2', 'member');
        await settle(fixture);
        expect((el(fixture, 'access-remove-p2') as HTMLButtonElement).disabled).toBe(true);
        await fixture.componentInstance['remove'](access.list()[0]);
        expect(remove).not.toHaveBeenCalled();

        finish(entry('p2', 'Sam Rivera', 'member'));
        await settle(fixture);
        expect((el(fixture, 'access-remove-p2') as HTMLButtonElement).disabled).toBe(false);
      });
    });

    describe('putting a member on it', () => {
      const addButton = (fixture: ComponentFixture<ProjectAccess>) =>
        el(fixture, 'access-add') as HTMLButtonElement;

      /** The picker, and the labels of the options it holds. */
      const picker = (fixture: ComponentFixture<ProjectAccess>) =>
        fixture.debugElement.query(By.css('[data-testid="access-person"]'))
          .componentInstance as Select;
      const labels = (options: { label: string }[]) => options.map((each) => each.label);

      it("offers the tenant's members who are not on it yet, by name", async () => {
        const fixture = await render();

        const select = picker(fixture);
        expect(labels(select.options() as { label: string }[])).toEqual([
          'Ada Lovelace',
          'Bob Example',
        ]);
        expect(select.optionLabel()).toBe('label');
        expect(select.optionValue()).toBe('id');
        expect(select.ariaLabel()).toBe('Member to put on the list');
      });

      it('offers each member with the e-mail address beside the name, and finds them by it', async () => {
        members.set([
          member('p1', 'Ada Lovelace', 'ada@example.com'),
          member('p2', 'Sam Rivera', 'sam@example.com'),
          member('p3', 'Bob Example'),
          member('p4', 'Sam Rivera', 'sam.rivera@globex.example'),
        ]);
        const fixture = await render(project(), []);
        const select = picker(fixture);

        // The label is what an option shows and what a screen reader names it.
        expect(labels(select.options() as { label: string }[])).toEqual([
          'Ada Lovelace (ada@example.com)',
          'Sam Rivera (sam@example.com)',
          'Bob Example',
          'Sam Rivera (sam.rivera@globex.example)',
        ]);
        select._filterValue.set('globex');
        expect(labels(select.visibleOptions())).toEqual(['Sam Rivera (sam.rivera@globex.example)']);
        select._filterValue.set('Rivera');
        expect(labels(select.visibleOptions())).toEqual([
          'Sam Rivera (sam@example.com)',
          'Sam Rivera (sam.rivera@globex.example)',
        ]);
      });
      it('offers viewer and member, member chosen, and says what the chosen one allows', async () => {
        const fixture = await render();

        const select = fixture.debugElement.query(By.css('[data-testid="access-new-role"]'))
          .componentInstance as Select;
        expect(select.options()).toEqual(['viewer', 'member']);
        expect(el(fixture, 'access-meaning')?.textContent).toContain(
          `member: ${accessMeanings.member}`,
        );

        choose(fixture, 'access-new-role', 'viewer');

        expect(el(fixture, 'access-meaning')?.textContent).toContain(
          `viewer: ${accessMeanings.viewer}`,
        );
        expect(el(fixture, 'access-meaning')?.textContent).toContain(
          "The team's administrators see every project without an entry.",
        );
      });

      it('cannot add before a member is chosen', async () => {
        const fixture = await render();
        expect(addButton(fixture).disabled).toBe(true);

        submit(fixture);
        await settle(fixture);

        expect(set).not.toHaveBeenCalled();
      });

      it('puts the member chosen on the list with the access chosen, and starts the choice again', async () => {
        const fixture = await render();
        choose(fixture, 'access-person', 'p3');
        choose(fixture, 'access-new-role', 'viewer');
        expect(addButton(fixture).disabled).toBe(false);

        submit(fixture);
        await settle(fixture);

        expect(set).toHaveBeenCalledExactlyOnceWith('p3', 'viewer');
        expect(fixture.componentInstance['candidate']()).toBeNull();
      });

      it('adds once while the request is out, and shows its button as busy', async () => {
        let finish: (added: ProjectAccessEntry) => void = () => undefined;
        const fixture = await render();
        set.mockReturnValue(new Promise<ProjectAccessEntry>((resolve) => (finish = resolve)));
        choose(fixture, 'access-person', 'p3');

        submit(fixture);
        submit(fixture);
        await settle(fixture);

        expect(set).toHaveBeenCalledOnce();
        expect(addButton(fixture).disabled).toBe(true);
        expect(addButton(fixture).querySelector('i.pi-spinner')).not.toBeNull();
        finish(entry('p3', 'Bob Example'));
        await settle(fixture);
        expect(addButton(fixture).querySelector('i.pi-spinner')).toBeNull();
      });

      it('adds nobody for a choice that is no member any more', async () => {
        const fixture = await render();
        choose(fixture, 'access-person', 'p3');
        members.set([member('p1', 'Ada Lovelace')]);
        await settle(fixture);

        await fixture.componentInstance['add']();

        expect(set).not.toHaveBeenCalled();
      });

      it('says so and keeps the choice when the member left the tenant meanwhile', async () => {
        const fixture = await render();
        set.mockRejectedValue(refusal(404, 'person_not_found'));
        choose(fixture, 'access-person', 'p3');

        submit(fixture);
        await settle(fixture);

        expect(el(fixture, 'access-notice')?.textContent?.trim()).toBe(
          'Bob Example is no longer a member of this team.',
        );
        expect(fixture.componentInstance['candidate']()).toBe('p3');
      });
    });
  });
});

describe('restrictionNotice', () => {
  it.each<[number | undefined, string]>([
    [
      0,
      "Nobody is on its access list: once it is restricted, only the team's administrators see it.",
    ],
    [
      1,
      "1 person is on its access list. Once it is restricted, only they and the team's administrators see it.",
    ],
    [
      3,
      "3 people are on its access list. Once it is restricted, only they and the team's administrators see it.",
    ],
    [
      undefined,
      "Once it is restricted, only the team's administrators and the people on its access list see it.",
    ],
  ])('says for %s people on the list: %s', (people, said) => {
    expect(restrictionNotice(people)).toBe(said);
  });
});
