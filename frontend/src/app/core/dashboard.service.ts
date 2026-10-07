import { DestroyRef, inject, Injectable, Injector, resource, ResourceRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../api/api';
import { getDashboard } from '../api/fn/dashboard/get-dashboard';
import { Dashboard } from '../api/models';
import { ConditionalPages } from './conditional';
import {
  changesMemberships,
  changesVisibility,
  EventStreamService,
  isImport,
  ofTenant,
  StreamEvent,
} from './event-stream.service';
import { keepShown, refresh } from './refresh';
import { SessionService } from './session.service';

/** What a dashboard is read for: the tenant, the `project` filter as the address holds it, the period. */
export interface DashboardQuery {
  tenant: string;
  project: string[];
  from: string;
  to: string;
}

/**
 * How long the dashboard waits after the first event of a burst before it loads again: at most one
 * reload a second, however many acts an agent makes, and never more than a second behind.
 */
export const dashboardReloadDelay = 1000;

/**
 * Whether an event may change a tile of the dashboard of the tenant shown (docs/adr/0018 D6): an act
 * on one of its tickets — its state, its fields, its filing, its deletion and restoration — or on a
 * question of one ({@link ofTenant}); a change of who sees a project of it, the person's own role
 * among them ({@link changesMemberships}, {@link changesVisibility}); a gap in the stream or the
 * fallback's tick; an import executed into one of its projects, whose tickets and questions come
 * without an event of their own ({@link isImport}). The person-level stream carries every tenant of
 * the person (docs/adr/0054 D1): another tenant's event changes nothing here. A comment, a stake, a
 * link and a sort of a rank change no tile, and time entries are not published (D4): a booking
 * shows at the next reload.
 */
export function changesDashboard(
  event: StreamEvent,
  tenant: string | null,
  person: string | undefined,
): boolean {
  switch (event.name) {
    case 'membership.changed':
      return changesMemberships(event, tenant) && changesVisibility(event, person);
    case 'ticket.changed':
    case 'question.changed':
    case 'resync':
    case 'poll':
      return ofTenant(event, tenant);
    case 'project.changed':
      return isImport(event) && ofTenant(event, tenant);
    default:
      return false;
  }
}

/**
 * The tenant's dashboard (docs/adr/0018 D6): one request for the nine tiles, which the page reads
 * through a resource of its own. A dashboard loads again, at most once a second, when an event may
 * have changed what it counts ({@link changesDashboard}); the request carries the weak `ETag` of the
 * last answer, so a reload that finds nothing new costs a `304` (docs/adr/0054 D7), and a reload
 * that fails keeps what is shown ({@link keepShown}).
 */
@Injectable({ providedIn: 'root' })
export class DashboardService {
  private readonly api = inject(Api);
  private readonly session = inject(SessionService);
  /** The dashboards shown, each with the injector of its view. */
  private readonly shown = new Map<ResourceRef<Dashboard | undefined>, Injector>();
  private reloadTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => this.react(event));
  }

  /** A dashboard, for the component that creates it in its injection context. */
  dashboard(
    params: () => DashboardQuery | undefined,
    injector = inject(Injector),
  ): ResourceRef<Dashboard | undefined> {
    const pages = new ConditionalPages(this.api);
    const ref: ResourceRef<Dashboard | undefined> = resource({
      params,
      loader: ({ params }) =>
        keepShown(ref, () => pages.load((page) => page(getDashboard, params))),
      injector,
    });
    this.shown.set(ref, injector);
    injector.get(DestroyRef).onDestroy(() => this.shown.delete(ref));
    return ref;
  }

  private react(event: StreamEvent): void {
    if (
      this.reloadTimer ||
      !changesDashboard(event, this.session.tenant(), this.session.person()?.id)
    ) {
      return;
    }
    this.reloadTimer = setTimeout(() => {
      this.reloadTimer = null;
      for (const [ref, injector] of this.shown) {
        refresh(ref, injector);
      }
    }, dashboardReloadDelay);
  }
}
