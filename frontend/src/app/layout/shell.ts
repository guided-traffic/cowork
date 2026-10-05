import { CdkScrollable } from '@angular/cdk/scrolling';
import {
  afterNextRender,
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  ElementRef,
  inject,
  Injector,
  linkedSignal,
  signal,
} from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import {
  IsActiveMatchOptions,
  NavigationEnd,
  Router,
  RouterLink,
  RouterLinkActive,
  RouterOutlet,
} from '@angular/router';
import { MenuItem } from 'primeng/api';
import { Avatar } from 'primeng/avatar';
import { ButtonDirective } from 'primeng/button';
import { InputText } from 'primeng/inputtext';
import { Menu } from 'primeng/menu';
import { Select } from 'primeng/select';
import { Toast } from 'primeng/toast';
import { Tooltip } from 'primeng/tooltip';
import { catchError, filter, map, of } from 'rxjs';
import { Wordmark } from '../brand/logo';
import { AuthService } from '../core/auth.service';
import { ChatService } from '../core/chat.service';
import { EventStreamService } from '../core/event-stream.service';
import { HARD_NAVIGATION } from '../core/hard-navigation';
import { InboxService } from '../core/inbox.service';
import { ProblemService } from '../core/problem.service';
import { ProjectsService } from '../core/projects.service';
import { TenantService } from '../core/tenant.service';
import { NewProjectDialog } from '../features/project/new-project-dialog';
import { SessionService } from '../core/session.service';
import { VersionService } from '../core/version.service';
import { devRoutes } from '../dev/dev.routes';
import { ThemePreference, ThemeService } from '../theme/theme.service';
import { ChatPanel } from './chat-panel';
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

/**
 * The words of the search a URL shows, `/me/search?q=…` or `/t/<tenant>/search?q=…`; null for any
 * other page.
 */
export function searchedFor(router: Router, url: string): string | null {
  const tree = router.parseUrl(url);
  const path = tree.root.children['primary']?.segments.map((segment) => segment.path) ?? [];
  const isSearch =
    (path.length === 2 && path[0] === 'me' && path[1] === 'search') ||
    (path.length === 3 && path[0] === 't' && path[2] === 'search');
  return isSearch ? String(tree.queryParams['q'] ?? '') : null;
}

/** The windows on which the assistant lies over the content instead of beside it (shell.scss). */
export const overlayQuery = '(max-width: 64rem)';

/**
 * The frame of every page: the top bar, the navigation of the tenant, the content, and the
 * assistant at the right edge where the tenant's chat is available.
 */
