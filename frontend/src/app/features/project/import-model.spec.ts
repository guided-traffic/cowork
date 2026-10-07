import { ImportFile } from '../../api/models';
import {
  assigneeOf,
  blocking,
  blockLacks,
  changeable,
  correctable,
  correctionOf,
  correctionsOf,
  defaultBlockFrom,
  Draft,
  importing,
  linkText,
  placeOf,
  refusedFiles,
  shortKey,
  stateOf,
} from './import-model';

const ada = { id: 'p-ada', display_name: 'Ada Lovelace' };

/** A file of a dry run's report: a repository's open task, to create, unless a test says more. */
function reported(overrides: Partial<ImportFile> = {}): ImportFile {
  return {
    path: 'docs/tickets/001-the-first.md',
    outcome: 'create',
    reason: null,
    format: 'repository',
    number: 1,
    key: 'acme/VKO-1',
    conflict: null,
    title: 'the first',
    type: 'task',
    type_reason: 'detected: the title names no question, defect, decision or missing capability',
    state: 'filed',
    block_candidate: null,
    block: null,
    columns: null,
    assignee: null,
    parent: null,
    note: null,
    questions: [],
    links: [],
    attachments: [],
    confidential: false,
    confidential_reason: null,
    warnings: [],
    errors: [],
    correction: null,
    ...overrides,
  };
}

