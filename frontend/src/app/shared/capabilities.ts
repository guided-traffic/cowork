import { Capability } from '../api/models';
import { CAPABILITY } from '../api/models/capability-array';

/** A capability a person chooses (docs/adr/0043 D4): every value of the API's `Capability`. */
export type SelectableCapability = Capability;

/** The nine selectable capabilities, in the catalogue's order. */
export const selectableCapabilities: SelectableCapability[] = [...CAPABILITY];

/** What each capability lets an agent do beyond the baseline (docs/adr/0043 D4). */
export const capabilityMeanings: Record<SelectableCapability, string> = {
  decide: 'Move a ticket from analysed to decided.',
  close: 'Move a ticket to done; the verification note stays mandatory.',
  drop: 'Move a ticket to dropped, with a reason.',
  rank: 'Move tickets in the order of the backlog, and adopt the score.',
  'set-horizon':
    'Set the horizon of a ticket — now, release, next, later or icebox —, with a reason.',
  interest: 'Register need and urgent interest, not only watch.',
  upload: 'Upload attachments.',
  'create-project': 'Create a project and bind a repository, where you may.',
  'record-answer': 'Record an answer you gave, marked as recorded by the agent.',
};

/** The "assisted" shortcut of docs/adr/0043 D4: a person stays the one to decide, close and rank. */
export const assisted: SelectableCapability[] = selectableCapabilities.filter(
  (capability) =>
    !['decide', 'close', 'rank', 'create-project', 'record-answer'].includes(capability),
);
