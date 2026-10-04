import { CdkScrollable } from '@angular/cdk/scrolling';
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  signal,
} from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { MenuItem } from 'primeng/api';
import { Avatar } from 'primeng/avatar';
import { ButtonDirective } from 'primeng/button';
import { Menu } from 'primeng/menu';
import { Select } from 'primeng/select';
import { Toast } from 'primeng/toast';
import { Tooltip } from 'primeng/tooltip';
import { catchError, of } from 'rxjs';
import { Wordmark } from '../brand/logo';
import { AuthService } from '../core/auth.service';
import { EventStreamService } from '../core/event-stream.service';
import { HARD_NAVIGATION } from '../core/hard-navigation';
import { ProblemService } from '../core/problem.service';
import { ProjectsService } from '../core/projects.service';
import { TenantService } from '../core/tenant.service';
import { NewProjectDialog } from '../features/project/new-project-dialog';
import { SessionService } from '../core/session.service';
import { VersionService } from '../core/version.service';
import { devRoutes } from '../dev/dev.routes';
import { ThemePreference, ThemeService } from '../theme/theme.service';
import { LiveIndicator } from './live-indicator';

const themeTexts: Record<ThemePreference, { icon: string; label: string }> = {
  system: { icon: 'pi pi-desktop', label: 'Theme: follows the system' },
  light: { icon: 'pi pi-sun', label: 'Theme: light' },
  dark: { icon: 'pi pi-moon', label: 'Theme: dark' },
};

/** `Ada Lovelace` → `AL`. */
export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  const letters =
    parts.length > 1 ? parts[0][0] + parts[parts.length - 1][0] : (parts[0] ?? '?').slice(0, 2);
  return letters.toUpperCase();
}

/** The frame of every page: the top bar, the navigation of the tenant, the content. */
@Component({
  selector: 'app-shell',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    Avatar,
    ButtonDirective,
    CdkScrollable,
    FormsModule,
    LiveIndicator,
    Menu,
    NewProjectDialog,
    RouterLink,
    RouterLinkActive,
    RouterOutlet,
    Select,
    Toast,
    Tooltip,
    Wordmark,
  ],
  templateUrl: './shell.html',
  styleUrl: './shell.scss',
})
export class Shell {
  private readonly router = inject(Router);
  protected readonly session = inject(SessionService);
  protected readonly projects = inject(ProjectsService);
  protected readonly stream = inject(EventStreamService);
  protected readonly theme = inject(ThemeService);
  protected readonly tenantInfo = inject(TenantService);
  protected readonly creatingProject = signal(false);
  protected readonly dev = devRoutes.length > 0;

  /** null until the backend answered, and null when it cannot be reached. */
  protected readonly version = toSignal(
    inject(VersionService)
      .get()
      .pipe(catchError(() => of(null))),
    { initialValue: null },
  );

  protected readonly tenants = computed(() =>
    this.session.memberships().map((membership) => membership.tenant),
  );
  protected readonly themeText = computed(() => themeTexts[this.theme.preference()]);
  protected readonly initials = computed(() => initials(this.session.person()?.display_name ?? ''));

  private readonly auth = inject(AuthService);
  private readonly problems = inject(ProblemService);
  private readonly navigate = inject(HARD_NAVIGATION);

  /** The person's own menu: who is signed in, their tokens, their password, the way out. */
  protected readonly meItems = computed<MenuItem[]>(() => {
    const person = this.session.person();
    return [
      { label: person?.display_name ?? '', disabled: true, styleClass: 'who' },
      { separator: true },
      { label: 'Your tokens', icon: 'pi pi-key', routerLink: '/me/tokens' },
      ...(person?.local
        ? [{ label: 'Change password', icon: 'pi pi-lock', routerLink: '/password' }]
        : []),
      { separator: true },
      { label: 'Sign out', icon: 'pi pi-sign-out', command: () => void this.signOut() },
    ];
  });

  constructor() {
    // A temporary password allows nothing but changing it (docs/adr/0033 D4): the shell does not
    // show pages the backend would refuse, it goes to the password page first.
    effect(() => {
      if (this.session.person()?.password_change_required) {
        void this.router.navigate(['/password'], { queryParams: { return: this.router.url } });
      }
    });
  }

  /**
   * Signing out ends with a new document, so that the next person in this tab starts from nothing:
   * the login page, or the identity provider's logout where the backend names one, which ends the
   * person's session there too and comes back to the login page (docs/adr/0031 D4).
   */
  protected async signOut(): Promise<void> {
    try {
      const next = await this.auth.logout();
      this.navigate(next ?? '/login');
    } catch (error) {
      this.problems.report(error);
    }
  }

  protected switchTenant(slug: string): void {
    void this.router.navigate(['/t', slug]);
  }
}
