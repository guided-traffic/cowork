import { Activity, PullRequest, TicketState } from '../api/models';

/**
 * The names the UI gives the system actors whose acts a ticket shows (docs/adr/0026 D1): GitHub's
 * webhook acts as `system:github` (docs/adr/0071 D6). Any other is shown as it is recorded.
 */
const systemNames: Readonly<Record<string, string>> = { 'system:github': 'GitHub' };

/** Who made an act: the person, the system actor by its name, or cowork. */
export function actorName(activity: Pick<Activity, 'actor' | 'actor_system'>): string {
  if (activity.actor) {
    return activity.actor.display_name;
  }
  if (activity.actor_system) {
    return systemNames[activity.actor_system] ?? activity.actor_system;
  }
  return 'cowork';
}

/** A commit's id as people read it: its first seven characters. */
export function shortSha(sha: string): string {
  return sha.slice(0, 7);
}

/**
 * What an act on a pull request or a commit names, from its payload — `pull request #34`, `commit
 * 0d1a26e` — or nothing for any other act. The payload carries no title: the audit record keeps no
 * text from outside the tenant (docs/adr/0071 D6).
 */
export function codeName(
  activity: Pick<Activity, 'entity_type' | 'action' | 'before' | 'after'>,
): string | undefined {
  if (activity.entity_type !== 'pull_request' && activity.entity_type !== 'commit') {
    return undefined;
  }
  const payload = (activity.action === 'unlinked' ? activity.before : activity.after) as Record<
    string,
    unknown
  > | null;
  const sha = payload?.['sha'];
  if (typeof sha === 'string') {
    return `commit ${shortSha(sha)}`;
  }
  const number = payload?.['number'];
  return typeof number === 'number' ? `pull request #${number}` : 'a pull request';
}

/** What a pull request or a commit is called in a list: `#34`, or the commit's short id. */
export function pullRequestLabel(pr: Pick<PullRequest, 'kind' | 'number' | 'sha'>): string {
  if (pr.kind === 'commit' && pr.sha) {
    return shortSha(pr.sha);
  }
  return `#${pr.number ?? '?'}`;
}

/**
 * Where the key that linked it was read (docs/adr/0068 D1, D2, D5), as a reader names the place: a
 * trailer, a line of the body, the title of a pull request, the subject of a commit.
 */
export function foundIn(pr: Pick<PullRequest, 'kind' | 'found_in'>): string {
  switch (pr.found_in) {
    case 'trailer':
      return 'a Cowork-Ticket trailer';
    case 'body':
      return 'a line of its body';
    case 'subject':
      return pr.kind === 'commit' ? 'its subject' : 'its title';
  }
}

/**
 * Whether the ticket page says the work may be ready to move (docs/adr/0071 D6): a pull request
 * merged, or a commit on the default branch, names the ticket, and the ticket is still open. It is a
 * hint and nothing more — no state changes by itself; a person or a capable agent moves it.
 */
export function readyToMove(
  prs: readonly PullRequest[],
  state: TicketState,
): PullRequest | undefined {
  if (state === 'done' || state === 'dropped') {
    return undefined;
  }
  return prs.find((pr) => pr.state === 'merged');
}
