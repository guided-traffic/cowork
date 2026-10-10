import { computed, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { provideRouter, Router } from '@angular/router';
import { MessageService } from 'primeng/api';
import { Dialog } from 'primeng/dialog';
import type { MockInstance } from 'vitest';
import { Me, Team } from '../../api/models';
import { OpenableTenant, SessionService } from '../../core/session.service';
import { TenantsService } from '../../core/tenants.service';
import { AllTeams } from './all-teams';
import { FirstTenant } from './first-tenant';

const hans: Me = {
  id: 'p1',
  display_name: 'Hans',
  username: 'hans',
  global_admin: true,
  local: true,
  password_change_required: false,
  memberships: [],
};

const made: Team = {
  slug: 'initech',
  name: 'Initech',
  version: 1,
  time_visible_to_members: false,
  members_create_projects: true,
  created_at: '2026-10-10T10:00:00Z',
  updated_at: '2026-10-10T10:00:00Z',
};

describe('AllTeams', () => {
  let person: WritableSignal<Me | undefined>;
  let tenants: WritableSignal<OpenableTenant[]>;
  let listed: WritableSignal<boolean>;
  let listing: WritableSignal<boolean>;
  let create: MockInstance<TenantsService['create']>;
  let navigate: MockInstance<Router['navigate']>;

  beforeEach(() => {
    person = signal<Me | undefined>(hans);
    tenants = signal<OpenableTenant[]>([
      { slug: 'acme', name: 'Acme Corp', role: 'admin' },
      { slug: 'globex', name: 'Globex', role: null },
    ]);
    listed = signal(true);
    listing = signal(false);
    create = vi.fn<TenantsService['create']>().mockResolvedValue(made);
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        MessageService,
        {
          provide: SessionService,
          useValue: {
            person,
            tenants: computed(() => tenants()),
            installation: { hasValue: () => listed(), isLoading: () => listing() },
          },
        },
        { provide: TenantsService, useValue: { create } },
      ],
    });
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
  });

  async function render(): Promise<{ fixture: ComponentFixture<AllTeams>; page: HTMLElement }> {
    const fixture = TestBed.createComponent(AllTeams);
    await fixture.whenStable();
    return { fixture, page: fixture.nativeElement as HTMLElement };
  }

  /** Lets what a submit started finish, and shows it. */
  async function settle(fixture: ComponentFixture<AllTeams>) {
    await new Promise((resolve) => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  const tiles = (page: HTMLElement) =>
    [...page.querySelectorAll('a.tenant')].map((tile) => [
      tile.getAttribute('href'),
      tile.querySelector('.name')?.textContent,
      tile.querySelector('.muted')?.textContent?.trim(),
    ]);

  it('lists every team of the installation for a global administrator, with their role or none', async () => {
    const { page } = await render();

    expect(page.querySelector('h1')?.textContent).toBe('All teams');
    expect(tiles(page)).toEqual([
      ['/t/acme', 'Acme Corp', 'acme · admin'],
      ['/t/globex', 'Globex', 'globex · no role'],
    ]);
  });

  it('shows a skeleton while the teams of the installation are listed the first time', async () => {
    listed.set(false);
    listing.set(true);

    const { page } = await render();

    expect(page.querySelector('p-skeleton')).not.toBeNull();
    expect(tiles(page)).toEqual([]);
  });

  it('shows anybody else their own teams, and offers no new one', async () => {
    person.set({ ...hans, global_admin: false });
    tenants.set([{ slug: 'acme', name: 'Acme Corp', role: 'member' }]);

    const { page } = await render();

    expect(page.querySelector('h1')?.textContent).toBe('Your teams');
    expect(tiles(page)).toEqual([['/t/acme', 'Acme Corp', 'acme · member']]);
    expect(page.querySelector('[data-testid="new-team"]')).toBeNull();
  });

  // docs/adr/0005 D5, docs/adr/0023 D4 as amended 2026-10-10.
  describe('a new team', () => {
    const dialog = (fixture: ComponentFixture<AllTeams>) =>
      fixture.debugElement.query(By.directive(Dialog)).componentInstance as Dialog;
    const form = (fixture: ComponentFixture<AllTeams>) =>
      fixture.debugElement.query(By.directive(FirstTenant));
    const field = (page: HTMLElement, testId: string) =>
      page.querySelector<HTMLInputElement>(`[data-testid="${testId}"]`) as HTMLInputElement;

    function typeInto(fixture: ComponentFixture<AllTeams>, testId: string, value: string) {
      const input = field(fixture.nativeElement as HTMLElement, testId);
      input.value = value;
      input.dispatchEvent(new Event('input'));
      fixture.detectChanges();
    }

    it('opens the form of the first team in a dialog, without its heading', async () => {
      const { fixture, page } = await render();
      expect(form(fixture)).toBeNull();

      page.querySelector<HTMLButtonElement>('[data-testid="new-team"]')?.click();
      await settle(fixture);

      expect(dialog(fixture).visible()).toBe(true);
      expect(page.querySelector('.p-dialog-title')?.textContent).toBe('New team');
      expect(form(fixture)).not.toBeNull();
      expect(page.querySelector('[data-testid="first-tenant"] h1')).toBeNull();
      expect(page.querySelector('[data-testid="first-tenant"] .lead')?.textContent).toContain(
        'The team you create makes you its administrator, and its dashboard opens.',
      );
      expect(field(page, 'first-tenant-slug')).not.toBeNull();
    });

    it("creates the team and opens its dashboard, the person its administrator", async () => {
      const { fixture, page } = await render();
      page.querySelector<HTMLButtonElement>('[data-testid="new-team"]')?.click();
      await settle(fixture);

      typeInto(fixture, 'first-tenant-slug', 'initech');
      typeInto(fixture, 'first-tenant-name', 'Initech');
      page.querySelector('form')?.dispatchEvent(new Event('submit', { cancelable: true }));
      await settle(fixture);

      expect(create).toHaveBeenCalledExactlyOnceWith(
        { slug: 'initech', name: 'Initech' },
        expect.any(String),
      );
      expect(navigate).toHaveBeenCalledExactlyOnceWith(['/t', 'initech']);
    });

    it('stays open while the team is on its way, so that a refusal lands in its form', async () => {
      let finish: (team: Team) => void = () => undefined;
      create.mockReturnValue(new Promise<Team>((resolve) => (finish = resolve)));
      const { fixture, page } = await render();
      page.querySelector<HTMLButtonElement>('[data-testid="new-team"]')?.click();
      await settle(fixture);
      typeInto(fixture, 'first-tenant-slug', 'initech');
      typeInto(fixture, 'first-tenant-name', 'Initech');

      page.querySelector('form')?.dispatchEvent(new Event('submit', { cancelable: true }));
      await settle(fixture);

      expect(dialog(fixture).closable()).toBe(false);
      expect(dialog(fixture).dismissableMask()).toBe(false);
      document.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
      );
      await settle(fixture);
      expect(dialog(fixture).visible()).toBe(true);

      finish(made);
      await settle(fixture);
      expect(dialog(fixture).closable()).toBe(true);
    });

    it('starts empty each time it opens', async () => {
      const { fixture, page } = await render();
      page.querySelector<HTMLButtonElement>('[data-testid="new-team"]')?.click();
      await settle(fixture);
      typeInto(fixture, 'first-tenant-slug', 'half');

      dialog(fixture).visible.set(false);
      await settle(fixture);
      page.querySelector<HTMLButtonElement>('[data-testid="new-team"]')?.click();
      await settle(fixture);

      expect(field(page, 'first-tenant-slug').value).toBe('');
    });
  });
});
