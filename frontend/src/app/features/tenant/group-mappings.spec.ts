import { HttpErrorResponse } from '@angular/common/http';
import { computed, signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Tooltip } from 'primeng/tooltip';
import type { MockInstance } from 'vitest';
import { GroupMapping, Membership, Problem, ProblemCode } from '../../api/models';
import { GroupMappingsService } from '../../core/group-mappings.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { dateTime } from '../../shared/time';
import { GroupMappings, includesYouMeaning } from './group-mappings';
import { NewMappingDialog } from './new-mapping-dialog';
import { lastAdminNotice } from './roles';

function mapping(group: string, overrides: Partial<GroupMapping> = {}): GroupMapping {
  return {
    id: `id-${group}`,
    group,
    role: 'member',
    includes_caller: false,
    version: 3,
    created_at: '2026-10-01T08:00:00Z',
    updated_at: '2026-10-03T09:30:00Z',
    ...overrides,
  };
}

/** The editor's own group, which makes them an administrator. */
const admins = mapping('cowork-admins', { role: 'admin', includes_caller: true });
const red = mapping('team-red');

/** The editor's membership: administrator through a mapping only, unless a test says otherwise. */
const mapped: Membership = {
  role: 'admin',
  team: { slug: 'acme', name: 'Acme Corp' },
  tenant: { slug: 'acme', name: 'Acme Corp' },
  origins: [{ source: 'mapping', role: 'admin' }],
};

function refusal(status: number, code: ProblemCode, detail = 'The server says no.') {
  const body: Problem = { type: 'about:blank', title: 'Refused', status, detail, code };
  return new HttpErrorResponse({ status, statusText: 'Refused', error: body });
}

