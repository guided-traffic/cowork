import {
  DestroyRef,
  inject,
  Injectable,
  Injector,
  linkedSignal,
  resource,
  ResourceRef,
  signal,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Api } from '../../api/api';
import { listAttachments } from '../../api/fn/attachments/list-attachments';
import { listActivity } from '../../api/fn/comments/list-activity';
import { listComments } from '../../api/fn/comments/list-comments';
import { listQuestions } from '../../api/fn/questions/list-questions';
import { getTicketBody } from '../../api/fn/tickets/get-ticket-body';
import { listInterest } from '../../api/fn/tickets/list-interest';
import { listPrerequisiteTree } from '../../api/fn/tickets/list-prerequisite-tree';
import { listTicketRelations } from '../../api/fn/tickets/list-ticket-relations';
import { listTicketTime } from '../../api/fn/time/list-ticket-time';
import {
  PrerequisiteHeadTree,
  Relation,
  RelationList,
  TicketBody as RenderedBody,
} from '../../api/models';
import { ConditionalPages, PageFetcher } from '../../core/conditional';
import { EventStreamService, StreamEvent, TicketEvent } from '../../core/event-stream.service';
import { followPages, PersonPages } from '../../core/inbox.service';
import { keepShown, refresh } from '../../core/refresh';

/** Where a ticket lives: the team, the project's key and the number, as the API names them. */
export interface TicketAddress {
  team: string;
  project: string;
  number: number;
}

/** `acme`, `VKO-12` → the address; undefined for a key that is not `<PROJECT>-<number>`. */
export function address(tenant: string | null, key: string): TicketAddress | undefined {
  const match = /^([A-Z][A-Z0-9]{1,9})-([1-9][0-9]*)$/.exec(key);
  return tenant && match
    ? { team: tenant, project: match[1], number: Number(match[2]) }
    : undefined;
}

/** Which way the prerequisite tree is read: what blocks the ticket, or what it blocks. */
export type TreeDirection = 'down' | 'up';

/** The page size of a ticket's children (docs/adr/0048 D3); *Load more* adds a page. */
export const childPageSize = 50;

/**
 * The acts on another ticket that may make it a child of the ticket shown, or end that: its
 * filing, a change of its fields — its parent among them — and its restoration (docs/adr/0008 D2).
 */
const parentingKinds: ReadonlySet<string> = new Set(['created', 'updated', 'restored']);

/** Whether two loads of the children hold the same items, each the same answer, and cursor. */
function samePages(held: PersonPages<Relation>, loaded: PersonPages<Relation>): boolean {
  return (
    held.nextCursor === loaded.nextCursor &&
    held.items.length === loaded.items.length &&
    held.items.every((item, index) => item === loaded.items[index])
  );
}

/** Whether a list of relations shows the ticket `key` at its other end. */
function shows(part: ResourceRef<{ items: Relation[] } | undefined>, key: string): boolean {
  return part.hasValue() && (part.value()?.items.some((each) => each.head.key === key) ?? false);
}

