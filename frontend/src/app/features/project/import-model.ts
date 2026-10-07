import {
  BlockKind,
  ImportCorrection,
  ImportFile,
  ImportLink,
  ImportMessage,
  ImportOutcome,
  TicketState,
  TicketType,
} from '../../api/models';

/**
 * What the page holds of a person's correction of one file before the execution
 * (docs/adr/0063 D1, docs/adr/0051 D2): its exclusion, or a type, a state with its block and an
 * assignee. An unset field leaves the report's value.
 */
export interface Draft {
  exclude?: boolean;
  type?: TicketType;
  state?: TicketState;
  /** The block of a file corrected to `blocked`, as far as the person has filled it in. */
  block?: DraftBlock;
  /** A member's id, `null` for nobody. */
  assignee?: string | null;
}

export interface DraftBlock {
  kind: BlockKind | null;
  reason: string;
  from: TicketState | null;
}

/** The kinds a correction's block takes: a block on a ticket is a `blocks` link, made after the import. */
export const correctionBlockKinds: BlockKind[] = [
  'human',
  'decision',
  'product',
  'release',
  'external',
];

/** The states a block comes from (docs/adr/0009 D2). */
export const blockableStates: TicketState[] = [
  'filed',
  'analysed',
  'decided',
  'in-progress',
  'review',
];

/** The longest reason of a block the API takes. */
export const blockReasonLimit = 2000;

/** What each outcome means, for the tooltip of its pill; the value is shown as the API spells it. */
export const outcomeMeanings: Record<ImportOutcome, string> = {
  create: 'The execution creates its ticket',
  conflict:
    'Its number is a ticket of the project already, or was one: a repeated import is a duplicate, not an update',
  error: 'It cannot be imported as it stands',
  skip: 'No ticket file, or a manifest the import reads beside the tickets',
  exclude: 'Left out by a correction',
  created: 'The execution created its ticket',
};

/** `acme/VKO-12` → `VKO-12`. */
export function shortKey(key: string): string {
  return key.slice(key.indexOf('/') + 1);
}

/** Whether a file of a dry run takes a correction at all: a skipped file takes none. */
export function correctable(file: ImportFile): boolean {
  return file.outcome === 'create' || file.outcome === 'conflict' || file.outcome === 'error';
}

/**
 * Whether a file takes a correction beyond its exclusion: a conflict's number stays taken whatever
 * else changes, so a conflict is only ever left out.
 */
export function changeable(file: ImportFile): boolean {
  return file.outcome === 'create' || file.outcome === 'error';
}

/** The state the file is imported in, after the person's correction. */
export function stateOf(file: ImportFile, draft: Draft | undefined): TicketState | null {
  return draft?.state ?? file.state;
}

/** The assignee's person id the file is imported with, after the person's correction. */
export function assigneeOf(file: ImportFile, draft: Draft | undefined): string | null {
  return draft?.assignee !== undefined ? draft.assignee : (file.assignee?.person?.id ?? null);
}

/**
 * The state a correction's block comes from by default: the file's own state where a block can come
 * from it, else the one its source names, else none — the person names it.
 */
export function defaultBlockFrom(file: ImportFile): TicketState | null {
  if (file.state !== null && blockableStates.includes(file.state)) {
    return file.state;
  }
  return file.block && blockableStates.includes(file.block.from) ? file.block.from : null;
}

/**
 * What the block of a file to be imported as blocked still lacks, as a sentence; null where it is
 * complete, or where the file keeps the block its source gives it.
 */
export function blockLacks(file: ImportFile, draft: Draft | undefined): string | null {
  if (draft?.exclude || !changeable(file) || stateOf(file, draft) !== 'blocked') {
    return null;
  }
  const block = draft?.block;
  if (!block) {
    return file.state === 'blocked' && file.block !== null
      ? null
      : 'The state blocked needs its block: what it waits on and why.';
  }
  if (block.kind === null) {
    return 'The block needs its kind.';
  }
  if (block.reason.trim() === '') {
    return 'The block needs its reason.';
  }
  if (block.reason.trim().length > blockReasonLimit) {
    return `The block's reason is longer than ${blockReasonLimit} characters.`;
  }
  if (block.from === null && (file.state === null || !blockableStates.includes(file.state))) {
    return 'The block needs the state it comes from.';
  }
  return null;
}

