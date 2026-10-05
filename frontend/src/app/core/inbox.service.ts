import { computed, inject, Injectable, Injector, resource, ResourceRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import { listMyInbox, markMyInboxRead, markNotificationRead } from '../api/functions';
import { EventStreamService } from './event-stream.service';
import { keepShown, refresh } from './refresh';
import { SessionService } from './session.service';

/**
 * Every page of a person-level list up to `pages`, followed cursor by cursor (docs/adr/0048 D3):
 * what a page holds after *Load more*, asked for again whole when it reloads, so that the order is
 * the current one and nothing loaded goes.
 */
export async function followPages<T>(
  pages: number,
  page: (cursor: string | undefined) => Promise<{ items: T[]; next_cursor: string | null }>,
): Promise<PersonPages<T>> {
  const items: T[] = [];
  let cursor: string | undefined;
  for (let n = 0; n < pages; n++) {
    const next = await page(cursor);
    items.push(...next.items);
    if (next.next_cursor === null) {
      return { items, nextCursor: null };
    }
    cursor = next.next_cursor;
  }
  return { items, nextCursor: cursor ?? null };
}

/** What a person-level page holds: the items of its pages, and whether there are more. */
export interface PersonPages<T> {
  items: T[];
  nextCursor: string | null;
}

/** The page size of the person-level lists. */
export const personPageSize = 50;

/**
 * The person's inbox (docs/adr/0020): the unread count of the bell in the top bar — loaded once the
 * person is known, kept by the person-level stream's `inbox.changed` (docs/adr/0054 D2) and loaded
 * again on `resync` and the fallback's `poll` — and marking notifications read. The inbox spans the
 * person's tenants (D1), so this service follows no tenant.
 */
@Injectable({ providedIn: 'root' })
export class InboxService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

  /**
   * The person's id, a primitive: `me` loaded again with the same person leaves the count alone
   * (a resource loads again every time its params function runs).
   */
  private readonly person = computed(() => this.session.person()?.id);

  /** The unread count; the answer of one entry of the inbox carries it. */
  readonly unread: ResourceRef<number | undefined> = resource({
    params: () => this.person(),
    loader: () =>
      keepShown(this.unread, () =>
        this.api.invoke(listMyInbox, { limit: 1 }).then((list) => list.unread),
      ),
  });

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (event.name === 'inbox.changed') {
          this.unread.set(event.unread);
        } else if (event.name === 'resync' || event.name === 'poll') {
          refresh(this.unread, this.injector);
        }
      });
  }

  /** The count to show: none until it is known. */
  count(): number {
    return this.unread.hasValue() ? (this.unread.value() ?? 0) : 0;
  }

  /** Marks one notification read; the answer is the count after it. */
  async markRead(id: string): Promise<void> {
    const state = await this.api.invoke(markNotificationRead, { notification: id });
    this.unread.set(state.unread);
  }

  /**
   * Marks every notification up to and including `through` — the newest the person saw — read
   * (docs/adr/0020 D6); one that arrived after it stays unread.
   */
  async markAllRead(through: string): Promise<void> {
    const state = await this.api.invoke(markMyInboxRead, { body: { through } });
    this.unread.set(state.unread);
  }
}
