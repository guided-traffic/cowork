import { join } from 'node:path';

/**
 * The identities of the suite (docs/adr/0056 D2, D3). Every value is development-only and public
 * (docs/developer/development-credentials.md): the stack of hack/e2e.sh lives for one run on the
 * loopback address.
 */

/** The stand-in's origin, `COWORK_BASE_URL` of the backend; hack/e2e.sh sets it. */
export const baseURL = process.env['COWORK_BASE_URL'] ?? 'https://localhost:18443';

/** The configuration's local administrator: `COWORK_LOCAL_ADMIN_*` of hack/e2e.sh. */
export const adminUser = process.env['COWORK_E2E_ADMIN'] ?? 'e2e-admin';
export const adminPassword = process.env['COWORK_E2E_ADMIN_PASSWORD'] ?? 'e2e-only-cowork';

/** The administrator's session, written by the global setup for the tests that start signed in. */
export const adminState = join(__dirname, '..', '.auth', 'admin.json');

/** The token the global setup made for seeding; workers inherit the environment. */
export function seedToken(): string {
  const token = process.env['COWORK_E2E_TOKEN'];
  if (!token) {
    throw new Error('COWORK_E2E_TOKEN is unset: the global setup did not run');
  }
  return token;
}

/**
 * A person of the identity provider: Dex's `bob` of hack/dex/config.yaml, in `cowork-users` (behind
 * the gate) and `team-red`, which the global setup maps to `member` in the fixture tenant.
 */
export const dexMember = { email: 'bob@example.com', password: 'dev-only-dex', name: 'bob' };

/** A password a test gives a local account; long enough for COWORK_PASSWORD_MIN_LENGTH's 12. */
export function freshPassword(): string {
  return `e2e-only-${Math.random().toString(36).slice(2, 12)}`;
}
