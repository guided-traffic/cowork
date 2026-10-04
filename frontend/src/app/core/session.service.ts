import {
  computed,
  inject,
  Injectable,
  Injector,
  resource,
  ResourceRef,
  signal,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import { getMe } from '../api/functions';
import { Me, Membership } from '../api/models';
import { changesMemberships, EventStreamService } from './event-stream.service';
import { keepShown, refresh } from './refresh';

/**
 * Who is working and in which tenant (docs/adr/0053 D1): the person and their memberships from
 * `GET /api/v1/me`, and the tenant the pages show, taken from the route (docs/adr/0023 D4). The
 * tenant-scoped services follow `tenant()` and drop what they hold when it changes (D4). A role
 * that a grant, a mapping or the person's groups change (docs/adr/0030) is loaded again when the
 * tenant's stream says so, or may have missed it, so the pages offer what the new role allows
 * without a reload; a load again that fails keeps the person shown ({@link keepShown}).
 */
@Injectable({ providedIn: 'root' })
export class SessionService {
  private readonly api = inject(Api);
  private readonly injector = inject(Injector);

  readonly me: ResourceRef<Me | undefined> = resource({
    loader: () => keepShown(this.me, () => this.api.invoke(getMe)),
  });
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

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (changesMemberships(event)) {
          refresh(this.me, this.injector);
        }
      });
  }

  enter(tenant: string | null): void {
    if (this.tenant() !== tenant) {
      this.tenant.set(tenant);
    }
  }
}
