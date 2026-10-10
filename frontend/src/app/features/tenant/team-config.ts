import { Location } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { Dialog } from 'primeng/dialog';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { configTabs, isOpenedFromPage, openedFromPage } from './config-tabs';
import { TenantDashboard } from './dashboard';

/**
 * A team's configuration (docs/adr/0023 D4 as amended 2026-10-10): one large dialog over the
 * team's dashboard, a tab for each page the person's role shows ({@link configTabs}). Each tab is
 * the page it was before, at its address — `/t/{slug}/members` and the others —, routed into the
 * dialog, so a reload, the back button and a bookmark keep working, and the pages' services take
 * the team from the route as before (`session.tenant()`, which `TenantScope` sets). A tab replaces
 * the address rather than adding one, so the back button and closing return to the page before the
 * dialog: closing goes back in the history when a link opened it ({@link openedFromPage}), and to
 * the team's dashboard when its address was opened directly. A page's own dialogs — a confirmation,
 * a form — lie over it, appended to the document's body.
 */
@Component({
  selector: 'app-team-config',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Dialog, RouterLink, RouterLinkActive, RouterOutlet, TenantDashboard],
  template: `
    <app-tenant-dashboard />
    <p-dialog
      [visible]="open()"
      (visibleChange)="$event || close()"
      [modal]="true"
      [draggable]="false"
      [resizable]="false"
      [header]="'Configuration of ' + name()"
      closeAriaLabel="Close the configuration"
      [style]="{ width: 'min(96rem, calc(100vw - 3rem))', height: 'calc(100vh - 3rem)' }"
      [contentStyle]="{ display: 'flex', flexDirection: 'column', minHeight: '0', padding: '0' }"
      data-testid="team-config"
    >
      <nav class="tabs" aria-label="Configuration pages">
        @for (tab of tabs(); track tab.path) {
          <a
            [routerLink]="['/t', tenant(), tab.path]"
            [replaceUrl]="true"
            [state]="state"
            routerLinkActive="active"
            ariaCurrentWhenActive="page"
            [attr.data-testid]="'config-tab-' + tab.path"
            >{{ tab.label }}</a
          >
        }
      </nav>
      <div class="pane"><router-outlet /></div>
    </p-dialog>
  `,
  styles: `
    .tabs {
      display: flex;
      flex-wrap: wrap;
      gap: 0 1.5rem;
      padding: 0 1.25rem;
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
    .pane {
      flex: 1;
      min-height: 0;
      overflow-y: auto;
    }
    /*
     * The page's own heading says what its tab says: it stays for a screen reader and for the
     * focus a page gives it, out of sight. A page keeps its padding and its width.
     */
    :host ::ng-deep .pane .page > h1,
    :host ::ng-deep .pane .page > .head > h1 {
      position: absolute;
      width: 1px;
      height: 1px;
      margin: -1px;
      padding: 0;
      overflow: hidden;
      clip: rect(0 0 0 0);
      white-space: nowrap;
      border: 0;
    }
  `,
})
export class TeamConfig {
  private readonly session = inject(SessionService);
  private readonly tenantInfo = inject(TenantService);
  private readonly router = inject(Router);
  private readonly location = inject(Location);

  protected readonly open = signal(true);
  protected readonly tenant = computed(() => this.session.tenant() ?? '');
  protected readonly name = computed(() => this.session.shown()?.name ?? this.tenant());
  protected readonly tabs = computed(() =>
    configTabs(this.tenantInfo.isAdmin(), this.session.oversight()),
  );
  /** Whether a link of this application opened the dialog over a page, which closing returns to. */
  private readonly fromPage = isOpenedFromPage(this.location.getState());
  /** The state each tab carries on, so that closing still knows where it came from. */
  protected readonly state = this.fromPage ? openedFromPage : {};

  /** Closing returns to the page before the dialog, or to the team's dashboard. */
  protected close(): void {
    this.open.set(false);
    if (this.fromPage) {
      this.location.back();
    } else {
      void this.router.navigate(['/t', this.tenant()], { replaceUrl: true });
    }
  }
}
