import { ParamMap, Params } from '@angular/router';
import { SavedFilterParameters } from '../../api/models';
import { withoutEmpty } from '../project/saved-filter-model';

/** How the address writes a filter: repeated, one text, a number or a flag. */
type Kind = 'repeated' | 'text' | 'number' | 'flag';

/**
 * Every filter of the ticket lists (docs/adr/0049 D1) and how the page's address writes it, in the
 * order the address lists them. The table is held to the generated `SavedFilterParameters`: a filter
 * the API document adds does not compile here until it has its kind.
 */
export const parameterKinds = {
  project: 'repeated',
  state: 'repeated',
  type: 'repeated',
  severity: 'repeated',
  security: 'repeated',
  horizon: 'repeated',
  effort: 'repeated',
  assignee: 'repeated',
  reporter: 'repeated',
  q: 'text',
  parent: 'repeated',
  interest: 'repeated',
  blocked: 'flag',
  has_open_questions: 'flag',
  include_terminal: 'flag',
  progress_min: 'number',
  progress_max: 'number',
  opened_after: 'text',
  opened_before: 'text',
  updated_after: 'text',
  updated_before: 'text',
  done_after: 'text',
  // The name horizon had before (docs/adr/0010 D1): an address that still names it is passed on as
  // it is, and the API takes it as horizon until a later release removes it.
  urgency: 'repeated',
} as const satisfies Record<keyof SavedFilterParameters, Kind>;

/** The parameters the bar has a select for, in its order; the text is `q`, a field of its own. */
export const barNames = [
  'project',
  'state',
  'type',
  'severity',
  'security',
  'horizon',
  'effort',
  'assignee',
  'reporter',
] as const;
export type BarName = (typeof barNames)[number];

/**
 * The filter the address names: every filter of the ticket lists, a repeated one with each value
 * once. A number or a flag that does not read as one is kept as it is written, so that the list's
 * request names it and the server refuses it by name (docs/adr/0049 D4) instead of the page
 * dropping it unseen.
 */
export function filterOf(query: ParamMap): SavedFilterParameters {
  const filter: Record<string, unknown> = {};
  for (const [name, kind] of Object.entries(parameterKinds)) {
    if (kind === 'repeated') {
      const values = [...new Set(query.getAll(name).map((value) => value.trim()))].filter(
        (value) => value !== '',
      );
      if (values.length > 0) {
        filter[name] = values;
      }
      continue;
    }
    const value = query.get(name)?.trim();
    if (value) {
      filter[name] = kind === 'number' ? numberOf(value) : kind === 'flag' ? flagOf(value) : value;
    }
  }
  return filter as SavedFilterParameters;
}

/** A filter as the address writes it; what holds no condition is left out. */
export function queryOf(filter: SavedFilterParameters): Params {
  const held = withoutEmpty(filter) as Record<string, unknown>;
  const query: Params = {};
  for (const name of Object.keys(parameterKinds)) {
    const value = held[name];
    if (value !== undefined && value !== '') {
      query[name] = Array.isArray(value) ? value : String(value);
    }
  }
  return query;
}

/** The values of a parameter a select shows: the plain ones. A negated value is beyond the bar. */
export function plain(values: readonly string[] | undefined): string[] {
  return (values ?? []).filter((value) => !value.startsWith('!'));
}

/** The filter with a select's choice for its parameter, the negated values of it kept. */
export function choosing(
  filter: SavedFilterParameters,
  name: BarName,
  chosen: readonly string[],
): SavedFilterParameters {
  const values = [...chosen, ...(filter[name] ?? []).filter((value) => value.startsWith('!'))];
  const next = { ...filter };
  if (values.length > 0) {
    next[name] = values;
  } else {
    delete next[name];
  }
  return next;
}

/**
 * What a filter holds beyond the bar's controls — a link's or a saved filter's: every parameter the
 * bar has no control for, and the negated values of those it has.
 */
export function beyondBar(filter: SavedFilterParameters): SavedFilterParameters {
  const beyond: Record<string, unknown> = {};
  for (const [name, value] of Object.entries(filter)) {
    if (name === 'q') {
      continue;
    }
    if ((barNames as readonly string[]).includes(name)) {
      const negated = (value as string[]).filter((each) => each.startsWith('!'));
      if (negated.length > 0) {
        beyond[name] = negated;
      }
    } else {
      beyond[name] = value;
    }
  }
  return beyond as SavedFilterParameters;
}

/** An option of a select: the value the address writes and what the person reads. */
export interface Choice {
  value: string;
  label: string;
}

/**
 * The options of a select, and each chosen value they do not hold under its own name — a project
 * the list does not offer, a person who left, a value no longer in a vocabulary —, so that the
 * select shows every condition the filter applies.
 */
export function withChosen(options: readonly Choice[], chosen: readonly string[]): Choice[] {
  const known = new Set(options.map((option) => option.value));
  return [
    ...options,
    ...chosen.filter((value) => !known.has(value)).map((value) => ({ value, label: value })),
  ];
}

/** Two lists of options, or of values, that are the same. */
export function sameList<T>(a: readonly T[], b: readonly T[]): boolean {
  return a.length === b.length && JSON.stringify(a) === JSON.stringify(b);
}

function numberOf(value: string): number {
  return /^-?\d+$/.test(value) ? Number(value) : (value as unknown as number);
}

function flagOf(value: string): boolean {
  return value === 'true' ? true : value === 'false' ? false : (value as unknown as boolean);
}
