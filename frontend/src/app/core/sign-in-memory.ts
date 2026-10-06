import { DOCUMENT } from '@angular/common';
import { inject, Injectable } from '@angular/core';

/** How the person signs in, in the browser's `localStorage`: `oidc`, or nothing. */
export const signInMethodKey = 'cowork.sign-in';
/** That this tab tried to sign in by itself, in the tab's `sessionStorage`. */
export const signInAttemptKey = 'cowork.sign-in.attempt';

/**
 * What the browser remembers of the sign-in, for the login page to sign the person in again by
 * itself once their session has ended (docs/adr/0029 D6):
 *
 * - **how the person signs in** — `cowork.sign-in` in `localStorage`, `oidc` once they started the
 *   identity provider's sign-in with its button. A local sign-in that succeeds and a sign-out forget
 *   it (`AuthService`), so an explicit sign-out is never undone by the page.
 * - **that this tab tried** — `cowork.sign-in.attempt` in `sessionStorage`, noted when the page leaves
 *   for an attempt of its own and cleared once the tab has a session again (`SessionService`). An
 *   issuer that ignores `prompt=none` shows its form instead of an error — Dex does — and a person who
 *   came back from it to the login page would be sent there again at every input: the tab tries once
 *   between two sessions.
 *
 * Every access is in `try`/`catch`: a private window, or storage the browser refuses, remembers
 * nothing, and nothing remembered means no sign-in of the page's own.
 */
@Injectable({ providedIn: 'root' })
export class SignInMemory {
  private readonly window = inject(DOCUMENT).defaultView;

  /** Whether the person signs in through the identity provider, as far as this browser knows. */
  provider(): boolean {
    try {
      return this.window?.localStorage.getItem(signInMethodKey) === 'oidc';
    } catch {
      return false;
    }
  }

  /** The person starts the identity provider's sign-in with its button. */
  rememberProvider(): void {
    try {
      this.window?.localStorage.setItem(signInMethodKey, 'oidc');
    } catch {
      // Storage refused: nothing is remembered, and the page waits for the button.
    }
  }

  /** A local sign-in succeeded, or the person signs out: the page no longer signs in by itself. */
  forgetProvider(): void {
    try {
      this.window?.localStorage.removeItem(signInMethodKey);
    } catch {
      // Storage refused: nothing can have been remembered either.
    }
  }

  /** Whether this tab tried to sign in by itself since its last session; yes where it cannot say. */
  tried(): boolean {
    try {
      return !this.window || this.window.sessionStorage.getItem(signInAttemptKey) !== null;
    } catch {
      return true;
    }
  }

  /** Notes that this tab tries now; false when it cannot note it, and must not try then. */
  markTried(): boolean {
    try {
      if (!this.window) {
        return false;
      }
      this.window.sessionStorage.setItem(signInAttemptKey, '1');
      return true;
    } catch {
      return false;
    }
  }

  /** The tab has a session again: its login page may try once more when that one ends. */
  clearTried(): void {
    try {
      this.window?.sessionStorage.removeItem(signInAttemptKey);
    } catch {
      // Storage refused: nothing was noted.
    }
  }
}
