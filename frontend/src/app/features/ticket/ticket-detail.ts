import { HttpErrorResponse } from '@angular/common/http';
import {
  afterNextRender,
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  ElementRef,
  inject,
  Injector,
  input,
  untracked,
} from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { ConfirmationService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Skeleton } from 'primeng/skeleton';
import { Tooltip } from 'primeng/tooltip';
import { Activity, Attachment, Interest } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { ProblemService, ProblemView } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TicketsService } from '../../core/tickets.service';
import { AgentMark } from '../../shared/agent-mark';
import { SecurityBadge, SeverityBadge, StateBadge, TypeIcon } from '../../shared/badges';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { RenderedText } from '../../shared/rendered-text';
import { HeadKey } from '../../shared/ticket-head';
import { ago, Clock, dateTime } from '../../shared/time';
import { CommentItem } from './comment-item';
import {
  AnswerQuestion,
  AskQuestion,
  CommentComposer,
  EditQuestion,
  LinkAdder,
} from './conversation-forms';
import { InterestControl } from './interest-control';
import { PrerequisiteTree } from './prerequisite-tree';
import { AttachmentsCard, TimeCard } from './records-cards';
import { TicketBody } from './ticket-body';
import { TicketDelete } from './ticket-delete';
import { TicketFields } from './ticket-fields';
import { TicketMoves } from './ticket-moves';
import { address, TicketRelations } from './ticket-relations';
import { TicketTitle } from './ticket-title';

/**
 * `transitioned` by Ada, with the agent that acted for her when there was one. The sort of the
 * project's rank by the score is the project's act, which the activity of every ticket it moved
 * shows (docs/adr/0014 D3, docs/adr/0015 D1). An act on the horizon is recorded as `overridden`,
 * its name before (docs/adr/0010 D1), and reads as what it did: a horizon set, or the ticket
 * returned to `later`. A prerequisite of another team that settled is named by no key, as its act
 * names none (docs/adr/0012 D5); a child detached by its key where the reader sees it.
 */
export function describe(activity: Activity): string {
  const who = activity.actor?.display_name ?? activity.actor_system ?? 'cowork';
  if (activity.entity_type === 'project' && activity.action === 'ranked') {
    return `${who} sorted the backlog by score`;
  }
  if (activity.action === 'prerequisite_settled') {
    // docs/adr/0012 D5: a ticket of another team that blocks this one reached done or dropped; the
    // act names it in its refs alone, so no reader of this team's record learns which.
    return `${who} closed a ticket of another team that blocks it`;
  }
  if (activity.action === 'detached') {
    // docs/adr/0008 D2: a child left the ticket; its key where the reader sees the child.
    const child = (activity.before as Record<string, unknown> | null)?.['child'];
    return typeof child === 'string'
      ? `${who} detached the child ${child.slice(child.indexOf('/') + 1)}`
      : `${who} detached a child`;
  }
  if (activity.action === 'overridden') {
    const horizon = (activity.after as Record<string, unknown> | null)?.['urgency_override'];
    return typeof horizon === 'string'
      ? `${who} set the horizon to ${horizon}`
      : `${who} returned the ticket to later`;
  }
  return `${who} ${activity.action.replace(/_/g, ' ')}`;
}

/**
 * The parts of the page a link may point at — a comment by its id, a question by its number — as a
 * search hit links them (docs/adr/0025 D5).
 */
const linkedPart = /^(comment-[0-9a-f-]{36}|question-[1-9][0-9]*)$/;

/** A key that names no ticket the way the server would answer it: not found (docs/adr/0023 D5). */
const notFound: ProblemView = {
  status: 404,
  code: 'not_found',
  title: 'No such ticket',
  detail: '',
  fields: {},
  current: {},
};

/**
 * One ticket (docs/adr/0018 D2): its title and body to edit, its fields, its moves, its
 * prerequisite tree, its questions with the answer form, children, links, interest, comments and
 * activity; a parent, a child or a link end of another project or team by its head
 * (docs/adr/0005 D3).
 * Everything on it follows the event stream. The page is reused when the path names another
 * ticket: a question the page asks goes then, and so does every editor and dialog of its parts,
 * each of which belongs to the ticket it was opened on.
 */
@Component({
  selector: 'app-ticket-detail',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    AgentMark,
    AnswerQuestion,
    AskQuestion,
    AttachmentsCard,
    ButtonDirective,
    CommentComposer,
    CommentItem,
    ConfirmDialog,
    EditQuestion,
    HeadKey,
    InterestControl,
    LinkAdder,
    PrerequisiteTree,
    RenderedText,
    RouterLink,
    SecurityBadge,
    SeverityBadge,
    Skeleton,
    StateBadge,
    TicketBody,
    TicketDelete,
    TicketFields,
    TicketMoves,
    TicketTitle,
    TimeCard,
    Tooltip,
    TypeIcon,
  ],
  providers: [TicketRelations, ConfirmationService],
  templateUrl: './ticket-detail.html',
  styleUrl: './ticket-detail.scss',
})
export class TicketDetail {
  /** The short key from the path, `<PROJECT>-<number>` (docs/adr/0007 D3). */
  readonly key = input.required<string>();

  protected readonly session = inject(SessionService);
  protected readonly relations = inject(TicketRelations);
  private readonly tickets = inject(TicketsService);
  private readonly problems = inject(ProblemService);
  private readonly conversation = inject(Conversation);
  private readonly confirm = inject(ConfirmationService);
  private readonly clock = inject(Clock);
  private readonly host: HTMLElement = inject(ElementRef).nativeElement;
  private readonly injector = inject(Injector);
  /** The part of the page the address names, `#comment-<id>` or `#question-<n>`. */
  private readonly fragment = toSignal(inject(ActivatedRoute).fragment, { initialValue: null });
  /** The part the page has scrolled to, once; a reload of the parts does not scroll again. */
  private scrolledTo: string | null = null;

