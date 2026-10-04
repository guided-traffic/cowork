import { HttpErrorResponse } from '@angular/common/http';
import { effect, Injector, ResourceRef, untracked } from '@angular/core';

/** The resources that wait for their current load to end before they reload. */
const waiting = new WeakSet<ResourceRef<unknown>>();

/**
 * Reloads a resource because something it shows changed. A resource that is loading already —
 * its first load, or a reload — refuses `reload()`, and the answer in flight may predate the
 * change; it reloads once more when that load ends, so an event that arrives during a load is
 * never lost. A resource without parameters (idle) has nothing to reload.
 */
export function refresh(ref: ResourceRef<unknown>, injector: Injector): void {
  if (ref.reload() || !ref.isLoading() || waiting.has(ref)) {
    return;
  }
  waiting.add(ref);
  const watcher = effect(
    () => {
      if (!ref.isLoading()) {
        waiting.delete(ref);
        watcher.destroy();
        untracked(() => ref.reload());
      }
    },
    { injector, manualCleanup: true },
  );
}

/**
 * Whether a failed request says that what it asked for is gone for the person: the session ended
 * (`401`), the access did (`403`), or the thing itself (`404`).
 */
export function gone(error: unknown): boolean {
  return (
    error instanceof HttpErrorResponse &&
    (error.status === 401 || error.status === 403 || error.status === 404)
  );
}

/**
 * The loader of a resource that loads again on events: it answers with what `load` gets, and a
 * failed reload with the value the resource shows, unless the failure says the value is
 * {@link gone}. An outage, a timeout or a `5xx` leave the view as it was — the polling fallback is
 * there for them (docs/adr/0054 D7) — as `TicketsService` keeps a cached ticket. A first load, or a
 * load for other params, shows nothing yet and fails.
 */
export async function keepShown<T>(
  ref: ResourceRef<T | undefined>,
  load: () => Promise<T>,
): Promise<T> {
  try {
    return await load();
  } catch (error) {
    if (!gone(error) && ref.hasValue()) {
      return ref.value();
    }
    throw error;
  }
}
