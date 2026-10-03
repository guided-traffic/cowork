import { DOCUMENT } from '@angular/common';
import { computed, effect, inject, Injectable, signal } from '@angular/core';

/** The person's choice (docs/adr/0052 D3): follow the system, or fix one scheme. */
export type ThemePreference = 'system' | 'light' | 'dark';
export type ColourScheme = 'light' | 'dark';

/** The class PrimeNG's dark mode selector names, on `<html>`. */
export const darkClass = 'app-dark';
const storageKey = 'cowork.theme';
const order: ThemePreference[] = ['system', 'light', 'dark'];

/**
 * The colour scheme: the preference lives in browser storage (docs/adr/0053 D6), *system*
 * follows `prefers-color-scheme` live, and the effective scheme is the `.app-dark` class on
 * `<html>` that the preset's tokens resolve against.
 */
@Injectable({ providedIn: 'root' })
export class ThemeService {
  private readonly document = inject(DOCUMENT);
  private readonly media = this.document.defaultView?.matchMedia?.('(prefers-color-scheme: dark)');
  private readonly systemDark = signal(this.media?.matches ?? false);

  readonly preference = signal<ThemePreference>(this.stored());
  readonly scheme = computed<ColourScheme>(() => {
    const preference = this.preference();
    if (preference === 'system') {
      return this.systemDark() ? 'dark' : 'light';
    }
    return preference;
  });

  constructor() {
    this.media?.addEventListener('change', (event) => this.systemDark.set(event.matches));
    this.apply(this.scheme());
    effect(() => this.apply(this.scheme()));
  }

  set(preference: ThemePreference): void {
    this.preference.set(preference);
    try {
      this.document.defaultView?.localStorage.setItem(storageKey, preference);
    } catch {
      // Storage refused (private mode, quota): the choice holds for this page only.
    }
  }

  /** system → light → dark → system, for a single toggle button. */
  cycle(): void {
    this.set(order[(order.indexOf(this.preference()) + 1) % order.length]);
  }

  private stored(): ThemePreference {
    try {
      const value = this.document.defaultView?.localStorage.getItem(storageKey);
      return order.includes(value as ThemePreference) ? (value as ThemePreference) : 'system';
    } catch {
      return 'system';
    }
  }

  private apply(scheme: ColourScheme): void {
    this.document.documentElement.classList.toggle(darkClass, scheme === 'dark');
  }
}
