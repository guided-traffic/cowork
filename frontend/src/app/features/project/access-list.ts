import {
  computed,
  DestroyRef,
  inject,
  Injectable,
  Injector,
  resource,
  ResourceRef,
  signal,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../../api/api';
import { listProjectAccess } from '../../api/fn/projects/list-project-access';
import { removeProjectAccess } from '../../api/fn/projects/remove-project-access';
import { setProjectAccess } from '../../api/fn/projects/set-project-access';
import { ProjectAccessEntry, ProjectAccessRole } from '../../api/models';
import { ConditionalPages } from '../../core/conditional';
import { changesMemberships, EventStreamService } from '../../core/event-stream.service';
import { keepShown, refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';

/** Where an access list is: the team and the project's key, as the API names them. */
interface Place {
  team: string;
  project: string;
}

/**
 * The access list of a project (docs/adr/0034 D3): the people a restricted project admits besides
 * the tenant's administrators, each with the most they may do in it. Only the administrators read
 * it, so it is not asked for anybody else: it would be a `403`. The list may be written before the
 * project is restricted, and counts while it is. Provided by the project's settings, so it lives
 * exactly as long as the page; `membership.changed`, a resync and the fallback's poll load it
 * again (docs/adr/0054), and a load again that fails keeps the list shown ({@link keepShown}).
 */
@Injectable()
export class AccessList {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

  /** The key of the project the page shows. */
  readonly project = signal<string | undefined>(undefined);

  /**
   * The place of the list while the person administers the tenant, and nothing else. Equal places
   * are one value, so that the person loaded again with the same role leaves the list alone.
   */
  private readonly place = computed<Place | undefined>(
    () => {
      const tenant = this.session.tenant();
      const project = this.project();
      return tenant !== null && project && this.session.membership()?.role === 'admin'
        ? { team: tenant, project }
        : undefined;
    },
    { equal: (a, b) => a?.team === b?.team && a?.project === b?.project },
  );

  private readonly pages = new ConditionalPages(this.api);

  readonly entries: ResourceRef<ProjectAccessEntry[] | undefined> = resource({
    params: () => this.place(),
    loader: ({ params }) =>
      keepShown(this.entries, () =>
        this.pages.load(async (page) => {
          const entries: ProjectAccessEntry[] = [];
          let cursor: string | undefined;
          do {
            const next = await page(listProjectAccess, { ...params, cursor, limit: 200 });
            entries.push(...next.items);
            cursor = next.next_cursor ?? undefined;
          } while (cursor);
          return entries;
        }),
      ),
  });

  readonly list = computed<ProjectAccessEntry[]>(() =>
    this.entries.hasValue() ? this.entries.value() : [],
  );

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed(inject(DestroyRef)))
      .subscribe((event) => {
        if (changesMemberships(event, this.session.tenant())) {
          refresh(this.entries, this.injector);
        }
      });
  }

  /** Puts a member of the tenant on the list, or changes their entry. */
  async set(personId: string, role: ProjectAccessRole): Promise<ProjectAccessEntry> {
    const place = this.place() as Place;
    const entry = await this.api.invoke(setProjectAccess, {
      ...place,
      person_id: personId,
      body: { role },
    });
    // The entry as the answer has it, at once: a select that shows the new role must not jump
    // back while the list loads. An answer for a list the page no longer shows is left out.
    if (this.place() === place && this.entries.hasValue()) {
      const held = this.entries.value();
      const at = held.findIndex((each) => each.person.id === personId);
      this.entries.set(
        at === -1 ? [...held, entry] : held.map((each, index) => (index === at ? entry : each)),
      );
    }
    refresh(this.entries, this.injector);
    return entry;
  }

  /**
   * Takes a person off the list, and the entry out of the list at once: its row must not take
   * another act while the list loads. An answer for a list the page no longer shows is left out.
   */
  async remove(personId: string): Promise<void> {
    const place = this.place() as Place;
    await this.api.invoke(removeProjectAccess, { ...place, person_id: personId });
    if (this.place() === place && this.entries.hasValue()) {
      this.entries.set(this.entries.value().filter((each) => each.person.id !== personId));
    }
    refresh(this.entries, this.injector);
  }
}