/**
 * What surrounds a ticket on its detail page — the rendered body, comments, activity, questions,
 * links, children, interest, attachments, time, the prerequisite tree — loaded through the API and
 * reloaded when the event stream names the ticket (docs/adr/0054 D2): an event says which part
 * changed, and only that part and the activity are fetched again; an upload is a `ticket.changed`.
 * Time entries are not published (D4): the page that books reloads them, and `resync` and `poll`
 * do. The links, the children and the tree show the tickets at their other ends of any project or
 * team, each as the reader sees it (docs/adr/0005 D3): the tree loads again on a link of the ticket
 * and on any change of a ticket it shows, the links and the children on a change of a ticket they
 * show, the children as well on an act that may have made a ticket a child of this one, and on the
 * ticket's own `derived` — a child of another team moved its stages (docs/adr/0017 D3). The
 * rendered body loads again when the ticket's version moves ({@link version}, which the page sets
 * from the cache), and on a `ticket.changed` that moves no version, an upload — whose image the
 * body may show (docs/adr/0016 D7). A part that is loading when its event arrives loads once more afterwards
 * (`refresh`), and a part that loads again and fails keeps what it shows ({@link keepShown}). Each
 * list part keeps the weak `ETag` of its last answer and is answered `304` while it is unchanged
 * (docs/adr/0054 D7). Provided by the page, so it lives exactly as long as the page.
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
    loader: ({ params: { team, project, number } }) =>
      keepShown(this.body, () => this.api.invoke(getTicketBody, { team, project, number })),
  });

  readonly comments = this.part((params, page) => page(listComments, { ...params, limit: 200 }));
  readonly activity = this.part((params, page) =>
    page(listActivity, { ...params, order: 'desc', limit: 100 }),
  );
  readonly questions = this.part((params, page) => page(listQuestions, { ...params, limit: 200 }));
  /** The links in both directions, each other end as the reader sees it (docs/adr/0012 D2). */
  readonly links: ResourceRef<RelationList | undefined> = this.part((params, page) =>
    page(listTicketRelations, { ...params, kind: ['link'], limit: 200 }),
  );
  /** How many pages of the children the page holds; another ticket starts at one. */
  readonly childPages = linkedSignal<TicketAddress | undefined, number>({
    source: this.at,
    computation: () => 1,
  });
  private readonly childPagesHeld = new ConditionalPages(this.api);
  /**
   * The children, of any project or team, each as the reader sees it, page after page in the order
   * they were filed (docs/adr/0008 D2): a reload asks for every page held again.
   */
  readonly children: ResourceRef<PersonPages<Relation> | undefined> = resource({
    params: () => {
      const at = this.at();
      return at ? { ...at, pages: this.childPages() } : undefined;
    },
    loader: ({ params: { pages, ...at } }) =>
      keepShown(this.children, async () => {
        const loaded = await this.childPagesHeld.load((page) =>
          followPages(pages, (cursor) =>
            page(listTicketRelations, { ...at, kind: ['child'], cursor, limit: childPageSize }),
          ),
        );
        // Every page answered 304 hands back the items held: the part keeps what it shows.
        const held = this.children.hasValue() ? this.children.value() : undefined;
        return held && samePages(held, loaded) ? held : loaded;
      }),
  });
  readonly interest = this.part((params, page) => page(listInterest, { ...params, limit: 200 }));
  readonly attachments = this.part((params, page) =>
    page(listAttachments, { ...params, limit: 200 }),
  );
  readonly time = this.part((params, page) => page(listTicketTime, { ...params, limit: 200 }));
  /** The prerequisites, or read upward the dependents, across teams (docs/adr/0012 D6). */
  readonly direction = signal<TreeDirection>('down');
  private readonly treePages = new ConditionalPages(this.api);
  readonly tree: ResourceRef<PrerequisiteHeadTree | undefined> = resource({
    params: () => {
      const at = this.at();
      return at ? { ...at, direction: this.direction() } : undefined;
    },
    loader: ({ params }) =>
      keepShown(this.tree, () =>
        this.treePages.load((page) => page(listPrerequisiteTree, { ...params, limit: 200 })),
      ),
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
        this.children,
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
    if (
      event.name === 'membership.changed' ||
      event.name === 'inbox.changed' ||
      event.name === 'project.changed'
    ) {
      return;
    }
    if (event.key !== `${at.team}/${at.project}-${at.number}`) {
      this.reactToAnother(event);
      return;
    }
    if (event.name === 'ticket.changed' && event.kind === 'derived') {
      // A child of another team moved the ticket's stages (docs/adr/0017 D3), which the cache
      // fetches again; the child's head may have changed with it. No act: the activity stays.
      refresh(this.children, this.injector);
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

  /**
   * An event of another ticket — of any team of the person, whose events the person-level stream
   * carries (docs/adr/0054 D1): the tree loads again where it shows that ticket, which moved or was
   * linked to another; the links and the children where they show it, its head having changed; and
   * the children on an act that may have made it a child of this ticket or ended that.
   */
  private reactToAnother(event: TicketEvent): void {
    const changed = event.name === 'ticket.changed';
    if (
      (changed || event.name === 'link.changed') &&
      this.tree.hasValue() &&
      this.tree.value().items.some((node) => node.head.key === event.key)
    ) {
      refresh(this.tree, this.injector);
    }
    if (!changed) {
      return;
    }
    if (shows(this.links, event.key)) {
      refresh(this.links, this.injector);
    }
    if (parentingKinds.has(event.kind) || shows(this.children, event.key)) {
      refresh(this.children, this.injector);
    }
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
