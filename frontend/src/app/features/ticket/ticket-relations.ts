import {
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
import {
  listActivity,
  listAttachments,
  listComments,
  listInterest,
  listQuestions,
  listTicketLinks,
  listTicketTime,
} from '../../api/functions';
import { ConditionalPages, PageFetcher } from '../../core/conditional';
import { EventStreamService, StreamEvent } from '../../core/event-stream.service';
import { keepShown, refresh } from '../../core/refresh';

/** Where a ticket lives: the tenant, the project's key and the number. */
export interface TicketAddress {
  tenant: string;
  project: string;
  number: number;
}

/** `acme`, `VKO-12` → the address; undefined for a key that is not `<PROJECT>-<number>`. */
export function address(tenant: string | null, key: string): TicketAddress | undefined {
  const match = /^([A-Z][A-Z0-9]{1,9})-([1-9][0-9]*)$/.exec(key);
  return tenant && match ? { tenant, project: match[1], number: Number(match[2]) } : undefined;
}

/**
 * What surrounds a ticket on its detail page — comments, activity, questions, links, interest,
 * attachments, time — loaded through the API and reloaded when the event stream names the ticket
 * (docs/adr/0054 D2): an event says which part changed, and only that part and the activity are
 * fetched again; an upload is a `ticket.changed`. Time entries are not published (D4): the page
 * that books reloads them, and `resync` and `poll` do. A part that is loading when its event
 * arrives loads once more afterwards (`refresh`), and a part that loads again and fails keeps
 * what it shows ({@link keepShown}). Provided by the page, so it lives exactly as long as the page.
 */
@Injectable()
export class TicketRelations {
  private readonly api = inject(Api);
  private readonly injector = inject(Injector);
  readonly at = signal<TicketAddress | undefined>(undefined);

  readonly comments = this.part((params, page) => page(listComments, { ...params, limit: 200 }));
  readonly activity = this.part((params, page) =>
    page(listActivity, { ...params, order: 'desc', limit: 100 }),
  );
  readonly questions = this.part((params, page) => page(listQuestions, { ...params, limit: 200 }));
  readonly links = this.part((params, page) => page(listTicketLinks, { ...params, limit: 200 }));
  readonly interest = this.part((params, page) => page(listInterest, { ...params, limit: 200 }));
  readonly attachments = this.part((params, page) =>
    page(listAttachments, { ...params, limit: 200 }),
  );
  readonly time = this.part((params, page) => page(listTicketTime, { ...params, limit: 200 }));

  constructor() {
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed(inject(DestroyRef)))
      .subscribe((event) => this.react(event));
  }

  private react(event: StreamEvent): void {
    const at = this.at();
    if (!at) {
      return;
    }
    if (event.name === 'resync' || event.name === 'poll') {
      for (const part of [
        this.comments,
        this.questions,
        this.links,
        this.interest,
        this.attachments,
        this.time,
        this.activity,
      ]) {
        refresh(part, this.injector);
      }
      return;
    }
    if (
      event.name === 'membership.changed' ||
      event.key !== `${at.tenant}/${at.project}-${at.number}`
    ) {
      return;
    }
    if (event.name === 'comment.changed') {
      refresh(this.comments, this.injector);
    } else if (event.name === 'question.changed') {
      refresh(this.questions, this.injector);
    } else if (event.name === 'link.changed') {
      refresh(this.links, this.injector);
    } else if (event.name === 'interest.changed') {
      refresh(this.interest, this.injector);
    } else {
      refresh(this.attachments, this.injector);
    }
    refresh(this.activity, this.injector);
  }

  /**
   * One part of the page: a list of the ticket the page shows, which keeps the weak `ETag` of its
   * last answer and is answered `304` while it is unchanged (docs/adr/0054 D7).
   */
  private part<T>(
    load: (params: TicketAddress, page: PageFetcher) => Promise<T>,
  ): ResourceRef<T | undefined> {
    const pages = new ConditionalPages(this.api);
    const ref: ResourceRef<T | undefined> = resource({
      params: () => this.at(),
      loader: ({ params }) => keepShown(ref, () => pages.load((page) => load(params, page))),
      injector: this.injector,
    });
    return ref;
  }

  /** After the person booked or corrected time: no event says so. */
  reloadTime(): void {
    refresh(this.time, this.injector);
  }
}
