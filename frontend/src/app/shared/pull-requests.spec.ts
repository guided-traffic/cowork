import { Activity, PullRequest } from '../api/models';
import {
  actorName,
  codeName,
  foundIn,
  pullRequestLabel,
  readyToMove,
  shortSha,
} from './pull-requests';

const sha = '0d1a26e67d8f5eaf1f6ba5c57fc3c7d91ac0fd1c';

function pr(overrides: Partial<PullRequest> = {}): PullRequest {
  return {
    id: 'l1',
    kind: 'pull_request',
    repository: 'github.com/acme/app',
    number: 34,
    sha: null,
    title: 'fix: the gate (VKO-12)',
    state: 'open',
    url: 'https://github.com/acme/app/pull/34',
    author: 'octocat',
    merged_at: null,
    found_in: 'subject',
    first_seen_at: '2026-10-05T08:00:00Z',
    last_seen_at: '2026-10-05T08:00:00Z',
    ...overrides,
  };
}

function act(overrides: Partial<Activity> = {}): Activity {
  return {
    id: 'a1',
    at: '2026-10-06T09:00:00Z',
    actor: null,
    actor_system: 'system:github',
    agent: null,
    token: null,
    action: 'linked',
    entity_type: 'pull_request',
    entity_id: 'l1',
    before: null,
    after: { number: 34, repository: 'github.com/acme/app', state: 'open', found_in: 'subject' },
    reason: null,
    note: null,
    explained_by_comment: null,
    redacted: false,
    ...overrides,
  };
}

describe('the pull requests of a ticket (docs/adr/0071 D6)', () => {
  it('names the system actor of the webhook GitHub, a person by name, and anything else as recorded', () => {
    expect(actorName(act())).toBe('GitHub');
    expect(actorName(act({ actor_system: 'system:ticket-purge' }))).toBe('system:ticket-purge');
    expect(actorName(act({ actor: { id: 'p1', display_name: 'Ada' }, actor_system: null }))).toBe(
      'Ada',
    );
    expect(actorName(act({ actor: null, actor_system: null }))).toBe('cowork');
  });

  it('names what an act is on by its number or its short id, and nothing for another act', () => {
    expect(codeName(act())).toBe('pull request #34');
    expect(
      codeName(act({ entity_type: 'commit', after: { sha, repository: 'github.com/acme/app' } })),
    ).toBe('commit 0d1a26e');
    expect(
      codeName(act({ action: 'unlinked', before: { number: 9, repository: 'x' }, after: null })),
    ).toBe('pull request #9');
    expect(codeName(act({ entity_type: 'ticket' }))).toBeUndefined();
  });

  it('labels a pull request by its number and a commit by its short id', () => {
    expect(pullRequestLabel(pr())).toBe('#34');
    expect(pullRequestLabel(pr({ kind: 'commit', number: null, sha }))).toBe('0d1a26e');
    expect(shortSha(sha)).toBe('0d1a26e');
  });

  it('says where the key was read as a reader names the place', () => {
    expect(foundIn(pr({ found_in: 'trailer' }))).toBe('a Cowork-Ticket trailer');
    expect(foundIn(pr({ found_in: 'body' }))).toBe('a line of its body');
    expect(foundIn(pr({ found_in: 'subject' }))).toBe('its title');
    expect(foundIn(pr({ kind: 'commit', found_in: 'subject' }))).toBe('its subject');
  });

  it('hints that an open ticket may be ready to move once something merged, and never for a closed one', () => {
    const merged = pr({ state: 'merged', merged_at: '2026-10-06T09:00:00Z' });
    expect(readyToMove([pr(), merged], 'in-progress')).toBe(merged);
    expect(readyToMove([pr()], 'review')).toBeUndefined();
    expect(readyToMove([merged], 'done')).toBeUndefined();
    expect(readyToMove([merged], 'dropped')).toBeUndefined();
    expect(readyToMove([], 'filed')).toBeUndefined();
  });
});
