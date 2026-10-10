import { MembershipOrigin, MembershipSource, Role } from '../../api/models';

/** The tenant roles, lowest first (docs/adr/0034 D1). */
export const roles: Role[] = ['viewer', 'member', 'admin'];

/** What each role may do, in a line (docs/adr/0034 D1), shown beside the choice. */
export const roleMeanings: Record<Role, string> = {
  viewer: 'Reads what the team shares and watches tickets; changes nothing.',
  member: 'Does the daily work: files and moves tickets, comments, books time.',
  admin: 'Does what a member does, and administers the team: accounts, settings, projects.',
};

/** The higher of two roles, as a mapped and a granted role combine (docs/adr/0030 D4); null for none. */
export function higher(a: Role | null, b: Role | null): Role | null {
  if (!a || !b) {
    return a ?? b;
  }
  return roles.indexOf(a) >= roles.indexOf(b) ? a : b;
}

/** The role one source of a membership gives, or null where the membership has no such source. */
export function roleFrom(origins: MembershipOrigin[], source: MembershipSource): Role | null {
  return origins.find((origin) => origin.source === source)?.role ?? null;
}

/** What each source of a membership is, for the tooltip of its badge (docs/adr/0034 D7). */
export const originMeanings: Record<MembershipSource | 'local', string> = {
  mapping:
    "Derived from the identity provider's groups by a group mapping: it changes when the mapping or the person's groups change.",
  grant: 'Granted by an administrator of this team: it stays until an administrator removes it.',
  local:
    'Has a local account: signs in with a username and a password of their own, not through the identity provider.',
};

/** The accent of each source's badge, as tokens of the preset (docs/adr/0052 D5). */
export const originAccents: Record<MembershipSource | 'local', string> = {
  mapping: 'var(--p-state-analysed)',
  grant: 'var(--p-state-decided)',
  local: 'var(--p-text-muted-color)',
};

/** What a page says when a change would leave the tenant without an administrator (`last_admin`). */
export const lastAdminNotice =
  'Not changed: the team would be left without an administrator. Make somebody else an administrator first.';
