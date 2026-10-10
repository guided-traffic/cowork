import { Location } from '@angular/common';
import { provideLocationMocks } from '@angular/common/testing';
import { Component, computed, signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { configTabs, gearTab, isOpenedFromPage, openedFromPage } from './config-tabs';
import { TenantDashboard } from './dashboard';
import { SelfGrant } from './self-grant';
import { TeamConfig } from './team-config';

describe('configTabs', () => {
  it("gives a team's administrator every page of the configuration, in the order of its tabs", () => {
    expect(configTabs(true, false)).toEqual([
      { path: 'members', label: 'Members' },
      { path: 'accounts', label: 'Accounts' },
      { path: 'group-mappings', label: 'Group mappings' },
      { path: 'tokens', label: 'Tokens' },
      { path: 'settings', label: 'Settings' },
      { path: 'audit', label: 'Audit record' },
      { path: 'deleted-tickets', label: 'Deleted tickets' },
    ]);
  });

  it('gives a member or a viewer the members and the settings', () => {
    expect(configTabs(false, false).map((tab) => tab.path)).toEqual(['members', 'settings']);
  });

  // docs/adr/0034 D2: a global administrator without a role sees the administration only.
  it('gives a global administrator who only oversees the team its members, mappings and settings', () => {
    expect(configTabs(false, true).map((tab) => tab.path)).toEqual([
      'members',
      'group-mappings',
      'settings',
    ]);
  });
});

describe('gearTab', () => {
  it('opens the settings, which every role shows', () => {
    expect(gearTab(configTabs(true, false))).toBe('settings');
    expect(gearTab(configTabs(false, false))).toBe('settings');
    expect(gearTab(configTabs(false, true))).toBe('settings');
  });

  it('opens the first tab where the settings are not among them', () => {
    expect(gearTab([{ path: 'members', label: 'Members' }])).toBe('members');
  });
});

describe('isOpenedFromPage', () => {
  it.each([
    [openedFromPage, true],
    [{ openedFromPage: true, navigationId: 4 }, true],
    [{ navigationId: 4 }, false],
    [{ openedFromPage: 'yes' }, false],
    [null, false],
    [undefined, false],
  ])('reads %j as %s', (state, opened) => {
    expect(isOpenedFromPage(state)).toBe(opened);
  });
});

@Component({ selector: 'app-tenant-dashboard', template: '<p data-testid="dashboard">the dashboard</p>' })
class Dashboard {}

@Component({ template: '<section class="page"><h1>Members</h1><p data-testid="members">the members</p></section>' })
class MembersPage {}

@Component({ template: '<section class="page"><h1>Settings</h1><p data-testid="settings">the settings</p></section>' })
class SettingsPage {}

@Component({ template: '<p data-testid="elsewhere">another page</p>' })
class Elsewhere {}

/** The offer of a grant stands in as itself: its own spec is self-grant.spec.ts. */
@Component({ selector: 'app-self-grant', template: '<p data-testid="offer">grant yourself a role</p>' })
class Offer {}

describe('TeamConfig', () => {
  let tenant: WritableSignal<string | null>;
  let oversight: WritableSignal<boolean>;
  let admin: WritableSignal<boolean>;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    oversight = signal(false);
    admin = signal(true);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([
          { path: 'me/next', component: Elsewhere },
          { path: 't/:tenant/board', component: Elsewhere },
          {
            path: 't/:tenant',
            children: [
              { path: '', pathMatch: 'full', component: Elsewhere },
              {
                path: '',
                component: TeamConfig,
                children: [
                  { path: 'members', component: MembersPage },
                  { path: 'settings', component: SettingsPage },
                ],
              },
            ],
          },
        ]),
        provideLocationMocks(),
        {
          provide: SessionService,
          useValue: {
            tenant,
            oversight,
            shown: computed(() =>
              tenant() === 'acme' ? { slug: 'acme', name: 'Acme Corp', role: 'admin' } : undefined,
            ),
          },
        },
        { provide: TenantService, useValue: { isAdmin: admin } },
      ],
    });
    TestBed.overrideComponent(TeamConfig, {
      remove: { imports: [TenantDashboard, SelfGrant] },
      add: { imports: [Dashboard, Offer] },
    });
  });

  /** The dialog lies in place, in the page: PrimeNG's own markup, the mask around it. */
  const dialog = () => document.querySelector<HTMLElement>('.p-dialog');
  const tabs = () =>
    [...(dialog()?.querySelectorAll('nav.tabs a') ?? [])].map((link) => [
      link.textContent?.trim(),
      link.getAttribute('href'),
    ]);

  /** Lets a navigation the page started — a popstate of the back button among them — finish. */
  async function settle(harness: RouterTestingHarness): Promise<void> {
    for (let round = 0; round < 3; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      harness.detectChanges();
      await harness.fixture.whenStable();
    }
  }

  /** A page of its own, whose router follows the back button as the browser's does. */
  async function harnessed(): Promise<RouterTestingHarness> {
    const harness = await RouterTestingHarness.create();
    TestBed.inject(Router).setUpLocationChangeListener();
    return harness;
  }

  async function open(url: string, state?: Record<string, unknown>): Promise<RouterTestingHarness> {
    const harness = await harnessed();
    await TestBed.inject(Router).navigateByUrl(url, state ? { state } : {});
    harness.detectChanges();
    await harness.fixture.whenStable();
    return harness;
  }

  it("shows the page of its address in a dialog over the team's dashboard", async () => {
    await open('/t/acme/members');

    expect(document.querySelector('[data-testid="dashboard"]')).not.toBeNull();
    expect(dialog()?.getAttribute('role')).toBe('dialog');
    expect(dialog()?.getAttribute('aria-modal')).toBe('true');
    expect(document.querySelector('.p-dialog-mask')).not.toBeNull();
    expect(dialog()?.querySelector('.p-dialog-title')?.textContent).toBe('Configuration of Acme Corp');
    expect(dialog()?.querySelector('[data-testid="members"]')).not.toBeNull();
  });

  it("gives an administrator a tab for each page of the configuration, each at the page's address", async () => {
    await open('/t/acme/settings');

    expect(tabs()).toEqual([
      ['Members', '/t/acme/members'],
      ['Accounts', '/t/acme/accounts'],
      ['Group mappings', '/t/acme/group-mappings'],
      ['Tokens', '/t/acme/tokens'],
      ['Settings', '/t/acme/settings'],
      ['Audit record', '/t/acme/audit'],
      ['Deleted tickets', '/t/acme/deleted-tickets'],
    ]);
    const current = dialog()?.querySelector('nav.tabs a.active');
    expect(current?.textContent?.trim()).toBe('Settings');
    expect(current?.getAttribute('aria-current')).toBe('page');
  });

  it('gives anybody else the tabs their role shows', async () => {
    admin.set(false);

    await open('/t/acme/members');

    expect(tabs().map(([label]) => label)).toEqual(['Members', 'Settings']);
  });

  it("gives a global administrator who only oversees the team what they see of it", async () => {
    admin.set(false);
    oversight.set(true);

    await open('/t/acme/members');

    expect(tabs().map(([label]) => label)).toEqual(['Members', 'Group mappings', 'Settings']);
  });

  // docs/adr/0034 D2: the offer above the dashboard lies under the dialog, so it stands in it too.
  it('offers a global administrator who only oversees the team a role above the tab', async () => {
    admin.set(false);
    oversight.set(true);

    await open('/t/acme/members');

    const pane = dialog()?.querySelector('.pane');
    const offer = pane?.querySelector('[data-testid="offer"]');
    const page = pane?.querySelector('[data-testid="members"]');
    expect(offer).not.toBeNull();
    expect(page).not.toBeNull();
    expect(offer?.compareDocumentPosition(page as Node)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  });

  it('offers a member of the team no role in its dialog', async () => {
    admin.set(false);

    await open('/t/acme/members');

    expect(dialog()?.querySelector('[data-testid="offer"]')).toBeNull();
  });

  it('keeps the page heading for a screen reader, out of sight, since the tab says it', async () => {
    await open('/t/acme/members');

    const heading = dialog()?.querySelector('h1');
    expect(heading?.textContent).toBe('Members');
  });

  it('moves between its tabs in place of the address, keeping where it was opened from', async () => {
    const harness = await harnessed();
    const router = TestBed.inject(Router);
    const location = TestBed.inject(Location);
    await router.navigateByUrl('/t/acme/board');
    await router.navigateByUrl('/t/acme/members', { state: openedFromPage });
    await settle(harness);

    dialog()?.querySelector<HTMLElement>('[data-testid="config-tab-settings"]')?.click();
    await settle(harness);

    expect(router.url).toBe('/t/acme/settings');
    expect(dialog()?.querySelector('[data-testid="settings"]')).not.toBeNull();
    expect(location.getState()).toMatchObject(openedFromPage);

    // The tab replaced the address: back leads out of the dialog, to the page before it.
    location.back();
    await settle(harness);
    expect(router.url).toBe('/t/acme/board');
  });

  it('closes to the page before after a tab was chosen', async () => {
    const harness = await harnessed();
    const router = TestBed.inject(Router);
    await router.navigateByUrl('/t/acme/board');
    await router.navigateByUrl('/t/acme/members', { state: openedFromPage });
    await settle(harness);
    dialog()?.querySelector<HTMLElement>('[data-testid="config-tab-settings"]')?.click();
    await settle(harness);

    document.querySelector<HTMLElement>('.p-dialog-close-button')?.click();
    await settle(harness);

    expect(router.url).toBe('/t/acme/board');
  });

  it('goes back to the page before when a link of the application opened it', async () => {
    const harness = await harnessed();
    const router = TestBed.inject(Router);
    await router.navigateByUrl('/t/acme/board');
    await router.navigateByUrl('/t/acme/members', { state: openedFromPage });
    harness.detectChanges();
    await harness.fixture.whenStable();

    document.querySelector<HTMLElement>('.p-dialog-close-button')?.click();
    await settle(harness);

    expect(router.url).toBe('/t/acme/board');
  });

  it("goes to the team's dashboard when its address was opened directly, in place of the address", async () => {
    const harness = await harnessed();
    const router = TestBed.inject(Router);
    await router.navigateByUrl('/me/next');
    await router.navigateByUrl('/t/acme/settings');
    harness.detectChanges();
    await harness.fixture.whenStable();

    document.querySelector<HTMLElement>('.p-dialog-close-button')?.click();
    await settle(harness);

    expect(router.url).toBe('/t/acme');
    // The dialog's address was replaced: back leads to the page before it, not into the dialog.
    TestBed.inject(Location).back();
    await settle(harness);
    expect(router.url).toBe('/me/next');
  });

  it('closes on Escape as on its cross', async () => {
    const harness = await harnessed();
    const router = TestBed.inject(Router);
    await router.navigateByUrl('/t/acme/settings');
    harness.detectChanges();
    await harness.fixture.whenStable();

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await harness.fixture.whenStable();

    expect(router.url).toBe('/t/acme');
  });

  it('names its cross', async () => {
    await open('/t/acme/settings');

    expect(document.querySelector('.p-dialog-close-button')?.getAttribute('aria-label')).toBe(
      'Close the configuration',
    );
  });
});
