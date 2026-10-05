import {
  computed,
  effect,
  linkedSignal,
  resource,
  ResourceRef,
  signal,
  untracked,
} from '@angular/core';
import type { PaginatorState } from 'primeng/types/paginator';
import { keepShown } from './refresh';

/** The sizes of a numbered page (docs/adr/0048 D2). */
export type PerPage = 25 | 50 | 100;

/** The sizes a table offers, smallest first. */
export const perPageOptions: PerPage[] = [25, 50, 100];

/** A numbered page as the API answers it: the rows and the total under the same filters. */
export interface NumberedPage<T> {
  items: T[];
  total?: number;
}

/**
 * A table read in numbered pages (docs/adr/0048 D2, D4), the way the audit page reads its record:
 * the page and its size as signals, a change of the request or of the size going back to the first
 * page, the total kept while the next page loads — so that the pages, and the page button that may
 * hold the focus, stay where they are — and a page past the end, after rows went away, moving back
 * to the last one. A load again that fails keeps the page shown ({@link keepShown}). Call it in an
 * injection context; `request` answers undefined while the table asks for nothing.
 */
export function tablePages<R, T>(
  request: () => R | undefined,
  load: (request: R, page: number, perPage: PerPage) => Promise<NumberedPage<T>>,
) {
  const perPage = signal<PerPage>(25);
  const asked = computed(() => request(), {
    equal: (a, b) => JSON.stringify(a) === JSON.stringify(b),
  });
  const page = linkedSignal<{ request?: R; perPage: PerPage }, number>({
    source: () => ({ request: asked(), perPage: perPage() }),
    computation: () => 1,
  });
  const rows: ResourceRef<NumberedPage<T> | undefined> = resource({
    params: () => {
      const r = asked();
      return r === undefined ? undefined : { request: r, page: page(), perPage: perPage() };
    },
    loader: ({ params }): Promise<NumberedPage<T>> =>
      keepShown(rows, () => load(params.request, params.page, params.perPage)),
  });
  const items = computed<T[]>(() => (rows.hasValue() ? rows.value().items : []));
  const total = linkedSignal<NumberedPage<T> | undefined, number>({
    source: () => (rows.hasValue() ? rows.value() : undefined),
    computation: (list, previous) =>
      list ? (list.total ?? 0) : rows.isLoading() ? (previous?.value ?? 0) : 0,
  });
  effect(() => {
    const shown = items().length;
    const all = total();
    const at = page();
    if (rows.hasValue() && !rows.isLoading() && shown === 0 && all > 0 && at > 1) {
      untracked(() => page.set(Math.max(1, Math.ceil(all / perPage()))));
    }
  });
  return {
    perPage,
    page,
    rows,
    items,
    total,
    /** The paginator's event: another size starts at the first page, another page is shown. */
    turn(state: PaginatorState): void {
      const size = (state.rows ?? perPage()) as PerPage;
      if (size !== perPage()) {
        perPage.set(size);
        return;
      }
      page.set((state.page ?? 0) + 1);
    },
  };
}
