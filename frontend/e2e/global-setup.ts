import { FullConfig } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { otherTenant, Session, sessionContext, signIn, tenant } from './support/api';
import { adminPassword, adminState, adminUser } from './support/identities';
import { seedVisualBoard } from './support/visual';

/**
 * Once per run: the local administrator of the configuration (docs/adr/0032 D1) signs in through
 * the API, makes the fixture tenant — and with it the installation's first, so that the identity
 * provider's people may sign in — maps the Dex group `team-red` to `member` in it, as `make dev`
 * does in its tenant, makes a second tenant of the administrator's for what spans the person's
 * tenants, a token the workers seed with, and the board the screenshots compare, in a tenant of its
 * own. The administrator's session is kept for the tests that start signed in; the login's own
 * paths sign in through the page.
 */
export default async function globalSetup(config: FullConfig): Promise<void> {
  const baseURL = config.projects[0].use.baseURL;
  if (!baseURL) {
    throw new Error('no baseURL: set COWORK_BASE_URL');
  }
  const context = await sessionContext(baseURL);
  try {
    await signIn(context, adminUser, adminPassword);
    const admin = new Session(context);
    await admin.ensureTenant(tenant, 'End to end');
    await admin.ensureMapping(tenant, 'team-red', 'member');
    await admin.ensureTenant(otherTenant, 'Other end');
    const token = await admin.createToken('e2e-seed');
    process.env['COWORK_E2E_TOKEN'] = token;
    await seedVisualBoard(admin, baseURL, token);
    mkdirSync(dirname(adminState), { recursive: true });
    await context.storageState({ path: adminState });
  } finally {
    await context.dispose();
  }
}
