import { ChangeDetectionStrategy, Component, inject, input, signal } from '@angular/core';
import { ConfirmationService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Tooltip } from 'primeng/tooltip';
import { PullRequest, PullRequestState } from '../../api/models';
import { Conversation } from '../../core/conversation.service';
import { ProblemService } from '../../core/problem.service';
import { foundIn, pullRequestLabel } from '../../shared/pull-requests';
import { ago, Clock, dateTime } from '../../shared/time';

/** The accent of a pull request's state: the ticket states' tokens, so both schemes hold. */
const stateAccent: Readonly<Record<PullRequestState, string>> = {
  open: 'var(--p-state-in-progress)',
  merged: 'var(--p-state-done)',
  closed: 'var(--p-state-dropped)',
};

/**
 * The pull requests and default-branch commits GitHub's webhook linked to the ticket
 * (docs/adr/0071 D6): each with its number or short id — a link to its page at GitHub —, its
 * state, its title as GitHub sent it, the repository, the author, when it merged or was last
 * heard of, and where its key was read. A member removes a wrong link like any link, for good,
 * after the page's question (docs/adr/0071 Residual risks); the page hides the card while the
 * ticket has none.
 */
@Component({
  selector: 'app-pull-requests-card',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, Tooltip],
  template: `
    <h2><i class="pi pi-github" aria-hidden="true"></i>Pull requests</h2>
    @for (pr of items(); track pr.id) {
      <div class="pr" [attr.data-testid]="'pull-request-' + pr.id">
        <div class="pr-head">
          <a
            class="tabular label"
            [href]="pr.url"
            target="_blank"
            rel="noopener noreferrer"
            [attr.aria-label]="
              (pr.kind === 'commit' ? 'Commit ' : 'Pull request ') + label(pr) + ' at GitHub'
            "
            >{{ label(pr) }}</a
          >
          <span class="pill" [style.--accent]="accent(pr.state)" data-testid="pull-request-state"
            ><span class="dot"></span>{{ pr.kind === 'commit' ? 'pushed' : pr.state }}</span
          >
          @if (mayRemove()) {
            <button
              pButton
              type="button"
              [text]="true"
              [rounded]="true"
              size="small"
              severity="secondary"
              [iconOnly]="true"
              [disabled]="removing() === pr.id"
              [attr.data-testid]="'remove-pull-request-' + pr.id"
              (click)="remove(pr)"
              aria-label="Remove this link"
              pTooltip="Remove a link its key named by mistake"
            >
              <i class="pi pi-times"></i>
            </button>
          }
        </div>
        <span class="small title">{{ pr.title }}</span>
        <span class="muted small"
          >{{ pr.repository }}{{ pr.author ? ' · ' + pr.author : '' }} ·
          <span [pTooltip]="dateTime(pr.merged_at ?? pr.last_seen_at)">{{ when(pr) }}</span></span
        >
        <span class="muted small" data-testid="pull-request-found-in"
          >key in {{ foundIn(pr) }}</span
        >
      </div>
    }
  `,
  styles: `
    h2 {
      display: flex;
      align-items: center;
      gap: 0.5rem;
    }
    .pr {
      display: flex;
      flex-direction: column;
      gap: 0.125rem;
      padding: 0.5rem 0;
      border-top: 1px solid var(--p-app-border);
      &:first-of-type {
        border-top: none;
      }
    }
    .pr-head {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      > button {
        margin-left: auto;
      }
    }
    .label {
      font-weight: 600;
    }
    .title {
      overflow-wrap: anywhere;
    }
    .small {
      font-size: 0.8125rem;
    }
  `,
})
export class PullRequestsCard {
  readonly ticketKey = input.required<string>();
  readonly items = input.required<readonly PullRequest[]>();
  /** A member's, with `write` scope — the role the page reads (docs/adr/0043 D2). */
  readonly mayRemove = input(false);

  private readonly conversation = inject(Conversation);
  private readonly problems = inject(ProblemService);
  private readonly confirm = inject(ConfirmationService);
  private readonly clock = inject(Clock);
  /** The link whose removal is on its way. */
  protected readonly removing = signal<string | null>(null);
  protected readonly label = pullRequestLabel;
  protected readonly foundIn = foundIn;
  protected readonly dateTime = dateTime;

  protected accent(state: PullRequestState): string {
    return stateAccent[state];
  }

  /** When it merged — a commit when it was pushed —, or else when GitHub last told of it. */
  protected when(pr: PullRequest): string {
    if (!pr.merged_at) {
      return `seen ${ago(pr.last_seen_at, this.clock.now())}`;
    }
    return `${pr.kind === 'commit' ? 'pushed' : 'merged'} ${ago(pr.merged_at, this.clock.now())}`;
  }

  /**
   * Asks first, because the removal stays: a later delivery that names the ticket does not bring the
   * link back. The list loads again on the act's event.
   */
  protected remove(pr: PullRequest): void {
    const key = this.ticketKey();
    const what =
      pr.kind === 'commit'
        ? `the commit ${pullRequestLabel(pr)}`
        : `pull request ${pullRequestLabel(pr)}`;
    this.confirm.confirm({
      header: 'Remove the link',
      message: `The ticket no longer lists ${what} of ${pr.repository}, and GitHub's later deliveries do not link it again. Remove it only where its key named this ticket by mistake.`,
      acceptLabel: 'Remove',
      rejectLabel: 'Keep it',
      accept: () => void this.guard(pr.id, () => this.conversation.removePullRequest(key, pr.id)),
    });
  }

  private async guard(id: string, act: () => Promise<unknown>): Promise<void> {
    this.removing.set(id);
    try {
      await act();
    } catch (error) {
      this.problems.report(error);
    } finally {
      this.removing.set(null);
    }
  }
}
