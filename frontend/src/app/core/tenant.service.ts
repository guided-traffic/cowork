import { computed, inject, Injectable, Injector, resource } from '@angular/core';
import { Api } from '../api/api';
import { getTeam } from '../api/fn/teams/get-team';
import { updateTeam } from '../api/fn/teams/update-team';
import { Team, TeamPatch } from '../api/models';
import { etagOf } from './entity-cache';
import { refresh } from './refresh';
import { SessionService } from './session.service';

/**
 * The settings of the tenant the pages show (docs/adr/0034 D9, docs/adr/0017): who may create
 * projects, who sees others' time, until when time is locked. Not on the event stream — the
 * page that changes them reloads. Whether the person may create a project in a team is its
 * membership's `can_create_projects` of `GET /api/v1/me`, which the sidebar's plus reads
 * (docs/adr/0023 D4 as amended 2026-10-10): a change of `members_create_projects` asks for the
 * person again.
 */
@Injectable({ providedIn: 'root' })
export class TenantService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

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

  /**
   * Writes the settings; the answer is shown only while its tenant is still the one entered. An
   * answer whose `members_create_projects` differs from the settings held asks for the person again
   * — once more after a load on its way, which may predate the change —, so that the plus of the
   * team's group follows it without waiting for the next entry into the team.
   */
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
    if (held?.members_create_projects !== tenant.members_create_projects) {
      refresh(this.session.me, this.injector);
    }
    return tenant;
  }
}
