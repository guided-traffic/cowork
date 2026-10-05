import { SavedFilter, SavedFilterParameters, TicketState } from '../../api/models';

/**
 * What a backlog makes of a saved filter (docs/adr/0018 D5, docs/adr/0049 D6): the search, the
 * open states its select shows, and every other parameter as it is, which the backlog hands on to
 * its list. `project` is no parameter of a project's list and stays out.
 */
export interface BacklogFilter {
  q: string;
  states: TicketState[];
  extra: SavedFilterParameters;
}

/** A saved filter's parameters as the backlog applies them. */
export function toBacklog(
  parameters: SavedFilterParameters,
  selectable: readonly TicketState[],
): BacklogFilter {
  const { q, state } = parameters;
  const rest = { ...parameters };
  delete rest.q;
  delete rest.state;
  delete rest.project;
  const states = (state ?? []).filter((value): value is TicketState =>
    selectable.includes(value as TicketState),
  );
  const others = (state ?? []).filter((value) => !selectable.includes(value as TicketState));
  return { q: q ?? '', states, extra: others.length > 0 ? { ...rest, state: others } : rest };
}

/** The backlog's search, states and the rest of an applied filter as one parameter set to save. */
export function fromBacklog(
  q: string,
  states: readonly TicketState[],
  extra: SavedFilterParameters,
): SavedFilterParameters {
  const state = [...states, ...(extra.state ?? [])];
  const out: SavedFilterParameters = { ...extra, state: state.length > 0 ? state : undefined };
  const text = q.trim();
  if (text !== '') {
    out.q = text;
  }
  return withoutEmpty(out);
}

/** The parameters a list's query takes from the rest of a filter, the states merged in. */
export function listParameters(
  extra: SavedFilterParameters,
  states: readonly TicketState[],
): SavedFilterParameters & { state?: string[] } {
  const state = [...states, ...(extra.state ?? [])];
  return withoutEmpty({ ...extra, state: state.length > 0 ? state : undefined });
}

/** A parameter set as a line a person reads: `severity=high,critical · q="crash"`. */
export function describe(parameters: SavedFilterParameters): string {
  const parts = Object.entries(withoutEmpty(parameters)).map(([name, value]) =>
    Array.isArray(value)
      ? `${name}=${value.join(',')}`
      : typeof value === 'string' && name === 'q'
        ? `q="${value}"`
        : `${name}=${String(value)}`,
  );
  return parts.length > 0 ? parts.join(' · ') : 'no condition';
}

/** What the backlog says of a filter it applies: its warnings, and a project it leaves out. */
export function notesOf(filter: SavedFilter): string[] {
  const notes = filter.warnings.map((w) => `${w.parameter}: ${w.message}`);
  if ((filter.parameters.project ?? []).length > 0) {
    notes.push('project: a backlog is one project; this condition applies to the tenant’s lists');
  }
  return notes;
}

function withoutEmpty<T extends object>(value: T): T {
  return Object.fromEntries(
    Object.entries(value).filter(
      ([, v]) => v !== undefined && v !== null && !(Array.isArray(v) && v.length === 0),
    ),
  ) as T;
}
