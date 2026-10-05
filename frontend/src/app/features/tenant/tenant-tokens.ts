import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  Injector,
  untracked,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ConfirmationService, MessageService } from 'primeng/api';
import { ButtonDirective } from 'primeng/button';
import { Paginator } from 'primeng/paginator';
import { Skeleton } from 'primeng/skeleton';
import { TableModule } from 'primeng/table';
import { Tooltip } from 'primeng/tooltip';
import { MemberToken, Scope, TokenState } from '../../api/models';
import { CAPABILITY } from '../../api/models/capability-array';
import { changesMemberships, EventStreamService } from '../../core/event-stream.service';
import { ProblemService } from '../../core/problem.service';
import { refresh } from '../../core/refresh';
import { SessionService } from '../../core/session.service';
import { perPageOptions, tablePages } from '../../core/table-pages';
import { TenantTokensService } from '../../core/tenant-tokens.service';
import { TenantService } from '../../core/tenant.service';
import { ConfirmDialog } from '../../shared/confirm-dialog';
import { dateTime } from '../../shared/time';
import { scopeMeanings } from '../me/new-token-dialog';
import { day } from '../me/tokens';

/** What each state of a member's token means, for the tooltip beside it (docs/adr/0035 D6). */
const stateMeanings: Record<TokenState, string> = {
  active: 'Works until it expires or is revoked.',
  expired: 'Past its expiry: requests with it answer 401.',
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

/** Whether a token reaches beyond this tenant: one restricted to none reaches every tenant of its person. */
export function unrestricted(token: MemberToken): boolean {
  return token.restricted_tenant === null;
}

/**
 * What revoking a token does, said before it is done: where the token stops working, that the
 * person makes a new one, that it is final and recorded. An unrestricted token ends in every tenant
 * of its person, not only here, and the sentence names that first.
 */
export function revocationMessage(token: MemberToken): string {
  const person = token.person.display_name;
  const reach = unrestricted(token)
    ? `It is not restricted to this tenant: revoking it ends it in every tenant ${person} ` +
      'belongs to, not only here.'
    : 'It is restricted to this tenant and ends here.';
  return (
    `${reach} Every request that presents it is refused from now on, and ${person} makes a new ` +
    "one if they need it. A revocation cannot be undone; it is recorded in this tenant's audit record."
  );
}

/**
 * The tokens that can act in the tenant, for its administrators (docs/adr/0035 D5 as amended
 * 2026-10-05): every member's unrestricted tokens and those restricted to this tenant, with their
 * person, scope, agent flag, reach, dates and state — metadata, never a secret — in numbered pages
 * of 25, 50 or 100 (docs/adr/0048 D4). A token restricted to another tenant is not shown. Revoking
 * one asks first, and for an unrestricted token the question says that it ends in the person's
 * other tenants too. A member who is no administrator reads that the list is the administrators'
 * and asks nothing. Tokens are not on the event stream: the list is read when the page opens, after
 * a revocation, and again on a membership change of the tenant, a `resync` and a `poll`.
 */
@Component({
  selector: 'app-tenant-tokens',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, ConfirmDialog, Paginator, Skeleton, TableModule, Tooltip],
  providers: [ConfirmationService],
  templateUrl: './tenant-tokens.html',
  styleUrl: './tenant-tokens.scss',
})
export class TenantTokens {
  private readonly service = inject(TenantTokensService);
  private readonly problems = inject(ProblemService);
  private readonly messages = inject(MessageService);
  private readonly confirm = inject(ConfirmationService);
  private readonly injector = inject(Injector);
  protected readonly session = inject(SessionService);
  protected readonly tenantInfo = inject(TenantService);

  protected readonly dateTime = dateTime;
  protected readonly day = day;
  protected readonly unrestricted = unrestricted;
  protected readonly perPageOptions = perPageOptions;
  protected readonly byId = (_: number, token: MemberToken) => token.id;

  protected readonly table = tablePages(
    () => {
      const tenant = this.session.tenant();
      return tenant && this.tenantInfo.isAdmin() ? tenant : undefined;
    },
    (tenant, page, perPage) => this.service.page(tenant, page, perPage),
  );

  protected readonly failure = computed(() => {
    const error = this.table.rows.error();
    if (!error) {
      return undefined;
    }
    const problem = this.problems.read(error);
    return `The tokens could not be loaded: ${problem.detail || problem.title}`;
  });

  constructor() {
    // Tokens are not on the event stream; who belongs to the tenant is, and a token of a person who
    // left is no longer in the list. A gap in the stream and the fallback's poll load it again, as
    // they do every list, at the cost of a 304 when nothing changed (docs/adr/0054 D7).
    inject(EventStreamService)
      .events.pipe(takeUntilDestroyed())
      .subscribe((event) => {
        if (changesMemberships(event, this.session.tenant())) {
          refresh(this.table.rows, this.injector);
        }
      });
    // A question belongs to the tenant it was asked in: another tenant's page closes it.
    effect(() => {
      this.session.tenant();
      untracked(() => this.confirm.close());
    });
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
    return stateMeanings[state];
  }

  /** What an agent token may do, as text in the cell, as the person's own token page says it. */
  protected capabilitiesText(token: MemberToken): string {
    if (token.capabilities.length === CAPABILITY.length) {
      return 'all nine capabilities';
    }
    return token.capabilities.length === 0 ? 'the baseline only' : token.capabilities.join(', ');
  }

  /** Revokes the token after the question, which names how far the revocation reaches. */
  protected revoke(token: MemberToken): void {
    const tenant = this.session.tenant();
    if (!tenant) {
      return;
    }
    this.confirm.confirm({
      header: `Revoke ${token.name} of ${token.person.display_name}?`,
      message: revocationMessage(token),
      acceptLabel: unrestricted(token) ? 'Revoke everywhere' : 'Revoke',
      rejectLabel: 'Keep it',
      acceptButtonProps: { severity: 'danger' },
      // It cannot be undone: an Enter that comes a moment late must not confirm it.
      defaultFocus: 'reject',
      accept: async () => {
        try {
          await this.service.revoke(tenant, token.id);
          this.messages.add({
            severity: 'success',
            summary: 'Token revoked',
            detail: `${token.name} of ${token.person.display_name}`,
            life: 4000,
          });
        } catch (error) {
          this.problems.report(error);
        } finally {
          refresh(this.table.rows, this.injector);
        }
      },
    });
  }
}