/**
 * The correction the execution gets for one file (`ImportCorrection`), or null where the draft
 * changes nothing: a file left out names `exclude` alone; any other only what the person changed
 * from the report — a block with the state `blocked` only.
 */
export function correctionOf(file: ImportFile, draft: Draft | undefined): ImportCorrection | null {
  if (!draft || !correctable(file)) {
    return null;
  }
  if (draft.exclude) {
    return { path: file.path, exclude: true };
  }
  if (!changeable(file)) {
    return null;
  }
  const correction: ImportCorrection = { path: file.path };
  if (draft.type !== undefined && draft.type !== file.type) {
    correction.type = draft.type;
  }
  if (draft.state !== undefined && draft.state !== file.state) {
    correction.state = draft.state;
  }
  const block = draft.block;
  if (stateOf(file, draft) === 'blocked' && block?.kind) {
    correction.state = 'blocked';
    correction.block = {
      kind: block.kind,
      reason: block.reason.trim(),
      ...(block.from ? { from: block.from } : {}),
    };
  }
  const assignee = assigneeOf(file, draft);
  if (draft.assignee !== undefined && assignee !== (file.assignee?.person?.id ?? null)) {
    correction.assignee = assignee;
  }
  return Object.keys(correction).length > 1 ? correction : null;
}

/** The corrections of the execution, in the report's order, each file once. */
export function correctionsOf(
  files: readonly ImportFile[],
  drafts: ReadonlyMap<string, Draft>,
): ImportCorrection[] {
  return files
    .map((file) => correctionOf(file, drafts.get(file.path)))
    .filter((correction): correction is ImportCorrection => correction !== null);
}

/** Whether the person changed the file in anything but its exclusion. */
export function corrected(file: ImportFile, draft: Draft | undefined): boolean {
  return !draft?.exclude && correctionOf(file, draft) !== null;
}

/**
 * The files that block the execution as the page reads them: a conflict not left out — its number
 * stays taken —, and a file with an error neither left out nor corrected. A correction may answer an
 * error — a state that needed a block, say —, and only the execution, which reads every file again
 * with the corrections, tells (docs/adr/0051 D2).
 */
export function blocking(
  files: readonly ImportFile[],
  drafts: ReadonlyMap<string, Draft>,
): ImportFile[] {
  return files.filter((file) => {
    const draft = drafts.get(file.path);
    if (draft?.exclude) {
      return false;
    }
    return file.outcome === 'conflict' || (file.outcome === 'error' && !corrected(file, draft));
  });
}

/**
 * The files the execution would import as the page reads them: those to create, and those with an
 * error the person corrected, none left out.
 */
export function importing(
  files: readonly ImportFile[],
  drafts: ReadonlyMap<string, Draft>,
): ImportFile[] {
  return files.filter((file) => {
    const draft = drafts.get(file.path);
    if (draft?.exclude) {
      return false;
    }
    return file.outcome === 'create' || (file.outcome === 'error' && corrected(file, draft));
  });
}

/** Where in its file a message points: `line 12 · severity`, or nothing. */
export function placeOf(message: ImportMessage): string {
  return [message.line !== null ? `line ${message.line}` : null, message.field]
    .filter((part): part is string => part !== null && part !== '')
    .join(' · ');
}

/**
 * A link as the file's ticket takes part in it: `blocks VKO-3` where the ticket is its source,
 * `VKO-3 blocks this one` where it is its target.
 */
export function linkText(link: ImportLink): string {
  const other = shortKey(link.key);
  return link.direction === 'outgoing' ? `${link.type} ${other}` : `${other} ${link.type} this one`;
}

/**
 * The files an execution's refusal names, by their path: `409 import_conflict` names each as
 * `file:<path>`, and a `400` points at `/corrections/<i>/…`, the correction's index in the request.
 */
export function refusedFiles(
  entries: readonly { pointer: string; message: string }[],
  sent: readonly ImportCorrection[],
): Map<string, string[]> {
  const files = new Map<string, string[]>();
  const add = (path: string, message: string) =>
    files.set(path, [...(files.get(path) ?? []), message]);
  for (const { pointer, message } of entries) {
    if (pointer.startsWith('file:')) {
      add(pointer.slice('file:'.length), message);
      continue;
    }
    const at = /^\/corrections\/(\d+)(?:\/|$)/.exec(pointer);
    const correction = at ? sent[Number(at[1])] : undefined;
    if (correction) {
      add(correction.path, message);
    }
  }
  return files;
}
