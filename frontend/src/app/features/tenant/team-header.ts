import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { IsActiveMatchOptions, RouterLink, RouterLinkActive } from '@angular/router';
import { ButtonDirective } from 'primeng/button';
import { Tooltip } from 'primeng/tooltip';
import { SessionService } from '../../core/session.service';
import { configTabs, gearTab, openedFromPage } from './config-tabs';

/** A tab active on its own path, whatever the query: the dashboard's filters, the list's. */
const ownPath: IsActiveMatchOptions = {
  paths: 'exact',
  queryParams: 'ignored',
  matrixParams: 'ignored',
  fragment: 'ignored',
};

/**
 * The head of a team's own pages (docs/adr/0018 D6, docs/adr/0023 D4 as amended 2026-10-10): the
 * team's name, and its dashboard, board, ticket list and time report as tabs, each a link to its own
 * address, as a project's header has its board and backlog. A global administrator who only
 * oversees the team sees its name alone (docs/adr/0034 D2), and beside it the gear of the team's
 * configuration, which the sidebar cannot offer: the sidebar lists the person's teams only.
 */
@Component({
  selector: 'app-team-header',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonDirective, RouterLink, RouterLinkActive, Tooltip],
  template: `
    <header class="head">
      <div class="title">
        <h1 data-testid="team-name">{{ name() }}</h1>
        @if (session.oversight() && session.tenant(); as tenant) {
          <a
            pButton
            [text]="true"
            [rounded]="true"
            severity="secondary"
            [iconOnly]="true"
            [routerLink]="['/t', tenant, gear()]"
            [state]="openedFromPage"
            [attr.aria-label]="'Configuration of ' + name()"
            [pTooltip]="'Configuration of ' + name()"
            tooltipPosition="bottom"
            data-testid="team-config-gear"
            ><i class="pi pi-cog"></i
          ></a>
        }
      </div>
      @if (!session.oversight() && session.tenant(); as tenant) {
        <nav class="tabs" aria-label="Views of the team">
          <a
            [routerLink]="['/t', tenant]"
            routerLinkActive="active"
            [routerLinkActiveOptions]="ownPath"
            ariaCurrentWhenActive="page"
            data-testid="team-tab-overview"
            >Overview</a
          >
          <a
            [routerLink]="['/t', tenant, 'board']"
            routerLinkActive="active"
            ariaCurrentWhenActive="page"
            data-testid="team-tab-board"
            >Board</a
          >
          <a
            [routerLink]="['/t', tenant, 'tickets']"
            routerLinkActive="active"
            [routerLinkActiveOptions]="ownPath"
            ariaCurrentWhenActive="page"
            data-testid="team-tab-tickets"
            >Tickets</a
          >
          <a
            [routerLink]="['/t', tenant, 'time']"
            routerLinkActive="active"
            ariaCurrentWhenActive="page"
            data-testid="team-tab-time"
            >Time</a
          >
        </nav>
      }
    </header>
  `,
  styles: `
    :host {
      display: block;
    }
    .title {
      display: flex;
      align-items: center;
      gap: 0.375rem;
      h1 {
        font-size: 1.5rem;
      }
    }
    .tabs {
      display: flex;
      gap: 1.5rem;
      margin-top: 1rem;
      border-bottom: 1px solid var(--p-app-border);
      a {
        margin-bottom: -1px;
        padding: 0.5rem 0.125rem;
        border-bottom: 2px solid transparent;
        font-weight: 550;
        color: var(--p-text-muted-color);
        &:hover {
          color: var(--p-text-color);
          text-decoration: none;
        }
        &.active {
          border-bottom-color: var(--p-primary-color);
          color: var(--p-text-color);
        }
        &:focus-visible {
          outline: 2px solid var(--p-primary-color);
          outline-offset: 2px;
          border-radius: var(--p-border-radius-sm);
        }
      }
    }
  `,
})
export class TeamHeader {
  protected readonly session = inject(SessionService);
  protected readonly ownPath = ownPath;
  protected readonly openedFromPage = openedFromPage;

  protected readonly name = computed(
    () => this.session.shown()?.name ?? this.session.tenant() ?? '',
  );
  /** The tab the gear opens: what a global administrator without a role sees of the team. */
  protected readonly gear = computed(() => gearTab(configTabs(false, true)));
}