  protected readonly at = computed(() => address(this.session.tenant(), this.key()));
  protected readonly fullKey = computed(() => {
    const at = this.at();
    return at ? `${at.team}/${at.project}-${at.number}` : undefined;
  });
  private readonly loading = this.tickets.ticket(() => this.fullKey());
  protected readonly ticket = computed(() => {
    const key = this.fullKey();
    return key ? this.tickets.cache.entry(key)()?.value : undefined;
  });
  /**
   * A key that is not `<PROJECT>-<number>` names nothing; a refused load names its problem. A
   * ticket that loaded and then left the cache is gone for the person — deleted, or out of their
   * sight —, which a refetch after its event said with a `404` (docs/adr/0024 D1).
   */
  protected readonly missing = computed<ProblemView | undefined>(() => {
    if (this.session.tenant() && !this.at()) {
      return notFound;
    }
    if (this.loading.error()) {
      return this.problems.read(this.loading.error());
    }
    const loaded = this.loading.hasValue() ? this.loading.value() : undefined;
    return loaded !== undefined && loaded === this.fullKey() && !this.ticket()
      ? notFound
      : undefined;
  });
  protected readonly openQuestions = computed(() =>
    this.relations.questions.hasValue()
      ? this.relations.questions.value().items.filter((q) => q.status === 'open')
      : [],
  );
  protected readonly settledQuestions = computed(() =>
    this.relations.questions.hasValue()
      ? this.relations.questions.value().items.filter((q) => q.status !== 'open')
      : [],
  );
  protected readonly interests = computed<Interest[]>(() =>
    this.relations.interest.hasValue() ? this.relations.interest.value().items : [],
  );
  /** The files of each comment, by the comment's id (docs/adr/0016 D1). */
  protected readonly commentFiles = computed(() => {
    const files = new Map<string, Attachment[]>();
    for (const file of this.relations.attachments.hasValue()
      ? this.relations.attachments.value().items
      : []) {
      if (file.comment) {
        files.set(file.comment, [...(files.get(file.comment) ?? []), file]);
      }
    }
    return files;
  });
  /** A tenant administrator withdraws any comment (docs/adr/0015 D3). */
  protected readonly administers = computed(() => this.session.membership()?.role === 'admin');
  /**
   * A member or an administrator writes the ticket, and so removes any of its relations
   * (docs/adr/0008 D2, docs/adr/0012 D2 as amended again 2026-10-10); a viewer does not.
   */
  protected readonly writes = computed(() => {
    const role = this.session.membership()?.role;
    return role === 'admin' || role === 'member';
  });

  constructor() {
    effect(() => this.relations.at.set(this.at()));
    // The rendered body follows the version of the ticket shown (TicketRelations.body).
    effect(() => this.relations.version.set(this.ticket()?.version));
    // A link to a comment or a question scrolls to it once the part has loaded.
    effect(() => {
      const target = this.fragment();
      const loaded = this.relations.comments.hasValue() && this.relations.questions.hasValue();
      if (!target || !loaded || target === this.scrolledTo || !linkedPart.test(target)) {
        return;
      }
      afterNextRender(
        () => {
          const part = this.host.querySelector<HTMLElement>(`[id="${target}"]`);
          if (part) {
            this.scrolledTo = target;
            part.scrollIntoView?.({ block: 'center' });
          }
        },
        { injector: this.injector },
      );
    });
    // A question of the page — whether to write over a newer version — belongs to the ticket it
    // was asked about. The page is reused when the path names another ticket or another tenant:
    // answered then, it would write the change onto the ticket shown now, where its fields stay,
    // or onto one the page no longer shows.
    effect(() => {
      this.fullKey();
      untracked(() => this.confirm.close());
    });
  }

  protected ago(iso: string): string {
    return ago(iso, this.clock.now());
  }

  protected dateTime(iso: string): string {
    return dateTime(iso);
  }

  protected describe(activity: Activity): string {
    return describe(activity);
  }

  /** What a failed part of the page says instead of its content. */
  protected failure(error: unknown): string {
    const problem = this.problems.read(error);
    return `Could not load this: ${problem.detail || problem.title}`;
  }

  /** One more page of the children (docs/adr/0048 D3). */
  protected moreChildren(): void {
    this.relations.childPages.update((pages) => pages + 1);
  }

  /**
   * Removes a link of the ticket by its id — outgoing or incoming, whatever team keeps it and
   * whatever the person reads of the other end (docs/adr/0012 D2 as amended again 2026-10-10).
   */
  protected unlink(link: string): void {
    const key = this.fullKey();
    if (key) {
      void this.removed(this.conversation.unlink(key, link), () => this.relations.reloadLinks());
    }
  }

  /**
   * Detaches a child of any project or team from the ticket by the handle its relation carries,
   * whatever the person reads of the child (docs/adr/0008 D2 as amended again 2026-10-10).
   */
  protected removeChild(handle: string): void {
    const key = this.fullKey();
    if (key) {
      void this.removed(this.conversation.removeChild(key, handle), () =>
        this.relations.reloadChildren(),
      );
    }
  }

  /**
   * A removal shows at once: the part loads again. A `404` says the relation is gone already —
   * another person or the other end removed it meanwhile — which is no failure; anything else is.
   */
  private async removed(removal: Promise<unknown>, reload: () => void): Promise<void> {
    try {
      await removal;
    } catch (error) {
      if (!(error instanceof HttpErrorResponse && error.status === 404)) {
        this.problems.report(error);
        return;
      }
    }
    reload();
  }
}
