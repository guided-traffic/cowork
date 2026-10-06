import { linkedSignal, ResourceRef, Signal } from '@angular/core';
import { TicketPage, TicketsService } from '../../core/tickets.service';
import { horizonsOf } from './board-model';
import { BoardExtra } from './saved-filter-model';

/** A board shows every card of the current work: its list follows the cursor to the end. */
const everyPage = Number.POSITIVE_INFINITY;

export interface BoardList {
  /** The list of the service, which the event stream reloads. */
  list: ResourceRef<TicketPage | undefined>;
  /** The keys it answered last for the project, kept while it loads again; none before the first answer. */
  keys: Signal<readonly string[] | undefined>;
}

/**
 * The tickets of a project's board (docs/adr/0018 D1, D4): the project's open tickets of the
 * horizons the board shows, every page of them, in the project's rank, asked for while `tenant`
 * names one — and what the list answered last for the project, kept while it loads again or while
 * it is not asked for. The project's board and each swimlane of the tenant's board read their cards
 * from it; a swimlane narrows it by the conditions of the saved filter the tenant's board applies
 * (`filter`, docs/adr/0049 D6). Call it in an injection context.
 */
export function boardList(
  tickets: TicketsService,
  project: () => string,
  tenant: () => string | null | undefined,
  filter: () => BoardExtra = () => ({}),
): BoardList {
  const list = tickets.projectTicketPages(() => {
    const at = tenant();
    if (!at) {
      return undefined;
    }
    const { horizon, ...rest } = filter();
    return {
      ...rest,
      tenant: at,
      project: project(),
      pages: everyPage,
      horizon: horizonsOf(horizon),
    };
  });
  const keys = linkedSignal<
    { project: string; page: TicketPage | undefined },
    readonly string[] | undefined
  >({
    source: () => ({ project: project(), page: list.hasValue() ? list.value() : undefined }),
    computation: (source, previous) =>
      source.page?.keys ??
      (previous?.source.project === source.project ? previous.value : undefined),
  });
  return { list, keys };
}
