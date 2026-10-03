import { ChangeDetectionStrategy, Component, computed, effect, inject } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { Skeleton } from 'primeng/skeleton';
import { ProblemService } from '../../core/problem.service';
import { SessionService } from '../../core/session.service';
import { FirstTenant } from './first-tenant';

/**
 * The start page: a person with one membership goes straight to its tenant (docs/adr/0023 D4);
 * with several, they choose. A global administrator with none makes the first one
 * (docs/adr/0032 D5). The person-level lists of docs/adr/0018 D3 replace it when they exist.
 */
@Component({
  selector: 'app-home',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FirstTenant, RouterLink, Skeleton],
  template: `
    <section class="page">
      @if (session.me.isLoading()) {
        <p-skeleton width="16rem" height="2rem" />
      } @else if (session.me.error(); as error) {
        <div class="notice" data-testid="signed-out">
          <h1>{{ problem(error).title }}</h1>
          <p class="muted">{{ problem(error).detail }}</p>
          @if (problem(error).status === 401) {
            <p><a routerLink="/login" data-testid="sign-in">Sign in</a></p>
          }
        </div>
      } @else if (firstTenant()) {
        <app-first-tenant />
      } @else {
        <h1>Your tenants</h1>
        <div class="tenants">
          @for (membership of session.memberships(); track membership.tenant.slug) {
            <a
              class="tenant"
              [routerLink]="['/t', membership.tenant.slug]"
              [attr.data-testid]="'tenant-' + membership.tenant.slug"
            >
              <span class="name">{{ membership.tenant.name }}</span>
              <span class="muted">{{ membership.tenant.slug }} · {{ membership.role }}</span>
            </a>
          } @empty {
            <p class="muted">You are not a member of any tenant yet.</p>
          }
        </div>
      }
    </section>
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

  /** A global administrator who is in no tenant yet: nothing to choose from, but one to make. */
  protected readonly firstTenant = computed(
    () => this.session.person()?.global_admin === true && this.session.memberships().length === 0,
  );

  constructor() {
    const router = inject(Router);
    effect(() => {
      const sole = this.session.soleTenant();
      if (sole) {
        void router.navigate(['/t', sole], { replaceUrl: true });
      }
    });
  }

  protected problem(error: unknown) {
    return this.problems.read(error);
  }
}
