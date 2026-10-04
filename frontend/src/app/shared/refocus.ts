import { afterNextRender, Injector } from '@angular/core';

/**
 * Puts the keyboard, once the page has rendered, on the first of the targets that is in the host:
 * the control a dialog was opened from, else one beside it, else the page's heading. PrimeNG's
 * dialogs do not give the focus back when they close, and a control that went with its row would
 * leave it on the document's body. The targets are selectors made of ids the page knows, never of
 * text people typed.
 */
export function refocus(host: HTMLElement, injector: Injector, ...targets: string[]): void {
  afterNextRender(
    () => {
      for (const target of targets) {
        const element = host.querySelector<HTMLElement>(target);
        if (element) {
          element.focus();
          return;
        }
      }
    },
    { injector },
  );
}
