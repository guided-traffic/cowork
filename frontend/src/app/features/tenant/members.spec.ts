import { HttpErrorResponse } from '@angular/common/http';
import { signal, WritableSignal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MessageService } from 'primeng/api';
import { Tooltip } from 'primeng/tooltip';
import type { MockInstance } from 'vitest';
import { Me, Member, Problem, ProblemCode } from '../../api/models';
import { MembersService } from '../../core/members.service';
import { SessionService } from '../../core/session.service';
import { TenantService } from '../../core/tenant.service';
import { AddMemberDialog } from './add-member-dialog';
import { Members } from './members';
import { lastAdminNotice, originMeanings, roleMeanings } from './roles';

/**
 * Ada works here: an administrator by a grant, who signs in with a local account. The members carry
 * no address, as the list has them for anybody but an administrator; a test that needs one adds it.
 */
const ada: Member = {
  role: 'admin',
  person: { id: 'p1', display_name: 'Ada Lovelace', username: 'local:ada' },
  origins: [{ source: 'grant', role: 'admin' }],
  local: true,
  email: null,
};
/** Sam is a member by a group mapping, and holds a viewer grant beside it. */
const sam: Member = {
  role: 'member',
  person: { id: 'p2', display_name: 'Sam Rivera', username: 'local:sam' },
  origins: [
    { source: 'mapping', role: 'member' },
    { source: 'grant', role: 'viewer' },
  ],
  local: false,
  email: null,
};
/** Bob is a member by a group mapping only, a person of the identity provider. */
const bob: Member = {
  role: 'member',
  person: { id: 'p3', display_name: 'Bob Example', username: null },
  origins: [{ source: 'mapping', role: 'member' }],
  local: false,
  email: null,
};
/** Cyd is a viewer by a grant only. */
const cyd: Member = {
  role: 'viewer',
  person: { id: 'p4', display_name: 'Cyd Charisse', username: null },
  origins: [{ source: 'grant', role: 'viewer' }],
  local: false,
  email: null,
};

const me = (id = 'p1'): Me => ({
  id,
  display_name: 'Ada Lovelace',
  username: 'local:ada',
  global_admin: false,
  local: true,
  password_change_required: false,
  memberships: [
    {
      role: 'admin',
      tenant: { slug: 'acme', name: 'Acme Corp' },
      origins: [{ source: 'grant', role: 'admin' }],
    },
  ],
});

function refusal(status: number, code: ProblemCode, detail = 'The server says no.') {
  const body: Problem = { type: 'about:blank', title: 'Refused', status, detail, code };
  return new HttpErrorResponse({ status, statusText: 'Refused', error: body });
}

