import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink } from '@angular/router';
import { Tooltip } from 'primeng/tooltip';
import { filter, map } from 'rxjs';
import { Project } from '../api/models';
import { MyProjectsService } from '../core/my-projects.service';
import { ProjectsService } from '../core/projects.service';
import { SessionService } from '../core/session.service';
import { NewProjectDialog } from '../features/project/new-project-dialog';
import { configTabs, gearTab, openedFromPage } from '../features/tenant/config-tabs';

/** Where the browser remembers the groups the person collapsed, by their teams' slugs. */
export const collapsedKey = 'cowork.nav.collapsed';

/** Where a page stands in the sidebar: its team, and the project of a project's page or a ticket's. */
export interface Place {
  team: string | null;
  project: string | null;
  /** The page is the team's dashboard, its front page. */
  front: boolean;
}

/**
 * Where the page of a URL stands: `/t/<team>` and every page below it name the team, a project's
 * pages `/t/<team>/p/<KEY>/…` and a ticket's page `/t/<team>/tickets/<KEY>-<number>` the project.
 */
export function placeOf(router: Router, url: string): Place {
  const path = router.parseUrl(url).root.children['primary']?.segments.map((s) => s.path) ?? [];
  if (path[0] !== 't' || path.length < 2) {
    return { team: null, project: null, front: false };
  }
  let project: string | null = null;
  if (path[2] === 'p' && path[3]) {
    project = path[3];
  } else if (path[2] === 'tickets' && path[3]?.includes('-')) {
    project = path[3].slice(0, path[3].lastIndexOf('-'));
  }
  return { team: path[1], project, front: path.length === 2 };
}

/** A team's group in the sidebar. */
export interface TeamGroup {
  slug: string;
  name: string;
  /** The team of the page: its group is always open. */
  current: boolean;
  open: boolean;
  /** The tab of the configuration the gear opens. */
  gear: string;
  canCreate: boolean;
  projects: Project[];
  /** The projects are not known yet: no "no projects yet" either. */
  loading: boolean;
  /** The key of the project the page belongs to, in this team. */
  activeProject: string | null;
  /** The page is this team's dashboard. */
  front: boolean;
}

/**
 * The sidebar's groups below "For you" (docs/adr/0023 D4 as amended 2026-10-10): one for every
 * team the person is a member of, by slug, on every page — a team a global administrator only
 * oversees has none (docs/adr/0034 D2). A group is the team's name, which leads to its dashboard;
 * a gear beside it, which opens the team's configuration over the dashboard (`TeamConfig`); a plus
 * where the person may create a project in the team (`can_create_projects` of the membership), which
 * creates it in that team; and the team's projects by key, archived ones left out, each leading to
 * its board. The current team's projects are `ProjectsService`'s, kept live by the team's events,
 * the other teams' `MyProjectsService`'s. A group collapses, and the browser remembers it per team
 * (`localStorage`, a convenience only, docs/adr/0053 D6); the current team's group is always open,
 * its name and the current project marked.
 */
@Component({
  selector: 'app-team-nav',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NewProjectDialog, RouterLink, Tooltip],
  templateUrl: './team-nav.html',
  styleUrl: './team-nav.scss',
})
export class TeamNav {
  private readonly session = inject(SessionService);
  private readonly projects = inject(ProjectsService);
  private readonly myProjects = inject(MyProjectsService);
  private readonly router = inject(Router);
  private readonly document = inject(DOCUMENT);

  protected readonly openedFromPage = openedFromPage;
  /** The team a new project is created in, and whether its dialog is open. */
  protected readonly creatingIn = signal<string | null>(null);
  protected readonly creating = signal(false);
  private readonly collapsed = signal<ReadonlySet<string>>(this.stored());

  private readonly place = toSignal(
    this.router.events.pipe(
      filter((event): event is NavigationEnd => event instanceof NavigationEnd),
      map((event) => placeOf(this.router, event.urlAfterRedirects)),
    ),
    { initialValue: placeOf(this.router, this.router.url) },
  );

  protected readonly groups = computed<TeamGroup[]>(() => {
    const current = this.session.tenant();
    const collapsed = this.collapsed();
    const place = this.place();
    return [...this.session.memberships()]
      .sort((a, b) => (a.team.slug < b.team.slug ? -1 : a.team.slug > b.team.slug ? 1 : 0))
      .map(({ team, role, can_create_projects }) => {
        const isCurrent = team.slug === current;
        const live = isCurrent && this.projects.projects.hasValue();
        const here = place.team === team.slug;
        return {
          slug: team.slug,
          name: team.name,
          current: isCurrent,
          open: isCurrent || !collapsed.has(team.slug),
          gear: gearTab(configTabs(role === 'admin', false)),
          canCreate: can_create_projects,
          projects: live ? this.projects.list() : this.myProjects.of(team.slug),
          loading: !live && !this.myProjects.projects.hasValue(),
          activeProject: here ? place.project : null,
          front: here && place.front,
        };
      });
  });

  /** Collapses an open group or opens a collapsed one, and remembers it. */
  protected toggle(slug: string): void {
    const next = new Set(this.collapsed());
    if (!next.delete(slug)) {
      next.add(slug);
    }
    this.collapsed.set(next);
    try {
      this.document.defaultView?.localStorage.setItem(collapsedKey, JSON.stringify([...next]));
    } catch {
      // Storage refused (private mode, quota): the choice holds for this page only.
    }
  }

  protected create(slug: string): void {
    this.creatingIn.set(slug);
    this.creating.set(true);
  }

  /** The groups the browser remembers collapsed; none where it remembers nothing or refuses. */
  private stored(): ReadonlySet<string> {
    try {
      const value: unknown = JSON.parse(
        this.document.defaultView?.localStorage.getItem(collapsedKey) ?? '[]',
      );
      return new Set(
        Array.isArray(value) ? value.filter((slug) => typeof slug === 'string') : [],
      );
    } catch {
      return new Set();
    }
  }
}
