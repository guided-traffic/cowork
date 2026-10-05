import { APIRequestContext, APIResponse, request } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import type {
  Account,
  Effort,
  Project,
  SecurityClass,
  Severity,
  Ticket,
  TicketList,
  TicketState,
  TicketType,
  TokenCreated,
  Urgency,
} from '../../src/app/api/models';

/**
 * The suite seeds its data through the API and asserts through the UI (docs/adr/0056 D7). Two
 * credentials: an administrator's token for what a token may do, and the administrator's session
 * for what only a session may do — creating a tenant, a token, a local account or a group mapping
 * (docs/adr/0031–0033, 0035). A session's writes carry what the frontend sends past the CSRF check:
 * the installation's origin and `X-Requested-With: cowork` (docs/adr/0037).
 */

/** The tenant every test works in, made by the global setup; each test owns a project in it. */
export const tenant = 'e2e';

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

  /** The tenant, or nothing when it exists already — a stack of `make e2e-up` is used again. */
  async ensureTenant(slug: string, name: string): Promise<void> {
    const response = await this.context.post('/api/v1/tenants', {
      data: { slug, name },
      headers: { 'Idempotency-Key': randomUUID() },
    });
    if (response.status() !== 409) {
      await ok('create the tenant', response);
    }
  }

  /** The group mapping, or nothing when the tenant maps the group already. */
  async ensureMapping(slug: string, group: string, role: 'viewer' | 'member' | 'admin') {
    const response = await this.context.post(`/api/v1/tenants/${slug}/group-mappings`, {
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

  /** A local account of the tenant with a temporary password, which its person changes first. */
  async createAccount(
    username: string,
    temporaryPassword: string,
    role: 'viewer' | 'member' | 'admin' = 'member',
  ): Promise<Account> {
    return ok(
      `create the account ${username}`,
      await this.context.post(`/api/v1/tenants/${tenant}/accounts`, {
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
}

export interface Filing {
  title: string;
  type?: TicketType;
  severity?: Severity;
  security?: SecurityClass;
  threat?: string;
  effort?: Effort;
  urgency?: Urgency;
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
    return `/api/v1/tenants/${this.slug}/projects/${project}${rest}`;
  }

  async project(key: string, name = `Project ${key}`): Promise<Project> {
    return ok(
      `create the project ${key}`,
      await this.context.post(`/api/v1/tenants/${this.slug}/projects`, {
        data: { key, name },
        headers: { 'Idempotency-Key': randomUUID() },
      }),
    );
  }

  /** The project, true when this call made it and false when the tenant has it already. */
  async ensureProject(key: string, name: string): Promise<boolean> {
    const response = await this.context.post(`/api/v1/tenants/${this.slug}/projects`, {
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
  async horizon(project: string, urgency: Urgency): Promise<Ticket[]> {
    const list = await ok<TicketList>(
      `list ${project} in ${urgency}`,
      await this.context.get(this.path(project, '/tickets'), { params: { urgency } }),
    );
    return list.items;
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
