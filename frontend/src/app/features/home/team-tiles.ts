import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { SessionService } from '../../core/session.service';

/**
 * A tile for every team the person may open, each leading to the team's dashboard with its slug
 * and the person's role in it, or `no role` — for a global administrator every team of the
 * installation, the ones they only oversee among them (docs/adr/0034 D2). The page of every team
 * shows them, and the start page of a person without a membership.
 */
@Component({
  selector: 'app-team-tiles',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
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
  `,
  styles: `
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
  `,
})
export class TeamTiles {
  protected readonly session = inject(SessionService);
}
