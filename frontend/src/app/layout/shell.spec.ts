import { ScrollDispatcher } from '@angular/cdk/scrolling';
import { provideLocationMocks } from '@angular/common/testing';
import { Component, computed, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { MenuItem, MessageService } from 'primeng/api';
import { Menu } from 'primeng/menu';
import { Select } from 'primeng/select';
import { Observable, of, throwError } from 'rxjs';
import type { MockInstance } from 'vitest';
import { ChatAvailability, Me, Membership, Project } from '../api/models';
import { AuthService } from '../core/auth.service';
import { ChatEntry, ChatService } from '../core/chat.service';
import { HARD_NAVIGATION, HardNavigation } from '../core/hard-navigation';
import { EventStreamService, StreamStatus } from '../core/event-stream.service';
import { InboxService } from '../core/inbox.service';
import { ProjectsService } from '../core/projects.service';
import { OpenableTenant, SessionService } from '../core/session.service';
import { TenantService } from '../core/tenant.service';
import { VersionInfo, VersionService } from '../core/version.service';
import { NewProjectDialog } from '../features/project/new-project-dialog';
import { ThemePreference, ThemeService } from '../theme/theme.service';
import { initials, Shell } from './shell';

const acme: Membership = {
  role: 'admin',
  tenant: { name: 'Acme Corp', slug: 'acme' },
  origins: [{ source: 'grant', role: 'admin' }],
};
const globex: Membership = {
  role: 'member',
  tenant: { name: 'Globex', slug: 'globex' },
  origins: [{ source: 'grant', role: 'member' }],
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
    projects: { isLoading: WritableSignal<boolean> };
  };
  let status: WritableSignal<StreamStatus>;
  let personal: MockInstance<(tenant: string | null) => void>;
  let unread: WritableSignal<number>;
  let canCreateProjects: WritableSignal<boolean>;
  let isAdmin: WritableSignal<boolean>;
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
    projects = { list: signal<Project[]>([]), projects: { isLoading: signal(false) } };
    status = signal<StreamStatus>('idle');
    personal = vi.fn<(tenant: string | null) => void>();
    unread = signal(0);
    canCreateProjects = signal(false);
    isAdmin = signal(false);
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
              ...memberships().map(({ tenant: t, role }) => ({ ...t, role })),
              ...roleless(),
            ]);
            return {
              person,
              memberships,
              tenants,
              tenant,
              membership: computed(() => memberships().find((m) => m.tenant.slug === tenant())),
              shown: computed(() => tenants().find((t) => t.slug === tenant())),
              soleTenant: computed(() => (tenants().length === 1 ? tenants()[0].slug : null)),
              oversight,
              signedOut,
            };
          })(),
        },
        { provide: ProjectsService, useValue: projects },
        { provide: TenantService, useValue: { canCreateProjects, isAdmin } },
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

  describe('the tenant in the top bar', () => {
    it('shows the name of the only tenant and no switch', async () => {
      const { page } = await render();

      expect(text(page, 'tenant-name')).toBe('Acme Corp');
      expect(page.querySelector('[data-testid="tenant-switch"]')).toBeNull();
    });

    // docs/adr/0023 D4 as amended 2026-10-05: the start page is "next for me", for one tenant too.
    it('leads to the only tenant by its name on a page that belongs to no tenant, and offers no switch', async () => {
      tenant.set(null);

      const { page } = await render();

      const name = page.querySelector('[data-testid="tenant-name"]');
      expect(name?.tagName).toBe('A');
      expect(name?.getAttribute('href')).toBe('/t/acme');
      expect(name?.textContent?.trim()).toBe('Acme Corp');
      expect(page.querySelector('[data-testid="tenant-switch"]')).toBeNull();
    });

    it('shows no name on a page that belongs to no tenant for a person without one', async () => {
      tenant.set(null);
      memberships.set([]);

      const { page } = await render();

      expect(page.querySelector('[data-testid="tenant-name"]')).toBeNull();
      expect(page.querySelector('[data-testid="tenant-switch"]')).toBeNull();
    });

    it('offers a switch over the tenants of the person when there are several', async () => {
      memberships.set([acme, globex]);

      const { fixture, page } = await render();

      expect(page.querySelector('[data-testid="tenant-name"]')).toBeNull();
      const select = fixture.debugElement.query(By.directive(Select));
      expect((select.componentInstance as Select).options()).toEqual([acme.tenant, globex.tenant]);
      expect(select.nativeElement).toBe(page.querySelector('[data-testid="tenant-switch"]'));
      expect(select.nativeElement.querySelector('.p-select-label').textContent.trim()).toBe(
        'Acme Corp',
      );
    });

    it('asks to choose a tenant while the page belongs to none', async () => {
      memberships.set([acme, globex]);
      tenant.set(null);

      const { page } = await render();

      expect(
        page.querySelector('[data-testid="tenant-switch"] .p-select-label')?.textContent?.trim(),
      ).toBe('Choose a tenant');
    });

    it('goes to the tenant that is chosen in the switch', async () => {
      memberships.set([acme, globex]);
      const { fixture } = await render();

      fixture.debugElement
        .query(By.directive(Select))
        .triggerEventHandler('ngModelChange', 'globex');

      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'globex']);
    });

    it('offers a global administrator the tenants they hold no role in too, marked so (docs/adr/0034 D2)', async () => {
      roleless.set([{ slug: 'initech', name: 'Initech', role: null }]);

      const { fixture } = await render();

      const select = fixture.debugElement.query(By.directive(Select));
      expect((select.componentInstance as Select).options()).toEqual([
        acme.tenant,
        { slug: 'initech', name: 'Initech (no role)' },
      ]);
    });

    it('names the only tenant a global administrator holds no role in', async () => {
      memberships.set([]);
      roleless.set([{ slug: 'initech', name: 'Initech', role: null }]);
      tenant.set('initech');

      const { page } = await render();

      expect(text(page, 'tenant-name')).toBe('Initech');
    });

    it('follows the tenant of the page', async () => {
      memberships.set([acme, globex]);
      const { fixture, page } = await render();

      tenant.set('globex');
      await fixture.whenStable();

      expect(
        page.querySelector('[data-testid="tenant-switch"] .p-select-label')?.textContent?.trim(),
      ).toBe('Globex');
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
        'Search all your tenants',
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
      isAdmin.set(true);

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

        page.querySelector<HTMLElement>('[data-testid="nav-overview"]')?.focus();
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
    it('shows the version and the commit of the backend', async () => {
      const { page } = await render();

      expect(version).toHaveBeenCalledOnce();
      expect(text(page, 'version')).toBe('1.0.0 (abc)');
    });

    it('says the backend is unreachable when its version cannot be read', async () => {
      version.mockReturnValue(throwError(() => new Error('connection refused')));

      const { page } = await render();

      expect(text(page, 'version')).toBe('backend unreachable');
    });
  });

  describe('the navigation', () => {
    it('links the overview, the board, the tickets, the members, the time and the settings of the tenant', async () => {
      const { page } = await render();

      const links = [
        'nav-overview',
        'nav-board',
        'nav-tickets',
        'nav-members',
        'nav-time',
        'nav-settings',
      ].map((testId) => [
        page.querySelector(`[data-testid="${testId}"]`)?.getAttribute('href'),
        page.querySelector(`[data-testid="${testId}"]`)?.textContent?.trim(),
      ]);
      expect(links).toEqual([
        ['/t/acme', 'Overview'],
        ['/t/acme/board', 'Board'],
        ['/t/acme/tickets', 'Tickets'],
        ['/t/acme/members', 'Members'],
        ['/t/acme/time', 'Time'],
        ['/t/acme/settings', 'Settings'],
      ]);
    });

    // docs/adr/0018 D4: the tenant's board stands beside its front page.
    it('links the board of the tenant right after its overview', async () => {
      const { page } = await render();

      expect(
        page
          .querySelector('[data-testid="nav-board"]')
          ?.previousElementSibling?.getAttribute('data-testid'),
      ).toBe('nav-overview');
    });

    // docs/adr/0018 D5, docs/adr/0023 D4: the list of the tenant's tickets, beside its board.
    it('links the tickets of the tenant right after its board', async () => {
      const { page } = await render();

      expect(
        page
          .querySelector('[data-testid="nav-tickets"]')
          ?.previousElementSibling?.getAttribute('data-testid'),
      ).toBe('nav-board');
    });

    it('lists the projects of the tenant, each linked to its board', async () => {
      projects.list.set([project('COW', 'Cowork'), project('OPS', 'Operations')]);

      const { page } = await render();

      const cow = page.querySelector('[data-testid="nav-project-COW"]');
      expect(cow?.getAttribute('href')).toBe('/t/acme/p/COW/board');
      expect(cow?.querySelector('.key')?.textContent).toBe('COW');
      expect(cow?.querySelector('.name')?.textContent).toBe('Cowork');
      expect(page.querySelector('[data-testid="nav-project-OPS"]')?.getAttribute('href')).toBe(
        '/t/acme/p/OPS/board',
      );
      expect(page.querySelector('.empty')).toBeNull();
    });

    it('says there are no projects yet when the tenant has none', async () => {
      const { page } = await render();

      expect(page.querySelector('.item.empty')?.textContent).toBe('No projects yet');
    });

    it('does not say there are no projects while they still load', async () => {
      projects.projects.isLoading.set(true);

      const { page } = await render();

      expect(page.querySelector('.item.empty')).toBeNull();
    });

    it('offers the accounts page to an administrator of the tenant only', async () => {
      const { page, fixture } = await render();
      expect(page.querySelector('[data-testid="nav-accounts"]')).toBeNull();

      isAdmin.set(true);
      await fixture.whenStable();

      expect(page.querySelector('[data-testid="nav-accounts"]')?.getAttribute('href')).toBe(
        '/t/acme/accounts',
      );
    });

    it('offers the group mappings to an administrator of the tenant only, beside the accounts', async () => {
      const { page, fixture } = await render();
      expect(page.querySelector('[data-testid="nav-group-mappings"]')).toBeNull();

      isAdmin.set(true);
      await fixture.whenStable();

      const link = page.querySelector('[data-testid="nav-group-mappings"]');
      expect(link?.getAttribute('href')).toBe('/t/acme/group-mappings');
      expect(link?.textContent).toBe('Group mappings');
      expect(link?.previousElementSibling?.getAttribute('data-testid')).toBe('nav-accounts');
    });

    // docs/adr/0026 D6: the tenant's audit record is its administrators'.
    it('offers the audit record to an administrator of the tenant only, after the group mappings', async () => {
      const { page, fixture } = await render();
      expect(page.querySelector('[data-testid="nav-audit"]')).toBeNull();

      isAdmin.set(true);
      await fixture.whenStable();

      const link = page.querySelector('[data-testid="nav-audit"]');
      expect(link?.getAttribute('href')).toBe('/t/acme/audit');
      expect(link?.textContent).toBe('Audit record');
      expect(link?.previousElementSibling?.getAttribute('data-testid')).toBe('nav-group-mappings');
    });

    // docs/adr/0035 D5: the tokens that can act in the tenant are its administrators'.
    it('offers the tokens of the tenant to an administrator of the tenant only, after the audit record', async () => {
      const { page, fixture } = await render();
      expect(page.querySelector('[data-testid="nav-tenant-tokens"]')).toBeNull();

      isAdmin.set(true);
      await fixture.whenStable();

      const link = page.querySelector('[data-testid="nav-tenant-tokens"]');
      expect(link?.getAttribute('href')).toBe('/t/acme/tokens');
      expect(link?.textContent).toBe('Tokens');
      expect(link?.previousElementSibling?.getAttribute('data-testid')).toBe('nav-audit');
    });

    it('offers the deleted tickets to an administrator of the tenant only (docs/adr/0024 D1)', async () => {
      const { page, fixture } = await render();
      expect(page.querySelector('[data-testid="nav-deleted-tickets"]')).toBeNull();

      isAdmin.set(true);
      await fixture.whenStable();

      const link = page.querySelector('[data-testid="nav-deleted-tickets"]');
      expect(link?.getAttribute('href')).toBe('/t/acme/deleted-tickets');
      expect(link?.textContent).toBe('Deleted tickets');
      expect(link?.previousElementSibling?.getAttribute('data-testid')).toBe('nav-time');
    });

    // docs/adr/0034 D2: a global administrator without a role in the tenant sees its
    // administration — the members, the group mappings, the settings — and none of its work.
    it('offers a global administrator without a role the administration only', async () => {
      memberships.set([]);
      roleless.set([{ slug: 'acme', name: 'Acme Corp', role: null }]);
      oversight.set(true);
      projects.list.set([project('COW', 'Cowork')]);

      const { page } = await render();

      const shown = [...page.querySelectorAll('nav a.item')].map((link) =>
        link.getAttribute('data-testid'),
      );
      expect(shown).toEqual([
        'nav-next',
        'nav-inbox',
        'nav-assigned',
        'nav-decisions',
        'nav-overview',
        'nav-members',
        'nav-group-mappings',
        'nav-settings',
        'nav-design',
      ]);
      expect(page.querySelector('[data-testid="nav-new-project"]')).toBeNull();
      expect(page.textContent).not.toContain('Projects');
    });

    it('has no tenant navigation on a page that belongs to no tenant', async () => {
      tenant.set(null);
      canCreateProjects.set(true);
      isAdmin.set(true);
      projects.list.set([project('COW', 'Cowork')]);

      const { page } = await render();

      expect(page.querySelector('[data-testid="nav-overview"]')).toBeNull();
      expect(page.querySelector('[data-testid="nav-members"]')).toBeNull();
      expect(page.querySelector('[data-testid="nav-accounts"]')).toBeNull();
      expect(page.querySelector('[data-testid="nav-group-mappings"]')).toBeNull();
      expect(page.querySelector('[data-testid="nav-board"]')).toBeNull();
      expect(page.querySelector('[data-testid="nav-tickets"]')).toBeNull();
      expect(page.querySelector('[data-testid="nav-time"]')).toBeNull();
      expect(page.querySelector('[data-testid="nav-settings"]')).toBeNull();
      expect(page.querySelector('[data-testid="nav-new-project"]')).toBeNull();
      expect(page.querySelector('[data-testid="nav-project-COW"]')).toBeNull();
    });

    it('links the design preview in a development build', async () => {
      const { page } = await render();

      const link = page.querySelector('[data-testid="nav-design"]');
      expect(link?.getAttribute('href')).toBe('/dev/design');
      expect(link?.textContent).toContain('Design preview');
    });

    it('marks only the link of the page that is open as active', async () => {
      projects.list.set([project('COW', 'Cowork')]);
      const { fixture, page } = await render();
      const active = () =>
        [...page.querySelectorAll('a.item.active')].map((link) => link.getAttribute('data-testid'));

      await TestBed.inject(Router).navigateByUrl('/t/acme');
      await fixture.whenStable();
      expect(active()).toEqual(['nav-overview']);

      // The dashboard keeps its filters in its address (docs/adr/0018 D6).
      await TestBed.inject(Router).navigateByUrl(
        '/t/acme?project=COW&from=2026-09-01&to=2026-09-30',
      );
      await fixture.whenStable();
      expect(active()).toEqual(['nav-overview']);

      await TestBed.inject(Router).navigateByUrl('/t/acme/board');
      await fixture.whenStable();
      expect(active()).toEqual(['nav-board']);

      await TestBed.inject(Router).navigateByUrl('/t/acme/board?project=COW');
      await fixture.whenStable();
      expect(active()).toEqual(['nav-board']);

      await TestBed.inject(Router).navigateByUrl('/t/acme/tickets');
      await fixture.whenStable();
      expect(active()).toEqual(['nav-tickets']);

      // The list stays active whatever it is filtered by; a ticket's own page is not the list.
      await TestBed.inject(Router).navigateByUrl('/t/acme/tickets?state=filed&project=COW');
      await fixture.whenStable();
      expect(active()).toEqual(['nav-tickets']);

      await TestBed.inject(Router).navigateByUrl('/t/acme/tickets/COW-12');
      await fixture.whenStable();
      expect(active()).toEqual([]);

      await TestBed.inject(Router).navigateByUrl('/t/acme/members');
      await fixture.whenStable();
      expect(active()).toEqual(['nav-members']);

      await TestBed.inject(Router).navigateByUrl('/t/acme/time');
      await fixture.whenStable();
      expect(active()).toEqual(['nav-time']);

      await TestBed.inject(Router).navigateByUrl('/t/acme/settings');
      await fixture.whenStable();
      expect(active()).toEqual(['nav-settings']);

      await TestBed.inject(Router).navigateByUrl('/t/acme/p/COW/board');
      await fixture.whenStable();
      expect(active()).toEqual(['nav-project-COW']);
    });
  });
});

