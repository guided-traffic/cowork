import { Signal, signal, WritableSignal } from '@angular/core';

export interface Versioned {
  version: number;
}

export interface Cached<T> {
  value: T;
  /** The strong `ETag` an overwriting write sends as `If-Match` (docs/adr/0050 D3). */
  etag: string;
  loadedAt: number;
}

/** The version is the strong `ETag` (docs/adr/0050 D2). */
export function etagOf(version: number): string {
  return `"${version}"`;
}

/**
 * The one copy of an entity that several views show (docs/adr/0053 D2): the detail page, the
 * boards and the lists read the same entry, and a write or a refetch replaces it for all of them.
 * A value older than the one held is ignored, so a slow list answer cannot undo the refetch an
 * event triggered; an equal version replaces, because derived fields (urgency, a parent's
 * progress) change without a new version.
 */
export class EntityCache<T extends Versioned> {
  private readonly slots = new Map<string, WritableSignal<Cached<T> | undefined>>();

  constructor(private readonly now: () => number = () => Date.now()) {}

  entry(id: string): Signal<Cached<T> | undefined> {
    return this.slot(id).asReadonly();
  }

  value(id: string): T | undefined {
    return this.slots.get(id)?.()?.value;
  }

  etag(id: string): string | undefined {
    return this.slots.get(id)?.()?.etag;
  }

  /** Stores the value unless an older version; returns whether it was stored. */
  put(id: string, value: T, etag: string = etagOf(value.version)): boolean {
    const slot = this.slot(id);
    const held = slot();
    if (held && held.value.version > value.version) {
      return false;
    }
    slot.set({ value, etag, loadedAt: this.now() });
    return true;
  }

  delete(id: string): void {
    this.slots.get(id)?.set(undefined);
  }

  /** Empties every entry; views that read one see it go, and see it again when it is refetched. */
  clear(): void {
    for (const slot of this.slots.values()) {
      slot.set(undefined);
    }
  }

  ids(): string[] {
    return [...this.slots.entries()].filter(([, slot]) => slot() !== undefined).map(([id]) => id);
  }

  private slot(id: string): WritableSignal<Cached<T> | undefined> {
    let slot = this.slots.get(id);
    if (!slot) {
      slot = signal<Cached<T> | undefined>(undefined);
      this.slots.set(id, slot);
    }
    return slot;
  }
}
