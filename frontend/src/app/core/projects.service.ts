import { computed, inject, Injectable, Injector, resource, ResourceRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import {
  archiveProject,
  createProject,
  listProjects,
  setProjectRestriction,
  updateProject,
} from '../api/functions';
import { Project, ProjectCreate, ProjectPatch } from '../api/models';
import { etagOf } from './entity-cache';
import { changesVisibility, EventStreamService } from './event-stream.service';
import { keepShown, refresh } from './refresh';
import { SessionService } from './session.service';

/**
 * The projects of the tenant the pages show, every page of them, for the navigation. Project acts
 * are not on the event stream, but who sees a project is (docs/adr/0034 D3): `membership.changed`
 * that names a project — its restriction set or lifted, an access entry changed — or the person's
 * own role loads the list again, and so do a resync and the fallback's poll, which may have missed
 * one (docs/adr/0054). A load again that fails keeps the list shown ({@link keepShown}).
 */
@Injectable({ providedIn: 'root' })
export class ProjectsService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

  readonly projects: ResourceRef<Project[] | undefined> = resource({
    params: () => this.session.tenant() ?? undefined,
    loader: ({ params: tenant }) =>
      keepShown(this.projects, async () => {
        const projects: Project[] = [];
        let cursor: string | undefined;
        do {
          const page = await this.api.invoke(listProjects, { tenant, cursor, limit: 200 });
          projects.push(...page.items);
          cursor = page.next_cursor ?? undefined;
        } while (cursor);
        return projects;
      }),
  });

  readonly list = computed<Project[]>(() =>
    this.projects.hasValue() ? this.projects.value() : [],
  );

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (
          event.name === 'resync' ||
          event.name === 'poll' ||
          (event.name === 'membership.changed' &&
            changesVisibility(event, this.session.person()?.id))
        ) {
          refresh(this.projects, this.injector);
        }
      });
  }

  byKey(key: string): Project | undefined {
    return this.list().find((project) => project.key === key);
  }

  /** Each write reloads the list itself. */
  async create(body: ProjectCreate): Promise<Project> {
    const project = await this.api.invoke(createProject, {
      tenant: this.session.tenant() as string,
      'Idempotency-Key': crypto.randomUUID(),
      body,
    });
    refresh(this.projects, this.injector);
    return project;
  }

  async update(project: Project, patch: ProjectPatch): Promise<Project> {
    const changed = await this.api.invoke(updateProject, {
      tenant: this.session.tenant() as string,
      project: project.key,
      'If-Match': etagOf(project.version),
      body: patch,
    });
    refresh(this.projects, this.injector);
    return changed;
  }

  /**
   * Restricts a project to the tenant's administrators and its access list, or opens it to every
   * member (docs/adr/0034 D3) — a setting of the project, written over the version that was read.
   * The project as the answer has it goes into the list at once, so that a switch that shows the
   * new setting does not jump back while the list loads; an answer that arrives after the pages
   * turned to another tenant is left out.
   */
  async restrict(project: Project, restricted: boolean): Promise<Project> {
    const tenant = this.session.tenant() as string;
    const changed = await this.api.invoke(setProjectRestriction, {
      tenant,
      project: project.key,
      'If-Match': etagOf(project.version),
      body: { restricted },
    });
    if (this.session.tenant() === tenant && this.projects.hasValue()) {
      this.projects.set(
        this.projects.value().map((each) => (each.key === changed.key ? changed : each)),
      );
    }
    refresh(this.projects, this.injector);
    return changed;
  }

  /** Archives a project: its tickets stay, it leaves the lists and takes no new ticket (docs/adr/0006 D4). */
  async archive(project: Project): Promise<Project> {
    const archived = await this.api.invoke(archiveProject, {
      tenant: this.session.tenant() as string,
      project: project.key,
    });
    refresh(this.projects, this.injector);
    return archived;
  }
}
