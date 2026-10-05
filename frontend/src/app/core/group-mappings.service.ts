import { computed, inject, Injectable, Injector, resource, ResourceRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import {
  createGroupMapping,
  deleteGroupMapping,
  listGroupMappings,
  updateGroupMapping,
} from '../api/functions';
import { GroupMapping, Role } from '../api/models';
import { ConditionalPages } from './conditional';
import { etagOf } from './entity-cache';
import { changesMemberships, EventStreamService } from './event-stream.service';
import { MembersService } from './members.service';
import { keepShown, refresh } from './refresh';
import { SessionService } from './session.service';

/**
 * The group mappings of the tenant the pages show (docs/adr/0030 D2, D7): each gives a role in the
 * tenant to everyone whose identity-provider groups include its group. Only the tenant's
 * administrators read them, and a global administrator who holds no role in the tenant, so the list
 * is not asked for anybody else: it would be a `403`. A
 * mapping's change re-derives the memberships of its group at once, so every act loads the members
 * again, and the person's own memberships where the mapping is theirs; `membership.changed`, a
 * resync and the fallback's poll load the list again (docs/adr/0054), and a load again that fails
 * keeps the list shown ({@link keepShown}).
 */
@Injectable({ providedIn: 'root' })
export class GroupMappingsService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

  /**
   * The tenant of the page while the person administers it, or oversees it as a global
   * administrator without a role there (docs/adr/0034 D2), and nothing else. A computed, so that the
   * person loaded again with the same role leaves the list alone.
   */
  private readonly administered = computed(() => {
    const tenant = this.session.tenant();
    const reads = this.session.membership()?.role === 'admin' || this.session.oversight();
    return tenant !== null && reads ? tenant : undefined;
  });

  private readonly pages = new ConditionalPages(this.api);

  readonly mappings: ResourceRef<GroupMapping[] | undefined> = resource({
    params: () => this.administered(),
    loader: ({ params: tenant }) =>
      keepShown(this.mappings, () =>
        this.pages.load(async (page) => {
          const mappings: GroupMapping[] = [];
          let cursor: string | undefined;
          do {
            const next = await page(listGroupMappings, { tenant, cursor, limit: 200 });
            mappings.push(...next.items);
            cursor = next.next_cursor ?? undefined;
          } while (cursor);
          return mappings;
        }),
      ),
  });

  readonly list = computed<GroupMapping[]>(() =>
    this.mappings.hasValue() ? this.mappings.value() : [],
  );

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (changesMemberships(event)) {
          refresh(this.mappings, this.injector);
        }
      });
  }

  /**
   * Maps a group to a role. The key is the form's, one for each content it holds, so a retry of a
   * lost answer is answered again instead of being refused as a mapping that exists
   * (docs/adr/0045).
   */
  async create(group: string, role: Role, idempotencyKey: string): Promise<GroupMapping> {
    const tenant = this.session.tenant() as string;
    const mapping = await this.api.invoke(createGroupMapping, {
      tenant,
      'Idempotency-Key': idempotencyKey,
      body: { group, role },
    });
    this.changed(tenant, mapping);
    return mapping;
  }

  /** Changes a mapping's role over the version that was read (docs/adr/0050 D3). */
  async changeRole(mapping: GroupMapping, role: Role): Promise<GroupMapping> {
    const tenant = this.session.tenant() as string;
    const changed = await this.api.invoke(updateGroupMapping, {
      tenant,
      mapping_id: mapping.id,
      'If-Match': etagOf(mapping.version),
      body: { role },
    });
    this.changed(tenant, changed);
    return changed;
  }

  /** Removes a mapping: the memberships it gave go, or fall to the person's next mapped group. */
  async remove(mapping: GroupMapping): Promise<void> {
    await this.api.invoke(deleteGroupMapping, {
      tenant: this.session.tenant() as string,
      mapping_id: mapping.id,
    });
    this.reload(mapping);
  }

  /**
   * Puts the mapping as the answer has it into the list at once — a select that shows the new
   * role must not jump back while the list loads — and loads what the mapping touches again. An
   * answer that arrives after the pages turned to another tenant is not put into that tenant's
   * list.
   */
  private changed(tenant: string, mapping: GroupMapping): void {
    if (this.session.tenant() === tenant && this.mappings.hasValue()) {
      const held = this.mappings.value();
      const at = held.findIndex((each) => each.id === mapping.id);
      this.mappings.set(
        at === -1 ? [...held, mapping] : held.map((each, index) => (index === at ? mapping : each)),
      );
    }
    this.reload(mapping);
  }

  /**
   * The mappings, the members the mapping derives, and the person's own memberships when the
   * mapping is theirs. `MembersService` is asked for only now, so that the page of the mappings
   * does not start a load of the members by being open.
   */
  private reload(mapping: GroupMapping): void {
    refresh(this.mappings, this.injector);
    refresh(this.injector.get(MembersService).members, this.injector);
    if (mapping.includes_caller) {
      refresh(this.session.me, this.injector);
    }
  }
}
