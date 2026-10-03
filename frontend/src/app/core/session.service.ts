import { computed, inject, Injectable, resource, signal } from '@angular/core';
import { Api } from '../api/api';
import { getMe } from '../api/functions';
import { Membership } from '../api/models';

/**
 * Who is working and in which tenant (docs/adr/0053 D1): the person and their memberships from
 * `GET /api/v1/me`, and the tenant the pages show, taken from the route (docs/adr/0023 D4). The
 * tenant-scoped services follow `tenant()` and drop what they hold when it changes (D4).
 */
@Injectable({ providedIn: 'root' })
export class SessionService {
  private readonly api = inject(Api);

  readonly me = resource({ loader: () => this.api.invoke(getMe) });
  readonly person = computed(() => (this.me.hasValue() ? this.me.value() : undefined));
  readonly memberships = computed<Membership[]>(() => this.person()?.memberships ?? []);

  /** The tenant of the current page; null on the person-level pages. */
  readonly tenant = signal<string | null>(null);
  readonly membership = computed(() =>
    this.memberships().find((membership) => membership.tenant.slug === this.tenant()),
  );
  /** The only tenant when there is exactly one; it gets no switcher (docs/adr/0023 D4). */
  readonly soleTenant = computed(() => {
    const memberships = this.memberships();
    return memberships.length === 1 ? memberships[0].tenant.slug : null;
  });

  enter(tenant: string | null): void {
    if (this.tenant() !== tenant) {
      this.tenant.set(tenant);
    }
  }
}