describe('Shell, creating a project', () => {
  let canCreateProjects: WritableSignal<boolean>;

  beforeEach(() => {
    canCreateProjects = signal(false);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([{ path: '**', component: Page }]),
        provideLocationMocks(),
        MessageService,
        {
          provide: SessionService,
          useValue: {
            person: signal<Me | undefined>(ada),
            memberships: signal<Membership[]>([acme]),
            tenants: signal<OpenableTenant[]>([{ ...acme.tenant, role: acme.role }]),
            tenant: signal<string | null>('acme'),
            membership: signal<Membership | undefined>(acme),
            shown: signal<OpenableTenant | undefined>({ ...acme.tenant, role: acme.role }),
            soleTenant: signal<string | null>('acme'),
            oversight: signal(false),
          },
        },
        {
          provide: ProjectsService,
          useValue: { list: signal<Project[]>([]), projects: { isLoading: signal(false) } },
        },
        { provide: TenantService, useValue: { canCreateProjects, isAdmin: signal(false) } },
        {
          provide: EventStreamService,
          useValue: { status: signal<StreamStatus>('idle'), personal: vi.fn() },
        },
        { provide: InboxService, useValue: { count: () => 0 } },
        {
          provide: ThemeService,
          useValue: { preference: signal<ThemePreference>('system'), cycle: vi.fn() },
        },
        { provide: VersionService, useValue: { get: () => of(backend) } },
        { provide: ChatService, useValue: new FakeChat() },
      ],
    });
  });

  async function render() {
    const fixture = TestBed.createComponent(Shell);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  const plus = (page: HTMLElement) =>
    page.querySelector<HTMLButtonElement>('[data-testid="nav-new-project"]');

  it('offers a plus next to the projects only to a person who may create one', async () => {
    const { fixture, page } = await render();
    expect(plus(page)).toBeNull();

    canCreateProjects.set(true);
    await fixture.whenStable();

    expect(plus(page)?.getAttribute('aria-label')).toBe('New project');
    expect(plus(page)?.closest('.heading')?.textContent).toContain('Projects');
  });

  it('opens the dialog for a new project from the plus', async () => {
    canCreateProjects.set(true);
    const { fixture, page } = await render();
    const dialog = fixture.debugElement.query(By.directive(NewProjectDialog));
    expect((dialog.componentInstance as NewProjectDialog).visible()).toBe(false);

    plus(page)?.click();
    await fixture.whenStable();

    expect((dialog.componentInstance as NewProjectDialog).visible()).toBe(true);
  });

  it('closes the dialog when it asks to be closed', async () => {
    canCreateProjects.set(true);
    const { fixture, page } = await render();
    const dialog = fixture.debugElement.query(By.directive(NewProjectDialog));
    plus(page)?.click();
    await fixture.whenStable();

    dialog.componentInstance.visible.set(false);
    await fixture.whenStable();

    expect((dialog.componentInstance as NewProjectDialog).visible()).toBe(false);
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
