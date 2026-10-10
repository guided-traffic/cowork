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

/**
 * What a board makes of a saved filter (docs/adr/0018 D4, D5, docs/adr/0049 D6): the projects of
 * its swimlanes — a project the filter excludes, `!OPS`, among them —, and every other condition,
 * which each swimlane's list takes as a pre-filter. `include_terminal` would add the closed
 * tickets, which a board never shows, and stays out.
 */
export interface BoardFilter {
  projects: string[];
  extra: BoardExtra;
}

/** The conditions of a filter a board hands its swimlanes' lists. */
export type BoardExtra = Omit<SavedFilterParameters, 'project' | 'include_terminal'>;

/** A saved filter's parameters as the board applies them. */
export function toBoard(parameters: SavedFilterParameters): BoardFilter {
  const extra = { ...parameters };
  delete extra.project;
  delete extra.include_terminal;
  return { projects: [...(parameters.project ?? [])], extra };
}

/** The board's projects and the rest of an applied filter as one parameter set to save. */
export function fromBoard(projects: readonly string[], extra: BoardExtra): SavedFilterParameters {
  return withoutEmpty({ project: [...projects], ...extra });
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

/**
 * The conditions of a saved filter a list does not apply, each with why it does not: a backlog is
 * one project, so it leaves `project` out; a board shows no closed ticket, so it leaves
 * `include_terminal` out; the tenant's ticket list applies every one.
 */
export type LeftOut = Partial<Record<keyof SavedFilterParameters, string>>;

/** What a backlog leaves out of a filter. */
export const backlogLeftOut: LeftOut = {
  project: 'a backlog is one project; the team’s ticket list applies this condition',
};

/** What a board leaves out of a filter. */
export const boardLeftOut: LeftOut = {
  include_terminal: 'a board shows no closed ticket; the team’s ticket list applies this condition',
};

/** What a list says of a filter it applies: its warnings, and each condition it leaves out. */
export function notesOf(filter: SavedFilter, leftOut: LeftOut = {}): string[] {
  const notes = filter.warnings.map((w) => `${w.parameter}: ${w.message}`);
  for (const [name, why] of Object.entries(leftOut)) {
    if (held(filter.parameters[name as keyof SavedFilterParameters])) {
      notes.push(`${name}: ${why}`);
    }
  }
  return notes;
}

/** Whether a parameter holds a condition: a value, or a list that is not empty. */
function held(value: unknown): boolean {
  return value !== undefined && value !== null && !(Array.isArray(value) && value.length === 0);
}

/** The parameters that hold a condition, the empty ones left out. */
export function withoutEmpty<T extends object>(value: T): T {
  return Object.fromEntries(Object.entries(value).filter(([, v]) => held(v))) as T;
}
