import { SecurityClass, Severity, TicketState, TicketType, Urgency } from '../api/models';

/**
 * What each vocabulary value means, for the tooltip beside it: values are shown as the API spells
 * them and explained, never translated (docs/adr/0055 D4).
 */
export const meanings = {
  state: {
    filed: 'Recorded, not analysed yet',
    analysed: 'Understood: cause and scope are known',
    decided: 'The way forward is decided',
    'in-progress': 'Someone is working on it',
    blocked: 'Waits on something named in its block reason',
    done: 'Finished, with a verification note',
    dropped: 'Closed without doing it, with a reason',
  } satisfies Record<TicketState, string>,
  severity: {
    critical: 'Impact if never fixed: critical',
    high: 'Impact if never fixed: high',
    medium: 'Impact if never fixed: medium',
    low: 'Impact if never fixed: low',
    cosmetic: 'Impact if never fixed: cosmetic',
  } satisfies Record<Severity, string>,
  security: {
    live: 'Weakens a guarantee already given; no hostile principal needed',
    boundary: 'A trust-boundary hole that needs a hostile principal',
    hardening: 'Defence in depth; no concrete attack path today',
    none: 'Not a security matter',
  } satisfies Record<SecurityClass, string>,
  type: {
    task: 'Work to do',
    bug: 'Something that does not work as it should',
    feature: 'Something new',
    decision: 'A choice to make and record',
    question: 'Something to find out',
  } satisfies Record<TicketType, string>,
  urgency: {
    now: 'Now: a defect in unreleased work, or a statement measured false',
    release: 'Gates the release, or is gated on it',
    next: 'Medium or worse, and its trigger is live',
    later: 'A decided or cheap known fix',
    icebox: 'Needs a product call, a decision or an escalation first',
  } satisfies Record<Urgency, string>,
};