describe('GroupMappings', () => {
  let list: WritableSignal<GroupMapping[]>;
  let loading: WritableSignal<boolean>;
  let error: WritableSignal<unknown>;
  let reload: MockInstance<() => boolean>;
  let tenantSlug: WritableSignal<string | null>;
  let membership: WritableSignal<Membership | undefined>;
  let personLoading: WritableSignal<boolean>;
  let isAdmin: WritableSignal<boolean>;
  let globalAdmin: WritableSignal<boolean>;
  let oversight: WritableSignal<boolean>;
  let changeRole: MockInstance<GroupMappingsService['changeRole']>;
  let remove: MockInstance<GroupMappingsService['remove']>;

  beforeEach(() => {
    list = signal<GroupMapping[]>([admins, red]);
    loading = signal(false);
    error = signal<unknown>(undefined);
    reload = vi.fn<() => boolean>().mockReturnValue(true);
    tenantSlug = signal<string | null>('acme');
    membership = signal<Membership | undefined>(mapped);
    personLoading = signal(false);
    isAdmin = signal(true);
    globalAdmin = signal(true);
    oversight = signal(false);
    changeRole = vi
      .fn<GroupMappingsService['changeRole']>()
      .mockImplementation(async (each, role) => ({ ...each, role, version: each.version + 1 }));
    remove = vi.fn<GroupMappingsService['remove']>().mockResolvedValue(undefined);
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        {
          provide: GroupMappingsService,
          useValue: {
            list,
            mappings: { isLoading: loading, error, reload },
            create: vi.fn(),
            changeRole,
            remove,
          },
        },
        {
          provide: SessionService,
          useValue: {
            tenant: tenantSlug,
            membership: computed(() => membership()),
            person: computed(() => ({ global_admin: globalAdmin() })),
            oversight,
            me: { isLoading: personLoading },
          },
        },
        { provide: TenantService, useValue: { isAdmin } },
      ],
    });
  });

  // Whatever a test spied on goes back, also when an assertion of the test has failed.
  afterEach(() => vi.restoreAllMocks());

  async function render() {
    const fixture = TestBed.createComponent(GroupMappings);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields take their model a moment after they appear. */
  async function settle(fixture: ComponentFixture<GroupMappings>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<GroupMappings>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<GroupMappings>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const row = (fixture: ComponentFixture<GroupMappings>, each: GroupMapping) =>
    el(fixture, `mapping-${each.group}`);
  const shownRole = (fixture: ComponentFixture<GroupMappings>, each: GroupMapping) =>
    el(fixture, `mapping-role-${each.group}`)
      ?.querySelector('.p-select-label')
      ?.textContent?.trim();
  function choose(fixture: ComponentFixture<GroupMappings>, each: GroupMapping, role: string) {
    fixture.debugElement
      .query(By.css(`[data-testid="mapping-role-${each.group}"]`))
      .triggerEventHandler('ngModelChange', role);
  }
  const toasts = () => vi.spyOn(TestBed.inject(MessageService), 'add');
  /** The focusable part of a row's select: PrimeNG's combobox. */
  const combobox = (fixture: ComponentFixture<GroupMappings>, each: GroupMapping) =>
    el(fixture, `mapping-role-${each.group}`)?.querySelector('[role="combobox"]');
  const heading = (fixture: ComponentFixture<GroupMappings>) => host(fixture).querySelector('h1');

  const dialog = () => document.body.querySelector('.p-confirmdialog');
  const press = (label: string) =>
    [...(dialog()?.querySelectorAll('button') ?? [])]
      .find((button) => button.textContent?.trim() === label)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

  describe('the page', () => {
    it('is headed Group mappings, says what a mapping does and offers a new one', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Group mappings');
      expect(host(fixture).querySelector('.lead')?.textContent).toContain(
        'several mapped groups give the highest of their roles',
      );
      expect(el(fixture, 'new-mapping')?.textContent?.trim()).toBe('New mapping');
      expect(el(fixture, 'mappings-global-only')).toBeNull();
    });

    it('shows a row per mapping with the group, its role and when it changed', async () => {
      const fixture = await render();

      expect([...host(fixture).querySelectorAll('th')].map((each) => each.textContent)).toEqual([
        'Group',
        'Role',
        'Changed',
        '',
      ]);
      expect(row(fixture, red)?.querySelector('code')?.textContent).toBe('team-red');
      expect(shownRole(fixture, red)).toBe('member');
      expect(shownRole(fixture, admins)).toBe('admin');
      expect(row(fixture, red)?.querySelectorAll('td')[2].textContent).toBe(
        dateTime('2026-10-03T09:30:00Z'),
      );
      const select = fixture.debugElement.query(By.css('[data-testid="mapping-role-team-red"]'));
      expect(select.componentInstance.ariaLabel()).toBe('Role of the group team-red');
      expect(el(fixture, 'mapping-remove-team-red')?.getAttribute('aria-label')).toBe(
        'Remove the mapping of team-red',
      );
    });

    it("marks the editor's own groups, and explains the mark", async () => {
      const fixture = await render();

      const mark = row(fixture, admins)?.querySelector('[data-testid="includes-you"]');
      expect(mark?.textContent).toBe('you');
      expect(row(fixture, red)?.querySelector('[data-testid="includes-you"]')).toBeNull();
      const tooltip = fixture.debugElement
        .queryAll(By.directive(Tooltip))
        .find((each) => each.nativeElement === mark)
        ?.injector.get(Tooltip);
      expect(tooltip?.content()).toContain(
        'Your groups at the identity provider include this group',
      );
    });

    it('lets the keyboard reach the meaning of the mark, as its description', async () => {
      const fixture = await render();

      const mark = row(fixture, admins)!.querySelector('[data-testid="includes-you"]')!;
      expect(mark.getAttribute('tabindex')).toBe('0');
      const description = host(fixture).querySelector(
        `[id="${mark.getAttribute('aria-describedby')}"]`,
      );
      expect(description?.textContent).toBe(includesYouMeaning);
      expect(description?.closest('[hidden]')).not.toBeNull();
    });

    it('shows the meaning of the mark when the keyboard reaches it, not only under the pointer', async () => {
      const fixture = await render();
      const mark = row(fixture, admins)!.querySelector<HTMLElement>(
        '[data-testid="includes-you"]',
      )!;

      mark.focus();
      await new Promise((resolve) => setTimeout(resolve, 450));

      expect(document.body.querySelector('.p-tooltip')?.textContent).toBe(includesYouMeaning);
      mark.blur();
    });

    it('says there are none yet, and not while they load', async () => {
      list.set([]);
      loading.set(true);
      const fixture = await render();
      expect(el(fixture, 'mappings-empty')).toBeNull();

      loading.set(false);
      await settle(fixture);

      expect(el(fixture, 'mappings-empty')?.textContent?.trim()).toBe('No group mappings yet.');
    });

    it('says why the mappings could not be loaded', async () => {
      list.set([]);
      error.set(refusal(503, 'not_ready', 'The database is starting.'));

      const fixture = await render();

      expect(el(fixture, 'mappings-empty')?.textContent?.trim()).toBe(
        'The group mappings could not be loaded: The database is starting.',
      );
    });

    it('says why with the title when the problem has no detail', async () => {
      list.set([]);
      error.set(refusal(403, 'forbidden', ''));

      const fixture = await render();

      expect(el(fixture, 'mappings-empty')?.textContent?.trim()).toBe(
        'The group mappings could not be loaded: Refused',
      );
    });
  });

  describe('for anybody but an administrator', () => {
    beforeEach(() => isAdmin.set(false));

    it('says that the administrators manage the mappings, and shows none of them', async () => {
      const fixture = await render();

      expect(el(fixture, 'mappings-not-admin')?.textContent?.trim()).toBe(
        'Only the administrators of this team manage its group mappings.',
      );
      expect(el(fixture, 'mappings')).toBeNull();
      expect(el(fixture, 'new-mapping')).toBeNull();
      expect(host(fixture).querySelector('.lead')).toBeNull();
    });

    it('does not say so while the person is still loading', async () => {
      personLoading.set(true);

      const fixture = await render();

      expect(el(fixture, 'mappings-not-admin')).toBeNull();
      expect(host(fixture).querySelector('p-skeleton')).not.toBeNull();
    });
  });

  // docs/adr/0034 D2: a global administrator without a role in the tenant reads its mappings, and
  // changes nothing: making one, changing its role and removing it are its administrators'.
  describe('for a global administrator without a role in the tenant', () => {
    beforeEach(() => {
      isAdmin.set(false);
      membership.set(undefined);
      oversight.set(true);
    });

    it('lists the mappings with their roles as text, and offers no act', async () => {
      const fixture = await render();

      expect(el(fixture, 'mappings')).not.toBeNull();
      expect(el(fixture, 'mapping-role-team-red')?.textContent?.trim()).toBe('member');
      expect(el(fixture, 'mapping-role-cowork-admins')?.textContent?.trim()).toBe('admin');
      expect(host(fixture).querySelector('p-select')).toBeNull();
      expect(el(fixture, 'mapping-remove-team-red')).toBeNull();
      expect(el(fixture, 'new-mapping')).toBeNull();
      expect(el(fixture, 'mappings-global-only')).toBeNull();
      expect(el(fixture, 'mappings-not-admin')).toBeNull();
    });
  });

  // docs/adr/0030 D7: every tenant shares the provider's groups, so only a global administrator
  // makes a mapping or changes its role; removing one only takes access away.
  describe('for an administrator who is not a global administrator', () => {
    beforeEach(() => globalAdmin.set(false));

    it('says that only a global administrator creates and changes mappings, and offers no new one', async () => {
      const fixture = await render();

      expect(el(fixture, 'mappings-global-only')?.textContent?.trim()).toBe(
        'Only a global administrator creates mappings and changes their roles; you can remove them.',
      );
      expect(el(fixture, 'new-mapping')).toBeNull();
      expect(el(fixture, 'mappings')).not.toBeNull();
    });

    it('shows each role as text, without a select', async () => {
      const fixture = await render();

      expect(el(fixture, 'mapping-role-team-red')?.textContent?.trim()).toBe('member');
      expect(el(fixture, 'mapping-role-cowork-admins')?.textContent?.trim()).toBe('admin');
      expect(host(fixture).querySelector('p-select')).toBeNull();
    });

    it('removes a mapping when it is confirmed', async () => {
      const fixture = await render();
      el(fixture, 'mapping-remove-team-red')?.click();
      await settle(fixture);

      press('Remove mapping');
      await settle(fixture);

      expect(remove).toHaveBeenCalledExactlyOnceWith(red);
    });

    it('puts the keyboard on the removal of the next row, which has no select', async () => {
      const blue = mapping('team-blue');
      list.set([admins, red, blue]);
      const fixture = await render();
      el(fixture, 'mapping-remove-team-red')?.click();
      await settle(fixture);

      press('Remove mapping');
      await settle(fixture);

      expect(document.activeElement).toBe(el(fixture, 'mapping-remove-team-blue'));
    });

    it('offers to create and change mappings once the person is a global administrator', async () => {
      const fixture = await render();

      globalAdmin.set(true);
      await settle(fixture);

      expect(el(fixture, 'new-mapping')).not.toBeNull();
      expect(shownRole(fixture, red)).toBe('member');
      expect(el(fixture, 'mappings-global-only')).toBeNull();
    });
  });

  describe('a new mapping', () => {
    const newDialog = (fixture: ComponentFixture<GroupMappings>) =>
      fixture.debugElement.query(By.directive(NewMappingDialog));

    it('opens the dialog for it, and closes it when the dialog asks', async () => {
      const fixture = await render();

      el(fixture, 'new-mapping')?.click();
      await settle(fixture);

      expect(newDialog(fixture).componentInstance.visible()).toBe(true);
      expect(el(fixture, 'mapping-group')).not.toBeNull();

      newDialog(fixture).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(fixture.componentInstance['creating']()).toBe(false);
      expect(el(fixture, 'mapping-group')).toBeNull();
    });

    it('leaves the keyboard alone when the dialog says it is open', async () => {
      const fixture = await render();
      const button = el(fixture, 'new-mapping');

      newDialog(fixture).triggerEventHandler('visibleChange', true);
      await settle(fixture);

      expect(fixture.componentInstance['creating']()).toBe(true);
      expect(document.activeElement).not.toBe(button);
    });

    it('gives the keyboard back to New mapping when the dialog closes', async () => {
      const fixture = await render();
      el(fixture, 'new-mapping')?.click();
      await settle(fixture);

      newDialog(fixture).triggerEventHandler('visibleChange', false);
      await settle(fixture);

      expect(document.activeElement).toBe(el(fixture, 'new-mapping'));
    });

    it('says which group gives which role once it is made', async () => {
      const add = toasts();
      const fixture = await render();

      newDialog(fixture).triggerEventHandler('created', mapping('team-blue', { role: 'viewer' }));
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          severity: 'success',
          summary: 'Group mapped',
          detail: 'Members of team-blue hold the role viewer here.',
        }),
      );
    });
  });

  describe("changing a mapping's role", () => {
    it('changes the role chosen in the row', async () => {
      const fixture = await render();

      choose(fixture, red, 'viewer');
      await settle(fixture);

      expect(changeRole).toHaveBeenCalledExactlyOnceWith(red, 'viewer');
    });

    it('changes nothing when the role chosen is the one it has', async () => {
      const fixture = await render();

      choose(fixture, red, 'member');
      await settle(fixture);

      expect(changeRole).not.toHaveBeenCalled();
    });

    it('shows the role chosen, and holds the select, until the answer is in', async () => {
      let finish: (each: GroupMapping) => void = () => undefined;
      changeRole.mockReturnValue(new Promise<GroupMapping>((resolve) => (finish = resolve)));
      const fixture = await render();

      choose(fixture, red, 'admin');
      await settle(fixture);
      expect(shownRole(fixture, red)).toBe('admin');
      expect(el(fixture, 'mapping-role-team-red')?.classList).toContain('p-disabled');

      // The service puts the answer into the list before the write resolves.
      const changed = { ...red, role: 'admin' as const, version: 4 };
      list.set([admins, changed]);
      finish(changed);
      await settle(fixture);

      expect(shownRole(fixture, red)).toBe('admin');
      expect(el(fixture, 'mapping-role-team-red')?.classList).not.toContain('p-disabled');
    });

    it('goes back to the role of the list and says why when the tenant would be left without an administrator', async () => {
      changeRole.mockRejectedValue(refusal(409, 'last_admin'));
      const fixture = await render();

      choose(fixture, red, 'viewer');
      await settle(fixture);

      expect(el(fixture, 'mappings-notice')?.textContent?.trim()).toBe(lastAdminNotice);
      expect(shownRole(fixture, red)).toBe('member');
    });

    it.each(['precondition_failed', 'not_found'] as const)(
      'says that the mapping changed meanwhile on %s, and loads the list again',
      async (code) => {
        changeRole.mockRejectedValue(refusal(code === 'not_found' ? 404 : 412, code));
        const fixture = await render();

        choose(fixture, red, 'viewer');
        await settle(fixture);

        expect(el(fixture, 'mappings-notice')?.textContent?.trim()).toBe(
          'The mapping of team-red changed meanwhile: the list shows it as it is now.',
        );
        expect(reload).toHaveBeenCalled();
        expect(shownRole(fixture, red)).toBe('member');
      },
    );

    it('toasts any other refusal', async () => {
      changeRole.mockRejectedValue(refusal(403, 'forbidden', 'Not for you.'));
      const add = toasts();
      const fixture = await render();

      choose(fixture, red, 'viewer');
      await settle(fixture);

      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ detail: 'Not for you.' }),
      );
      expect(el(fixture, 'mappings-notice')).toBeNull();
    });

    it('lets the message be closed', async () => {
      changeRole.mockRejectedValue(refusal(409, 'last_admin'));
      const fixture = await render();
      choose(fixture, red, 'viewer');
      await settle(fixture);

      el(fixture, 'mappings-notice')?.querySelector('button')?.click();
      await settle(fixture);

      expect(el(fixture, 'mappings-notice')).toBeNull();
    });

    describe("that takes the editor's own administrator role away", () => {
      it('asks first, and says why it would', async () => {
        const fixture = await render();

        choose(fixture, admins, 'member');
        await settle(fixture);

        expect(dialog()?.textContent).toContain('Give up your administrator role?');
        expect(dialog()?.textContent).toContain(
          'Your administrator role here comes only from the group cowork-admins',
        );
        expect(changeRole).not.toHaveBeenCalled();
        expect(shownRole(fixture, admins)).toBe('member');
      });

      it('opens with the focus on the button that keeps the role', async () => {
        const fixture = await render();

        choose(fixture, admins, 'viewer');
        await settle(fixture);
        await new Promise((resolve) => setTimeout(resolve, 50));

        expect((document.activeElement as HTMLElement | null)?.textContent?.trim()).toBe(
          'Keep admin',
        );
      });

      it('changes the role when it is confirmed', async () => {
        const fixture = await render();
        choose(fixture, admins, 'member');
        await settle(fixture);

        press('Make it member');
        await settle(fixture);

        expect(changeRole).toHaveBeenCalledExactlyOnceWith(admins, 'member');
      });

      it('goes back to the role of the list when the editor keeps it', async () => {
        const fixture = await render();
        choose(fixture, admins, 'member');
        await settle(fixture);

        press('Keep admin');
        await settle(fixture);

        expect(changeRole).not.toHaveBeenCalled();
        expect(shownRole(fixture, admins)).toBe('admin');
      });

      it('gives the keyboard back to the select when the editor keeps the role', async () => {
        const fixture = await render();
        choose(fixture, admins, 'member');
        await settle(fixture);

        press('Keep admin');
        await settle(fixture);

        expect(document.activeElement).toBe(combobox(fixture, admins));
      });

      it('puts the keyboard on the heading once the role is given up, which takes the controls away', async () => {
        const fixture = await render();
        choose(fixture, admins, 'member');
        await settle(fixture);

        press('Make it member');
        await settle(fixture);

        expect(document.activeElement).toBe(heading(fixture));
      });

      it('gives the keyboard back to the select when the change is refused', async () => {
        changeRole.mockRejectedValue(refusal(409, 'last_admin'));
        const fixture = await render();
        choose(fixture, admins, 'member');
        await settle(fixture);

        press('Make it member');
        await settle(fixture);

        expect(document.activeElement).toBe(combobox(fixture, admins));
      });

      it('asks while the membership of the editor is not known, since no grant can be seen', async () => {
        membership.set(undefined);
        const fixture = await render();

        choose(fixture, admins, 'member');
        await settle(fixture);

        expect(dialog()?.textContent).toContain('Give up your administrator role?');
        press('Keep admin');
        await settle(fixture);
      });

      it('asks nothing when a grant keeps the editor an administrator', async () => {
        membership.set({
          ...mapped,
          origins: [...mapped.origins, { source: 'grant', role: 'admin' }],
        });
        const fixture = await render();

        choose(fixture, admins, 'member');
        await settle(fixture);

        expect(dialog()).toBeNull();
        expect(changeRole).toHaveBeenCalledOnce();
      });

      it('asks nothing when another of their groups makes them an administrator too', async () => {
        list.set([admins, mapping('ops-admins', { role: 'admin', includes_caller: true }), red]);
        const fixture = await render();

        choose(fixture, admins, 'member');
        await settle(fixture);

        expect(dialog()).toBeNull();
        expect(changeRole).toHaveBeenCalledOnce();
      });

      it('asks nothing for a group that is not theirs, or one that gives no administrator role', async () => {
        list.set([
          mapping('other-admins', { role: 'admin' }),
          mapping('own-members', { includes_caller: true }),
        ]);
        const fixture = await render();

        choose(fixture, list()[0], 'member');
        choose(fixture, list()[1], 'viewer');
        await settle(fixture);

        expect(dialog()).toBeNull();
        expect(changeRole).toHaveBeenCalledTimes(2);
      });
    });
  });

  describe('removing a mapping', () => {
    async function ask(fixture: ComponentFixture<GroupMappings>, each: GroupMapping) {
      el(fixture, `mapping-remove-${each.group}`)?.click();
      await settle(fixture);
    }

    it('asks first, and says what happens to the roles it gave and to the grants', async () => {
      const fixture = await render();

      await ask(fixture, red);

      expect(dialog()?.textContent).toContain('Remove the mapping of team-red?');
      expect(dialog()?.textContent).toContain('loses it at once');
      expect(dialog()?.textContent).toContain('Grants stay.');
      expect(dialog()?.textContent).not.toContain('Your administrator role');
      expect(remove).not.toHaveBeenCalled();
    });

    it("warns when it takes the editor's own administrator role away", async () => {
      const fixture = await render();

      await ask(fixture, admins);

      expect(dialog()?.textContent).toContain(
        'Your administrator role here comes only from the group cowork-admins',
      );
    });

    describe('where the keyboard goes', () => {
      const blue = mapping('team-blue');

      it('back to the removal when the mapping is kept', async () => {
        const fixture = await render();
        await ask(fixture, red);

        press('Keep it');
        await settle(fixture);

        expect(document.activeElement).toBe(el(fixture, 'mapping-remove-team-red'));
      });

      it('to the select of the next row, where it stays when the list loads again', async () => {
        list.set([admins, red, blue]);
        const fixture = await render();
        await ask(fixture, red);

        press('Remove mapping');
        await settle(fixture);
        const next = combobox(fixture, blue);
        expect(document.activeElement).toBe(next);

        // The list as it loads again: other objects of the same mappings, without team-red.
        list.set([{ ...admins }, { ...blue }]);
        await settle(fixture);

        expect(row(fixture, red)).toBeNull();
        expect(document.activeElement).toBe(next);
        expect(next?.isConnected).toBe(true);
      });

      it('to the select of the row before when the mapping is the last', async () => {
        const fixture = await render();
        await ask(fixture, red);

        press('Remove mapping');
        await settle(fixture);

        expect(document.activeElement).toBe(combobox(fixture, admins));
      });

      it('to the heading when no other row is left', async () => {
        list.set([red]);
        const fixture = await render();
        await ask(fixture, red);

        press('Remove mapping');
        await settle(fixture);

        expect(document.activeElement).toBe(heading(fixture));
      });

      it("to the heading when the mapping takes the editor's own administrator role", async () => {
        const fixture = await render();
        await ask(fixture, admins);

        press('Remove mapping');
        await settle(fixture);

        expect(document.activeElement).toBe(heading(fixture));
      });

      it('back to the removal when it is refused', async () => {
        remove.mockRejectedValue(refusal(409, 'last_admin'));
        const fixture = await render();
        await ask(fixture, red);

        press('Remove mapping');
        await settle(fixture);

        expect(document.activeElement).toBe(el(fixture, 'mapping-remove-team-red'));
      });
    });

    it('quotes the group as text, never as markup', async () => {
      const marked = mapping('<a href="x">y</a>', { role: 'admin', includes_caller: true });
      list.set([marked, red]);
      const fixture = await render();

      fixture.componentInstance['remove'](marked);
      await settle(fixture);

      expect(dialog()?.querySelector('.p-confirmdialog-message')?.textContent).toContain(
        'Your administrator role here comes only from the group <a href="x">y</a>: you lose it',
      );
      expect(dialog()?.querySelector('a')).toBeNull();
    });

    it('offers the removal as a dangerous act, with the focus on the button that keeps it', async () => {
      const fixture = await render();

      await ask(fixture, red);
      await new Promise((resolve) => setTimeout(resolve, 50));

      const accept = [...(dialog()?.querySelectorAll('button') ?? [])].find(
        (button) => button.textContent?.trim() === 'Remove mapping',
      );
      expect(accept?.className).toContain('p-button-danger');
      expect((document.activeElement as HTMLElement | null)?.textContent?.trim()).toBe('Keep it');
    });

    it('removes the mapping when it is confirmed, and says so', async () => {
      const add = toasts();
      const fixture = await render();
      await ask(fixture, red);

      press('Remove mapping');
      await settle(fixture);

      expect(remove).toHaveBeenCalledExactlyOnceWith(red);
      expect(add).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          severity: 'success',
          summary: 'Mapping removed',
          detail: 'team-red gives no role here any more.',
        }),
      );
    });

    it('does nothing when the mapping is kept', async () => {
      const fixture = await render();
      await ask(fixture, red);

      press('Keep it');
      await settle(fixture);

      expect(remove).not.toHaveBeenCalled();
    });

    it('says why when the tenant would be left without an administrator', async () => {
      remove.mockRejectedValue(refusal(409, 'last_admin'));
      const add = toasts();
      const fixture = await render();
      await ask(fixture, admins);

      press('Remove mapping');
      await settle(fixture);

      expect(el(fixture, 'mappings-notice')?.textContent?.trim()).toBe(lastAdminNotice);
      expect(add).not.toHaveBeenCalled();
    });
  });

  describe('another tenant', () => {
    it('closes the dialog and drops the message of the tenant before', async () => {
      changeRole.mockRejectedValue(refusal(409, 'last_admin'));
      const fixture = await render();
      el(fixture, 'new-mapping')?.click();
      choose(fixture, red, 'viewer');
      await settle(fixture);
      expect(fixture.componentInstance['creating']()).toBe(true);
      expect(el(fixture, 'mappings-notice')).not.toBeNull();

      tenantSlug.set('globex');
      await settle(fixture);

      expect(fixture.componentInstance['creating']()).toBe(false);
      expect(el(fixture, 'mappings-notice')).toBeNull();
    });

    // The page is reused when only the tenant changes: an answer now would send the mapping of
    // the tenant before to the tenant shown.
    it('closes an open question, whose answer would act in the tenant shown now', async () => {
      const fixture = await render();
      el(fixture, `mapping-remove-${red.group}`)?.click();
      await settle(fixture);
      expect(dialog()?.textContent).toContain('Remove the mapping of team-red?');

      tenantSlug.set('globex');
      await settle(fixture);

      expect(dialog()).toBeNull();
      press('Remove mapping');
      await settle(fixture);
      expect(remove).not.toHaveBeenCalled();
    });

    it('drops the role an open question held, and the select of the same mapping is free again', async () => {
      const fixture = await render();
      choose(fixture, admins, 'member');
      await settle(fixture);
      expect(shownRole(fixture, admins)).toBe('member');
      expect(el(fixture, `mapping-role-${admins.group}`)?.classList).toContain('p-disabled');

      tenantSlug.set('globex');
      await settle(fixture);

      expect(dialog()).toBeNull();
      expect(shownRole(fixture, admins)).toBe('admin');
      expect(el(fixture, `mapping-role-${admins.group}`)?.classList).not.toContain('p-disabled');
      press('Make it member');
      await settle(fixture);
      expect(changeRole).not.toHaveBeenCalled();
    });
  });
});
