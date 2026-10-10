import { APIRequestContext, APIResponse, request } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import type {
  Account,
  Attachment,
  Comment,
  Effort,
  Horizon,
  Project,
  Question,
  SecurityClass,
  Severity,
  Ticket,
  TicketList,
  TicketState,
  TicketType,
  TokenCreated,
} from '../../src/app/api/models';

/**
 * The suite seeds its data through the API and asserts through the UI (docs/adr/0056 D7). Two
 * credentials: an administrator's token for what a token may do, and the administrator's session
 * for what only a session may do — creating a team, a token, a local account or a group mapping
 * (docs/adr/0031–0033, 0035). A session's writes carry what the frontend sends past the CSRF check:
 * the installation's origin and `X-Requested-With: cowork` (docs/adr/0037).
 */

/** The tenant every test works in, made by the global setup; each test owns a project in it. */
export const tenant = 'e2e';

/**
 * A second tenant of the administrator, made by the global setup, for what spans the person's
 * tenants; a test owns a project in it as it does in the fixture tenant.
 */
export const otherTenant = 'e2e-other';

/** A failed call names its operation, its status and the problem body. */
async function ok<T>(what: string, response: APIResponse): Promise<T> {
  if (!response.ok()) {
    throw new Error(`${what}: ${response.status()} ${await response.text()}`);
  }
  return response.status() === 204 ? (undefined as T) : ((await response.json()) as T);
}

/** A request context that holds a session cookie and writes as the browser does. */
export async function sessionContext(baseURL: string, storageState?: string) {
  return request.newContext({
    baseURL,
    ignoreHTTPSErrors: true,
    storageState,
    extraHTTPHeaders: { Origin: baseURL, 'X-Requested-With': 'cowork' },
  });
}

/** The local login through the API; the context keeps the cookie. */
export async function signIn(
  context: APIRequestContext,
  username: string,
  password: string,
): Promise<{ password_change_required: boolean }> {
  return ok(
    `sign in as ${username}`,
    await context.post('/auth/local', { data: { username, password } }),
  );
}

/**
 * Signs a browser context in as a local account with its temporary password and changes it, through
 * the API: the context's pages are then that person's (docs/adr/0033 D4). The context's own request
 * context shares its cookies, and every call carries what the browser would past the origin check.
 */
export async function signInWithNewPassword(
  context: APIRequestContext,
  baseURL: string,
  username: string,
  temporary: string,
  chosen: string,
): Promise<void> {
  const headers = { Origin: baseURL, 'X-Requested-With': 'cowork' };
  await ok(
    `sign in as ${username}`,
    await context.post('/auth/local', { data: { username, password: temporary }, headers }),
  );
  await ok(
    `change the temporary password of ${username}`,
    await context.put('/api/v1/me/password', {
      data: { current_password: temporary, new_password: chosen },
      headers,
    }),
  );
}

/** What only a session may do. */
export class Session {
  constructor(readonly context: APIRequestContext) {}

  /** The team, or nothing when it exists already — a stack of `make e2e-up` is used again. */
  async ensureTeam(slug: string, name: string): Promise<void> {
    const response = await this.context.post('/api/v1/teams', {
      data: { slug, name },
      headers: { 'Idempotency-Key': randomUUID() },
    });
    if (response.status() !== 409) {
      await ok('create the team', response);
    }
  }

  /** The group mapping, or nothing when the team maps the group already. */
  async ensureMapping(slug: string, group: string, role: 'viewer' | 'member' | 'admin') {
    const response = await this.context.post(`/api/v1/teams/${slug}/group-mappings`, {
      data: { group, role },
      headers: { 'Idempotency-Key': randomUUID() },
    });
    if (response.status() !== 409) {
      await ok('create the group mapping', response);
    }
  }

  async createToken(name: string): Promise<string> {
    const created = await ok<TokenCreated>(
      'create a token',
      await this.context.post('/api/v1/me/tokens', {
        data: { name, scope: 'admin', lifetime_days: 1 },
        headers: { 'Idempotency-Key': randomUUID() },
      }),
    );
    if (!created.token) {
      throw new Error('the token was created without its plaintext');
    }
    return created.token;
  }

