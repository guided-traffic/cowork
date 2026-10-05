import { computed, inject, Injectable, Injector, resource, ResourceRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import {
  createSavedFilter,
  deleteSavedFilter,
  listSavedFilters,
  updateSavedFilter,
} from '../api/functions';
import { SavedFilter, SavedFilterParameters, SavedFilterPatch } from '../api/models';
import { etagOf } from './entity-cache';
import { EventStreamService } from './event-stream.service';
import { keepShown, refresh } from './refresh';
import { SessionService } from './session.service';

/**
 * The saved filters of the tenant the pages show (docs/adr/0018 D5, docs/adr/0049 D6): the
 * person's own and those shared with the tenant, every page of them, and the owner's acts on
 * their own — saving, renaming or sharing with `If-Match`, deleting. The filters are not on the
 * event stream: the list loads again after each act, on `resync` and on `poll`, and when a page
 * that offers them opens.
 */
@Injectable({ providedIn: 'root' })
export class SavedFiltersService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

  readonly filters: ResourceRef<SavedFilter[] | undefined> = resource({
    params: () => this.session.workTenant() ?? undefined,
    loader: ({ params: tenant }) =>
      keepShown(this.filters, async () => {
        const filters: SavedFilter[] = [];
        let cursor: string | undefined;
        do {
          const page = await this.api.invoke(listSavedFilters, { tenant, cursor, limit: 200 });
          filters.push(...page.items);
          cursor = page.next_cursor ?? undefined;
        } while (cursor);
        return filters;
      }),
  });

  readonly list = computed<SavedFilter[]>(() =>
    this.filters.hasValue() ? this.filters.value() : [],
  );

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (event.name === 'resync' || event.name === 'poll') {
          refresh(this.filters, this.injector);
        }
      });
  }

  /** Loads the list again: another member may have shared one meanwhile. */
  reload(): void {
    refresh(this.filters, this.injector);
  }

  /** Saves a filter of the person's, with a key of its own (docs/adr/0045). */
  async create(
    name: string,
    parameters: SavedFilterParameters,
    shared: boolean,
  ): Promise<SavedFilter> {
    const filter = await this.api.invoke(createSavedFilter, {
      tenant: this.session.tenant() as string,
      'Idempotency-Key': crypto.randomUUID(),
      body: { name, parameters, shared },
    });
    refresh(this.filters, this.injector);
    return filter;
  }

  /** Changes the person's filter over the version it was read in (docs/adr/0050 D3). */
  async update(filter: SavedFilter, patch: SavedFilterPatch): Promise<SavedFilter> {
    const changed = await this.api.invoke(updateSavedFilter, {
      tenant: this.session.tenant() as string,
      filter: filter.id,
      'If-Match': etagOf(filter.version),
      body: patch,
    });
    refresh(this.filters, this.injector);
    return changed;
  }

  async remove(filter: SavedFilter): Promise<void> {
    await this.api.invoke(deleteSavedFilter, {
      tenant: this.session.tenant() as string,
      filter: filter.id,
    });
    refresh(this.filters, this.injector);
  }
}
