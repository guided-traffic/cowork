import { computed, inject, Injectable, resource } from '@angular/core';
import { Api } from '../api/api';
import { getTeam } from '../api/fn/teams/get-team';
import { updateTeam } from '../api/fn/teams/update-team';
import { Team, TeamPatch } from '../api/models';
import { etagOf } from './entity-cache';
import { SessionService } from './session.service';

/**
 * The settings of the tenant the pages show (docs/adr/0034 D9, docs/adr/0017): who may create
 * projects, who sees others' time, until when time is locked. Not on the event stream — the
 * page that changes them reloads.
 */
@Injectable({ providedIn: 'root' })
export class TenantService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);

  readonly tenant = resource({
    params: () => this.session.tenant() ?? undefined,
    loader: ({ params: tenant }) => this.api.invoke(getTeam, { team: tenant }),
  });
  readonly value = computed<Team | undefined>(() =>
    this.tenant.hasValue() ? this.tenant.value() : undefined,
  );
  readonly isAdmin = computed(() => this.session.membership()?.role === 'admin');
  /** A member or an administrator: who writes in the tenant's projects; a viewer never. */
  readonly canWrite = computed(() => {
    const role = this.session.membership()?.role;
    return role === 'admin' || role === 'member';
  });
  /** An administrator always; a member while the tenant allows it; a viewer never. */
  readonly canCreateProjects = computed(() => {
    const role = this.session.membership()?.role;
    return (
      role === 'admin' || (role === 'member' && (this.value()?.members_create_projects ?? false))
    );
  });

  /** Writes the settings; the answer is shown only while its tenant is still the one entered. */
  async update(patch: TeamPatch): Promise<Team> {
    const slug = this.session.tenant() as string;
    const held = this.value();
    const tenant = await this.api.invoke(updateTeam, {
      team: slug,
      ...(held ? { 'If-Match': etagOf(held.version) } : {}),
      body: patch,
    });
    if (this.session.tenant() === slug) {
      this.tenant.set(tenant);
    }
    return tenant;
  }
}
