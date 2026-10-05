import { Member, Ticket } from '../api/models';

/** A person a comment can mention: the id the API takes, the name the text shows. */
export interface Mentionable {
  id: string;
  name: string;
  /** The username, which the list matches as well; none for a person of the identity provider. */
  username?: string | null;
}

/** The `@` being typed before the caret, and the text after it; null when the caret is in none. */
export interface MentionQuery {
  /** Where the `@` stands. */
  start: number;
  /** What follows it up to the caret. */
  query: string;
}

/**
 * The mention being typed: an `@` at the start of the text or after white space, followed up to the
 * caret by no white space and no other `@`. `mail@example.com` is no mention.
 */
export function mentionQuery(text: string, caret: number): MentionQuery | null {
  const before = text.slice(0, caret);
  const match = /(^|\s)@([^\s@]{0,64})$/.exec(before);
  if (!match) {
    return null;
  }
  return { start: before.length - match[2].length - 1, query: match[2] };
}

/** The candidates a query names: by the start of a word of the name, or of the username. */
export function matching(candidates: Mentionable[], query: string): Mentionable[] {
  const q = query.toLowerCase();
  return candidates.filter(
    (each) =>
      each.name
        .toLowerCase()
        .split(/\s+/)
        .some((word) => word.startsWith(q)) ||
      (each.username ?? '')
        .toLowerCase()
        .replace(/^local:/, '')
        .startsWith(q),
  );
}

/** The text with the `@query` from `start` to `caret` replaced by `@<name> `, and the caret after it. */
export function insertMention(
  text: string,
  start: number,
  caret: number,
  name: string,
): { text: string; caret: number } {
  const inserted = `@${name} `;
  return {
    text: text.slice(0, start) + inserted + text.slice(caret),
    caret: start + inserted.length,
  };
}

/** The ids of the persons whose `@<name>` the text still holds, each once, in the order given. */
export function mentionsIn(text: string, persons: Mentionable[]): string[] {
  const out: string[] = [];
  for (const person of persons) {
    if (text.includes(`@${person.name}`) && !out.includes(person.id)) {
      out.push(person.id);
    }
  }
  return out;
}

/**
 * The members a comment on the ticket can mention (docs/adr/0015 D5): a member who sees the ticket,
 * never the writer. Of a confidential ticket only the tenant's administrators, its assignee and its
 * reporter see it (docs/adr/0065); of a restricted project the page does not know the access list —
 * a member who is no administrator cannot read it — so it offers every member and the server refuses
 * one who cannot see the ticket.
 */
export function mentionCandidates(
  members: Member[],
  ticket: Ticket | undefined,
  me: string | undefined,
): Mentionable[] {
  return members
    .filter((member) => member.person.id !== me)
    .filter(
      (member) =>
        !ticket?.confidential ||
        member.role === 'admin' ||
        member.person.id === ticket.assignee?.id ||
        member.person.id === ticket.reporter.id,
    )
    .map((member) => ({
      id: member.person.id,
      name: member.person.display_name,
      username: member.person.username ?? null,
    }));
}
