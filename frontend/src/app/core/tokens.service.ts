import { computed, inject, Injectable, Injector, resource } from '@angular/core';
import { Api } from '../api/api';
import { createMyToken, listMyTokens, listProjects, revokeMyToken } from '../api/functions';
import { Project, Token, TokenCreate, TokenCreated } from '../api/models';
import { refresh } from './refresh';

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
 * The person's own personal access tokens (docs/adr/0035): every page of the list, the creation
 * that hands the plaintext to its caller once, and the revocation. The routes name no tenant, so
 * unlike the tenant-scoped services this one does not follow the session. Nothing here keeps a
 * plaintext: `create` returns it and forgets it, and no list carries one.
 */
@Injectable({ providedIn: 'root' })
export class TokensService {
  private readonly api = inject(Api);
  private readonly injector = inject(Injector);

  readonly tokens = resource({
    loader: () => everyPage((cursor) => this.api.invoke(listMyTokens, { cursor, limit: 200 })),
  });

  readonly list = computed<Token[]>(() => (this.tokens.hasValue() ? this.tokens.value() : []));

  /**
   * The project restrictions of the listed tokens as one string, `tenant/id` each, or undefined
   * when there is none. A resource loads again whenever its params function runs, so this is a
   * computed: a list that reloads with the same restrictions does not load the keys again.
   */
  private readonly restrictions = computed(() => {
    const each = this.list()
      .filter((token) => token.restricted_tenant && token.restricted_project_id)
      .map((token) => `${token.restricted_tenant}/${token.restricted_project_id}`);
    return each.length > 0 ? [...new Set(each)].sort().join(' ') : undefined;
  });

  /**
   * The key of each project a listed token is restricted to, by project id: the API names a
   * token's project by its id, the person knows it by its key. A tenant whose projects cannot be
   * read leaves its ids out, and the page shows the id instead.
   */
  readonly projectKeys = resource({
    params: () => this.restrictions(),
    loader: async ({ params }) => {
      const tenants = new Set(params.split(' ').map((restriction) => restriction.split('/')[0]));
      const keys = new Map<string, string>();
      for (const tenant of tenants) {
        try {
          for (const project of await this.projectsOf(tenant, true)) {
            keys.set(project.id, project.key);
          }
        } catch {
          // The id is shown for these; a lookup that failed is no reason to hide the list.
        }
      }
      return keys;
    },
  });

  /** The key of a project a token is restricted to, once the lookup has found it. */
  keyOfProject(id: string): string | undefined {
    return this.projectKeys.hasValue() ? this.projectKeys.value().get(id) : undefined;
  }

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
  projectsOf(tenant: string, includeArchived = false): Promise<Project[]> {
    return everyPage((cursor) =>
      this.api.invoke(listProjects, {
        tenant,
        cursor,
        limit: 200,
        ...(includeArchived ? { include_archived: true } : {}),
      }),
    );
  }
}
