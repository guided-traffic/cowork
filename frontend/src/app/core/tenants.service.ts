import { inject, Injectable, Injector } from '@angular/core';
import { Api } from '../api/api';
import { createTenant } from '../api/fn/tenants/create-tenant';
import { Tenant, TenantCreate } from '../api/models';
import { refresh } from './refresh';
import { SessionService } from './session.service';

/**
 * The installation's tenants as a collection: creating one, which a global administrator does in a
 * session (docs/adr/0005 D5). Their list is `SessionService.installation`; the settings of the
 * tenant a page shows are `TenantService`'s.
 */
@Injectable({ providedIn: 'root' })
export class TenantsService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

  /**
   * Creates a tenant; its creator becomes its first administrator (docs/adr/0032 D7). The key is
   * the form's, one for each content it holds: a retry of the same slug and name sends the same
   * key, so an answer that was lost is answered again instead of being refused as taken
   * (docs/adr/0045). The person's memberships are loaded again, because the new one is what the
   * tenant's pages and the tenant switcher read, and the installation's tenants, which the tenant
   * joins (docs/adr/0034 D2).
   */
  async create(body: TenantCreate, idempotencyKey: string): Promise<Tenant> {
    const tenant = await this.api.invoke(createTenant, {
      'Idempotency-Key': idempotencyKey,
      body,
    });
    refresh(this.session.me, this.injector);
    refresh(this.session.installation, this.injector);
    return tenant;
  }
}
