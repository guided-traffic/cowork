import { inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import { listTeamTokens } from '../api/fn/teams/list-team-tokens';
import { revokeTeamToken } from '../api/fn/teams/revoke-team-token';
import { MemberTokenList } from '../api/models';
import { ConditionalPages } from './conditional';
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
  /** The weak `ETag` of the page held: a load again that finds it unchanged is a `304` (docs/adr/0054 D7). */
  private readonly pages = new ConditionalPages(this.api);

  /** One numbered page, newest first. */
  page(tenant: string, page: number, perPage: PerPage): Promise<MemberTokenList> {
    return this.pages.load((fetch) =>
      fetch(listTeamTokens, { team: tenant, page, per_page: perPage }),
    );
  }

  /** Revokes the token for good; it stays listed as revoked (docs/adr/0035 D6). */
  async revoke(tenant: string, tokenId: string): Promise<void> {
    await this.api.invoke(revokeTeamToken, { team: tenant, token_id: tokenId });
  }
}
