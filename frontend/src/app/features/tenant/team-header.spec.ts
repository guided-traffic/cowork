import { Location } from '@angular/common';
import { provideLocationMocks } from '@angular/common/testing';
import { Component, computed, signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { SessionService } from '../../core/session.service';
import { TeamHeader } from './team-header';

@Component({ imports: [TeamHeader], template: '<app-team-header />' })
class Page {}

describe('TeamHeader', () => {
  let tenant: WritableSignal<string | null>;
  let oversight: WritableSignal<boolean>;

  beforeEach(() => {
    tenant = signal<string | null>('acme');
    oversight = signal(false);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([{ path: '**', component: Page }]),
        provideLocationMocks(),
        {
          provide: SessionService,
          useValue: {
            tenant,
            oversight,
            shown: computed(() =>
              tenant() === 'acme'
                ? { slug: 'acme', name: 'Acme Corp', role: oversight() ? null : 'admin' }
                : undefined,
            ),
          },
        },
      ],
    });
  });

  async function at(url: string): Promise<{ harness: RouterTestingHarness; page: HTMLElement }> {
    const harness = await RouterTestingHarness.create(url);
    await harness.fixture.whenStable();
    return { harness, page: harness.routeNativeElement as HTMLElement };
  }

  const tabs = (page: HTMLElement) =>
    [...page.querySelectorAll('nav.tabs a')].map((link) => [
      link.textContent?.trim(),
      link.getAttribute('href'),
      link.classList.contains('active'),
    ]);

  it("names the team, as the page's heading", async () => {
    const { page } = await at('/t/acme');

    expect(page.querySelector('h1')?.textContent).toBe('Acme Corp');
  });

  it('names the team by its slug while its name is not known', async () => {
    tenant.set('globex');

    const { page } = await at('/t/globex');

    expect(page.querySelector('h1')?.textContent).toBe('globex');
  });

  // docs/adr/0018 D6 as amended 2026-10-10: the board, the tickets and the time are the dashboard's tabs.
  it('has the dashboard, the board, the tickets and the time as tabs, each at its own address', async () => {
    const { page } = await at('/t/acme');

    expect(page.querySelector('nav.tabs')?.getAttribute('aria-label')).toBe('Views of the team');
    expect(tabs(page)).toEqual([
      ['Overview', '/t/acme', true],
      ['Board', '/t/acme/board', false],
      ['Tickets', '/t/acme/tickets', false],
      ['Time', '/t/acme/time', false],
    ]);
    expect(page.querySelector('nav.tabs a.active')?.getAttribute('aria-current')).toBe('page');
  });

  it.each([
    ['/t/acme?project=COW&from=2026-09-01', 'Overview'],
    ['/t/acme/board', 'Board'],
    ['/t/acme/board?project=COW', 'Board'],
    ['/t/acme/tickets', 'Tickets'],
    ['/t/acme/tickets?state=filed&project=COW', 'Tickets'],
    ['/t/acme/time', 'Time'],
  ])('marks the tab of %s, whatever filters its address holds', async (url, tab) => {
    const { page } = await at(url);

    expect(tabs(page).filter(([, , active]) => active).map(([label]) => label)).toEqual([tab]);
  });

  it('marks no tab on a page that is none of them', async () => {
    const { page } = await at('/t/acme/tickets/COW-12');

    expect(tabs(page).filter(([, , active]) => active)).toEqual([]);
  });

  it('offers no gear to a member: the sidebar has the team’s gear', async () => {
    const { page } = await at('/t/acme');

    expect(page.querySelector('[data-testid="team-config-gear"]')).toBeNull();
  });

  // docs/adr/0034 D2: a team a global administrator only oversees has no group in the sidebar.
  describe('for a global administrator who only oversees the team', () => {
    beforeEach(() => oversight.set(true));

    it('shows the name and no tab of its work', async () => {
      const { page } = await at('/t/acme');

      expect(page.querySelector('h1')?.textContent).toBe('Acme Corp');
      expect(page.querySelector('nav.tabs')).toBeNull();
    });

    it("offers the gear of the team's configuration beside the name, as a link of the application", async () => {
      const { harness, page } = await at('/t/acme');
      const gear = page.querySelector<HTMLElement>('[data-testid="team-config-gear"]');
      expect(gear?.getAttribute('href')).toBe('/t/acme/settings');
      expect(gear?.getAttribute('aria-label')).toBe('Configuration of Acme Corp');

      gear?.click();
      await harness.fixture.whenStable();

      expect(TestBed.inject(Router).url).toBe('/t/acme/settings');
      expect(TestBed.inject(Location).getState()).toMatchObject({ openedFromPage: true });
    });
  });
});