@Component({
  selector: 'app-shell',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    Avatar,
    ButtonDirective,
    CdkScrollable,
    ChatPanel,
    FormsModule,
    InputText,
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
  protected readonly chat = inject(ChatService);
  protected readonly inbox = inject(InboxService);
  protected readonly creatingProject = signal(false);
  protected readonly dev = devRoutes.length > 0;
  /** A link active on its own path, whatever the query; not on the paths below it. */
  protected readonly listOnly: IsActiveMatchOptions = {
    paths: 'exact',
    queryParams: 'ignored',
    matrixParams: 'ignored',
    fragment: 'ignored',
  };

  /** null until the backend answered, and null when it cannot be reached. */
  protected readonly version = toSignal(
    inject(VersionService)
      .get()
      .pipe(catchError(() => of(null))),
    { initialValue: null },
  );

  /**
   * The tenants of the switch: the person's, and for a global administrator every other tenant of
   * the installation, marked as one they hold no role in (docs/adr/0034 D2).
   */
  protected readonly tenants = computed(() =>
    this.session.tenants().map(({ slug, name, role }) => ({
      slug,
      name: role ? name : `${name} (no role)`,
    })),
  );
  protected readonly themeText = computed(() => themeTexts[this.theme.preference()]);
  /** The bell's count as it is read: `99+` above ninety-nine. */
  protected readonly unread = computed(() => {
    const n = this.inbox.count();
    return n > 99 ? '99+' : String(n);
  });
  protected readonly bellLabel = computed(() => {
    const n = this.inbox.count();
    return n === 0 ? 'Inbox' : `Inbox, ${n} unread`;
  });
  protected readonly initials = computed(() => initials(this.session.person()?.display_name ?? ''));

  /** The words of the search the page shows; null on any other page. */
  private readonly searched = toSignal(
    this.router.events.pipe(
      filter((event): event is NavigationEnd => event instanceof NavigationEnd),
      map((event) => searchedFor(this.router, event.urlAfterRedirects)),
    ),
    { initialValue: searchedFor(this.router, this.router.url) },
  );
  /** What the search box holds: what the person types, the search shown, empty elsewhere. */
  protected readonly query = linkedSignal(() => this.searched() ?? '');
  /** Where the search box looks: inside a tenant the person works in, that tenant first. */
  private readonly searchTenant = computed(() =>
    this.session.oversight() ? null : this.session.tenant(),
  );
  protected readonly searchLabel = computed(() => {
    const tenant = this.searchTenant();
    return tenant ? `Search ${this.session.shown()?.name ?? tenant}` : 'Search all your tenants';
  });

  private readonly auth = inject(AuthService);
  private readonly problems = inject(ProblemService);
  private readonly navigate = inject(HARD_NAVIGATION);
  private readonly injector = inject(Injector);
  private readonly host: HTMLElement = inject(ElementRef).nativeElement;

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
    // The person-level stream follows the tenant the pages show; on the person-level pages, and
    // wherever no tenant is shown, it is held on the person's first tenant, so that the bell and the
    // person-level lists are live everywhere (docs/adr/0054 D1).
    effect(() => this.stream.personal(this.session.memberships()[0]?.tenant.slug ?? null));
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
   * person's session there too and comes back to the login page (docs/adr/0031 D4). The other tabs
   * of the application start anew as well ({@link SessionService.signedOut}).
   */
  protected async signOut(): Promise<void> {
    try {
      const next = await this.auth.logout();
      this.session.signedOut();
      this.navigate(next ?? '/login');
    } catch (error) {
      this.problems.report(error);
    }
  }

  /**
   * Opens the results of what the box holds: inside a tenant the person works in, that tenant's
   * search, which offers every tenant's next; anywhere else — and in a tenant a global
   * administrator only oversees, whose work they do not see — every tenant's (docs/adr/0023 D4).
   */
  protected search(): void {
    const q = this.query().trim();
    if (!q) {
      return;
    }
    const tenant = this.searchTenant();
    void this.router.navigate(tenant ? ['/t', tenant, 'search'] : ['/me', 'search'], {
      queryParams: { q },
    });
  }

  protected switchTenant(slug: string): void {
    void this.router.navigate(['/t', slug]);
  }

  /**
   * Opens or closes the assistant; opened, it takes the keyboard into its input. The panel is
   * loaded apart from the shell, once the tenant's chat is available, so it is found by its id.
   */
  protected toggleChat(): void {
    const open = !this.chat.open();
    this.chat.setOpen(open);
    if (open) {
      afterNextRender(() => this.host.querySelector<HTMLElement>('#chat-panel textarea')?.focus(), {
        injector: this.injector,
      });
    }
  }

  /**
   * Escape closes the assistant where it lies over the content, and the toggle gets the keyboard;
   * the Escape that ends a composition (an IME) does not. A component's host names no event type.
   */
  protected closeOnEscape(event: Event): void {
    if (!(event as KeyboardEvent).isComposing && this.overlaid()) {
      this.chat.setOpen(false);
      this.host.querySelector<HTMLElement>('.chat-toggle')?.focus();
    }
  }

  /**
   * Where the assistant lies over the content, the focus moving into the page beneath it — the
   * navigation or the content, by the keyboard or by a click — closes it, so that the focus never
   * stands on a control the panel covers, and the part of the page beside it stays usable. The
   * focus leaving the panel is no such sign: the input lets go of it while a turn runs, the window
   * loses it, a toast takes it, and the top bar with the toggle is not covered.
   */
  protected closeOverPage(): void {
    if (this.overlaid()) {
      this.chat.setOpen(false);
    }
  }

  /** Whether the assistant is open over the content; the window's width is asked as it matters. */
  private overlaid(): boolean {
    return (
      this.chat.available() &&
      this.chat.open() &&
      (this.host.ownerDocument.defaultView?.matchMedia?.(overlayQuery).matches ?? false)
    );
  }
}