  /**
   * Grants a person who exists a role in a team, by username or e-mail address — a manual grant,
   * which takes a session (docs/adr/0030 D3, docs/adr/0035 D5): how a local account of one team
   * joins a second.
   */
  async addMember(
    slug: string,
    person: string,
    role: 'viewer' | 'member' | 'admin' = 'member',
  ): Promise<void> {
    await ok(
      `grant ${person} the role ${role} in ${slug}`,
      await this.context.post(`/api/v1/teams/${slug}/members`, {
        data: { person, role },
        headers: { 'Idempotency-Key': randomUUID() },
      }),
    );
  }

  /** A local account of a tenant with a temporary password, which its person changes first. */
  async createAccount(
    username: string,
    temporaryPassword: string,
    role: 'viewer' | 'member' | 'admin' = 'member',
    slug = tenant,
  ): Promise<Account> {
    return ok(
      `create the account ${username}`,
      await this.context.post(`/api/v1/teams/${slug}/accounts`, {
        data: {
          username,
          display_name: `E2E ${username}`,
          temporary_password: temporaryPassword,
          role,
        },
        headers: { 'Idempotency-Key': randomUUID() },
      }),
    );
  }

  /**
   * Restricts a project to the tenant's administrators and its access list, over the version just
   * read (docs/adr/0034 D3, docs/adr/0050 D3).
   */
  async restrict(slug: string, project: string): Promise<void> {
    const path = `/api/v1/teams/${slug}/projects/${project}`;
    const etag = await etagOf(`read the project ${project}`, await this.context.get(path));
    await ok(
      `restrict the project ${project}`,
      await this.context.put(`${path}/restriction`, {
        data: { restricted: true },
        headers: { 'If-Match': etag },
      }),
    );
  }
}

/** The `ETag` of a read that must have answered. */
async function etagOf(what: string, response: APIResponse): Promise<string> {
  await ok(what, response);
  const etag = response.headers()['etag'];
  if (!etag) {
    throw new Error(`${what}: no ETag`);
  }
  return etag;
}

export interface Filing {
  title: string;
  body?: string;
  type?: TicketType;
  severity?: Severity;
  security?: SecurityClass;
  threat?: string;
  effort?: Effort;
  horizon?: Horizon;
  /** The person the ticket is assigned to, by id. */
  assignee?: string;
}

/** What a transition carries beside the two states: a reason, a block (docs/adr/0009 D2). */
export interface TransitionExtra {
  reason?: string;
  block?: { kind: 'decision' | 'human' | 'product' | 'release' | 'external' };
}

/** What an administrator's token seeds: projects and tickets in a tenant, the fixture tenant by default. */
export class Seed {
  constructor(
    readonly context: APIRequestContext,
    readonly slug = tenant,
  ) {}

