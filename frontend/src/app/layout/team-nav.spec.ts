import { Location } from '@angular/common';
import { provideLocationMocks } from '@angular/common/testing';
import { Component, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Membership, Project } from '../api/models';
import { MyProjectsService } from '../core/my-projects.service';
import { ProjectsService } from '../core/projects.service';
import { SessionService } from '../core/session.service';
import { NewProjectDialog } from '../features/project/new-project-dialog';
import { collapsedKey, placeOf, TeamNav } from './team-nav';

function membership(slug: string, name: string, role: Membership['role'], create = true): Membership {
  return {
    role,
    team: { slug, name },
    tenant: { slug, name },
    origins: [{ source: 'grant', role }],
    can_create_projects: create,
  };
}

const acme = membership('acme', 'Acme Corp', 'admin');
const globex = membership('globex', 'Globex', 'viewer', false);
const initech = membership('initech', 'Initech', 'member');

function project(key: string, name = `Project ${key}`): Project {
  return {
    id: `id-${key}`,
    key,
    name,
    description: '',
    restricted: false,
    wip_limits: {},
    version: 1,
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-01T09:00:00Z',
  };
}

@Component({ template: '<p>a routed page</p>' })
class Page {}

describe('placeOf', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
  });

  it.each([
    ['/me/next', { team: null, project: null, front: false }],
    ['/', { team: null, project: null, front: false }],
    ['/teams', { team: null, project: null, front: false }],
    ['/t/acme', { team: 'acme', project: null, front: true }],
    ['/t/acme?project=COW&from=2026-09-01', { team: 'acme', project: null, front: true }],
    ['/t/acme/board', { team: 'acme', project: null, front: false }],
    ['/t/acme/settings', { team: 'acme', project: null, front: false }],
    ['/t/acme/tickets', { team: 'acme', project: null, front: false }],
    ['/t/acme/p/COW/board', { team: 'acme', project: 'COW', front: false }],
    ['/t/acme/p/COW/backlog?closed=true', { team: 'acme', project: 'COW', front: false }],
    ['/t/acme/p/COW/imports/0199a3c2', { team: 'acme', project: 'COW', front: false }],
    ['/t/acme/tickets/COW-12', { team: 'acme', project: 'COW', front: false }],
    ['/t/acme/tickets/A1B-7#comment-3', { team: 'acme', project: 'A1B', front: false }],
  ])('places %s', (url, place) => {
    expect(placeOf(TestBed.inject(Router), url)).toEqual(place);
  });
});

