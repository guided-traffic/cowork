import { DestroyRef, inject, Injector, ResourceRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import {
  changesVisibility,
  EventStreamService,
  StreamEvent,
} from '../../core/event-stream.service';
import { refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { splitKey } from '../../core/tickets.service';

/** The route of a ticket by its canonical key, `acme/COW-12` → `/t/acme/tickets/COW-12`. */
export function ticketRoute(key: string): string[] {
  const { team, key: short } = splitKey(key);
  return ['/t', team, 'tickets', short];
}

/** The short key a person reads beside the tenant's name, `acme/COW-12` → `COW-12`. */
export function shortKey(key: string): string {
  return splitKey(key).key;
}

/**
 * Loads a person-level list again when the person-level stream says it may have changed. The
 * stream carries the events of every tenant the person belongs to (docs/adr/0054 D1 as amended on
 * 2026-10-05), so a person-level page follows all of them through this one call, made in its
 * constructor: the events `changes` picks, of any tenant; a change of what the person sees in any
 * tenant — a membership act that names them, a tenant joined or left among them, or a project's
 * restriction or access list ({@link changesVisibility}); a `resync` and the fallback's `poll`. A
 * burst costs a load in flight and one after it ({@link refresh}).
 */
export function reloadOn(
  list: ResourceRef<unknown>,
  changes: (event: StreamEvent) => boolean,
): void {
  const injector = inject(Injector);
  const session = inject(SessionService);
  inject(EventStreamService)
    .events.pipe(takeUntilDestroyed(inject(DestroyRef)))
    .subscribe((event) => {
      if (
        event.name === 'resync' ||
        event.name === 'poll' ||
        (event.name === 'membership.changed' && changesVisibility(event, session.person()?.id)) ||
        changes(event)
      ) {
        refresh(list, injector);
      }
    });
}