  static async create(baseURL: string, token: string, slug = tenant): Promise<Seed> {
    const context = await request.newContext({
      baseURL,
      ignoreHTTPSErrors: true,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` },
    });
    return new Seed(context, slug);
  }

  private path(project: string, rest = ''): string {
    return `/api/v1/teams/${this.slug}/projects/${project}${rest}`;
  }

  async project(key: string, name = `Project ${key}`): Promise<Project> {
    return ok(
      `create the project ${key}`,
      await this.context.post(`/api/v1/teams/${this.slug}/projects`, {
        data: { key, name },
        headers: { 'Idempotency-Key': randomUUID() },
      }),
    );
  }

  /** The project, true when this call made it and false when the tenant has it already. */
  async ensureProject(key: string, name: string): Promise<boolean> {
    const response = await this.context.post(`/api/v1/teams/${this.slug}/projects`, {
      data: { key, name },
      headers: { 'Idempotency-Key': randomUUID() },
    });
    if (response.status() === 409) {
      return false;
    }
    await ok(`create the project ${key}`, response);
    return true;
  }

  /** A ticket at the end of its horizon, `later` unless it names another. */
  async file(project: string, filing: Filing): Promise<Ticket> {
    return ok(
      `file "${filing.title}"`,
      await this.context.post(this.path(project, '/tickets'), {
        data: { type: 'task', severity: 'medium', security: 'none', effort: 'M', ...filing },
        headers: { 'Idempotency-Key': randomUUID() },
      }),
    );
  }

  async transition(
    project: string,
    number: number,
    from: TicketState,
    to: TicketState,
    extra: TransitionExtra = {},
  ) {
    return ok<Ticket>(
      `move ${project}-${number} from ${from} to ${to}`,
      await this.context.post(this.path(project, `/tickets/${number}/transitions`), {
        data: { from, to, ...extra },
        headers: { 'Idempotency-Key': randomUUID() },
      }),
    );
  }

  async ticket(project: string, number: number): Promise<Ticket> {
    return ok(
      `read ${project}-${number}`,
      await this.context.get(this.path(project, `/tickets/${number}`)),
    );
  }

  /** The project's open tickets of a horizon, in the project's rank. */
  async horizon(project: string, horizon: Horizon): Promise<Ticket[]> {
    const list = await ok<TicketList>(
      `list ${project} in ${horizon}`,
      await this.context.get(this.path(project, '/tickets'), { params: { horizon } }),
    );
    return list.items;
  }

  /** Replaces the ticket's body over the version just read (docs/adr/0050 D3). */
  async replaceBody(project: string, number: number, body: string): Promise<Ticket> {
    const etag = await etagOf(
      `read ${project}-${number}`,
      await this.context.get(this.path(project, `/tickets/${number}`)),
    );
    return ok(
      `replace the body of ${project}-${number}`,
      await this.context.put(this.path(project, `/tickets/${number}/body`), {
        data: { body },
        headers: { 'If-Match': etag },
      }),
    );
  }

  /**
   * Sets the ticket's parent over the version just read (docs/adr/0050 D3): a canonical key of any
   * team the token's person reads (docs/adr/0008 D2), or null for none.
   */
  async setParent(project: string, number: number, parent: string | null): Promise<Ticket> {
    const etag = await etagOf(
      `read ${project}-${number}`,
      await this.context.get(this.path(project, `/tickets/${number}`)),
    );
    return ok(
      `set the parent of ${project}-${number}`,
      await this.context.patch(this.path(project, `/tickets/${number}`), {
        data: { parent },
        headers: { 'If-Match': etag },
      }),
    );
  }

  /** Makes the ticket confidential over the version just read (docs/adr/0065). */
  async confidential(project: string, number: number): Promise<Ticket> {
    const etag = await etagOf(
      `read ${project}-${number}`,
      await this.context.get(this.path(project, `/tickets/${number}`)),
    );
    return ok(
      `make ${project}-${number} confidential`,
      await this.context.put(this.path(project, `/tickets/${number}/confidential`), {
        data: { confidential: true },
        headers: { 'If-Match': etag },
      }),
    );
  }

  /** Uploads a file to the ticket (docs/adr/0016). */
  async attach(
    project: string,
    number: number,
    file: { name: string; mimeType: string; buffer: Buffer },
  ): Promise<Attachment> {
    return ok(
      `attach ${file.name} to ${project}-${number}`,
      await this.context.post(this.path(project, `/tickets/${number}/attachments`), {
        multipart: { file },
        headers: { 'Idempotency-Key': randomUUID() },
      }),
    );
  }

  /** A comment on the ticket. */
  async comment(project: string, number: number, body: string): Promise<Comment> {
    return ok(
      `comment on ${project}-${number}`,
      await this.context.post(this.path(project, `/tickets/${number}/comments`), {
        data: { body },
        headers: { 'Idempotency-Key': randomUUID() },
      }),
    );
  }

  /** A question open in the tenant, asked as the token's person (docs/adr/0011 D2). */
  async ask(project: string, number: number, question: string): Promise<Question> {
    return ok(
      `ask on ${project}-${number}`,
      await this.context.post(this.path(project, `/tickets/${number}/questions`), {
        data: { question },
        headers: { 'Idempotency-Key': randomUUID() },
      }),
    );
  }

  /** Deletes the ticket into the bin (docs/adr/0024 D1). */
  async remove(project: string, number: number): Promise<void> {
    await ok(
      `delete ${project}-${number}`,
      await this.context.delete(this.path(project, `/tickets/${number}`)),
    );
  }

  async dispose(): Promise<void> {
    await this.context.dispose();
  }
}

/** A project key of its own for every test, so that tests run in parallel without a reset (D7). */
export function uniqueKey(prefix = 'E'): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
  let key = prefix;
  while (key.length < 8) {
    key += alphabet[Math.floor(Math.random() * alphabet.length)];
  }
  return key;
}

/** The canonical key of a ticket of the fixture tenant, as the page's test ids carry it. */
export function keyOf(project: string, number: number): string {
  return `${tenant}/${project}-${number}`;
}