describe('the import model', () => {
  it('names a key without its tenant', () => {
    expect(shortKey('acme/VKO-12')).toBe('VKO-12');
  });

  it('corrects a ticket file only, and a conflict only by leaving it out', () => {
    for (const outcome of ['create', 'conflict', 'error'] as const) {
      expect(correctable(reported({ outcome }))).toBe(true);
    }
    for (const outcome of ['skip', 'exclude', 'created'] as const) {
      expect(correctable(reported({ outcome }))).toBe(false);
    }
    expect(changeable(reported({ outcome: 'create' }))).toBe(true);
    expect(changeable(reported({ outcome: 'error' }))).toBe(true);
    expect(changeable(reported({ outcome: 'conflict' }))).toBe(false);
  });

  it("reads the state and the assignee after the person's correction", () => {
    const file = reported({ assignee: { source: 'Ada <local:ada>', person: ada } });

    expect(stateOf(file, undefined)).toBe('filed');
    expect(stateOf(file, { state: 'review' })).toBe('review');
    expect(assigneeOf(file, undefined)).toBe('p-ada');
    expect(assigneeOf(file, { assignee: null })).toBeNull();
    expect(assigneeOf(file, { assignee: 'p-sam' })).toBe('p-sam');
  });

  it("takes the file's own state as the block's origin where a block comes from it", () => {
    expect(defaultBlockFrom(reported({ state: 'in-progress' }))).toBe('in-progress');
    expect(defaultBlockFrom(reported({ state: 'done' }))).toBeNull();
    expect(
      defaultBlockFrom(
        reported({
          state: 'blocked',
          block: { kind: 'human', reason: 'r', from: 'review', ticket: null },
        }),
      ),
    ).toBe('review');
  });

  describe('the correction of a file (ImportCorrection)', () => {
    it('is none without a draft, and none for a skipped file', () => {
      expect(correctionOf(reported(), undefined)).toBeNull();
      expect(correctionOf(reported({ outcome: 'skip' }), { exclude: true })).toBeNull();
    });

    it('names exclude alone for a file left out, whatever else the draft holds', () => {
      expect(correctionOf(reported(), { exclude: true, type: 'bug', assignee: null })).toEqual({
        path: 'docs/tickets/001-the-first.md',
        exclude: true,
      });
      expect(correctionOf(reported({ outcome: 'conflict' }), { exclude: true })).toEqual({
        path: 'docs/tickets/001-the-first.md',
        exclude: true,
      });
    });

    it('takes nothing but the exclusion for a conflict', () => {
      expect(correctionOf(reported({ outcome: 'conflict' }), { type: 'bug' })).toBeNull();
    });

    it('names only what the person changed from the report', () => {
      expect(correctionOf(reported(), { type: 'bug' })).toEqual({
        path: 'docs/tickets/001-the-first.md',
        type: 'bug',
      });
      expect(correctionOf(reported(), { state: 'done' })).toEqual({
        path: 'docs/tickets/001-the-first.md',
        state: 'done',
      });
      expect(correctionOf(reported(), { type: 'task', state: 'filed' })).toBeNull();
      expect(correctionOf(reported(), { exclude: false })).toBeNull();
    });

    it('sends the state blocked with its block, the reason trimmed, its origin where one is named', () => {
      expect(
        correctionOf(reported(), {
          state: 'blocked',
          block: { kind: 'human', reason: '  the owner decides  ', from: 'filed' },
        }),
      ).toEqual({
        path: 'docs/tickets/001-the-first.md',
        state: 'blocked',
        block: { kind: 'human', reason: 'the owner decides', from: 'filed' },
      });
      expect(
        correctionOf(reported(), {
          state: 'blocked',
          block: { kind: 'external', reason: 'the provider', from: null },
        }),
      ).toEqual({
        path: 'docs/tickets/001-the-first.md',
        state: 'blocked',
        block: { kind: 'external', reason: 'the provider' },
      });
    });

    it('sends a new block of a file blocked by its source, with the state blocked', () => {
      const file = reported({ outcome: 'error', state: 'blocked' });

      expect(
        correctionOf(file, { block: { kind: 'decision', reason: 'which way', from: 'decided' } }),
      ).toEqual({
        path: 'docs/tickets/001-the-first.md',
        state: 'blocked',
        block: { kind: 'decision', reason: 'which way', from: 'decided' },
      });
    });

    it('sends no block with another state, and none without its kind', () => {
      const block = { kind: 'human' as const, reason: 'r', from: null };
      expect(correctionOf(reported(), { state: 'review', block })).toEqual({
        path: 'docs/tickets/001-the-first.md',
        state: 'review',
      });
      expect(
        correctionOf(reported(), {
          state: 'blocked',
          block: { kind: null, reason: 'r', from: null },
        }),
      ).toEqual({ path: 'docs/tickets/001-the-first.md', state: 'blocked' });
    });

    it('sends the assignee where it changed, null for nobody', () => {
      const file = reported({ assignee: { source: 'Ada <local:ada>', person: ada } });

      expect(correctionOf(file, { assignee: null })).toEqual({
        path: 'docs/tickets/001-the-first.md',
        assignee: null,
      });
      expect(correctionOf(file, { assignee: 'p-sam' })).toEqual({
        path: 'docs/tickets/001-the-first.md',
        assignee: 'p-sam',
      });
      expect(correctionOf(file, { assignee: 'p-ada' })).toBeNull();
      expect(correctionOf(reported(), { assignee: null })).toBeNull();
    });

    it("lists the corrections in the report's order, each file once, none that changes nothing", () => {
      const first = reported();
      const second = reported({ path: 'docs/tickets/002-b.md', key: 'acme/VKO-2', number: 2 });
      const third = reported({ path: 'docs/tickets/003-c.md', key: 'acme/VKO-3', number: 3 });
      const drafts = new Map<string, Draft>([
        [third.path, { exclude: true }],
        [first.path, { type: 'bug' }],
        [second.path, { type: 'task' }],
      ]);

      expect(correctionsOf([first, second, third], drafts)).toEqual([
        { path: first.path, type: 'bug' },
        { path: third.path, exclude: true },
      ]);
    });
  });

  describe('what a block lacks', () => {
    it('is nothing where the state is not blocked, or the file is left out', () => {
      expect(blockLacks(reported(), undefined)).toBeNull();
      expect(blockLacks(reported(), { state: 'blocked', exclude: true })).toBeNull();
    });

    it('is nothing for a file that keeps the block its source gives it', () => {
      const file = reported({
        state: 'blocked',
        block: { kind: 'human', reason: 'r', from: 'filed', ticket: null },
      });

      expect(blockLacks(file, undefined)).toBeNull();
    });

    it('names the kind, the reason and the origin while they are missing', () => {
      expect(blockLacks(reported(), { state: 'blocked' })).toBe(
        'The state blocked needs its block: what it waits on and why.',
      );
      expect(
        blockLacks(reported(), {
          state: 'blocked',
          block: { kind: null, reason: '', from: 'filed' },
        }),
      ).toBe('The block needs its kind.');
      expect(
        blockLacks(reported(), {
          state: 'blocked',
          block: { kind: 'human', reason: '  ', from: 'filed' },
        }),
      ).toBe('The block needs its reason.');
      expect(
        blockLacks(reported({ state: 'done' }), {
          state: 'blocked',
          block: { kind: 'human', reason: 'r', from: null },
        }),
      ).toBe('The block needs the state it comes from.');
      expect(
        blockLacks(reported(), {
          state: 'blocked',
          block: { kind: 'human', reason: 'x'.repeat(2001), from: 'filed' },
        }),
      ).toBe("The block's reason is longer than 2000 characters.");
    });

    it('takes the file’s own state as the origin where a block comes from it', () => {
      expect(
        blockLacks(reported({ state: 'review' }), {
          state: 'blocked',
          block: { kind: 'release', reason: 'r', from: null },
        }),
      ).toBeNull();
    });

    it('names the block of a file whose source says blocked without one that holds', () => {
      expect(blockLacks(reported({ outcome: 'error', state: 'blocked' }), undefined)).toBe(
        'The state blocked needs its block: what it waits on and why.',
      );
    });
  });

  describe('what blocks the execution, and what it imports', () => {
    const create = reported();
    const conflict = reported({
      path: 'docs/tickets/002-b.md',
      outcome: 'conflict',
      conflict: 'acme/VKO-2',
    });
    const error = reported({
      path: 'docs/tickets/003-c.md',
      outcome: 'error',
      errors: [{ field: 'state', line: 4, message: 'no state' }],
    });
    const skip = reported({
      path: 'docs/tickets/README.md',
      outcome: 'skip',
      reason: 'no ticket file',
    });
    const files = [create, conflict, error, skip];

    it('blocks on a conflict and on an error the person neither left out nor corrected', () => {
      expect(blocking(files, new Map())).toEqual([conflict, error]);
    });

    it('lets a conflict and an error pass once they are left out', () => {
      const drafts = new Map<string, Draft>([
        [conflict.path, { exclude: true }],
        [error.path, { exclude: true }],
      ]);

      expect(blocking(files, drafts)).toEqual([]);
      expect(importing(files, drafts)).toEqual([create]);
    });

    it('leaves a corrected error to the execution, which reads it again', () => {
      const drafts = new Map<string, Draft>([
        [conflict.path, { exclude: true }],
        [error.path, { state: 'analysed' }],
      ]);

      expect(blocking(files, drafts)).toEqual([]);
      expect(importing(files, drafts)).toEqual([create, error]);
    });

    it('imports nothing that is left out', () => {
      expect(importing([create], new Map([[create.path, { exclude: true }]]))).toEqual([]);
    });
  });

  it('names where in its file a message points', () => {
    expect(placeOf({ field: 'severity', line: 5, message: 'm' })).toBe('line 5 · severity');
    expect(placeOf({ field: null, line: 2, message: 'm' })).toBe('line 2');
    expect(placeOf({ field: 'links.json', line: null, message: 'm' })).toBe('links.json');
    expect(placeOf({ field: null, line: null, message: 'm' })).toBe('');
  });

  it('says a link as the file’s ticket takes part in it', () => {
    expect(
      linkText({ type: 'blocks', direction: 'outgoing', key: 'acme/VKO-3', source: 'blocked-by' }),
    ).toBe('blocks VKO-3');
    expect(
      linkText({
        type: 'found-in',
        direction: 'incoming',
        key: 'acme/VKO-9',
        source: 'links.json',
      }),
    ).toBe('VKO-9 found-in this one');
  });

  it('finds the files an execution refusal names, by file:<path> and by the index of a correction', () => {
    const sent = [
      { path: 'docs/tickets/001-a.md', type: 'bug' as const },
      { path: 'docs/tickets/002-b.md', exclude: true },
    ];

    const files = refusedFiles(
      [
        { pointer: 'file:docs/tickets/003-c.md', message: 'conflict: VKO-3' },
        { pointer: 'file:docs/tickets/003-c.md', message: 'error: line 4' },
        { pointer: '/corrections/1/exclude', message: 'no other correction' },
        { pointer: '/corrections/7/path', message: 'out of range' },
        { pointer: '/corrections', message: 'too many' },
      ],
      sent,
    );

    expect([...files]).toEqual([
      ['docs/tickets/003-c.md', ['conflict: VKO-3', 'error: line 4']],
      ['docs/tickets/002-b.md', ['no other correction']],
    ]);
  });
});
