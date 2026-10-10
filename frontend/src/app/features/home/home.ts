import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Skeleton } from 'primeng/skeleton';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { MyTickets } from '../me/my-tickets';
import { FirstTenant } from './first-tenant';

/**
 * The start page is "next for me" (docs/adr/0018 D3, docs/adr/0023 D4 as amended 2026-10-05): what
 * the person could take up next across their tenants, whether they belong to one or to many. A
 * person who belongs to none chooses among the tenants they may open — a global administrator among
 * every tenant of the installation, the ones they hold no role in marked so (docs/adr/0034 D2) — and
 * a global administrator makes the first one while there is none (docs/adr/0032 D5).
 */
@Component({
  selector: 'app-home',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FirstTenant, MyTickets, RouterLink, Skeleton],
  template: `
    @if (session.me.isLoading() || listing()) {
      <section class="page">
        <p-skeleton width="16rem" height="2rem" />
      </section>
    } @else if (session.me.error(); as error) {
      <section class="page">
        <div class="notice" data-testid="signed-out">
          <h1>{{ problem(error).title }}</h1>
          <p class="muted">{{ problem(error).detail }}</p>
          @if (problem(error).status === 401) {
            <p><a routerLink="/login" data-testid="sign-in">Sign in</a></p>
          }
        </div>
      </section>
    } @else if (firstTenant()) {
      <section class="page"><app-first-tenant /></section>
    } @else if (session.memberships().length > 0) {
      <app-my-tickets list="next" />
    } @else {
      <section class="page">
        <h1>Your teams</h1>
        <div class="tenants">
          @for (tenant of session.tenants(); track tenant.slug) {
            <a
              class="tenant"
              [routerLink]="['/t', tenant.slug]"
              [attr.data-testid]="'tenant-' + tenant.slug"
            >
              <span class="name">{{ tenant.name }}</span>
              <span class="muted">{{ tenant.slug }} · {{ tenant.role ?? 'no role' }}</span>
            </a>
          } @empty {
            <p class="muted">You are not a member of any team yet.</p>
          }
        </div>
      </section>
    }
  `,
  styles: `
    .page {
      padding: 2rem;
      display: flex;
      flex-direction: column;
      gap: 1.25rem;
    }
    .tenants {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(16rem, 1fr));
      gap: 0.75rem;
    }
    .tenant {
      display: flex;
      flex-direction: column;
      gap: 0.25rem;
      padding: 1rem 1.125rem;
      border: 1px solid var(--p-app-border);
      border-radius: var(--p-border-radius-lg);
      background: var(--p-app-panel);
      color: var(--p-text-color);
      &:hover {
        border-color: var(--p-primary-color);
        text-decoration: none;
      }
    }
    .name {
      font-weight: 600;
    }
    .notice {
      max-width: 40rem;
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
    }
  `,
})
export class Home {
  protected readonly session = inject(SessionService);
  private readonly problems = inject(ProblemService);

  /** A global administrator's tenants are still being listed. */
  protected readonly listing = computed(
    () => this.session.person()?.global_admin === true && this.session.installation.isLoading(),
  );
  /** A global administrator in an installation without a tenant: nothing to choose, one to make. */
  protected readonly firstTenant = computed(
    () =>
      this.session.person()?.global_admin === true &&
      this.session.installation.hasValue() &&
      this.session.tenants().length === 0,
  );

  protected problem(error: unknown) {
    return this.problems.read(error);
  }
}