describe('TeamNav', () => {
  let memberships: WritableSignal<Membership[]>;
  let tenant: WritableSignal<string | null>;
  let current: WritableSignal<Project[]>;
  let currentKnown: WritableSignal<boolean>;
  let theirs: WritableSignal<Record<string, Project[]>>;
  let theirsKnown: WritableSignal<boolean>;

  beforeEach(() => {
    localStorage.clear();
    memberships = signal([globex, acme]);
    tenant = signal<string | null>(null);
    current = signal<Project[]>([]);
    currentKnown = signal(false);
    theirs = signal<Record<string, Project[]>>({
      acme: [project('COW', 'Cowork'), project('OPS', 'Operations')],
      globex: [project('WEB', 'Website')],
    });
    theirsKnown = signal(true);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([{ path: '**', component: Page }]),
        provideLocationMocks(),
        MessageService,
        { provide: SessionService, useValue: { memberships, tenant } },
        {
          provide: ProjectsService,
          useValue: { list: current, projects: { hasValue: () => currentKnown() } },
        },
        {
          provide: MyProjectsService,
          useValue: {
            of: (team: string) => theirs()[team] ?? [],
            projects: { hasValue: () => theirsKnown() },
          },
        },
      ],
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  async function render(): Promise<{ fixture: ComponentFixture<TeamNav>; page: HTMLElement }> {
    const fixture = TestBed.createComponent(TeamNav);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const group = (page: HTMLElement, slug: string) =>
    page.querySelector<HTMLElement>(`[data-team="${slug}"]`);
  const projectsOf = (page: HTMLElement, slug: string) =>
    [...(group(page, slug)?.querySelectorAll('a.project') ?? [])].map((link) => [
      link.querySelector('.key')?.textContent,
      link.querySelector('.name')?.textContent,
      link.getAttribute('href'),
    ]);
  const byTestId = (page: HTMLElement, testId: string) =>
    page.querySelector<HTMLElement>(`[data-testid="${testId}"]`);

  describe('the groups', () => {
    it('are the teams of the person by slug, each its name leading to its dashboard', async () => {
      const { page } = await render();

      const teams = [...page.querySelectorAll('[data-team]')].map((each) =>
        each.getAttribute('data-team'),
      );
      expect(teams).toEqual(['acme', 'globex']);
      expect(byTestId(page, 'nav-team-acme')?.textContent?.trim()).toBe('Acme Corp');
      expect(byTestId(page, 'nav-team-acme')?.getAttribute('href')).toBe('/t/acme');
      expect(byTestId(page, 'nav-team-globex')?.getAttribute('href')).toBe('/t/globex');
    });

    it("list each team's projects by key, each leading to its board", async () => {
      const { page } = await render();

      expect(projectsOf(page, 'acme')).toEqual([
        ['COW', 'Cowork', '/t/acme/p/COW/board'],
        ['OPS', 'Operations', '/t/acme/p/OPS/board'],
      ]);
      expect(projectsOf(page, 'globex')).toEqual([['WEB', 'Website', '/t/globex/p/WEB/board']]);
      expect(byTestId(page, 'nav-project-acme-COW')).not.toBeNull();
    });

    it("show the current team's projects as the team's own list has them, kept live by its events", async () => {
      tenant.set('acme');
      current.set([project('COW', 'Cowork'), project('NEW', 'Made a moment ago')]);
      currentKnown.set(true);

      const { page } = await render();

      expect(projectsOf(page, 'acme').map(([key]) => key)).toEqual(['COW', 'NEW']);
      expect(projectsOf(page, 'globex').map(([key]) => key)).toEqual(['WEB']);
    });

    it("show the person's list of the current team while its own list is loading", async () => {
      tenant.set('acme');

      const { page } = await render();

      expect(projectsOf(page, 'acme').map(([key]) => key)).toEqual(['COW', 'OPS']);
    });

    it('say that a team has no projects yet once that is known, and nothing while it is not', async () => {
      theirs.set({ acme: [] });
      const { fixture, page } = await render();
      expect(group(page, 'globex')?.querySelector('.empty')?.textContent).toBe('No projects yet');

      theirsKnown.set(false);
      await fixture.whenStable();

      expect(group(page, 'globex')?.querySelector('.empty')).toBeNull();
    });

    it('follow the memberships: a team joined appears, a team left goes', async () => {
      const { fixture, page } = await render();

      memberships.set([acme, initech]);
      await fixture.whenStable();

      expect([...page.querySelectorAll('[data-team]')].map((each) => each.getAttribute('data-team'))).toEqual(
        ['acme', 'initech'],
      );
    });
  });

  describe("a team's gear", () => {
    it('opens the settings of the team, whatever role the person holds there', async () => {
      const { page } = await render();

      for (const slug of ['acme', 'globex']) {
        expect(byTestId(page, `nav-team-config-${slug}`)?.getAttribute('href')).toBe(
          `/t/${slug}/settings`,
        );
      }
      expect(byTestId(page, 'nav-team-config-acme')?.getAttribute('aria-label')).toBe(
        'Configuration of Acme Corp',
      );
    });

    it('opens the dialog as a link of this application, which closing goes back from (team-config.ts)', async () => {
      const { fixture, page } = await render();

      byTestId(page, 'nav-team-config-globex')?.click();
      await fixture.whenStable();

      expect(TestBed.inject(Router).url).toBe('/t/globex/settings');
      expect(TestBed.inject(Location).getState()).toMatchObject({ openedFromPage: true });
    });
  });

  describe("a team's plus", () => {
    it('is offered in a team the person may create projects in, and in no other', async () => {
      const { page } = await render();

      expect(byTestId(page, 'nav-new-project-acme')?.getAttribute('aria-label')).toBe(
        'New project in Acme Corp',
      );
      expect(byTestId(page, 'nav-new-project-globex')).toBeNull();
    });

    it('opens the dialog for a new project in that team, whichever team the pages show', async () => {
      tenant.set('globex');
      memberships.set([acme, membership('globex', 'Globex', 'admin')]);
      const { fixture, page } = await render();
      const dialog = fixture.debugElement.query(By.directive(NewProjectDialog))
        .componentInstance as NewProjectDialog;
      expect(dialog.visible()).toBe(false);

      byTestId(page, 'nav-new-project-acme')?.click();
      await fixture.whenStable();

      expect(dialog.visible()).toBe(true);
      expect(dialog.team()).toBe('acme');

      dialog.visible.set(false);
      await fixture.whenStable();
      byTestId(page, 'nav-new-project-globex')?.click();
      await fixture.whenStable();

      expect(dialog.visible()).toBe(true);
      expect(dialog.team()).toBe('globex');
    });
  });

  describe('collapsing', () => {
    const toggle = (page: HTMLElement, slug: string) =>
      byTestId(page, `nav-team-toggle-${slug}`) as HTMLButtonElement;

    it('hides a group’s projects and says so to a screen reader, and opens them again', async () => {
      const { fixture, page } = await render();
      expect(toggle(page, 'acme').getAttribute('aria-expanded')).toBe('true');
      expect(toggle(page, 'acme').getAttribute('aria-controls')).toBe('nav-team-projects-acme');
      expect(toggle(page, 'acme').getAttribute('aria-label')).toBe('Projects of Acme Corp');

      toggle(page, 'acme').click();
      await fixture.whenStable();

      expect(toggle(page, 'acme').getAttribute('aria-expanded')).toBe('false');
      expect(projectsOf(page, 'acme')).toEqual([]);
      expect(projectsOf(page, 'globex')).toHaveLength(1);

      toggle(page, 'acme').click();
      await fixture.whenStable();

      expect(projectsOf(page, 'acme')).toHaveLength(2);
    });

    it('is remembered by the browser per team, for the next page that shows the sidebar', async () => {
      const first = await render();
      toggle(first.page, 'globex').click();
      await first.fixture.whenStable();
      expect(JSON.parse(localStorage.getItem(collapsedKey) ?? 'null')).toEqual(['globex']);
      first.fixture.destroy();

      const { page } = await render();

      expect(toggle(page, 'globex').getAttribute('aria-expanded')).toBe('false');
      expect(projectsOf(page, 'globex')).toEqual([]);
      expect(projectsOf(page, 'acme')).toHaveLength(2);
    });

    it('never closes the group of the team the pages show, and marks it current', async () => {
      localStorage.setItem(collapsedKey, JSON.stringify(['acme']));
      tenant.set('acme');

      const { fixture, page } = await render();

      expect(toggle(page, 'acme').disabled).toBe(true);
      expect(toggle(page, 'acme').getAttribute('aria-expanded')).toBe('true');
      expect(projectsOf(page, 'acme')).toHaveLength(2);
      expect(group(page, 'acme')?.classList).toContain('current');

      // Collapsed as remembered, once it is not the current team any more.
      tenant.set('globex');
      await fixture.whenStable();

      expect(projectsOf(page, 'acme')).toEqual([]);
    });

    it('reads nothing it did not write, and holds for the page where the browser refuses storage', async () => {
      localStorage.setItem(collapsedKey, '{"not": "a list"}');
      const odd = await render();
      expect(projectsOf(odd.page, 'acme')).toHaveLength(2);
      odd.fixture.destroy();

      vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
        throw new DOMException('denied', 'SecurityError');
      });
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new DOMException('denied', 'SecurityError');
      });
      const { fixture, page } = await render();
      expect(projectsOf(page, 'acme')).toHaveLength(2);

      toggle(page, 'acme').click();
      await fixture.whenStable();

      expect(projectsOf(page, 'acme')).toEqual([]);
    });
  });

  describe('the marks of where the page is', () => {
    const marked = (page: HTMLElement) =>
      [...page.querySelectorAll('a.item.active')].map((link) => [
        link.getAttribute('data-testid'),
        link.getAttribute('aria-current'),
      ]);

    async function at(url: string, team: string | null) {
      const shown = await render();
      tenant.set(team);
      await TestBed.inject(Router).navigateByUrl(url);
      await shown.fixture.whenStable();
      return shown;
    }

    it("marks the current team's name, as the page itself on its dashboard", async () => {
      const { page } = await at('/t/acme?project=COW', 'acme');

      expect(marked(page)).toEqual([['nav-team-acme', 'page']]);
    });

    it("marks the current team's name on its other pages, without saying it is the page", async () => {
      const { page } = await at('/t/acme/settings', 'acme');

      expect(marked(page)).toEqual([['nav-team-acme', 'true']]);
    });

    it.each(['/t/acme/p/COW/board', '/t/acme/p/COW/backlog', '/t/acme/tickets/COW-12'])(
      'marks the project of %s beside its team',
      async (url) => {
        const { page } = await at(url, 'acme');

        expect(marked(page)).toEqual([
          ['nav-team-acme', 'true'],
          ['nav-project-acme-COW', 'true'],
        ]);
      },
    );

    it('marks nothing on a page of no team', async () => {
      const { page } = await at('/me/next', null);

      expect(marked(page)).toEqual([]);
    });
  });
});
