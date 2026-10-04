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
    review: 'The work is checked before it ends: the code read, the result tried',
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
  /**
   * The horizons of docs/adr/0010 D3 as amended 2026-10-04: the API's `urgency` is the ticket's
   * horizon, the one a person or an agent set, and the UI calls it so.
   */
  urgency: {
    now: 'Now: to be worked on now — maybe still to be refined, but it matters to the project now, or it is a low-hanging fruit',
    release: 'Release: has to be in the next release',
    next: 'Next: taken up when now is empty, to move the project forward',
    later:
      'Later: worth less at the moment — maybe some day, maybe never; kept so it is not forgotten',
    icebox: 'Icebox: frozen as things stand, kept until what it waits for changes',
  } satisfies Record<Urgency, string>,
};