describe('Members', () => {
  let list: WritableSignal<Member[]>;
  let loading: WritableSignal<boolean>;
  let error: WritableSignal<unknown>;
  let reload: MockInstance<() => boolean>;
  let tenantSlug: WritableSignal<string | null>;
  let person: WritableSignal<Me | undefined>;
  let isAdmin: WritableSignal<boolean>;
  let mayGrantSelf: WritableSignal<boolean>;
  let oversight: WritableSignal<boolean>;
  let add: MockInstance<MembersService['add']>;
  let setGrant: MockInstance<MembersService['setGrant']>;
  let removeGrant: MockInstance<MembersService['removeGrant']>;

  beforeEach(() => {
    list = signal<Member[]>([ada, sam, bob]);
    loading = signal(false);
    error = signal<unknown>(undefined);
    reload = vi.fn<() => boolean>().mockReturnValue(true);
    tenantSlug = signal<string | null>('acme');
    person = signal<Me | undefined>(me());
    isAdmin = signal(false);
    mayGrantSelf = signal(false);
    oversight = signal(false);
    add = vi.fn<MembersService['add']>();
    setGrant = vi.fn<MembersService['setGrant']>().mockImplementation(async (id, role) => ({
      ...(list().find((each) => each.person.id === id) as Member),
      role,
    }));
    removeGrant = vi.fn<MembersService['removeGrant']>().mockResolvedValue(undefined);
    TestBed.configureTestingModule({
      providers: [
        MessageService,
        {
          provide: MembersService,
          useValue: {
            list,
            members: { isLoading: loading, error, reload },
            add,
            setGrant,
            removeGrant,
          },
        },
        {
          provide: SessionService,
          useValue: {
            tenant: tenantSlug,
            person,
            mayGrantSelf,
            oversight,
            membership: signal(undefined),
            shown: signal({ slug: 'acme', name: 'Acme Corp', role: 'viewer' }),
          },
        },
        { provide: TenantService, useValue: { isAdmin } },
      ],
    });
  });

  // Whatever a test spied on goes back, also when an assertion of the test has failed.
  afterEach(() => vi.restoreAllMocks());

  async function render() {
    const fixture = TestBed.createComponent(Members);
    await settle(fixture);
    return fixture;
  }

  /** Lets what a click started finish, and shows it; fields take their model a moment after they appear. */
  async function settle(fixture: ComponentFixture<Members>) {
    for (let round = 0; round < 2; round++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    await fixture.whenStable();
  }

  const host = (fixture: ComponentFixture<Members>) => fixture.nativeElement as HTMLElement;
  const el = (fixture: ComponentFixture<Members>, testId: string) =>
    host(fixture).querySelector<HTMLElement>(`[data-testid="${testId}"]`);
  const row = (fixture: ComponentFixture<Members>, member: Member) =>
    el(fixture, `member-${member.person.username ?? member.person.id}`);
  const cells = (fixture: ComponentFixture<Members>, member: Member) =>
    [...(row(fixture, member)?.querySelectorAll('td') ?? [])].map((cell) =>
      cell.textContent?.trim().replace(/\s+/g, ' '),
    );
  const badges = (fixture: ComponentFixture<Members>, member: Member) =>
    [...(row(fixture, member)?.querySelectorAll('.pill') ?? [])].map((badge) =>
      badge.textContent?.trim(),
    );
  const tooltipOf = (fixture: ComponentFixture<Members>, element: Element) =>
    fixture.debugElement
      .queryAll(By.directive(Tooltip))
      .find((each) => each.nativeElement === element)
      ?.injector.get(Tooltip)
      .content();
  /** The grant a row's select shows: its role, or the placeholder. */
  const shownGrant = (fixture: ComponentFixture<Members>, member: Member) =>
    el(fixture, `grant-${member.person.id}`)?.querySelector('.p-select-label')?.textContent?.trim();
  /** What choosing a role in a row's select tells the page. */
  function choose(fixture: ComponentFixture<Members>, member: Member, role: string) {
    fixture.debugElement
      .query(By.css(`[data-testid="grant-${member.person.id}"]`))
      .triggerEventHandler('ngModelChange', role);
  }
  const toasts = () => vi.spyOn(TestBed.inject(MessageService), 'add');
  /** The focusable part of a row's select: PrimeNG's combobox. */
  const combobox = (fixture: ComponentFixture<Members>, member: Member) =>
    el(fixture, `grant-${member.person.id}`)?.querySelector('[role="combobox"]');
  /** The text of the element that describes another, by its aria-describedby. */
  const description = (fixture: ComponentFixture<Members>, element: Element) =>
    host(fixture).querySelector(`[id="${element.getAttribute('aria-describedby')}"]`);

  const dialog = () => document.body.querySelector('.p-confirmdialog');
  const press = (label: string) =>
    [...(dialog()?.querySelectorAll('button') ?? [])]
      .find((button) => button.textContent?.trim() === label)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

  describe('the list', () => {
    it('shows a row per member with the name, the username, the effective role and the origins', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('h1')?.textContent).toBe('Members');
      expect([...host(fixture).querySelectorAll('th')].map((each) => each.textContent)).toEqual([
        'Name',
        'Username',
        'Role',
        'Origin',
      ]);
      expect(cells(fixture, sam).slice(0, 3)).toEqual(['Sam Rivera', 'local:sam', 'member']);
      expect(badges(fixture, sam)).toEqual(['mapping · member', 'grant · viewer']);
      expect(el(fixture, 'members')).not.toBeNull();
      expect(el(fixture, 'members-empty')).toBeNull();
    });

    it('identifies a member without a username by the id of the person and shows a dash', async () => {
      const fixture = await render();

      expect(cells(fixture, bob).slice(0, 2)).toEqual(['Bob Example', '—']);
    });

    it('marks the row of the person who is working', async () => {
      const fixture = await render();

      expect(cells(fixture, ada)[0]).toBe('Ada Lovelace (you)');
      expect(cells(fixture, sam)[0]).toBe('Sam Rivera');
    });

    it('marks nobody while the person is not known', async () => {
      person.set(undefined);

      const fixture = await render();

      expect(cells(fixture, ada)[0]).toBe('Ada Lovelace');
    });

    it('explains the effective role in a tooltip', async () => {
      const fixture = await render();

      const role = row(fixture, sam)!.querySelector('td:nth-child(3) span')!;
      expect(tooltipOf(fixture, role)).toBe(roleMeanings.member);
    });

    it('shows each origin as a badge with its role, the mapping before the grant, and the local account', async () => {
      const fixture = await render();

      expect(badges(fixture, sam)).toEqual(['mapping · member', 'grant · viewer']);
      expect(badges(fixture, ada)).toEqual(['grant · admin', 'local account']);
      expect(badges(fixture, bob)).toEqual(['mapping · member']);
    });

    it('explains each origin in a tooltip', async () => {
      const fixture = await render();

      const of = (member: Member, testId: string) =>
        tooltipOf(fixture, row(fixture, member)!.querySelector(`[data-testid="${testId}"]`)!);
      expect(of(sam, 'origin-mapping')).toBe(originMeanings.mapping);
      expect(of(sam, 'origin-grant')).toBe(originMeanings.grant);
      expect(of(ada, 'origin-local')).toBe(originMeanings.local);
    });

    it('lets the keyboard reach the meaning of the role and of each badge, as their description', async () => {
      const fixture = await render();

      const role = row(fixture, sam)!.querySelector('td:nth-child(3) span')!;
      expect(role.getAttribute('tabindex')).toBe('0');
      expect(description(fixture, role)?.textContent).toBe(roleMeanings.member);
      for (const [member, testId, meaning] of [
        [sam, 'origin-mapping', originMeanings.mapping],
        [sam, 'origin-grant', originMeanings.grant],
        [ada, 'origin-local', originMeanings.local],
      ] as const) {
        const badge = row(fixture, member)!.querySelector(`[data-testid="${testId}"]`)!;
        expect(badge.getAttribute('tabindex'), testId).toBe('0');
        expect(description(fixture, badge)?.textContent, testId).toBe(meaning);
        // The description is read out, not shown a second time.
        expect(description(fixture, badge)?.closest('[hidden]'), testId).not.toBeNull();
      }
    });

    it('shows the meaning of a badge when the keyboard reaches it, not only under the pointer', async () => {
      const fixture = await render();
      const badge = row(fixture, sam)!.querySelector<HTMLElement>('[data-testid="origin-grant"]')!;

      badge.focus();
      await new Promise((resolve) => setTimeout(resolve, 450));

      expect(document.body.querySelector('.p-tooltip')?.textContent).toBe(originMeanings.grant);
      badge.blur();
    });

    it('takes the colour of each badge from a token of the preset, never a literal', async () => {
      const fixture = await render();

      const accent = (member: Member, testId: string) =>
        row(fixture, member)
          ?.querySelector<HTMLElement>(`[data-testid="${testId}"]`)
          ?.style.getPropertyValue('--accent');
      expect(accent(sam, 'origin-mapping')).toBe('var(--p-state-analysed)');
      expect(accent(sam, 'origin-grant')).toBe('var(--p-state-decided)');
      expect(accent(ada, 'origin-local')).toBe('var(--p-text-muted-color)');
    });

    it('follows the list when members arrive', async () => {
      list.set([]);
      const fixture = await render();
      expect(row(fixture, cyd)).toBeNull();

      list.set([cyd]);
      await settle(fixture);

      expect(row(fixture, cyd)).not.toBeNull();
    });
  });

  describe('for anybody but an administrator (docs/adr/0034 D8)', () => {
    it('offers no control: no way to add a member, no grant to change, none to remove', async () => {
      const fixture = await render();

      expect(el(fixture, 'add-member')).toBeNull();
      expect(el(fixture, 'members-lead')).toBeNull();
      expect(host(fixture).querySelector('p-select')).toBeNull();
      expect(host(fixture).querySelector('[data-testid^="remove-grant-"]')).toBeNull();
      expect(host(fixture).querySelector('app-self-grant')).toBeNull();
    });

    // docs/adr/0034 D2: a global administrator who holds a role below admin raises their own grant.
    it('offers a global administrator below admin to raise their own grant, above the list', async () => {
      mayGrantSelf.set(true);

      const fixture = await render();

      expect(
        host(fixture).querySelector('app-self-grant [data-testid="grant-yourself"]'),
      ).not.toBeNull();
    });

    it("leaves the offer to the tenant's pages where the global administrator holds no role", async () => {
      mayGrantSelf.set(true);
      oversight.set(true);

      const fixture = await render();

      expect(host(fixture).querySelector('app-self-grant')).toBeNull();
    });

    it('shows the names as before, without an address, which the list has for administrators only', async () => {
      const fixture = await render();

      expect(host(fixture).querySelector('.email')).toBeNull();
      expect(cells(fixture, sam)[0]).toBe('Sam Rivera');
      expect(cells(fixture, bob)[0]).toBe('Bob Example');
    });
  });

  describe('for an administrator', () => {
    beforeEach(() => isAdmin.set(true));

    it('says how a mapped and a granted role combine', async () => {
      const fixture = await render();

      expect(el(fixture, 'members-lead')?.textContent).toContain(
        'A grant adds to a mapped role and never lowers it',
      );
    });

    it('adds the columns of the grant and its removal', async () => {
      const fixture = await render();

      expect([...host(fixture).querySelectorAll('th')].map((each) => each.textContent)).toEqual([
        'Name',
        'Username',
        'Role',
        'Origin',
        'Grant',
        '',
      ]);
    });

    it("shows each member's grant in the row's select, and none where there is no grant", async () => {
      const fixture = await render();

      expect(shownGrant(fixture, sam)).toBe('viewer');
      expect(shownGrant(fixture, bob)).toBe('none');
      const select = fixture.debugElement.query(By.css('[data-testid="grant-p2"]'));
      expect(select.componentInstance.ariaLabel()).toBe('Granted role of Sam Rivera');
    });

    it('offers the removal of a grant only where there is one', async () => {
      const fixture = await render();

      expect(el(fixture, 'remove-grant-p2')?.getAttribute('aria-label')).toBe(
        'Remove the grant of Sam Rivera',
      );
      expect(el(fixture, 'remove-grant-p3')).toBeNull();
    });

    it('spans every column with the empty list', async () => {
      list.set([]);

      const fixture = await render();

      expect(el(fixture, 'members-empty')?.getAttribute('colspan')).toBe('6');
    });

    describe('the e-mail address', () => {
      /** Another Sam Rivera, a person of the identity provider with an address of their own. */
      const otherSam: Member = {
        role: 'viewer',
        person: { id: 'p5', display_name: 'Sam Rivera', username: null },
        origins: [{ source: 'mapping', role: 'viewer' }],
        local: false,
        email: 'sam.rivera@globex.example',
      };
      const email = (fixture: ComponentFixture<Members>, member: Member) =>
        row(fixture, member)?.querySelector('.email')?.textContent?.trim();

      it('shows each member under the name, which tells two persons of one name apart', async () => {
        list.set([ada, { ...sam, email: 'sam@example.com' }, otherSam]);

        const fixture = await render();

        expect(email(fixture, sam)).toBe('sam@example.com');
        expect(email(fixture, otherSam)).toBe('sam.rivera@globex.example');
        expect(cells(fixture, otherSam)[0]).toBe('Sam Rivera sam.rivera@globex.example');
      });

      it('shows none for a member who has none, such as a local account', async () => {
        list.set([ada, otherSam]);

        const fixture = await render();

        expect(row(fixture, ada)?.querySelector('.email')).toBeNull();
        expect(cells(fixture, ada)[0]).toBe('Ada Lovelace (you)');
      });
    });

    describe('adding a member', () => {
      const addDialog = (fixture: ComponentFixture<Members>) =>
        fixture.debugElement.query(By.directive(AddMemberDialog));

      it('opens the dialog for it, and closes it when the dialog asks', async () => {
        const fixture = await render();
        expect(addDialog(fixture).componentInstance.visible()).toBe(false);

        el(fixture, 'add-member')?.click();
        await settle(fixture);

        expect(addDialog(fixture).componentInstance.visible()).toBe(true);
        expect(el(fixture, 'member-person')).not.toBeNull();

        addDialog(fixture).triggerEventHandler('visibleChange', false);
        await settle(fixture);

        expect(fixture.componentInstance['adding']()).toBe(false);
        expect(el(fixture, 'member-person')).toBeNull();
      });

      it('leaves the keyboard alone when the dialog says it is open', async () => {
        const fixture = await render();
        const button = el(fixture, 'add-member');

        addDialog(fixture).triggerEventHandler('visibleChange', true);
        await settle(fixture);

        expect(fixture.componentInstance['adding']()).toBe(true);
        expect(document.activeElement).not.toBe(button);
      });

      it('gives the keyboard back to Add member when the dialog closes', async () => {
        const fixture = await render();
        el(fixture, 'add-member')?.click();
        await settle(fixture);

        addDialog(fixture).triggerEventHandler('visibleChange', false);
        await settle(fixture);

        expect(document.activeElement).toBe(el(fixture, 'add-member'));
      });

      it('says who was added with which role once the dialog made the grant', async () => {
        const add = toasts();
        const fixture = await render();

        addDialog(fixture).triggerEventHandler('added', cyd);
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({
            severity: 'success',
            summary: 'Member added',
            detail: 'Cyd Charisse holds the role viewer here.',
          }),
        );
      });
    });

    describe("changing a member's grant", () => {
      it('gives the member the role chosen in the row', async () => {
        const fixture = await render();

        choose(fixture, bob, 'admin');
        await settle(fixture);

        expect(setGrant).toHaveBeenCalledExactlyOnceWith('p3', 'admin');
      });

      it('changes nothing when the role chosen is the grant that is there', async () => {
        const fixture = await render();

        choose(fixture, sam, 'viewer');
        await settle(fixture);

        expect(setGrant).not.toHaveBeenCalled();
      });

      it('shows the role chosen, and holds the select, until the answer is in', async () => {
        let finish: (member: Member) => void = () => undefined;
        setGrant.mockReturnValue(new Promise<Member>((resolve) => (finish = resolve)));
        const fixture = await render();

        choose(fixture, bob, 'admin');
        await settle(fixture);
        expect(shownGrant(fixture, bob)).toBe('admin');
        expect(el(fixture, 'grant-p3')?.classList).toContain('p-disabled');

        // The service puts the answer into the list before the write resolves.
        const granted: Member = {
          ...bob,
          role: 'admin',
          origins: [...bob.origins, { source: 'grant', role: 'admin' }],
        };
        list.set([ada, sam, granted]);
        finish(granted);
        await settle(fixture);

        expect(shownGrant(fixture, bob)).toBe('admin');
        expect(el(fixture, 'grant-p3')?.classList).not.toContain('p-disabled');
      });

      it('goes back to the grant of the list and says why when the tenant would be left without an administrator', async () => {
        setGrant.mockRejectedValue(refusal(409, 'last_admin'));
        const add = toasts();
        const fixture = await render();

        choose(fixture, sam, 'member');
        await settle(fixture);

        expect(el(fixture, 'members-notice')?.textContent?.trim()).toBe(lastAdminNotice);
        expect(el(fixture, 'members-notice')?.getAttribute('role')).toBe('alert');
        expect(shownGrant(fixture, sam)).toBe('viewer');
        expect(add).not.toHaveBeenCalled();
      });

      it('says that a member who is gone is no longer one, and loads the list again', async () => {
        setGrant.mockRejectedValue(refusal(404, 'person_not_found'));
        const fixture = await render();

        choose(fixture, bob, 'viewer');
        await settle(fixture);

        expect(el(fixture, 'members-notice')?.textContent?.trim()).toBe(
          'Bob Example is no longer a member of this tenant.',
        );
        expect(reload).toHaveBeenCalled();
        expect(shownGrant(fixture, bob)).toBe('none');
      });

      it('toasts any other refusal, with the detail of the problem', async () => {
        setGrant.mockRejectedValue(refusal(403, 'forbidden', 'Only an administrator grants.'));
        const add = toasts();
        const fixture = await render();

        choose(fixture, bob, 'viewer');
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({ detail: 'Only an administrator grants.' }),
        );
        expect(el(fixture, 'members-notice')).toBeNull();
      });

      it('lets the message be closed, and drops it at the next change', async () => {
        setGrant.mockRejectedValueOnce(refusal(409, 'last_admin'));
        const fixture = await render();
        choose(fixture, sam, 'member');
        await settle(fixture);
        expect(el(fixture, 'members-notice')).not.toBeNull();

        choose(fixture, bob, 'viewer');
        await settle(fixture);
        expect(el(fixture, 'members-notice')).toBeNull();

        setGrant.mockRejectedValueOnce(refusal(409, 'last_admin'));
        choose(fixture, sam, 'member');
        await settle(fixture);
        el(fixture, 'members-notice')?.querySelector('button')?.click();
        await settle(fixture);
        expect(el(fixture, 'members-notice')).toBeNull();
      });

      describe("of the administrator's own row", () => {
        it('asks first when it would take their administrator role away, and says how it comes back', async () => {
          const fixture = await render();

          choose(fixture, ada, 'member');
          await settle(fixture);

          expect(dialog()?.textContent).toContain('Give up your administrator role?');
          expect(dialog()?.textContent).toContain('Your role here would be member');
          expect(dialog()?.textContent).toContain(
            'only another administrator could give the role back',
          );
          expect(setGrant).not.toHaveBeenCalled();
          expect(shownGrant(fixture, ada)).toBe('member');
        });

        it('opens with the focus on the button that keeps the role', async () => {
          const fixture = await render();

          choose(fixture, ada, 'viewer');
          await settle(fixture);
          await new Promise((resolve) => setTimeout(resolve, 50));

          expect((document.activeElement as HTMLElement | null)?.textContent?.trim()).toBe(
            'Keep it',
          );
        });

        it('changes the grant when it is confirmed', async () => {
          const fixture = await render();
          choose(fixture, ada, 'member');
          await settle(fixture);

          press('Change my grant');
          await settle(fixture);

          expect(setGrant).toHaveBeenCalledExactlyOnceWith('p1', 'member');
        });

        it('goes back to the grant of the list when the person keeps their role', async () => {
          const fixture = await render();
          choose(fixture, ada, 'member');
          await settle(fixture);

          press('Keep it');
          await settle(fixture);

          expect(setGrant).not.toHaveBeenCalled();
          expect(shownGrant(fixture, ada)).toBe('admin');
        });

        it('asks nothing when a group mapping keeps them an administrator', async () => {
          list.set([{ ...ada, origins: [{ source: 'mapping', role: 'admin' }, ...ada.origins] }]);
          const fixture = await render();

          choose(fixture, ada, 'viewer');
          await settle(fixture);

          expect(dialog()).toBeNull();
          expect(setGrant).toHaveBeenCalledExactlyOnceWith('p1', 'viewer');
        });

        it('gives the keyboard back to the select when the person keeps their role', async () => {
          const fixture = await render();
          choose(fixture, ada, 'member');
          await settle(fixture);

          press('Keep it');
          await settle(fixture);

          expect(document.activeElement).toBe(combobox(fixture, ada));
        });

        it('puts the keyboard on the heading once the role is given up, which takes the controls away', async () => {
          const fixture = await render();
          choose(fixture, ada, 'member');
          await settle(fixture);

          press('Change my grant');
          await settle(fixture);

          expect(document.activeElement).toBe(host(fixture).querySelector('h1'));
        });

        it('gives the keyboard back to the select when the change is refused', async () => {
          setGrant.mockRejectedValue(refusal(409, 'last_admin'));
          const fixture = await render();
          choose(fixture, ada, 'member');
          await settle(fixture);

          press('Change my grant');
          await settle(fixture);

          expect(document.activeElement).toBe(combobox(fixture, ada));
        });
      });
    });

    describe("removing a member's grant", () => {
      async function ask(fixture: ComponentFixture<Members>, member: Member) {
        el(fixture, `remove-grant-${member.person.id}`)?.click();
        await settle(fixture);
      }

      it('asks first, and says that the mapped role stays', async () => {
        const fixture = await render();

        await ask(fixture, sam);

        expect(dialog()?.textContent).toContain('Remove the grant of Sam Rivera?');
        expect(dialog()?.textContent).toContain(
          'Sam Rivera keeps the role member through a group mapping: a mapped membership stays',
        );
        expect(removeGrant).not.toHaveBeenCalled();
      });

      it('says that a member without a mapping leaves the tenant', async () => {
        list.set([ada, cyd]);
        const fixture = await render();

        await ask(fixture, cyd);

        expect(dialog()?.textContent).toContain(
          'Cyd Charisse has no other membership here and is no longer a member of this tenant.',
        );
      });

      it('offers the removal as a dangerous act, with the focus on the button that keeps the grant', async () => {
        const fixture = await render();

        await ask(fixture, sam);
        await new Promise((resolve) => setTimeout(resolve, 50));

        const accept = [...(dialog()?.querySelectorAll('button') ?? [])].find(
          (button) => button.textContent?.trim() === 'Remove grant',
        );
        expect(accept?.className).toContain('p-button-danger');
        expect((document.activeElement as HTMLElement | null)?.textContent?.trim()).toBe('Keep it');
      });

      it('removes the grant when it is confirmed and says what stays', async () => {
        const add = toasts();
        const fixture = await render();
        await ask(fixture, sam);

        press('Remove grant');
        await settle(fixture);

        expect(removeGrant).toHaveBeenCalledExactlyOnceWith('p2');
        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({
            severity: 'success',
            summary: 'Grant removed',
            detail: 'Sam Rivera keeps the role member through a group mapping.',
          }),
        );
      });

      it('says that a member without a mapping is gone once the grant is', async () => {
        list.set([ada, cyd]);
        const add = toasts();
        const fixture = await render();
        await ask(fixture, cyd);

        press('Remove grant');
        await settle(fixture);

        expect(add).toHaveBeenCalledExactlyOnceWith(
          expect.objectContaining({ detail: 'Cyd Charisse is no longer a member of this tenant.' }),
        );
      });

      it('does nothing when the grant is kept', async () => {
        const fixture = await render();
        await ask(fixture, sam);

        press('Keep it');
        await settle(fixture);

        expect(removeGrant).not.toHaveBeenCalled();
      });

      it('says why when the tenant would be left without an administrator', async () => {
        removeGrant.mockRejectedValue(refusal(409, 'last_admin'));
        const add = toasts();
        const fixture = await render();
        await ask(fixture, sam);

        press('Remove grant');
        await settle(fixture);

        expect(el(fixture, 'members-notice')?.textContent?.trim()).toBe(lastAdminNotice);
        expect(add).not.toHaveBeenCalled();
      });

      describe('where the keyboard goes', () => {
        it('back to the removal when the grant is kept', async () => {
          const fixture = await render();
          await ask(fixture, sam);

          press('Keep it');
          await settle(fixture);

          expect(document.activeElement).toBe(el(fixture, 'remove-grant-p2'));
        });

        it('to the select of the row when a group mapping keeps the member', async () => {
          const fixture = await render();
          await ask(fixture, sam);

          press('Remove grant');
          await settle(fixture);

          expect(document.activeElement).toBe(combobox(fixture, sam));
        });

        it('to the select of the next row when the member leaves, where it stays when the list loads again', async () => {
          list.set([ada, cyd, sam]);
          const fixture = await render();
          await ask(fixture, cyd);

          press('Remove grant');
          await settle(fixture);
          const next = combobox(fixture, sam);
          expect(document.activeElement).toBe(next);

          // The list as it loads again: other objects of the same people, without Cyd.
          list.set([{ ...ada }, { ...sam }]);
          await settle(fixture);

          expect(row(fixture, cyd)).toBeNull();
          expect(document.activeElement).toBe(next);
          expect(next?.isConnected).toBe(true);
        });

        it('to the select of the row before when the member who leaves is the last', async () => {
          list.set([ada, sam, cyd]);
          const fixture = await render();
          await ask(fixture, cyd);

          press('Remove grant');
          await settle(fixture);

          expect(document.activeElement).toBe(combobox(fixture, sam));
        });

        it('to the heading when no other row is left', async () => {
          list.set([cyd]);
          const fixture = await render();
          await ask(fixture, cyd);

          press('Remove grant');
          await settle(fixture);

          expect(document.activeElement).toBe(host(fixture).querySelector('h1'));
        });

        it('to the heading when administrators remove their own administrator role', async () => {
          const fixture = await render();
          await ask(fixture, ada);

          press('Remove grant');
          await settle(fixture);

          expect(document.activeElement).toBe(host(fixture).querySelector('h1'));
        });

        it('back to the removal when it is refused', async () => {
          removeGrant.mockRejectedValue(refusal(409, 'last_admin'));
          const fixture = await render();
          await ask(fixture, sam);

          press('Remove grant');
          await settle(fixture);

          expect(document.activeElement).toBe(el(fixture, 'remove-grant-p2'));
        });
      });

      it('quotes the name of the member as text, never as markup', async () => {
        const marked: Member = {
          ...cyd,
          person: { ...cyd.person, display_name: '<a href="x">y</a>' },
        };
        list.set([ada, marked]);
        const fixture = await render();

        await ask(fixture, marked);

        expect(dialog()?.querySelector('.p-confirmdialog-message')?.textContent).toBe(
          '<a href="x">y</a> has no other membership here and is no longer a member of this tenant.',
        );
        expect(dialog()?.querySelector('a')).toBeNull();
      });

      describe('of the administrator themselves', () => {
        it('says that they leave the tenant and could no longer administer it', async () => {
          const fixture = await render();

          await ask(fixture, ada);

          expect(dialog()?.textContent).toContain('Remove your grant?');
          expect(dialog()?.textContent).toContain(
            'You have no other membership here and leave this tenant.',
          );
          expect(dialog()?.textContent).toContain('You could no longer administer this tenant');
        });

        it('says which role a group mapping leaves them, and that it is no administrator role', async () => {
          list.set([{ ...ada, origins: [{ source: 'mapping', role: 'member' }, ...ada.origins] }]);
          const fixture = await render();

          await ask(fixture, ada);

          expect(dialog()?.textContent).toContain(
            'You keep the role member through a group mapping.',
          );
          expect(dialog()?.textContent).toContain('You could no longer administer this tenant');
        });

        it('does not warn when a group mapping keeps them an administrator', async () => {
          list.set([{ ...ada, origins: [{ source: 'mapping', role: 'admin' }, ...ada.origins] }]);
          const fixture = await render();

          await ask(fixture, ada);

          expect(dialog()?.textContent).toContain(
            'You keep the role admin through a group mapping.',
          );
          expect(dialog()?.textContent).not.toContain('could no longer administer');
        });
      });
    });

    describe('another tenant', () => {
      it('closes the dialog and drops the message of the tenant before', async () => {
        setGrant.mockRejectedValue(refusal(409, 'last_admin'));
        const fixture = await render();
        el(fixture, 'add-member')?.click();
        choose(fixture, sam, 'member');
        await settle(fixture);
        expect(fixture.componentInstance['adding']()).toBe(true);
        expect(el(fixture, 'members-notice')).not.toBeNull();

        tenantSlug.set('globex');
        await settle(fixture);

        expect(fixture.componentInstance['adding']()).toBe(false);
        expect(el(fixture, 'members-notice')).toBeNull();
      });

      // The page is reused when only the tenant changes, and a person's id is the same in every
      // tenant: an answer now would remove the grant of the same person in the tenant shown.
      it('closes an open question, whose answer would act in the tenant shown now', async () => {
        const fixture = await render();
        el(fixture, 'remove-grant-p2')?.click();
        await settle(fixture);
        expect(dialog()?.textContent).toContain('Remove the grant of Sam Rivera?');

        tenantSlug.set('globex');
        await settle(fixture);

        expect(dialog()).toBeNull();
        press('Remove grant');
        await settle(fixture);
        expect(removeGrant).not.toHaveBeenCalled();
      });

      it('drops the grant an open question held, and the select of the same person is free again', async () => {
        const fixture = await render();
        choose(fixture, ada, 'member');
        await settle(fixture);
        expect(shownGrant(fixture, ada)).toBe('member');
        expect(el(fixture, 'grant-p1')?.classList).toContain('p-disabled');

        tenantSlug.set('globex');
        await settle(fixture);

        expect(dialog()).toBeNull();
        expect(shownGrant(fixture, ada)).toBe('admin');
        expect(el(fixture, 'grant-p1')?.classList).not.toContain('p-disabled');
        press('Change my grant');
        await settle(fixture);
        expect(setGrant).not.toHaveBeenCalled();
      });
    });
  });

  describe('without members', () => {
    beforeEach(() => list.set([]));

    it('says there are none, across the columns of the list', async () => {
      const fixture = await render();

      expect(el(fixture, 'members-empty')?.textContent?.trim()).toBe('No members.');
      expect(el(fixture, 'members-empty')?.getAttribute('colspan')).toBe('4');
    });

    it('does not say so while the members load', async () => {
      loading.set(true);

      const fixture = await render();
      expect(el(fixture, 'members-empty')).toBeNull();

      loading.set(false);
      await settle(fixture);

      expect(el(fixture, 'members-empty')?.textContent?.trim()).toBe('No members.');
    });
  });

  describe('members that could not be loaded', () => {
    beforeEach(() => list.set([]));

    it('says why, with the detail of the problem', async () => {
      error.set(refusal(503, 'not_ready', 'The database is starting.'));

      const fixture = await render();

      expect(el(fixture, 'members-empty')?.textContent?.trim()).toBe(
        'The members could not be loaded: The database is starting.',
      );
    });

    it('says why with the title when the problem has no detail', async () => {
      error.set(refusal(403, 'forbidden', ''));

      const fixture = await render();

      expect(el(fixture, 'members-empty')?.textContent?.trim()).toBe(
        'The members could not be loaded: Refused',
      );
    });

    it('says why when the backend cannot be reached', async () => {
      error.set(new HttpErrorResponse({ status: 0, statusText: 'Unknown Error' }));

      const fixture = await render();

      expect(el(fixture, 'members-empty')?.textContent?.trim()).toBe(
        'The members could not be loaded: The connection failed; cowork tries again on its own.',
      );
    });
  });
});
