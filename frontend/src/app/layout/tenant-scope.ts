import { DestroyRef, Component, effect, inject, input } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { EventStreamService } from '../core/event-stream.service';
import { SessionService } from '../core/session.service';
import { SelfGrant } from '../features/tenant/self-grant';

/**
 * The pages under `/t/{slug}` (docs/adr/0023 D4): the slug becomes the session's tenant, which the
 * tenant-scoped services follow, and the tenant's event stream opens (docs/adr/0054 D1). A global
 * administrator who holds no role in the tenant sees its administration only, under the offer to
 * grant themselves a role, and its stream stays closed: it is its members' (docs/adr/0034 D2).
 */
@Component({
  selector: 'app-tenant-scope',
  imports: [RouterOutlet, SelfGrant],
  // The offer's code loads only where a global administrator meets a tenant without a role.
  template: `
    @if (session.oversight()) {
      <div class="offer">
        @defer (on immediate) {
          <app-self-grant />
        }
      </div>
    }
    <router-outlet />
  `,
  styles: `
    .offer {
      padding: 1.25rem 2rem 0;
    }
  `,
})
export class TenantScope {
  readonly tenant = input.required<string>();
  protected readonly session = inject(SessionService);

  constructor() {
    const session = this.session;
    const stream = inject(EventStreamService);
    effect(() => {
      const tenant = this.tenant();
      session.enter(tenant);
      stream.connect(session.oversight() ? null : tenant);
    });
    inject(DestroyRef).onDestroy(() => {
      session.enter(null);
      stream.connect(null);
    });
  }
}
