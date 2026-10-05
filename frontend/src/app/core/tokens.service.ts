import { inject, Injectable, Injector } from '@angular/core';
import { Api } from '../api/api';
import { createMyToken } from '../api/fn/me/create-my-token';
import { listMyTokens } from '../api/fn/me/list-my-tokens';
import { revokeMyToken } from '../api/fn/me/revoke-my-token';
import { listProjects } from '../api/fn/projects/list-projects';
import { Project, Token, TokenCreate, TokenCreated } from '../api/models';
import { refresh } from './refresh';
import { tablePages } from './table-pages';

/** Every page of a list, 200 at a time, in the order of the pages. */
async function everyPage<T>(
  page: (cursor: string | undefined) => Promise<{ items: T[]; next_cursor: string | null }>,
): Promise<T[]> {
  const items: T[] = [];
  let cursor: string | undefined;
  do {
    const next = await page(cursor);
    items.push(...next.items);
    cursor = next.next_cursor ?? undefined;
  } while (cursor);
  return items;
}

/**
 * The person's own personal access tokens (docs/adr/0035): the list in numbered pages of 25, 50 or
 * 100 with a total (docs/adr/0048 D2, D4), the creation
 * that hands the plaintext to its caller once, and the revocation. The routes name no tenant, so
 * unlike the tenant-scoped services this one does not follow the session. A token names the
 * project it is restricted to by its key (`restricted_project`), so nothing here looks projects up
 * for the list. Nothing here keeps a plaintext: `create` returns it and forgets it, and no list
 * carries one.
 */
@Injectable({ providedIn: 'root' })
export class TokensService {
  private readonly api = inject(Api);
  private readonly injector = inject(Injector);

  /** The page of the list shown, its size and the total; another size starts at the first page. */
  readonly table = tablePages(
    () => 'mine',
    (_mine, page, perPage) => this.api.invoke(listMyTokens, { page, per_page: perPage }),
  );
  readonly tokens = this.table.rows;
  readonly list = this.table.items;

  /**
   * Creates a token (docs/adr/0035 D5). The answer carries the plaintext once; the caller shows it
   * and drops it. The key is new for every act, so a retry by the person is a new token.
   */
  async create(body: TokenCreate): Promise<TokenCreated> {
    const created = await this.api.invoke(createMyToken, {
      'Idempotency-Key': crypto.randomUUID(),
      body,
    });
    refresh(this.tokens, this.injector);
    return created;
  }

  /** Revokes a token for good: it answers 401 from now on and stays listed (docs/adr/0035 D6). */
  async revoke(token: Token): Promise<void> {
    await this.api.invoke(revokeMyToken, { token_id: token.id });
    refresh(this.tokens, this.injector);
  }

  /** The projects of one tenant the person belongs to, for the restriction of a new token. */
  projectsOf(tenant: string): Promise<Project[]> {
    return everyPage((cursor) => this.api.invoke(listProjects, { tenant, cursor, limit: 200 }));
  }
}
