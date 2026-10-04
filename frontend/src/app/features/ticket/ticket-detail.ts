import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  untracked,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { ConfirmationService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Skeleton } from 'primeng/skeleton';
import { Tooltip } from 'primeng/tooltip';
import { Activity, Interest, Link } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { ProblemService, ProblemView } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { TicketsService } from '../../core/tickets.service';
import { SecurityBadge, SeverityBadge, StateBadge, TypeIcon } from '../../shared/badges';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { ago, Clock, dateTime } from '../../shared/time';
import { AnswerQuestion, AskQuestion, CommentComposer, LinkAdder } from './conversation-forms';
import { InterestControl } from './interest-control';
import { AttachmentsCard, TimeCard } from './records-cards';
import { TicketFields } from './ticket-fields';
import { TicketMoves } from './ticket-moves';
import { address, TicketRelations } from './ticket-relations';

/** `transitioned` by Ada, with the agent that acted for her when there was one. */
export function describe(activity: Activity): string {
  const who = activity.actor?.display_name ?? activity.actor_system ?? 'cowork';
  return `${who} ${activity.action.replace(/_/g, ' ')}`;
}

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
 * One ticket (docs/adr/0018 D2): its fields to edit, its moves, its body, its questions with the
 * answer form, links, interest, comments and activity. Everything on it follows the event stream.
 * A question the page asks goes when the path names another ticket or another tenant.
 */
@Component({
  selector: 'app-ticket-detail',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    AnswerQuestion,
    AskQuestion,
    AttachmentsCard,
    ButtonDirective,
    CommentComposer,
    ConfirmDialog,
    InterestControl,
    LinkAdder,
    RouterLink,
    SecurityBadge,
    SeverityBadge,
    Skeleton,
    StateBadge,
    TicketFields,
    TicketMoves,
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
  /** A key that is not `<PROJECT>-<number>` names nothing; a refused load names its problem. */
  protected readonly missing = computed<ProblemView | undefined>(() => {
    if (this.session.tenant() && !this.at()) {
      return notFound;
    }
    return this.loading.error() ? this.problems.read(this.loading.error()) : undefined;
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

  constructor() {
    effect(() => this.relations.at.set(this.at()));
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
