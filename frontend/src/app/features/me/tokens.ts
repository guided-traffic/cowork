import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { ConfirmationService, MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Paginator } from 'primeng/paginator';
import { TableModule } from 'primeng/table';
import { Tooltip } from 'primeng/tooltip';
import { Scope, Token, TokenCreated, TokenState } from '../../api/models';
import { CAPABILITY } from '../../api/models/capability-array';
import { ProblemService } from '../../core/problem.service';
import { perPageOptions } from '../../core/table-pages';
import { TokensService } from '../../core/tokens.service';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { SecretDialog } from '../../shared/secret-dialog';
import { dateTime } from '../../shared/time';
import { NewTokenDialog, scopeMeanings } from './new-token-dialog';

/** What each state of a token means, for the tooltip beside it (docs/adr/0035 D6). */
export const tokenStateMeanings: Record<TokenState, string> = {
  active: 'Works until it expires or you revoke it.',
  expired: 'Past its expiry: requests with it answer 401. Make a new token.',
  revoked: 'Revoked for good: requests with it answer 401, and a revocation cannot be undone.',
};

/** The accent of each state and of each scope, as tokens of the preset (docs/adr/0052 D5). */
const stateAccents: Record<TokenState, string> = {
  active: 'var(--p-state-done)',
  expired: 'var(--p-state-filed)',
  revoked: 'var(--p-severity-critical)',
};
const scopeAccents: Record<Scope, string> = {
  read: 'var(--p-text-muted-color)',
  write: 'var(--p-state-analysed)',
  admin: 'var(--p-severity-high)',
};

/**
 * An API day — `last_used_on` is a date, not a moment — in the browser's locale. It is read and
 * written in UTC, so that the day shown is the day the API names whatever the time zone is.
 */
export function day(value: string, locale?: string): string {
  return new Date(`${value}T00:00:00Z`).toLocaleDateString(locale, {
    dateStyle: 'medium',
    timeZone: 'UTC',
  });
}

/**
 * The person's own tokens (docs/adr/0035), in numbered pages of 25, 50 or 100 (docs/adr/0048 D4):
 * every one they made with its scope, restriction, dates and state, never a plaintext — cowork keeps only a hash. A new token's plaintext is shown
 * once, in a dialog, and held nowhere else. Revoking one is final and asks first.
 */
@Component({
  selector: 'app-tokens',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    ButtonDirective,
    ConfirmDialog,
    NewTokenDialog,
    Paginator,
    SecretDialog,
    TableModule,
    Tooltip,
  ],
  providers: [ConfirmationService],
  templateUrl: './tokens.html',
  styleUrl: './tokens.scss',
})
export class Tokens {
  protected readonly tokens = inject(TokensService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly confirm = inject(ConfirmationService);

  protected readonly dateTime = dateTime;
  protected readonly day = day;
  protected readonly perPageOptions = perPageOptions;

  protected readonly creating = signal(false);
  /** The plaintext being shown: the one place the page holds it, emptied when the dialog closes. */
  protected readonly secret = signal<string | null>(null);
  /** The token being shown, without its plaintext. */
  protected readonly issued = signal<Token | null>(null);

  /** When the token being shown expires: the answer says, because the installation may have shortened it. */
  protected readonly expiry = computed(() => {
    const token = this.issued();
    return token ? dateTime(token.expires_at) : '';
  });

  protected readonly failure = computed(() => {
    const error = this.tokens.tokens.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The tokens could not be loaded: ${problem.detail || problem.title}`;
  });

  constructor() {
    // What the list holds is as old as the last visit — its last-used days above all: ask again
    // on the way in. A load that is under way already is a fresh one, and `reload` leaves it alone.
    this.tokens.tokens.reload();
  }

  protected scopeMeaning(scope: Scope): string {
    return scopeMeanings[scope];
  }

  protected scopeAccent(scope: Scope): string {
    return scopeAccents[scope];
  }

  protected stateAccent(state: TokenState): string {
    return stateAccents[state];
  }

  protected stateMeaning(state: TokenState): string {
    return tokenStateMeanings[state];
  }

  /**
   * What an agent token may do, as text in the cell and not behind a hover: `all nine capabilities`,
   * the names of the ones it has, or `the baseline only` for an agent that was given none. These
   * are what a person looks at to decide which token to revoke.
   */
  protected capabilitiesText(token: Token): string {
    if (token.capabilities.length === CAPABILITY.length) {
      return 'all nine capabilities';
    }
    return token.capabilities.length === 0 ? 'the baseline only' : token.capabilities.join(', ');
  }

  protected created({ token: plaintext, ...token }: TokenCreated): void {
    this.issued.set(token);
    this.secret.set(plaintext ?? null);
  }

  protected revoke(token: Token): void {
    this.confirm.confirm({
      header: `Revoke ${token.name}?`,
      message:
        'Every request that presents it is refused from now on. It stays in the list, and a ' +
        'revocation cannot be undone.',
      acceptLabel: 'Revoke',
      rejectLabel: 'Keep it',
      acceptButtonProps: { severity: 'danger' },
      // It cannot be undone: an Enter that comes a moment late must not confirm it.
      defaultFocus: 'reject',
      accept: async () => {
        try {
          await this.tokens.revoke(token);
          this.messages.add({
            severity: 'success',
            summary: 'Token revoked',
            detail: token.name,
            life: 4000,
          });
        } catch (error) {
          this.problems.report(error);
        }
      },
    });
  }
}
