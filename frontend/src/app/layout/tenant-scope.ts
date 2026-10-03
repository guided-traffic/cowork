import { DestroyRef, Component, effect, inject, input } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { EventStreamService } from '../core/event-stream.service';
import { SessionService } from '../core/session.service';

/**
 * The pages under `/t/{slug}` (docs/adr/0023 D4): the slug becomes the session's tenant, which the
 * tenant-scoped services follow, and the tenant's event stream opens (docs/adr/0054 D1).
 */
@Component({
  selector: 'app-tenant-scope',
  imports: [RouterOutlet],
  template: '<router-outlet />',
})
export class TenantScope {
  readonly tenant = input.required<string>();

  constructor() {
    const session = inject(SessionService);
    const stream = inject(EventStreamService);
    effect(() => {
      const tenant = this.tenant();
      session.enter(tenant);
      stream.connect(tenant);
    });
    inject(DestroyRef).onDestroy(() => {
      session.enter(null);
      stream.connect(null);
    });
  }
}
