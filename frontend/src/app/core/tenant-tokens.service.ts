import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import { listTenantTokens, revokeTenantToken } from '../api/functions';
import { MemberTokenList } from '../api/models';
import { PerPage } from './table-pages';

/**
 * The tokens that can act in a tenant, for its administrators (docs/adr/0035 D5): every member's
 * unrestricted tokens and those restricted to the tenant, metadata only, read in numbered pages
 * (docs/adr/0048 D2), and their revocation — which, for an unrestricted token, ends it in every
 * tenant of its person. A token restricted to another tenant is never in the list.
 */
@Injectable({ providedIn: 'root' })
export class TenantTokensService {
  private readonly api = inject(Api);

  /** One numbered page, newest first. */
  page(tenant: string, page: number, perPage: PerPage): Promise<MemberTokenList> {
    return this.api.invoke(listTenantTokens, { tenant, page, per_page: perPage });
  }

  /** Revokes the token for good; it stays listed as revoked (docs/adr/0035 D6). */
  async revoke(tenant: string, tokenId: string): Promise<void> {
    await this.api.invoke(revokeTenantToken, { tenant, token_id: tokenId });
  }
}
