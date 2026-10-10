import { MembershipOrigin } from '../../api/models';
import { higher, originAccents, originMeanings, roleFrom, roleMeanings, roles } from './roles';

describe('roles', () => {
  it('are the three tenant roles, lowest first (docs/adr/0034 D1), each with what it may do', () => {
    expect(roles).toEqual(['viewer', 'member', 'admin']);
    expect(Object.keys(roleMeanings)).toEqual(roles);
  });
});

describe('higher', () => {
  it.each([
    ['viewer', 'member', 'member'],
    ['member', 'viewer', 'member'],
    ['admin', 'member', 'admin'],
    ['member', 'admin', 'admin'],
    ['member', 'member', 'member'],
  ] as const)(
    'of %s and %s is %s, as a mapped and a granted role combine (docs/adr/0030 D4)',
    (a, b, expected) => {
      expect(higher(a, b)).toBe(expected);
    },
  );

  it('is the one role there is when the other is none', () => {
    expect(higher(null, 'viewer')).toBe('viewer');
    expect(higher('admin', null)).toBe('admin');
  });

  it('is none when there is no role at all', () => {
    expect(higher(null, null)).toBeNull();
  });
});

describe('roleFrom', () => {
  const origins: MembershipOrigin[] = [
    { source: 'mapping', role: 'member' },
    { source: 'grant', role: 'admin' },
  ];

  it('is the role each source gives', () => {
    expect(roleFrom(origins, 'mapping')).toBe('member');
    expect(roleFrom(origins, 'grant')).toBe('admin');
  });

  it('is none where the membership has no such source', () => {
    expect(roleFrom([origins[0]], 'grant')).toBeNull();
    expect(roleFrom([], 'mapping')).toBeNull();
  });
});

describe('the origins of a membership', () => {
  it('say what each source is, and take their accent from a token of the preset', () => {
    for (const source of ['mapping', 'grant', 'local'] as const) {
      expect(originMeanings[source].length).toBeGreaterThan(0);
      expect(originAccents[source]).toMatch(/^var\(--p-[a-z-]+\)$/);
    }
  });

  it('say of a local account that the person signs in with a username and a password, wherever it was made', () => {
    expect(originMeanings.local).toContain('a username and a password of their own');
    expect(originMeanings.local).not.toContain('this team');
    expect(originMeanings.local).not.toContain('Accounts');
  });
});
