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
import { Activity, Attachment, Interest, Link } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { ProblemService, ProblemView } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TicketsService } from '../../core/tickets.service';
import { AgentMark } from '../../shared/agent-mark';
import { SecurityBadge, SeverityBadge, StateBadge, TypeIcon } from '../../shared/badges';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { RenderedText } from '../../shared/rendered-text';
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
 * shows (docs/adr/0014 D3, docs/adr/0015 D1).
 */
export function describe(activity: Activity): string {
  const who = activity.actor?.display_name ?? activity.actor_system ?? 'cowork';
  if (activity.entity_type === 'project' && activity.action === 'ranked') {
    return `${who} sorted the backlog by score`;
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
 * prerequisite tree, its questions with the answer form, links, interest, comments and activity.
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
    return at ? `${at.tenant}/${at.project}-${at.number}` : undefined;
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

  /** Removes a link from its source's side, whichever side this ticket is. */
  protected unlink(link: Link): void {
    const key = this.fullKey();
    if (!key) {
      return;
    }
    const [source, target] =
      link.direction === 'outgoing' ? [key, link.ticket.key] : [link.ticket.key, key];
    this.conversation
      .unlink(source, link.type, target)
      .catch((error: unknown) => this.problems.report(error));
  }
}
