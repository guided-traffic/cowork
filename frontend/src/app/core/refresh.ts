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
