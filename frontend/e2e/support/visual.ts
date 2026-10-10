import { Seed, Session } from './api';

/**
 * The page the dark-mode screenshot compares (docs/adr/0056 D3): a tenant of its own with one
 * project whose board has a card in every column, so that the shell's navigation lists the same
 * one project in every run and every browser. The global setup makes it before any test runs; a
 * stack used again keeps the board it has.
 */
export const visualTenant = 'e2e-visual';
export const visualProject = 'VIEW';

export async function seedVisualBoard(
  admin: Session,
  baseURL: string,
  token: string,
): Promise<void> {
  await admin.ensureTeam(visualTenant, 'Visual check');
  const seed = await Seed.create(baseURL, token, visualTenant);
  try {
    if (!(await seed.ensureProject(visualProject, 'The board in the dark'))) {
      return;
    }
    const p = visualProject;
    await seed.file(p, { title: 'Sketch the import report', horizon: 'now', effort: 'S' });
    const analysed = await seed.file(p, {
      title: 'Decide the archive format',
      horizon: 'now',
      effort: 'L',
      type: 'decision',
    });
    await seed.transition(p, analysed.number, 'filed', 'analysed');
    const decided = await seed.file(p, {
      title: 'Write the migration',
      horizon: 'now',
      severity: 'high',
    });
    await forward(seed, decided.number, 'decided');
    const release = await seed.file(p, {
      title: 'Ship the release notes',
      horizon: 'release',
      type: 'feature',
      effort: 'XS',
    });
    await forward(seed, release.number, 'decided');
    const working = await seed.file(p, {
      title: 'Draw the dark theme',
      horizon: 'now',
      type: 'feature',
    });
    await forward(seed, working.number, 'in-progress');
    const blocked = await seed.file(p, {
      title: 'Translate the copy',
      horizon: 'now',
      severity: 'low',
    });
    await forward(seed, blocked.number, 'in-progress');
    await seed.transition(p, blocked.number, 'in-progress', 'blocked', {
      block: { kind: 'human' },
      reason: 'Waiting for the reviewed wording',
    });
    const review = await seed.file(p, {
      title: 'Check the export of confidential tickets',
      horizon: 'now',
      type: 'bug',
      security: 'hardening',
      threat: 'A member reads a confidential ticket in an export',
    });
    await forward(seed, review.number, 'review');
    await seed.file(p, { title: 'Plan the search', horizon: 'next', effort: 'L' });
    await seed.file(p, {
      title: 'Measure the stream',
      horizon: 'next',
      type: 'question',
      effort: 'XS',
    });
  } finally {
    await seed.dispose();
  }
}

const steps = ['filed', 'analysed', 'decided', 'in-progress', 'review'] as const;

/** The forward steps from `filed` up to `to`, one transition each. */
async function forward(seed: Seed, number: number, to: (typeof steps)[number]): Promise<void> {
  for (let at = 1; at <= steps.indexOf(to); at++) {
    await seed.transition(visualProject, number, steps[at - 1], steps[at]);
  }
}
