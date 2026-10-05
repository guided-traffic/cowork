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
  getTicketBody,
  listActivity,
  listAttachments,
  listComments,
  listInterest,
  listPrerequisites,
  listQuestions,
  listTicketLinks,
  listTicketTime,
} from '../../api/functions';
import {
  ActivityList,
  AttachmentList,
  CommentList,
  InterestList,
  LinkList,
  PrerequisiteTree,
  QuestionList,
  TicketBody as RenderedBody,
  TimeEntryList,
} from '../../api/models';
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

/** Which way the prerequisite tree is read: what blocks the ticket, or what it blocks. */
export type TreeDirection = 'down' | 'up';

/**
 * What surrounds a ticket on its detail page — the rendered body, comments, activity, questions,
 * links, interest, attachments, time, the prerequisite tree — loaded through the API and reloaded
 * when the event stream names the ticket (docs/adr/0054 D2): an event says which part changed, and
 * only that part and the activity are fetched again; an upload is a `ticket.changed`. Time entries
 * are not published (D4): the page that books reloads them, and `resync` and `poll` do. The tree
 * loads again on a link of the ticket and on any change of a ticket it shows. The rendered body
 * loads again when the ticket's version moves ({@link version}, which the page sets from the
 * cache), and on a `ticket.changed` that moves no version, an upload — whose image the body may
 * show (docs/adr/0016 D7). A part that is loading when its event arrives loads once more afterwards
 * (`refresh`), and a part that loads again and fails keeps what it shows ({@link keepShown}).
 * Provided by the page, so it lives exactly as long as the page.
 */
@Injectable()
export class TicketRelations {
  private readonly api = inject(Api);
  private readonly injector = inject(Injector);
  readonly at = signal<TicketAddress | undefined>(undefined);
  /** The version of the ticket the page shows, from the cache; the rendered body follows it. */
  readonly version = signal<number | undefined>(undefined);

  /** The body as the server rendered and sanitised it (docs/adr/0011 D6). */
  readonly body: ResourceRef<RenderedBody | undefined> = resource({
    params: () => {
      const at = this.at();
      const version = this.version();
      return at && version !== undefined ? { ...at, version } : undefined;
    },
    loader: ({ params: { tenant, project, number } }) =>
      keepShown(this.body, () => this.api.invoke(getTicketBody, { tenant, project, number })),
  });

  readonly comments: ResourceRef<CommentList | undefined> = resource({
    params: () => this.at(),
    loader: ({ params }) =>
      keepShown(this.comments, () => this.api.invoke(listComments, { ...params, limit: 200 })),
  });
  readonly activity: ResourceRef<ActivityList | undefined> = resource({
    params: () => this.at(),
    loader: ({ params }) =>
      keepShown(this.activity, () =>
        this.api.invoke(listActivity, { ...params, order: 'desc', limit: 100 }),
      ),
  });
  readonly questions: ResourceRef<QuestionList | undefined> = resource({
    params: () => this.at(),
    loader: ({ params }) =>
      keepShown(this.questions, () => this.api.invoke(listQuestions, { ...params, limit: 200 })),
  });
  readonly links: ResourceRef<LinkList | undefined> = resource({
    params: () => this.at(),
    loader: ({ params }) =>
      keepShown(this.links, () => this.api.invoke(listTicketLinks, { ...params, limit: 200 })),
  });
  readonly interest: ResourceRef<InterestList | undefined> = resource({
    params: () => this.at(),
    loader: ({ params }) =>
      keepShown(this.interest, () => this.api.invoke(listInterest, { ...params, limit: 200 })),
  });
  readonly attachments: ResourceRef<AttachmentList | undefined> = resource({
    params: () => this.at(),
    loader: ({ params }) =>
      keepShown(this.attachments, () =>
        this.api.invoke(listAttachments, { ...params, limit: 200 }),
      ),
  });
  readonly time: ResourceRef<TimeEntryList | undefined> = resource({
    params: () => this.at(),
    loader: ({ params }) =>
      keepShown(this.time, () => this.api.invoke(listTicketTime, { ...params, limit: 200 })),
  });
  /** The prerequisites, or read upward the dependents (docs/adr/0012 D6). */
  readonly direction = signal<TreeDirection>('down');
  readonly tree: ResourceRef<PrerequisiteTree | undefined> = resource({
    params: () => {
      const at = this.at();
      return at ? { ...at, direction: this.direction() } : undefined;
    },
    loader: ({ params }) =>
      keepShown(this.tree, () => this.api.invoke(listPrerequisites, { ...params, limit: 200 })),
  });

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
        this.body,
        this.comments,
        this.questions,
        this.links,
        this.interest,
        this.attachments,
        this.time,
        this.tree,
        this.activity,
      ]) {
        refresh(part, this.injector);
      }
      return;
    }
    if (event.name === 'membership.changed' || event.name === 'inbox.changed') {
      return;
    }
    if (event.key !== `${at.tenant}/${at.project}-${at.number}`) {
      // A ticket of the tree moved, or was linked to another: the tree may show it otherwise.
      if (
        (event.name === 'ticket.changed' || event.name === 'link.changed') &&
        this.tree.hasValue() &&
        this.tree.value().items.some((node) => node.key === event.key)
      ) {
        refresh(this.tree, this.injector);
      }
      return;
    }
    if (event.name === 'comment.changed') {
      refresh(this.comments, this.injector);
    } else if (event.name === 'question.changed') {
      refresh(this.questions, this.injector);
    } else if (event.name === 'link.changed') {
      refresh(this.links, this.injector);
      refresh(this.tree, this.injector);
    } else if (event.name === 'interest.changed') {
      refresh(this.interest, this.injector);
    } else {
      refresh(this.attachments, this.injector);
      // A change that moves the version reloads the body through the version the page sets.
      if (this.body.hasValue() && event.version <= (this.body.value()?.version ?? 0)) {
        refresh(this.body, this.injector);
      }
    }
    refresh(this.activity, this.injector);
  }

  /** After the person booked or corrected time: no event says so. */
  reloadTime(): void {
    refresh(this.time, this.injector);
  }
}
