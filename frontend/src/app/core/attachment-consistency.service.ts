import { computed, inject, Injectable, Injector, resource, ResourceRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import { acceptDanglingAttachments } from '../api/fn/attachments/accept-dangling-attachments';
import { getAttachmentConsistency } from '../api/fn/attachments/get-attachment-consistency';
import { removeOrphanedObjects } from '../api/fn/attachments/remove-orphaned-objects';
import { AttachmentConsistency, DanglingAcceptance, OrphanRemoval } from '../api/models';
import { ConditionalPages } from './conditional';
import { EventStreamService } from './event-stream.service';
import { keepShown, refresh } from './refresh';
import { SessionService } from './session.service';

/**
 * The tenant's latest consistency check of its attachments against the bucket (docs/adr/0059 D4),
 * and its administrators' two acts on it: accepting the loss of the files whose bytes are missing,
 * and removing the objects no file names. Only a tenant administrator reads the check, so it is not
 * asked for anybody else. The answer carries the time of the tenant's last export beside it. The
 * check runs once a day and is on no event stream: it is read when the page opens, after either act
 * and after an export on the settings page, and again on `resync` and `poll`, a `304` when nothing
 * changed.
 */
@Injectable({ providedIn: 'root' })
export class AttachmentConsistencyService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  private readonly injector = inject(Injector);

  /** The tenant of the page while the person administers it. */
  private readonly administered = computed(() => {
    const tenant = this.session.tenant();
    return tenant !== null && this.session.membership()?.role === 'admin' ? tenant : undefined;
  });

  /** The weak `ETag` of the result held: a load again that finds it unchanged is a `304`. */
  private readonly conditional = new ConditionalPages(this.api);

  readonly latest: ResourceRef<AttachmentConsistency | undefined> = resource({
    params: () => this.administered(),
    loader: ({ params: tenant }) =>
      keepShown(this.latest, () =>
        this.conditional.load((fetch) => fetch(getAttachmentConsistency, { tenant })),
      ),
  });

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (event.name === 'resync' || event.name === 'poll') {
          refresh(this.latest, this.injector);
        }
      });
  }

  /**
   * Reads the check again after the person exported the tenant on its settings page: the answer
   * carries the time of the tenant's last export (docs/adr/0059 D2).
   */
  exported(): void {
    refresh(this.latest, this.injector);
  }

  /**
   * Accepts the loss of the missing files the check `check` listed (docs/adr/0059 D5): they count
   * as accepted from then on, and nothing is removed.
   */
  async acceptLoss(check: string): Promise<DanglingAcceptance> {
    const done = await this.api.invoke(acceptDanglingAttachments, {
      tenant: this.session.tenant() as string,
      body: { check_id: check },
    });
    refresh(this.latest, this.injector);
    return done;
  }

  /**
   * Removes the objects no file names that the check `check` listed — for good; the browser
   * session the request goes in is what the API asks of it (docs/adr/0035 D5).
   */
  async removeOrphans(check: string): Promise<OrphanRemoval> {
    const done = await this.api.invoke(removeOrphanedObjects, {
      tenant: this.session.tenant() as string,
      body: { check_id: check },
    });
    refresh(this.latest, this.injector);
    return done;
  }
}
