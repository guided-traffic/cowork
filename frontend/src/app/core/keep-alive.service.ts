import { DOCUMENT } from '@angular/common';
import { DestroyRef, inject, Injectable } from '@angular/core';
import { Api } from '../api/api';
import { getMe } from '../api/fn/me/get-me';

/** How often the keep-alive may ask, at most: every five minutes (docs/adr/0031 D3). */
export const keepAliveEvery = 5 * 60_000;

/** The input that says a person works in the page: a pointer pressed, a key, the wheel, a touch. */
export const workInputs = ['pointerdown', 'keydown', 'wheel', 'touchstart'] as const;

/**
 * Keeps the session's idle clock moving while a person works in the page (docs/adr/0031 D3).
 * Reading a ticket, scrolling a board or writing a long comment makes no request, and the backend
 * moves the clock only on a request, so the idle limit would end the session under the person's
 * hands. This notes the time of the person's last input — {@link workInputs}, through passive
 * listeners that only note it — and every five minutes, while the document is visible and there was
 * input since it last asked, asks `GET /api/v1/me` once, which moves the clock as any request of the
 * session does. A tab nobody works in asks nothing, and the event stream moves no clock: an
 * unattended tab still reaches the idle limit. A `401` goes to the login page as any `401` does
 * (`signInOnUnauthorised`); the shell starts this and stops it with itself.
 */
@Injectable({ providedIn: 'root' })
export class KeepAliveService {
  private readonly api = inject(Api);
  private readonly document = inject(DOCUMENT);
  private timer: ReturnType<typeof setInterval> | undefined;
  /**
   * When the person last gave input, and when this last asked — or started; milliseconds since the
   * epoch. An input in the very millisecond of an ask counts as one after it.
   */
  private lastInput = Number.NEGATIVE_INFINITY;
  private lastAsked = 0;
  private readonly noteInput = () => {
    this.lastInput = Date.now();
  };

  constructor() {
    inject(DestroyRef).onDestroy(() => this.stop());
  }

  /** Starts watching for the person's input; started already, it goes on as it is. */
  start(): void {
    if (this.timer !== undefined) {
      return;
    }
    this.lastInput = Number.NEGATIVE_INFINITY;
    this.lastAsked = Date.now();
    for (const name of workInputs) {
      this.document.addEventListener(name, this.noteInput, { capture: true, passive: true });
    }
    this.timer = setInterval(() => this.tick(), keepAliveEvery);
  }

  /** Stops watching and asking. */
  stop(): void {
    if (this.timer === undefined) {
      return;
    }
    clearInterval(this.timer);
    this.timer = undefined;
    for (const name of workInputs) {
      this.document.removeEventListener(name, this.noteInput, true);
    }
  }

  private tick(): void {
    if (this.document.visibilityState !== 'visible' || this.lastInput < this.lastAsked) {
      return;
    }
    this.lastAsked = Date.now();
    // The answer is not needed, the request was: it moved the idle clock. A failure is no news —
    // a 401 has sent the browser to the login page already, and an outage meets the request of
    // the next interval with input.
    this.api.invoke(getMe).catch(() => undefined);
  }
}
