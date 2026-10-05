import { HttpErrorResponse } from '@angular/common/http';
import { Api, ApiFnOptional, ApiFnRequired } from '../api/api';

/** A page as the list's last load received it, with the weak `ETag` the server gave it. */
interface HeldPage {
  etag: string;
  body: unknown;
}

/** The parameters of a list request that may carry the conditional header. */
interface Conditional {
  'If-None-Match'?: string;
}

/**
 * Asks for one page of a list; the request's parameters name the page. A list whose parameters are
 * all optional, such as the person's, has a function that takes none, which is the same call here.
 */
export type PageFetcher = <P extends Conditional, R>(
  fn: ApiFnRequired<P, R> | ApiFnOptional<P, R>,
  params: P,
) => Promise<R>;

/**
 * The weak `ETag`s of the pages a list holds (docs/adr/0054 D7), one instance per list: a load runs
 * its requests through `load`, which sends `If-None-Match` with the tag of the same request's last
 * answer and keeps that answer on a `304` — a poll or a reload that finds nothing new moves no list.
 * A page is the request that fetched it, so a list followed cursor by cursor sends the tag of each
 * page with the same cursor; the pages a load no longer asks for are forgotten once it ends, and a
 * load that fails keeps the pages held before it.
 */
export class ConditionalPages {
  private held = new Map<string, HeldPage>();

  constructor(private readonly api: Api) {}

  /** Runs one load of the list, whose requests go through the fetcher it is handed. */
  async load<T>(work: (page: PageFetcher) => Promise<T>): Promise<T> {
    const used = new Map<string, HeldPage>();
    const result = await work((fn, params) => this.page(fn, params, used));
    this.held = used;
    return result;
  }

  private async page<P extends Conditional, R>(
    fn: ApiFnRequired<P, R> | ApiFnOptional<P, R>,
    params: P,
    used: Map<string, HeldPage>,
  ): Promise<R> {
    const key = requestKey(params);
    const known = this.held.get(key);
    try {
      const answer = await this.api.invoke$Response(
        fn as ApiFnRequired<P, R>,
        known ? { ...params, 'If-None-Match': known.etag } : params,
      );
      const etag = answer.headers.get('ETag');
      if (etag) {
        used.set(key, { etag, body: answer.body });
      }
      return answer.body;
    } catch (error) {
      if (known && error instanceof HttpErrorResponse && error.status === 304) {
        used.set(key, known);
        return known.body as R;
      }
      throw error;
    }
  }
}

/** The parameters of a request, the conditional header left out, as one string. */
function requestKey(params: Conditional): string {
  const rest: Record<string, unknown> = { ...params };
  delete rest['If-None-Match'];
  return JSON.stringify(rest);
}
