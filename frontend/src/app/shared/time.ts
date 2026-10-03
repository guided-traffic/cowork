import { DestroyRef, inject, Injectable, signal } from '@angular/core';

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 365 * 24 * 3600],
  ['month', 30 * 24 * 3600],
  ['week', 7 * 24 * 3600],
  ['day', 24 * 3600],
  ['hour', 3600],
  ['minute', 60],
];
const relative = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });

/**
 * `3 minutes ago`, `yesterday`, `just now` — English words (docs/adr/0055 D1). Whole units only,
 * cut toward zero: 59 minutes and 59 seconds is "59 minutes ago", never "60 minutes ago".
 */
export function ago(iso: string, now: number): string {
  const seconds = Math.round((Date.parse(iso) - now) / 1000);
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return relative.format(Math.trunc(seconds / size), unit);
    }
  }
  return 'just now';
}

/** The browser locale's date and time (docs/adr/0055 D2). */
export function dateTime(iso: string, locale?: string): string {
  return new Date(iso).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
}

/** A clock for relative times: they move on twice a minute without a change detection cycle of their own. */
@Injectable({ providedIn: 'root' })
export class Clock {
  readonly now = signal(Date.now());

  constructor() {
    const timer = setInterval(() => this.now.set(Date.now()), 30_000);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));
  }
}

/** `1 open ticket`, `3 open tickets` — the plural is English's, with an `s` unless named. */
export function count(n: number, singular: string, plural = `${singular}s`): string {
  return `${n} ${n === 1 ? singular : plural}`;
}

/**
 * A duration as a person types it, in minutes: `90`, `1:30`, `1h 30m`, `1.5h`, `45m`. Null for
 * anything else, or for nothing.
 */
export function parseDuration(text: string): number | null {
  const value = text.trim().toLowerCase();
  let minutes: number | null = null;
  if (/^\d+$/.test(value)) {
    minutes = Number(value);
  } else if (/^\d+:[0-5]\d$/.test(value)) {
    const [hours, rest] = value.split(':').map(Number);
    minutes = hours * 60 + rest;
  } else {
    const match = /^(?:(\d+(?:[.,]\d+)?)\s*h)?\s*(?:(\d+)\s*m(?:in)?)?$/.exec(value);
    if (match && (match[1] || match[2])) {
      minutes =
        Math.round(Number((match[1] ?? '0').replace(',', '.')) * 60) + Number(match[2] ?? 0);
    }
  }
  return minutes !== null && minutes > 0 ? minutes : null;
}

/** `95` → `1 h 35 min`; `45` → `45 min`. */
export function duration(minutes: number): string {
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  if (hours === 0) {
    return `${rest} min`;
  }
  return rest === 0 ? `${hours} h` : `${hours} h ${rest} min`;
}

/**
 * A byte count in the browser's number format: `840 B`, `12.4 KB`, `3.1 MB`. The unit is chosen
 * after rounding, so 1048575 bytes read `1 MB`, never `1,024 KB`.
 */
export function size(bytes: number, locale?: string): string {
  const format = (value: number) => value.toLocaleString(locale, { maximumFractionDigits: 1 });
  const kilobytes = Math.round((bytes / 1024) * 10) / 10;
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (kilobytes < 1024) {
    return `${format(kilobytes)} KB`;
  }
  return `${format(bytes / (1024 * 1024))} MB`;
}

/** Today's date in the browser's time zone as the API's day, `2026-10-03` (docs/adr/0055 D3). */
export function today(now = new Date()): string {
  const local = new Date(now.getTime() - now.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 10);
}
