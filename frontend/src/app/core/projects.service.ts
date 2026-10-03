import { computed, inject, Injectable, Injector, resource } from '@angular/core';
import { Api } from '../api/api';
import { archiveProject, createProject, listProjects, updateProject } from '../api/functions';
import { Project, ProjectCreate, ProjectPatch } from '../api/models';
import { etagOf } from './entity-cache';
import { refresh } from './refresh';
import { SessionService } from './session.service';

/** The projects of the tenant the pages show, every page of them, for the navigation. */
@Injectable({ providedIn: 'root' })
export class ProjectsService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

  readonly projects = resource({
    params: () => this.session.tenant() ?? undefined,
    loader: async ({ params: tenant }) => {
      const projects: Project[] = [];
      let cursor: string | undefined;
      do {
        const page = await this.api.invoke(listProjects, { tenant, cursor, limit: 200 });
        projects.push(...page.items);
        cursor = page.next_cursor ?? undefined;
      } while (cursor);
      return projects;
    },
  });

  readonly list = computed<Project[]>(() =>
    this.projects.hasValue() ? this.projects.value() : [],
  );

  byKey(key: string): Project | undefined {
    return this.list().find((project) => project.key === key);
  }

  /** Project acts are not on the event stream: each write reloads the list itself. */
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
