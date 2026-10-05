import { meanings } from './vocabulary';

/** The values of the API's vocabularies, as the API spells them (docs/adr/0009, 0010, 0008). */
const vocabulary = {
  state: ['filed', 'analysed', 'decided', 'in-progress', 'review', 'blocked', 'done', 'dropped'],
  severity: ['critical', 'high', 'medium', 'low', 'cosmetic'],
  security: ['live', 'boundary', 'hardening', 'none'],
  type: ['task', 'bug', 'feature', 'decision', 'question'],
  horizon: ['now', 'release', 'next', 'later', 'icebox'],
};

describe('meanings', () => {
  it('has one table for each vocabulary of the API and nothing else', () => {
    expect(Object.keys(meanings).sort()).toEqual(Object.keys(vocabulary).sort());
  });

  describe.each(Object.entries(vocabulary))('%s', (name, values) => {
    const table = meanings[name as keyof typeof meanings] as Record<string, string>;

    it('keys every value exactly as the API spells it (docs/adr/0055 D4)', () => {
      expect(Object.keys(table).sort()).toEqual([...values].sort());
    });

    it('explains every value with a text of its own', () => {
      const texts = Object.values(table);

      expect(texts.every((text) => text.trim().length > 0)).toBe(true);
      expect(new Set(texts).size).toBe(texts.length);
    });
  });

  it('explains review as the check of the work before it ends (docs/adr/0009 D1)', () => {
    expect(meanings.state.review).toBe(
      'The work is checked before it ends: the code read, the result tried',
    );
  });

  it('keeps the hyphen of in-progress instead of translating it', () => {
    expect(meanings.state['in-progress']).toBe('Someone is working on it');
    expect(Object.keys(meanings.state)).not.toContain('inProgress');
  });
});
