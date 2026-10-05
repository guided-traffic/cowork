import { DestroyRef, inject, Injector, ResourceRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { EventStreamService, StreamEvent } from '../../core/event-stream.service';
import { refresh } from '../../core/refresh';
import { splitKey } from '../../core/tickets.service';

/** The route of a ticket by its canonical key, `acme/COW-12` → `/t/acme/tickets/COW-12`. */
export function ticketRoute(key: string): string[] {
  const { tenant, key: short } = splitKey(key);
  return ['/t', tenant, 'tickets', short];
}

/** The short key a person reads beside the tenant's name, `acme/COW-12` → `COW-12`. */
export function shortKey(key: string): string {
  return splitKey(key).key;
}

/**
 * Loads a person-level list again when the person-level stream says it may have changed
 * (docs/adr/0054 D1): the events `changes` picks, a `resync` and the fallback's `poll`. The stream
 * carries the person's own events across their tenants and the events of one tenant; a change in
 * another tenant that is not the person's own shows at the next reload (docs/adr/0018 D3).
 */
export function reloadOn(
  list: ResourceRef<unknown>,
  changes: (event: StreamEvent) => boolean,
): void {
  const injector = inject(Injector);
  inject(EventStreamService)
    .events.pipe(takeUntilDestroyed(inject(DestroyRef)))
    .subscribe((event) => {
      if (event.name === 'resync' || event.name === 'poll' || changes(event)) {
        refresh(list, injector);
      }
    });
}
