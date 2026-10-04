import { Capability } from '../api/models';
import { CAPABILITY } from '../api/models/capability-array';

/** What each capability lets an agent do beyond the baseline (docs/adr/0043 D4). */
export const capabilityMeanings: Record<Capability, string> = {
  decide: 'Move a ticket from analysed to decided.',
  close: 'Move a ticket to done; the verification note stays mandatory.',
  drop: 'Move a ticket to dropped, with a reason.',
  rank: 'Move the rank and adopt the score.',
  'override-urgency': 'Override the urgency of a ticket, with a reason.',
  interest: 'Register need and urgent interest, not only watch.',
  upload: 'Upload attachments.',
  'create-project': 'Create a project and bind a repository, where you may.',
  'record-answer': 'Record an answer you gave, marked as recorded by the agent.',
};

/** The "assisted" shortcut of docs/adr/0043 D4: a person stays the one to decide, close and rank. */
export const assisted: Capability[] = CAPABILITY.filter(
  (capability) =>
    !['decide', 'close', 'rank', 'create-project', 'record-answer'].includes(capability),
);
