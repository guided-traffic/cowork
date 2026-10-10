import { ScrollDispatcher } from '@angular/cdk/scrolling';
import { provideLocationMocks } from '@angular/common/testing';
import { Component, computed, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { MenuItem, MessageService } from 'primeng/api';
import { Menu } from 'primeng/menu';
import { Observable, of, throwError } from 'rxjs';
import type { MockInstance } from 'vitest';
import { ChatAvailability, Me, Membership, Project } from '../api/models';
import { AuthService } from '../core/auth.service';
import { ChatEntry, ChatService } from '../core/chat.service';
import { HARD_NAVIGATION, HardNavigation } from '../core/hard-navigation';
import { EventStreamService, StreamStatus } from '../core/event-stream.service';
import { InboxService } from '../core/inbox.service';
import { MyProjectsService } from '../core/my-projects.service';
import { ProjectsService } from '../core/projects.service';
import { OpenableTenant, SessionService } from '../core/session.service';
import { VersionInfo, VersionService } from '../core/version.service';
import { ThemePreference, ThemeService } from '../theme/theme.service';
import { initials, Shell } from './shell';

const acme: Membership = {
  role: 'admin',
  team: { name: 'Acme Corp', slug: 'acme' },
  tenant: { name: 'Acme Corp', slug: 'acme' },
  origins: [{ source: 'grant', role: 'admin' }],
  can_create_projects: true,
};
const globex: Membership = {
  role: 'member',
  team: { name: 'Globex', slug: 'globex' },
  tenant: { name: 'Globex', slug: 'globex' },
  origins: [{ source: 'grant', role: 'member' }],
  can_create_projects: true,
};
const ada: Me = {
  id: 'p1',
  display_name: 'Ada Lovelace',
  username: 'local:ada',
  memberships: [acme],
  global_admin: false,
  local: true,
  password_change_required: false,
};
const backend: VersionInfo = {
  version: '1.0.0',
  commit: 'abc',
  build_time: '2026-10-03T09:00:00Z',
};

function project(key: string, name: string): Project {
  return {
    id: `id-${key}`,
    key,
    name,
    description: '',
    restricted: false,
    wip_limits: {},
    version: 1,
    created_at: '2026-10-01T09:00:00Z',
    updated_at: '2026-10-01T09:00:00Z',
  };
}

@Component({ template: '<p data-testid="page">a routed page</p>' })
class Page {}

/** The part of the chat the shell and its panel read, with what a test sets. */
class FakeChat {
  readonly availabilityValue = signal<ChatAvailability | undefined>(undefined);
  readonly availability = {
    hasValue: () => this.availabilityValue() !== undefined,
    value: () => this.availabilityValue(),
  };
  readonly available = computed(() => this.availabilityValue()?.available ?? false);
  readonly providers = computed(() => this.availabilityValue()?.providers ?? []);
  readonly provider = computed(() => this.providers()[0] ?? null);
  readonly setProvider = vi.fn();
  readonly capabilities = {
    hasValue: () => false,
    value: () => undefined,
    error: () => undefined,
  };
  readonly setCapabilities = vi.fn();
  readonly open = signal(false);
  readonly setOpen = vi.fn((open: boolean) => this.open.set(open));
  readonly entries = signal<ChatEntry[]>([]);
  readonly busy = signal(false);
  readonly send = vi.fn();
  readonly stop = vi.fn();
  readonly stopElsewhere = vi.fn();
  readonly restart = vi.fn();
}

const chatAvailable: ChatAvailability = {
  available: true,
  providers: [
    { id: 'lmstudio', name: 'LM Studio', kind: 'openai', model: 'qwen/qwen3-30b-a3b-2507' },
  ],
  reason: null,
};

describe('Shell', () => {
  let memberships: WritableSignal<Membership[]>;
  /** The tenants of the installation a global administrator holds no role in. */
  let roleless: WritableSignal<OpenableTenant[]>;
  let oversight: WritableSignal<boolean>;
  let tenant: WritableSignal<string | null>;
  let person: WritableSignal<Me | undefined>;
  let projects: {
    list: WritableSignal<Project[]>;
    projects: { isLoading: WritableSignal<boolean>; hasValue: () => boolean };
  };
  /** The current team's projects are known. */
  let projectsKnown: WritableSignal<boolean>;
  /** The other teams' projects, by their slugs, and whether they are known. */
  let theirs: WritableSignal<Record<string, Project[]>>;
  let theirsKnown: WritableSignal<boolean>;
  let status: WritableSignal<StreamStatus>;
  let personal: MockInstance<(tenant: string | null) => void>;
  let unread: WritableSignal<number>;
  let logout: MockInstance<AuthService['logout']>;
  let preference: WritableSignal<ThemePreference>;
  let cycle: MockInstance<() => void>;
  let version: MockInstance<() => Observable<VersionInfo>>;
  let navigate: MockInstance<Router['navigate']>;
  let hardNavigate: MockInstance<HardNavigation>;
  let signedOut: MockInstance<() => void>;
  let chat: FakeChat;

  beforeEach(() => {
    chat = new FakeChat();
    memberships = signal<Membership[]>([acme]);
    roleless = signal<OpenableTenant[]>([]);
    oversight = signal(false);
    tenant = signal<string | null>('acme');
    person = signal<Me | undefined>(ada);
    projectsKnown = signal(false);
    projects = {
      list: signal<Project[]>([]),
      projects: { isLoading: signal(false), hasValue: () => projectsKnown() },
    };
    theirs = signal<Record<string, Project[]>>({});
    theirsKnown = signal(false);
    status = signal<StreamStatus>('idle');
    personal = vi.fn<(tenant: string | null) => void>();
    unread = signal(0);
    logout = vi.fn<AuthService['logout']>().mockResolvedValue(null);
    preference = signal<ThemePreference>('system');
    cycle = vi.fn<() => void>();
    version = vi.fn<() => Observable<VersionInfo>>(() => of(backend));
    hardNavigate = vi.fn<HardNavigation>();
    signedOut = vi.fn<() => void>();
  });

  function configure() {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([{ path: '**', component: Page }]),
        provideLocationMocks(),
        MessageService,
        {
          provide: SessionService,
          useValue: (() => {
            const tenants = computed<OpenableTenant[]>(() => [
              ...memberships().map(({ team: t, role }) => ({ ...t, role })),
              ...roleless(),
            ]);
            return {
              person,
              memberships,
              tenants,
              tenant,
              membership: computed(() => memberships().find((m) => m.team.slug === tenant())),
              shown: computed(() => tenants().find((t) => t.slug === tenant())),
              oversight,
              signedOut,
            };
          })(),
        },
        { provide: ProjectsService, useValue: projects },
        {
          provide: MyProjectsService,
          useValue: {
            of: (team: string) => theirs()[team] ?? [],
            projects: { hasValue: () => theirsKnown() },
          },
        },
        { provide: AuthService, useValue: { logout } },
        { provide: HARD_NAVIGATION, useValue: hardNavigate },
        { provide: EventStreamService, useValue: { status, personal } },
        { provide: InboxService, useValue: { count: () => unread() } },
        { provide: ThemeService, useValue: { preference, cycle } },
        { provide: VersionService, useValue: { get: version } },
        { provide: ChatService, useValue: chat },
      ],
    });
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
  }

  async function render(): Promise<{ fixture: ComponentFixture<Shell>; page: HTMLElement }> {
    configure();
    const fixture = TestBed.createComponent(Shell);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const text = (page: HTMLElement, testId: string) =>
    page.querySelector(`[data-testid="${testId}"]`)?.textContent?.trim();

  describe('the person-level pages (docs/adr/0018 D3, docs/adr/0020 D1)', () => {
    it('offers "next for me", the inbox, the tickets assigned to the person and the open decisions to every person', async () => {
      tenant.set(null);

      const { page } = await render();

      expect(page.querySelector('[data-testid="nav-next"]')?.getAttribute('href')).toBe('/me/next');
      expect(page.querySelector('[data-testid="nav-next"]')?.textContent?.trim()).toBe(
        'Next for me',
      );
      expect(page.querySelector('[data-testid="nav-inbox"]')?.getAttribute('href')).toBe(
        '/me/inbox',
      );
      expect(page.querySelector('[data-testid="nav-assigned"]')?.getAttribute('href')).toBe(
        '/me/assigned',
      );
      expect(page.querySelector('[data-testid="nav-decisions"]')?.getAttribute('href')).toBe(
        '/me/decisions',
      );
    });

    it('shows the bell without a count while nothing is unread', async () => {
      const { page } = await render();

      const bell = page.querySelector('[data-testid="bell"]');
      expect(bell?.getAttribute('href')).toBe('/me/inbox');
      expect(bell?.getAttribute('aria-label')).toBe('Inbox');
      expect(page.querySelector('[data-testid="bell-count"]')).toBeNull();
      expect(page.querySelector('[data-testid="nav-inbox-count"]')).toBeNull();
    });

    it('counts the unread notifications on the bell and beside the inbox, live', async () => {
      const { fixture, page } = await render();

      unread.set(3);
      await fixture.whenStable();

      expect(text(page, 'bell-count')).toBe('3');
      expect(text(page, 'nav-inbox-count')).toBe('3');
      expect(page.querySelector('[data-testid="bell"]')?.getAttribute('aria-label')).toBe(
        'Inbox, 3 unread',
      );
      unread.set(120);
      await fixture.whenStable();
      expect(text(page, 'bell-count')).toBe('99+');
    });

    it("holds the person-level stream on the person's first tenant, and on none without one", async () => {
      memberships.set([acme, globex]);
      const { fixture } = await render();

      expect(personal).toHaveBeenLastCalledWith('acme');
      memberships.set([]);
      await fixture.whenStable();
      expect(personal).toHaveBeenLastCalledWith(null);
    });
  });

  describe("the person's own menu", () => {
    const items = (fixture: ComponentFixture<Shell>): MenuItem[] =>
      (fixture.debugElement.query(By.directive(Menu)).componentInstance as Menu).model() ?? [];
    const labels = (fixture: ComponentFixture<Shell>) =>
      items(fixture)
        .filter((item) => !item.separator)
        .map((item) => item.label);
    const item = (fixture: ComponentFixture<Shell>, label: string) =>
      items(fixture).find((entry) => entry.label === label) as MenuItem;

    it('names the person and offers their tokens, their password and the way out', async () => {
      const { fixture, page } = await render();

      expect(labels(fixture)).toEqual([
        'Ada Lovelace',
        'Your tokens',
        'Change password',
        'Sign out',
      ]);
      expect(item(fixture, 'Ada Lovelace').disabled).toBe(true);
      expect(item(fixture, 'Your tokens').routerLink).toBe('/me/tokens');
      expect(item(fixture, 'Change password').routerLink).toBe('/password');
      expect(text(page, 'me')).toBe('AL');
    });

    it('offers no password to a person whose login is not a local account', async () => {
      person.set({ ...ada, local: false });

      const { fixture } = await render();

      expect(labels(fixture)).toEqual(['Ada Lovelace', 'Your tokens', 'Sign out']);
    });

    // docs/adr/0023 D4 as amended 2026-10-10, docs/adr/0034 D2: every team of the installation.
    it('offers a global administrator every team of the installation, first', async () => {
      person.set({ ...ada, global_admin: true });

      const { fixture } = await render();

      expect(labels(fixture)).toEqual([
        'Ada Lovelace',
        'All teams',
        'Your tokens',
        'Change password',
        'Sign out',
      ]);
      expect(item(fixture, 'All teams').routerLink).toBe('/teams');
    });

    it('offers anybody else no list of every team', async () => {
      const { fixture } = await render();

      expect(labels(fixture)).not.toContain('All teams');
    });

    it('signs out and loads the login page as a new document, which empties what the application holds', async () => {
      const { fixture } = await render();

      item(fixture, 'Sign out').command?.({});
      await fixture.whenStable();

      expect(logout).toHaveBeenCalledTimes(1);
      expect(hardNavigate).toHaveBeenCalledExactlyOnceWith('/login');
      expect(navigate).not.toHaveBeenCalledWith(['/login']);
    });

    it("tells the application's other tabs once the backend has ended the session, before it leaves", async () => {
      const { fixture } = await render();
      hardNavigate.mockImplementation(() => expect(signedOut).toHaveBeenCalledOnce());

      item(fixture, 'Sign out').command?.({});
      await fixture.whenStable();

      expect(signedOut).toHaveBeenCalledOnce();
      expect(hardNavigate).toHaveBeenCalledOnce();
    });

    it('loads the login page only after the backend has ended the session', async () => {
      let finish: () => void = () => undefined;
      logout.mockReturnValueOnce(
        new Promise<string | null>((resolve) => (finish = () => resolve(null))),
      );
      const { fixture } = await render();

      item(fixture, 'Sign out').command?.({});
      await new Promise((resolve) => setTimeout(resolve));
      expect(hardNavigate).not.toHaveBeenCalled();
      finish();
      await fixture.whenStable();
      await new Promise((resolve) => setTimeout(resolve));

      expect(hardNavigate).toHaveBeenCalledExactlyOnceWith('/login');
    });

    it("goes on to the identity provider's logout when the backend names one, which comes back to the login page (docs/adr/0031 D4)", async () => {
      logout.mockResolvedValueOnce('https://login.example.com/logout?client_id=cowork');
      const { fixture } = await render();

      item(fixture, 'Sign out').command?.({});
      await fixture.whenStable();
      await new Promise((resolve) => setTimeout(resolve));

      expect(hardNavigate).toHaveBeenCalledExactlyOnceWith(
        'https://login.example.com/logout?client_id=cowork',
      );
    });

    it('stays and says so when the sign-out fails', async () => {
      logout.mockRejectedValueOnce(new Error('the backend is away'));
      const { fixture } = await render();
      const toasts = vi.spyOn(TestBed.inject(MessageService), 'add');

      item(fixture, 'Sign out').command?.({});
      await fixture.whenStable();
      await new Promise((resolve) => setTimeout(resolve));

      expect(toasts).toHaveBeenCalledTimes(1);
      expect(hardNavigate).not.toHaveBeenCalled();
      expect(navigate).not.toHaveBeenCalledWith(['/login']);
      expect(signedOut).not.toHaveBeenCalled();
    });
  });

  describe('a temporary password', () => {
    it('sends the person to the password page first, and back to where they were going', async () => {
      person.set({ ...ada, password_change_required: true });

      await render();

      expect(navigate).toHaveBeenCalledWith(['/password'], { queryParams: { return: '/' } });
    });

    it('lets a person with a password of their own stay where they are', async () => {
      await render();

      expect(navigate).not.toHaveBeenCalledWith(['/password'], expect.anything());
    });
  });

  it('links the brand to the start page', async () => {
    const { page } = await render();

    const brand = page.querySelector('a.brand');
    expect(brand?.getAttribute('href')).toBe('/');
    expect(brand?.getAttribute('aria-label')).toBe('cowork, home');
    expect(brand?.querySelector('app-wordmark')).not.toBeNull();
  });

  it('shows the routed page in the content area', async () => {
    const { fixture, page } = await render();

    await TestBed.inject(Router).navigateByUrl('/somewhere');
    await fixture.whenStable();

    expect(page.querySelector('main.content [data-testid="page"]')?.textContent).toBe(
      'a routed page',
    );
  });

  it('makes the content area the scroll container a drag in the routed page scrolls and follows', async () => {
    const { fixture, page } = await render();

    await TestBed.inject(Router).navigateByUrl('/somewhere');
    await fixture.whenStable();

    // What a drop list asks for when it measures itself as a drag starts.
    const routed = page.querySelector<HTMLElement>('[data-testid="page"]')!;
    const scrolled = TestBed.inject(ScrollDispatcher)
      .getAncestorScrollContainers(routed)
      .map((scrollable) => scrollable.getElementRef().nativeElement);
    expect(scrolled).toEqual([page.querySelector('main.content')]);
  });

  it('hosts the toasts that services add through the message service', async () => {
    const { fixture, page } = await render();
    expect(page.querySelector('p-toast')).not.toBeNull();

    TestBed.inject(MessageService).add({
      severity: 'warn',
      summary: 'Ticket not found',
      detail: 'COW-9',
    });
    await fixture.whenStable();

    expect(page.querySelector('p-toast')?.textContent).toContain('Ticket not found');
  });

  // docs/adr/0023 D4 as amended 2026-10-10: the sidebar is the way between the person's teams.
  describe('the top bar', () => {
    it.each([
      ['a page of a team', 'acme'],
      ['a page of no team', null],
    ])('names no team and switches none on %s, whatever teams the person has', async (_, shown) => {
      tenant.set(shown);
      memberships.set([acme, globex]);
      roleless.set([{ slug: 'initech', name: 'Initech', role: null }]);

      const { page } = await render();

      const banner = page.querySelector('header.topbar') as HTMLElement;
      expect(banner.querySelector('[role="combobox"]')).toBeNull();
      expect(banner.querySelector('p-select')).toBeNull();
      expect(page.querySelector('[data-testid="tenant-switch"]')).toBeNull();
      expect(page.querySelector('[data-testid="tenant-name"]')).toBeNull();
      expect(banner.textContent).not.toContain('Acme Corp');
      expect(banner.textContent).not.toContain('Globex');
    });

    it('names no team for a person with one either', async () => {
      tenant.set(null);

      const { page } = await render();

      expect(page.querySelector('header.topbar')?.textContent).not.toContain('Acme Corp');
      expect(page.querySelector('header.topbar a[href="/t/acme"]')).toBeNull();
    });
  });

  describe('the search box (docs/adr/0018 D7)', () => {
    async function submit(fixture: ComponentFixture<Shell>, page: HTMLElement, words: string) {
      const box = page.querySelector('[data-testid="search-input"]') as HTMLInputElement;
      box.value = words;
      box.dispatchEvent(new Event('input'));
      fixture.detectChanges();
      box.form?.dispatchEvent(new Event('submit', { cancelable: true }));
      await fixture.whenStable();
    }

    it('searches the tenant the pages show, that tenant first', async () => {
      const { fixture, page } = await render();

      await submit(fixture, page, '  readyReplicas gate ');

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'acme', 'search'], {
        queryParams: { q: 'readyReplicas gate' },
      });
      const box = page.querySelector('[data-testid="search-input"]');
      expect(box?.getAttribute('aria-label')).toBe('Search Acme Corp');
    });

    it('searches every tenant of the person outside a tenant, and in one a global administrator only oversees', async () => {
      tenant.set(null);
      const { fixture, page } = await render();
      expect(page.querySelector('[data-testid="search-input"]')?.getAttribute('aria-label')).toBe(
        'Search all your teams',
      );

      await submit(fixture, page, 'gate');
      expect(navigate).toHaveBeenLastCalledWith(['/me', 'search'], { queryParams: { q: 'gate' } });

      tenant.set('acme');
      oversight.set(true);
      fixture.detectChanges();
      await submit(fixture, page, 'gate');
      expect(navigate).toHaveBeenLastCalledWith(['/me', 'search'], { queryParams: { q: 'gate' } });
    });

    it('searches nothing for a box of white space', async () => {
      const { fixture, page } = await render();

      await submit(fixture, page, '   ');

      expect(navigate).not.toHaveBeenCalled();
    });

    it('holds the words of the search the page shows', async () => {
      const { fixture, page } = await render();
      navigate.mockRestore();

      await TestBed.inject(Router).navigateByUrl('/t/acme/search?q=flicker%20board');
      fixture.detectChanges();
      await fixture.whenStable();

      expect((page.querySelector('[data-testid="search-input"]') as HTMLInputElement).value).toBe(
        'flicker board',
      );

      await TestBed.inject(Router).navigateByUrl('/t/acme/members');
      fixture.detectChanges();
      await fixture.whenStable();

      expect((page.querySelector('[data-testid="search-input"]') as HTMLInputElement).value).toBe(
        '',
      );
    });
  });

  describe('the live indicator', () => {
    it('shows nothing outside a tenant', async () => {
      const { page } = await render();

      expect(page.querySelector('[data-testid="live-indicator"]')).toBeNull();
    });

    it('is handed the status of the event stream', async () => {
      status.set('connecting');
      const { fixture, page } = await render();
      const state = () =>
        page.querySelector('[data-testid="live-indicator"]')?.getAttribute('data-status');
      expect(state()).toBe('connecting');

      status.set('live');
      await fixture.whenStable();
      expect(state()).toBe('live');

      status.set('polling');
      await fixture.whenStable();
      expect(state()).toBe('polling');
    });
  });

  describe('the theme button', () => {
    it.each([
      ['system', 'Theme: follows the system', 'pi-desktop'],
      ['light', 'Theme: light', 'pi-sun'],
      ['dark', 'Theme: dark', 'pi-moon'],
    ] as const)('is labelled for the %s preference', async (value, label, icon) => {
      preference.set(value);

      const { page } = await render();

      const button = page.querySelector('[data-testid="theme-toggle"]');
      expect(button?.tagName).toBe('BUTTON');
      expect(button?.getAttribute('aria-label')).toBe(label);
      expect(button?.querySelector('i')?.classList).toContain(icon);
    });

    it('changes its label when the preference changes', async () => {
      const { fixture, page } = await render();

      preference.set('dark');
      await fixture.whenStable();

      const button = page.querySelector('[data-testid="theme-toggle"]');
      expect(button?.getAttribute('aria-label')).toBe('Theme: dark');
      expect(button?.querySelector('i')?.classList).toContain('pi-moon');
    });

    it('cycles the theme when it is clicked', async () => {
      const { page } = await render();
      expect(cycle).not.toHaveBeenCalled();

      page.querySelector<HTMLButtonElement>('[data-testid="theme-toggle"]')?.click();

      expect(cycle).toHaveBeenCalledOnce();
    });
  });

  describe('the assistant', () => {
    const toggle = (page: HTMLElement) =>
      page.querySelector<HTMLButtonElement>('[data-testid="chat-toggle"]');
    const panel = (page: HTMLElement) =>
      page.querySelector<HTMLElement>('[data-testid="chat-panel"]');

    it('is neither offered nor shown while the tenant has no chat', async () => {
      chat.availabilityValue.set({ available: false, providers: [], reason: 'not_configured' });

      const { page } = await render();

      expect(toggle(page)).toBeNull();
      expect(panel(page)).toBeNull();
    });

    it('is neither offered nor shown before its availability is known', async () => {
      const { page } = await render();

      expect(toggle(page)).toBeNull();
      expect(panel(page)).toBeNull();
    });

    it('is a toggle in the top bar that says whether the panel it controls is open', async () => {
      chat.availabilityValue.set(chatAvailable);

      const { page } = await render();

      expect(toggle(page)?.tagName).toBe('BUTTON');
      expect(toggle(page)?.closest('.topbar')).not.toBeNull();
      expect(toggle(page)?.getAttribute('aria-label')).toBe('Assistant');
      expect(toggle(page)?.getAttribute('aria-expanded')).toBe('false');
      expect(toggle(page)?.getAttribute('aria-controls')).toBe('chat-panel');
      expect(page.querySelector('#chat-panel')).toBe(panel(page));
      expect(panel(page)?.hidden).toBe(true);
    });

    it('stands at the right of the content', async () => {
      chat.availabilityValue.set(chatAvailable);

      const { page } = await render();

      expect(panel(page)?.parentElement?.classList).toContain('frame');
      expect(panel(page)?.previousElementSibling?.tagName).toBe('MAIN');
    });

    it('opens from the toggle and takes the keyboard into its input', async () => {
      chat.availabilityValue.set(chatAvailable);
      const { fixture, page } = await render();

      toggle(page)?.click();
      await fixture.whenStable();

      expect(chat.setOpen).toHaveBeenCalledExactlyOnceWith(true);
      expect(panel(page)?.hidden).toBe(false);
      expect(toggle(page)?.getAttribute('aria-expanded')).toBe('true');
      expect(toggle(page)?.classList).toContain('on');
      expect(document.activeElement).toBe(page.querySelector('[data-testid="chat-input"]'));
    });

    it('closes from the toggle, which keeps the keyboard', async () => {
      chat.availabilityValue.set(chatAvailable);
      chat.open.set(true);
      const { fixture, page } = await render();
      toggle(page)?.focus();

      toggle(page)?.click();
      await fixture.whenStable();

      expect(chat.setOpen).toHaveBeenCalledExactlyOnceWith(false);
      expect(panel(page)?.hidden).toBe(true);
      expect(toggle(page)?.getAttribute('aria-expanded')).toBe('false');
      expect(document.activeElement).toBe(toggle(page));
    });

    it('is open as the person left it, without taking the keyboard', async () => {
      chat.availabilityValue.set(chatAvailable);
      chat.open.set(true);

      const { page } = await render();

      expect(panel(page)?.hidden).toBe(false);
      expect(document.activeElement).not.toBe(page.querySelector('[data-testid="chat-input"]'));
    });

    describe('lying over the content on a narrow window', () => {
      /** jsdom has no matchMedia; this one says whether the window is narrow, and records the query. */
      function windowIs(narrow: boolean): string[] {
        const queries: string[] = [];
        Object.defineProperty(window, 'matchMedia', {
          configurable: true,
          writable: true,
          value: (query: string) => {
            queries.push(query);
            return { matches: narrow, addEventListener: () => undefined };
          },
        });
        return queries;
      }

      afterEach(() => Reflect.deleteProperty(window, 'matchMedia'));

      async function opened(): Promise<{ fixture: ComponentFixture<Shell>; page: HTMLElement }> {
        chat.availabilityValue.set(chatAvailable);
        chat.open.set(true);
        return render();
      }

      const input = (page: HTMLElement) =>
        page.querySelector<HTMLTextAreaElement>('[data-testid="chat-input"]') as HTMLElement;

      function escape(target: HTMLElement, init: KeyboardEventInit = {}): void {
        target.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true, ...init }),
        );
      }

      it('closes on Escape, and the keyboard goes back to the toggle', async () => {
        const queries = windowIs(true);
        const { fixture, page } = await opened();
        input(page).focus();

        escape(input(page));
        await fixture.whenStable();

        expect(queries).toContain('(max-width: 64rem)');
        expect(chat.setOpen).toHaveBeenCalledExactlyOnceWith(false);
        expect(panel(page)?.hidden).toBe(true);
        expect(document.activeElement).toBe(toggle(page));
      });

      it('stays open on the Escape that ends a composition', async () => {
        windowIs(true);
        const { fixture, page } = await opened();

        escape(input(page), { isComposing: true });
        await fixture.whenStable();

        expect(chat.setOpen).not.toHaveBeenCalled();
        expect(panel(page)?.hidden).toBe(false);
      });

      it('closes when the focus moves into the content beneath it', async () => {
        windowIs(true);
        const { fixture, page } = await opened();
        await TestBed.inject(Router).navigateByUrl('/somewhere');
        await fixture.whenStable();

        page
          .querySelector('[data-testid="page"]')
          ?.dispatchEvent(new FocusEvent('focusin', { bubbles: true }));
        await fixture.whenStable();

        expect(chat.setOpen).toHaveBeenCalledExactlyOnceWith(false);
        expect(panel(page)?.hidden).toBe(true);
      });

      it('closes when the focus moves into the navigation beneath it', async () => {
        windowIs(true);
        const { fixture, page } = await opened();

        page.querySelector<HTMLElement>('[data-testid="nav-team-acme"]')?.focus();
        await fixture.whenStable();

        expect(chat.setOpen).toHaveBeenCalledExactlyOnceWith(false);
      });

      it('stays open when the focus goes to the top bar, which it does not cover', async () => {
        windowIs(true);
        const { fixture, page } = await opened();

        page.querySelector<HTMLElement>('[data-testid="theme-toggle"]')?.focus();
        await fixture.whenStable();

        expect(chat.setOpen).not.toHaveBeenCalled();
        expect(panel(page)?.hidden).toBe(false);
      });

      it('stays open beside the content on a wide window, for Escape and for the focus', async () => {
        windowIs(false);
        const { fixture, page } = await opened();

        escape(input(page));
        page.querySelector<HTMLElement>('[data-testid="nav-overview"]')?.focus();
        await fixture.whenStable();

        expect(chat.setOpen).not.toHaveBeenCalled();
      });

      it('stays open where the browser cannot tell the width of the window', async () => {
        const { fixture, page } = await opened();

        escape(input(page));
        await fixture.whenStable();

        expect(chat.setOpen).not.toHaveBeenCalled();
      });

      it("leaves the person's choice alone while the panel is closed, or the tenant has no chat", async () => {
        windowIs(true);
        chat.open.set(true);
        chat.availabilityValue.set({
          ...chatAvailable,
          available: false,
          reason: 'not_configured',
        });
        const { fixture, page } = await render();

        page.querySelector<HTMLElement>('[data-testid="nav-overview"]')?.focus();
        chat.availabilityValue.set(chatAvailable);
        chat.open.set(false);
        await fixture.whenStable();
        page.querySelector<HTMLElement>('[data-testid="nav-members"]')?.focus();
        await fixture.whenStable();

        expect(chat.setOpen).not.toHaveBeenCalled();
      });
    });
  });

  describe('the person', () => {
    it('shows the initials of the person in the avatar', async () => {
      const { page } = await render();

      expect(text(page, 'me')).toBe('AL');
    });

    it('shows no avatar before the person is known', async () => {
      person.set(undefined);

      const { page } = await render();

      expect(page.querySelector('[data-testid="me"]')).toBeNull();
    });
  });

  describe('the version in the footer', () => {
    it('shows the version of the backend alone, without its commit', async () => {
      const { page } = await render();

      expect(version).toHaveBeenCalledOnce();
      expect(text(page, 'version')).toBe('1.0.0');
    });

    it('says the backend is unreachable when its version cannot be read', async () => {
      version.mockReturnValue(throwError(() => new Error('connection refused')));

      const { page } = await render();

      expect(text(page, 'version')).toBe('backend unreachable');
    });
  });

  // docs/adr/0023 D4 as amended 2026-10-10: the sidebar holds the daily links — the person-level
  // pages, then every team of the person with its projects (layout/team-nav.ts).
  describe('the navigation', () => {
    const groups = (page: HTMLElement) =>
      [...page.querySelectorAll('nav.sidebar [data-team]')].map((group) => ({
        team: group.getAttribute('data-team'),
        projects: [...group.querySelectorAll('a.project')].map((link) =>
          link.getAttribute('data-testid'),
        ),
      }));

    beforeEach(() => {
      memberships.set([acme, globex]);
      projects.list.set([project('COW', 'Cowork')]);
      projectsKnown.set(true);
      theirs.set({ acme: [project('COW', 'Cowork')], globex: [project('OPS', 'Operations')] });
      theirsKnown.set(true);
    });

    it('lists every team of the person with its projects inside a team', async () => {
      const { page } = await render();

      expect(groups(page)).toEqual([
        { team: 'acme', projects: ['nav-project-acme-COW'] },
        { team: 'globex', projects: ['nav-project-globex-OPS'] },
      ]);
    });

    it('lists them on a person-level page as well', async () => {
      tenant.set(null);

      const { page } = await render();

      expect(groups(page)).toEqual([
        { team: 'acme', projects: ['nav-project-acme-COW'] },
        { team: 'globex', projects: ['nav-project-globex-OPS'] },
      ]);
    });

    it('stands between the person-level pages and the foot of the sidebar', async () => {
      const { page } = await render();

      const parts = [...(page.querySelector('nav.sidebar')?.children ?? [])].map(
        (part) => part.getAttribute('class') ?? part.tagName.toLowerCase(),
      );
      expect(parts).toEqual(['section', 'app-team-nav', 'section bottom']);
    });

    it('offers no page of a team: its dashboard has the board, the tickets and the time as tabs, its gear the rest', async () => {
      const { page } = await render();

      for (const testId of [
        'nav-overview',
        'nav-board',
        'nav-tickets',
        'nav-members',
        'nav-accounts',
        'nav-group-mappings',
        'nav-audit',
        'nav-tenant-tokens',
        'nav-time',
        'nav-deleted-tickets',
        'nav-settings',
        'nav-new-project',
      ]) {
        expect(page.querySelector(`[data-testid="${testId}"]`), testId).toBeNull();
      }
      expect(page.querySelector('nav.sidebar')?.textContent).not.toContain('Projects');
    });

    // docs/adr/0034 D2: the team a global administrator oversees without a role is no team of theirs.
    it('has no group for a team a global administrator only oversees', async () => {
      memberships.set([]);
      roleless.set([{ slug: 'acme', name: 'Acme Corp', role: null }]);
      oversight.set(true);

      const { page } = await render();

      expect(groups(page)).toEqual([]);
      const shown = [...page.querySelectorAll('nav a.item')].map((link) =>
        link.getAttribute('data-testid'),
      );
      expect(shown).toEqual(['nav-next', 'nav-inbox', 'nav-assigned', 'nav-decisions', 'nav-design']);
    });

    it('links the design preview in a development build', async () => {
      const { page } = await render();

      const link = page.querySelector('[data-testid="nav-design"]');
      expect(link?.getAttribute('href')).toBe('/dev/design');
      expect(link?.textContent).toContain('Design preview');
    });
  });
});

describe('initials', () => {
  it.each([
    ['Ada Lovelace', 'AL'],
    ['Developer', 'DE'],
    ['', '?'],
    ['   ', '?'],
    ['x', 'X'],
    ['Ada Augusta King Lovelace', 'AL'],
    ['  grace   hopper ', 'GH'],
  ])('turns %j into %j', (name, expected) => {
    expect(initials(name)).toBe(expected);
  });
});
