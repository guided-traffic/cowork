import { DOCUMENT } from '@angular/common';
import { DestroyRef, inject } from '@angular/core';

/**
 * Keeps the dialogs of a component open on Escape while `busy()` — while a request of the form is
 * running, whose refusal must land in the form that sent it. PrimeNG binds the Escape key of a
 * dialog once, when it opens, and never reads `closable` or `closeOnEscape` again, so turning them
 * off for the length of a request would leave Escape closing the dialog. A listener in the capture
 * phase of the document comes first and stops the key before PrimeNG's listener sees it. The cross
 * and the click beside the dialog are read at each use, so `closable` and `dismissableMask` do for
 * those. Call it in the constructor of the component.
 */
export function keepOpenWhile(busy: () => boolean): void {
  const document = inject(DOCUMENT);
  const stop = (event: KeyboardEvent) => {
    if (event.key === 'Escape' && busy()) {
      event.stopPropagation();
    }
  };
  document.addEventListener('keydown', stop, true);
  inject(DestroyRef).onDestroy(() => document.removeEventListener('keydown', stop, true));
}
