import {
  DashboardAgeBucket,
  DashboardStateCount,
  DashboardWeek,
  TicketState,
} from '../../api/models';

/** The open states in the order of docs/adr/0009 D1. */
export const openStates: TicketState[] = [
  'filed',
  'analysed',
  'decided',
  'in-progress',
  'review',
  'blocked',
];

/** The length of the default period, as the API has it: the thirty days that end today. */
export const periodDays = 30;

/** A day of the API, `2026-10-07`, moved by `days`; the calendar's arithmetic, no time zone. */
export function addDays(day: string, days: number): string {
  const moved = new Date(`${day}T00:00:00Z`);
  moved.setUTCDate(moved.getUTCDate() + days);
  return moved.toISOString().slice(0, 10);
}

/**
 * The period the page reads (docs/adr/0018 D6): the days the address names, else the thirty days that
 * end today in the browser's calendar — a booking's day is the person's (docs/adr/0055 D3).
 */
export function periodOf(
  from: string | null,
  to: string | null,
  today: string,
): { from: string; to: string } {
  const last = to || today;
  return { from: from || addDays(last, 1 - periodDays), to: last };
}

/** A state with the count of open tickets in it. */
export interface StateShare {
  state: TicketState;
  count: number;
}

/** Tile 1's open tickets per state over every counted project, in the states' order, zero included. */
export function stateTotals(rows: DashboardStateCount[]): StateShare[] {
  return openStates.map((state) => ({
    state,
    count: rows.filter((row) => row.state === state).reduce((sum, row) => sum + row.count, 0),
  }));
}

export interface ProjectCard {
  key: string;
  name: string;
  description: string;
  open: number;
  byState: StateShare[];
}

/**
 * A card per counted project — every project of the list without a plain value in the filter, else
 * the ones it names, never a negated one — with its open tickets by state from tile 1, the states
 * without one left out.
 */
export function projectCards(
  projects: { key: string; name: string; description: string }[],
  rows: DashboardStateCount[],
  filter: string[],
): ProjectCard[] {
  const named = filter.filter((value) => !value.startsWith('!'));
  const without = filter.filter((value) => value.startsWith('!')).map((value) => value.slice(1));
  return projects
    .filter((project) => named.length === 0 || named.includes(project.key))
    .filter((project) => !without.includes(project.key))
    .map((project) => {
      const byState = stateTotals(rows.filter((row) => row.project === project.key)).filter(
        (part) => part.count > 0,
      );
      return {
        key: project.key,
        name: project.name,
        description: project.description,
        open: byState.reduce((sum, part) => sum + part.count, 0),
        byState,
      };
    });
}

/** A bar's length: `count` of `largest`, as a CSS width; an empty tile draws nothing. */
export function share(count: number, largest: number): string {
  return `${largest > 0 ? (100 * count) / largest : 0}%`;
}

/** The names of the age buckets by their lower bound in days (docs/adr/0018 D6). */
const ageNames: Record<number, string> = {
  0: 'Under a week',
  7: '1 to 4 weeks',
  30: '1 to 3 months',
  90: '3 to 12 months',
  365: 'A year or more',
};

/** An age bucket's name; a bucket this page does not know is named by its days. */
export function ageLabel(bucket: DashboardAgeBucket): string {
  return (
    ageNames[bucket.from_days] ??
    (bucket.to_days === null
      ? `${bucket.from_days} days or more`
      : `${bucket.from_days} to ${bucket.to_days} days`)
  );
}

/**
 * A lead time in words: minutes under an hour, hours under two days, days with one decimal after
 * that — `45 min`, `5 h`, `3.5 days`.
 */
export function leadTime(seconds: number): string {
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) {
    return `${minutes} min`;
  }
  const hours = Math.round(seconds / 3600);
  if (hours < 48) {
    return `${hours} h`;
  }
  return `${(Math.round((seconds / 86400) * 10) / 10).toFixed(1)} days`;
}

/** A day of the API in the browser's locale, without the year: `Oct 5`, `5. Okt.`. */
export function shortDay(day: string, locale?: string): string {
  return new Date(`${day}T00:00:00Z`).toLocaleDateString(locale, {
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  });
}

/** Whether a week of throughput ends before its Sunday: the period's last day cut it. */
export function partialWeek(week: DashboardWeek): boolean {
  return addDays(week.from, 6) !== week.to;
}

/** `acme/VKO-12` → `VKO-12`, the key a ticket's page is addressed by. */
export function shortKey(key: string): string {
  return key.slice(key.indexOf('/') + 1);
}
