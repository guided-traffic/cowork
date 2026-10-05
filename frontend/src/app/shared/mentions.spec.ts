import { Member, Ticket } from '../api/models';
import { insertMention, matching, mentionCandidates, mentionQuery, mentionsIn } from './mentions';

const member = (
  id: string,
  name: string,
  role: Member['role'],
  username: string | null = null,
): Member => ({
  person: { id, display_name: name, username },
  role,
  origins: [{ source: 'grant', role }],
  local: username !== null,
  email: null,
});

describe('mentions (docs/adr/0015 D5)', () => {
  it('finds the @ being typed at the start of a word, up to the caret', () => {
    expect(mentionQuery('@', 1)).toEqual({ start: 0, query: '' });
    expect(mentionQuery('Look @sa', 8)).toEqual({ start: 5, query: 'sa' });
    expect(mentionQuery('Look @sa and more', 8)).toEqual({ start: 5, query: 'sa' });
    expect(mentionQuery('Look @sam rivera', 16)).toBeNull();
    expect(mentionQuery('mail@example.com', 16)).toBeNull();
    expect(mentionQuery('line\n@ad', 8)).toEqual({ start: 5, query: 'ad' });
    expect(mentionQuery('no mention', 10)).toBeNull();
  });

  it('matches by the start of a word of the name, or of the username without local:', () => {
    const people = [
      { id: 'p1', name: 'Ada Lovelace', username: 'local:ada' },
      { id: 'p2', name: 'Sam Rivera', username: null },
    ];
    expect(matching(people, '').map((p) => p.id)).toEqual(['p1', 'p2']);
    expect(matching(people, 'riv').map((p) => p.id)).toEqual(['p2']);
    expect(matching(people, 'ADA').map((p) => p.id)).toEqual(['p1']);
    expect(matching(people, 'love').map((p) => p.id)).toEqual(['p1']);
    expect(matching(people, 'vera')).toEqual([]);
  });

  it('writes @<name> and a space over the @ being typed, the caret after it', () => {
    expect(insertMention('Look @sa, ok', 5, 8, 'Sam Rivera')).toEqual({
      text: 'Look @Sam Rivera , ok',
      caret: 17,
    });
  });

  it('mentions the persons whose @<name> the text still holds, each once', () => {
    const sam = { id: 'p2', name: 'Sam Rivera' };
    const ada = { id: 'p1', name: 'Ada Lovelace' };
    expect(mentionsIn('@Sam Rivera and @Ada Lovelace', [sam, ada, sam])).toEqual(['p2', 'p1']);
    expect(mentionsIn('Sam Rivera, no @', [sam])).toEqual([]);
  });

  it('offers every member but the writer, and of a confidential ticket only those who see it', () => {
    const members = [
      member('p1', 'Ada', 'admin'),
      member('p2', 'Sam', 'member'),
      member('p3', 'Bob', 'viewer'),
      member('p4', 'Cyd', 'member'),
    ];
    const open = {
      confidential: false,
      assignee: null,
      reporter: { id: 'p4' },
    } as unknown as Ticket;
    expect(mentionCandidates(members, open, 'p2').map((p) => p.id)).toEqual(['p1', 'p3', 'p4']);
    const secret = {
      confidential: true,
      assignee: { id: 'p3' },
      reporter: { id: 'p4' },
    } as unknown as Ticket;
    expect(mentionCandidates(members, secret, 'p4').map((p) => p.id)).toEqual(['p1', 'p3']);
    expect(mentionCandidates(members, undefined, undefined).map((p) => p.name)).toEqual([
      'Ada',
      'Sam',
      'Bob',
      'Cyd',
    ]);
  });
});
