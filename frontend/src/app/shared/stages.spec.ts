import { Block, Ticket, TicketState } from '../api/models';
import {
  currentStage,
  effectOfStages,
  Stages,
  stageFields,
  stageNames,
  stageOfState,
  stages,
  stagesOf,
  staysDoneByStages,
} from './stages';

type StageTicket = Parameters<typeof effectOfStages>[0];

function ticket(fields: Partial<Ticket> = {}): StageTicket {
  return {
    state: 'in-progress',
    done_by_hand: false,
    progress_derived: false,
    progress_refinement: 100,
    progress: 50,
    progress_review: 0,
    ...fields,
  };
}

const at = (refinement: number, implementation: number, review: number): Stages => ({
  refinement,
  implementation,
  review,
});

describe('the stages', () => {
  it('are refinement, implementation and review, in the order work passes them (docs/adr/0017 D2)', () => {
    expect(stages).toEqual(['refinement', 'implementation', 'review']);
    expect(stages.map((stage) => stageNames[stage])).toEqual([
      'Refinement',
      'Implementation',
      'Review',
    ]);
  });

  it('live in progress_refinement, progress and progress_review: implementation keeps the old name', () => {
    expect(stageFields).toEqual({
      refinement: 'progress_refinement',
      implementation: 'progress',
      review: 'progress_review',
    });
  });

  it('are read off a ticket by those fields', () => {
    expect(stagesOf({ progress_refinement: 10, progress: 20, progress_review: 30 })).toEqual(
      at(10, 20, 30),
    );
  });
});

describe('stageOfState', () => {
  it.each([
    ['filed', 'refinement'],
    ['analysed', 'refinement'],
    ['decided', null],
    ['in-progress', 'implementation'],
    ['review', 'review'],
    ['blocked', null],
    ['done', null],
    ['dropped', null],
  ] as [TicketState, string | null][])('gives %s the stage %s', (state, stage) => {
    expect(stageOfState(state)).toBe(stage);
  });
});

describe('currentStage', () => {
  const blocked = (from: TicketState) =>
    ({ state: 'blocked', block: { kind: 'human', from, reason: 'Waiting' } as Block }) as const;

  it('is the stage of the state, as the board shows it (docs/adr/0018 D1)', () => {
    expect(currentStage({ state: 'analysed', block: null })).toBe('refinement');
    expect(currentStage({ state: 'in-progress', block: null })).toBe('implementation');
    expect(currentStage({ state: 'review', block: null })).toBe('review');
    expect(currentStage({ state: 'decided', block: null })).toBeNull();
  });

  it.each([
    ['filed', 'refinement'],
    ['decided', null],
    ['in-progress', 'implementation'],
    ['review', 'review'],
  ] as [TicketState, string | null][])(
    'is, for a ticket blocked from %s, the stage of that state: %s',
    (from, stage) => {
      expect(currentStage(blocked(from))).toBe(stage);
    },
  );

  it('is none for a blocked ticket that does not say where it came from', () => {
    expect(currentStage({ state: 'blocked', block: null })).toBeNull();
  });
});

describe('effectOfStages (backend/internal/domain/progress.go EffectOfStages)', () => {
  it('completes an open ticket when the write brings the last stage to 100 (docs/adr/0009 D5)', () => {
    for (const state of [
      'filed',
      'analysed',
      'decided',
      'in-progress',
      'review',
      'blocked',
    ] as const) {
      expect(
        effectOfStages(ticket({ state, progress: 100, progress_review: 90 }), at(100, 100, 100)),
      ).toBe('complete');
    }
  });

  it('completes whichever stage is the last one to fill', () => {
    const almost = ticket({ progress_refinement: 75, progress: 100, progress_review: 100 });

    expect(effectOfStages(almost, at(100, 100, 100))).toBe('complete');
  });

  it('keeps the state of an open ticket while a stage is short of 100', () => {
    expect(effectOfStages(ticket(), at(100, 100, 95))).toBe('keep');
    expect(effectOfStages(ticket(), at(100, 75, 0))).toBe('keep');
  });

  it('closes no open ticket whose stages were all full already: it is closed by hand', () => {
    const full = ticket({ progress: 100, progress_review: 100 });

    expect(effectOfStages(full, at(100, 100, 100))).toBe('keep');
  });

  it('reopens a ticket done by its stages when a stage goes down', () => {
    const done = ticket({ state: 'done', progress: 100, progress_review: 100 });

    expect(effectOfStages(done, at(100, 100, 75))).toBe('reopen');
    expect(effectOfStages(done, at(0, 100, 100))).toBe('reopen');
  });

  it('keeps a ticket done by hand done, whatever its stages do', () => {
    const byHand = ticket({ state: 'done', done_by_hand: true, progress: 60, progress_review: 0 });

    expect(effectOfStages(byHand, at(100, 25, 0))).toBe('keep');
    expect(effectOfStages(byHand, at(100, 100, 100))).toBe('keep');
  });

  it('keeps a dropped ticket and a parent, whose stages take no write', () => {
    expect(effectOfStages(ticket({ state: 'dropped' }), at(100, 100, 100))).toBe('keep');
    expect(effectOfStages(ticket({ progress_derived: true }), at(100, 100, 100))).toBe('keep');
    expect(
      effectOfStages(
        ticket({ state: 'done', progress_derived: true, progress: 100, progress_review: 100 }),
        at(0, 0, 0),
      ),
    ).toBe('keep');
  });
});

describe('staysDoneByStages', () => {
  it('holds for a ticket without children whose three stages are full', () => {
    expect(staysDoneByStages(ticket({ progress: 100, progress_review: 100 }))).toBe(true);
  });

  it('does not hold while a stage is short of full, nor for a parent', () => {
    expect(staysDoneByStages(ticket({ progress: 100, progress_review: 95 }))).toBe(false);
    expect(
      staysDoneByStages(ticket({ progress: 100, progress_review: 100, progress_derived: true })),
    ).toBe(false);
  });
});
