import { InjectionToken } from '@angular/core';

/** Leaves the application for a full load of a path of this site. */
export type HardNavigation = (url: string) => void;

/**
 * How a sign-in and a sign-out end: with a new document, not a route change. The services of the
 * application are root singletons that keep what the person last looked at — their tokens, a
 * tenant's accounts, the tickets in the cache — and a router navigation would leave all of it in
 * memory for whoever signs in next in the same tab. A new document starts with nothing. The URL is
 * always a path of this application: `/login`, or the way back that `safeReturn` has checked.
 * Tests provide a function of their own.
 */
export const HARD_NAVIGATION = new InjectionToken<HardNavigation>('HARD_NAVIGATION', {
  providedIn: 'root',
  factory: () => (url) => window.location.assign(url),
});
