import { inject, Injectable, Injector } from '@angular/core';
import { Api } from '../api/api';
import { createTenant } from '../api/functions';
import { Tenant, TenantCreate } from '../api/models';
import { refresh } from './refresh';
import { SessionService } from './session.service';

/**
 * The installation's tenants as a collection: creating one, which a global administrator does in a
 * session (docs/adr/0005 D5). The settings of the tenant a page shows are `TenantService`'s.
 */
@Injectable({ providedIn: 'root' })
export class TenantsService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

  /**
   * Creates a tenant; its creator becomes its first administrator (docs/adr/0032 D7). The person's
   * memberships are loaded again, because the new one is what the tenant's pages and the tenant
   * switcher read.
   */
  async create(body: TenantCreate): Promise<Tenant> {
    const tenant = await this.api.invoke(createTenant, {
      'Idempotency-Key': crypto.randomUUID(),
      body,
    });
    refresh(this.session.me, this.injector);
    return tenant;
  }
}
