import { DOCUMENT } from '@angular/common';
import {
  computed,
  DestroyRef,
  effect,
  inject,
  Injectable,
  Injector,
  resource,
  ResourceRef,
  untracked,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import { listMyProjects } from '../api/fn/me/list-my-projects';
import { MyProject, Project } from '../api/models';
import { ConditionalPages } from './conditional';
import { changesVisibility, EventStreamService } from './event-stream.service';
import { keepShown, refresh } from './refresh';
import { SessionService } from './session.service';

/** How long after one sign of the tab coming back another one loads nothing again. */
export const shownAgainWithin = 1000;

/**
 * The projects of every team of the person, for the sidebar's groups (docs/adr/0023 D2, D4 as
 * amended 2026-10-10): `GET /api/v1/me/projects`, every page of it, each through
 * {@link ConditionalPages}, so that a load that finds nothing new costs a `304`. The current team's
 * projects are `ProjectsService`'s, which the team's events keep live; this list stands in for the
 * other teams. What makes or renames a project reaches no event stream (events.md), so the list
 * loads again when the person may look at it anew: the tab shown again or the window's focus back,
 * the pages entering or leaving a team, a `membership.changed` of any team of the person that may
 * change what they see — a team joined or left, a role changed, a project restricted or opened
 * (`changesVisibility`) —, the person's teams changing in `me`, a `resync` and the fallback's
 * `poll`. A load again that fails keeps the list shown ({@link keepShown}).
 */
@Injectable({ providedIn: 'root' })
export class MyProjectsService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);
  private readonly document = inject(DOCUMENT);
  private readonly pages = new ConditionalPages(this.api);

  /**
   * The person's id, a primitive: `me` loaded again with the same person leaves the list alone
   * (a resource loads again every time its params function runs).
   */
  private readonly person = computed(() => this.session.person()?.id);
  /** The slugs of the person's teams as one string, which changes only when the teams do. */
  private readonly teams = computed(() =>
    this.session
      .memberships()
      .map((membership) => membership.team.slug)
      .sort()
      .join(' '),
  );

  readonly projects: ResourceRef<MyProject[] | undefined> = resource({
    params: () => this.person(),
    loader: () =>
      keepShown(this.projects, () =>
        this.pages.load(async (page) => {
          const projects: MyProject[] = [];
          let cursor: string | undefined;
          do {
            const next = await page(listMyProjects, { cursor, limit: 200 });
            projects.push(...next.items);
            cursor = next.next_cursor ?? undefined;
          } while (cursor);
          return projects;
        }),
      ),
  });

  /** The projects of each team by its slug, in the order of the list: by key. */
  readonly byTeam = computed(() => {
    const teams = new Map<string, Project[]>();
    for (const { team, project } of this.projects.hasValue() ? this.projects.value() : []) {
      teams.set(team.slug, [...(teams.get(team.slug) ?? []), project]);
    }
    return teams;
  });

  /** When the tab or the window last came back, for {@link shownAgainWithin}. */
  private shownAt = Number.NEGATIVE_INFINITY;

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (
          event.name === 'resync' ||
          event.name === 'poll' ||
          (event.name === 'membership.changed' && changesVisibility(event, this.person()))
        ) {
          this.reload();
        }
      });

    // The team the pages show: every change of it but the first loads the list again.
    let shown: string | null | undefined;
    effect(() => {
      const tenant = this.session.tenant();
      untracked(() => {
        if (shown !== undefined && shown !== tenant) {
          this.reload();
        }
        shown = tenant;
      });
    });
    // The person's teams, once the list holds them: a team joined or left, whichever way `me`
    // learnt of it — an event, a poll, a team made on the page of every team —, loads it again.
    let known: string | undefined;
    effect(() => {
      const teams = this.teams();
      untracked(() => {
        if (known !== undefined && known !== teams && this.projects.hasValue()) {
          this.reload();
        }
        known = teams;
      });
    });

    // A tab shown again and a window that gets the focus back usually come together: one load.
    const back = () => {
      const now = Date.now();
      if (this.document.visibilityState !== 'hidden' && now - this.shownAt >= shownAgainWithin) {
        this.shownAt = now;
        this.reload();
      }
    };
    const view = this.document.defaultView;
    this.document.addEventListener('visibilitychange', back);
    view?.addEventListener('focus', back);
    inject(DestroyRef).onDestroy(() => {
      this.document.removeEventListener('visibilitychange', back);
      view?.removeEventListener('focus', back);
    });
  }

  /** The projects of one team, by key; none while the list does not hold the team. */
  of(team: string): Project[] {
    return this.byTeam().get(team) ?? [];
  }

  /** Loads the list again — through `refresh`, so a load on its way is followed by one more. */
  reload(): void {
    refresh(this.projects, this.injector);
  }
}
