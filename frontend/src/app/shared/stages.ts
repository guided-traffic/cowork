import { Ticket, TicketState } from '../api/models';

/**
 * The three progress stages of docs/adr/0017 D2, one per kind of work, in the order work passes
 * them: refinement, the work of `filed` and `analysed`; implementation, the work of `in-progress`
 * and the progress of the first release, which keeps that name; review, the work of `review`.
 */
export type Stage = 'refinement' | 'implementation' | 'review';

export const stages: readonly Stage[] = ['refinement', 'implementation', 'review'];

/** The field of the ticket, and of its patch, that holds each stage. */
export const stageFields = {
  refinement: 'progress_refinement',
  implementation: 'progress',
  review: 'progress_review',
} as const satisfies Record<Stage, keyof Ticket>;

export const stageNames: Record<Stage, string> = {
  refinement: 'Refinement',
  implementation: 'Implementation',
  review: 'Review',
};

export type Stages = Record<Stage, number>;

/** The three stages a ticket shows: its own, or a parent's from its children (docs/adr/0017 D3). */
export function stagesOf(
  ticket: Pick<Ticket, 'progress_refinement' | 'progress' | 'progress_review'>,
): Stages {
  return {
    refinement: ticket.progress_refinement,
    implementation: ticket.progress,
    review: ticket.progress_review,
  };
}

/** The stage a state works on; `decided` and `blocked` wait, and so do the closed states. */
export function stageOfState(state: TicketState): Stage | null {
  switch (state) {
    case 'filed':
    case 'analysed':
      return 'refinement';
    case 'in-progress':
      return 'implementation';
    case 'review':
      return 'review';
    default:
      return null;
  }
}

/**
 * The stage a ticket works on now, the one its bar shows on the board and in the backlog
 * (docs/adr/0018 D1): a blocked ticket's is the stage of the state it came from, `decided` has
 * none, and neither has a closed ticket.
 */
export function currentStage(ticket: Pick<Ticket, 'state' | 'block'>): Stage | null {
  if (ticket.state === 'blocked') {
    return ticket.block ? stageOfState(ticket.block.from) : null;
  }
  return stageOfState(ticket.state);
}

const full = (s: Stages) => s.refinement === 100 && s.implementation === 100 && s.review === 100;

/** What a write of the stages does to the ticket's state; see {@link effectOfStages}. */
export type StageEffect = 'keep' | 'complete' | 'reopen';

/**
 * What writing `after` as the stages of `ticket` does (docs/adr/0009 D5, docs/adr/0017 D4, D5),
 * as `EffectOfStages` in backend/internal/domain/progress.go decides it: bringing the last stage
 * of an open ticket to 100 is the done act, which needs the verification note; lowering a stage of
 * a ticket done by its stages reopens it, which needs a reason; anything else changes the stages
 * only. A parent's stages are its children's and take no write; a dropped ticket's take none
 * either; a ticket done by hand stays done whatever its stages do; and an open ticket whose stages
 * are all full already is closed by hand, since no write brings the last of them to 100.
 */
export function effectOfStages(
  ticket: Pick<
    Ticket,
    | 'state'
    | 'done_by_hand'
    | 'progress_derived'
    | 'progress_refinement'
    | 'progress'
    | 'progress_review'
  >,
  after: Stages,
): StageEffect {
  const before = stagesOf(ticket);
  if (ticket.progress_derived || ticket.state === 'dropped') {
    return 'keep';
  }
  if (ticket.state === 'done') {
    const lowered = stages.some((stage) => after[stage] < before[stage]);
    return !ticket.done_by_hand && lowered ? 'reopen' : 'keep';
  }
  return !full(before) && full(after) ? 'complete' : 'keep';
}

/** Whether withdrawing a done by hand leaves the ticket done, by its full stages (docs/adr/0009 D5). */
export function staysDoneByStages(
  ticket: Pick<Ticket, 'progress_derived' | 'progress_refinement' | 'progress' | 'progress_review'>,
): boolean {
  return !ticket.progress_derived && full(stagesOf(ticket));
}
